// Package integrationprefs declares the integrations' preference keys and decodes and
// encodes their values: whether each integration is on, off, or automatic, which copy of
// a tool to use, where the runs go, whether a merge offers a release, which sources Repo
// scripts lists, whether a run Repo scripts starts shows beside its scripts, and the
// window's memory of the Scripts tab (whether it was announced, the page and clone it
// reopens on, and the sections the user opened or folded). The settings package writes them, and the
// integrations, release, and scripts packages read them. It imports nothing of the app's,
// so settings never depends on the integrations package, nor through it on the GitHub
// client.
package integrationprefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// keyPrefix starts every integration's keys.
	keyPrefix = "integrations."

	// KeyRunsIn is the preference key holding where the runs go: "app", or "terminal".
	KeyRunsIn = keyPrefix + "runsIn"

	// KeyOfferAfterMerge is the preference key holding whether merging a pull request in
	// the review sheet offers to release its repository: "true" or "false", on when unset.
	KeyOfferAfterMerge = keyPrefix + "offerAfterMerge"

	// KeyMagexTargets is the preference key holding whether Repo scripts lists magex's
	// targets: "true" or "false", on when unset.
	KeyMagexTargets = "scripts.magexTargets"

	// KeyRepoFiles is the preference key holding whether Repo scripts lists the commands in
	// a repository's own files, t3.json and .vscode/tasks.json: "true" or "false", on when
	// unset.
	KeyRepoFiles = "scripts.repoFiles"

	// KeyAnnounced is the preference key holding whether the window announced the Scripts
	// tab as it first appeared because a tool was found: "true" or "false", off when unset.
	KeyAnnounced = keyPrefix + "announced"

	// KeyLastPage is the preference key holding the Scripts tab's page shown last, which
	// the tab reopens on: one of ScriptsPage's pages, or none when unset.
	KeyLastPage = "scripts.lastPage"

	// KeyLastRepo is the preference key holding the clone Repo scripts showed last, as the
	// window names it (its folder, with ~ for home), which Repo scripts reopens on: none
	// when unset.
	KeyLastRepo = "scripts.lastRepo"

	// KeySections is the preference key holding which of the Scripts tab's sections the
	// user opened or folded: a JSON object of each ScriptsSection to whether it is open,
	// each section left out at its page's own default.
	KeySections = "scripts.sections"

	// KeyShowRuns is the preference key holding whether a run Repo scripts starts, a
	// script's or a pull's, shows beside its scripts at once: "true" or "false", off when
	// unset.
	KeyShowRuns = "scripts.showRuns"

	// maxRepoLen is the longest clone folder KeyLastRepo holds.
	maxRepoLen = 4096
)

// ScriptsSection is one of the sections the Scripts tab's pages open and fold.
type ScriptsSection string

// The Scripts tab's sections, by page.
const (
	// SectionHooksAttention is Hooks' clones that need attention.
	SectionHooksAttention ScriptsSection = "hooks.attention"

	// SectionHooksReady is Hooks' clones that are ready.
	SectionHooksReady ScriptsSection = "hooks.ready"

	// SectionHooksOff is Hooks' clones whose repository turns go-pre-commit off.
	SectionHooksOff ScriptsSection = "hooks.off"

	// SectionHooksNotSetUp is Hooks' clones without a go-pre-commit config.
	SectionHooksNotSetUp ScriptsSection = "hooks.notSetUp"

	// SectionScriptsYours is Repo scripts' scripts of the user's own.
	SectionScriptsYours ScriptsSection = "scripts.yours"

	// SectionScriptsT3 is Repo scripts' commands from t3.json.
	SectionScriptsT3 ScriptsSection = "scripts.t3"

	// SectionScriptsTasks is Repo scripts' tasks from .vscode/tasks.json.
	SectionScriptsTasks ScriptsSection = "scripts.tasks"

	// SectionScriptsMagex is Repo scripts' magex targets.
	SectionScriptsMagex ScriptsSection = "scripts.magex"
)

// Sections returns every section, in page order.
func Sections() []ScriptsSection {
	return []ScriptsSection{
		SectionHooksAttention, SectionHooksReady, SectionHooksOff, SectionHooksNotSetUp,
		SectionScriptsYours, SectionScriptsT3, SectionScriptsTasks, SectionScriptsMagex,
	}
}

// Valid reports whether s is one of Sections.
func (s ScriptsSection) Valid() bool {
	return slices.Contains(Sections(), s)
}

// ScriptsPage is one of the Scripts tab's pages, which the tab reopens on.
type ScriptsPage string

// The Scripts tab's pages, in the rail's order.
const (
	// PageNone is no page yet: the tab opens on the first page that's on.
	PageNone ScriptsPage = ""

	// PageRelease is Release.
	PageRelease ScriptsPage = "release"

	// PageHooks is Hooks.
	PageHooks ScriptsPage = "hooks"

	// PageScripts is Repo scripts.
	PageScripts ScriptsPage = "scripts"

	// PageRuns is Runs.
	PageRuns ScriptsPage = "runs"
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

	// ErrInvalidPage is returned for a ScriptsPage that is not one of the tab's pages.
	ErrInvalidPage = errors.New("unknown Scripts page")

	// ErrInvalidRepo is returned for a clone folder to reopen Repo scripts on that isn't
	// an absolute or ~ path on one line.
	ErrInvalidRepo = errors.New("invalid clone folder")

	// ErrInvalidSection is returned for a ScriptsSection that is not one of Sections.
	ErrInvalidSection = errors.New("unknown Scripts section")
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

	// OfferAfterMerge reports whether merging a pull request in the review sheet offers to
	// release its repository, when Release can.
	OfferAfterMerge bool `json:"offerAfterMerge"`

	// ScriptSources are the sources Repo scripts lists.
	ScriptSources ScriptSources `json:"scriptSources"`

	// Announced reports that the window announced the Scripts tab once, as it first
	// appeared because a tool was found, so it never does again.
	Announced bool `json:"announced"`

	// LastPage is the Scripts tab's page shown last, and LastRepo the clone Repo scripts
	// showed last, with ~: where each reopens, none until the user picks one.
	LastPage ScriptsPage `json:"lastPage"`
	LastRepo string      `json:"lastRepo"`

	// Sections are the Scripts tab's sections the user opened (true) or folded (false);
	// one left out shows as its page starts it.
	Sections map[ScriptsSection]bool `json:"sections"`

	// ShowRuns reports whether a run Repo scripts starts shows beside its scripts at once;
	// off, the page says the run started, with a way to show it.
	ShowRuns bool `json:"showRuns"`
}

// ScriptSources are the sources Repo scripts lists besides the user's own scripts: magex's
// targets, and the commands in a repository's own files. Both start on.
type ScriptSources struct {
	MagexTargets bool `json:"magexTargets"`
	RepoFiles    bool `json:"repoFiles"`
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
	keys := make([]string, 0, len(IDs())+len(Tools())+4)

	for _, id := range IDs() {
		keys = append(keys, KeyEnabled(id))
	}

	for _, id := range Tools() {
		keys = append(keys, KeyPath(id))
	}

	return append(keys, KeyRunsIn, KeyOfferAfterMerge, KeyMagexTargets, KeyRepoFiles, KeyAnnounced, KeyLastPage,
		KeyLastRepo, KeySections, KeyShowRuns)
}

// Decode reads the integrations' settings from the stored preferences. A switch that is
// unset or holds an unknown value is automatic, a path that is not absolute is none, runs
// go in the app unless the terminal is stored, and a merge offers a release, and Repo
// scripts lists each of its sources, unless that is stored as off. The Scripts tab is
// announced only once that is stored, and a page or clone to reopen on that isn't one
// reads as none.
func Decode(stored map[string]string) Config {
	out := Config{
		Switches: map[ID]Switch{}, Paths: map[ID]string{}, RunsIn: DecodeRunsIn(stored[KeyRunsIn]),
		OfferAfterMerge: DecodeOfferAfterMerge(stored[KeyOfferAfterMerge]),
		ScriptSources: ScriptSources{
			MagexTargets: decodeOn(stored[KeyMagexTargets]), RepoFiles: decodeOn(stored[KeyRepoFiles]),
		},
		Announced: decodeOff(stored[KeyAnnounced]),
		LastPage:  DecodeLastPage(stored[KeyLastPage]),
		LastRepo:  DecodeLastRepo(stored[KeyLastRepo]),
		Sections:  DecodeSections(stored[KeySections]),
		ShowRuns:  decodeOff(stored[KeyShowRuns]),
	}

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

// DecodeOfferAfterMerge reads whether a merge offers a release: as stored, when it reads as
// a boolean, else on, the default.
func DecodeOfferAfterMerge(stored string) bool {
	return decodeOn(stored)
}

// EncodeOfferAfterMerge returns the stored form of whether a merge offers a release.
func EncodeOfferAfterMerge(on bool) string {
	return EncodeOn(on)
}

// EncodeOn returns the stored form of a switch that starts on, such as a Repo scripts
// source's: "true" or "false".
func EncodeOn(on bool) string {
	return strconv.FormatBool(on)
}

// decodeOn reads a switch that starts on: as stored, when it reads as a boolean, else on.
func decodeOn(stored string) bool {
	if on, err := strconv.ParseBool(stored); err == nil {
		return on
	}

	return true
}

// decodeOff reads a switch that starts off: true only when it is stored as true.
func decodeOff(stored string) bool {
	on, err := strconv.ParseBool(stored)

	return err == nil && on
}

// Valid reports whether p is one of the Scripts tab's pages (PageNone is not).
func (p ScriptsPage) Valid() bool {
	return slices.Contains([]ScriptsPage{PageRelease, PageHooks, PageScripts, PageRuns}, p)
}

// DecodeLastPage reads the Scripts tab's page to reopen on: as stored, when it is one of
// the tab's pages, else none.
func DecodeLastPage(stored string) ScriptsPage {
	if p := ScriptsPage(stored); p.Valid() {
		return p
	}

	return PageNone
}

// EncodeLastPage returns the stored form of the Scripts tab's page to reopen on. A page
// that is not one of the tab's is refused.
func EncodeLastPage(p ScriptsPage) (string, error) {
	if !p.Valid() {
		return "", ErrInvalidPage
	}

	return string(p), nil
}

// DecodeSections reads the sections the user opened or folded: each of Sections the stored
// JSON object names with a boolean; any other entry, or a value that isn't an object, is
// dropped.
func DecodeSections(stored string) map[ScriptsSection]bool {
	out := map[ScriptsSection]bool{}

	var read map[string]any
	if stored == "" || json.Unmarshal([]byte(stored), &read) != nil {
		return out
	}

	for name, value := range read {
		if open, ok := value.(bool); ok && ScriptsSection(name).Valid() {
			out[ScriptsSection(name)] = open
		}
	}

	return out
}

// EncodeSections returns the stored form of the sections the user opened or folded: a
// JSON object, "" for none. A section that isn't one of Sections is refused.
func EncodeSections(sections map[ScriptsSection]bool) (string, error) {
	if len(sections) == 0 {
		return "", nil
	}

	for section := range sections {
		if !section.Valid() {
			return "", ErrInvalidSection
		}
	}

	out, err := json.Marshal(sections)
	if err != nil {
		return "", fmt.Errorf("encode the sections: %w", err)
	}

	return string(out), nil
}

// DecodeLastRepo reads the clone Repo scripts reopens on: as stored, when it reads as one
// (validRepo), else none.
func DecodeLastRepo(stored string) string {
	if validRepo(stored) {
		return stored
	}

	return ""
}

// EncodeLastRepo returns the stored form of the clone Repo scripts reopens on: dir as the
// window names it, or "" for none. A folder that isn't an absolute or ~ path on one line,
// with no control character, of at most maxRepoLen bytes, is refused.
func EncodeLastRepo(dir string) (string, error) {
	if dir != "" && !validRepo(dir) {
		return "", ErrInvalidRepo
	}

	return dir, nil
}

// validRepo reports whether dir reads as a clone folder the window names: an absolute or
// ~ path, of at most maxRepoLen bytes, valid UTF-8, with no control character.
func validRepo(dir string) bool {
	if dir == "" || len(dir) > maxRepoLen || !utf8.ValidString(dir) {
		return false
	}

	if !strings.HasPrefix(dir, "/") && !strings.HasPrefix(dir, "~/") {
		return false
	}

	return !strings.ContainsFunc(dir, unicode.IsControl)
}
