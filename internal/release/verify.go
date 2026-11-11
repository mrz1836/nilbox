package release

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// verifyTimeout bounds verifying a release: origin's answer about the tag, which gitread
// already stops after RemoteTimeout, and three quick reads of the clone.
const verifyTimeout = gitread.RemoteTimeout + 5*time.Second

// Intent is what a release sets out to do, written into its run's result as it starts
// (scriptrun.Spec.Intent), so that a release cut short, or left running when the app
// quit, is still verified against it.
type Intent struct {
	// Version is the version the button showed, and Bump the bump that makes it.
	Version string `json:"version"`
	Bump    Bump   `json:"bump"`

	// Branch is the branch released from, and Original the one the clone was on when the
	// sheet opened, which magex switches back to.
	Branch   string `json:"branch"`
	Original string `json:"original"`

	// Head is the commit the sheet showed, the newest on origin's branch then.
	Head string `json:"head"`

	// FastForward reports that magex was told to update the branch by fast-forward only
	// (ff-only), so it never rebases there.
	FastForward bool `json:"fastForward"`
}

// ResultKind is how a release came out, as the app verified it after magex ended.
type ResultKind string

// The ways a release comes out.
const (
	// ResultReleased is a release origin has: its tag is on GitHub.
	ResultReleased ResultKind = "released"

	// ResultNotPushed is a tag made on this Mac that origin doesn't have: magex skipped
	// or failed the push, or GitHub refused the tag.
	ResultNotPushed ResultKind = "notPushed"

	// ResultFailed is a release that made no tag at all.
	ResultFailed ResultKind = "failed"

	// ResultUnverified is a release origin couldn't be asked about: it may be on GitHub.
	ResultUnverified ResultKind = "unverified"
)

// Result is a release's result for the window: its run's outcome detail
// (scriptrun.Result.Outcome), worked out from what magex said and what origin and the
// clone say once it ended, never from its exit status alone (§5.11.6).
type Result struct {
	// Kind is how it came out.
	Kind ResultKind `json:"kind"`

	// Version is the version the sheet showed, and Tag the one magex made, or set out to;
	// Moved reports they differ, as when a version was tagged meanwhile and magex, without
	// expect=, made the next one.
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Moved   bool   `json:"moved"`

	// Commit is the commit the release's tag points at on origin, for ResultReleased. Head
	// is the commit the sheet showed, and Newer reports the release tagged another, a
	// commit that landed between the sheet and the run.
	Commit string `json:"commit"`
	Head   string `json:"head"`
	Newer  bool   `json:"newer"`

	// Taken reports origin has the tag at a commit this release didn't tag: someone else
	// released that version, and this release isn't on GitHub.
	Taken bool `json:"taken"`

	// Tagged reports the clone has the tag.
	Tagged bool `json:"tagged"`

	// Branch is the branch the clone is on now, "" when Detached or unknown; Original is
	// the one it was on when the sheet opened, and Back reports it is on that one again.
	// InProgress is what git is in the middle of there, as a rebase a stopped pull left.
	Branch     string            `json:"branch"`
	Detached   bool              `json:"detached"`
	Original   string            `json:"original"`
	Back       bool              `json:"back"`
	InProgress gitread.Operation `json:"inProgress"`

	// TagURL is the tag's page on GitHub, which shows the release once its workflow
	// publishes it, and WorkflowURL the repository's Actions runs for the tag; both are ""
	// unless it was released.
	TagURL      string `json:"tagUrl"`
	WorkflowURL string `json:"workflowUrl"`

	// PushWarning is magex's warning that it skipped the push, and Commands the commands
	// to finish or undo a tag origin doesn't have, quoted for a shell, which the app never
	// runs.
	PushWarning string   `json:"pushWarning"`
	Commands    []string `json:"commands"`

	// Failure names what stopped a release that isn't on GitHub, or is nil.
	Failure *Failure `json:"failure"`

	// Error says why origin couldn't be asked, for ResultUnverified.
	Error string `json:"error"`
}

// verifyGit is what verifying a release reads with git: *gitread.Git.
type verifyGit interface {
	RemoteTagCommit(ctx context.Context, dir, tag string) (string, error)
	LocalTagCommit(ctx context.Context, dir, tag string) (string, error)
	CurrentBranch(ctx context.Context, dir string) (string, error)
	InProgress(ctx context.Context, dir string) (gitread.Operation, error)
}

// finisher returns a release's Finish: once magex has ended and the reader has read all of
// its output, it verifies the release in the clone at dir against in, which Before
// completed, and works out the run's outcome. When its context ends first, as the app
// quits, origin's answer is missing: the outcome is unsettled, and the release stays
// recorded as going for the next start to verify (Verifier).
func (s *Service) finisher(name repo.Name, dir string, in *Intent, reader *OutputReader,
) func(context.Context, scriptrun.Ending, scriptrun.Outcome) scriptrun.Outcome {
	return func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
		reader.Flush()
		said := reader.Snapshot()

		got := s.look(ctx, dir, tagOf(said, *in))
		if got.err != nil && ctx.Err() != nil {
			return scriptrun.Outcome{Unsettled: true}
		}

		res := judge(name, *in, said, end, got)
		s.released(ctx, name, dir, res)

		return outcomeOf(res, said, engine)
	}
}

// released notes that a release ended: the clone's kept facts are out of date, and so is
// the repository's row on the Release page, which the Refresher reads again and patches
// into the list, telling the window (event.ReleasesUpdated) whether or not the sheet is
// open (queuePatch); and a failure only a bug causes is logged as one.
func (s *Service) released(ctx context.Context, name repo.Name, dir string, res Result) {
	s.forget(dir)
	s.queuePatch(name)

	if res.Failure != nil && res.Failure.Bug {
		s.logger.ErrorContext(ctx, "magex wanted a confirmation the release didn't send; this is a bug",
			"repo", name.String(), "version", res.Version)
	}
}

// announcer returns a release's scriptrun Recorded: once the runner has recorded the
// release's outcome, a release on GitHub is told to the user (release_done). It is told
// only then, so a release whose record failed, which the next start verifies again, is
// told once, by the start whose record holds.
func (s *Service) announcer(name repo.Name, in *Intent) func(context.Context, int64, scriptrun.Outcome) {
	return func(ctx context.Context, _ int64, out scriptrun.Outcome) {
		res, ok := out.Detail.(Result)
		if !ok || res.Kind != ResultReleased || s.notifier == nil {
			return
		}

		s.notifier.Released(ctx, noticeOf(name, *in, res))
	}
}

// Notifier tells the user that a release reached GitHub: a notification in the log, and
// its banner when the user has it on (release_done, §5.19). Core adapts the notify
// package's Dispatcher. It must return promptly; a failure is its own to log.
type Notifier interface {
	Released(ctx context.Context, n Notice)
}

// Notice is what the user is told when a release's tag reaches GitHub.
type Notice struct {
	// Repo is the repository released.
	Repo repo.Name

	// Title says what happened ("v1.4.3 is on GitHub"), Body where the tag went ("Tagged
	// a1b2c3d on master."), and URL is the tag's page on GitHub.
	Title string
	Body  string
	URL   string
}

// noticeOf words the notice of a release on GitHub: the tag, the commit and branch it
// tagged, and the version that appeared meanwhile when it moved.
func noticeOf(name repo.Name, in Intent, res Result) Notice {
	body := "Tagged " + res.Commit[:min(len(res.Commit), shortOIDLen)]
	if in.Branch != "" {
		body += " on " + in.Branch
	}

	body += "."

	if res.Moved {
		body += " " + res.Version + " appeared on GitHub meanwhile."
	}

	return Notice{Repo: name, Title: res.Tag + " is on GitHub", Body: body, URL: res.TagURL}
}

// tagOf is the tag a release made, or set out to: magex's JSON result's, the tag it said it
// created, the version its summary said it makes, or else the intended one.
func tagOf(said Output, in Intent) string {
	result := ""
	if said.Result != nil {
		result = said.Result.Tag
	}

	return cmp.Or(result, said.Tag, said.Version, in.Version)
}

// seen is what verifying found: the commit origin's tag points at, or why origin couldn't
// say; the commit the clone's own tag points at; the branch the clone is on; and what git
// is in the middle of there. Each commit is "" without the tag.
type seen struct {
	tag         string
	commit      string
	err         error
	local       string
	branch      string
	branchKnown bool
	op          gitread.Operation
}

// look verifies a release in the clone at dir: it asks origin for tag, peeled
// (`ls-remote origin refs/tags/<tag> refs/tags/<tag>^{}`), and the clone for the tag, its
// branch, and what git is in the middle of. It takes at most verifyTimeout, and returns at
// once when ctx ends, with origin's answer missing.
func (s *Service) look(ctx context.Context, dir, tag string) seen {
	if err := ctx.Err(); err != nil {
		return seen{tag: tag, err: err}
	}

	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	done := make(chan seen, 1)

	go func() { done <- s.read(ctx, dir, tag) }()

	select {
	case got := <-done:
		return got
	case <-ctx.Done():
		return seen{tag: tag, err: ctx.Err()}
	}
}

// read reads what look returns: the clone first, which is quick, then origin. Only
// origin's answer is needed: the clone's reads are best effort.
func (s *Service) read(ctx context.Context, dir, tag string) seen {
	out := seen{tag: tag}

	if local, err := s.git.LocalTagCommit(ctx, dir, tag); err == nil {
		out.local = local
	}

	if branch, err := s.git.CurrentBranch(ctx, dir); err == nil {
		out.branch, out.branchKnown = branch, true
	}

	if op, err := s.git.InProgress(ctx, dir); err == nil {
		out.op = op
	}

	out.commit, out.err = s.git.RemoteTagCommit(ctx, dir, tag)

	return out
}

// judge works a release's result out from what it set out to do, what magex said, how the
// run ended, and what origin and the clone say now: on GitHub, tagged here only, no tag at
// all, or unknown. Only this release's own tag counts as released: origin's tag must point
// at the commit this run tagged, as magex's JSON result names it, or else as the clone's
// tag does when magex said it made that tag. A tag origin has at any other commit, or with
// no tag of this run's to match, is someone else's: Taken.
func judge(name repo.Name, in Intent, said Output, end scriptrun.Ending, got seen) Result {
	res := cloneSide(in, got)
	res.Version, res.Tag, res.Moved, res.Head = in.Version, got.tag, got.tag != in.Version, in.Head
	res.Tagged = got.local != ""

	// ownTag is a tag of this run's in the clone that origin has at another commit.
	ownTag := res.Tagged && got.local != got.commit

	switch mine := taggedCommit(said, got); {
	case got.err != nil:
		res.Kind, res.Error = ResultUnverified, gitMessage(got.err)
	case got.commit != "" && got.commit == mine:
		return releasedResult(name, res, got.commit)
	case got.commit != "":
		res.Kind, res.Taken = ResultFailed, true
		if ownTag {
			res.Commands = []string{"git tag -d " + shellWord(got.tag)}
		}
	case res.Tagged:
		res.Kind, res.PushWarning = ResultNotPushed, said.PushSkipWarning
		res.Commands = []string{
			"git push " + remoteOrigin + " " + shellWord(got.tag), "git tag -d " + shellWord(got.tag),
		}
	default:
		res.Kind = ResultFailed
	}

	res.Failure = failureOf(res, in, said, end, ownTag)

	return res
}

// taggedCommit is the commit this run tagged: magex's JSON result's, or the clone's tag's
// when magex said it made the tag; "" when the run made no tag of its own.
func taggedCommit(said Output, got seen) string {
	if r := said.Result; r != nil && r.Tag == got.tag && r.Commit != "" {
		return r.Commit
	}

	if tagMade(said, got.tag) {
		return got.local
	}

	return ""
}

// failureOf names what stopped a release that isn't on GitHub: always for one that made
// no tag, else only when magex said something to name. A version someone else released
// reads as taken when nothing else names it: at another commit than ownTag, this run's
// own tag in the clone, or with no tag of this run's at all. A tag magex kept because it
// couldn't reach origin needs no name when magex's error only repeats that: the result
// quotes magex's warning, which says so (magex 1.30.0 and later fail with it).
func failureOf(res Result, in Intent, said Output, end scriptrun.Ending, ownTag bool) *Failure {
	f := ExplainFailure(FailureInput{Output: said, End: end, Version: in.Version, Branch: in.Branch})

	switch {
	case res.Kind == ResultNotPushed && f.Kind == FailureOther && repeatsPushWarning(f.Quote, res.PushWarning):
		return nil
	case f.Kind != FailureOther || f.Quote != "":
		return &f
	case res.Taken && ownTag:
		return &Failure{Kind: FailureTaken, Heading: takenHeading}
	case res.Taken:
		return &Failure{Kind: FailureTaken, Heading: "Someone else released " + res.Tag + " meanwhile."}
	case res.Kind == ResultFailed:
		return &f
	default:
		return nil
	}
}

// repeatsPushWarning reports that quote, magex's error, only repeats the cause in warning,
// its warning that origin couldn't be reached: "failed to push tag: <cause>" after "Git
// remote validation failed: <cause>".
func repeatsPushWarning(quote, warning string) bool {
	cause := strings.TrimSpace(mxPushSkipped.ReplaceAllString(warning, ""))

	return cause != "" && cause != warning && strings.HasSuffix(quote, cause)
}

// cloneSide is the part of a result that says where the clone is: its branch, whether it
// is back on the one the sheet saw, and what git is in the middle of there.
func cloneSide(in Intent, got seen) Result {
	return Result{
		Branch: got.branch, Detached: got.branchKnown && got.branch == "", Original: in.Original,
		Back: got.branchKnown && in.Original != "" && got.branch == in.Original, InProgress: got.op,
		Commands: []string{},
	}
}

// releasedResult completes res for a tag origin has at commit: whether it is newer than
// the one the sheet showed, and the links to the tag and its workflow runs.
func releasedResult(name repo.Name, res Result, commit string) Result {
	res.Kind, res.Commit, res.Newer = ResultReleased, commit, res.Head != "" && commit != res.Head

	if name.Valid() {
		res.TagURL = "https://github.com/" + name.String() + "/releases/tag/" + url.PathEscape(res.Tag)
		res.WorkflowURL = "https://github.com/" + name.String() + "/actions?query=" + url.QueryEscape("branch:"+res.Tag)
	}

	return res
}

// tagMade reports magex said it made tag on this Mac.
func tagMade(said Output, tag string) bool {
	if said.Tag == tag && tag != "" {
		return true
	}

	return said.Result != nil && said.Result.Tag == tag && tag != ""
}

// outcomeOf turns a release's result into its run's outcome: succeeded only when the tag
// is on GitHub. Anything else failed, unless the user stopped it or its end wasn't seen,
// which the engine's own outcome says. Its warnings are magex's notable ones.
func outcomeOf(res Result, said Output, engine scriptrun.Outcome) scriptrun.Outcome {
	out := scriptrun.Outcome{
		Status: scriptrun.StatusFailed, Summary: summaryOf(res, engine), Warnings: said.NotableWarnings(),
		Unrecognized: said.Unrecognized, UnrecognizedCount: said.UnrecognizedCount, Detail: res,
	}

	switch {
	case res.Kind == ResultReleased:
		out.Status = scriptrun.StatusSucceeded
	case engine.Status == scriptrun.StatusStopped || engine.Status == scriptrun.StatusInterrupted:
		out.Status = engine.Status
	}

	return out
}

// summaryOf says, in a sentence, how a release came out.
func summaryOf(res Result, engine scriptrun.Outcome) string {
	switch res.Kind {
	case ResultReleased:
		if res.Moved {
			return fmt.Sprintf("Released %s. %s appeared on GitHub meanwhile.", res.Tag, res.Version)
		}

		return res.Tag + " is on GitHub."
	case ResultNotPushed:
		return res.Tag + " is tagged on this Mac but isn't on GitHub."
	case ResultUnverified:
		return textutil.WithReason(fmt.Sprintf("Couldn't check whether %s reached GitHub", res.Tag), res.Error)
	case ResultFailed:
	}

	// magex's words when it said what stopped it, else the runner's: stopped, past its time
	// limit, or the exit status.
	if f := res.Failure; f != nil && (f.Kind != FailureOther || f.Quote != "") {
		return f.Heading
	}

	return engine.Summary
}

// Verifier verifies the releases from before the app restarted (scriptrun.Verifier): a
// release orphaned, still running, or interrupted gets an output reader and a Finish
// again, built from its record's intent, so it is read and verified as a release the app
// started is. Core registers it with the runner for scriptrun.KindRelease. It is its own
// type, not a method of the Service, which the window binds.
type Verifier struct {
	s *Service
}

// NewVerifier returns the Verifier of s's releases.
func NewVerifier(s *Service) Verifier {
	return Verifier{s: s}
}

// Verify gives a release from before the app restarted its reader, its progress, its
// Recorded, and its Finish, which first waits for the login shell's environment
// (Config.Ready). A record
// whose intent can't be read, or a Service that can't read git, gets none: the runner's own
// outcome stands.
func (v Verifier) Verify(run scriptrun.Record) scriptrun.Verification {
	in, ok := intentOf(run.Result.Intent)
	if !ok || v.s.git == nil {
		v.s.logger.Warn("a release from before the app restarted can't be verified", "run", run.ID)

		return scriptrun.Verification{}
	}

	reader := NewOutputReader()
	finish := v.s.finisher(run.Repo, run.Dir, &in, reader)

	return scriptrun.Verification{
		Output:   reader,
		Progress: func() scriptrun.Progress { return reader.Progress(in) },
		Recorded: v.s.announcer(run.Repo, &in),
		Finish: func(ctx context.Context, end scriptrun.Ending, out scriptrun.Outcome) scriptrun.Outcome {
			v.s.awaitShell(ctx)

			return finish(ctx, end, out)
		},
	}
}

// awaitShell waits, until ctx ends, for the login shell's environment to have been read
// once: the runs from before a restart are picked up as the app starts, alongside that
// read. A context that ends first leaves the release unsettled, for the next start.
func (s *Service) awaitShell(ctx context.Context) {
	if s.ready != nil {
		shellenv.Await(ctx, s.ready())
	}
}

// intentOf reads a release's intent back from its record, where it was decoded as plain
// JSON values, and reports whether it names a version.
func intentOf(recorded any) (Intent, bool) {
	data, err := json.Marshal(recorded)
	if err != nil {
		return Intent{}, false
	}

	var in Intent
	if err = json.Unmarshal(data, &in); err != nil {
		return Intent{}, false
	}

	_, ok := ParseVersion(in.Version)

	return in, ok
}
