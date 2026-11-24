package hooks

import (
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What a run's Before keeps beside its record (scriptrun.Spec.Keep): a file of the run's
// own, beside its log, which the runner removes once the run's end is recorded. Neither the
// run's intent nor its result carries it, so the window never gets it, and a clone's whole
// dirty state never outlives its run.

var (
	// errNotKept is a run from before the app restarted that kept nothing of what its
	// Before read, so what it changed can't be told.
	errNotKept = errors.New("what the run's Before read wasn't kept")

	// errNoStatus is a checks run from before the app restarted whose Before couldn't read
	// git's status of the clone, as it kept.
	errNoStatus = errors.New("git's status of the clone couldn't be read before the run")
)

// keptAction is what a hooks action's Before read, under the clone's lock: the copy of
// go-pre-commit that runs, and the clone's hooks.
type keptAction struct {
	Tool  keptTool  `json:"tool"`
	Hooks keptHooks `json:"hooks"`
}

// keptTool is a copy of go-pre-commit as an action's Before found it: its path, its
// version, and the upstream features that version has.
type keptTool struct {
	Path     string                 `json:"path"`
	Version  string                 `json:"version"`
	Features []integrations.Feature `json:"features"`
}

// keepTool keeps the copy tool.
func keepTool(tool ToolFacts) keptTool {
	return keptTool{Path: tool.Path, Version: tool.Version, Features: textutil.NonNil(tool.Features)}
}

// facts returns the copy k keeps as facts to judge an action with: found and usable, with
// the features it had.
func (k keptTool) facts() ToolFacts {
	return ToolFacts{
		Path: k.Path, Version: k.Version, State: integrations.StateFound, Features: textutil.NonNil(k.Features),
		Copies: []string{k.Path},
	}
}

// keptHooks is a clone's hooks: where git ran the hooks from, the pre-commit hook, the hook
// types that carried go-pre-commit's marker, and the hooks it had set aside.
type keptHooks struct {
	Toplevel  string `json:"toplevel"`
	Hooks     string `json:"hooks"`
	CommonDir string `json:"commonDir"`

	// Present, Ours, SHA, and OtherTool are the pre-commit hook's: there, go-pre-commit's,
	// its contents' SHA-256, and the other tool whose hook it is.
	Present   bool   `json:"present"`
	Ours      bool   `json:"ours"`
	SHA       string `json:"sha"`
	OtherTool string `json:"otherTool"`

	Marked  []string `json:"marked"`
	Backups []string `json:"backups"`
}

// keepHooks keeps the hooks l.
func keepHooks(l clonesHooks) keptHooks {
	return keptHooks{
		Toplevel: l.paths.Toplevel, Hooks: l.paths.Hooks, CommonDir: l.paths.CommonDir,
		Present: l.hook.present, Ours: l.hook.ours, SHA: l.hook.sha, OtherTool: l.hook.tool,
		Marked: textutil.NonNil(l.marked), Backups: textutil.NonNil(l.backups),
	}
}

// hooks returns the hooks k keeps, as the action's Before read them.
func (k keptHooks) hooks() clonesHooks {
	hp := gitread.HookPaths{Toplevel: k.Toplevel, Hooks: k.Hooks, CommonDir: k.CommonDir}

	return clonesHooks{
		paths: hp, shared: isShared(hp),
		hook: hookFile{
			path: filepath.Join(k.Hooks, hookPreCommit), present: k.Present, ours: k.Ours, sha: k.SHA, tool: k.OtherTool,
		},
		marked: textutil.NonNil(k.Marked), backups: textutil.NonNil(k.Backups),
	}
}

// keptChecks is what a checks run's Before read, under the clone's lock: the config's
// switch for each check, which says which checks are off, and the clone's snapshot.
type keptChecks struct {
	Checks   []CheckSetting `json:"checks"`
	Snapshot keptSnapshot   `json:"snapshot"`
}

// configOf returns a config that turns each check on or off as settings do.
func configOf(settings []CheckSetting) repoConfig {
	cfg := repoConfig{present: true, values: map[string]string{}}

	for _, c := range settings {
		cfg.values[checkKey(c.Name)] = strconv.FormatBool(c.On)
	}

	return cfg
}

// keptSnapshot is a clone's snapshot: each dirty file's status, what the index holds for
// it, and its sum, its contents' SHA-256 when Hashed, else its size and modification time,
// as for a clone with more than maxHashed dirty files; or why git's status couldn't be read.
type keptSnapshot struct {
	Error  string      `json:"error,omitempty"`
	Hashed bool        `json:"hashed"`
	Files  []keptEntry `json:"files"`
}

// keptEntry is one dirty file in a keptSnapshot.
type keptEntry struct {
	Path  string `json:"path"`
	Code  string `json:"code"`
	Index string `json:"index,omitempty"`
	Sum   string `json:"sum"`
}

// keepSnapshot keeps snap, however many files it holds.
func keepSnapshot(snap snapshot) keptSnapshot {
	if snap.err != nil {
		return keptSnapshot{Error: uierr.Message(snap.err), Files: []keptEntry{}}
	}

	files := make([]keptEntry, 0, len(snap.entries))

	for _, p := range slices.Sorted(maps.Keys(snap.entries)) {
		e := snap.entries[p]
		files = append(files, keptEntry{Path: p, Code: e.Code, Index: e.Index, Sum: snap.sums[p]})
	}

	return keptSnapshot{Hashed: snap.hashed, Files: files}
}

// snapshot returns the snapshot k keeps.
func (k keptSnapshot) snapshot() snapshot {
	if k.Error != "" {
		return snapshot{err: uierr.New(k.Error, errNoStatus)}
	}

	snap := snapshot{
		entries: make(map[string]gitread.Entry, len(k.Files)), sums: make(map[string]string, len(k.Files)),
		hashed: k.Hashed,
	}

	for _, f := range k.Files {
		snap.entries[f.Path] = gitread.Entry{Code: f.Code, Index: f.Index, Path: f.Path}
		snap.sums[f.Path] = f.Sum
	}

	return snap
}
