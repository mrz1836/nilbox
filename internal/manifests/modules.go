package manifests

import (
	"cmp"
	"context"
	"errors"
	"path"
	"slices"
	"sync/atomic"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The longest a manifest's own words are kept, in runes: a module path or a package's
// name, a Go version or a toolchain, and a workspace pattern.
const (
	maxName    = 300
	maxVersion = 60
	maxPattern = 200
)

// moduleReader reads the modules of one clone at one commit, moduleParallel at a time.
type moduleReader struct {
	git    Git
	dir    string
	commit string
	files  treeFiles

	// unsettled reports a read that failed in a way the next may not, as one the clone's
	// time ran out on, so what was read isn't kept for the next scan.
	unsettled atomic.Bool
}

// readAll reads the module of each manifest, moduleParallel at a time, in their order.
func (r *moduleReader) readAll(ctx context.Context, manifests []gitread.TreeEntry) []Module {
	modules := make([]Module, len(manifests))

	parallel(manifests, moduleParallel, func(i int, e gitread.TreeEntry) {
		modules[i] = r.readInTime(ctx, e)
	})

	return modules
}

// readInTime reads the module whose manifest is e, or, when the clone's time ran out
// before its turn, says so: the modules already read are still shown, and the next scan
// reads them all again.
func (r *moduleReader) readInTime(ctx context.Context, e gitread.TreeEntry) Module {
	if ctx.Err() != nil {
		r.unsettled.Store(true)

		return Module{
			Kind: kindOf(e.Path), Path: e.Path, Workspaces: []string{},
			Problem:   "This " + path.Base(e.Path) + " wasn't read: reading this clone took too long.",
			Uncounted: uncountedAt(e.Path),
		}
	}

	return r.read(ctx, e)
}

// read reads the module whose manifest is e: its manifest, then its lockfile, which its
// package manager picks among those in a folder, then its newest change, and whether it
// counts toward its clone's age.
func (r *moduleReader) read(ctx context.Context, e gitread.TreeEntry) Module {
	m := Module{Kind: kindOf(e.Path), Path: e.Path, Workspaces: []string{}}

	m.Uncounted = uncountedOf(m, r.parse(ctx, e, &m))

	lock, writer := r.files.lockfileFor(m.Kind, m.Path, m.PackageManager)
	m.Lockfile = lock

	if m.Kind == KindJS && m.PackageManager == ToolNone {
		m.PackageManager = writer
	}

	r.history(ctx, &m)

	return m
}

// kindOf returns the kind of the module whose manifest is at file.
func kindOf(file string) Kind {
	if path.Base(file) == goModFile {
		return KindGo
	}

	return KindJS
}

// parse reads the module's manifest into m, when the clone holds it and its size in the
// tree is at most maxManifest, or says in m.Problem why it couldn't, and reports whether
// it read it. A partial clone may not hold it, and no read here fetches it: its history is
// still read.
func (r *moduleReader) parse(ctx context.Context, e gitread.TreeEntry, m *Module) bool {
	name := path.Base(e.Path)

	switch {
	case e.Missing:
		m.Problem = "This clone doesn't hold this " + name + "'s contents, as a partial clone may not, so only " +
			"when it last changed was read."

		return false
	case e.Size > maxManifest:
		m.Problem = "This " + name + " is larger than 1 MiB, so it wasn't read."

		return false
	}

	data, err := r.git.Blob(ctx, r.dir, e.Object)
	if err != nil {
		r.failed(err)
		m.Problem = problem("Couldn't read this "+name, err)

		return false
	}

	if m.Kind == KindGo {
		return readGoMod(m, data)
	}

	return readPackageJSON(m, data)
}

// uncountedOf says why the module m doesn't count toward its clone's age, its manifest
// read when read is set: its folder's reason (uncountedAt), else that it names no
// dependency of any kind, which only a manifest read can say; UncountedNone when it
// counts.
func uncountedOf(m Module, read bool) Uncounted {
	if why := uncountedAt(m.Path); why != UncountedNone {
		return why
	}

	if read && m.Direct+m.Indirect+m.Deps+m.DevDeps+m.PeerDeps+m.OptionalDeps == 0 {
		return UncountedNoDeps
	}

	return UncountedNone
}

// history reads when the module's manifest or its lockfile last changed into m, or says
// in m.Problem why it couldn't, unless it already says something.
func (r *moduleReader) history(ctx context.Context, m *Module) {
	c, err := r.git.LastChange(ctx, r.dir, r.commit, m.Path, m.Lockfile)
	if err != nil {
		r.failed(err)

		if m.Problem == "" {
			m.Problem = problem("Couldn't read when this "+path.Base(m.Path)+" last changed", err)
		}

		return
	}

	if !c.At.IsZero() {
		m.changed, m.ChangedAt = c.At, clock.Format(c.At)
		m.Subject = textutil.Truncate(textutil.OneLine(c.Subject), maxSubject)
	}
}

// failed notes a read that failed in a way the next may not: anything but git printing
// more than the read holds, which it will again at the same commit.
func (r *moduleReader) failed(err error) {
	if !errors.Is(err, execpath.ErrOutputLimit) {
		r.unsettled.Store(true)
	}
}

// problem words a read of a module that failed: lead, and what git said.
func problem(lead string, err error) string {
	return uierr.Message(uierr.New(textutil.WithReason(lead, gitread.Message(err)), err))
}

// readGoMod reads a go.mod's bytes into m, and reports whether it read as one: a go.mod
// that names no module doesn't.
func readGoMod(m *Module, data []byte) bool {
	mod, ok := parseGoMod(data)

	m.GoModule = clean(mod.module, maxName)
	m.GoVersion, m.Toolchain = clean(mod.goVersion, maxVersion), clean(mod.toolchain, maxVersion)
	m.Direct, m.Indirect = mod.direct, mod.indirect

	if !ok {
		m.Problem = "This go.mod names no module."
	}

	return ok
}

// readPackageJSON reads a package.json's bytes into m, and reports whether it read as one:
// a package.json that isn't a JSON object doesn't.
func readPackageJSON(m *Module, data []byte) bool {
	pkg, err := parsePackageJSON(data)
	if err != nil {
		m.Problem = "This package.json isn't a JSON object, so it wasn't read."

		return false
	}

	m.Name, m.PackageManager = clean(pkg.name, maxName), pkg.manager
	m.Deps, m.DevDeps, m.PeerDeps, m.OptionalDeps = pkg.deps, pkg.devDeps, pkg.peerDeps, pkg.optionalDeps

	for _, pattern := range pkg.workspaces {
		m.Workspaces = append(m.Workspaces, clean(pattern, maxPattern))
	}

	return true
}

// clean returns a manifest's own words as one line of plain text, at most n runes.
func clean(s string, n int) string {
	return textutil.Truncate(textutil.OneLine(s), n)
}

// sortModules sorts modules as a clone lists them: those that count toward its age first,
// then those that don't, each part stalest first, those whose age isn't known last, then
// by path.
func sortModules(modules []Module) {
	slices.SortStableFunc(modules, func(a, b Module) int {
		return cmp.Or(cmp.Compare(countRank(a), countRank(b)),
			compareAges(a.changed.IsZero(), b.changed.IsZero(), a.changed.Compare(b.changed)),
			cmp.Compare(a.Path, b.Path))
	})
}

// countRank is 0 for a module that counts toward its clone's age, else 1.
func countRank(m Module) int {
	if m.Uncounted == UncountedNone {
		return 0
	}

	return 1
}

// countedOf counts the modules that count toward their clone's age.
func countedOf(modules []Module) int {
	n := 0

	for _, m := range modules {
		if m.Uncounted == UncountedNone {
			n++
		}
	}

	return n
}

// compareAges orders two ages, the older first and an unknown one last: aUnknown and
// bUnknown report the ages that aren't known, and byTime how the two compare by time.
func compareAges(aUnknown, bUnknown bool, byTime int) int {
	switch {
	case aUnknown && bUnknown:
		return 0
	case aUnknown:
		return 1
	case bUnknown:
		return -1
	default:
		return byTime
	}
}

// toolsOf returns the tools a clone's tree and modules show it is set up for, in the
// order tools lists them: magex, by its signs, and each manager a package uses, pnpm's
// workspace file counting for pnpm.
func toolsOf(files treeFiles, modules []Module) []Tool {
	found := map[Tool]bool{ToolMagex: files.magex, ToolPNPM: files.pnpmWorkspace()}

	for _, m := range modules {
		found[m.PackageManager] = found[m.PackageManager] || m.PackageManager != ToolNone
	}

	return slices.DeleteFunc(tools(), func(t Tool) bool { return !found[t] })
}
