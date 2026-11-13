package repoguard_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundary forbids a package to depend on others, directly or through any package it
// imports. Paths are relative to the module.
type boundary struct {
	pkg       string
	forbidden []string
	why       string
}

// boundaries are the package dependencies the app keeps cut. A forbidden path covers
// the packages under it too.
func boundaries() []boundary {
	// services are the app's services that a building block below them never imports,
	// and noService adds the store: what a leaf that serves any of them never imports.
	services := []string{"internal/github", "internal/settings", "internal/inbox", "internal/core"}
	noService := append([]string{"internal/store"}, services...)

	return []boundary{
		{
			pkg:       "internal/settings",
			forbidden: []string{"internal/notify", "internal/inbox", "internal/github", "internal/integrations"},
			why: "settings stores preferences; the services that read them depend on it, never the reverse " +
				"(it writes the integrations' keys through integrationprefs)",
		},
		{
			pkg:       "internal/worktrees",
			forbidden: []string{"internal/inbox", "internal/github", "internal/settings"},
			why:       "worktrees reads its settings and GitHub's answers through its own SettingsReader and GitHub interfaces",
		},
		{
			pkg:       "internal/launcher",
			forbidden: []string{"internal/inbox", "internal/github", "internal/settings"},
			why:       "the launcher reads items, file checks, and settings through its ItemStore, FileChecker, and SettingsReader interfaces",
		},
		{
			pkg: "internal/event",
			forbidden: []string{
				"internal/core", "internal/inbox", "internal/settings", "internal/worktrees", "internal/notify",
				"internal/appinfo", "internal/store",
			},
			why: "every service emits events, so the event types depend on none of them",
		},
		{
			pkg:       "internal/budget",
			forbidden: []string{"internal/core", "internal/inbox", "internal/worktrees", "internal/settings", "internal/store"},
			why:       "the budget hears the GitHub client's readings and the inbox's refreshes, and keeps its state, through interfaces",
		},
		{
			pkg: "internal/sessions",
			forbidden: []string{
				"internal/launcher", "internal/settings", "internal/notify", "internal/inbox", "internal/github",
				"internal/desktop", "internal/core",
			},
			why: "sessions follows the agents through its own Observer, Switch, Focuser, and AttentionSink interfaces, " +
				"and never reaches GitHub",
		},
		{
			pkg:       "internal/desktop",
			forbidden: []string{"internal/core"},
			why:       "the shell reaches the services through the interfaces it declares; the desktop bootstrap wires the core into it",
		},
		{
			pkg: "internal/bump",
			forbidden: []string{
				"internal/github", "internal/inbox", "internal/prompts", "internal/settings", "internal/store",
			},
			why: "bump reads a dependency update from its title alone; prompts and the review sheet call it, never the reverse",
		},
		{
			pkg:       "internal/toolinstall",
			forbidden: append(slices.Clone(noService), "internal/integrations"),
			why:       "toolinstall places a verified release; it keeps no records and speaks no HTTP, and the integrations call it",
		},
		{
			pkg:       "internal/integrationprefs",
			forbidden: noService,
			why:       "integrationprefs declares the integrations' settings keys, which settings writes; it knows no service",
		},
		{
			pkg:       "internal/reporules",
			forbidden: noService,
			why: "reporules declares the pinned and muted repositories' settings keys, which settings writes and the " +
				"inbox and release read; it knows no store or service",
		},
		{
			pkg:       "internal/prefs/prefval",
			forbidden: noService,
			why: "prefval spells the switches and lists that settings and the leaves declaring its keys store; " +
				"it knows no store or service",
		},
		{
			pkg:       "internal/integrations",
			forbidden: append(slices.Clone(services), "internal/launcher", "internal/desktop"),
			why: "the integrations reach the settings through integrationprefs and GitHub through their own Releases, " +
				"which core adapts; a tool's own updater runs through scriptrun, never the reverse",
		},
		{
			pkg:       "internal/clonelock",
			forbidden: noService,
			why:       "clonelock keeps one job at a time on each clone for whatever changes one; it knows no service",
		},
		{
			pkg: "internal/scriptrun",
			forbidden: append(slices.Clone(services),
				"internal/release", "internal/hooks", "internal/scripts", "internal/integrations", "internal/launcher",
				"internal/desktop", "internal/notify"),
			why: "the runner opens the terminal through its own TerminalOpener, works a command out again through " +
				"each kind's Rerunner, and tells of a failed run through its Config's Failed, which core hands the " +
				"notifications; the kinds call it, never the reverse",
		},
		{
			pkg:       "internal/release",
			forbidden: append(slices.Clone(services), "internal/launcher", "internal/desktop"),
			why: "release reads GitHub through its own GitHub interface, which core adapts, and the clones through " +
				"gitread; it starts runs through scriptrun, never the reverse",
		},
		{
			pkg:       "internal/hooks",
			forbidden: append(slices.Clone(services), "internal/release", "internal/launcher", "internal/desktop"),
			why: "hooks reads the clones through gitread and its own Clones, go-pre-commit through its ToolSource, " +
				"and its preferences through Memory, which core adapts; it starts runs through scriptrun, never the reverse",
		},
		{
			pkg: "internal/scripts",
			forbidden: append(slices.Clone(services), "internal/release", "internal/hooks", "internal/launcher",
				"internal/desktop"),
			why: "scripts reads the clones through its own Clones and Git, magex through its ToolSource, and the " +
				"settings through Settings, which core adapts; it starts runs through scriptrun, never the reverse",
		},
		{
			pkg:       "internal/shellenv",
			forbidden: noService,
			why:       "shellenv reads the login shell's environment for whatever runs tools; it knows no service",
		},
		{
			pkg:       "internal/gitread",
			forbidden: noService,
			why:       "gitread runs the read-only git commands the release and hooks services ask of a clone; it knows no service",
		},
		{
			pkg: "internal/review",
			forbidden: []string{
				"internal/inbox", "internal/settings", "internal/prompts", "internal/launcher", "internal/worktrees",
				"internal/sessions", "internal/core", "internal/desktop",
			},
			why: "the review sheet reaches the inbox, the settings, and the replies through its own Inbox, Settings, " +
				"and Replies interfaces",
		},
	}
}

// buildOSes are the systems whose builds the boundaries hold for: the one the app ships
// on and the one CI runs on. Files for one system can import what the other's do not.
func buildOSes() []string {
	return []string{"darwin", "linux"}
}

// listedPackage is one package as go list describes it.
type listedPackage struct {
	ImportPath string   `json:"ImportPath"`
	Deps       []string `json:"Deps"`
}

// packageDeps returns every package each of pkgs depends on, directly or not, when built
// for goos, keyed by import path.
func packageDeps(t *testing.T, root, goos string, pkgs []string) map[string][]string {
	t.Helper()

	args := append([]string{"list", "-json=ImportPath,Deps"}, pkgs...)

	list := exec.CommandContext(t.Context(), "go", args...) //nolint:gosec // argv only, no shell
	list.Dir = root

	list.Env = append(os.Environ(), "GOOS="+goos)

	out, err := list.Output()
	require.NoError(t, err, "go list %s for %s", strings.Join(pkgs, " "), goos)

	deps := make(map[string][]string, len(pkgs))
	dec := json.NewDecoder(bytes.NewReader(out))

	for {
		var p listedPackage

		err = dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		deps[p.ImportPath] = p.Deps
	}

	return deps
}

// dependsOn returns the packages in deps that are forbidden or under it.
func dependsOn(deps []string, forbidden string) []string {
	var found []string

	for _, dep := range deps {
		if dep == forbidden || strings.HasPrefix(dep, forbidden+"/") {
			found = append(found, dep)
		}
	}

	return found
}

// crossings returns "pkg depends on dep (why)" for every forbidden dependency of the
// rule's package, given deps keyed by import path.
func crossings(t *testing.T, module string, r boundary, deps map[string][]string) []string {
	t.Helper()

	pkgDeps, ok := deps[module+"/"+r.pkg]
	require.True(t, ok, "%s is gone; update this guard", r.pkg)

	var found []string

	for _, f := range r.forbidden {
		for _, dep := range dependsOn(pkgDeps, module+"/"+f) {
			found = append(found, r.pkg+" depends on "+strings.TrimPrefix(dep, module+"/")+" ("+r.why+")")
		}
	}

	return found
}

func TestPackageBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not available")
	}

	root := moduleRoot(t)
	module := modulePath(t, root)
	rules := boundaries()

	pkgs := make([]string, 0, len(rules))
	for _, r := range rules {
		pkgs = append(pkgs, module+"/"+r.pkg)
	}

	var found []string

	for _, goos := range buildOSes() {
		deps := packageDeps(t, root, goos, pkgs)

		for _, r := range rules {
			for _, c := range crossings(t, module, r, deps) {
				found = append(found, goos+": "+c)
			}
		}
	}

	assert.Empty(t, found, "keep these package dependencies cut; reach the other package through an interface "+
		"declared where it is used, and adapt it in internal/core")
}

func TestDependsOn(t *testing.T) {
	t.Parallel()

	deps := []string{
		"example.com/m/internal/inbox", "example.com/m/internal/inbox/internal/queries",
		"example.com/m/internal/inboxtools", "example.com/m/internal/github",
	}

	assert.Equal(t, []string{"example.com/m/internal/inbox", "example.com/m/internal/inbox/internal/queries"},
		dependsOn(deps, "example.com/m/internal/inbox"))
	assert.Equal(t, []string{"example.com/m/internal/github"}, dependsOn(deps, "example.com/m/internal/github"))
	assert.Empty(t, dependsOn(deps, "example.com/m/internal/settings"))
}
