package devtools_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errDatabase is the fake database's failure, and errStop a reader's that wants no more.
var (
	errDatabase = errors.New("the database is locked")
	errStop     = errors.New("enough")
)

// database is a fake database: it answers every query with result and records what it
// was asked, or fails with err.
type database struct {
	mu      sync.Mutex
	result  store.QueryResult
	err     error
	queries []string
	rows    []int
}

func (d *database) Query(_ context.Context, query string, maxRows int) (store.QueryResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.queries = append(d.queries, query)
	d.rows = append(d.rows, maxRows)

	return d.result, d.err
}

func (d *database) Tables(context.Context) ([]store.TableInfo, error) {
	return []store.TableInfo{{Name: "prefs", Rows: 3}}, d.err
}

// app is what a test's inspector reads: its feeds, its page and the fake window the page
// runs scripts in, and its fake database.
type app struct {
	cfg    devtools.Config
	window *window
	db     *database
}

// at is the test's start time moved on by seconds.
func at(seconds int) time.Time {
	return fixedNow.Add(time.Duration(seconds) * time.Second)
}

// newApp is an app whose feeds hold a few entries, its log a debug, an info, and an
// error, a second apart, and whose page answers every evaluation with 42.
func newApp() *app {
	logs := devtools.NewFeed[devtools.LogEntry](10)
	logs.Add(devtools.LogEntry{
		Time: at(1), Level: "DEBUG", Message: "fetched", Attrs: []devtools.Attr{{Key: "repo", Value: "acme-org/example-repo"}},
	})
	logs.Add(devtools.LogEntry{Time: at(3), Level: "INFO", Message: "refreshed the inbox"})
	logs.Add(devtools.LogEntry{Time: at(5), Level: "ERROR", Message: "the budget ran out"})

	events := devtools.NewFeed[devtools.EventEntry](10)
	events.Add(devtools.EventEntry{Time: at(2), Name: "inbox:updated", Data: json.RawMessage(`{}`)})
	events.Add(devtools.EventEntry{Time: at(4), Name: "tools:updated"})

	page := devtools.NewPage(10, 10, func() time.Time { return at(6) })
	page.Receive(devtools.Prefix + `{"t":"console","level":"warn","text":"slow render"}`)
	page.Receive(devtools.Prefix + `{"t":"console","level":"error","text":"boom"}`)
	page.Receive(devtools.Prefix + `{"t":"trail","kind":"click","target":"button “Snooze”","where":"card 7"}`)
	page.Receive(devtools.Prefix + `{"t":"hello","url":"wails://wails/","width":1200}`)

	a := &app{db: &database{result: store.QueryResult{Columns: []string{"key"}, Rows: [][]string{{"a"}}}}}
	a.window = openWindow(page, func(string) string { return `"ok":true,"value":42` })
	a.cfg = devtools.Config{
		Logs:     logs,
		Events:   events,
		Page:     page,
		Database: a.db,
		Shot:     func(context.Context) ([]byte, error) { return []byte("png"), nil },
		About:    func() devtools.About { return devtools.About{Version: "v1", PID: 42, Started: fixedNow} },
		Now:      func() time.Time { return at(90) },
	}

	return a
}

// serve starts an inspector over cfg on a socket of its own, which it returns, and stops
// it when the test ends.
func serve(t *testing.T, cfg devtools.Config) string {
	t.Helper()

	socket := devtools.SocketPath(testutil.SocketDir(t))

	ctx, cancel := context.WithCancel(t.Context())

	ln, err := devtools.Listen(ctx, socket)
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() { done <- devtools.NewServer(cfg).Serve(ctx, ln) }()

	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})

	return socket
}

// ask asks the inspector at socket for cmd with args, and returns its answer's lines.
func ask(t *testing.T, socket, cmd string, args map[string]string) ([]devtools.Line, error) {
	t.Helper()

	var lines []devtools.Line

	err := devtools.Ask(t.Context(), socket, devtools.Request{Command: cmd, Args: args}, func(line devtools.Line) error {
		lines = append(lines, line)

		return nil
	})

	return lines, err
}

// valuesOf decodes the value of each line, all of type typ.
func valuesOf[T any](t *testing.T, lines []devtools.Line, typ string) []T {
	t.Helper()

	values := make([]T, 0, len(lines))

	for _, line := range lines {
		require.Equal(t, typ, line.Type)

		var v T
		require.NoError(t, json.Unmarshal(line.Value, &v))

		values = append(values, v)
	}

	return values
}

// messages are the log entries' messages.
func messages(entries []devtools.LogEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Message)
	}

	return out
}

func TestServerFeeds(t *testing.T) {
	t.Parallel()

	socket := serve(t, newApp().cfg)

	logs := func(args map[string]string) []string {
		lines, err := ask(t, socket, "logs", args)
		require.NoError(t, err)

		return messages(valuesOf[devtools.LogEntry](t, lines, devtools.LineLog))
	}

	assert.Equal(t, []string{"fetched", "refreshed the inbox", "the budget ran out"}, logs(nil))
	assert.Equal(t, []string{"refreshed the inbox", "the budget ran out"}, logs(map[string]string{"n": "2"}))
	assert.Equal(t, []string{"the budget ran out"}, logs(map[string]string{"level": "warn"}))
	assert.Equal(t, []string{"fetched"}, logs(map[string]string{"grep": "EXAMPLE-REPO"}), "grep reads the attributes too")

	lines, err := ask(t, socket, "events", map[string]string{"grep": "tools"})
	require.NoError(t, err)
	assert.Equal(t, []devtools.EventEntry{{Time: at(4), Name: "tools:updated"}},
		valuesOf[devtools.EventEntry](t, lines, devtools.LineEvent))

	lines, err = ask(t, socket, "console", map[string]string{"level": "error"})
	require.NoError(t, err)
	assert.Equal(t, []devtools.ConsoleEntry{{Time: at(6), Level: "error", Text: "boom"}},
		valuesOf[devtools.ConsoleEntry](t, lines, devtools.LineConsole))

	lines, err = ask(t, socket, "trail", map[string]string{"grep": "card 7"})
	require.NoError(t, err)
	assert.Len(t, valuesOf[devtools.TrailEntry](t, lines, devtools.LineTrail), 1)

	lines, err = ask(t, socket, "trail", map[string]string{"grep": "nothing like it"})
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestServerFollows(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	got := make(chan devtools.Line, 10)
	done := make(chan error, 1)

	go func() {
		done <- devtools.Ask(ctx, socket, devtools.Request{Command: "logs", Args: map[string]string{"n": "1", "follow": "true"}},
			func(line devtools.Line) error {
				got <- line

				return nil
			})
	}()

	// The last kept record comes first; by then the inspector follows the log.
	first := <-got
	assert.Contains(t, string(first.Value), "the budget ran out")

	a.cfg.Logs.Add(devtools.LogEntry{Time: at(7), Level: "INFO", Message: "a new one"})

	next := <-got
	assert.Contains(t, string(next.Value), "a new one")

	cancel()
	require.NoError(t, <-done, "ending a follow is no error")
}

func TestServerWatches(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	got := make(chan devtools.Line, 10)
	done := make(chan error, 1)

	go func() {
		done <- devtools.Ask(ctx, socket, devtools.Request{Command: "watch", Args: map[string]string{"n": "1"}},
			func(line devtools.Line) error {
				got <- line

				return nil
			})
	}()

	types := make([]string, 0, 5)
	for range 4 {
		types = append(types, (<-got).Type)
	}

	assert.Equal(t, []string{devtools.LineEvent, devtools.LineLog, devtools.LineConsole, devtools.LineTrail}, types,
		"the last of each feed, in the order they happened (the console and the trail in the order they came)")

	a.cfg.Page.Receive(devtools.Prefix + `{"t":"trail","kind":"key","target":"Escape"}`)
	assert.Equal(t, devtools.LineTrail, (<-got).Type, "then each new entry of any feed")

	cancel()
	require.NoError(t, <-done)
}

func TestServerStatus(t *testing.T) {
	t.Parallel()

	lines, err := ask(t, serve(t, newApp().cfg), "status", nil)
	require.NoError(t, err)

	statuses := valuesOf[devtools.Status](t, lines, devtools.LineResult)
	require.Len(t, statuses, 1)

	status := statuses[0]
	assert.Equal(t, "v1", status.Version)
	assert.Equal(t, "1m30s", status.Uptime)
	assert.Positive(t, status.Goroutines)
	assert.Equal(t, "wails://wails/", status.Page.URL)
	assert.Equal(t, map[string]int{"log": 3, "event": 2, "console": 2, "trail": 1}, status.Kept)
}

func TestServerHelp(t *testing.T) {
	t.Parallel()

	lines, err := ask(t, serve(t, newApp().cfg), "help", nil)
	require.NoError(t, err)

	infos := valuesOf[[]devtools.CommandInfo](t, lines, devtools.LineResult)
	require.Len(t, infos, 1)

	names := make([]string, 0, len(infos[0]))
	for _, info := range infos[0] {
		names = append(names, info.Command)
		assert.NotEmpty(t, info.About, info.Command)
	}

	assert.IsIncreasing(t, names)
	assert.Contains(t, names, "sql")
}

func TestServerDatabase(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	lines, err := ask(t, socket, "sql", map[string]string{"q": "SELECT key FROM prefs", "max": "99999"})
	require.NoError(t, err)
	assert.Equal(t, []store.QueryResult{a.db.result}, valuesOf[store.QueryResult](t, lines, devtools.LineTable))
	assert.Equal(t, []string{"SELECT key FROM prefs"}, a.db.queries)
	assert.Equal(t, []int{10000}, a.db.rows, "max is capped")

	lines, err = ask(t, socket, "tables", nil)
	require.NoError(t, err)
	assert.Equal(t, []store.QueryResult{{Columns: []string{"table", "rows"}, Rows: [][]string{{"prefs", "3"}}}},
		valuesOf[store.QueryResult](t, lines, devtools.LineTable))

	_, err = ask(t, socket, "sql", nil)
	require.ErrorIs(t, err, devtools.ErrInspector)
	require.ErrorContains(t, err, "missing argument: q")

	a.db.err = errDatabase

	_, err = ask(t, socket, "sql", map[string]string{"q": "SELECT 1"})
	require.ErrorContains(t, err, errDatabase.Error())

	_, err = ask(t, socket, "tables", nil)
	require.ErrorContains(t, err, errDatabase.Error())
}

func TestServerPage(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	for _, c := range []struct {
		cmd  string
		args map[string]string
		code string
	}{
		{cmd: "js", args: map[string]string{"code": "document.title"}, code: "document.title"},
		{cmd: "dom", args: map[string]string{"sel": "li", "styles": "yes"}, code: `nb.dom("li", {"html":false,"limit":50,"styles":true})`},
		{cmd: "cards", code: "nb.cards()"},
		{cmd: "click", args: map[string]string{"target": "Refresh"}, code: `nb.click("Refresh")`},
	} {
		lines, err := ask(t, socket, c.cmd, c.args)
		require.NoError(t, err, c.cmd)
		assert.Equal(t, []int{42}, valuesOf[int](t, lines, devtools.LineResult), c.cmd)

		_, codes := a.window.scripts()
		assert.Equal(t, c.code, codes[len(codes)-1], c.cmd)
	}

	for _, cmd := range []string{"js", "dom", "click"} {
		_, err := ask(t, socket, cmd, nil)
		require.ErrorIs(t, err, devtools.ErrInspector, cmd)
	}

	lines, err := ask(t, socket, "reload", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"The page is reloading.\n"}, valuesOf[string](t, lines, devtools.LineText))

	ran, _ := a.window.scripts()
	assert.Equal(t, []string{"location.reload()"}, ran)
}

func TestServerPageFailures(t *testing.T) {
	t.Parallel()

	a := newApp()
	a.window = openWindow(a.cfg.Page, func(string) string { return `"ok":false,"error":"ReferenceError: x"` })
	socket := serve(t, a.cfg)

	_, err := ask(t, socket, "js", map[string]string{"code": "x"})
	require.ErrorContains(t, err, "ReferenceError: x")

	closed := newApp()
	closed.cfg.Page = devtools.NewPage(1, 1, now)
	socket = serve(t, closed.cfg)

	_, err = ask(t, socket, "reload", nil)
	require.ErrorContains(t, err, devtools.ErrNoWindow.Error())
}

func TestServerShotAndStacks(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	lines, err := ask(t, socket, "shot", nil)
	require.NoError(t, err)
	assert.Equal(t, []devtools.Shot{{PNG: []byte("png")}}, valuesOf[devtools.Shot](t, lines, devtools.LineShot))

	lines, err = ask(t, socket, "stacks", nil)
	require.NoError(t, err)

	stacks := valuesOf[string](t, lines, devtools.LineText)
	require.Len(t, stacks, 1)
	assert.Contains(t, stacks[0], "goroutine profile")

	failing := newApp()
	failing.cfg.Shot = func(context.Context) ([]byte, error) { return nil, devtools.ErrNoWindow }

	_, err = ask(t, serve(t, failing.cfg), "shot", nil)
	require.ErrorContains(t, err, devtools.ErrNoWindow.Error())
}

func TestServerRefuses(t *testing.T) {
	t.Parallel()

	socket := serve(t, newApp().cfg)

	_, err := ask(t, socket, "rm -rf", nil)
	require.ErrorIs(t, err, devtools.ErrInspector)
	require.ErrorContains(t, err, devtools.ErrUnknownCommand.Error())

	// A request that is not JSON is answered with why, alone.
	var dialer net.Dialer

	conn, err := dialer.DialContext(t.Context(), "unix", socket)
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)

	answer, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(answer, `{"t":"error","v":"read the request: `), answer)
}

func TestServerReadsALongRequest(t *testing.T) {
	t.Parallel()

	a := newApp()
	socket := serve(t, a.cfg)

	code := strings.Repeat("1+", 100000) + "1"

	_, err := ask(t, socket, "js", map[string]string{"code": code})
	require.NoError(t, err)

	_, codes := a.window.scripts()
	assert.Equal(t, code, codes[0])
}

func TestListen(t *testing.T) {
	t.Parallel()

	t.Run("keeps the socket to the user", func(t *testing.T) {
		t.Parallel()

		socket := serve(t, newApp().cfg)

		info, err := os.Stat(socket)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("leaves alone a socket another copy answers on", func(t *testing.T) {
		t.Parallel()

		socket := serve(t, newApp().cfg)

		_, err := devtools.Listen(t.Context(), socket)
		require.ErrorIs(t, err, devtools.ErrInUse)
	})

	t.Run("takes the place of a socket a run left behind", func(t *testing.T) {
		t.Parallel()

		socket := devtools.SocketPath(testutil.SocketDir(t))
		testutil.WriteFile(t, socket, "left behind")

		ln, err := devtools.Listen(t.Context(), socket)
		require.NoError(t, err)
		require.NoError(t, ln.Close())

		assert.NoFileExists(t, socket, "closing the listener removes the socket")
	})

	t.Run("says why it can't listen", func(t *testing.T) {
		t.Parallel()

		_, err := devtools.Listen(t.Context(), devtools.SocketPath(testutil.SocketDir(t)+"/missing"))
		require.ErrorContains(t, err, "listen on")
	})
}

func TestAsk(t *testing.T) {
	t.Parallel()

	t.Run("says when nothing listens", func(t *testing.T) {
		t.Parallel()

		_, err := ask(t, devtools.SocketPath(testutil.SocketDir(t)), "status", nil)
		require.ErrorIs(t, err, devtools.ErrNotRunning)
	})

	t.Run("says when the answer stops before its end", func(t *testing.T) {
		t.Parallel()

		socket := devtools.SocketPath(testutil.SocketDir(t))

		ln, err := devtools.Listen(t.Context(), socket)
		require.NoError(t, err)

		defer func() { _ = ln.Close() }()

		go func() {
			conn, acceptErr := ln.Accept()
			if acceptErr == nil {
				_, _ = conn.Write([]byte(`{"t":"text","v":"half"}` + "\n"))
				_ = conn.Close()
			}
		}()

		lines, err := ask(t, socket, "status", nil)
		require.ErrorIs(t, err, devtools.ErrCutShort)
		assert.Len(t, lines, 1)
	})

	t.Run("stops at the first line its reader refuses", func(t *testing.T) {
		t.Parallel()

		socket := serve(t, newApp().cfg)

		err := devtools.Ask(t.Context(), socket, devtools.Request{Command: "logs"}, func(devtools.Line) error { return errStop })
		require.ErrorIs(t, err, errStop)
	})
}
