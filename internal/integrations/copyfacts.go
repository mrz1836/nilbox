package integrations

import (
	"slices"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
)

// CopyFacts is what a page that runs a tool knows of the copy in use: where it is, its
// version, what the settings say of it, and what it can do. Release, Hooks, and Repo
// scripts each embed it in their own ToolFacts, beside what only that page needs.
type CopyFacts struct {
	// Path is the copy in use, absolute, or "" when there is none; Version is its version
	// as printed, without a leading v.
	Path    string
	Version string

	// State is what the settings say of it, and Off reports the user turned the tool off:
	// only their switch says so, since automatic is on whenever the copy can be used, and a
	// copy too old to use is too old, not off.
	State State
	Off   bool

	// Features are the upstream changes this copy has, sorted (Definition.FeaturesOf).
	Features []Feature
}

// CopyFactsOf reads the facts of the copy in use from what the settings show of tool, as
// def, its definition, describes it. Features is never nil.
func CopyFactsOf(def Definition, tool Tool) CopyFacts {
	return CopyFacts{
		Path: tool.Copy.Path, Version: tool.Copy.Version.Raw, State: tool.State,
		Off: tool.Switch == integrationprefs.SwitchOff, Features: def.FeaturesOf(tool.Copy.Version),
	}
}

// Usable reports a copy a page can run: on, found, and new enough.
func (c CopyFacts) Usable() bool {
	return !c.Off && c.Path != "" && c.State.Usable()
}

// Has reports whether the copy in use has feature f; one that can't be used has none.
func (c CopyFacts) Has(f Feature) bool {
	return c.Usable() && slices.Contains(c.Features, f)
}

// Key names the copy in use, for what a page keeps of what it read with it: its path and
// its version.
func (c CopyFacts) Key() string {
	return c.Path + "@" + c.Version
}
