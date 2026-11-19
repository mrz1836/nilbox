package inbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userTriage is each of the user's triage steps on the item with id, under ctx, by what
// it reports when it fails.
func userTriage(ctx context.Context, h *harness, id int64) map[string]func() error {
	return map[string]func() error{
		"dismiss the item": func() error { _, err := h.svc.Dismiss(ctx, id); return err },
		"snooze the item":  func() error { _, err := h.svc.Snooze(ctx, id, 3); return err },
		"restore the item": func() error { _, err := h.svc.Restore(ctx, id); return err },
		"unpin the item":   func() error { _, err := h.svc.Unpin(ctx, id); return err },
	}
}

func TestTheUsersTriageReportsATriageItCantRead(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)

	_, err := h.svc.Pin(t.Context(), id)
	require.NoError(t, err)

	// Renaming keeps the rows, while every read of the triage by its name fails.
	h.exec(t, "ALTER TABLE item_local_state RENAME TO item_local_state_gone")

	for want, step := range userTriage(t.Context(), h, id) {
		require.ErrorContains(t, step(), want)
	}
}

func TestTheUsersTriageThatCantBeStoredChangesNothing(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	notified := h.events.Count()

	refuse(t, h, "BEFORE INSERT ON item_local_state", "")

	steps := userTriage(t.Context(), h, id)

	for _, want := range []string{"dismiss the item", "snooze the item"} {
		require.ErrorContains(t, steps[want](), want)
	}

	assert.Zero(t, h.count(t, "item_local_state"), "nothing is set aside")
	assert.Equal(t, notified, h.events.Count(), "and the window is told nothing")
}

func TestASnoozeReportsTheActivityItCantRead(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)

	h.exec(t, "ALTER TABLE item_details RENAME TO item_details_gone")

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.ErrorContains(t, err, "snooze the item")
	assert.Zero(t, h.count(t, "item_local_state"))
}
