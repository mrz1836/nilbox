//go:build unix

package scriptrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// testFocusLink is a focus link as Warp exports it.
const testFocusLink = "warp://session/0123456789abcdef0123456789abcdef"

// errFocus is what the fake terminal says when it can't bring a tab forward.
var errFocus = errors.New("the terminal can't show it")

// fakeTabs records the tabs it was asked to bring forward, as core's terminal does.
type fakeTabs struct {
	mu   sync.Mutex
	err  error
	tabs []TerminalTab
}

func (f *fakeTabs) FocusRun(_ context.Context, tab TerminalTab) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tabs = append(f.tabs, tab)

	return f.err
}

// asked returns the tabs it was asked to bring forward.
func (f *fakeTabs) asked() []TerminalTab {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]TerminalTab(nil), f.tabs...)
}

// tabRig is a harness whose runs in the terminal open in term, which reports it opened
// them in terminal, with tabs to bring them forward and ps answering every shell's tty
// with tty.
type tabRig struct {
	*harness

	term *fakeTerminal
	tabs *fakeTabs
	ps   *testutil.Runner
}

func newTabRig(t *testing.T, terminal, tty string) tabRig {
	t.Helper()

	term := newFakeTerminal(t)
	term.id = terminal
	tabs := &fakeTabs{}
	ps := &testutil.Runner{Stdout: tty}

	h := newHarness(t, func(cfg *Config) { cfg.Terminal, cfg.Tabs, cfg.PS = term, tabs, ps })

	return tabRig{harness: h, term: term, tabs: tabs, ps: ps}
}

// going starts a run in the terminal that goes on until the test ends it with proceed,
// and returns its ID once its shell is recorded.
func (r tabRig) going() (id int64, proceed func()) {
	r.t.Helper()

	path := filepath.Join(r.t.TempDir(), "proceed")
	id = r.start(r.terminalSpec(testutil.FakeRun{Sleep: time.Minute, WaitFor: path}))
	r.waitPID(id)

	return id, func() { testutil.WriteFile(r.t, path, "") }
}

func TestShowInTerminalBringsARunsTabForward(t *testing.T) {
	t.Parallel()

	t.Run("in Warp, by the link its script recorded", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		id, proceed := r.going()
		testutil.WriteFile(t, r.e.files.link(id), testFocusLink+"\n")

		require.NoError(t, r.s.ShowInTerminal(t.Context(), id))

		pid := r.record(id).PID
		assert.Equal(t, []TerminalTab{{Terminal: "warp", TTY: "/dev/ttys004", PID: pid, Link: testFocusLink}},
			r.tabs.asked(), "the terminal it opened in, its shell's tty and PID, and its link, without the newline")
		assert.Equal(t, [][]string{{"/bin/ps", "-o", "tty=", "-p", strconv.Itoa(pid)}}, r.ps.Calls(),
			"the tty of the shell its script recorded")

		proceed()
		r.ended(id)
	})

	t.Run("in iTerm2, by its tty", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "iterm2", "/dev/ttys012\n")
		id, proceed := r.going()

		require.NoError(t, r.s.ShowInTerminal(t.Context(), id))
		assert.Equal(t, []TerminalTab{{Terminal: "iterm2", TTY: "/dev/ttys012", PID: r.record(id).PID}}, r.tabs.asked(),
			"a run whose shell had no link records none")

		proceed()
		r.ended(id)
	})

	t.Run("a run recorded before runs recorded their terminal", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "", "ttys004\n")
		id, proceed := r.going()

		require.NoError(t, r.s.ShowInTerminal(t.Context(), id))

		tabs := r.tabs.asked()
		require.Len(t, tabs, 1)
		assert.Empty(t, tabs[0].Terminal, "no terminal: the one chosen now stands in for it")
		assert.Empty(t, tabs[0].Link, "and no link file")

		proceed()
		r.ended(id)
	})

	t.Run("what the terminal says comes back as it is", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		r.tabs.err = uierr.New("Couldn't tell which Warp tab runs this.", errFocus)
		id, proceed := r.going()

		err := r.s.ShowInTerminal(t.Context(), id)
		require.ErrorIs(t, err, errFocus)
		assert.Equal(t, "Couldn't tell which Warp tab runs this.", uierr.Message(err))

		proceed()
		r.ended(id)
	})
}

func TestShowInTerminalHandsOverWhateverTheLinkFileHolds(t *testing.T) {
	t.Parallel()

	r := newTabRig(t, "warp", "ttys004\n")
	id, proceed := r.going()

	defer func() {
		proceed()
		r.ended(id)
	}()

	link := r.e.files.link(id)
	long := strings.Repeat("x", 4*linkLimit)

	// The terminal's driver checks it is a link (core's words say so when it isn't), never
	// the runner, which only takes the newline its script ends it with.
	for _, tt := range []struct{ name, body, want string }{
		{name: "a link", body: testFocusLink + "\n", want: testFocusLink},
		{name: "no newline", body: testFocusLink, want: testFocusLink},
		{name: "an action", body: "warp://action/new_tab?path=/tmp\n", want: "warp://action/new_tab?path=/tmp"},
		{name: "two lines", body: testFocusLink + "\n" + testFocusLink + "\n", want: testFocusLink + "\n" + testFocusLink},
		{name: "a value far past a link", body: long, want: long[:linkLimit]},
	} {
		testutil.WriteFile(t, link, tt.body)
		require.NoError(t, r.s.ShowInTerminal(t.Context(), id), tt.name)

		tabs := r.tabs.asked()
		assert.Equal(t, tt.want, tabs[len(tabs)-1].Link, tt.name)
	}

	require.NoError(t, os.Remove(link))
	require.NoError(t, syscall.Mkfifo(link, 0o600))
	require.NoError(t, r.s.ShowInTerminal(t.Context(), id), "a pipe in its place is never read")

	tabs := r.tabs.asked()
	assert.Empty(t, tabs[len(tabs)-1].Link)
}

func TestShowInTerminalRefusesWhatHasNoTabToShow(t *testing.T) {
	t.Parallel()

	t.Run("a run that has ended", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		id, proceed := r.going()
		proceed()
		r.ended(id)

		err := r.s.ShowInTerminal(t.Context(), id)
		require.ErrorIs(t, err, ErrNotRunning)
		assert.Equal(t, "The run has ended.", uierr.Message(err))
		assert.Empty(t, r.tabs.asked(), "no terminal is asked")
		assert.Empty(t, r.ps.Calls())
	})

	t.Run("a run in the app", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		id := r.start(r.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
		r.waitReady(id)

		err := r.s.ShowInTerminal(t.Context(), id)
		require.ErrorIs(t, err, ErrNotInTerminal)
		assert.Equal(t, "This run goes in "+brand.Name+", not in your terminal.", uierr.Message(err))
		assert.Empty(t, r.tabs.asked())
	})

	t.Run("a run whose shell isn't recorded yet", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		r.term.nothing = true

		id := r.start(r.terminalSpec(testutil.FakeRun{}))

		err := r.s.ShowInTerminal(t.Context(), id)
		require.ErrorIs(t, err, ErrNoTab)
		assert.Equal(t, "The run's terminal tab isn't known yet. Try again once the run has started.", uierr.Message(err))
		assert.Empty(t, r.tabs.asked())
	})

	t.Run("a shell on no terminal", func(t *testing.T) {
		t.Parallel()

		for name, ps := range map[string]*testutil.Runner{
			"no tty":         {Stdout: "??\n"},
			"ps fails":       {Err: errGone},
			"nothing at all": {},
		} {
			r := newTabRig(t, "iterm2", "")
			r.e.ps = ps
			id, proceed := r.going()

			err := r.s.ShowInTerminal(t.Context(), id)
			require.ErrorIs(t, err, ErrNoTab, name)
			assert.Equal(t, "Couldn't find the run's terminal tab. The run may have just ended.", uierr.Message(err), name)
			assert.Empty(t, r.tabs.asked(), name)

			proceed()
			r.ended(id)
		}
	})

	t.Run("nothing to show a tab with", func(t *testing.T) {
		t.Parallel()

		h := terminalHarness(t, newFakeTerminal(t))
		path := filepath.Join(t.TempDir(), "proceed")
		id := h.start(h.terminalSpec(testutil.FakeRun{Sleep: time.Minute, WaitFor: path}))
		h.waitPID(id)

		err := h.s.ShowInTerminal(t.Context(), id)
		require.ErrorIs(t, err, ErrNoTerminal)
		assert.Equal(t, "Showing a run's terminal tab isn't supported here.", uierr.Message(err))

		testutil.WriteFile(t, path, "")
		h.ended(id)
	})

	t.Run("a run the history no longer holds", func(t *testing.T) {
		t.Parallel()

		r := newTabRig(t, "warp", "ttys004\n")
		require.ErrorIs(t, r.s.ShowInTerminal(t.Context(), 99), ErrNotFound)
	})
}
