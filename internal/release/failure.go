package release

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// FailureKind names what stopped a release, one for each heading.
type FailureKind string

// The kinds of failure, in the order they are told apart.
const (
	// FailureUnconfirmed is magex asking to confirm a major bump, which the app always
	// does: a bug in the app.
	FailureUnconfirmed FailureKind = "unconfirmed"

	// FailureMoved is a version tagged on GitHub between the sheet and the run, which
	// expect= made magex refuse.
	FailureMoved FailureKind = "moved"

	// FailureDirty is a clone with uncommitted changes.
	FailureDirty FailureKind = "dirty"

	// FailureNothingNew is a newest commit that already has a version tag.
	FailureNothingNew FailureKind = "nothingNew"

	// FailureNoBranch is a branch neither the clone nor origin has.
	FailureNoBranch FailureKind = "noBranch"

	// FailureUnsafe is magex's safety check refusing the bump.
	FailureUnsafe FailureKind = "unsafe"

	// FailureTaken is the version on GitHub at another commit.
	FailureTaken FailureKind = "taken"

	// FailureTimeout is a git step past magex's 30-second limit.
	FailureTimeout FailureKind = "timeout"

	// FailureMidPull is magex stopped while it pulled, which can leave a rebase, or while
	// it fast-forwarded the branch (ff-only), which can't.
	FailureMidPull FailureKind = "midPull"

	// FailureSigning is git unable to sign the tag, with gpg or ssh.
	FailureSigning FailureKind = "signing"

	// FailureAuth is origin refusing the git credentials.
	FailureAuth FailureKind = "auth"

	// FailureRefused is GitHub refusing the tag: a tag ruleset, or a protected tag.
	FailureRefused FailureKind = "refused"

	// FailureHook is the clone's pre-push hook stopping the tag.
	FailureHook FailureKind = "hook"

	// FailureOther is anything else, told in magex's own words.
	FailureOther FailureKind = "other"
)

// Failure is a failed release's heading: what happened, in words.
type Failure struct {
	// Kind is what happened, and Heading says it in a sentence.
	Kind    FailureKind `json:"kind"`
	Heading string      `json:"heading"`

	// Quote is the line of the output the heading quotes, or "".
	Quote string `json:"quote"`

	// Bug reports a failure only a bug in the app can cause, to report as one.
	Bug bool `json:"bug"`
}

// FailureInput is what ExplainFailure reads: what magex said, how the run ended, and the
// release the sheet started.
type FailureInput struct {
	// Output is what the output reader read, the whole of it.
	Output Output

	// End is how the run ended.
	End scriptrun.Ending

	// Version is the version the sheet showed, such as v1.4.3, and Branch the branch it
	// released from, such as master.
	Version string
	Branch  string
}

// ExplainFailure names what stopped a release, from magex's ❌ Error: line and the other
// signs in its output, with the headings of the plan's §5.11.7. The version and branch a
// heading names are the input's, or else what magex said; a tag GitHub refused is the
// one magex made. Anything it can't name reads "magex stopped:" and magex's last error
// line.
func ExplainFailure(in FailureInput) Failure {
	said := lastErrorMessage(in.Output)

	for _, rule := range failureRules() {
		if rule.matches(in, said) {
			heading, quote := rule.heading(in, said)

			return Failure{Kind: rule.kind, Heading: heading, Quote: quote, Bug: rule.kind == FailureUnconfirmed}
		}
	}

	quote := in.Output.LastError()
	if quote == "" && len(in.Output.LoggedErrors) > 0 {
		quote = in.Output.LoggedErrors[len(in.Output.LoggedErrors)-1]
	}

	return Failure{Kind: FailureOther, Heading: textutil.WithReason("magex stopped", quote), Quote: quote}
}

// failureRule tells one kind of failure: whether the input is it, and its heading and
// quote.
type failureRule struct {
	kind    FailureKind
	matches func(in FailureInput, said string) bool
	heading func(in FailureInput, said string) (heading, quote string)
}

// failureRules returns the rules in the order they are tried: magex's own errors first,
// then how the run was cut short, then what git and origin said.
func failureRules() []failureRule {
	return []failureRule{
		{FailureUnconfirmed, errorSays("major version bump requires explicit confirmation"), unconfirmedHeading},
		{FailureMoved, versionMoved, movedHeading},
		{
			FailureDirty, errorSays("working directory has uncommitted changes"),
			fixedHeading("The clone has uncommitted changes. Nothing was tagged."),
		},
		{
			FailureNothingNew, errorSays("current commit already has version tags"),
			fixedHeading("Nothing new: the newest commit already has a version."),
		},
		{FailureNoBranch, errorSays("branch does not exist locally or remotely"), noBranchHeading},
		{FailureUnsafe, errorSays("version bump blocked due to safety check"), unsafeHeading},
		{FailureTaken, errorSays("tag already exists on remote"), fixedHeading(takenHeading)},
		{FailureTimeout, timedOut, fixedHeading("A git step took longer than magex's 30 seconds.")},
		{FailureMidPull, stoppedMidPull, midPullHeading},
		{
			FailureSigning, func(in FailureInput, _ string) bool { return in.Output.Signing != "" },
			fixedHeading("Git couldn't sign the tag here. Release in your terminal, where signing can ask you."),
		},
		{
			FailureAuth, func(in FailureInput, _ string) bool { return in.Output.Auth != "" },
			fixedHeading("origin refused your git credentials."),
		},
		{FailureRefused, func(in FailureInput, _ string) bool { return in.Output.Refused != "" }, refusedHeading},
		{FailureHook, hookStopped, hookHeading},
	}
}

// lastErrorMessage returns magex's last ❌ Error: message, with the lines it continues
// on, or the JSON result's error when there was none.
func lastErrorMessage(out Output) string {
	if len(out.Errors) > 0 {
		return out.Errors[len(out.Errors)-1]
	}

	if out.Result != nil {
		return out.Result.Error
	}

	return ""
}

// errorSays matches magex's last error when it holds text.
func errorSays(text string) func(FailureInput, string) bool {
	return func(_ FailureInput, said string) bool {
		return strings.Contains(said, text)
	}
}

// fixedHeading is a heading that quotes nothing.
func fixedHeading(heading string) func(FailureInput, string) (string, string) {
	return func(FailureInput, string) (string, string) {
		return heading, ""
	}
}

// takenHeading is the heading of a version GitHub has at another commit than this
// release's.
const takenHeading = "That version is already on GitHub at another commit. Someone released it first."

func unconfirmedHeading(FailureInput, string) (string, string) {
	return "magex wanted a confirmation " + brand.Name + " didn't send.", ""
}

// expectMismatch is magex's error when the version it worked out isn't the one expect=
// pinned, because another was tagged meanwhile (1.30.0).
var expectMismatch = regexp.MustCompile(`expected (v\S+), but the next \w+ is \S+: another version was tagged meanwhile`)

func versionMoved(_ FailureInput, said string) bool {
	return expectMismatch.MatchString(said)
}

func movedHeading(in FailureInput, said string) (string, string) {
	version := in.Version
	if version == "" {
		version = expectMismatch.FindStringSubmatch(said)[1]
	}

	return version + " appeared on GitHub meanwhile, so magex tagged nothing.", ""
}

// missingBranch names the branch in magex's error.
var missingBranch = regexp.MustCompile(`branch does not exist locally or remotely: '(.*)'`)

func noBranchHeading(in FailureInput, said string) (string, string) {
	branch := in.Branch
	if m := missingBranch.FindStringSubmatch(said); branch == "" && m != nil {
		branch = m[1]
	}

	return branch + " doesn't exist in this clone or on origin.", ""
}

// unsafeHeading quotes the warning magex printed before refusing the bump: the version
// jump it found unsafe, which comes just before its advice to add force.
func unsafeHeading(in FailureInput, _ string) (string, string) {
	warnings := in.Output.Warnings

	quote := ""
	if i := slices.Index(warnings, "To proceed anyway, add 'force' parameter"); i > 0 {
		quote = warnings[i-1]
	}

	return textutil.WithReason("magex refused this bump as unsafe", quote), quote
}

// gitTimedOut is magex's error for a git command past its limit.
var gitTimedOut = regexp.MustCompile(`exceeded timeout of|context deadline exceeded`)

func timedOut(_ FailureInput, said string) bool {
	return gitTimedOut.MatchString(said)
}

// gitCanceled is magex's error for a git command a signal stopped. Its "after 30s" is
// wrong, and isn't a timeout.
var gitCanceled = regexp.MustCompile(`signal: \w+|context canceled|was canceled`)

// stoppedMidPull matches a run cut short while magex pulled: by a signal it reported, or
// the run's own end.
func stoppedMidPull(in FailureInput, said string) bool {
	return in.Output.Working == StepUpdated &&
		(in.Output.Canceled || gitCanceled.MatchString(said) || endedOnSignal(in.End))
}

// midPullHeading says where a run cut short while it pulled stopped. git pull --rebase
// may leave a rebase going; a fast-forward (ff-only) never starts one, but may leave the
// branch's files partly updated.
func midPullHeading(in FailureInput, _ string) (string, string) {
	if !in.Output.FastForward {
		return "Stopped while magex was pulling. A rebase may be in progress.", ""
	}

	branch := cmp.Or(in.Branch, in.Output.Branch, "the branch")

	return "Stopped while magex was fast-forwarding " + branch + ", which may be partly updated. Nothing was tagged.", ""
}

// endedOnSignal reports a run stopped, timed out, or stopped as the app quit, one whose
// end wasn't seen, and one whose status says a signal ended it.
func endedOnSignal(end scriptrun.Ending) bool {
	if end.Stopped || end.TimedOut || end.Quit || end.Interrupted != "" {
		return true
	}

	return end.ExitCode != nil && *end.ExitCode > 128
}

// refusedHeading quotes the rule origin said the tag broke. The tag is the one magex
// made, which is the version shown unless it moved.
func refusedHeading(in FailureInput, _ string) (string, string) {
	tag := cmp.Or(in.Output.Tag, in.Version)

	quote := in.Output.Refused

	return textutil.WithReason("GitHub refused the tag "+tag, quote), quote
}

// hookStopped matches git's line that the tag didn't go, with nothing refused: a pre-push
// hook that failed.
func hookStopped(in FailureInput, _ string) bool {
	return in.Output.PushRefsFailed && !in.Output.PushRejected
}

func hookHeading(in FailureInput, _ string) (string, string) {
	quote := in.Output.HookLine

	return textutil.WithReason("Your pre-push hook stopped the push", quote), quote
}
