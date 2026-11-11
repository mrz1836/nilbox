package release

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// progressAt is a release's progress once the reader has read a recording's lines up to
// and including the first that holds line.
type progressAt struct {
	line string
	want scriptrun.Progress
}

// progressThrough feeds a fresh reader the recording called name one line at a time, and
// returns the progress after each line, in order, with the lines.
func progressThrough(t *testing.T, name string, in Intent) ([]string, []scriptrun.Progress) {
	t.Helper()

	rec := loadRecording(t, name)
	r := NewOutputReader()
	lines := make([]string, 0, len(rec.Lines))
	seen := make([]scriptrun.Progress, 0, len(rec.Lines))

	for _, l := range rec.Lines {
		_, err := r.Write([]byte(l.Text + "\n"))
		require.NoError(t, err)

		lines = append(lines, l.Text)
		seen = append(seen, r.Progress(in))
	}

	return lines, seen
}

func TestProgressThroughARelease(t *testing.T) {
	t.Parallel()

	const (
		leaveTag    = "Stopping now can leave v1.4.3 on this Mac but not on GitHub, and your clone on master."
		leaveClone  = "Stopping now can leave your clone on master."
		leaveMinor  = "Stopping now can leave v1.5.0 on this Mac but not on GitHub."
		bumpTypeMsg = "Using bump type"
	)

	fromFeature := Intent{Version: recVersion, Bump: BumpPatch, Branch: recBranch, Original: recFeature}
	onMaster := Intent{Version: "v1.5.0", Bump: BumpMinor, Branch: recBranch, Original: recBranch}

	tests := []struct {
		name string
		in   Intent
		at   []progressAt
	}{
		{
			name: "patch-feature-behind", in: fromFeature,
			at: []progressAt{
				{bumpTypeMsg, scriptrun.Progress{}},
				{"Switching to branch 'master'", scriptrun.Progress{Working: "onBranch", StopRefused: stopOff}},
				{"✅ Switched to branch 'master'", scriptrun.Progress{Step: "onBranch", StopRefused: stopOff}},
				{"Pulling latest changes", scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}},
				{"Updating 3eeea85", scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}},
				{"Successfully pulled", scriptrun.Progress{Step: "updated", StopConfirm: leaveClone}},
				{"Created tag", scriptrun.Progress{Step: "tagCreated", StopConfirm: leaveTag}},
				{"Pushing tag", scriptrun.Progress{Step: "tagCreated", Working: "tagPushed", StopConfirm: leaveTag}},
				{"Tag pushed to remote", scriptrun.Progress{Step: "tagPushed", StopConfirm: leaveClone}},
				{"Switched back to original branch", scriptrun.Progress{Step: "tagPushed"}},
			},
		},
		{
			name: "minor-on-master", in: onMaster,
			at: []progressAt{
				{"Already on target branch", scriptrun.Progress{Step: "onBranch", StopRefused: stopOff}},
				{"Pulling latest changes", scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}},
				{"Successfully pulled", scriptrun.Progress{Step: "updated"}},
				{"Created tag", scriptrun.Progress{Step: "tagCreated", StopConfirm: leaveMinor}},
				{"Tag pushed to remote", scriptrun.Progress{Step: "tagPushed"}},
			},
		},
		{
			name: "sigint-mid-rebase", in: Intent{Version: recVersion, Branch: recBranch, Original: recBranch},
			at: []progressAt{
				{"Pulling latest changes", scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}},
				{"Canceled by user", scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines, seen := progressThrough(t, tt.name, tt.in)

			for _, at := range tt.at {
				i := indexOfLine(lines, at.line)
				require.GreaterOrEqual(t, i, 0, "the recording prints %q", at.line)
				assert.Equal(t, at.want, seen[i], "after %q", lines[i])
			}
		})
	}
}

// indexOfLine returns the index of the first of lines that holds part, or -1.
func indexOfLine(lines []string, part string) int {
	for i, l := range lines {
		if strings.Contains(l, part) {
			return i
		}
	}

	return -1
}

func TestStopIsOffWheneverMagexSwitchesOrPulls(t *testing.T) {
	t.Parallel()

	in := Intent{Version: recVersion, Branch: recBranch, Original: recFeature}

	for _, name := range recordingNames(t) {
		lines, seen := progressThrough(t, name, in)

		for i, p := range seen {
			pulling := p.Working == string(StepOnBranch) || p.Working == string(StepUpdated) ||
				p.Step == string(StepOnBranch)

			assert.Equal(t, pulling, p.StopRefused != "", "%s, after %q: %+v", name, lines[i], p)

			if p.StopRefused != "" {
				assert.Equal(t, stopOff, p.StopRefused)
				assert.Empty(t, p.StopConfirm, "Stop is off, so it asks nothing")
			}
		}
	}
}

func TestProgressOfTheFastForwardOnlyPull(t *testing.T) {
	t.Parallel()

	r := NewOutputReader()
	in := Intent{Version: recVersion, Branch: recBranch, Original: recBranch}

	_, _ = r.Write([]byte("21:59:47 [INFO] Already on target branch 'master'\n" +
		"21:59:47 [INFO] Updating 'master' from origin (fast-forward only)...\n"))
	assert.Equal(t, scriptrun.Progress{Step: "onBranch", Working: "updated", StopRefused: stopOff}, r.Progress(in))

	_, _ = r.Write([]byte("✅ Fast-forwarded 'master' by 2 commits to 3bb9381\n"))
	assert.Equal(t, scriptrun.Progress{Step: "updated"}, r.Progress(in), "Stop is on again once the branch is updated")
}

func TestProgressOfATagNotNamedYet(t *testing.T) {
	t.Parallel()

	r := NewOutputReader()
	r.out.Step, r.out.Branch = StepTagCreated, recBranch

	assert.Equal(t, "Stopping now can leave v1.4.3 on this Mac but not on GitHub.",
		r.Progress(Intent{Version: recVersion, Original: recBranch}).StopConfirm,
		"the version the sheet showed, while magex hasn't named its tag")

	detached := NewOutputReader()
	detached.out.Step = StepUpdated

	assert.Equal(t, "Stopping now can leave your clone on master.",
		detached.Progress(Intent{Version: recVersion, Branch: recBranch}).StopConfirm,
		"a clone that was detached is away once magex is on the branch")
}
