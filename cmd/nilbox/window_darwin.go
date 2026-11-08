//go:build desktop && darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/settings"
)

// macAppOptions quits the app when its last window closes.
func macAppOptions() application.MacOptions {
	return application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true}
}

// windowOptions returns the macOS window, at the given size and on the background of the
// dark or light palette, with an inset, hidden title bar.
//
// The content extends beneath the window controls, where the frontend's toolbar sits.
// The toolbar marks itself as the drag region with the --wails-draggable CSS property
// and its buttons opt out, so the bar drags and zooms the window while its controls take
// clicks. A native InvisibleTitleBarHeight is not set: it would turn every click in that
// strip into a window drag, buttons included.
func windowOptions(dark bool, size settings.WindowSize) application.WebviewWindowOptions {
	opts := baseWindowOptions(dark, size)
	opts.Mac = application.MacWindow{TitleBar: application.MacTitleBarHiddenInset}

	return opts
}

// applyAppearance pins the app's native appearance to a light or dark theme, or lets it
// follow the system, on the main thread. Before the window opens, this is what makes its
// first frame match the theme: the page's prefers-color-scheme already answers for it.
func applyAppearance(theme settings.Theme) {
	application.InvokeSync(func() { setNativeAppearance(theme) })
}

// activateApp brings the app to the front, on the main thread, as a click on its menu bar
// icon or one of its notifications asks.
func activateApp() {
	application.InvokeSync(activateNative)
}

// addSectionHeader heads a run of menu items: a native section header where macOS has them
// (14 and later), and a disabled item, which reads the same way, before.
func addSectionHeader(m *application.Menu, title string) {
	if nativeSectionHeaders() {
		m.AddSectionHeader(title)

		return
	}

	m.Add(title).SetEnabled(false)
}
