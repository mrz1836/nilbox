package autopilot_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errUnread is a read beside a change to the routines that fails: the account signed in,
// or the starter prompts' names.
var errUnread = errors.New("it can't be read now")

// unreadNames is the starter prompts' names, which can't be read.
type unreadNames struct{}

func (unreadNames) StarterNames(context.Context) (map[string]string, error) { return nil, errUnread }

func TestAChangeTheDatabaseRefusesLeavesTheRoutinesAsTheyWere(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	saved := r.save(t, draft(firstName, notifySpec()))
	ctx := t.Context()

	testutil.Exec(t, r.db, `CREATE TRIGGER no_inserts BEFORE INSERT ON autopilot_routines
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	testutil.Exec(t, r.db, `CREATE TRIGGER no_updates BEFORE UPDATE ON autopilot_routines
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	for name, change := range map[string]func() error{
		"a new routine": func() error { return testutil.ErrOf(r.svc.Save(ctx, draft("Second", notifySpec()))) },
		"an edit": func() error {
			return testutil.ErrOf(r.svc.Save(ctx, autopilot.Draft{ID: saved.ID, Name: "Renamed", Spec: notifySpec()}))
		},
		"a copy":   func() error { return testutil.ErrOf(r.svc.Duplicate(ctx, saved.ID)) },
		"a delete": func() error { return r.svc.Delete(ctx, saved.ID) },
		"switching it on": func() error {
			return testutil.ErrOf(r.svc.SetEnabled(ctx, saved.ID, true, autopilot.CurrentRun))
		},
		"its mode": func() error {
			return testutil.ErrOf(r.svc.SetMode(ctx, saved.ID, autopilotprefs.ModeAuto, autopilot.CurrentRun))
		},
	} {
		require.ErrorContains(t, change(), "refused by the test", name)
	}

	got := r.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, firstName, got[0].Name)
	assert.False(t, got[0].Enabled)
	assert.Equal(t, autopilotprefs.ModeAsk, got[0].Mode)
}

func TestARoutinesDescriptionIsPlainText(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	_, err := r.svc.Save(t.Context(), autopilot.Draft{Name: firstName, Description: "Fix\u202eit", Spec: notifySpec()})
	require.ErrorIs(t, err, autopilot.ErrInvalidDescription)
	assert.Equal(t, "Keep the description to plain text.", uierr.Message(err))
	assert.Empty(t, r.list(t))
}

func TestWhatCantBeReadBesideAChangeIsLoggedAndTheChangeStands(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	r := rigAt(t, db, filepath.Join(t.TempDir(), "gone", "autopilot.json"), autopilotprefs.ModeAsk)
	r.accounts.err = errUnread

	saved := r.save(t, draft(firstName, notifySpec()))
	assert.Empty(t, saved.Account, "no account could be read to own it")
	assert.Contains(t, r.logs.String(), "could not read the account signed in for the routines")
	assert.Contains(t, r.logs.String(), "could not save the copy of the routines", "its copy waits for the next change")

	logs := &testutil.Logs{}
	svc := autopilot.NewService(autopilot.Config{Pool: db, Names: unreadNames{}, Logger: logs.Logger()})

	got, err := svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.NotEmpty(t, got[0].Sentence, "said with each starter's key in words")
	assert.Contains(t, logs.String(), "could not read the starter prompts' names for the routines")
}

func TestTheRoutinesSayWhatOfThemCouldntBeRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	_, err := autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)}).List(ctx)
	require.Error(t, err)

	// Their figures come from the matches.
	r := newRig(t, autopilotprefs.ModeAsk)
	r.save(t, draft(firstName, notifySpec()))
	testutil.Exec(t, r.db, `DROP TABLE autopilot_matches`)

	_, err = r.svc.List(ctx)
	assert.Equal(t, "Autopilot's history couldn't be read.", uierr.Message(err))

	// A copy reads them in its own transaction, to place itself.
	gone := newRig(t, autopilotprefs.ModeAsk)
	testutil.Exec(t, gone.db, `DROP TABLE autopilot_routines`)

	_, err = gone.svc.Duplicate(ctx, 1)
	require.ErrorContains(t, err, "read the routines")
}
