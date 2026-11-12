package scripts

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// trackingRef starts origin's branches as the clone keeps them: the one ref a pull's fetch
// updates is the pulled branch's.
const trackingRef = "refs/remotes/" + originRemote + "/"

// stopPullRefused is why a pull can't be stopped, for its whole run: git writes its output
// only as it exits, so no line says in time that it has begun writing the clone's files,
// which stopping would leave half written. Its kind's limit ends it all the same.
const stopPullRefused = "A pull can't be stopped: stopping it partway could leave the clone's files half " +
	"updated. It ends on its own, within thirty minutes."

// PullIntent is what a pull sets out to do, written into its run's result as it starts, so
// Run again pulls the same branch, the one the clone was on, and a pull followed again
// after a restart (Verifier) is worded as it would have been: the branch, and what the
// pull's Before read under the clone's lock, the branch on origin it follows and the
// branch's commit then.
type PullIntent struct {
	Branch   string `json:"branch"`
	Upstream string `json:"upstream,omitempty"`
	Before   string `json:"before,omitempty"`
}

// pullRun is one pull as it goes: the clone and what it sets out to do, which its Before
// completes under the clone's lock, the clone's own settings git goes without, and git's
// environment, its settings in it.
type pullRun struct {
	c      clone
	intent PullIntent
	aside  []string
	env    []string
}

// Pull pulls the branch the clone at dir, as the picker lists it, with ~, was on when its
// BranchStatus was last worked out, from the branch it follows on origin, and returns the
// run's ID. It runs in the app, in the clone, under the clone's lock, which the runner
// takes and a busy clone fails at once. The run's Before reads the clone again under the
// lock and refuses a clone on another branch, one whose branch follows none on origin,
// and one git is in the middle of something in; then it works out what git runs with
// (gitread's RemoteSetup), every one of gitread's guards applied, before anything runs,
// asking origin nothing: the pull's own fetch is the one time it is asked. git's pull is
// fast forward only: it never merges or rebases, and a branch with commits of its own, or
// one origin no longer has, fails with git's own message. Stop is held back for the whole
// run, and so is a quit, which leaves it going for the next start. Its Finish reads the
// branch again, and words what the pull did. Like a script, it is refused while the Scripts
// integration is off.
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
// clone again and working out git's guards under the clone's lock, Stop held back for its
// whole run, and its Finish reading what the pull did.
func (s *Service) pullSpec(c clone, branch string) scriptrun.Spec {
	run := &pullRun{c: c, intent: PullIntent{Branch: branch}}

	// The intent is recorded once Before has run, so it holds what Before read too.
	return scriptrun.Spec{
		Kind: scriptrun.KindPull, Title: "Pull " + branch, Source: "git", Repo: c.name, Dir: c.dir,
		RunsIn: scriptrun.RunsInApp, Intent: &run.intent, Callbacks: s.pullCallbacks(run),
		Before:    func(ctx context.Context) ([]string, error) { return s.beforePull(ctx, run) },
		BeforeEnv: func() []string { return run.env },
	}
}

// pullCallbacks are the callbacks of run, a pull, built alike for the run pullSpec starts
// and for one from before the app restarted, which the Verifier picks up: git's output
// read for its last error line, Stop held back for its whole run (pullProgress), and a
// Finish that reads what the pull did (finishPull).
func (s *Service) pullCallbacks(run *pullRun) scriptrun.Callbacks {
	out := newPullOutput()

	return scriptrun.Callbacks{
		Output:   out,
		Progress: pullProgress,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			return s.finishPull(ctx, run, out, end, engine)
		},
	}
}

// beforePull is a pull's Before: under the clone's lock, it reads the clone again and
// refuses one on another branch than the one asked for, one whose branch follows none on
// origin, and one git is in the middle of something in; and works out what git runs with
// (gitread.RemoteSetup), reading only the clone: a clone whose own config holds anything
// but what git and Git LFS write there is refused, and otherwise git is gitread's own,
// with the ls-remote's environment, the clone's folders as GIT_DIR and GIT_WORK_TREE, and
// every setting (no hook, no maintenance) passed through the environment. It returns git's
// pull, which holds only the pull's own words.
func (s *Service) beforePull(ctx context.Context, run *pullRun) ([]string, error) {
	f, stop, why := s.readBranch(ctx, run.c.dir)

	switch {
	case stop == BranchUnknown && f.branch == "":
		return nil, cantPull(why, nil)
	case f.branch != run.intent.Branch:
		return nil, cantPull(movedText(run.intent.Branch, f.branch), nil)
	case stop != "":
		return nil, cantPull(why, nil)
	}

	setup, err := s.branchGit.RemoteSetup(ctx, run.c.dir)
	if err != nil {
		return nil, cantPull("Couldn't pull "+run.intent.Branch+". "+textutil.Sentence(gitread.Message(err)), err)
	}

	run.intent.Upstream, run.intent.Before, run.aside = f.upstream, f.local, setup.SetAside
	// LC_ALL=C has git speak the window's language, which the run's summary quotes.
	run.env = append(slices.Clone(setup.Env), "LC_ALL=C")

	return append([]string{setup.Git}, pullArgs(f.upstream)...), nil
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
// read from the branch itself; or git's last error line when it failed, with the clone's
// own settings it went without, which may be why. A pull its limit or a stop cut off is
// read again and said to be cut off partway (cutOff), and one whose end wasn't seen, as one
// followed again after a restart, is settled from the branch alone (settleUnseen).
// Origin's branches as kept are brought up to date with what git fetched, or forgotten
// when that can't be told.
func (s *Service) finishPull(ctx context.Context, run *pullRun, out *pullOutput, end scriptrun.Ending,
	engine scriptrun.Outcome,
) scriptrun.Outcome {
	said := out.lastError()

	switch {
	case end.Stopped || end.TimedOut || end.Quit:
		return s.cutOff(ctx, run, engine)
	case !ranToItsEnd(end):
		return s.settleUnseen(ctx, run, engine)
	}

	if now, err := s.branchGit.CurrentBranch(ctx, run.c.dir); err == nil && now != run.intent.Branch {
		s.forgetHeads(run.c.dir)

		return scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: movedDuringText(run.intent.Branch, now)}
	}

	if *end.ExitCode != 0 {
		s.forgetHeads(run.c.dir)
		engine.Summary = cmp.Or(failedText(run, said), engine.Summary)

		return engine
	}

	s.fetched(ctx, run)

	after, err := s.branchGit.LocalBranch(ctx, run.c.dir, run.intent.Branch)
	if err != nil || after == "" {
		engine.Summary = "git finished, but " + run.intent.Branch + " couldn't be read afterwards."

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

// settleUnseen works out what a pull whose end wasn't seen did, from the branch alone: a
// clone now on another branch fails, as git may have pulled into it; a branch moved on
// from its commit before the pull was pulled, which a fast-forward-only pull alone does
// here; and one where it was keeps the engine's words, saying so. Origin's branches as
// kept are forgotten, as what git fetched can't be told.
func (s *Service) settleUnseen(ctx context.Context, run *pullRun, engine scriptrun.Outcome) scriptrun.Outcome {
	s.forgetHeads(run.c.dir)

	if now, err := s.branchGit.CurrentBranch(ctx, run.c.dir); err == nil && now != run.intent.Branch {
		return scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: movedDuringText(run.intent.Branch, now)}
	}

	after, err := s.branchGit.LocalBranch(ctx, run.c.dir, run.intent.Branch)
	if err != nil || after == "" || run.intent.Before == "" {
		return engine
	}

	if after != run.intent.Before {
		return scriptrun.Outcome{Status: scriptrun.StatusSucceeded, Summary: s.pulled(ctx, run, after)}
	}

	engine.Summary = textutil.EndSentence(engine.Summary) + " " + run.intent.Branch + " is where it was, at " +
		short(after) + "."

	return engine
}

// cutOff works out what a pull its limit or a stop cut off left, reading the clone again:
// the branch it is on, the branch's commit against the one before the pull, whether git's
// index is still locked, and what git is in the middle of. It keeps the engine's status
// and says, after the engine's words, that the pull was cut off partway, so the clone may
// be partly updated, what it found, and what to do: open the clone in a terminal and run
// git status. Origin's branches as kept are forgotten, as what git fetched can't be told.
func (s *Service) cutOff(ctx context.Context, run *pullRun, engine scriptrun.Outcome) scriptrun.Outcome {
	s.forgetHeads(run.c.dir)

	found := s.leftBehind(ctx, run)

	text := textutil.EndSentence(engine.Summary) + " The pull was cut off partway, so the clone may be partly updated"
	if len(found) > 0 {
		text += ": " + strings.Join(found, ", ")
	}

	engine.Summary = text + ". Open the clone in a terminal and run git status to see where it stands."

	return engine
}

// leftBehind reads what a pull cut off left in the clone, each in a few words: another
// branch checked out, where the branch is against its commit before the pull, git's index
// still locked, and what git is in the middle of. A read that fails says nothing.
func (s *Service) leftBehind(ctx context.Context, run *pullRun) []string {
	g, dir, branch, before := s.branchGit, run.c.dir, run.intent.Branch, run.intent.Before

	var found []string

	if now, err := g.CurrentBranch(ctx, dir); err == nil && now != branch {
		found = append(found, onNow(now))
	}

	if after, err := g.LocalBranch(ctx, dir, branch); err == nil && after != "" {
		found = append(found, branchMoved(branch, before, after))
	}

	if locked, err := g.IndexLocked(ctx, dir); err == nil && locked {
		found = append(found, "git's index is still locked (.git/index.lock)")
	}

	if op, err := g.InProgress(ctx, dir); err == nil && op != gitread.OperationNone {
		found = append(found, "git is "+string(op)+" there")
	}

	return found
}

// branchMoved says where branch is, at after, against before, its commit before the pull.
func branchMoved(branch, before, after string) string {
	if before == "" || after == before {
		return branch + " is still at " + short(after)
	}

	return branch + " moved from " + short(before) + " to " + short(after)
}

// onNow says which branch the clone is on now, or that HEAD is detached.
func onNow(now string) string {
	if now == "" {
		return "HEAD is detached now"
	}

	return "the clone is on " + now + " now"
}

// failedText says why a pull failed, from git's last error line said, with any of the
// clone's own settings it went without, which may be why; "" when git said nothing.
func failedText(run *pullRun, said string) string {
	if said == "" {
		return ""
	}

	return strings.TrimSpace("Couldn't pull " + run.intent.Branch + ": " + textutil.EndSentence(said) + " " +
		gitread.SetAsideNote(originRemote, run.aside))
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

// fetched brings the pulled branch on origin, as kept for the clone, up to date with the
// commit git's fetch just stored as origin's branch, which origin may have moved since it
// was asked, keeping when it was asked: only that branch's commit is new, and every other
// branch kept stays as old as it was. When it can't be read, origin's branches as kept
// are forgotten, so the next status asks origin.
func (s *Service) fetched(ctx context.Context, run *pullRun) {
	oid, err := s.branchGit.TrackingBranch(ctx, run.c.dir, run.intent.Upstream)
	if err != nil || oid == "" {
		s.forgetHeads(run.c.dir)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	head, ok := s.heads[run.c.dir][run.intent.Upstream]
	if !ok {
		return
	}

	kept := maps.Clone(s.heads[run.c.dir])
	head.oid = oid
	kept[run.intent.Upstream] = head
	s.heads[run.c.dir] = kept
}

// pulled says what a pull that ended well did to the branch, now at after: nothing, as it
// was up to date, or which commits it went from and to, and how many it brought.
func (s *Service) pulled(ctx context.Context, run *pullRun, after string) string {
	switch {
	case after == run.intent.Before:
		return run.intent.Branch + " was already up to date."
	case run.intent.Before == "":
		return "Updated " + run.intent.Branch + " to " + short(after) + "."
	}

	text := "Updated " + run.intent.Branch + " from " + short(run.intent.Before) + " to " + short(after)

	if n, err := s.branchGit.CommitsBetween(ctx, run.c.dir, run.intent.Before, after); err == nil {
		text += ", " + textutil.Counted(n, "new commit", "new commits")
	}

	return text + "."
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

// pullProgress is a pull's Progress: Stop is held back for its whole run (stopPullRefused),
// and a quit too, within scriptrun.QuitKillAfter, after which it is left going.
func pullProgress() scriptrun.Progress {
	return scriptrun.Progress{StopRefused: stopPullRefused}
}

// Verifier gives a pull from before the app restarted, followed again or found ended,
// what its Spec gave it, from the run's record alone (scriptrun.Verifier): Stop and a quit
// held back for its whole run, as git may still be writing the clone's files, and its
// Finish, which words what it did as its Spec's would, or, when its end wasn't seen,
// settles it from the branch (settleUnseen). Core registers it for scriptrun.KindPull.
type Verifier struct {
	s *Service
}

// NewVerifier returns the Verifier of s's pulls.
func NewVerifier(s *Service) Verifier {
	return Verifier{s: s}
}

// Verify gives a pull from before the app restarted what its Spec gave it. A record whose
// intent can't be read still has Stop held back, and keeps the runner's own outcome.
func (v Verifier) Verify(rec scriptrun.Record) scriptrun.Verification {
	if rec.Kind != scriptrun.KindPull {
		return scriptrun.Verification{}
	}

	in, err := scriptrun.Decode[PullIntent](rec.Result.Intent)
	if err != nil || !gitread.ValidBranch(in.Branch) || v.s.branchGit == nil {
		v.s.logger.Warn("a pull from before the app restarted can't be checked", "run", rec.ID)

		return scriptrun.Verification{Callbacks: scriptrun.Callbacks{Progress: pullProgress}}
	}

	run := &pullRun{c: clone{dir: rec.Dir, name: rec.Repo}, intent: in}

	return scriptrun.Verification{Callbacks: v.s.pullCallbacks(run)}
}

// pullOutput reads a pull's output for its last error line, git's last fatal or error
// line, its prefix cut, or else its last line that isn't a hint. It is fed the log's bytes
// in order, split anywhere (see scriptrun.LineWriter).
type pullOutput struct {
	*scriptrun.LineWriter

	said string
	last string
}

// newPullOutput returns a reader that has read nothing.
func newPullOutput() *pullOutput {
	o := &pullOutput{}
	o.LineWriter = scriptrun.NewLineWriter(o.read)

	return o
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
	o.Flush()

	o.Lock()
	defer o.Unlock()

	return cmp.Or(o.said, o.last)
}

// cantPull refuses a pull, saying why.
func cantPull(why string, err error) error {
	if err == nil {
		return uierr.New(why, ErrCantPull)
	}

	return uierr.New(why, fmt.Errorf("%w: %w", ErrCantPull, err))
}
