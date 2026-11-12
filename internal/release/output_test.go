package release

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The recorded version:bump runs of the real magex: P0's of magex 1.29.5, and magex
// 1.30.2's, run with the arguments Decide sends a magex that has expect=, json=true, and
// ff-only. Both were recorded in temp repositories with a local bare origin and an isolated
// HOME and git config, and hold the clone's state before and after each run.
const (
	magexRecordings     = "testdata/magex-1.29.5"
	magex1302Recordings = "testdata/magex-1.30.2"
)

// What most recorded runs release: a patch from v1.4.2 to v1.4.3 on master, many of them
// started on feature-x, and the patch after it.
const (
	recBranch  = "master"
	recFeature = "feature-x"
	recFrom    = "v1.4.2"
	recVersion = "v1.4.3"
	recNext    = "v1.4.4"
	recBump    = "patch"
)

// magexRecording is one recorded run: its arguments, its exit status, its lines in the
// order they arrived, each from stdout ("out") or stderr ("err"), and the clone's state
// before and after it, as git told the recorder.
type magexRecording struct {
	Name  string   `json:"name"`
	Argv  []string `json:"argv"`
	Exit  int      `json:"exit"`
	Lines []struct {
		Stream string `json:"stream"`
		Text   string `json:"text"`
	} `json:"lines"`
	Before cloneRecord `json:"before"`
	After  cloneRecord `json:"after"`
}

// cloneRecord is a clone's state as the recorder read it with git: its branch, or
// "(detached)"; each local tag as "<name> <object> <commit>", abbreviated; origin's tags
// as ls-remote prints them, or its error; and what git is in the middle of.
type cloneRecord struct {
	Branch     string `json:"branch"`
	LocalTags  string `json:"localTags"`
	RemoteTags string `json:"remoteTags"`
	InProgress string `json:"inProgress"`
}

// loadRecording reads magex 1.29.5's recording called name.
func loadRecording(t *testing.T, name string) magexRecording {
	t.Helper()

	return loadRecordingIn(t, magexRecordings, name)
}

// loadRecordingIn reads the recording called name from the folder dir.
func loadRecordingIn(t *testing.T, dir, name string) magexRecording {
	t.Helper()

	data, err := fs.ReadFile(os.DirFS(dir), name+".json")
	require.NoError(t, err)

	var rec magexRecording
	require.NoError(t, json.Unmarshal(data, &rec))
	require.Equal(t, name, rec.Name)
	require.NotEmpty(t, rec.Lines)

	return rec
}

// recordingNames lists magex 1.29.5's recorded version:bump runs.
func recordingNames(t *testing.T) []string {
	t.Helper()

	return recordingNamesIn(t, magexRecordings)
}

// recordingNamesIn lists the recorded version:bump runs in the folder dir.
func recordingNamesIn(t *testing.T, dir string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)

	names := make([]string, 0, len(paths))
	for _, p := range paths {
		if name := strings.TrimSuffix(filepath.Base(p), ".json"); name != "next-version" {
			names = append(names, name)
		}
	}

	return names
}

// log returns the run's output as one log holds it, stdout and stderr in the order they
// arrived, each line ended by eol.
func (m magexRecording) log(eol string) []byte {
	var b strings.Builder

	for _, l := range m.Lines {
		b.WriteString(l.Text)
		b.WriteString(eol)
	}

	return []byte(b.String())
}

// param returns the value of the run's key=value argument, or "".
func (m magexRecording) param(key string) string {
	for _, arg := range m.Argv {
		if value, ok := strings.CutPrefix(arg, key+"="); ok {
			return value
		}
	}

	return ""
}

// readOutput feeds a new reader the chunks, then flushes it.
func readOutput(chunks ...[]byte) Output {
	r := NewOutputReader()
	for _, c := range chunks {
		n, err := r.Write(c)
		if err != nil || n != len(c) {
			panic("the reader refused a write")
		}
	}

	r.Flush()

	return r.Snapshot()
}

// readLines feeds a new reader the lines, each ended by a newline.
func readLines(lines ...string) Output {
	return readOutput([]byte(strings.Join(lines, "\n") + "\n"))
}

// expected returns want as a Snapshot returns it: no nil lists, and every unrecognized
// line counted.
func expected(want Output) Output {
	want = want.clone()
	want.UnrecognizedCount = len(want.Unrecognized)

	return want
}

const (
	notOnHead     = "Current version tag is not on HEAD - there are 1 commits since v1.4.2"
	noBranchParam = "No branch parameter specified - performing version bump on current branch: 'master'"
	considerParam = "If not on a release branch, consider using: magex version:bump branch=master bump=patch"
	sendByHand    = "To push manually: git push origin v1.4.3"
	notSent       = "Failed to push tag to remote"
	dirtyError    = "Cannot proceed: Working directory has uncommitted changes"
	switchBackCut = "Failed to switch back to original branch 'feature-x': failed to checkout branch 'feature-x': " +
		"local branch not found (command 'git' was canceled after 30s: command failed [git checkout feature-x]: " +
		"context canceled) and remote checkout failed (command 'git' was canceled after 30s: command failed " +
		"[git checkout -b feature-x origin/feature-x]: context canceled)"
	cantReachOrigin = "fatal: Could not read from remote repository."
	accessRights    = "Please make sure you have the correct access rights"
	repoExists      = "and the repository exists."
	noUpstream1     = "You asked to pull from the remote 'origin', but did not specify"
	noUpstream2     = "a branch. Because this is not the default configured remote"
	noUpstream3     = "for your current branch, you must specify a branch on the command line."
)

// released is a patch release from master that magex says went well.
func released(version string, warnings ...string) Output {
	return Output{
		Step: StepTagPushed, Branch: recBranch, Bump: recBump, From: recFrom, Version: version,
		Tag: version, TagPushed: true, Notes: scriptrun.Notes{Warnings: warnings},
	}
}

// recordedOutputs is what each recording should read as.
func recordedOutputs() map[string]Output {
	majorWarnings := func(from, to string) []string {
		return []string{
			"Current version tag is not on HEAD - there are 1 commits since " + from,
			"MAJOR VERSION BUMP DETECTED:", "Current version: " + from, "New version:     " + to,
			"This will create a breaking change release!",
		}
	}

	signing := Output{
		Step: StepUpdated, Branch: recBranch, Bump: recBump, From: recFrom, Version: recVersion,
		Notes:   scriptrun.Notes{Warnings: []string{notOnHead}, Unrecognized: []string{"(no gpg output)"}},
		Signing: "error: gpg failed to sign the data:",
		Errors:  []string{"failed to create tag: command failed [git tag -a v1.4.3 -m GitHubRelease v1.4.3]: exit status 128"},
	}

	signingFromFeature := signing
	signingFromFeature.BackOn = recFeature

	sshSigning := signing
	sshSigning.Signing = "error: unable to sign the tag"
	sshSigning.Unrecognized = []string{
		"error: Couldn't load public key /work/cases/signing-ssh-missing-key/missing-key.pub: No such file or directory?",
	}

	prePush := Output{
		Step: StepTagCreated, Working: StepTagPushed, Branch: recBranch, Bump: recBump, From: recFrom,
		Version: recVersion, Tag: recVersion, PushFailed: true, PushRefsFailed: true,
		HookLine: "pre-push: blocked by policy",
		Notes: scriptrun.Notes{
			Warnings: []string{notOnHead, sendByHand}, Unrecognized: []string{"pre-push: blocked by policy"},
		},
		Errors:       []string{"failed to push tag: command failed [git push origin v1.4.3]: exit status 1"},
		LoggedErrors: []string{notSent},
	}

	prePushFromFeature := prePush
	prePushFromFeature.BackOn = recFeature

	stoppedAfterTag := func(signal string) Output {
		return Output{
			Step: StepTagCreated, Working: StepTagPushed, Branch: recBranch, Bump: recBump, From: recFrom,
			Version: recVersion, Tag: recVersion, PushFailed: true, SwitchBackFailed: true, Canceled: true,
			Notes: scriptrun.Notes{Warnings: []string{notOnHead, sendByHand}},
			Errors: []string{"failed to push tag: command 'git' was canceled after 30s: context canceled " +
				"(command failed [git push origin v1.4.3]: signal: " + signal + ")"},
			LoggedErrors: []string{notSent, switchBackCut},
		}
	}

	stoppedMidPull := func(err string) Output {
		return Output{
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump, Canceled: true,
			Errors: []string{err},
		}
	}

	dirty := Output{Bump: recBump, Errors: []string{"working directory has uncommitted changes"}, LoggedErrors: []string{dirtyError}}

	fromFeature := released(recVersion, "Current version tag is not on HEAD - there are 4 commits since v1.4.2")
	fromFeature.BackOn = recFeature

	withHook := released(recVersion, notOnHead)
	withHook.BackOn = recFeature
	withHook.Unrecognized = []string{"post-checkout: ran", "post-checkout: ran"}

	detached := released(recVersion, notOnHead)
	detached.SwitchBackFailed = true
	detached.LoggedErrors = []string{
		"Failed to switch back to original branch '': failed to checkout branch '': local branch not found " +
			"(command failed [git checkout ]: exit status 128) and remote checkout failed " +
			"(command failed [git checkout -b  origin/]: exit status 128)",
	}
	detached.Unrecognized = []string{
		"fatal: empty string is not a valid pathspec. please use . instead if you meant to match all paths",
		"fatal: 'origin/' is not a commit and a branch '' cannot be created from it",
	}

	noBranch := released(recVersion, "No branch parameter specified - performing version bump on current branch: ''",
		considerParam, notOnHead)
	noBranch.Branch = ""

	localOnly := released(recNext, "Current version tag is not on HEAD - there are 2 commits since v1.4.3")
	localOnly.From = recVersion

	moved := released(recNext, "Current version tag is not on HEAD - there are 1 commits since v1.4.3")
	moved.From = recVersion

	minor := released("v1.5.0", notOnHead)
	minor.Bump = "minor"

	prerelease := released("v1.6.0", "Current version tag is not on HEAD - there are 1 commits since v1.5.0-rc.1")
	prerelease.Bump, prerelease.From = "minor", "v1.5.0-rc.1"

	major := released("v2.0.0", append(majorWarnings(recFrom, "v2.0.0"), notOnHead)...)
	major.Bump = "major"

	majorFromV0 := released("v1.0.0", append(majorWarnings("v0.9.1", "v1.0.0"),
		"Current version tag is not on HEAD - there are 1 commits since v0.9.1")...)
	majorFromV0.Bump, majorFromV0.From = "major", "v0.9.1"

	noTags := func(bump, version string) Output {
		o := released(version)
		o.Bump, o.From = bump, "v0.0.0"

		return o
	}

	return map[string]Output{
		"patch-feature-behind":        fromFeature,
		"minor-on-master":             minor,
		"major-confirm":               major,
		"major-from-v0":               majorFromV0,
		"no-tags-patch":               noTags(recBump, "v0.0.1"),
		"no-tags-minor":               noTags("minor", "v0.1.0"),
		"no-tags-major":               noTags("major", "v1.0.0"),
		"prerelease-minor":            prerelease,
		"local-only-tag":              localOnly,
		"version-moved":               moved,
		"unpushed-commits":            released(recVersion, "Current version tag is not on HEAD - there are 4 commits since v1.4.2"),
		"sigint-during-pull-behind":   released(recVersion, "Current version tag is not on HEAD - there are 2 commits since v1.4.2"),
		"post-checkout-hook":          withHook,
		"detached-head":               detached,
		"detached-no-branch":          noBranch,
		"dirty":                       dirty,
		"untracked-only":              dirty,
		"signing-gpg-fails":           signing,
		"signing-fails-from-feature":  signingFromFeature,
		"signing-ssh-missing-key":     sshSigning,
		"pre-push-fails":              prePush,
		"pre-push-fails-from-feature": prePushFromFeature,
		"sigint-after-tag":            stoppedAfterTag("interrupt"),
		"sigterm-after-tag":           stoppedAfterTag("terminated"),
		"sigint-during-pull-diverged": stoppedMidPull("failed to pull latest changes: command 'git' was canceled after 30s: " +
			"context canceled (command failed [git pull --rebase origin]: signal: interrupt)"),
		"sigint-mid-rebase": stoppedMidPull("failed to pull latest changes: command failed [git pull --rebase origin]: " +
			"signal: interrupt"),
		"dry-run": {
			Step: StepOnBranch, Branch: recBranch, Bump: recBump, From: recFrom, Version: recVersion, DryRun: true,
			Notes: scriptrun.Notes{Warnings: []string{notOnHead}},
		},
		"major-no-confirm": {
			Bump: "major",
			Notes: scriptrun.Notes{Warnings: append(majorWarnings(recFrom, "v2.0.0"),
				"To proceed with major version bump, add 'major-confirm' parameter",
				"Example: magex version:bump bump=major major-confirm push",
				"Or use 'dry-run' to preview the change first:",
				"Example: magex version:bump bump=major dry-run")},
			Errors: []string{"major version bump requires explicit confirmation via MAJOR_BUMP_CONFIRM=true"},
		},
		"missing-branch": {
			Bump: recBump, Errors: []string{"branch does not exist locally or remotely: 'release'"},
		},
		"tag-on-head": {
			Step: StepUpdated, Branch: recBranch, Bump: recBump,
			Notes: scriptrun.Notes{Warnings: []string{
				"Current commit already has version tags: v1.4.2",
				"Please create a new commit before bumping the version again",
			}},
			Errors: []string{"current commit already has version tags: v1.4.2"},
		},
		"no-upstream": {
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump,
			Errors: []string{"failed to pull latest changes: command failed [git pull --rebase origin]: exit status 1"},
			Notes:  scriptrun.Notes{Unrecognized: []string{noUpstream1, noUpstream2, noUpstream3}},
		},
		"no-upstream-from-feature": {
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump, BackOn: recFeature,
			Errors: []string{"failed to pull latest changes on branch 'master': failed to pull latest changes: " +
				"command failed [git pull --rebase origin]: exit status 1"},
			Notes: scriptrun.Notes{Unrecognized: []string{noUpstream1, noUpstream2, noUpstream3}},
		},
		"origin-unreachable-branch": {
			Step: StepOnBranch, Working: StepUpdated, Branch: recBranch, Bump: recBump,
			Errors: []string{"failed to fetch from origin: command failed [git fetch --tags --force origin]: exit status 128"},
			Notes: scriptrun.Notes{Unrecognized: []string{
				"fatal: '/work/cases/origin-unreachable-branch/missing.git' does not appear to be a git repository",
				cantReachOrigin, accessRights, repoExists,
			}},
		},
		"origin-unreachable-nobranch": {
			Step: StepTagCreated, Bump: recBump, From: recFrom, Version: recVersion, Tag: recVersion, PushSkipped: true,
			PushSkipWarning: "Git remote validation failed: remote 'origin' exists but may not be accessible: " +
				"command failed [git ls-remote --exit-code origin HEAD]: exit status 128",
			Notes: scriptrun.Notes{
				Warnings: []string{
					noBranchParam, considerParam, notOnHead,
					"Git remote validation failed: remote 'origin' exists but may not be accessible: " +
						"command failed [git ls-remote --exit-code origin HEAD]: exit status 128",
				},
				Unrecognized: []string{
					"fatal: '/work/cases/origin-unreachable-nobranch/missing.git' does not appear to be a git repository",
					cantReachOrigin, accessRights, repoExists,
				},
			},
		},
		"other-worktree": {
			Working: StepOnBranch, Branch: recBranch, Bump: recBump,
			Errors: []string{"failed to checkout branch 'master': local branch not found (command failed " +
				"[git checkout master]: exit status 128) and remote checkout failed (command failed " +
				"[git checkout -b master origin/master]: exit status 128)"},
			Notes: scriptrun.Notes{Unrecognized: []string{
				"fatal: 'master' is already used by worktree at '/work/cases/other-worktree/wt-master'",
				"fatal: a branch named 'master' already exists",
			}},
		},
		"post-checkout-fails": {
			Working: StepOnBranch, Branch: recBranch, Bump: recBump,
			Errors: []string{"failed to checkout branch 'master': local branch not found (command failed " +
				"[git checkout master]: exit status 1) and remote checkout failed (command failed " +
				"[git checkout -b master origin/master]: exit status 128)"},
			Notes: scriptrun.Notes{Unrecognized: []string{"post-checkout: blocked by policy", "fatal: a branch named 'master' already exists"}},
		},
		"remote-tag-other-commit": {
			Step: StepTagCreated, Bump: recBump, From: recFrom, Version: recVersion, Tag: recVersion,
			Notes: scriptrun.Notes{Warnings: []string{
				noBranchParam, considerParam, notOnHead, "This usually means:",
				"The tag was created from a different branch", "Another developer created this tag",
				"Your local tags were out of sync with remote",
			}},
			Errors:       []string{"tag already exists on remote: v1.4.3"},
			LoggedErrors: []string{"Tag v1.4.3 already exists on remote pointing at a different commit"},
		},
		"ruleset-refused": {
			Step: StepTagCreated, Working: StepTagPushed, Branch: recBranch, Bump: recBump, From: "v0.0.1",
			Version: "v0.0.2", Tag: "v0.0.2", PushFailed: true, PushRejected: true, PushRefsFailed: true,
			Refused: "Cannot create ref due to creations being restricted.",
			Notes: scriptrun.Notes{Warnings: []string{
				"Found tag v0.0.1 in repository, but it's not reachable from current commit",
				"To push manually: git push origin v0.0.2",
			}},
			Errors:       []string{"failed to push tag: command failed [git push origin v0.0.2]: exit status 1"},
			LoggedErrors: []string{notSent},
		},
	}
}

func TestOutputReadsEveryRecording(t *testing.T) {
	t.Parallel()

	wants := recordedOutputs()
	names := recordingNames(t)
	require.Len(t, names, 38, "every recorded version:bump run is copied")

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want, ok := wants[name]
			require.True(t, ok, "no expected output for %s", name)

			rec := loadRecording(t, name)
			got := readOutput(rec.log("\n"))
			assert.Equal(t, expected(want), got)

			// A release magex said went well tagged and sent the version it printed.
			if rec.Exit == 0 && !got.DryRun && !got.PushSkipped {
				assert.Equal(t, StepTagPushed, got.Step)
				assert.Equal(t, got.Version, got.Tag)
				assert.Empty(t, got.Errors)
			}
		})
	}

	for name := range wants {
		assert.Contains(t, names, name, "an expected output with no recording")
	}
}

// coloredLogLine splits a magex log line into its time and level, and its message.
var coloredLogLine = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2} \[[A-Z]+\]) (.*)$`)

// colored returns line as magex prints it to a terminal: its log lines' time and level,
// its ✅ lines' text, and its header in color.
func colored(line string) string {
	if m := coloredLogLine.FindStringSubmatch(line); m != nil {
		return "\x1b[34m" + m[1] + "\x1b[0m " + m[2]
	}

	if rest, ok := strings.CutPrefix(line, "✅ "); ok {
		return "✅ \x1b[32m" + rest + "\x1b[0m"
	}

	if strings.HasPrefix(line, "===") {
		return "\x1b[1m\x1b[34m" + line + "\x1b[0m"
	}

	return line
}

func TestOutputReadsTheSameHoweverItArrives(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{magexRecordings, magex1302Recordings} {
		for _, name := range recordingNamesIn(t, dir) {
			t.Run(filepath.Base(dir)+"/"+name, func(t *testing.T) {
				t.Parallel()
				readsTheSameHoweverItArrives(t, loadRecordingIn(t, dir, name))
			})
		}
	}
}

// readsTheSameHoweverItArrives checks that rec's output reads the same fed a byte at a
// time, in uneven parts, with \r\n endings, and in color.
func readsTheSameHoweverItArrives(t *testing.T, rec magexRecording) {
	t.Helper()

	whole := rec.log("\n")
	want := readOutput(whole)

	bytewise := make([][]byte, 0, len(whole))
	for i := range whole {
		bytewise = append(bytewise, whole[i:i+1])
	}

	assert.Equal(t, want, readOutput(bytewise...), "fed a byte at a time")

	var uneven [][]byte

	for rest, n := whole, 1; len(rest) > 0; n = n%13 + 1 {
		n = min(n, len(rest))
		uneven, rest = append(uneven, rest[:n]), rest[n:]
	}

	assert.Equal(t, want, readOutput(uneven...), "fed in uneven parts")
	assert.Equal(t, want, readOutput(rec.log("\r\n")), "with \\r\\n endings")

	var paint strings.Builder
	for _, l := range rec.Lines {
		paint.WriteString(colored(l.Text) + "\n")
	}

	assert.Equal(t, want, readOutput([]byte(paint.String())), "in color")
}

func TestOutputKeepsLinesItDoesNotKnow(t *testing.T) {
	t.Parallel()

	got := readLines(
		"10:00:00 [INFO] Using bump type: patch",
		"10:00:00 [INFO] Switching to branch 'master'...",
		"Switched to branch 'master'",
		"✅ Switched to branch 'master'",
		"10:00:00 [INFO] Pulling latest changes...",
		"something new from a hook",
		"10:00:00 [INFO] A line a newer magex added",
		"✅ A success a newer magex added",
		"10:00:00 [ERROR] An error a newer magex added",
		"10:00:00 [FATAL] A level a newer magex added",
		"❌ Not the error line",
		"{\"not\": \"the result\"}",
		"Already up to date.",
		"✅ Successfully pulled latest changes",
	)

	assert.Equal(t, StepUpdated, got.Step, "unknown lines leave the steps as they go")
	assert.Empty(t, got.Working)
	assert.Equal(t, []string{
		"something new from a hook",
		"10:00:00 [INFO] A line a newer magex added",
		"✅ A success a newer magex added",
		"10:00:00 [ERROR] An error a newer magex added",
		"10:00:00 [FATAL] A level a newer magex added",
		"❌ Not the error line",
		"{\"not\": \"the result\"}",
	}, got.Unrecognized)
	assert.Equal(t, 7, got.UnrecognizedCount)
	assert.Equal(t, []string{"An error a newer magex added", "A level a newer magex added"}, got.LoggedErrors)
	assert.Empty(t, got.Errors)
	assert.Nil(t, got.Result)

	// An unknown line in the middle of a step leaves it under way.
	midway := readLines("10:00:00 [INFO] Pulling latest changes...", "a line from git nobody expected")
	assert.Equal(t, StepUpdated, midway.Working)
	assert.Equal(t, []string{"a line from git nobody expected"}, midway.Unrecognized)
}

func TestOutputCountsEveryUnknownLineButKeepsTheFirst200(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 250)
	for i := range 250 {
		lines = append(lines, "unknown line "+strings.Repeat("x", i%7)+string(rune('a'+i%26)))
	}

	got := readLines(lines...)
	assert.Equal(t, 250, got.UnrecognizedCount)
	assert.Len(t, got.Unrecognized, scriptrun.MaxUnrecognized)
	assert.Equal(t, lines[:scriptrun.MaxUnrecognized], got.Unrecognized)
}

func TestOutputCleansLines(t *testing.T) {
	t.Parallel()

	got := readOutput([]byte(
		"\x1b[33m10:00:00 [WARN]\x1b[0m \x1b]8;;https://example.com\x07a linked\x1b]8;;\x07 warning\r\n" +
			"progress 10%\rprogress 100%\rdone \x07with a bell\n" +
			"bad \xff byte\n" +
			"\t  \r\n",
	))

	assert.Equal(t, []string{"a linked warning"}, got.Warnings)
	assert.Equal(t, []string{"done with a bell", "bad � byte"}, got.Unrecognized, "a bare \\r overwrites the line")
}

func TestOutputCutsALongLine(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("y", textutil.MaxLine+5000)
	got := readOutput([]byte(long[:30000]), []byte(long[30000:]+"\n"), []byte("✅ Successfully pulled latest changes\n"))

	require.Len(t, got.Unrecognized, 1)
	assert.Len(t, got.Unrecognized[0], textutil.MaxLine)
	assert.Equal(t, StepUpdated, got.Step, "the line after it is read whole")
}

func TestOutputReadsAnUnfinishedLastLineOnFlush(t *testing.T) {
	t.Parallel()

	r := NewOutputReader()
	_, err := r.Write([]byte("10:00:00 [INFO]   To:      v1.4.3\n❌ Error: cut sh"))
	require.NoError(t, err)

	before := r.Snapshot()
	assert.Equal(t, recVersion, before.Version)
	assert.Empty(t, before.Errors, "a line is read once it ends")

	r.Flush()
	r.Flush()
	assert.Equal(t, []string{"cut sh"}, r.Snapshot().Errors)
}

func TestOutputReaderZeroValueIsReady(t *testing.T) {
	t.Parallel()

	// Each method works as the first call on a zero value.
	for name, call := range map[string]func(r *OutputReader){
		"Write":    func(r *OutputReader) { _, _ = r.Write([]byte("partial")) },
		"Flush":    func(r *OutputReader) { r.Flush() },
		"Snapshot": func(r *OutputReader) { _ = r.Snapshot() },
		"Progress": func(r *OutputReader) { _ = r.Progress(Intent{}) },
	} {
		assert.NotPanics(t, func() { call(new(OutputReader)) }, name)
	}

	whole := loadRecording(t, "patch-feature-behind").log("\n")

	var r OutputReader

	_, err := r.Write(whole)
	require.NoError(t, err)
	r.Flush()

	assert.Equal(t, readOutput(whole), r.Snapshot(), "a zero value reads as NewOutputReader's does")
}

func TestOutputSnapshotsShareNothing(t *testing.T) {
	t.Parallel()

	r := NewOutputReader()
	_, err := r.Write([]byte("10:00:00 [WARN] careful\n❌ Error: it broke\nunknown\n" + jsonLine(t, BumpResult{
		Tag: recVersion, Warnings: []string{"careful"},
	}) + "\n"))
	require.NoError(t, err)

	first := r.Snapshot()
	first.Warnings[0] = "changed"
	first.Errors[0] = "changed"
	first.Unrecognized[0] = "changed"
	first.Result.Warnings[0] = "changed"
	first.Result.Tag = "changed"

	second := r.Snapshot()
	assert.Equal(t, []string{"careful"}, second.Warnings)
	assert.Equal(t, []string{"it broke"}, second.Errors)
	assert.Equal(t, []string{"unknown"}, second.Unrecognized)
	assert.Equal(t, []string{"careful"}, second.Result.Warnings)
	assert.Equal(t, recVersion, second.Result.Tag)

	empty := NewOutputReader().Snapshot()
	assert.NotNil(t, empty.Warnings, "no nil lists")
	assert.NotNil(t, empty.Errors)
	assert.NotNil(t, empty.LoggedErrors)
	assert.NotNil(t, empty.Unrecognized)
}

func TestOutputSnapshotsWhileItReads(t *testing.T) {
	t.Parallel()

	whole := loadRecording(t, "patch-feature-behind").log("\n")
	r := NewOutputReader()

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range whole {
			_, _ = r.Write(whole[i : i+1])
		}
	})

	for range 200 {
		got := r.Snapshot()
		assert.True(t, got.Step == "" || slices.Contains(stepOrder(), got.Step))
	}

	wg.Wait()
	assert.Equal(t, readOutput(whole), r.Snapshot())
}

func TestStepsAreOrdered(t *testing.T) {
	t.Parallel()

	assert.True(t, StepTagPushed.Reached(StepTagCreated))
	assert.True(t, StepTagCreated.Reached(StepTagCreated))
	assert.False(t, StepUpdated.Reached(StepTagCreated))
	assert.False(t, Step("").Reached(StepOnBranch))
	assert.True(t, StepVerified.Reached(StepTagPushed))
	assert.True(t, Step("").Reached(""))
}

func TestLastErrorIsTheFirstLineOfTheLastError(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Output{}.LastError())
	assert.Equal(t, "second", Output{Errors: []string{"first", " second \nhow to fix it"}}.LastError())
	assert.Equal(t, "from the result", Output{Result: &BumpResult{Error: "from the result\nmore"}}.LastError())
	assert.Equal(t, "from the line", Output{
		Errors: []string{"from the line"}, Result: &BumpResult{Error: "from the result"},
	}.LastError())
}

func TestOutputKeepsTheLastErrors(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, maxOutputErrors+5)
	for i := range maxOutputErrors + 5 {
		lines = append(lines, "❌ Error: number "+strings.Repeat("i", i+1))
	}

	got := readLines(lines...)
	require.Len(t, got.Errors, maxOutputErrors)
	assert.Equal(t, "number "+strings.Repeat("i", maxOutputErrors+5), got.LastError())
}

// jsonLine returns result as magex 1.30.1 prints it: one line, with no HTML escaping.
func jsonLine(t *testing.T, result BumpResult) string {
	t.Helper()

	if result.Warnings == nil {
		result.Warnings = []string{}
	}

	var b strings.Builder

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	require.NoError(t, enc.Encode(result))

	return strings.TrimSuffix(b.String(), "\n")
}

// bumpHeader is the start of every version:bump run's output.
func bumpHeader() []string {
	return []string{
		"",
		"============================================================",
		"=== Bumping Version ===",
		"============================================================",
		"",
		"10:00:00 [INFO] Using bump type: patch",
	}
}

// fromFeatureToTag is a patch release from feature-x, up to its tag and the push's start,
// with expect=v1.4.3, as magex 1.30.1 prints it (pkg/mage/version.go).
func fromFeatureToTag() []string {
	return append(bumpHeader(),
		"10:00:00 [INFO] Switching to branch 'master'...",
		"Switched to branch 'master'",
		"Your branch is up to date with 'origin/master'.",
		"✅ Switched to branch 'master'",
		"10:00:00 [INFO] Pulling latest changes...",
		"Already up to date.",
		"✅ Successfully pulled latest changes",
		"10:00:00 [INFO] Detecting current version...",
		"10:00:00 [INFO] No tags found on HEAD commit",
		"10:00:00 [WARN] "+notOnHead,
		"10:00:00 [INFO] Recent commits:",
		"725b4e0 fix: retries on 502 (#45)",
		"10:00:00 [INFO] 📋 Version Bump Summary:",
		"10:00:00 [INFO]   From:    v1.4.2",
		"10:00:00 [INFO]   To:      v1.4.3",
		"10:00:00 [INFO]   Type:    patch bump",
		"✅ v1.4.3 matches expect",
		"✅ ✅ Created tag: v1.4.3",
		"725b4e00476a9f1c2fd877b7157fb3eac747e300\tHEAD",
		"10:00:00 [INFO] Pushing tag to remote...",
	)
}

// backOnFeature is magex switching back to feature-x.
func backOnFeature() []string {
	return []string{
		"10:00:00 [INFO] Switching to branch 'feature-x'...",
		"Switched to branch 'feature-x'",
		"✅ Switched to branch 'feature-x'",
		"✅ ✅ Switched back to original branch 'feature-x'",
	}
}

func TestOutputReadsTheJSONResult(t *testing.T) {
	t.Parallel()

	// Runs built from mage-x's code (bumpReport in pkg/mage/version_bump_json.go), whose
	// JSON line is the last of stdout and comes before the ❌ Error: line magex prints to
	// stderr once the run returns. magex 1.30.2's recordings hold the real runs
	// (TestOutputReadsEveryMagex1302Recording, TestMagex1302ResultsAgreeWithGit).
	const commit = "725b4e00476a9f1c2fd877b7157fb3eac747e300"

	t.Run("a release that went well", func(t *testing.T) {
		t.Parallel()

		result := BumpResult{
			From: recFrom, To: recVersion, Bump: recBump, Expect: recVersion, Tag: recVersion, Commit: commit,
			Pushed: true, StartBranch: recFeature, Branch: recFeature, Warnings: []string{notOnHead},
		}

		lines := append(fromFeatureToTag(), "To /work/origin.git", " * [new tag]         v1.4.3 -> v1.4.3",
			"✅ ✅ Tag pushed to remote")
		lines = append(lines, backOnFeature()...)
		got := readLines(append(lines, jsonLine(t, result))...)

		require.NotNil(t, got.Result)
		assert.Equal(t, result, *got.Result)
		assert.Equal(t, StepTagPushed, got.Step, "the lines still drive the steps")
		assert.True(t, got.TagPushed)
		assert.Equal(t, recFeature, got.BackOn)
		assert.Empty(t, got.Unrecognized)
		assert.Empty(t, got.LastError())
	})

	t.Run("a tag a pre-push hook stopped", func(t *testing.T) {
		t.Parallel()

		why := "failed to push tag: command failed [git push origin v1.4.3]: exit status 1"
		how := "tag v1.4.3 was left in the local repository: push it with 'git push origin v1.4.3' " +
			"or delete it with 'git tag -d v1.4.3'"
		result := BumpResult{
			From: recFrom, To: recVersion, Bump: recBump, Expect: recVersion, Tag: recVersion, Commit: commit,
			StartBranch: recFeature, Branch: recFeature, Warnings: []string{notOnHead, sendByHand},
			Error: why + "\n" + how,
		}

		lines := append(fromFeatureToTag(),
			"pushes are refused here",
			"error: failed to push some refs to '/work/origin.git'",
			"10:00:00 [ERROR] ❌ Failed to push tag to remote",
			"10:00:00 [WARN] 💡 "+sendByHand)
		lines = append(lines, backOnFeature()...)
		got := readLines(append(lines, jsonLine(t, result), "❌ Error: "+why, how)...)

		require.NotNil(t, got.Result)
		assert.False(t, got.Result.Pushed)
		assert.Equal(t, result.Error, got.Result.Error)
		assert.Equal(t, []string{why + "\n" + how}, got.Errors, "the error keeps the line it goes on to")
		assert.Equal(t, why, got.LastError())
		assert.Equal(t, []string{"pushes are refused here"}, got.Unrecognized)
		assert.Equal(t, "pushes are refused here", got.HookLine)
		assert.True(t, got.PushRefsFailed)
	})

	t.Run("a version that moved", func(t *testing.T) {
		t.Parallel()

		why := "expected v1.4.3, but the next patch is v1.4.4: another version was tagged meanwhile; nothing was tagged"
		result := BumpResult{
			From: recVersion, To: recNext, Bump: recBump, Expect: recVersion, StartBranch: recBranch, Branch: recBranch,
			Error: why,
		}

		got := readLines(append(bumpHeader(),
			"10:00:00 [INFO] Already on target branch 'master'",
			"10:00:00 [INFO] Pulling latest changes...",
			"✅ Successfully pulled latest changes",
			"10:00:00 [INFO]   From:    v1.4.3",
			"10:00:00 [INFO]   To:      v1.4.4",
			"10:00:00 [INFO]   Type:    patch bump",
			jsonLine(t, result),
			"❌ Error: "+why,
		)...)

		require.NotNil(t, got.Result)
		assert.Empty(t, got.Result.Tag)
		assert.Empty(t, got.Tag)
		assert.Equal(t, StepUpdated, got.Step)
		assert.Equal(t, recNext, got.Version)
		assert.Equal(t, []string{why}, got.Errors)
		assert.Empty(t, got.Unrecognized)
	})

	t.Run("only a line with the result's fields is the result", func(t *testing.T) {
		t.Parallel()

		got := readLines(`{"from":"v1.4.2","to":"v1.4.3"}`, `{"tag": broken`, `{"from":"","to":"","tag":"",`+
			`"commit":"","pushed":"yes","branch":"","warnings":[],"error":""}`)
		assert.Nil(t, got.Result)
		assert.Equal(t, 3, got.UnrecognizedCount)

		// warnings null still reads as a list.
		got = readLines(`{"from":"","to":"","tag":"v1.4.3","commit":"","pushed":true,"branch":"","warnings":null,"error":""}`)
		require.NotNil(t, got.Result)
		assert.NotNil(t, got.Result.Warnings)
		assert.True(t, got.Result.Pushed)
	})
}

func TestOutputReadsTheFastForwardOnlyLines(t *testing.T) {
	t.Parallel()

	// magex 1.30.2's ff-only, from fastForwardBranch in pkg/mage/version.go.
	for _, done := range []string{
		"✅ Fast-forwarded 'master' by 3 commits to origin/master",
		"✅ 'master' is level with origin/master",
	} {
		got := readLines(append(bumpHeader(),
			"10:00:00 [INFO] Already on target branch 'master'",
			"10:00:00 [INFO] Updating 'master' from origin/master (fast-forward only)...",
		)...)
		assert.Equal(t, StepUpdated, got.Working, done)
		assert.True(t, got.FastForward, done)

		got = readLines(append(bumpHeader(),
			"10:00:00 [INFO] Already on target branch 'master'",
			"10:00:00 [INFO] Updating 'master' from origin/master (fast-forward only)...",
			done,
		)...)
		assert.Equal(t, StepUpdated, got.Step, done)
		assert.Empty(t, got.Working, done)
		assert.Empty(t, got.Unrecognized, done)
	}

	pulling := readLines(append(bumpHeader(), "10:00:00 [INFO] Pulling latest changes...")...)
	assert.Equal(t, StepUpdated, pulling.Working)
	assert.False(t, pulling.FastForward, "git pull --rebase, as magex does without ff-only")

	refused := readLines(
		"❌ Error: ff-only: branch 'master' has 2 commits that origin/master doesn't have, so it can't be "+
			"fast-forwarded; nothing was tagged",
		"to release them, push them first (git push origin master); to drop them, reset the branch to origin "+
			"in a terminal (git checkout master && git reset --hard origin/master)",
	)
	require.Len(t, refused.Errors, 1)
	assert.Contains(t, refused.Errors[0], "\nto release them, push them first")
	assert.Empty(t, refused.Unrecognized)

	// A line like a continuation, after anything but an error, is not one.
	stray := readLines("10:00:00 [INFO] Detecting current version...", "tag v1.4.3 was left in the local repository: x")
	assert.Equal(t, []string{"tag v1.4.3 was left in the local repository: x"}, stray.Unrecognized)
}

func TestOutputReadsWhatGitAndOriginSaid(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		lines []string
		check func(t *testing.T, got Output)
	}{
		"ssh credentials": {
			lines: []string{"git@github.com: Permission denied (publickey).", cantReachOrigin},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Equal(t, "git@github.com: Permission denied (publickey).", got.Auth)
				assert.Equal(t, []string{cantReachOrigin}, got.Unrecognized)
			},
		},
		"https credentials": {
			lines: []string{
				"remote: Permission to acme-org/example-repo.git denied to someone.",
				"fatal: unable to access 'https://github.com/acme-org/example-repo.git/': The requested URL returned error: 403",
			},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Equal(t, "Permission to acme-org/example-repo.git denied to someone.", got.Auth, "the first line")
				assert.Empty(t, got.Unrecognized)
			},
		},
		"a rejection without a rule": {
			lines: []string{
				"To https://github.com/acme-org/example-repo.git",
				" ! [remote rejected] v1.4.3 -> v1.4.3 (pre-receive hook declined)",
				"error: failed to push some refs to 'https://github.com/acme-org/example-repo.git'",
			},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Equal(t, "pre-receive hook declined", got.Refused)
				assert.True(t, got.PushRejected)
				assert.True(t, got.PushRefsFailed)
			},
		},
		"a rejection on this Mac": {
			lines: []string{" ! [rejected]        v1.4.3 -> v1.4.3 (already exists)"},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.True(t, got.PushRejected)
				assert.Empty(t, got.Refused, "origin refused nothing")
			},
		},
		"origin's other lines": {
			lines: []string{"remote: Something origin said", "remote:"},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Equal(t, []string{"remote: Something origin said"}, got.Unrecognized)
			},
		},
		"a hook's line after the push failed": {
			lines: []string{
				"10:00:00 [INFO] Pushing tag to remote...",
				"hook says no",
				"error: failed to push some refs to '/work/origin.git'",
				"post-checkout: noise",
			},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Equal(t, "hook says no", got.HookLine, "a line after git's says nothing of the push")
			},
		},
		"debug lines": {
			lines: []string{"10:00:00 [DEBUG] running git"},
			check: func(t *testing.T, got Output) {
				t.Helper()
				assert.Empty(t, got.Unrecognized)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, readLines(tc.lines...))
		})
	}
}
