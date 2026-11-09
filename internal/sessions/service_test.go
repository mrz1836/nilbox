package sessions_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fakeFocuser records the terminals it was asked to bring forward.
type fakeFocuser struct {
	mu    sync.Mutex
	asked []string
	err   error
}

func (f *fakeFocuser) Focus(_ context.Context, terminal, tty string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, terminal+" "+tty)

	return f.err
}

// newService returns a Service over a fresh ledger holding a running session, s1, whose
// process is 4100, and an ended one, s2.
func newService(t *testing.T, ps *testutil.Runner, focus *fakeFocuser) *sessions.Service {
	t.Helper()

	store := sessions.NewStore(testutil.DB(t))

	for _, r := range []sessions.Record{record("s1", 12), record("s2", 12), record("s3", 13)} {
		require.NoError(t, store.Insert(t.Context(), r))
	}

	_, err := store.Apply(t.Context(), launched().Add(time.Minute), []sessions.Observation{
		{ID: "s1", State: sessions.StateNeedsApproval, PID: 4100}, {ID: "s2", State: sessions.StateEnded},
	})
	require.NoError(t, err)

	return sessions.NewService(sessions.Config{
		Store: store, Focuser: focus, PS: ps, Home: "/srv", Switch: &toggle{on: true},
		Clock: testutil.NewClock(launched().Add(time.Hour)),
	})
}

func TestList(t *testing.T) {
	t.Parallel()

	svc := newService(t, &testutil.Runner{}, &fakeFocuser{})

	got, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.True(t, got.Following)
	require.Len(t, got.Sessions, 3)

	byID := map[string]sessions.Session{}
	for _, s := range got.Sessions {
		byID[s.ID] = s
	}

	assert.Equal(t, sessions.StateNeedsApproval, byID["s1"].State)
	assert.True(t, byID["s1"].CanFocus)
	assert.True(t, byID["s2"].CanResume)
	assert.False(t, byID["s3"].CanFocus, "its process is not known yet")

	empty := sessions.NewService(sessions.Config{Store: sessions.NewStore(testutil.DB(t)), Switch: &toggle{}})

	got, err = empty.List(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got.Sessions, "never nil, so the window gets []")
	assert.False(t, got.Following)
}

func TestFocus(t *testing.T) {
	t.Parallel()

	ps := &testutil.Runner{Stdout: "ttys004 \n"}
	focus := &fakeFocuser{}
	svc := newService(t, ps, focus)

	require.NoError(t, svc.Focus(t.Context(), "s1"))
	assert.Equal(t, []string{"iterm2 /dev/ttys004"}, focus.asked)
	assert.Equal(t, [][]string{{"/bin/ps", "-o", "tty=", "-p", "4100"}}, ps.Calls())

	err := svc.Focus(t.Context(), "s2")
	require.ErrorIs(t, err, sessions.ErrEnded)

	err = svc.Focus(t.Context(), "s3")
	require.ErrorIs(t, err, sessions.ErrNoTerminal)
	assert.Contains(t, uierr.Message(err), "isn't known yet")

	err = svc.Focus(t.Context(), "missing")
	require.ErrorIs(t, err, sessions.ErrNotFound)
}

func TestFocusWithoutATerminal(t *testing.T) {
	t.Parallel()

	for name, ps := range map[string]*testutil.Runner{
		"no tty":       {Stdout: "??\n"},
		"no output":    {Stdout: ""},
		"ps failed":    {Err: assert.AnError},
		"odd output":   {Stdout: "two words\n"},
		"just a query": {Stdout: "?\n"},
	} {
		svc := newService(t, ps, &fakeFocuser{})

		err := svc.Focus(t.Context(), "s1")
		require.ErrorIs(t, err, sessions.ErrNoTerminal, name)
		assert.Contains(t, uierr.Message(err), "Couldn't find the session's terminal", name)
	}
}

func TestTTY(t *testing.T) {
	t.Parallel()

	got, err := sessions.TTY(t.Context(), &testutil.Runner{Stdout: "pts/3\n"}, 7)
	require.NoError(t, err)
	assert.Equal(t, "/dev/pts/3", got, "Linux names its terminals pts/<n>")

	got, err = sessions.TTY(t.Context(), &testutil.Runner{Stdout: "/dev/ttys001\n"}, 7)
	require.NoError(t, err)
	assert.Equal(t, "/dev/ttys001", got)
}

func TestListOffersNoResumeForAConversationStillRunning(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	ended, carried := record("ended", 12), record("carried", 12)
	carried.AgentSessionID, carried.ResumedFrom = "ended", "ended"

	require.NoError(t, store.Insert(t.Context(), ended))
	_, err := store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "ended", State: sessions.StateEnded}})
	require.NoError(t, err)
	require.NoError(t, store.Insert(t.Context(), carried))

	svc := sessions.NewService(sessions.Config{Store: store, Clock: testutil.NewClock(launched().Add(time.Hour))})

	got, err := svc.List(t.Context())
	require.NoError(t, err)

	for _, s := range got.Sessions {
		assert.False(t, s.CanResume, "%s: its conversation runs on in the session resumed from it", s.ID)
	}
}
