//go:build desktop && !darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/settings"
)

// macAppOptions has nothing to set off macOS.
func macAppOptions() application.MacOptions {
	return application.MacOptions{}
}

// windowOptions returns the shared window settings at the given size, on the background
// of the dark or light palette; a port adds this OS's options here.
func windowOptions(dark bool, size settings.WindowSize) application.WebviewWindowOptions {
	return baseWindowOptions(dark, size)
}

// applyAppearance has no native appearance to set off macOS; a port sets this OS's here.
// The page still shows the theme, which the frontend applies.
func applyAppearance(string) {}

// activateApp has nothing to do off macOS, where showing the window brings it forward; a
// port activates the app here if its OS needs it.
func activateApp() {}

// addSectionHeader heads a run of menu items with a disabled item.
func addSectionHeader(m *application.Menu, title string) {
	m.Add(title).SetEnabled(false)
}
