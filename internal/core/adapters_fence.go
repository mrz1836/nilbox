package core

import (
	"context"
	"errors"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// agentChanges is what the launcher's file check reads beside the inbox's own: the files
// agent runs' commits changed on an item, at a head. *inbox.Store implements it.
type agentChanges interface {
	AgentChanged(ctx context.Context, itemID int64, head string) (sensitive.Check, bool, error)
}

// agentFiles is the launcher's FileChecker: the inbox's file check, with what agent runs
// changed added in (fence.MergeChanged), so a launch on an item at a head where a run
// wrote an agent file is held for it, as an outside contributor's pull request is, whoever
// opened the item, and a run's own changes show among the files the hold names.
type agentFiles struct {
	files   launcher.FileChecker
	changes agentChanges
}

// CheckFiles checks the item's files as of now, with what agent runs changed at its head.
func (f agentFiles) CheckFiles(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	check, err := f.files.CheckFiles(ctx, itemID)

	return f.withChanges(ctx, itemID, check, err)
}

// CheckFilesUnattended checks the item's files under the unattended scope, for a
// routine's launch, with what agent runs changed at its head: a second background run on
// an item a run wrote agent files to waits for the user too.
func (f agentFiles) CheckFilesUnattended(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	check, err := f.files.CheckFilesUnattended(ctx, itemID)

	return f.withChanges(ctx, itemID, check, err)
}

// StoredFiles returns the item's last check, with what agent runs changed at its head.
func (f agentFiles) StoredFiles(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	check, err := f.files.StoredFiles(ctx, itemID)

	return f.withChanges(ctx, itemID, check, err)
}

// withChanges adds what agent runs changed at the check's head (or the item's stored
// head, for an item whose files aren't checked) to a check, when a run changed a guarded
// file there. An item whose files aren't checked stays so when no run did.
func (f agentFiles) withChanges(ctx context.Context, itemID int64, check *sensitive.Check, err error) (*sensitive.Check, error) {
	if err != nil && !errors.Is(err, sensitive.ErrNotChecked) {
		return nil, err
	}

	head := ""
	if check != nil {
		head = check.Head
	}

	changed, found, changesErr := f.changes.AgentChanged(ctx, itemID, head)
	if changesErr != nil {
		return nil, changesErr
	}

	if !found || changed.Status != sensitive.StatusFlagged && changed.Complete {
		return check, err
	}

	merged := fence.MergeChanged(check, changed)

	return &merged, nil
}
