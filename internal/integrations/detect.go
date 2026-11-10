package integrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

const (
	// versionTimeout bounds one run of a tool's --version.
	versionTimeout = 5 * time.Second

	// maxOthers caps how many other copies of a tool are run for their version.
	maxOthers = 8

	// versionParallel bounds how many copies run their --version at once.
	versionParallel = 4
)

// Source says where a copy of a tool came from.
type Source string

// The sources.
const (
	// SourceHomebrew is a copy inside a Homebrew Cellar.
	SourceHomebrew Source = "homebrew"

	// SourceGoInstall is a copy in $GOBIN or $GOPATH/bin, where go install puts one.
	SourceGoInstall Source = "goInstall"

	// SourceDevelopment is a copy built from a working copy: its version is dev, a
	// pseudo-version, or dirty.
	SourceDevelopment Source = "development"

	// SourceRelease is any other copy, which a GitHub release put there.
	SourceRelease Source = "release"
)

// Copy is one copy of a tool on this machine.
type Copy struct {
	// Path is where it was found, and DisplayPath the same with ~ for the home folder.
	Path        string `json:"path"`
	DisplayPath string `json:"displayPath"`

	// Link reports a symbolic link, and Target where it points, with ~; the installer
	// never replaces a link.
	Link   bool   `json:"link"`
	Target string `json:"target"`

	// Runs reports that it ran and printed its version; Error is its first error line
	// when it didn't.
	Runs    bool    `json:"runs"`
	Version Version `json:"version"`
	Error   string  `json:"error"`

	// Source is where it came from.
	Source Source `json:"source"`

	// OnPath reports that the user's shell finds it on its PATH.
	OnPath bool `json:"onPath"`

	// Shadowing reports the copy the user's terminal runs when it isn't the one in use.
	Shadowing bool `json:"shadowing"`
}

// detection is what one detection found of one tool.
type detection struct {
	// selected is the copy in use, valid when has is set.
	selected Copy
	has      bool

	// chosenPath is the copy the user had chosen when this detection ran, or "" for none;
	// chosen reports that the selected copy is it, and chosenMissing that it is no longer
	// there or won't run as a program.
	chosenPath    string
	chosen        bool
	chosenMissing bool

	// others are every other copy found, in the order they were found.
	others []Copy

	// stat is the selected file's size, time, and inode, which tell a later look whether
	// it changed.
	stat fileStat
}

// runFunc runs a program and returns what it wrote.
type runFunc func(ctx context.Context, path string, args, env []string) (stdout, stderr []byte, err error)

// execRun runs a program through execpath, with stdin from /dev/null.
func execRun(ctx context.Context, path string, args, env []string) ([]byte, []byte, error) {
	return execpath.Exec{Env: env}.Run(ctx, path, args...)
}

// searchDirs are the folders a tool is looked for in.
type searchDirs struct {
	// path is the shell's PATH, in order: the copy found first is the one the user's
	// terminal runs.
	path []string

	// extra are the well-known tool folders and go install's, searched after path.
	extra []string

	// goBins are the folders go install puts a tool in.
	goBins []string
}

// dirsFrom works out where to look from the user's shell environment: its PATH, then
// execpath's well-known folders, then $GOBIN, or $GOPATH/bin, or ~/go/bin.
func dirsFrom(env []string, home string) searchDirs {
	var goBins []string

	switch gobin, gopath := shellenv.Lookup(env, "GOBIN"), shellenv.Lookup(env, "GOPATH"); {
	case filepath.IsAbs(gobin):
		goBins = []string{gobin}
	case gopath != "":
		for _, dir := range filepath.SplitList(gopath) {
			if filepath.IsAbs(dir) {
				goBins = append(goBins, filepath.Join(dir, "bin"))
			}
		}
	case home != "":
		goBins = []string{filepath.Join(home, "go", "bin")}
	}

	return searchDirs{
		path:   absDirs(filepath.SplitList(shellenv.Lookup(env, "PATH"))),
		extra:  absDirs(append(filepath.SplitList(execpath.FallbackPath(home, "")), goBins...)),
		goBins: goBins,
	}
}

// absDirs keeps the absolute folders of dirs, cleaned, each once.
func absDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		if filepath.IsAbs(dir) && !slices.Contains(out, filepath.Clean(dir)) {
			out = append(out, filepath.Clean(dir))
		}
	}

	return out
}

// detector finds one tool's copies and runs each for its version.
type detector struct {
	def  Definition
	dirs searchDirs
	env  []string
	home string
	run  runFunc

	// slots bounds how many copies run their --version at once, across every tool
	// detected together; nil allows versionParallel for this tool alone.
	slots chan struct{}
}

// candidate is a copy found before it is run.
type candidate struct {
	path, resolved string
	onPath         bool
}

// detect finds the tool's copies, picks the one in use, and runs each for its version:
// the copy at chosen when the user chose one that is still an executable file, else the
// first on the shell's PATH, else the first in the well-known folders.
func (d detector) detect(ctx context.Context, chosen string) detection {
	found := d.candidates()
	selected, out := pick(found, chosen)

	if selected.path == "" {
		return out
	}

	// The copy the user's terminal runs, when it isn't the one in use.
	var terminal string

	if i := slices.IndexFunc(found, func(c candidate) bool { return c.onPath }); i >= 0 && found[i].resolved != selected.resolved {
		terminal = found[i].resolved
	}

	others := slices.DeleteFunc(slices.Clone(found), func(c candidate) bool { return c.resolved == selected.resolved })
	if len(others) > maxOthers {
		others = others[:maxOthers]
	}

	copies := d.inspectAll(ctx, append([]candidate{selected}, others...))

	for i := range copies[1:] {
		copies[i+1].Shadowing = others[i].resolved == terminal
	}

	out.selected, out.has, out.others = copies[0], true, copies[1:]
	out.stat = statOf(selected.path)

	return out
}

// pick chooses the copy in use: the one at chosen when the user chose one that is still an
// executable file, else the first found. The detection it returns says which.
func pick(found []candidate, chosen string) (candidate, detection) {
	out := detection{chosenPath: chosen}

	if chosen != "" {
		if !isExecutable(chosen) {
			out.chosenMissing = true
		} else {
			selected := candidate{path: chosen, resolved: paths.Resolved(chosen)}
			selected.onPath = slices.ContainsFunc(found, func(c candidate) bool { return c.onPath && c.resolved == selected.resolved })
			out.chosen = true

			return selected, out
		}
	}

	if len(found) == 0 {
		return candidate{}, out
	}

	return found[0], out
}

// inspectAll runs each copy for its version, a few at a time.
func (d detector) inspectAll(ctx context.Context, all []candidate) []Copy {
	slots := d.slots
	if slots == nil {
		slots = make(chan struct{}, versionParallel)
	}

	copies := make([]Copy, len(all))

	var wg sync.WaitGroup

	for i, c := range all {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			copies[i] = d.inspect(ctx, c)
		})
	}

	wg.Wait()

	return copies
}

// candidates lists every executable copy of the tool in the shell's PATH, then in the
// extra folders, each once by where it resolves to.
func (d detector) candidates() []candidate {
	var out []candidate

	seen := map[string]bool{}

	add := func(dirs []string, onPath bool) {
		for _, dir := range dirs {
			path := filepath.Join(dir, d.def.Binary)
			if !isExecutable(path) {
				continue
			}

			resolved := paths.Resolved(path)
			if seen[resolved] {
				continue
			}

			seen[resolved] = true
			out = append(out, candidate{path: path, resolved: resolved, onPath: onPath})
		}
	}

	add(d.dirs.path, true)
	add(d.dirs.extra, false)

	return out
}

// inspect runs one copy for its version and works out where it came from.
func (d detector) inspect(ctx context.Context, c candidate) Copy {
	out := Copy{Path: c.path, DisplayPath: d.display(c.path), OnPath: c.onPath}

	if info, err := os.Lstat(c.path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		out.Link, out.Target = true, d.display(c.resolved)
	}

	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	stdout, stderr, err := d.run(ctx, c.path, d.def.VersionArgs, d.runEnv())

	version, parseErr := d.def.ParseVersion(stdout)

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		out.Error = "it didn't answer within 5 seconds"
	case err != nil:
		out.Error = firstLine(stderr, stdout, err.Error())
	case parseErr != nil:
		// Its own output is no help here: magex's starts with a banner.
		out.Error = firstLine(stderr, nil, "it printed no version")
	default:
		out.Runs, out.Version = true, version
	}

	out.Source = d.source(c, out.Version)

	return out
}

// display writes path with ~ for the home folder, as given or with its links resolved, since
// a link's target is written resolved.
func (d detector) display(path string) string {
	if shown := paths.Tilde(path, d.home); shown != path || d.home == "" {
		return shown
	}

	return paths.Tilde(path, paths.Resolved(d.home))
}

// source works out where a copy came from, by its folder and its version.
func (d detector) source(c candidate, v Version) Source {
	switch {
	case strings.Contains(c.resolved, string(filepath.Separator)+"Cellar"+string(filepath.Separator)):
		return SourceHomebrew
	case slices.ContainsFunc(d.dirs.goBins, func(dir string) bool { return paths.Same(filepath.Dir(c.path), dir) }):
		return SourceGoInstall
	case v.Development():
		return SourceDevelopment
	default:
		return SourceRelease
	}
}

// runEnv is the environment a tool runs with: the shell's, with the tool's quiet
// variables on top.
func (d detector) runEnv() []string {
	env := slices.Clone(d.env)

	for _, k := range sortedKeys(d.def.QuietEnv) {
		env = append(env, k+"="+d.def.QuietEnv[k])
	}

	return env
}

// sortedKeys returns m's keys in order, so a tool's environment is the same every run.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// firstLine returns the first line of the first of stderr and stdout that has one, or
// fallback.
func firstLine(stderr, stdout []byte, fallback string) string {
	for _, out := range [][]byte{stderr, stdout} {
		if line := execpath.FirstLine(stripANSI(out)); line != "" {
			return line
		}
	}

	return fallback
}

// isExecutable reports whether path is a regular file, following links, that someone
// may run.
func isExecutable(path string) bool {
	info, err := os.Stat(path)

	return err == nil && execpath.Executable(info.Mode())
}

// detectAll detects every definition's copies, the tools side by side, with at most
// versionParallel copies running their --version at once in all.
func detectAll(ctx context.Context, defs []Definition, chosen map[string]string, dirs searchDirs, env []string,
	home string, run runFunc,
) map[string]detection {
	out := make(map[string]detection, len(defs))

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	slots := make(chan struct{}, versionParallel)

	for _, def := range defs {
		wg.Go(func() {
			d := detector{def: def, dirs: dirs, env: env, home: home, run: run, slots: slots}
			got := d.detect(ctx, chosen[string(def.ID)])

			mu.Lock()
			out[string(def.ID)] = got
			mu.Unlock()
		})
	}

	wg.Wait()

	return out
}
