//go:build desktop

package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/menubar"
)

// trayIcon is the menu bar icon: the mark as a template image, which the system tints to
// suit the menu bar. magex brand:icons renders it from build/trayicon.svg.
//
//go:embed trayicon.png
var trayIcon []byte

// trayActions is what the menu bar's menu does in the rest of the shell.
type trayActions interface {
	// showWindow brings the window forward.
	showWindow()

	// reveal brings the window forward with an item in view.
	reveal(itemID int64, url string)

	// refreshInbox refreshes the inbox from GitHub now.
	refreshInbox()

	// quit quits the app.
	quit()
}

// tray is the menu bar icon: the mark, with how many items need the user beside it, and a
// menu that opens the window, lists the items by list, refreshes, and quits. It is drawn
// from the stored inbox, again whenever the inbox changes and whenever a snooze ends.
type tray struct {
	app     *application.App
	inbox   *inbox.Service
	actions trayActions
	stderr  io.Writer

	// mu guards the fields below and serializes updates, so menus are built one at a time.
	mu    sync.Mutex
	item  *application.SystemTray
	menu  *application.Menu
	timer *time.Timer
}

// newTray returns a tray that is not shown yet.
func newTray(app *application.App, svc *inbox.Service, actions trayActions, stderr io.Writer) *tray {
	return &tray{app: app, inbox: svc, actions: actions, stderr: stderr}
}

// show puts the icon in the menu bar, if it is not there, and draws it.
func (t *tray) show() {
	t.mu.Lock()

	if t.item == nil {
		t.menu = application.NewMenu()
		t.item = t.app.SystemTray.New()
		t.item.SetTemplateIcon(trayIcon)
		t.item.SetMenu(t.menu)
	}

	t.mu.Unlock()

	t.update()
}

// hide takes the icon out of the menu bar.
func (t *tray) hide() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopTimer()

	if t.item != nil {
		t.item.Destroy()
		t.item, t.menu = nil, nil
	}
}

// update draws the icon's count and menu from the stored inbox, and sets a timer for the
// soonest snooze to end, which changes the count with nothing stored.
func (t *tray) update() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.item == nil {
		return
	}

	ctx := t.app.Context()

	snap, err := t.inbox.Snapshot(ctx)
	if err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintln(t.stderr, "read the inbox for the menu bar:", err)
		}

		return
	}

	now := time.Now()
	sum := menubar.Summarize(snap, now)

	t.build(sum)
	t.item.SetLabel(sum.Label)

	t.stopTimer()

	if !sum.NextChange.IsZero() {
		// A second past the end, so the snooze has ended when the count is taken again.
		t.timer = time.AfterFunc(sum.NextChange.Sub(now)+time.Second, t.update)
	}
}

// build fills the menu from a summary. The caller holds t.mu.
func (t *tray) build(sum menubar.Summary) {
	m := t.menu
	m.Clear()

	m.Add("Open " + brand.Name).OnClick(func(*application.Context) { t.actions.showWindow() })
	m.AddSeparator()

	if len(sum.Sections) == 0 {
		m.Add(sum.Nothing).SetEnabled(false)
		m.AddSeparator()
	}

	for _, section := range sum.Sections {
		addSectionHeader(m, section.Title)

		for _, e := range section.Entries {
			id := e.ItemID
			m.Add(e.Label).SetTooltip(e.Tooltip).OnClick(func(*application.Context) { t.actions.reveal(id, "") })
		}

		if section.More > 0 {
			m.Add(menubar.MoreLabel(section.More)).OnClick(func(*application.Context) { t.actions.showWindow() })
		}

		m.AddSeparator()
	}

	m.Add("Refresh Now").OnClick(func(*application.Context) { t.actions.refreshInbox() })

	status := m.Add(sum.Status).SetEnabled(false)
	if sum.Failed {
		status.SetTooltip(sum.Error)
	}

	m.AddSeparator()
	m.Add("Quit " + brand.Name).SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) { t.actions.quit() })

	m.Update()
}

// stopTimer stops the snooze timer, if one is set. The caller holds t.mu.
func (t *tray) stopTimer() {
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

// refreshInbox refreshes the inbox now, in the background; the refresh tells the window and
// the menu bar when it is stored, and records a failure for both to show.
func (s *shell) refreshInbox() {
	app := s.app.Load()
	if app == nil || s.stopping.Load() {
		return
	}

	go func(ctx context.Context) {
		_, _ = s.core.Inbox.Refresh(ctx)
	}(app.Context())
}

// quit quits the app.
func (s *shell) quit() {
	if app := s.app.Load(); app != nil {
		app.Quit()
	}
}
