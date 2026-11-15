// Package updateprefs declares the update setting's key and its choices: what the app does
// when a new version is out (update.mode). The settings package writes it, and the update
// package reads it. It is a leaf that imports nothing of the app's, so settings never
// depends on the update package, nor the update package on settings.
//
// The update package keeps its own state under the other update.* keys, which it alone
// writes: none of them is a setting.
package updateprefs

import (
	"errors"
	"slices"
)

// KeyMode is the preference key holding what the app does when a new version is out: one
// of the Mode choices, DefaultMode when unset.
const KeyMode = "update.mode"

// Mode is what the app does when a new version is out.
type Mode string

// The choices, in the order Settings offers them.
const (
	// ModeDownload checks on its own, downloads a new version and checks it, then tells
	// the user it is ready: "Download it and tell me".
	ModeDownload Mode = "download"

	// ModeNotify checks on its own and tells the user a new version is out; Update then
	// downloads and restarts as one click: "Just tell me".
	ModeNotify Mode = "notify"

	// ModeOff makes no timed checks: "Don't check". Check for Updates still works.
	ModeOff Mode = "off"
)

// DefaultMode is the choice of a fresh database.
const DefaultMode = ModeDownload

// ErrUnknownMode is returned for a mode that is not one of the choices.
var ErrUnknownMode = errors.New("unknown update mode")

// Modes returns every choice, in the order Settings offers them.
func Modes() []Mode {
	return []Mode{ModeDownload, ModeNotify, ModeOff}
}

// Valid reports whether m is one of the choices.
func (m Mode) Valid() bool {
	return slices.Contains(Modes(), m)
}

// Checks reports whether m makes timed checks: every choice but ModeOff.
func (m Mode) Checks() bool {
	return m != ModeOff
}

// Decode reads a stored mode: as stored when it is one of the choices, else DefaultMode.
func Decode(stored string) Mode {
	if m := Mode(stored); m.Valid() {
		return m
	}

	return DefaultMode
}
