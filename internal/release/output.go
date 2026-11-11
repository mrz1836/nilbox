package release

import (
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Step is a step of a release, as the sheet lists them. The names avoid git's own verbs,
// which the repository guards ban as string literals.
type Step string

// The steps of a release, in order.
const (
	// StepOnBranch is magex on the branch it releases from: switched to it, or already
	// on it.
	StepOnBranch Step = "onBranch"

	// StepUpdated is the branch brought up to date with origin.
	StepUpdated Step = "updated"

	// StepTagCreated is the version tag made on this Mac.
	StepTagCreated Step = "tagCreated"

	// StepTagPushed is the tag sent to origin, as magex tells it.
	StepTagPushed Step = "tagPushed"

	// StepVerified is the tag found on origin by the app itself, after magex ends. The
	// output reader never sets it.
	StepVerified Step = "verified"
)

// stepOrder returns every step, in order.
func stepOrder() []Step {
	return []Step{StepOnBranch, StepUpdated, StepTagCreated, StepTagPushed, StepVerified}
}

// Reached reports whether s is step or a later one.
func (s Step) Reached(step Step) bool {
	return s.rank() >= step.rank()
}

// rank orders the steps: 0 for none, then 1 for StepOnBranch onward.
func (s Step) rank() int {
	return slices.Index(stepOrder(), s) + 1
}

const (
	// maxOutputNotes caps the warning lines and logged errors an Output keeps: the first
	// ones.
	maxOutputNotes = 200

	// maxOutputErrors caps the error lines an Output keeps: the last ones, since a run
	// ends with the one that stopped it.
	maxOutputErrors = 20
)

// BumpResult is the outcome magex 1.30.1 and later print with json=true: one JSON object on
// a line of its own, the last line of stdout, on success and on failure. A step the run
// didn't reach leaves its fields empty. The field names are mage-x's (bumpResult in
// pkg/mage/version_bump_json.go), which it keeps stable for scripts.
type BumpResult struct {
	// From and To are the version bumped from and to, as vX.Y.Z; Bump is patch, minor,
	// or major; Expect is the version expect= pinned, or "".
	From   string `json:"from"`
	To     string `json:"to"`
	Bump   string `json:"bump"`
	Expect string `json:"expect"`

	// DryRun reports a dry run.
	DryRun bool `json:"dryRun"`

	// Tag is the tag made, or "", and Commit the full ID of the commit it points at.
	Tag    string `json:"tag"`
	Commit string `json:"commit"`

	// Pushed is true only when origin has the tag at that commit.
	Pushed bool `json:"pushed"`

	// StartBranch is the branch the clone was on, and Branch the one it ended on; ""
	// is a detached HEAD.
	StartBranch string `json:"startBranch"`
	Branch      string `json:"branch"`

	// Warnings are the warnings magex printed, as plain text, never nil.
	Warnings []string `json:"warnings"`

	// Error is the error the run failed with, or "".
	Error string `json:"error"`
}

// Output is what a release's output said, as an OutputReader read it: a value that
// shares nothing with the reader, safe to keep while it goes on reading.
type Output struct {
	// Step is the last step magex reached, or "" before the first.
	Step Step `json:"step"`

	// Working is the step magex started and hasn't finished, or "": StepOnBranch while it
	// switches branches, StepUpdated while it pulls, and StepTagPushed while it sends the
	// tag. It stays set when magex stops partway, so it names where it stopped.
	Working Step `json:"working"`

	// Branch is the branch magex released from: the one it switched to, or found it was
	// already on.
	Branch string `json:"branch"`

	// Bump is the bump magex made: patch, minor, or major.
	Bump string `json:"bump"`

	// From and Version are the summary's From: and To: lines, the version magex worked
	// out from and the one it makes.
	From    string `json:"from"`
	Version string `json:"version"`

	// Tag is the tag magex made on this Mac, or "", and TagPushed reports that magex said
	// it reached origin.
	Tag       string `json:"tag"`
	TagPushed bool   `json:"tagPushed"`

	// PushSkipped reports magex's warning that origin couldn't be reached, so it skipped
	// sending the tag (magex 1.29.5 then exits 0), and PushSkipWarning is that warning.
	PushSkipped     bool   `json:"pushSkipped"`
	PushSkipWarning string `json:"pushSkipWarning"`

	// PushFailed reports magex's line saying the tag didn't reach origin.
	PushFailed bool `json:"pushFailed"`

	// BackOn is the branch magex said it switched back to, or "", and SwitchBackFailed
	// reports that it said it couldn't.
	BackOn           string `json:"backOn"`
	SwitchBackFailed bool   `json:"switchBackFailed"`

	// DryRun reports magex's dry-run mode, which the app never asks for.
	DryRun bool `json:"dryRun"`

	// Canceled reports magex's line saying a signal stopped it.
	Canceled bool `json:"canceled"`

	// Warnings are magex's WARN lines, as plain text: the first maxOutputNotes.
	Warnings []string `json:"warnings"`

	// Errors are the messages of magex's ❌ Error: lines, the last maxOutputErrors, each
	// with the lines magex continues it on, joined by newlines.
	Errors []string `json:"errors"`

	// LoggedErrors are magex's ERROR and FATAL lines, as plain text: the first
	// maxOutputNotes.
	LoggedErrors []string `json:"loggedErrors"`

	// Signing is git's line saying it couldn't sign the tag, or "".
	Signing string `json:"signing"`

	// Auth is the line saying origin refused the git credentials, or "".
	Auth string `json:"auth"`

	// Refused is why origin refused the tag: the rule it broke, or the reason in git's
	// [remote rejected] line; "" when it refused nothing.
	Refused string `json:"refused"`

	// PushRejected reports one of git's ! [rejected] or ! [remote rejected] lines.
	PushRejected bool `json:"pushRejected"`

	// PushRefsFailed reports git's line saying the tag didn't go, and HookLine is the
	// last line that wasn't magex's or git's while magex sent it, before that line: a
	// pre-push hook's own, when one stopped it.
	PushRefsFailed bool   `json:"pushRefsFailed"`
	HookLine       string `json:"hookLine"`

	// Unrecognized are the lines the reader didn't recognize, the first
	// scriptrun.MaxUnrecognized of them, and UnrecognizedCount counts them all.
	Unrecognized      []string `json:"unrecognized"`
	UnrecognizedCount int      `json:"unrecognizedCount"`

	// Result is the JSON result of a run with json=true, or nil.
	Result *BumpResult `json:"result"`
}

// LastError returns the first line of the last ❌ Error: line, or of the JSON result's
// error when there was none, or "".
func (o Output) LastError() string {
	last := ""
	if len(o.Errors) > 0 {
		last = o.Errors[len(o.Errors)-1]
	} else if o.Result != nil {
		last = o.Result.Error
	}

	first, _, _ := strings.Cut(last, "\n")

	return strings.TrimSpace(first)
}

// clone returns a copy of o that shares nothing with it, with no nil lists.
func (o Output) clone() Output {
	o.Warnings = cloneOutputLines(o.Warnings)
	o.Errors = cloneOutputLines(o.Errors)
	o.LoggedErrors = cloneOutputLines(o.LoggedErrors)
	o.Unrecognized = cloneOutputLines(o.Unrecognized)

	if o.Result != nil {
		result := *o.Result
		result.Warnings = cloneOutputLines(result.Warnings)
		o.Result = &result
	}

	return o
}

// cloneOutputLines copies lines, never returning nil.
func cloneOutputLines(lines []string) []string {
	if lines == nil {
		return []string{}
	}

	return slices.Clone(lines)
}

// OutputReader reads magex version:bump's output as it arrives: an io.Writer for a run's
// Spec.Output, fed the log's bytes in order and split anywhere. It reads whole lines,
// ended by \r\n or \n, a bare \r overwriting the line as a terminal does, and color
// escapes removed. Snapshot returns what it has read, and is safe to call while it reads.
//
// A line it doesn't recognize leaves the step as it was and is kept for the result. It
// knows magex 1.29.5's lines, recorded in testdata/magex-1.29.5, and the JSON result that
// json=true adds from 1.30.1.
type OutputReader struct {
	mu sync.Mutex

	// lines cuts the output into the lines read reads.
	lines textutil.LineSplitter

	out Output

	// targetSeen reports the switch to the branch released from, so a later switch is
	// the one back; inCommits reports the commit list after "Recent commits:"; continues
	// reports that the last line was a ❌ Error: line or its continuation; and refusal
	// reports origin's message that a rule refused the tag, whose lines follow it.
	targetSeen bool
	inCommits  bool
	continues  bool
	refusal    bool
}

// NewOutputReader returns a reader that has read nothing.
func NewOutputReader() *OutputReader {
	return &OutputReader{}
}

// Write reads p, the next part of the output. It never fails.
func (r *OutputReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines.Write(p, r.read)

	return len(p), nil
}

// Flush reads the last line when the output ended without a newline. Call it once the
// output has ended.
func (r *OutputReader) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines.Flush(r.read)
}

// Snapshot returns what the reader has read so far. A line still missing its newline is
// read by Flush, not here.
func (r *OutputReader) Snapshot() Output {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.out.clone()
}

// plainOutputText returns a magex message without the indentation, emoji, or bullet it
// starts with, as mage-x's own plainWarning does for json=true's warnings.
func plainOutputText(msg string) string {
	return strings.TrimSpace(strings.TrimLeftFunc(msg, func(c rune) bool {
		return unicode.IsSpace(c) || unicode.In(c, unicode.So, unicode.Mn, unicode.Cf) || c == '•'
	}))
}

var (
	// mxLogLine is one of magex's own log lines: "21:59:50 [INFO] Using bump type: patch".
	mxLogLine = regexp.MustCompile(`^\d{2}:\d{2}:\d{2} \[([A-Z]+)\] ?(.*)$`)

	// mxError is magex's last word on a failure, on stderr.
	mxError = regexp.MustCompile(`^❌ Error: (.*)$`)

	// mxErrorContinued is a line magex continues an error on: how to finish or undo a tag
	// that didn't reach origin (1.30.0), and how to release what ff-only refused (1.30.2).
	mxErrorContinued = regexp.MustCompile(`^(?:tag \S+ was left in the local repository: |to release (?:it|them), )`)

	// mxCommit is a line of the commit list magex prints after "Recent commits:".
	mxCommit = regexp.MustCompile(`^[0-9a-f]{7,64} `)

	// mxHeader is a line of magex's "Bumping Version" header.
	mxHeader = regexp.MustCompile(`^(?:={10,}|=== .+ ===)$`)
)

// read reads one line.
func (r *OutputReader) read(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}

	continues := r.continues
	r.continues = false

	if r.readKnown(line, continues) {
		return
	}

	// Neither magex's nor git's: while the tag is sent, a pre-push hook's own.
	if r.out.Working == StepTagPushed && !r.out.PushRefsFailed && !r.out.PushFailed {
		r.out.HookLine = strings.TrimSpace(line)
	}

	r.unrecognized(line)
}

// readKnown reads a line the reader knows, reporting whether it is one: the JSON result,
// magex's error and log lines, its ✅ lines, its commit list and header, and git's lines.
func (r *OutputReader) readKnown(line string, continues bool) bool {
	return r.readResult(line) ||
		r.readError(line, continues) ||
		r.readLogLine(line) ||
		r.readSuccessLine(line) ||
		(r.inCommits && mxCommit.MatchString(line)) ||
		mxHeader.MatchString(line) ||
		r.readGit(line)
}

// unrecognized keeps a line the reader didn't recognize.
func (r *OutputReader) unrecognized(line string) {
	r.out.UnrecognizedCount++
	if len(r.out.Unrecognized) < scriptrun.MaxUnrecognized {
		r.out.Unrecognized = append(r.out.Unrecognized, line)
	}
}

// reach records that magex reached step, which ends the step under way when it is that
// one or an earlier one.
func (r *OutputReader) reach(step Step) {
	if step.rank() > r.out.Step.rank() {
		r.out.Step = step
	}

	if r.out.Working != "" && step.rank() >= r.out.Working.rank() {
		r.out.Working = ""
	}
}

// mxResultKeys are fields a JSON line must have to be read as the result. They leave out
// the commit's, whose name the repository guards ban as a string literal, as a git verb.
func mxResultKeys() []string {
	return []string{"from", "to", "tag", "pushed", "branch", "warnings", "error"}
}

// readResult reads the JSON result of a run with json=true, reporting whether line is it.
func (r *OutputReader) readResult(line string) bool {
	if !strings.HasPrefix(line, "{") {
		return false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return false
	}

	for _, key := range mxResultKeys() {
		if _, ok := fields[key]; !ok {
			return false
		}
	}

	var result BumpResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		return false
	}

	result.Warnings = cloneOutputLines(result.Warnings)
	r.out.Result = &result

	return true
}

// readError reads a ❌ Error: line, or a line continuing the one before it.
func (r *OutputReader) readError(line string, continues bool) bool {
	if m := mxError.FindStringSubmatch(line); m != nil {
		r.out.Errors = append(r.out.Errors, strings.TrimSpace(m[1]))
		if over := len(r.out.Errors) - maxOutputErrors; over > 0 {
			r.out.Errors = slices.Delete(r.out.Errors, 0, over)
		}

		r.continues = true

		return true
	}

	if continues && len(r.out.Errors) > 0 && mxErrorContinued.MatchString(line) {
		r.out.Errors[len(r.out.Errors)-1] += "\n" + strings.TrimSpace(line)
		r.continues = true

		return true
	}

	return false
}

// readLogLine reads one of magex's own log lines, reporting whether line is one.
func (r *OutputReader) readLogLine(line string) bool {
	m := mxLogLine.FindStringSubmatch(line)
	if m == nil {
		return false
	}

	r.inCommits = false

	if !r.readLevel(m[1], plainOutputText(m[2])) {
		r.unrecognized(line)
	}

	return true
}

// readLevel reads a log line's text at its level, reporting whether it knows it. Every
// WARN line is a warning, and every DEBUG line is noise.
func (r *OutputReader) readLevel(level, text string) bool {
	switch level {
	case "DEBUG":
		return true
	case "WARN":
		r.readWarning(text)

		return true
	case "INFO":
		return r.readInfo(text)
	case "ERROR":
		r.logError(text)

		return r.readLoggedError(text)
	case "FATAL":
		r.logError(text)
	}

	return false
}

// logError keeps an ERROR or FATAL line's text.
func (r *OutputReader) logError(text string) {
	if text != "" && len(r.out.LoggedErrors) < maxOutputNotes {
		r.out.LoggedErrors = append(r.out.LoggedErrors, text)
	}
}

// mxPushSkipped is magex's warning that it couldn't reach origin, after which it skips
// sending the tag.
var mxPushSkipped = regexp.MustCompile(`^Git remote validation failed: `)

// readWarning reads a WARN line.
func (r *OutputReader) readWarning(text string) {
	if text == "" {
		return
	}

	if len(r.out.Warnings) < maxOutputNotes {
		r.out.Warnings = append(r.out.Warnings, text)
	}

	if mxPushSkipped.MatchString(text) {
		r.out.PushSkipped = true
		if r.out.PushSkipWarning == "" {
			r.out.PushSkipWarning = text
		}
	}
}

var (
	mxBumpType    = regexp.MustCompile(`^Using bump type: (\w+)$`)
	mxSwitching   = regexp.MustCompile(`^Switching to branch '(.*)'\.\.\.$`)
	mxAlreadyOn   = regexp.MustCompile(`^Already on target branch '(.*)'$`)
	mxUpdating    = regexp.MustCompile(`^(?:Pulling latest changes\.\.\.|Updating '.+' from \S+ \(fast-forward only\)\.\.\.)$`)
	mxFrom        = regexp.MustCompile(`^From:\s+(\S+)$`)
	mxTo          = regexp.MustCompile(`^To:\s+(\S+)$`)
	mxType        = regexp.MustCompile(`^Type:\s+(\w+) bump$`)
	mxSending     = regexp.MustCompile(`^Pushing tag to remote\.\.\.$`)
	mxCommitsNext = regexp.MustCompile(`^Recent commits:$`)
	mxDryRun      = regexp.MustCompile(`^Running in DRY-RUN mode`)

	// mxInfo are the other INFO lines magex prints on a version bump.
	mxInfo = regexp.MustCompile(`^(?:` + strings.Join([]string{
		`Local branch not found, trying to checkout from remote\.\.\.`,
		`Detecting current version\.\.\.`,
		`No tags found on HEAD commit`,
		`Found tag on HEAD commit: \S+`,
		`Using highest version: \S+`,
		`No previous tags found in repository`,
		`No previous tags found, starting from \S+`,
		`This can happen after squash-merges where the tag was on a different branch`,
		`Using highest tag to prevent version regression`,
		`Recent version tags: .+`,
		`Found tag: \S+ \(on current commit\)`,
		`Tag \S+ already exists on current commit`,
		`Using next available version: \S+`,
		`Version validation skipped \(auto-incremented due to existing tags\)`,
		`Version Bump Summary:`,
		`To push manually later, run: git push origin \S+`,
		`To push the tag, run: git push origin \S+`,
		`Or add 'push' parameter to push automatically`,
		`Local tag → \S+, remote tag → \S+`,
		`To see all remote tags: git ls-remote --tags origin`,
		`To fetch all remote tags: git fetch --tags origin`,
		`You are currently on branch '.*'`,
		`To switch back manually: git checkout ?.*`,
		`To proceed with version bump on current branch, this is normal behavior`,
		`Please commit or stash your changes before bumping version`,
		`DRY-RUN: .+`,
		`DRY-RUN Summary:`,
		`Current version: +\S+`,
		`New version: +\S+`,
		`Bump type: +\w+`,
		`Commands that would be executed:`,
		`git tag -a \S+ -m .+`,
		`git push origin \S+`,
		`Note: .+`,
		`To push: git push origin \S+`,
	}, "|") + `)$`)
)

// readInfo reads an INFO line, reporting whether it knows it.
func (r *OutputReader) readInfo(text string) bool {
	if m := mxBumpType.FindStringSubmatch(text); m != nil {
		r.out.Bump = m[1]

		return true
	}

	if m := mxSwitching.FindStringSubmatch(text); m != nil {
		// The first switch is to the branch released from; a later one is back.
		if !r.targetSeen && !r.out.Step.Reached(StepOnBranch) {
			r.targetSeen = true
			r.out.Branch = m[1]
			r.out.Working = StepOnBranch
		}

		return true
	}

	if m := mxAlreadyOn.FindStringSubmatch(text); m != nil {
		r.targetSeen = true
		r.out.Branch = m[1]
		r.reach(StepOnBranch)

		return true
	}

	return r.readSummary(text) || r.readProgress(text) || mxInfo.MatchString(text)
}

// readSummary reads the version summary's lines, reporting whether text is one.
func (r *OutputReader) readSummary(text string) bool {
	if m := mxFrom.FindStringSubmatch(text); m != nil {
		r.out.From = m[1]

		return true
	}

	if m := mxTo.FindStringSubmatch(text); m != nil {
		r.out.Version = m[1]

		return true
	}

	if m := mxType.FindStringSubmatch(text); m != nil {
		r.out.Bump = m[1]

		return true
	}

	return false
}

// readProgress reads the INFO lines that start a step or set the run's mode, reporting
// whether text is one.
func (r *OutputReader) readProgress(text string) bool {
	switch {
	case mxUpdating.MatchString(text):
		r.out.Working = StepUpdated
	case mxSending.MatchString(text):
		r.out.Working = StepTagPushed
		r.out.HookLine = ""
	case mxCommitsNext.MatchString(text):
		r.inCommits = true
	case mxDryRun.MatchString(text):
		r.out.DryRun = true
	default:
		return false
	}

	return true
}

var (
	mxTagNotSent    = regexp.MustCompile(`^Failed to push tag to remote$`)
	mxSwitchBackErr = regexp.MustCompile(`^Failed to switch back to original branch '.*': `)

	// mxLoggedErrors are the other ERROR lines magex prints on a version bump.
	mxLoggedErrors = regexp.MustCompile(`^(?:Cannot proceed: Working directory has uncommitted changes|Tag \S+ already exists on remote pointing at a different commit)$`)
)

// readLoggedError reads an ERROR line, reporting whether it knows it.
func (r *OutputReader) readLoggedError(text string) bool {
	switch {
	case mxTagNotSent.MatchString(text):
		r.out.PushFailed = true
	case mxSwitchBackErr.MatchString(text):
		r.out.SwitchBackFailed = true
	default:
		return mxLoggedErrors.MatchString(text)
	}

	return true
}

var (
	mxSwitched     = regexp.MustCompile(`^Switched to branch '(.*)'$`)
	mxUpdated      = regexp.MustCompile(`^(?:Successfully pulled latest changes|Fast-forwarded '.+' by \d+ commits? to \S+|'.+' is level with \S+)$`)
	mxTagMade      = regexp.MustCompile(`^(?:Created tag: (\S+)|Tag (\S+) already exists on current commit)$`)
	mxTagSent      = regexp.MustCompile(`^(?:Tag pushed to remote|Tag \S+ already exists on remote at the same commit \(\w+\); nothing to push|Tag \S+ already present on remote at the same commit \(\w+\) — pushed by a concurrent run; continuing)$`)
	mxSwitchedBack = regexp.MustCompile(`^Switched back to original branch '(.*)'$`)
	mxSuccess      = regexp.MustCompile(`^(?:Major version bump confirmed via 'major-confirm' parameter|DRY-RUN completed - no changes made|\S+ matches expect)$`)
)

// readSuccessLine reads one of magex's ✅ lines, reporting whether line is one it knows.
func (r *OutputReader) readSuccessLine(line string) bool {
	return strings.HasPrefix(line, "✅") && r.readSuccess(plainOutputText(line))
}

// readSuccess reads a ✅ line's text, without its marks, reporting whether it knows it.
func (r *OutputReader) readSuccess(text string) bool {
	if m := mxSwitched.FindStringSubmatch(text); m != nil {
		// The switch to the branch released from; the one back has a line of its own.
		if !r.out.Step.Reached(StepOnBranch) {
			r.targetSeen = true
			if r.out.Branch == "" {
				r.out.Branch = m[1]
			}

			r.reach(StepOnBranch)
		}

		return true
	}

	if m := mxTagMade.FindStringSubmatch(text); m != nil {
		r.out.Tag = m[1] + m[2]
		r.reach(StepTagCreated)

		return true
	}

	if m := mxSwitchedBack.FindStringSubmatch(text); m != nil {
		r.out.BackOn = m[1]

		return true
	}

	switch {
	case mxUpdated.MatchString(text):
		r.reach(StepUpdated)
	case mxTagSent.MatchString(text):
		r.out.TagPushed = true
		r.reach(StepTagPushed)
	default:
		return mxSuccess.MatchString(text)
	}

	return true
}

var (
	// mxGitPassThrough are git's own lines on a release that goes well: the switch, the
	// pull and its summary, and the tag sent.
	mxGitPassThrough = regexp.MustCompile(`^(?:` + strings.Join([]string{
		`Switched to (?:a new )?branch '.*'`,
		`Already on '.*'`,
		`Your branch is (?:up to date with|behind|ahead of) '.*'.*`,
		`Your branch and '.*' have diverged,`,
		`and have \d+ and \d+ different commits each, respectively\.`,
		`\s+\(use "git [^"]+"[^)]*\)`,
		`Previous HEAD position was [0-9a-f]+ .*`,
		`HEAD is now at [0-9a-f]+ .*`,
		`Already up.to.date\.?`,
		`Current branch \S+ is up to date\.`,
		`From \S+`,
		`To \S+`,
		` [ +*=t-] .*\S\s+-> \S.*`,
		`Updating [0-9a-f]+\.\.[0-9a-f]+`,
		`Fast-forward`,
		` .+ \| +(?:\d+ ?[+-]*|Bin .+)`,
		` \d+ files? changed(?:, \d+ insertions?\(\+\))?(?:, \d+ deletions?\(-\))?`,
		` (?:create|delete) mode \d+ .+`,
		` rename .+ \(\d+%\)`,
		` mode change \d+ => \d+ .+`,
		`Successfully rebased and updated \S+\.`,
		`[0-9a-f]{40}(?:[0-9a-f]{24})?\tHEAD`,
	}, "|") + `)$`)

	// mxSigningFailed is git's line saying it couldn't sign the tag, with gpg or ssh.
	mxSigningFailed = regexp.MustCompile(`^error: (?:unable to sign the tag|gpg failed to sign the data:?)$`)

	// mxTagMessageLeft follows a tag git couldn't make.
	mxTagMessageLeft = regexp.MustCompile(`^The tag message has been left in \S+$`)

	// mxAuthRefused is a line saying origin refused the git credentials, from ssh, git,
	// or GitHub.
	mxAuthRefused = regexp.MustCompile(`(?i)permission denied \(publickey|authentication failed|could not read (?:username|password)|terminal prompts disabled|returned error: 40[13]|invalid username or password|password authentication was removed|permission to \S+ denied to`)

	// mxRemote is a line of origin's own, which git prints after "remote:".
	mxRemote = regexp.MustCompile(`^remote:(.*)$`)

	// mxRuleRefused starts origin's message that a rule refused the tag.
	mxRuleRefused = regexp.MustCompile(`(?i)GH013|rule violations|protected tag`)

	// mxRejected is git's line for a ref it, or origin, refused, with the reason.
	mxRejected = regexp.MustCompile(`^ ! \[(remote rejected|rejected)\]\s+\S+ -> \S+(?: \((.+)\))?$`)

	// mxRefsFailed is git's line saying the tag didn't go: after a rejection, or a
	// pre-push hook that failed.
	mxRefsFailed = regexp.MustCompile(`^error: failed to push some refs to `)

	// mxCanceled is magex's line when a signal stops it.
	mxCanceled = regexp.MustCompile(`Canceled by user \(Ctrl\+C\)`)
)

// readGit reads a line of git's, or origin's, or magex's on a signal, reporting whether
// it knows it. The first line that tells why signing or the credentials failed is kept.
func (r *OutputReader) readGit(line string) bool {
	switch {
	case mxGitPassThrough.MatchString(line), mxTagMessageLeft.MatchString(line):
	case mxCanceled.MatchString(line):
		r.out.Canceled = true
	case mxSigningFailed.MatchString(line):
		r.out.Signing = cmp.Or(r.out.Signing, line)
	case mxAuthRefused.MatchString(line):
		r.out.Auth = cmp.Or(r.out.Auth, strings.TrimSpace(strings.TrimPrefix(line, "remote:")))
	default:
		return r.readRefusal(line)
	}

	return true
}

// readRefusal reads git's and origin's lines when the tag didn't go: origin's own,
// git's ! lines for the refs it, or origin, refused, and its line saying the tag didn't
// go. It reports whether it knows the line.
func (r *OutputReader) readRefusal(line string) bool {
	if m := mxRemote.FindStringSubmatch(line); m != nil {
		return r.readRemote(strings.TrimSpace(m[1]))
	}

	if m := mxRejected.FindStringSubmatch(line); m != nil {
		r.out.PushRejected = true
		if m[1] == "remote rejected" {
			r.out.Refused = cmp.Or(r.out.Refused, m[2])
		}

		return true
	}

	if mxRefsFailed.MatchString(line) {
		r.out.PushRefsFailed = true

		return true
	}

	return false
}

// readRemote reads a line of origin's, after "remote:", reporting whether it knows it.
func (r *OutputReader) readRemote(text string) bool {
	switch {
	case text == "":
	case mxRuleRefused.MatchString(text):
		r.refusal = true
	case r.refusal && strings.HasPrefix(text, "- "):
		r.out.Refused = cmp.Or(r.out.Refused, strings.TrimSpace(text[2:]))
	case r.refusal:
		// The rest of the refusal, such as where to read the rules.
	default:
		return false
	}

	return true
}
