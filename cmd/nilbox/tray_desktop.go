//go:build desktop

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
)

// trayIcon is the menu bar icon: the mark as a template image, which the system tints to
// suit the menu bar. magex brand:icons renders it from build/trayicon.svg.
//
//go:embed trayicon.png
var trayIcon []byte

// tray is the menu bar icon: the mark, with how many items need the user beside it, and a
// menu that opens the window, lists the agent sessions waiting for the user and then the
// items by list, then Autopilot's group while the user has a routine, offers the app's
// update in a build that updates, refreshes, and quits. A click on a session brings its
// terminal forward. The shell decides when it shows and what it says; its clicks go back
// to the shell.
//
// Every change runs on the main thread, in the order it was asked for, so its fields
// need no lock, and the shell never waits for the main thread, which its shutdown can
// hold. A change the main thread reaches once the app is shutting down is dropped. A
// build from source's inspector adds its item above Quit (devKit.menu); a release's
// adds nothing.
type tray struct {
	ui  *desktopUI
	dev *devKit

	item *application.SystemTray
	menu *application.Menu

	// last is the summary the menu was last filled from, once drawn, for Redraw.
	last  menubar.Summary
	drawn bool
}

// Show puts the icon in the menu bar, if it is not there.
func (t *tray) Show() {
	t.onMain(func(app *application.App) {
		if t.item != nil {
			return
		}

		t.menu = application.NewMenu()
		t.item = app.SystemTray.New()
		t.item.SetTemplateIcon(trayIcon)
		t.item.SetMenu(t.menu)
	})
}

// Hide takes the icon out of the menu bar.
func (t *tray) Hide() {
	t.onMain(func(*application.App) {
		if t.item == nil {
			return
		}

		t.item.Destroy()
		t.item, t.menu = nil, nil
	})
}

// Draw sets the icon's count and fills its menu from sum.
func (t *tray) Draw(sum menubar.Summary) {
	t.onMain(func(*application.App) {
		if t.item == nil {
			return
		}

		t.build(sum)
		t.item.SetLabel(sum.Label)
	})
}

// Redraw fills the menu again from the summary it was last filled from, once it has been.
func (t *tray) Redraw() {
	t.onMain(func(*application.App) {
		if t.item != nil && t.drawn {
			t.build(t.last)
		}
	})
}

// onMain runs fn on the main thread, unless the app is shutting down by then.
func (t *tray) onMain(fn func(app *application.App)) {
	application.InvokeAsync(func() {
		app := t.ui.app.Load()
		if app == nil || t.ui.shell.Stopping() {
			return
		}

		fn(app)
	})
}

// build fills the menu from a summary.
func (t *tray) build(sum menubar.Summary) {
	shell := t.ui.shell
	m := t.menu
	m.Clear()

	t.last, t.drawn = sum, true

	m.Add("Open " + brand.Name).OnClick(func(*application.Context) { shell.ShowWindow() })
	m.AddSeparator()

	addWaiting(m, sum.Waiting, sum.WaitingMore, shell)

	if len(sum.Sections) == 0 && len(sum.Waiting) == 0 {
		m.Add(sum.Nothing).SetEnabled(false)
		m.AddSeparator()
	}

	for _, section := range sum.Sections {
		addSectionHeader(m, section.Title)

		for _, e := range section.Entries {
			reveal := event.RevealItem{ItemID: e.ItemID}
			m.Add(e.Label).SetTooltip(e.Tooltip).OnClick(func(*application.Context) { shell.Emit(reveal) })
		}

		if section.More > 0 {
			m.Add(menubar.MoreLabel(section.More)).OnClick(func(*application.Context) { shell.ShowWindow() })
		}

		m.AddSeparator()
	}

	addAutopilot(m, sum.Autopilot, shell)
	addUpdates(m, sum.Updates, shell)

	m.Add("Refresh Now").OnClick(func(*application.Context) { shell.RefreshNow() })

	status := m.Add(sum.Status).SetEnabled(false)
	if sum.Failed {
		status.SetTooltip(sum.Error)
	}

	t.dev.menu(m, shell.ShowWindow)

	m.AddSeparator()
	m.Add("Quit " + brand.Name).SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) { shell.Quit() })

	m.Update()
}

// addAutopilot adds Autopilot's group, while the user has a routine: its heading, each
// agent run working, which opens the run, how many more run, which opens the window, the
// matches waiting for the user, which opens Activity, and Pause Autopilot, the menu's
// checkbox row, ticked while paused, then a separator. Each click goes through the shell;
// the checkbox's own tick is drawn again from what was stored at the next redraw.
func addAutopilot(m *application.Menu, group menubar.Autopilot, shell *desktop.Shell) {
	if !group.Shown {
		return
	}

	addSectionHeader(m, menubar.AutopilotTitle)

	for _, w := range group.Working {
		m.Add(w.Label).SetTooltip(w.Tooltip).OnClick(func(*application.Context) {
			shell.AutopilotAction(menubar.AutopilotWatch, w)
		})
	}

	if group.WorkingMore > 0 {
		m.Add(menubar.MoreLabel(group.WorkingMore)).OnClick(func(*application.Context) { shell.ShowWindow() })
	}

	if group.NeedsYou != "" {
		m.Add(group.NeedsYou).OnClick(func(*application.Context) {
			shell.AutopilotAction(menubar.AutopilotActivity, menubar.Working{})
		})
	}

	m.AddCheckbox(menubar.PauseLabel, group.Paused).OnClick(func(*application.Context) {
		shell.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
	})

	m.AddSeparator()
}

// addUpdates adds the update's rows, Check for Updates… and the version ready, then a
// separator: a row with an action does it through the shell when clicked, and one without
// is shown disabled, with its tooltip. A build that doesn't update has none, and adds
// nothing.
func addUpdates(m *application.Menu, updates menubar.Updates, shell *desktop.Shell) {
	if len(updates.Rows) == 0 {
		return
	}

	for _, row := range updates.Rows {
		item := m.Add(row.Label)
		if row.Tooltip != "" {
			item.SetTooltip(row.Tooltip)
		}

		if row.Action == menubar.UpdateNone {
			item.SetEnabled(false)

			continue
		}

		item.OnClick(func(*application.Context) { shell.UpdateAction(row.Action) })
	}

	m.AddSeparator()
}

// addWaiting lists the agent sessions waiting for the user, under their heading, each
// bringing its terminal tab forward when clicked, and then how many more wait, which
// opens the window, as a list's items left out do. It adds nothing when none waits.
func addWaiting(m *application.Menu, waiting []menubar.Waiting, more int, shell *desktop.Shell) {
	if len(waiting) == 0 {
		return
	}

	addSectionHeader(m, menubar.WaitingTitle)

	for _, w := range waiting {
		m.Add(w.Label).SetTooltip(w.Tooltip).OnClick(func(*application.Context) {
			shell.FocusSession(w.SessionID, w.ItemID, w.URL)
		})
	}

	if more > 0 {
		m.Add(menubar.MoreLabel(more)).OnClick(func(*application.Context) { shell.ShowWindow() })
	}

	m.AddSeparator()
}
