package manifests

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/toolsigns"
)

// The files Deps picks out of a commit's tree, beside the JavaScript lockfiles.
const (
	goModFile         = "go.mod"
	goSumFile         = "go.sum"
	packageJSONFile   = "package.json"
	goWorkFile        = "go.work"
	pnpmWorkspaceFile = "pnpm-workspace.yaml"
)

// lockfile is a JavaScript package's lockfile, by its file's name, and the manager that
// writes it.
type lockfile struct {
	name string
	tool Tool
}

// jsLockfiles are a JavaScript package's lockfiles, in the order a folder's are taken
// when it holds more than one and the package names no manager of its own.
func jsLockfiles() []lockfile {
	return []lockfile{
		{name: "pnpm-lock.yaml", tool: ToolPNPM},
		{name: "package-lock.json", tool: ToolNPM},
		{name: "yarn.lock", tool: ToolYarn},
		{name: "bun.lock", tool: ToolBun},
		{name: "bun.lockb", tool: ToolBun},
	}
}

// skippedFolders are the folders whose manifests are another project's: vendored
// modules, installed packages, and test fixtures.
func skippedFolders() []string {
	return []string{"vendor", "node_modules", "testdata"}
}

// picker picks what Deps reads out of a commit's tree.
type picker struct {
	// signs are magex's signs (toolsigns.MageX), and locks the JavaScript lockfiles.
	signs []string
	locks []lockfile
}

// newPicker returns a picker.
func newPicker() picker {
	return picker{signs: toolsigns.MageX(), locks: jsLockfiles()}
}

// wanted reports a file the read of a tree keeps: one of magex's signs, or a manifest, a
// lockfile, or a workspace file outside every skipped folder.
func (p picker) wanted(file string) bool {
	return p.sign(file) || (p.readName(path.Base(file)) && !inSkippedFolder(file))
}

// readName reports the name of a file Deps reads: a manifest, a lockfile, or a workspace
// file.
func (p picker) readName(name string) bool {
	switch name {
	case goModFile, goSumFile, packageJSONFile, goWorkFile, pnpmWorkspaceFile:
		return true
	default:
		return slices.ContainsFunc(p.locks, func(l lockfile) bool { return l.name == name })
	}
}

// sign reports one of magex's signs: the file itself, or anything in the folder.
func (p picker) sign(file string) bool {
	return slices.ContainsFunc(p.signs, func(s string) bool { return file == s || inFolder(file, s) })
}

// inFolder reports a file inside the folder dir, both paths from the tree's top.
func inFolder(file, dir string) bool {
	return len(file) > len(dir) && file[len(dir)] == '/' && strings.HasPrefix(file, dir)
}

// inSkippedFolder reports a file inside a vendor, node_modules, or testdata folder, at any
// depth.
func inSkippedFolder(file string) bool {
	folders := strings.Split(file, "/")

	return slices.ContainsFunc(folders[:len(folders)-1], func(f string) bool { return slices.Contains(skippedFolders(), f) })
}

// treeFiles is what Deps found in a commit's tree.
type treeFiles struct {
	// manifests are the go.mod and package.json files read, the shallowest first, at most
	// maxModules of them, and truncated reports that there were more.
	manifests []gitread.TreeEntry
	truncated bool

	// lockfiles holds the lockfiles' paths.
	lockfiles map[string]bool

	// workspaces are the go.work and pnpm-workspace.yaml files, by path, and magex reports
	// one of magex's signs.
	workspaces []string
	magex      bool
}

// files sorts entries, those wanted kept, into what Deps reads. Only a regular file
// counts, never a link or a submodule, and a manifest or a lockfile whose path gitread
// won't pass to git is left out.
func (p picker) files(entries []gitread.TreeEntry) treeFiles {
	out := treeFiles{manifests: []gitread.TreeEntry{}, lockfiles: map[string]bool{}, workspaces: []string{}}

	for _, e := range entries {
		if p.sign(e.Path) {
			out.magex = true
		}

		if e.Regular() && !inSkippedFolder(e.Path) {
			out.add(e)
		}
	}

	slices.SortFunc(out.manifests, func(a, b gitread.TreeEntry) int {
		return cmp.Or(cmp.Compare(strings.Count(a.Path, "/"), strings.Count(b.Path, "/")), cmp.Compare(a.Path, b.Path))
	})

	if len(out.manifests) > maxModules {
		out.manifests, out.truncated = out.manifests[:maxModules], true
	}

	slices.Sort(out.workspaces)

	return out
}

// add files e where it belongs, if anywhere.
func (f *treeFiles) add(e gitread.TreeEntry) {
	switch name := path.Base(e.Path); {
	case name == goModFile || name == packageJSONFile:
		if gitread.ValidManifestPath(e.Path) {
			f.manifests = append(f.manifests, e)
		}
	case name == goWorkFile || name == pnpmWorkspaceFile:
		f.workspaces = append(f.workspaces, e.Path)
	case gitread.ValidLockfilePath(e.Path):
		f.lockfiles[e.Path] = true
	}
}

// pnpmWorkspace reports a pnpm-workspace.yaml anywhere in the tree.
func (f *treeFiles) pnpmWorkspace() bool {
	return slices.ContainsFunc(f.workspaces, func(w string) bool { return path.Base(w) == pnpmWorkspaceFile })
}

// lockfileFor returns the lockfile of the module of kind whose manifest is at file, and
// the manager that writes it: a go.mod's go.sum beside it; a package.json's nearest
// lockfile, in its folder or one above it up to the tree's top, its manager's own first
// when manager names one. It is "" when the module has none.
func (f *treeFiles) lockfileFor(kind Kind, file string, manager Tool) (string, Tool) {
	dir := path.Dir(file)

	if kind == KindGo {
		if sum := path.Join(dir, goSumFile); f.lockfiles[sum] {
			return sum, ToolNone
		}

		return "", ToolNone
	}

	for {
		if lock, tool, found := f.lockfileIn(dir, manager); found {
			return lock, tool
		}

		if dir == "." {
			return "", ToolNone
		}

		dir = path.Dir(dir)
	}
}

// lockfileIn returns the JavaScript lockfile in the folder dir ("." for the tree's top),
// manager's own first, and the manager that writes it.
func (f *treeFiles) lockfileIn(dir string, manager Tool) (string, Tool, bool) {
	order := jsLockfiles()
	slices.SortStableFunc(order, func(a, b lockfile) int { return cmp.Compare(ownRank(a, manager), ownRank(b, manager)) })

	for _, l := range order {
		if lock := path.Join(dir, l.name); f.lockfiles[lock] {
			return lock, l.tool, true
		}
	}

	return "", ToolNone, false
}

// ownRank is 0 for a lockfile manager writes, else 1.
func ownRank(l lockfile, manager Tool) int {
	if manager != ToolNone && l.tool == manager {
		return 0
	}

	return 1
}
