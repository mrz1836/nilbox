//go:build desktop && !darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// macAppOptions has nothing to set off macOS.
func macAppOptions() application.MacOptions {
	return application.MacOptions{}
}

// windowOptions returns the shared window settings; a port adds this OS's options here.
func windowOptions(_ bool) application.WebviewWindowOptions {
	return baseWindowOptions()
}
