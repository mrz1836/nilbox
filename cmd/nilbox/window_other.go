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

// windowOptions returns the shared window settings at the given size; a port adds this
// OS's options here.
func windowOptions(_ bool, size settings.WindowSize) application.WebviewWindowOptions {
	return baseWindowOptions(size)
}
