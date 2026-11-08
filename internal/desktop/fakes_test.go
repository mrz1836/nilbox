package desktop_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/settings"
)

var (
	errFakeSettings = errors.New("fake: the settings cannot be read")
	errFakeInbox    = errors.New("fake: the inbox cannot be read")
	errFakeClose    = errors.New("fake: the database would not close")
	errFakeBanners  = errors.New("fake: the notification center did not answer")
)

// journal records what the fakes were asked to do, in order. It is safe for concurrent
// use.
type journal struct {
	mu    sync.Mutex
	lines []string
}

func (j *journal) add(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.lines = append(j.lines, fmt.Sprintf(format, args...))
}

func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	return slices.Clone(j.lines)
}

// buffer is a bytes.Buffer safe for concurrent writers.
type buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// fakePage records the window events passed on to the page.
type fakePage struct {
	mu  sync.Mutex
	got []event.WindowEvent
}

func (p *fakePage) Emit(e event.WindowEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.got = append(p.got, e)
}

func (p *fakePage) events() []event.WindowEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.got)
}

// opening is one call to Window.Open.
type opening struct {
	theme settings.Theme
	size  settings.WindowSize
}

// fakeWindow records what the window was asked to do.
type fakeWindow struct {
	mu     sync.Mutex
	opened []opening
	shows  int
	themes []settings.Theme
}

func (w *fakeWindow) Open(theme settings.Theme, size settings.WindowSize) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.opened = append(w.opened, opening{theme: theme, size: size})
}

func (w *fakeWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.shows++
}

func (w *fakeWindow) SetTheme(theme settings.Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.themes = append(w.themes, theme)
}

func (w *fakeWindow) openings() []opening {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.opened)
}

func (w *fakeWindow) shown() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.shows
}

func (w *fakeWindow) themesSet() []settings.Theme {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.themes)
}

// fakeTray records the menu bar icon's calls, in order, in its journal.
type fakeTray struct {
	log *journal

	mu    sync.Mutex
	draws []menubar.Summary
}

func (t *fakeTray) Show() { t.log.add("tray shown") }

func (t *fakeTray) Hide() { t.log.add("tray hidden") }

func (t *fakeTray) Draw(sum menubar.Summary) {
	t.mu.Lock()
	t.draws = append(t.draws, sum)
	t.mu.Unlock()

	t.log.add("tray drawn: %q", sum.Label)
}

func (t *fakeTray) drawn() []menubar.Summary {
	t.mu.Lock()
	defer t.mu.Unlock()

	return slices.Clone(t.draws)
}

// fakeApp counts the requests to quit.
type fakeApp struct {
	mu    sync.Mutex
	quits int
}

func (a *fakeApp) Quit() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.quits++
}

func (a *fakeApp) quitCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.quits
}

// fakeInbox serves a fixed snapshot. While gate is set, each read waits for it to close
// or for its context to end. Refresh records itself and waits for its context to end.
type fakeInbox struct {
	log *journal

	mu    sync.Mutex
	snap  inbox.Snapshot
	err   error
	gate  chan struct{}
	reads int
}

func (f *fakeInbox) Snapshot(ctx context.Context) (inbox.Snapshot, error) {
	f.mu.Lock()
	f.reads++
	gate := f.gate
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return inbox.Snapshot{}, ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.snap, f.err
}

func (f *fakeInbox) Refresh(ctx context.Context) (inbox.Snapshot, error) {
	f.log.add("refresh started")
	<-ctx.Done()
	f.log.add("refresh returned")

	return inbox.Snapshot{}, ctx.Err()
}

func (f *fakeInbox) set(snap inbox.Snapshot, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.snap, f.err = snap, err
}

func (f *fakeInbox) hold(gate chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.gate = gate
}

func (f *fakeInbox) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

// fakeSettings serves fixed settings, or fails every read with err.
type fakeSettings struct {
	theme   settings.Theme
	menuBar bool
	size    settings.WindowSize
	err     error
}

func (f fakeSettings) Theme(context.Context) (settings.Theme, error) {
	if f.err != nil {
		return "", f.err
	}

	return f.theme, nil
}

func (f fakeSettings) MenuBar(context.Context) (bool, error) {
	if f.err != nil {
		return settings.DefaultMenuBar, f.err
	}

	return f.menuBar, nil
}

func (f fakeSettings) WindowSize(context.Context) (settings.WindowSize, error) {
	if f.err != nil {
		return settings.WindowSize{}, f.err
	}

	return f.size, nil
}

// fakeWorker runs until its context ends, then takes linger more and, when release is
// set, waits for it before it returns.
type fakeWorker struct {
	name    string
	log     *journal
	linger  time.Duration
	release chan struct{}
}

func (w *fakeWorker) Run(ctx context.Context) {
	w.log.add("%s started", w.name)
	<-ctx.Done()

	if w.linger > 0 {
		time.Sleep(w.linger)
	}

	if w.release != nil {
		<-w.release
	}

	w.log.add("%s returned", w.name)
}

// fakeData records its closing.
type fakeData struct {
	log *journal
	err error
}

func (d *fakeData) Close() error {
	d.log.add("data closed")

	return d.err
}

// fakeBanners records the requests to allow banners.
type fakeBanners struct {
	log *journal
	err error
}

func (b *fakeBanners) Authorize() (bool, error) {
	b.log.add("banners asked")

	return b.err == nil, b.err
}

// rig is a Shell's fakes. Build it inside the bubble that uses it.
type rig struct {
	log      *journal
	page     *fakePage
	window   *fakeWindow
	tray     *fakeTray
	app      *fakeApp
	inbox    *fakeInbox
	settings fakeSettings
	data     *fakeData
	stdout   *buffer
	stderr   *buffer
}

func newRig() *rig {
	log := &journal{}

	return &rig{
		log:      log,
		page:     &fakePage{},
		window:   &fakeWindow{},
		tray:     &fakeTray{log: log},
		app:      &fakeApp{},
		inbox:    &fakeInbox{log: log},
		settings: fakeSettings{theme: settings.ThemeDark, menuBar: true, size: settings.WindowSize{Width: 1440, Height: 900}},
		data:     &fakeData{log: log},
		stdout:   &buffer{},
		stderr:   &buffer{},
	}
}

// config returns a Config over the rig's fakes.
func (r *rig) config() desktop.Config {
	return desktop.Config{
		Frontend: r.page,
		Window:   r.window,
		Tray:     r.tray,
		App:      r.app,
		Inbox:    r.inbox,
		Settings: r.settings,
		Data:     r.data,
		Stdout:   r.stdout,
		Stderr:   r.stderr,
	}
}

// shell returns a Shell over the rig's fakes, as change adjusts their Config.
func (r *rig) shell(change ...func(*desktop.Config)) *desktop.Shell {
	cfg := r.config()
	for _, c := range change {
		c(&cfg)
	}

	return desktop.New(cfg)
}

// lines returns the journal's lines that start with prefix.
func (r *rig) lines(prefix string) []string {
	var out []string

	for _, line := range r.log.all() {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}

	return out
}
