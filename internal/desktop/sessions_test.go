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
	"github.com/mrz1836/nilbox/internal/inbox"
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

func (f *fakeSessions) set(waiting ...sessions.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.waiting = waiting
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

		input := approval()
		input.State = sessions.StateNeedsInput
		waiting.set(input)

		s.Emit(event.SessionsUpdated{})
		synctest.Wait()

		draws = r.tray.drawn()
		require.Len(t, draws, 2, "a sessions change redraws the menu")
		assert.Equal(t, "example-repo #12 · Claude needs your input", draws[1].Waiting[0].Label)
		assert.Equal(t, []event.WindowEvent{event.SessionsUpdated{}}, r.page.events())

		require.NoError(t, s.Stop())
	})
}

func TestASessionsChangeRedrawsOnlyWhatTheMenuShows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		first := approval()
		waiting := &fakeSessions{waiting: []sessions.Session{first}}
		s := r.shell(func(c *desktop.Config) { c.Sessions = waiting })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		require.Len(t, r.tray.drawn(), 1)

		reads := r.inbox.readCount()

		// The agent said something new: nothing the menu shows changed.
		talked := first
		talked.LastMessage, talked.Detail = "Running the tests", "Bash"
		waiting.set(talked)
		s.Emit(event.SessionsUpdated{})
		synctest.Wait()

		assert.Len(t, r.tray.drawn(), 1, "the menu is left as it is")
		assert.Equal(t, reads, r.inbox.readCount(), "and the inbox is not read")
		assert.Len(t, r.page.events(), 1, "the window still hears of it")

		steps := []struct {
			name    string
			waiting []sessions.Session
		}{
			{"its item's title changed", []sessions.Session{withTitle(first, "Fix the widget")}},
			{"another session waits", []sessions.Session{withTitle(first, "Fix the widget"), second()}},
			{"they swapped places", []sessions.Session{second(), withTitle(first, "Fix the widget")}},
			{"one stopped waiting", []sessions.Session{second()}},
			{"none waits", nil},
		}

		for i, step := range steps {
			waiting.set(step.waiting...)
			s.Emit(event.SessionsUpdated{})
			synctest.Wait()

			draws := r.tray.drawn()
			require.Len(t, draws, i+2, step.name)
			assert.Len(t, draws[i+1].Waiting, len(step.waiting), step.name)
		}

		// An inbox change still redraws everything, the sessions as they were.
		s.Emit(event.InboxUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), len(steps)+2, "an inbox change redraws the menu")

		require.NoError(t, s.Stop())
	})
}

func TestASessionsChangeRedrawsAMenuThatCouldNotBeDrawn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(inbox.Snapshot{}, assert.AnError)

		waiting := &fakeSessions{waiting: []sessions.Session{approval()}}
		s := r.shell(func(c *desktop.Config) { c.Sessions = waiting })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		assert.Empty(t, r.tray.drawn())

		r.inbox.set(inbox.Snapshot{}, nil)
		s.Emit(event.SessionsUpdated{})
		synctest.Wait()

		draws := r.tray.drawn()
		require.Len(t, draws, 1, "the same sessions, but the menu was never drawn")
		assert.Len(t, draws[0].Waiting, 1)

		require.NoError(t, s.Stop())
	})
}

// second is a Codex session on other-repo#3 that needs input.
func second() sessions.Session {
	return sessions.Session{
		ID: "s2", Agent: agent.Codex, ItemID: 8, Repo: "acme-org/other-repo", Number: 3, State: sessions.StateNeedsInput,
	}
}

// withTitle returns s with its item's title set.
func withTitle(s sessions.Session, title string) sessions.Session {
	s.Title = title

	return s
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
