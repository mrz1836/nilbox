//go:build desktop

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
)

// trayIcon is the menu bar icon: the mark as a template image, which the system tints to
// suit the menu bar. magex brand:icons renders it from build/trayicon.svg.
//
//go:embed trayicon.png
var trayIcon []byte

// tray is the menu bar icon: the mark, with how many items need the user beside it, and a
// menu that opens the window, lists the items by list, refreshes, and quits. The shell
// decides when it shows and what it says; its clicks go back to the shell.
//
// Every change runs on the main thread, in the order it was asked for, so its fields
// need no lock, and the shell never waits for the main thread, which its shutdown can
// hold. A change the main thread reaches once the app is shutting down is dropped.
type tray struct {
	ui *desktopUI

	item *application.SystemTray
	menu *application.Menu
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

	m.Add("Open " + brand.Name).OnClick(func(*application.Context) { shell.ShowWindow() })
	m.AddSeparator()

	if len(sum.Sections) == 0 {
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

	m.Add("Refresh Now").OnClick(func(*application.Context) { shell.RefreshNow() })

	status := m.Add(sum.Status).SetEnabled(false)
	if sum.Failed {
		status.SetTooltip(sum.Error)
	}

	m.AddSeparator()
	m.Add("Quit " + brand.Name).SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) { shell.Quit() })

	m.Update()
}
