//go:build desktop && darwin

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/brand"
)

// macAppOptions quits the app when its last window closes.
func macAppOptions() application.MacOptions {
	return application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true}
}

// windowOptions returns the macOS window: an inset, hidden title bar and a native
// background in the brand palette for the system appearance, so the frame never
// flashes a different color before the page paints.
//
// The content extends beneath the window controls, where the frontend's toolbar sits.
// The toolbar marks itself as the drag region with the --wails-draggable CSS property
// and its buttons opt out, so the bar drags and zooms the window while its controls take
// clicks. A native InvisibleTitleBarHeight is not set: it would turn every click in that
// strip into a window drag, buttons included.
func windowOptions(dark bool) application.WebviewWindowOptions {
	opts := baseWindowOptions()
	opts.BackgroundColour = backgroundColour(dark)
	opts.Mac = application.MacWindow{TitleBar: application.MacTitleBarHiddenInset}

	return opts
}

// backgroundColour is the palette's window background for the appearance.
func backgroundColour(dark bool) application.RGBA {
	palette := brand.Light
	if dark {
		palette = brand.Dark
	}

	red, green, blue, err := brand.RGB(palette.Background)
	if err != nil {
		// Unreachable: the brand tests validate every palette color.
		return application.NewRGB(0, 0, 0)
	}

	return application.NewRGB(red, green, blue)
}
