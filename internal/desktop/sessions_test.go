package desktop_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// fakeSessions lists waiting sessions and records what it was asked to focus.
type fakeSessions struct {
	mu       sync.Mutex
	waiting  []sessions.Session
	err      error
	focusErr error
	focused  []string
}

func (f *fakeSessions) Waiting(context.Context) ([]sessions.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.waiting, f.err
}

func (f *fakeSessions) Focus(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.focused = append(f.focused, id)

	return f.focusErr
}

func (f *fakeSessions) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.focused...)
}

// approval is a Claude session on example-repo#12 that needs approval.
func approval() sessions.Session {
	return sessions.Session{
		ID: "s1", Agent: agent.Claude, ItemID: 7, Repo: "acme-org/example-repo", Number: 12, URL: exampleURL,
		State: sessions.StateNeedsApproval,
	}
}

func TestTheMenuListsTheSessionsWaiting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		waiting := &fakeSessions{waiting: []sessions.Session{approval()}}
		s := r.shell(func(c *desktop.Config) { c.Sessions = waiting })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		draws := r.tray.drawn()
		require.Len(t, draws, 1)
		require.Len(t, draws[0].Waiting, 1)
		assert.Equal(t, "example-repo #12 · Claude needs approval", draws[0].Waiting[0].Label)

		s.Emit(event.SessionsUpdated{})
		synctest.Wait()

		assert.Len(t, r.tray.drawn(), 2, "a sessions change redraws the menu")
		assert.Equal(t, []event.WindowEvent{event.SessionsUpdated{}}, r.page.events())

		require.NoError(t, s.Stop())
	})
}

func TestTheMenuShowsTheInboxWhenTheSessionsCannotBeRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(func(c *desktop.Config) { c.Sessions = &fakeSessions{err: assert.AnError} })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		draws := r.tray.drawn()
		require.Len(t, draws, 1)
		assert.Empty(t, draws[0].Waiting)
		assert.Contains(t, r.logs.String(), "could not read the sessions for the menu bar")

		require.NoError(t, s.Stop())
	})
}

func TestFocusSessionFallsBackToTheItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		focus := &fakeSessions{}
		s := r.shell(func(c *desktop.Config) { c.Sessions = focus })

		s.Start(t.Context())

		s.FocusSession("s1", 7, exampleURL)
		synctest.Wait()

		assert.Equal(t, []string{"s1"}, focus.asked())
		assert.Zero(t, r.window.shown(), "the terminal came forward, not the window")

		focus.focusErr = assert.AnError

		s.FocusSession("s1", 7, exampleURL)
		synctest.Wait()

		assert.Equal(t, 1, r.window.shown())
		assert.Equal(t, []event.WindowEvent{event.RevealItem{ItemID: 7, URL: exampleURL}}, r.page.events())

		require.NoError(t, s.Stop())
	})
}
