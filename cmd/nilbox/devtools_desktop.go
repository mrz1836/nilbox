//go:build desktop && devtools

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// devLogSize, devEventSize, devConsoleSize, devTrailSize, and devNoteSize are how many
	// log records, events, console lines, steps of the user's trail, and notes the user
	// left on elements (each with a picture of the window) the inspector keeps.
	devLogSize     = 5000
	devEventSize   = 2000
	devConsoleSize = 2000
	devTrailSize   = 1000
	devNoteSize    = 50
)

// errNoSnapshot is returned when the web view gave no picture back.
var errNoSnapshot = errors.New("the web view took no picture")

// devKit is the inspector a build from source carries (internal/devtools), which only a
// build with the devtools tag compiles in; a release's devKit (nodevtools_desktop.go)
// does nothing. From the moment the app starts it keeps the log, every record from Debug
// up, and the events the services emit; once the window opens, the agent it runs in the
// page on every load keeps the page's console and the user's trail; and it answers the
// magex inspect:* targets on a socket in the data directory.
type devKit struct {
	logs    *devtools.Feed[devtools.LogEntry]
	events  *devtools.Feed[devtools.EventEntry]
	page    *devtools.Page
	started time.Time

	// window is the app's main window, the first it opens, once it has.
	window atomic.Pointer[application.WebviewWindow]

	// cancel stops the server, and done closes once it has stopped; both nil until it
	// starts.
	cancel context.CancelFunc
	done   chan struct{}
}

// newDevKit returns the inspector, keeping nothing yet. Each note the user leaves in the
// page keeps a picture of the main window.
func newDevKit() *devKit {
	k := &devKit{
		logs:    devtools.NewFeed[devtools.LogEntry](devLogSize),
		events:  devtools.NewFeed[devtools.EventEntry](devEventSize),
		page:    devtools.NewPage(devConsoleSize, devTrailSize, devNoteSize, time.Now),
		started: time.Now(),
	}

	k.page.AttachShot(k.shot)

	return k
}

// logger returns logger, its records kept by the inspector too, from Debug up, whatever
// level logger writes at.
func (k *devKit) logger(logger *slog.Logger) *slog.Logger {
	return slog.New(slog.NewMultiHandler(logger.Handler(), devtools.NewLogHandler(k.logs)))
}

// sink returns next, each event kept by the inspector before next hears it.
func (k *devKit) sink(next event.Sink) event.Sink {
	return devtools.Tee(next, k.events, time.Now)
}

// configure has the app hand the inspector each message its page posts, the agent's
// among them; the page posts none other.
func (k *devKit) configure(opts *application.Options) {
	opts.RawMessageHandler = func(_ application.Window, message string, origin *application.OriginInfo) {
		if origin != nil && !origin.IsMainFrame {
			return
		}

		k.page.Receive(message)
	}
}

// start runs the agent in the main window's page on every load, and answers the
// inspector's clients on the socket in the data directory until stop. A socket it can't
// listen on, such as one another running copy answers on, leaves the inspector off, which
// it logs.
func (k *devKit) start(app *application.App, l launch) {
	app.Window.OnCreate(func(w application.Window) {
		window, ok := w.(*application.WebviewWindow)
		if !ok || !k.window.CompareAndSwap(nil, window) {
			return
		}

		k.page.Attach(window.ExecJS)
		window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
			window.ExecJS(k.page.Agent())
		})
	})

	socket := devtools.SocketPath(l.paths.Root)
	ctx, cancel := context.WithCancel(context.Background())

	ln, err := devtools.Listen(ctx, socket)
	if err != nil {
		cancel()
		l.logger.Warn("the inspector is off: it can't listen on its socket", "socket", socket, "error", err)

		return
	}

	server := devtools.NewServer(devtools.Config{
		Logs:     k.logs,
		Events:   k.events,
		Page:     k.page,
		Database: devDatabase{path: l.paths.DB},
		Shot:     k.shot,
		About:    func() devtools.About { return k.about(l, socket) },
	})

	k.cancel, k.done = cancel, make(chan struct{})

	go func() {
		defer close(k.done)

		if serveErr := server.Serve(ctx, ln); serveErr != nil {
			l.logger.Warn("the inspector stopped", "error", serveErr)
		}
	}()

	l.logger.Info("the inspector listens; magex inspect:status talks to it", "socket", socket)
}

// stop stops answering and removes the socket.
func (k *devKit) stop() {
	if k.cancel == nil {
		return
	}

	k.cancel()
	<-k.done
}

// about describes the running app, with its main window once it has opened.
func (k *devKit) about(l launch, socket string) devtools.About {
	about := devtools.About{
		Version: version, Commit: commit, Flavor: buildFlavor(), PID: os.Getpid(), Started: k.started,
		Data: l.paths.Root, Database: l.paths.DB, Logs: l.paths.Logs, Socket: socket,
	}

	if window := k.window.Load(); window != nil {
		bounds := window.Bounds()
		about.Window = devtools.WindowInfo{
			Open: true, Visible: window.IsVisible(), Focused: window.IsFocused(),
			X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height,
		}
	}

	return about
}

// shot takes a picture of the main window as its web view draws it. The web view takes it
// on the main thread, which this waits on from its own; one that takes longer than ctx
// allows is left to finish on its own.
func (k *devKit) shot(ctx context.Context) ([]byte, error) {
	window := k.window.Load()
	if window == nil {
		return nil, devtools.ErrNoWindow
	}

	type picture struct {
		png []byte
		err error
	}

	done := make(chan picture, 1)

	go func() {
		png, err := snapshotWebView(window.NativeWindow())
		done <- picture{png: png, err: err}
	}()

	select {
	case p := <-done:
		return p.png, p.err
	case <-ctx.Done():
		return nil, fmt.Errorf("take a picture of the window: %w", ctx.Err())
	}
}

// devDatabase is the app's database as the inspector reads it, read-only, at path.
type devDatabase struct {
	path string
}

// Query runs one statement that reads.
func (d devDatabase) Query(ctx context.Context, query string, maxRows int) (store.QueryResult, error) {
	return store.ReadOnlyQuery(ctx, d.path, query, maxRows)
}

// Tables lists the tables.
func (d devDatabase) Tables(ctx context.Context) ([]store.TableInfo, error) {
	return store.Tables(ctx, d.path)
}
