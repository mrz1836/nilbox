package repoguard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
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
	// inbox and settings are the inbox's and the settings' packages, which many boundaries
	// name.
	const (
		inbox    = "internal/inbox"
		settings = "internal/settings"
	)

	// services are the app's services that a building block below them never imports,
	// and noService adds the store: what a leaf that serves any of them never imports.
	services := []string{"internal/github", settings, inbox, "internal/core"}
	noService := append([]string{"internal/store"}, services...)
	// sheets are what the review sheet and the issue sheet reach only through the
	// interfaces each declares.
	sheets := []string{
		inbox, settings, "internal/prompts", "internal/launcher", "internal/worktrees",
		"internal/sessions", "internal/core", "internal/desktop",
	}

	return []boundary{
		{
			pkg:       settings,
			forbidden: []string{"internal/notify", "internal/inbox", "internal/github", "internal/integrations"},
			why: "settings stores preferences; the services that read them depend on it, never the reverse " +
				"(it writes the integrations' keys through integrationprefs)",
		},
		{
			pkg:       "internal/worktrees",
			forbidden: []string{"internal/inbox", "internal/github", settings},
			why:       "worktrees reads its settings and GitHub's answers through its own SettingsReader and GitHub interfaces",
		},
		{
			pkg:       "internal/agent",
			forbidden: append(slices.Clone(noService), "internal/launcher", "internal/scriptrun", "internal/autopilot"),
			why: "agent is the pure registry of the coding agents: each spec turns a launch's options, a fenced run's " +
				"included, into its own flags, and reads no service",
		},
		{
			pkg:       "internal/launcher",
			forbidden: []string{"internal/inbox", "internal/github", settings},
			why:       "the launcher reads items, file checks, and settings through its ItemStore, FileChecker, and SettingsReader interfaces",
		},
		{
			pkg: "internal/event",
			forbidden: []string{
				"internal/core", "internal/inbox", settings, "internal/worktrees", "internal/notify",
				"internal/appinfo", "internal/store",
			},
			why: "every service emits events, so the event types depend on none of them",
		},
		{
			pkg:       "internal/budget",
			forbidden: []string{"internal/core", "internal/inbox", "internal/worktrees", settings, "internal/store"},
			why:       "the budget hears the GitHub client's readings and the inbox's refreshes, and keeps its state, through interfaces",
		},
		{
			pkg: "internal/sessions",
			forbidden: []string{
				"internal/launcher", settings, "internal/notify", "internal/inbox", "internal/github",
				"internal/desktop", "internal/core",
			},
			why: "sessions follows the agents through its own Observer, Switch, Focuser, AttentionSink, and Rereader " +
				"interfaces, and never reaches GitHub: the inbox reads a session's item again, through core",
		},
		{
			pkg:       "internal/pushwatch",
			forbidden: append(slices.Clone(noService), "internal/worktrees", "internal/sessions", "internal/desktop"),
			why: "pushwatch reads the pull requests, the clones, and the switch through its own Targets, Clones, " +
				"Origins, and Switch interfaces, stores nothing, and never reaches GitHub: the inbox reads a pull " +
				"request again, through core",
		},
		{
			pkg:       "internal/desktop",
			forbidden: []string{"internal/core", "internal/setup"},
			why: "the shell reaches the services through the interfaces it declares, and hears whether the setup " +
				"shows through its Settings; the desktop bootstrap wires the core into it",
		},
		{
			pkg: "internal/bump",
			forbidden: []string{
				"internal/github", "internal/inbox", "internal/prompts", settings, "internal/store",
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
			pkg: "internal/threadrules",
			forbidden: append(slices.Clone(noService),
				"internal/notify", "internal/notifyprefs", "internal/clock", "internal/integrations", "internal/ghthreads"),
			why: "threadrules declares the GitHub notification rules' settings keys, which settings writes, and decides " +
				"a thread from them and what it is given alone; this rule keeps it off the store, the GitHub client, " +
				"settings, the inbox, core, the notifications, the clock, the integrations, and the mirror of the " +
				"notifications that obeys it, and by convention it imports only item and prefval",
		},
		{
			pkg: "internal/statusprefs",
			forbidden: append(slices.Clone(noService),
				"internal/notify", "internal/notifyprefs", "internal/clock", "internal/svcstatus", "internal/event"),
			why: "statusprefs declares the service status check's catalog and the settings keys of what it watches, " +
				"which settings writes; it knows no store, service, or the check that obeys it, and by convention " +
				"it imports only prefval",
		},
		{
			pkg: "internal/svcstatus",
			forbidden: []string{
				"internal/notify", settings, "internal/core", inbox, "internal/desktop", "internal/store",
			},
			why: "svcstatus reads the status pages through the GitHub client's FetchStatus, its settings through its " +
				"Config's Rules, and tells of a change through its own Notifier, which core adapts to the " +
				"notifications; it keeps its readings in memory alone",
		},
		{
			pkg:       "internal/ghthreads",
			forbidden: append(slices.Clone(sheets), "internal/notify", "internal/review", "internal/issues"),
			why: "ghthreads mirrors the user's GitHub notifications through the GitHub client and reads its rules " +
				"through its own RuleSource, which core adapts; what the inbox tells it comes through core, never " +
				"through an import",
		},
		{
			pkg:       inbox,
			forbidden: []string{"internal/ghthreads"},
			why: "the inbox tells what the user finished only to the OutcomeSink it is given, so what follows its " +
				"items, the mirror of the user's GitHub notifications among them, is wired by core; it never imports " +
				"the mirror",
		},
		{
			pkg: "internal/heldruns",
			forbidden: append(slices.Clone(noService),
				"internal/review", "internal/event", "internal/clock", "internal/autopilot"),
			why: "heldruns declares the ignored workflows' settings key, which settings writes, and decides which " +
				"held workflow runs count from it and what it is given alone; it knows no store, service, or the " +
				"inbox and the review sheet that obey it, and by convention it imports only prefval",
		},
		{
			pkg:       "internal/updateprefs",
			forbidden: append(slices.Clone(noService), "internal/update", "internal/notify", "internal/event"),
			why: "updateprefs declares the update setting's key and choices, which settings writes and the " +
				"update package reads; it knows no store, service, or the update package itself",
		},
		{
			pkg:       "internal/semver",
			forbidden: noService,
			why: "semver reads and compares versions for the integrations and the app's updates; it is a leaf " +
				"that knows no store or service",
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
			pkg: "internal/update",
			forbidden: append(slices.Clone(noService),
				"internal/desktop", "internal/notify", "internal/scriptrun", "internal/prefs"),
			why: "update holds the release manifest's format and its checks, which the release targets in magefiles " +
				"share, and checks, downloads, and installs the app's updates; it reads the releases through its " +
				"Feed, keeps its state through Memory, backs up the database through DataBackup, asks about the " +
				"runs through Runs, tells of a new version through its Notifier, and installs through the " +
				"Installer the shell attaches, all of which core adapts",
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
			pkg:       "internal/review",
			forbidden: append(slices.Clone(sheets), "internal/issues"),
			why: "the review sheet reaches the inbox, the settings, and the replies through its own Inbox, Settings, " +
				"and Replies interfaces, and knows nothing of the issue sheet",
		},
		{
			pkg: "internal/timeline",
			forbidden: append(slices.Clone(sheets),
				"internal/store", "internal/review", "internal/issues", "internal/avatars", "internal/notify"),
			why: "timeline is the conversation both sheets show, converted from the GitHub client's types; it " +
				"knows no store, no service, and neither sheet, and by convention it imports only github, item, " +
				"clock, and textutil",
		},
		{
			pkg:       "internal/issues",
			forbidden: append(slices.Clone(sheets), "internal/review"),
			why:       "the issue sheet reaches the inbox through its own Inbox interface, and knows nothing of the review sheet",
		},
		{
			pkg: "internal/backup",
			forbidden: append([]string{
				"internal/prompts", "internal/launchdefaults", "internal/scripts", "internal/autopilot",
			}, services...),
			why: "the backup reaches each category's owner only through its Part, which core adapts; the owners " +
				"import its plan's words, never the reverse",
		},
		{
			pkg: "internal/autopilot",
			forbidden: []string{
				inbox, "internal/review", "internal/issues", "internal/ghthreads", "internal/launcher",
				"internal/worktrees", "internal/sessions", "internal/scriptrun", "internal/notify", settings,
				"internal/desktop", "internal/core",
			},
			why: "Autopilot reaches the inbox, the sheets, the launcher, the worktrees, the sessions, the runs, the " +
				"notifications, and the settings through the narrow interfaces it declares, which core adapts " +
				"(adapters_autopilot.go); it reads its settings through autopilotprefs",
		},
		{
			pkg: "internal/agentrun",
			forbidden: append([]string{
				"internal/autopilot", "internal/launcher", "internal/desktop", "internal/sessions", "internal/worktrees",
				"internal/notify",
			}, services...),
			why: "the background runner takes a finished plan (agentrun.Plan), which core builds from the launcher's " +
				"background plan, and tells how each run ended through the interfaces it declares: it never plans a " +
				"launch itself, so a run has no more access than the launcher gave it",
		},
		{
			pkg:       "internal/autopilotprefs",
			forbidden: append(slices.Clone(noService), "internal/autopilot", "internal/event"),
			why: "autopilotprefs declares Autopilot's settings keys and a routine's modes, which settings writes " +
				"and Autopilot reads; it knows no store, service, or Autopilot itself",
		},
		{
			pkg: "internal/setup",
			forbidden: []string{
				inbox, settings, "internal/core", "internal/desktop", "internal/store", "internal/backup",
				"internal/scriptrun", "internal/integrations", "internal/profiles", "internal/notify",
			},
			why: "the setup reaches the inbox, the clones, the runs, and the backup through the narrow interfaces it " +
				"declares, which core adapts, and keeps its state through its Memory",
		},
	}
}

// leaf holds a package to the internal packages it may import directly, beside the
// standard library, and to no other module: a stricter rule than a boundary's, for the
// pure leaves whose whole world is a short list. Paths are relative to the module.
type leaf struct {
	pkg     string
	allowed []string
	why     string
}

// leaves are the packages held to what they import.
func leaves() []leaf {
	// autopilot are the routines core's pure leaves, which may import one another, as
	// long as they form no cycle, which Go refuses anyway.
	autopilot := []string{
		"internal/autopilot/catalog", "internal/autopilot/watch", "internal/autopilot/spec",
		"internal/autopilot/facts", "internal/autopilot/eval",
	}
	allowed := append([]string{
		"internal/item", "internal/repo", "internal/trust", "internal/sensitive", "internal/bump", "internal/clock",
		"internal/textutil",
	}, autopilot...)

	out := make([]leaf, 0, len(autopilot)+1)

	for _, pkg := range autopilot {
		out = append(out, leaf{
			pkg: pkg, allowed: allowed,
			why: "the autopilot leaves (spec, catalog, facts, eval, watch) are pure: they read nothing, keep no clock " +
				"of their own, and import only item, repo, trust, sensitive, bump, clock, textutil, one another, and " +
				"the standard library; the autopilot service declares interfaces and core adapts them",
		})
	}

	return append(out, leaf{
		pkg: "internal/autopilotprefs", allowed: []string{"internal/prefs/prefval"},
		why: "autopilotprefs declares Autopilot's settings for settings to write and Autopilot to read, so it " +
			"imports only prefval and the standard library",
	}, leaf{
		pkg: "internal/fence", allowed: []string{"internal/sensitive", "internal/paths", "internal/textutil"},
		why: "fence is the agent-independent fence policy, and its hook mode runs before the app opens anything, so " +
			"it imports only sensitive's patterns, paths' readers, textutil's quoting, and the standard library",
	}, leaf{
		pkg: "internal/agentrun/runevent", allowed: []string{},
		why: "runevent is a background run's event model, which Autopilot and the window read without reaching " +
			"the runner, so it imports nothing of the app's",
	})
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
	Imports    []string `json:"Imports"`
}

// listing is how go list describes the packages the boundaries and the leaves name, for
// each OS in buildOSes, keyed by OS and then by import path, and the module's path.
type listing struct {
	module string
	byOS   map[string]map[string]listedPackage
}

// sharedListing runs go list once per OS per test binary, for TestPackageBoundaries and
// TestLeafImports alike.
var sharedListing = sync.OnceValues(listGuarded) //nolint:gochecknoglobals // one read-only listing per test binary

// listed returns the shared listing, skipping t when the go command is not available
// and failing it when go list fails.
func listed(t *testing.T) listing {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not available")
	}

	l, err := sharedListing()
	require.NoError(t, err)

	return l
}

// listGuarded lists every package a boundary or a leaf names, for each OS in buildOSes.
func listGuarded() (listing, error) {
	root, err := findModuleRoot()
	if err != nil {
		return listing{}, err
	}

	module, err := readModulePath(root)
	if err != nil {
		return listing{}, err
	}

	var pkgs []string

	for _, r := range boundaries() {
		pkgs = append(pkgs, module+"/"+r.pkg)
	}

	for _, l := range leaves() {
		pkgs = append(pkgs, module+"/"+l.pkg)
	}

	slices.Sort(pkgs)
	pkgs = slices.Compact(pkgs)

	l := listing{module: module, byOS: map[string]map[string]listedPackage{}}

	for _, goos := range buildOSes() {
		if l.byOS[goos], err = listPackages(root, goos, pkgs); err != nil {
			return listing{}, err
		}
	}

	return l, nil
}

// listPackages returns how go list describes each of pkgs when built for goos, keyed by
// import path: what it depends on, directly or not, and what it imports itself.
func listPackages(root, goos string, pkgs []string) (map[string]listedPackage, error) {
	args := append([]string{"list", "-json=ImportPath,Deps,Imports"}, pkgs...)

	list := exec.CommandContext(context.Background(), "go", args...) //nolint:gosec // argv only, no shell
	list.Dir = root

	list.Env = append(os.Environ(), "GOOS="+goos)

	out, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s for %s: %w", strings.Join(pkgs, " "), goos, err)
	}

	listed := make(map[string]listedPackage, len(pkgs))

	dec := json.NewDecoder(bytes.NewReader(out))

	for {
		var p listedPackage

		err = dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return listed, nil
		}

		if err != nil {
			return nil, fmt.Errorf("read go list for %s: %w", goos, err)
		}

		listed[p.ImportPath] = p
	}
}

// strayImports returns "pkg imports dep (why)" for every import of a leaf's package that
// its rule doesn't allow: an internal package off its list, or another module's.
func strayImports(module string, l leaf, imports []string) []string {
	var found []string

	for _, imp := range imports {
		rel, internal := strings.CutPrefix(imp, module+"/")

		switch {
		case internal && slices.Contains(l.allowed, rel):
		case internal:
			found = append(found, l.pkg+" imports "+rel+" ("+l.why+")")
		case strings.Contains(strings.SplitN(imp, "/", 2)[0], "."):
			found = append(found, l.pkg+" imports "+imp+", another module's ("+l.why+")")
		}
	}

	return found
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
// rule's package, given the packages go list described, keyed by import path.
func crossings(t *testing.T, module string, r boundary, pkgs map[string]listedPackage) []string {
	t.Helper()

	p, ok := pkgs[module+"/"+r.pkg]
	require.True(t, ok, "%s is gone; update this guard", r.pkg)

	var found []string

	for _, f := range r.forbidden {
		for _, dep := range dependsOn(p.Deps, module+"/"+f) {
			found = append(found, r.pkg+" depends on "+strings.TrimPrefix(dep, module+"/")+" ("+r.why+")")
		}
	}

	return found
}

func TestPackageBoundaries(t *testing.T) {
	t.Parallel()

	l := listed(t)

	var found []string

	for _, goos := range buildOSes() {
		for _, r := range boundaries() {
			for _, c := range crossings(t, l.module, r, l.byOS[goos]) {
				found = append(found, goos+": "+c)
			}
		}
	}

	assert.Empty(t, found, "keep these package dependencies cut; reach the other package through an interface "+
		"declared where it is used, and adapt it in internal/core")
}

func TestLeafImports(t *testing.T) {
	t.Parallel()

	l := listed(t)

	var found []string

	for _, goos := range buildOSes() {
		for _, rule := range leaves() {
			p, ok := l.byOS[goos][l.module+"/"+rule.pkg]
			require.True(t, ok, "%s is gone; update this guard", rule.pkg)

			for _, s := range strayImports(l.module, rule, p.Imports) {
				found = append(found, goos+": "+s)
			}
		}
	}

	assert.Empty(t, found, "keep these leaves to what they import; reach anything else through an interface the "+
		"service declares, and adapt it in internal/core")
}

func TestStrayImports(t *testing.T) {
	t.Parallel()

	l := leaf{pkg: "internal/autopilot/eval", allowed: []string{"internal/item", "internal/autopilot/spec"}, why: "pure"}

	assert.Empty(t, strayImports("example.com/m", l, []string{
		"strings", "encoding/json", "example.com/m/internal/item", "example.com/m/internal/autopilot/spec",
	}))
	assert.Equal(t, []string{
		"internal/autopilot/eval imports internal/store (pure)",
		"internal/autopilot/eval imports github.com/stretchr/testify/assert, another module's (pure)",
	}, strayImports("example.com/m", l, []string{
		"example.com/m/internal/store", "github.com/stretchr/testify/assert", "time",
	}))
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
