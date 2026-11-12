package hooks

import (
	"path/filepath"
	"slices"

	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

// ToolFacts is what Hooks knows of go-pre-commit: the copy in use, what it can do
// (integrations.CopyFacts), and where the user's shell finds copies of it.
type ToolFacts struct {
	integrations.CopyFacts

	// Copies are every copy found, the one in use first, and OnPath reports that the
	// user's shell finds one of them on its PATH.
	Copies []string
	OnPath bool

	// ShellRead reports that the user's login shell's environment was read, so OnPath
	// says what their shell finds.
	ShellRead bool
}

// ToolFactsOf reads go-pre-commit's facts from the integrations' view of it: def is its
// definition, tool what the settings show of it, and shellRead whether the user's shell's
// environment was read.
func ToolFactsOf(def integrations.Definition, tool integrations.Tool, shellRead bool) ToolFacts {
	out := ToolFacts{CopyFacts: integrations.CopyFactsOf(def, tool), Copies: []string{}, ShellRead: shellRead}

	for _, c := range append([]integrations.Copy{tool.Copy}, tool.Others...) {
		if c.Path == "" {
			continue
		}

		out.Copies = append(out.Copies, c.Path)
		out.OnPath = out.OnPath || c.OnPath
	}

	return out
}

// ToolStatus is go-pre-commit as the page heads it.
type ToolStatus struct {
	// Version is the copy in use's version, and Path where it is, with ~; both are ""
	// without one.
	Version string `json:"version"`
	Path    string `json:"path"`

	// State is what the settings say of it, Off that the user turned it off, and Usable
	// that the page can run it.
	State  integrations.State `json:"state"`
	Off    bool               `json:"off"`
	Usable bool               `json:"usable"`
}

// status returns what the page shows of the copy in use.
func (t ToolFacts) status(home string) ToolStatus {
	out := ToolStatus{Version: t.Version, State: t.State, Off: t.Off, Usable: t.Usable()}
	if t.Path != "" {
		out.Path = paths.Tilde(t.Path, home)
	}

	return out
}

// PathWarning is the page's warning that the hook can't find go-pre-commit, so commits
// fail: the hook looks on the PATH of the shell git runs it from, and in $(go env
// GOPATH)/bin, never in a folder such as ~/.local/bin that isn't on the PATH.
type PathWarning struct {
	// Show reports the warning, and Dir is the folder of the copy in use, with ~, which
	// the user's shell's startup file should put on the PATH.
	Show bool   `json:"show"`
	Dir  string `json:"dir"`
}

// pathWarning works out the warning: shown when there's a copy, the user's shell was read,
// and no copy is on its PATH or in GOPATH's bin, the folders the hook searches.
func pathWarning(t ToolFacts, env []string, home string) PathWarning {
	if t.Path == "" || !t.ShellRead || t.OnPath {
		return PathWarning{}
	}

	bin := goPathBin(env, home)
	if bin != "" && slices.ContainsFunc(t.Copies, func(c string) bool { return paths.Same(filepath.Dir(c), bin) }) {
		return PathWarning{}
	}

	return PathWarning{Show: true, Dir: paths.Tilde(filepath.Dir(t.Path), home)}
}

// goPathBin is the folder the hook's $(go env GOPATH)/bin names: the first of GOPATH's
// folders, or ~/go when GOPATH isn't set; "" when neither is known.
func goPathBin(env []string, home string) string {
	if list := filepath.SplitList(shellenv.Lookup(env, "GOPATH")); len(list) > 0 && filepath.IsAbs(list[0]) {
		return filepath.Join(list[0], "bin")
	}

	if home == "" {
		return ""
	}

	return filepath.Join(home, "go", "bin")
}

// ToolNote is the page's note that the checks install their own tools: gofumpt,
// golangci-lint, and gitleaks, without checking them, until go-pre-commit has
// FeatureVerifiedTools.
type ToolNote struct {
	// Show reports the note, and Open that it shows unfolded, until a checks run has
	// ended in the app.
	Show bool `json:"show"`
	Open bool `json:"open"`
}
