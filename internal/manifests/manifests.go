// Package manifests is the Deps page's service: every clone in the repository locations
// whose origin is on GitHub, muted ones left out, each with its Go modules' go.mod and its
// JavaScript packages' package.json files and how long ago their dependencies last
// changed, stalest first.
//
// A clone is read at origin's default branch as it last fetched it
// (refs/remotes/origin/<default>), the default branch worked out by the rule Repo scripts
// shares (gitread.DefaultBranch: the one the app stored, else the one the clone's
// refs/remotes/origin/HEAD names, else origin's main or master), or at HEAD when the clone
// has none of them, which its row says. One `git ls-tree` of that commit finds every
// go.mod and package.json outside a vendor, node_modules, or testdata folder, up to
// maxModules, each one's lockfile (a go.mod's go.sum beside it, a package.json's nearest
// pnpm, npm, Yarn, or Bun lockfile in its folder or one above), the workspace files, and
// magex's signs (toolsigns). A module's age is the newest commit, merges left out, that
// changed its manifest or its lockfile, one `git log -1` each, and its manifest, read
// with `git cat-file` when it is at most a mebibyte, gives its module path, Go version,
// and toolchain, or its name, package manager, and workspaces, and how many dependencies
// it names. Everything is read from the clone's own objects, never its worktree or a
// package registry, through gitread's allowlist, so reading a clone runs no program it
// names and reaches no network.
//
// What a clone's read found is kept in memory, by its folder and the commit read, so
// reading an unchanged clone again costs one rev-parse; the page is kept FreshFor, while
// the repository locations and the pinned and muted repositories stay as they were. A
// read of the clones runs apart from the caller that started it, within ScanTimeout, and
// Scans holds it to the app's lifetime.
package manifests

import (
	"errors"
	"time"
)

const (
	// FreshFor is how long a read of the clones is served before List reads them again.
	FreshFor = 5 * time.Minute

	// ScanTimeout bounds a read of the clones the page's callers share. A read runs apart
	// from the caller that started it, so a window that asks again or closes doesn't throw
	// away a read half done: the caller stops waiting, and the read goes on and is kept for
	// the next one. The app's quit ends it sooner (Scans).
	ScanTimeout = 5 * time.Minute

	// cloneTimeout bounds the read of one clone, its origin's included, so one on a disk
	// that stopped answering doesn't hold up the rest. A clone whose time runs out keeps the
	// modules it read; the others say they weren't.
	cloneTimeout = 60 * time.Second

	// scanParallel is how many clones are read at once, and moduleParallel how many of one
	// clone's modules: a large monorepo's hundred histories then fit its time.
	scanParallel   = 4
	moduleParallel = 4

	// maxModules is how many of a clone's modules are read, the shallowest first; a clone
	// with more is marked Truncated.
	maxModules = 200

	// maxManifest is the largest manifest read, in bytes, by its size in the tree.
	maxManifest = 1 << 20

	// maxSubject is the longest commit subject a module shows, in runes.
	maxSubject = 200
)

var (
	// ErrNoClone is returned for a folder that isn't a clone in the repository locations.
	ErrNoClone = errors.New("not a clone in the repository locations")

	// ErrInvalidRequest is returned for a file that isn't a clone's manifest or lockfile.
	ErrInvalidRequest = errors.New("invalid deps request")

	// ErrNoTerminal is returned where the user's terminal can't be opened in a clone.
	ErrNoTerminal = errors.New("the terminal can't be opened here")

	// ErrNoFinder is returned where Finder can't show a file, as on Linux.
	ErrNoFinder = errors.New("finder can't show a file here")

	// errQuitting refuses a call of the page, or a read, once the app is quitting.
	errQuitting = errors.New("the app is quitting")
)

// Kind is what a module is: a Go module, by its go.mod, or a JavaScript package, by its
// package.json.
type Kind string

// The kinds of module.
const (
	KindGo Kind = "go"
	KindJS Kind = "js"
)

// Tool is a tool a clone is set up for, as Deps names it: magex, or a JavaScript package
// manager. ToolNone is a package whose manager isn't known.
type Tool string

// The tools, in the order a clone's are listed.
const (
	ToolNone  Tool = ""
	ToolMagex Tool = "magex"
	ToolPNPM  Tool = "pnpm"
	ToolNPM   Tool = "npm"
	ToolYarn  Tool = "yarn"
	ToolBun   Tool = "bun"
)

// tools returns every tool but ToolNone, in the order a clone's are listed.
func tools() []Tool {
	return []Tool{ToolMagex, ToolPNPM, ToolNPM, ToolYarn, ToolBun}
}

// Page is the Deps page: every clone read that holds a module or couldn't be read,
// stalest first, and how many were left out.
type Page struct {
	// Clones are the clones read that hold a go.mod or a package.json at the commit read,
	// and those that couldn't be read, stalest first, those whose age isn't known last.
	Clones []Clone `json:"clones"`

	// Muted counts the clones of a repository the user muted, itself or its org;
	// WithoutGitHub those whose origin isn't on GitHub; and WithoutModules those read that
	// hold no go.mod or package.json. None of them is listed.
	Muted          int `json:"muted"`
	WithoutGitHub  int `json:"withoutGitHub"`
	WithoutModules int `json:"withoutModules"`

	// Modules counts the modules the listed clones hold.
	Modules int `json:"modules"`

	// CheckedAt is when the clones were last read, RFC 3339; Error says why the last read
	// failed while the one before it is shown, "" when it didn't.
	CheckedAt string `json:"checkedAt"`
	Error     string `json:"error"`
}

// Clone is one clone, as the page lists it.
type Clone struct {
	// Repo is its origin's repository, "owner/name", and Dir its folder, with ~, which
	// every request names it by.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// Pinned reports a repository the user pinned, itself or its org; the window lists
	// those first.
	Pinned bool `json:"pinned"`

	// Modules are its go.mod and package.json files, stalest first, those whose age isn't
	// known last.
	Modules []Module `json:"modules"`

	// Oldest is the stalest module's ChangedAt, RFC 3339, or "" when no module's is known;
	// the page is sorted by it.
	Oldest string `json:"oldest"`

	// Tools are the tools it is set up for: magex, by its signs, and the JavaScript
	// package managers its packages use, in the order tools lists them.
	Tools []Tool `json:"tools"`

	// Workspaces are its go.work and pnpm-workspace.yaml files, by path.
	Workspaces []string `json:"workspaces"`

	// Tip is what was read, as in "origin/main" or "HEAD", and Commit the commit it named.
	// AtHead reports a clone read at HEAD rather than at origin's default branch, and
	// AtHeadWhy says why, "" when it wasn't.
	Tip       string `json:"tip"`
	Commit    string `json:"commit"`
	AtHead    bool   `json:"atHead"`
	AtHeadWhy string `json:"atHeadWhy"`

	// DefaultBranch is the repository's default branch as the app knows it, "" when it
	// knows none.
	DefaultBranch string `json:"defaultBranch"`

	// FetchedAt is when the clone last fetched, RFC 3339, "" when it never did or that
	// can't be read; Shallow reports a shallow clone, whose dates stop at its shallow
	// commits; and Truncated one with more than maxModules modules, of which only the
	// shallowest were read.
	FetchedAt string `json:"fetchedAt"`
	Shallow   bool   `json:"shallow"`
	Truncated bool   `json:"truncated"`

	// Error says why the clone couldn't be read, "" when it could.
	Error string `json:"error"`

	// oldest is Oldest as a time, for sorting.
	oldest time.Time
}

// Module is a Go module or a JavaScript package in a clone.
type Module struct {
	Kind Kind `json:"kind"`

	// Path is the manifest's path in the clone, as in "tools/go.mod", and Lockfile its
	// lockfile's, "" when it has none.
	Path     string `json:"path"`
	Lockfile string `json:"lockfile"`

	// ChangedAt is when the newest commit to change the manifest or its lockfile was
	// made, RFC 3339, and Subject that commit's subject; both "" when that isn't known.
	ChangedAt string `json:"changedAt"`
	Subject   string `json:"subject"`

	// GoModule, GoVersion, and Toolchain are a Go module's module path, go directive, and
	// toolchain, and Direct and Indirect how many modules it requires directly and
	// indirectly (// indirect).
	GoModule  string `json:"goModule"`
	GoVersion string `json:"goVersion"`
	Toolchain string `json:"toolchain"`
	Direct    int    `json:"direct"`
	Indirect  int    `json:"indirect"`

	// Name is a JavaScript package's name; PackageManager the manager its packageManager
	// field names, else its lockfile's, else ToolNone; Workspaces its workspaces'
	// patterns; and Deps, DevDeps, PeerDeps, and OptionalDeps how many dependencies of
	// each kind it names.
	Name           string   `json:"name"`
	PackageManager Tool     `json:"packageManager"`
	Workspaces     []string `json:"workspaces"`
	Deps           int      `json:"deps"`
	DevDeps        int      `json:"devDeps"`
	PeerDeps       int      `json:"peerDeps"`
	OptionalDeps   int      `json:"optionalDeps"`

	// Problem says why the manifest, or when it last changed, couldn't be read, as one
	// too large to read or one that doesn't parse, "" when nothing went wrong.
	Problem string `json:"problem"`

	// changed is ChangedAt as a time, for sorting.
	changed time.Time
}
