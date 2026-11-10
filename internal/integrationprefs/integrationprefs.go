// Package integrationprefs declares the integrations' preference keys and decodes and
// encodes their values: whether each integration is on, off, or automatic, which copy of
// a tool to use, and where the runs go. The settings package writes them and the integrations package reads
// them. It imports nothing of the app's, so settings never depends on the integrations
// package, nor through it on the GitHub client.
package integrationprefs

import (
	"errors"
	"path/filepath"
	"slices"
)

const (
	// keyPrefix starts every integration's keys.
	keyPrefix = "integrations."

	// KeyRunsIn is the preference key holding where the runs go: "app", or "terminal".
	KeyRunsIn = keyPrefix + "runsIn"
)

// ID names an integration: a tool the app finds and runs, or the scripts.
type ID string

// The integrations.
const (
	// MageX is mage-x, whose magex releases the user's repositories.
	MageX ID = "mage-x"

	// GoPreCommit is go-pre-commit, which runs the pre-commit checks.
	GoPreCommit ID = "go-pre-commit"

	// Scripts is the user's and the repositories' own scripts.
	Scripts ID = "scripts"
)

// Switch is how the user set an integration: automatic, on, or off.
type Switch string

// The switch's states.
const (
	// SwitchAuto is on while the integration can be used: a tool is found and runs. It
	// is every integration's state until the user flips its switch.
	SwitchAuto Switch = "auto"

	// SwitchOn is on, whatever is found.
	SwitchOn Switch = "on"

	// SwitchOff is off, whatever is found.
	SwitchOff Switch = "off"
)

// RunsIn is where the runs go: releases, hooks actions, checks, and scripts.
type RunsIn string

// The places a run goes.
const (
	// RunsInApp runs in the app, which shows the run's progress and its whole log. It is
	// the default.
	RunsInApp RunsIn = "app"

	// RunsInTerminal runs in the terminal chosen for launches, where a run can ask for a
	// passphrase; the app follows it.
	RunsInTerminal RunsIn = "terminal"
)

var (
	// ErrUnknownIntegration is returned for an ID that is not one of IDs.
	ErrUnknownIntegration = errors.New("unknown integration")

	// ErrNotATool is returned for an integration that has no copy to choose: the scripts.
	ErrNotATool = errors.New("this integration has no tool to choose a copy of")

	// ErrInvalidSwitch is returned for a Switch that is not one of its states.
	ErrInvalidSwitch = errors.New("unknown switch state")

	// ErrRelativePath is returned for a chosen copy whose path is not absolute.
	ErrRelativePath = errors.New("the path is not absolute")

	// ErrInvalidRunsIn is returned for a RunsIn that is not one of its places.
	ErrInvalidRunsIn = errors.New("unknown place to run")
)

// Config is how the user set the integrations up, as the settings show it. Every
// integration has an entry in Switches, and every tool one in Paths.
type Config struct {
	// Switches is each integration's switch.
	Switches map[ID]Switch `json:"switches"`

	// Paths is the copy of each tool the user chose, or "" for the one their shell finds.
	Paths map[ID]string `json:"paths"`

	// RunsIn is where the runs go.
	RunsIn RunsIn `json:"runsIn"`
}

// IDs returns the integrations, in the order the settings list them.
func IDs() []ID {
	return []ID{MageX, GoPreCommit, Scripts}
}

// Tools returns the integrations that are a tool the app finds and runs.
func Tools() []ID {
	return []ID{MageX, GoPreCommit}
}

// Valid reports whether id is one of IDs.
func (id ID) Valid() bool {
	return slices.Contains(IDs(), id)
}

// Tool reports whether id is one of Tools.
func (id ID) Tool() bool {
	return slices.Contains(Tools(), id)
}

// Valid reports whether s is one of the switch's states.
func (s Switch) Valid() bool {
	return s == SwitchAuto || s == SwitchOn || s == SwitchOff
}

// KeyEnabled is the preference key holding id's switch: "" for automatic, "on", or "off".
func KeyEnabled(id ID) string {
	return keyPrefix + string(id) + ".enabled"
}

// KeyPath is the preference key holding the copy of tool id the user chose, an absolute
// path, or "" for the one their shell finds.
func KeyPath(id ID) string {
	return keyPrefix + string(id) + ".path"
}

// Valid reports whether r is one of the places a run goes.
func (r RunsIn) Valid() bool {
	return r == RunsInApp || r == RunsInTerminal
}

// Keys returns every key Decode reads.
func Keys() []string {
	keys := make([]string, 0, len(IDs())+len(Tools())+1)

	for _, id := range IDs() {
		keys = append(keys, KeyEnabled(id))
	}

	for _, id := range Tools() {
		keys = append(keys, KeyPath(id))
	}

	return append(keys, KeyRunsIn)
}

// Decode reads the integrations' settings from the stored preferences. A switch that is
// unset or holds an unknown value is automatic, a path that is not absolute is none, and
// runs go in the app unless the terminal is stored.
func Decode(stored map[string]string) Config {
	out := Config{Switches: map[ID]Switch{}, Paths: map[ID]string{}, RunsIn: DecodeRunsIn(stored[KeyRunsIn])}

	for _, id := range IDs() {
		out.Switches[id] = DecodeSwitch(stored[KeyEnabled(id)])
	}

	for _, id := range Tools() {
		if path := stored[KeyPath(id)]; filepath.IsAbs(path) {
			out.Paths[id] = filepath.Clean(path)
		} else {
			out.Paths[id] = ""
		}
	}

	return out
}

// DecodeSwitch reads a stored switch: "on" and "off" as they are, anything else automatic.
func DecodeSwitch(stored string) Switch {
	switch s := Switch(stored); s {
	case SwitchOn, SwitchOff:
		return s
	case SwitchAuto:
		return SwitchAuto
	default:
		return SwitchAuto
	}
}

// EncodeSwitch returns the stored form of s: "" for automatic, so a fresh database and a
// reset switch read the same.
func EncodeSwitch(s Switch) (string, error) {
	switch s {
	case SwitchAuto:
		return "", nil
	case SwitchOn, SwitchOff:
		return string(s), nil
	default:
		return "", ErrInvalidSwitch
	}
}

// EncodePath returns the stored form of a chosen copy's path: cleaned, or "" for none. A
// path that is not absolute is refused.
func EncodePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	if !filepath.IsAbs(path) {
		return "", ErrRelativePath
	}

	return filepath.Clean(path), nil
}

// DecodeRunsIn reads where the runs go: the terminal when it is stored, else the app.
func DecodeRunsIn(stored string) RunsIn {
	if RunsIn(stored) == RunsInTerminal {
		return RunsInTerminal
	}

	return RunsInApp
}

// EncodeRunsIn returns the stored form of where the runs go. A place that is not one of
// RunsIn's is refused.
func EncodeRunsIn(r RunsIn) (string, error) {
	if !r.Valid() {
		return "", ErrInvalidRunsIn
	}

	return string(r), nil
}
