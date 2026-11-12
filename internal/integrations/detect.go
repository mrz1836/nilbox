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
	"github.com/mrz1836/nilbox/internal/integrationprefs"
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
	// SourceApp is a copy the app installed: the newest install recorded at its path has
	// its SHA-256.
	SourceApp Source = "app"

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

	// InstalledAt and Archive say, for a copy the app installed, when it did and from which
	// release archive.
	InstalledAt string `json:"installedAt"`
	Archive     string `json:"archive"`

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

	// selfUpdates reports that the selected copy updates itself with the tool's own
	// updater (Service.updatesItself), as of this detection.
	selfUpdates bool
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

	// goBins are go install's folders.
	goBins goFolders
}

// goFolders are go install's folders, by the one rule that decides both where a copy came
// from and whether the tool's own updater would take it for one of go install's (magex's
// then runs go install): $GOBIN and each $GOPATH folder's bin, from the shell's environment
// and, while go is on its PATH, from go env too, which adds go env -w settings and GOPATH's
// default, and ~/go/bin.
type goFolders struct {
	// dirs are the absolute folders; rel are relative ones, which an updater takes from the
	// folder it runs in, its copy's.
	dirs []string
	rel  []string

	// unsure reports that go is on the PATH but its settings couldn't be read: no copy can
	// be told clear of go install's folders.
	unsure bool
}

// add adds gobin and the bin of each folder of gopath, a list as in $GOPATH.
func (g *goFolders) add(gobin, gopath string) {
	bins := []string{}

	if gobin = strings.TrimSpace(gobin); gobin != "" {
		bins = append(bins, gobin)
	}

	for _, root := range filepath.SplitList(strings.TrimSpace(gopath)) {
		if root != "" {
			bins = append(bins, filepath.Join(root, "bin"))
		}
	}

	for _, bin := range bins {
		switch {
		case !filepath.IsAbs(bin):
			g.rel = append(g.rel, bin)
		case !slices.Contains(g.dirs, filepath.Clean(bin)):
			g.dirs = append(g.dirs, filepath.Clean(bin))
		}
	}
}

// has reports whether dir is one of go install's folders, links resolved, a relative one
// taken from dir itself.
func (g *goFolders) has(dir string) bool {
	same := func(bin string) bool { return paths.Same(bin, dir) }
	fromDir := func(bin string) bool { return paths.Same(filepath.Join(dir, bin), dir) }

	return slices.ContainsFunc(g.dirs, same) || slices.ContainsFunc(g.rel, fromDir)
}

// dirsFrom works out where to look from the user's shell environment: its PATH, then
// execpath's well-known folders, then go install's: $GOBIN, each $GOPATH folder's bin, and
// ~/go/bin. Service.addGoEnv adds what go env says.
func dirsFrom(env []string, home string) searchDirs {
	var bins goFolders

	bins.add(shellenv.Lookup(env, "GOBIN"), shellenv.Lookup(env, "GOPATH"))

	if home != "" {
		bins.add(filepath.Join(home, "go", "bin"), "")
	}

	return searchDirs{
		path:   absDirs(filepath.SplitList(shellenv.Lookup(env, "PATH"))),
		extra:  absDirs(append(filepath.SplitList(execpath.FallbackPath(home, "")), bins.dirs...)),
		goBins: bins,
	}
}

// addGoEnv adds go install's folders as go itself reads them, when go is on the shell's
// PATH: go env's GOBIN and GOPATH, run with the shell's environment and GOTOOLCHAIN=local,
// so asking never fetches a toolchain. An answer it can't read leaves them unsure. Without
// go on the PATH, nothing can run go install, so the environment's answer stands.
func (s *Service) addGoEnv(ctx context.Context, env []string, dirs *searchDirs) {
	goPath := findGo(dirs.path)
	if goPath == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	stdout, _, err := s.run(ctx, goPath, []string{"env", "GOBIN", "GOPATH"}, shellenv.With(env, "GOTOOLCHAIN", "local"))

	gobin, gopath, ok := parseGoEnv(stdout)
	if err != nil || !ok {
		dirs.goBins.unsure = true

		s.logger.InfoContext(ctx, "couldn't read go's settings, so no tool updates itself", "error", err)

		return
	}

	dirs.goBins.add(gobin, gopath)
	dirs.extra = absDirs(append(dirs.extra, dirs.goBins.dirs...))
}

// findGo returns the first go on the shell's PATH, as a tool's own updater would find it,
// or "".
func findGo(dirs []string) string {
	for _, dir := range dirs {
		if path := filepath.Join(dir, "go"); isExecutable(path) {
			return path
		}
	}

	return ""
}

// parseGoEnv reads go env GOBIN GOPATH's answer, a line each, GOBIN's empty when it isn't
// set; ok is false for any other answer.
func parseGoEnv(out []byte) (gobin, gopath string, ok bool) {
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(lines) != 2 || strings.TrimSpace(lines[1]) == "" {
		return "", "", false
	}

	return lines[0], lines[1], true
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

	stdout, stderr, err := d.run(ctx, c.path, d.def.VersionArgs, d.def.RunEnv(d.env, integrationprefs.RunsInApp))

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
	case d.dirs.goBins.has(filepath.Dir(c.path)):
		return SourceGoInstall
	case v.Development():
		return SourceDevelopment
	default:
		return SourceRelease
	}
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
