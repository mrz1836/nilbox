package core

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

var errChangesUnreadable = errors.New("changes unreadable")

// fakeChecks answers the inbox's file checks with one check, or ErrNotChecked for nil.
type fakeChecks struct {
	check *sensitive.Check
}

func (f fakeChecks) CheckFiles(context.Context, int64) (*sensitive.Check, error) { return f.answer() }

func (f fakeChecks) StoredFiles(context.Context, int64) (*sensitive.Check, error) { return f.answer() }

func (f fakeChecks) CheckFilesUnattended(context.Context, int64) (*sensitive.Check, error) {
	return f.answer()
}

func (f fakeChecks) answer() (*sensitive.Check, error) {
	if f.check == nil {
		return nil, sensitive.ErrNotChecked
	}

	return f.check, nil
}

// fakeChanges answers what agent runs changed, at the head it was asked for.
type fakeChanges struct {
	byHead map[string]sensitive.Check
	err    error
	asked  []string
}

func (f *fakeChanges) AgentChanged(_ context.Context, _ int64, head string) (sensitive.Check, bool, error) {
	f.asked = append(f.asked, head)
	c, ok := f.byHead[head]

	return c, ok, f.err
}

const fenceHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestARunsAgentFileHoldsAnItemWhoseFilesArentChecked(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{byHead: map[string]sensitive.Check{
		"": fence.CheckChanged(fenceHead, []string{".claude/settings.json", "main.go"}, sensitive.DefaultConfig()),
	}}
	files := agentFiles{files: fakeChecks{}, changes: changes}

	reads := []func(context.Context, int64) (*sensitive.Check, error){
		files.CheckFiles, files.StoredFiles, files.CheckFilesUnattended,
	}

	for _, read := range reads {
		check, err := read(t.Context(), 1)
		require.NoError(t, err, "a trusted bot's pull request an agent run wrote to is checked")
		require.NotNil(t, check)

		agent, ok := check.Category(sensitive.CategoryAgent)
		require.True(t, ok, "the run's agent file holds the next click")
		assert.Equal(t, ".claude/settings.json", agent.Files[0].Path)
		assert.Equal(t, fenceHead, check.Head)
	}

	assert.Equal(t, []string{"", "", ""}, changes.asked, "with no check of its own, the item's stored head is read")
}

func TestARunsChangesJoinTheItemsOwnCheck(t *testing.T) {
	t.Parallel()

	own := &sensitive.Check{
		Status: sensitive.StatusClear, Head: fenceHead, Checked: 1, Total: 1, Complete: true, Groups: []sensitive.Group{},
	}
	changes := &fakeChanges{byHead: map[string]sensitive.Check{
		fenceHead: fence.CheckChanged(fenceHead, []string{"CLAUDE.md"}, sensitive.DefaultConfig()),
	}}

	check, err := agentFiles{files: fakeChecks{check: own}, changes: changes}.CheckFiles(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, sensitive.StatusFlagged, check.Status)
	assert.Equal(t, []string{fenceHead}, changes.asked, "the check's own head is read")

	_, ok := check.Category(sensitive.CategoryAgent)
	assert.True(t, ok)
	assert.Equal(t, sensitive.StatusClear, own.Status, "the inbox's own check is left as it was")
}

// The routines store a tools preset in the catalog's words, and a run is fenced in the
// fence's: the two are one list, converted as they are.
func TestEveryRoutinePresetIsAFence(t *testing.T) {
	t.Parallel()

	presets := catalog.Presets()
	require.Len(t, presets, len(fence.Presets()))

	for i, info := range presets {
		p := fence.Preset(info.ID)
		assert.True(t, p.Valid(), "%s", info.ID)
		assert.Equal(t, fence.Presets()[i], p, "in the same order, from the narrowest")
		assert.Equal(t, info.Label, p.Label())
		assert.Equal(t, info.MayPush, p.MayPush(), "%s", info.ID)
		assert.Equal(t, info.ReadOnly, !p.RunsCommands() && !p.Edits(), "%s", info.ID)
	}

	assert.Equal(t, catalog.PresetEditTestPush, catalog.DefaultPreset)
}

func TestNoChangesLeaveTheCheckAsItWas(t *testing.T) {
	t.Parallel()

	clean := &fakeChanges{byHead: map[string]sensitive.Check{
		"": fence.CheckChanged(fenceHead, []string{"main.go"}, sensitive.DefaultConfig()),
	}}

	_, err := agentFiles{files: fakeChecks{}, changes: clean}.StoredFiles(t.Context(), 1)
	require.ErrorIs(t, err, sensitive.ErrNotChecked, "a run that changed no guarded file adds nothing")

	_, err = agentFiles{files: fakeChecks{}, changes: &fakeChanges{}}.StoredFiles(t.Context(), 1)
	require.ErrorIs(t, err, sensitive.ErrNotChecked)

	_, err = agentFiles{files: fakeChecks{}, changes: &fakeChanges{err: errChangesUnreadable}}.StoredFiles(t.Context(), 1)
	require.ErrorIs(t, err, errChangesUnreadable, "what runs changed can't be read, so nothing launches unheld")
}
