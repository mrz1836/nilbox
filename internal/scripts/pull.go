package scripts

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// trackingRef starts origin's branches as the clone keeps them: the one ref a pull's fetch
// updates is the pulled branch's.
const trackingRef = "refs/remotes/" + originRemote + "/"

// What the window says of stopping a pull: asked first while git fetches, and refused
// once it says it is updating the clone's files, which stopping would leave half written.
const (
	stopPullConfirm = "Stopping a pull partway can leave the clone's files half updated."
	stopPullRefused = "git is updating the clone's files now. Stopping could leave them half updated."
)

// updatingPrefix starts the line git prints as a fast-forward begins writing the clone's
// files, in git's own words: the pull runs with LC_ALL=C, so it is never translated.
const updatingPrefix = "Updating "

// PullIntent is what a pull sets out to do, written into its run's result as it starts, so
// Run again pulls the same branch: the one the clone was on.
type PullIntent struct {
	Branch string `json:"branch"`
}

// pullRun is one pull as it goes: the clone and the branch it pulls, and what its Before
// read and worked out under the clone's lock: the branch on origin it follows, its commit
// before the pull, the clone's own settings git goes without, and git's environment.
type pullRun struct {
	c        clone
	branch   string
	upstream string
	before   string
	aside    []string
	env      []string
}

// Pull pulls the branch the clone at dir, as the picker lists it, with ~, was on when its
// BranchStatus was last worked out, from the branch it follows on origin, and returns the
// run's ID. It runs in the app, in the clone, under the clone's lock, which the runner
// takes and a busy clone fails at once. The run's Before reads the clone again under the
// lock and refuses a clone on another branch, one whose branch follows none on origin,
// and one git is in the middle of something in; then it asks origin for its branches
// again, and works out what git runs with (gitread's RemoteSetup), every one of gitread's
// guards applied, before anything runs. git's pull is fast forward only: it never merges
// or rebases, and a branch with commits of its own fails with git's own message. Its
// Finish reads the branch again, and words what the pull did. Like a script, it is refused
// while the Scripts integration is off.
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
// clone and origin again and working out git's guards under the clone's lock, Stop held
// back while git writes the clone's files, and its Finish reading what the pull did.
func (s *Service) pullSpec(c clone, branch string) scriptrun.Spec {
	run := &pullRun{c: c, branch: branch}
	out := &pullOutput{}

	return scriptrun.Spec{
		Kind: scriptrun.KindPull, Title: "Pull " + branch, Source: "git", Repo: c.name, Dir: c.dir,
		RunsIn: scriptrun.RunsInApp, Intent: PullIntent{Branch: branch}, Output: out,
		Progress:  out.progress,
		Before:    func(ctx context.Context) ([]string, error) { return s.beforePull(ctx, run) },
		BeforeEnv: func() []string { return run.env },
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			return s.finishPull(ctx, run, out, end, engine)
		},
	}
}

// beforePull is a pull's Before: under the clone's lock, it reads the clone again and
// refuses one on another branch than the one asked for, one whose branch follows none on
// origin, and one git is in the middle of something in; asks origin for its branches, with
// gitread's guards, and keeps the answer; and works out what git runs with
// (gitread.RemoteSetup): gitread's own git, the ls-remote's environment and settings, the
// clone's own filter drivers off, and no hook. It returns git's pull.
func (s *Service) beforePull(ctx context.Context, run *pullRun) ([]string, error) {
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

	setup, err := s.branchGit.RemoteSetup(ctx, run.c.dir)
	if err != nil {
		return nil, cantPull("Couldn't work out how to reach origin safely: "+gitread.Message(err), err)
	}

	run.upstream, run.before, run.aside = f.upstream, f.local, setup.SetAside
	run.env = append(slices.Clone(setup.Env), "LC_ALL=C")

	return slices.Concat([]string{setup.Git}, setup.Settings, pullArgs(f.upstream)), nil
}

// movedText says that the clone is no longer on the branch a pull was asked for.
func movedText(asked, now string) string {
	where := "HEAD is detached now"
	if now != "" {
		where = "the clone is on " + now + " now"
	}

	return asked + " is no longer checked out here: " + where + ". Check the branch again before pulling."
}

// finishPull works out a pull's outcome once git ended: a clone moved to another branch
// while git ran, which git may have pulled into instead; what the pull did to the branch,
// read from the branch itself; or git's last error line when it failed. A pull stopped, cut
// off, or whose end wasn't seen keeps the engine's words. Origin's branches as kept are
// brought up to date with what git fetched, or forgotten when that can't be told.
func (s *Service) finishPull(ctx context.Context, run *pullRun, out *pullOutput, end scriptrun.Ending,
	engine scriptrun.Outcome,
) scriptrun.Outcome {
	said := out.lastError()

	if !ranToItsEnd(end) {
		s.forgetHeads(run.c.dir)

		return engine
	}

	if now, err := s.branchGit.CurrentBranch(ctx, run.c.dir); err == nil && now != run.branch {
		s.forgetHeads(run.c.dir)

		return scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: movedDuringText(run.branch, now)}
	}

	if *end.ExitCode != 0 {
		s.forgetHeads(run.c.dir)
		engine.Summary = cmp.Or(failedText(run, said), engine.Summary)

		return engine
	}

	s.fetched(ctx, run)

	after, err := s.branchGit.LocalBranch(ctx, run.c.dir, run.branch)
	if err != nil || after == "" {
		engine.Summary = "git finished, but " + run.branch + " couldn't be read afterwards."

		return engine
	}

	engine.Summary = s.pulled(ctx, run, after)

	return engine
}

// ranToItsEnd reports a run whose program ended on its own, with its exit status known:
// not stopped, cut off, or ended unseen.
func ranToItsEnd(end scriptrun.Ending) bool {
	return !end.Stopped && !end.TimedOut && !end.Quit && end.Interrupted == "" && end.ExitCode != nil
}

// failedText says why a pull failed, from git's last error line said, with any of the
// clone's own settings it went without, which may be why; "" when git said nothing.
func failedText(run *pullRun, said string) string {
	if said == "" {
		return ""
	}

	return strings.TrimSpace("Couldn't pull " + run.branch + ": " + textutil.EndSentence(said) + " " +
		gitread.SetAsideNote(run.aside))
}

// movedDuringText says that the clone was switched to another branch while a pull ran, so
// git may have pulled into that one.
func movedDuringText(asked, now string) string {
	where := "HEAD was detached"
	if now != "" {
		where = "The clone was switched to " + now
	}

	return where + " while the pull ran, so git may have pulled into it rather than " + asked +
		". Look at the clone before going on."
}

// fetched brings origin's branches, as kept for the clone, up to date with the commit
// git's fetch just stored as origin's branch, which origin may have moved since Before
// asked; when it can't be read, they are forgotten, so the next status asks origin.
func (s *Service) fetched(ctx context.Context, run *pullRun) {
	oid, err := s.branchGit.TrackingBranch(ctx, run.c.dir, run.upstream)
	if err != nil || oid == "" {
		s.forgetHeads(run.c.dir)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	kept, ok := s.heads[run.c.dir]
	if !ok {
		return
	}

	heads := maps.Clone(kept.heads)
	heads[run.upstream] = oid
	s.heads[run.c.dir] = keptHeads{heads: heads, at: s.clock.Now()}
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
// one git command the app runs that changes a clone's checkout, every word fixed but the
// branch, which gitread.ValidBranch passed:
//
//   - --ff-only and --no-rebase: fast-forward only, never a rebase (whatever pull.rebase
//     says) or a merge commit, so a branch with commits of its own fails with git's words;
//   - --refmap= and the refspec: the fetch updates origin's branch's own remote-tracking
//     ref and nothing else, never a ref the clone's remote.origin.fetch maps (one could
//     name the branch itself, and force it), and no tag (--no-tags);
//   - --no-autostash: nothing of the user's is set aside; --no-recurse-submodules: no
//     submodule is fetched; --no-verify-signatures: no signature program runs, whatever
//     merge.verifySignatures says; --no-stat: no diff of what changed is worked out;
//   - --upload-pack=git-upload-pack: git's own upload-pack, as gitread's ls-remote asks
//     for, so a remote.origin.uploadpack in the clone's config runs nothing.
func pullArgs(upstream string) []string {
	return append([]string{
		"pull", "--ff-only", "--no-rebase", "--refmap=", "--no-tags", "--no-autostash", "--no-recurse-submodules",
		"--no-verify-signatures", "--no-stat", "--upload-pack=git-upload-pack",
	}, originRemote, pullRefspec(upstream))
}

// pullRefspec fetches origin's branch upstream into its own remote-tracking ref alone,
// forced as git's default refspec forces it; the fast-forward checks the branch itself.
func pullRefspec(upstream string) string {
	return "+" + headsRef + upstream + ":" + trackingRef + upstream
}

// forgetHeads forgets origin's branches as kept for the clone at dir.
func (s *Service) forgetHeads(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.heads, dir)
}

// pullOutput reads a pull's output for its last error line, git's last fatal or error
// line, its prefix cut, or else its last line that isn't a hint, and for the line that
// says git has begun writing the clone's files, after which Stop is held back. It is fed
// the log's bytes in order, split anywhere.
type pullOutput struct {
	mu       sync.Mutex
	lines    textutil.LineSplitter
	said     string
	last     string
	updating bool
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
	case strings.HasPrefix(line, updatingPrefix):
		o.updating = true
	case strings.HasPrefix(line, "fatal: ") || strings.HasPrefix(line, "error: "):
		o.said = execpath.GitMessage([]byte(line))
	}

	o.last = line
}

// progress is the pull's Progress: Stop asks first while git fetches, and is held back
// once git has begun writing the clone's files, until the pull ends.
func (o *pullOutput) progress() scriptrun.Progress {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.updating {
		return scriptrun.Progress{Step: "updating", StopRefused: stopPullRefused}
	}

	return scriptrun.Progress{StopConfirm: stopPullConfirm}
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
