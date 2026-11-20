package describeit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestATurnThatFailedSaysWhy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		res    stream.Result
		ok     bool
		limit  *stream.Limit
		ending ending
		want   string
	}{
		{
			name: "a usage limit", limit: &stream.Limit{Status: stream.LimitRejected},
			want: agentWords + "'s usage limit is reached, so the turn stopped. Try again once it resets.",
		},
		{
			name: "its budget", res: stream.Result{Subtype: budgetSubtype, IsError: true}, ok: true,
			want: "The turn reached its budget of $2 before it finished. Ask again for less, or carry on in your next message.",
		},
		{
			name: "an error the result names", res: stream.Result{Subtype: "error_during_execution", Errors: []string{"API Error: 529"}},
			ok: true, want: agentWords + " ended the turn in error: API Error: 529.",
		},
		{
			name: "an error by its subtype", res: stream.Result{Subtype: "error_during_execution"}, ok: true,
			want: agentWords + " ended the turn in error: error during execution.",
		},
		{
			name: "a line it wrote", ending: ending{detail: "unknown option '--frobnicate'", code: 1},
			want: agentWords + " ended without finishing the turn: unknown option '--frobnicate'.",
		},
		{name: "its exit", ending: ending{code: 137}, want: agentWords + " ended without finishing the turn (exit status 137)."},
		{name: "nothing at all", want: agentWords + " ended without finishing the turn."},
	} {
		assert.Equal(t, tt.want, failureWords(tt.res, tt.ok, tt.limit, tt.ending), tt.name)
	}
}

func TestOnlyWhatAPartCanHaveIsRecorded(t *testing.T) {
	t.Parallel()

	all := []kit.OutcomeKind{kit.OutcomeAdded, kit.OutcomeSaved, kit.OutcomeApplied, kit.OutcomeBuilder, kit.OutcomeDiscarded}

	for part, want := range map[kit.Part][]kit.OutcomeKind{
		kit.PartPrompt:   {kit.OutcomeAdded, kit.OutcomeDiscarded},
		kit.PartRoutine:  {kit.OutcomeAdded, kit.OutcomeSaved, kit.OutcomeBuilder, kit.OutcomeDiscarded},
		kit.PartSettings: {kit.OutcomeApplied, kit.OutcomeDiscarded},
		kit.PartRules:    {kit.OutcomeApplied, kit.OutcomeDiscarded},
		"other":          {},
	} {
		var got []kit.OutcomeKind

		for _, kind := range all {
			if fits(part, kind) {
				got = append(got, kind)
			}
		}

		assert.ElementsMatch(t, want, got, part)
	}
}

func TestSmallWordsReadAsASentence(t *testing.T) {
	t.Parallel()

	assert.Empty(t, orSeries(nil))
	assert.Equal(t, "low", orSeries([]string{"low"}))
	assert.Equal(t, "low or high", orSeries([]string{"low", "high"}))
	assert.Equal(t, "low, medium, or high", orSeries([]string{"low", "medium", "high"}))

	page := listedPage(runevent.Page{})
	assert.NotNil(t, page.Events)
	assert.NotNil(t, page.Progress.Todos)
}

func TestTheFolderIsTheUsersAloneAndNeverALink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	elsewhere := t.TempDir()
	link := filepath.Join(dir, workName)
	require.NoError(t, os.Symlink(elsewhere, link))

	require.NoError(t, ensureDir(link))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "a link where a folder belongs is replaced with one")
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	file := filepath.Join(dir, "file")
	testutil.WriteFile(t, file, "x")
	require.Error(t, ensureDir(filepath.Join(file, "below")), "a folder under a file can't be made")

	_, err = writeIfChanged(filepath.Join(file, "below", "x.json"), []byte("{}"))
	require.Error(t, err)
}

func TestWritesThatFailAreLoggedAndSaid(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	testutil.WriteFile(t, r.path("conversations"), "a file where the folder goes")

	_, err := r.svc.NewConversation(t.Context(), AgentChoice{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversations")

	testutil.SkipAsRoot(t)

	blocked := newRig(t)
	testutil.WriteFile(t, blocked.path("instructions.md"), "old words")
	require.NoError(t, os.Chmod(blocked.dir, 0o500))       //nolint:gosec // the folder made read-only, so its writes fail
	t.Cleanup(func() { _ = os.Chmod(blocked.dir, 0o700) }) //nolint:gosec // and the user's again, as the app makes it

	require.Error(t, blocked.svc.writeInstructions(), "a folder that can't be written is said")
	require.Error(t, blocked.svc.writeContext(t.Context()))

	blocked.svc.preparePlugin(t.Context())
	assert.NoDirExists(t, blocked.path("plugin"), "and what can't be written is left")
}

func TestStopBeforeATurnStartedStopsItAsItStarts(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.newConversation()

	lt, err := r.svc.reserve()
	require.NoError(t, err)

	require.NoError(t, r.svc.Stop(t.Context(), id), "a turn getting ready in the current conversation is its")
	assert.True(t, lt.stopped.Load())

	r.svc.release(lt)

	other, err := r.svc.reserve()
	require.NoError(t, err)

	require.NoError(t, r.svc.Stop(t.Context(), "00000000-0000-4000-8000-000000000009"))
	assert.False(t, other.stopped.Load(), "another conversation's stop isn't")

	r.svc.release(other)
}

func TestTheExecutableMustBeAbsolute(t *testing.T) {
	t.Parallel()

	svc := New(Config{Dir: t.TempDir(), Executable: func() string { return brand.Slug }, Stable: func() bool { return true }})
	assert.Empty(t, svc.executable())
	assert.False(t, svc.stable(), "an app that can't tell where it is isn't in a stable place")
	assert.False(t, svc.linkUsable())
}
