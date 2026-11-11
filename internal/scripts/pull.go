package scripts

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// stopPullConfirm is what the window asks before it stops a pull: git may be writing the
// clone's files.
const stopPullConfirm = "Stopping a pull partway can leave the clone's files half updated."

// PullIntent is what a pull sets out to do, written into its run's result as it starts, so
// Run again pulls the same branch: the one the clone was on.
type PullIntent struct {
	Branch string `json:"branch"`
}

// pullRun is one pull as it goes: the clone and the branch it pulls, and what its Before
// read under the clone's lock, the branch's commit before the pull.
type pullRun struct {
	c      clone
	branch string
	before string
}

// Pull pulls the branch the clone at dir, as the picker lists it, with ~, was on when its
// BranchStatus was last worked out, from the branch it follows on origin, and returns the
// run's ID. It runs in the app, in the clone, under the clone's lock, which the runner
// takes and a busy clone fails at once. The run's Before reads the clone again under the
// lock and refuses a clone on another branch, one whose branch follows none on origin,
// and one git is in the middle of something in; then it asks origin for its branches
// again, every one of gitread's guards applied, before anything runs. git's pull is fast
// forward only: it never merges or rebases, and a branch with commits of its own fails
// with git's own message. Its Finish reads the branch again, and words what the pull did.
// Like a script, it is refused while the Scripts integration is off.
func (s *Service) Pull(ctx context.Context, dir string) (int64, error) {
	if s.runs == nil || s.branchGit == nil {
		return 0, ErrNotSetUp
	}

	if err := s.pullsOff(ctx); err != nil {
		return 0, err
	}

	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	branch := s.shown[c.dir]
	s.mu.Unlock()

	if branch == "" {
		return 0, cantPull("Check "+c.display()+"'s branch again before pulling it.", nil)
	}

	id, err := s.runs.Start(ctx, s.pullSpec(c, branch))
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a pull started", "run", id, "repo", c.display())

	return id, nil
}

// pullsOff refuses a pull, saying why, while the Scripts integration is off.
func (s *Service) pullsOff(ctx context.Context) error {
	cfg, err := s.config(ctx)
	if err != nil {
		return err
	}

	if why := scriptsOff(cfg, s.toolsNow(ctx)); why != "" {
		return uierr.New(why, ErrCantRun)
	}

	return nil
}

// pullSpec is the run that pulls branch in c: git, in the app, its Before reading the
// clone and origin again under the clone's lock, and its Finish reading what the pull did.
func (s *Service) pullSpec(c clone, branch string) scriptrun.Spec {
	run := &pullRun{c: c, branch: branch}
	out := &pullOutput{}
	base, _ := s.env()

	if base == nil {
		base = os.Environ()
	}

	return scriptrun.Spec{
		Kind: scriptrun.KindPull, Title: "Pull " + branch, Source: "git", Repo: c.name, Dir: c.dir,
		Env: pullEnv(base), RunsIn: scriptrun.RunsInApp, Intent: PullIntent{Branch: branch}, Output: out,
		Progress: func() scriptrun.Progress { return scriptrun.Progress{StopConfirm: stopPullConfirm} },
		Before:   func(ctx context.Context) ([]string, error) { return s.beforePull(ctx, run, base) },
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			return s.finishPull(ctx, run, out, end, engine)
		},
	}
}

// beforePull is a pull's Before: under the clone's lock, it reads the clone again and
// refuses one on another branch than the one asked for, one whose branch follows none on
// origin, and one git is in the middle of something in; asks origin for its branches, so
// every one of gitread's guards on reaching origin applies before git does, and keeps the
// answer; and returns git's pull, found on env's PATH.
func (s *Service) beforePull(ctx context.Context, run *pullRun, env []string) ([]string, error) {
	f, stop, why := s.readBranch(ctx, run.c.dir)

	switch {
	case stop == BranchUnknown && f.branch == "":
		return nil, cantPull(why, nil)
	case f.branch != run.branch:
		return nil, cantPull(movedText(run.branch, f.branch), nil)
	case stop != "":
		return nil, cantPull(why, nil)
	}

	refs, err := s.branchGit.RemoteRefs(ctx, run.c.dir)
	if err != nil {
		return nil, cantPull("Couldn't ask origin about "+f.upstream+": "+gitread.Message(err), err)
	}

	if s.keepHeads(run.c.dir, refs.Heads).heads[f.upstream] == "" {
		return nil, cantPull("origin has no branch "+f.upstream+".", nil)
	}

	git, err := execpath.Search{Path: shellenv.Lookup(env, "PATH"), Home: shellenv.Lookup(env, "HOME")}.Find("git")
	if err != nil {
		return nil, cantPull("Couldn't find git on your PATH.", err)
	}

	run.before = f.local

	return slices.Concat([]string{git}, pullSettings(), pullArgs(f.upstream)), nil
}

// movedText says that the clone is no longer on the branch a pull was asked for.
func movedText(asked, now string) string {
	where := "HEAD is detached now"
	if now != "" {
		where = "the clone is on " + now + " now"
	}

	return asked + " is no longer checked out here: " + where + ". Check the branch again before pulling."
}

// finishPull works out a pull's outcome once git ended: what it did to the branch, read
// from the branch itself, or git's last error line when it failed. A pull stopped, cut
// off, or whose end wasn't seen keeps the engine's words.
func (s *Service) finishPull(ctx context.Context, run *pullRun, out *pullOutput, end scriptrun.Ending,
	engine scriptrun.Outcome,
) scriptrun.Outcome {
	said := out.lastError()

	switch {
	case end.Stopped || end.TimedOut || end.Quit || end.Interrupted != "" || end.ExitCode == nil:
		return engine
	case *end.ExitCode != 0:
		if said != "" {
			engine.Summary = "Couldn't pull " + run.branch + ": " + textutil.EndSentence(said)
		}

		return engine
	}

	after, err := s.branchGit.LocalBranch(ctx, run.c.dir, run.branch)
	if err != nil || after == "" {
		engine.Summary = "git finished, but " + run.branch + " couldn't be read afterwards."

		return engine
	}

	engine.Summary = s.pulled(ctx, run, after)

	return engine
}

// pulled says what a pull that ended well did to the branch, now at after: nothing, as it
// was up to date, or which commits it went from and to, and how many it brought.
func (s *Service) pulled(ctx context.Context, run *pullRun, after string) string {
	switch {
	case after == run.before:
		return run.branch + " was already up to date."
	case run.before == "":
		return "Updated " + run.branch + " to " + short(after) + "."
	}

	text := "Updated " + run.branch + " from " + short(run.before) + " to " + short(after)

	if n, err := s.branchGit.CommitsBetween(ctx, run.c.dir, run.before, after); err == nil {
		text += fmt.Sprintf(", %d new commit%s", n, plural(n))
	}

	return text + "."
}

// plural is the "s" a count other than one takes.
func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}

// pullArgs is git's pull of origin's branch upstream into the branch the clone is on, the
// one git command the app runs that changes a clone's checkout: fast-forward only, never
// a rebase (whatever pull.rebase says) or a merge commit, so a branch with commits of its
// own fails with git's own message; no autostash, so nothing of the user's is set aside;
// no submodule recursion; git's own upload-pack, as gitread's ls-remote asks for, so a
// remote.origin.uploadpack in the clone's config runs nothing; and the branch's full ref,
// so no tag of the same name stands in for it.
func pullArgs(upstream string) []string {
	return []string{
		"pull", "--ff-only", "--no-rebase", "--no-autostash", "--no-recurse-submodules", "--upload-pack=git-upload-pack",
		originRemote, headsRef + upstream,
	}
}

// pullSettings are the settings the pull runs with, those internal/worktrees runs every
// git command with: no hook, which would run a program from the clone's own files, no
// file system monitor, and no submodule recursion.
func pullSettings() []string {
	return []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "submodule.recurse=false",
	}
}

// pullEnv is a pull's environment: the login shell's, so git, ssh, and the credential
// helpers are the ones the user's terminal runs, the ssh agent included, with
// GIT_TERMINAL_PROMPT=0, since no one can answer a prompt in the app, and without the
// variables that would point git at another repository than the clone it runs in.
func pullEnv(base []string) []string {
	dropped := []string{
		"GIT_TERMINAL_PROMPT", "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	}
	env := make([]string, 0, len(base)+1)

	for _, kv := range base {
		if key, _, _ := strings.Cut(kv, "="); !slices.Contains(dropped, key) {
			env = append(env, kv)
		}
	}

	return append(env, "GIT_TERMINAL_PROMPT=0")
}

// pullOutput reads a pull's output for its last error line: git's last fatal or error
// line, its prefix cut, or else its last line that isn't a hint, as a translated git
// words its prefixes otherwise. It is fed the log's bytes in order, split anywhere.
type pullOutput struct {
	mu    sync.Mutex
	lines textutil.LineSplitter
	said  string
	last  string
}

// Write reads p, the next part of the output. It never fails.
func (o *pullOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.lines.Write(p, o.read)

	return len(p), nil
}

// read reads one line.
func (o *pullOutput) read(line string) {
	line = strings.TrimSpace(line)

	switch {
	case line == "" || strings.HasPrefix(line, "hint:"):
		return
	case strings.HasPrefix(line, "fatal: ") || strings.HasPrefix(line, "error: "):
		o.said = execpath.GitMessage([]byte(line))
	}

	o.last = line
}

// lastError returns git's last error line, once the output has ended, or "" for none.
func (o *pullOutput) lastError() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.lines.Flush(o.read)

	return cmp.Or(o.said, o.last)
}

// cantPull refuses a pull, saying why.
func cantPull(why string, err error) error {
	if err == nil {
		return uierr.New(why, ErrCantPull)
	}

	return uierr.New(why, fmt.Errorf("%w: %w", ErrCantPull, err))
}
