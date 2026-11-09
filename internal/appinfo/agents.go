package appinfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/paths"
)

// versionTimeout bounds one agent's --version, which an agent that hangs would
// otherwise hold the About page on.
const versionTimeout = 5 * time.Second

// versionPattern finds a version in what an agent's --version prints, as in
// "2.1.319 (Claude Code)" or "codex-cli 0.163.0".
var versionPattern = regexp.MustCompile(`\d+\.\d+(?:\.\d+)*(?:-[0-9A-Za-z][0-9A-Za-z.]*)?`)

// AgentVersion is one coding agent's installed version, as its command reports it.
type AgentVersion struct {
	// Agent and Name are the agent.
	Agent agent.ID `json:"agent"`
	Name  string   `json:"name"`

	// Installed reports that its command was found.
	Installed bool `json:"installed"`

	// Path is where its command is, with ~ for the home directory, or "".
	Path string `json:"path"`

	// Version is the version the command reports, such as 2.1.319, or "" when it could
	// not be read.
	Version string `json:"version"`

	// Error says why the version could not be read, or is "".
	Error string `json:"error"`
}

// versionKey identifies one build of an agent's command: its real path, size, and
// modification time. An agent that updates itself replaces its command, or points its
// link at a new one, which changes the key, so a version is never shown stale.
type versionKey struct {
	path string
	size int64
	mod  time.Time
}

// versionResult is what one build's --version gave: its version, or why there is none.
type versionResult struct {
	key     versionKey
	version string
	err     string
}

// versions reads the coding agents' versions and keeps each one until its command
// changes.
type versions struct {
	finder execpath.Finder
	run    execpath.Runner
	home   string

	// calls runs one --version per agent at a time, for every caller asking then.
	calls flight.Group[versionResult]

	// mu guards known.
	mu    sync.Mutex
	known map[agent.ID]versionResult
}

// all returns every agent's version, in the agents' order, reading the agents in
// parallel.
func (v *versions) all(ctx context.Context) []AgentVersion {
	agents := agent.All()
	out := make([]AgentVersion, len(agents))

	var wg sync.WaitGroup

	for i, a := range agents {
		wg.Go(func() {
			out[i] = v.of(ctx, a)
		})
	}

	wg.Wait()

	return out
}

// of returns one agent's version: the one kept for its command's current build, or one
// read now.
func (v *versions) of(ctx context.Context, a agent.Agent) AgentVersion {
	out := AgentVersion{Agent: a.ID, Name: a.Name}

	if v.finder == nil || v.run == nil {
		return out
	}

	path, err := v.finder.Find(a.Command)
	if err != nil {
		return out
	}

	out.Installed, out.Path = true, paths.Tilde(path, v.home)

	key, err := buildOf(path)
	if err != nil {
		out.Error = fmt.Sprintf("couldn't read %s: %v", a.Command, err)

		return out
	}

	got, ok := v.kept(a.ID, key)
	if !ok {
		// A run cut short because its caller left returns that caller's error, so flight
		// counts it abandoned: a caller that joins it as it ends starts it again, rather than
		// taking the cut-short run as its answer.
		got, err = v.calls.Do(ctx, string(a.ID), func(ctx context.Context) (versionResult, error) {
			return v.read(ctx, a, path, key), ctx.Err()
		})
		if err != nil {
			out.Error = "the version check was cut short"

			return out
		}
	}

	out.Version, out.Error = got.version, got.err

	return out
}

// kept returns the version kept for an agent, when it was read from the same build.
func (v *versions) kept(id agent.ID, key versionKey) (versionResult, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	got, ok := v.known[id]

	return got, ok && got.key == key
}

// read runs the agent's `<command> --version` and keeps what it gave, unless the run was
// cut short, so the next ask tries again.
func (v *versions) read(ctx context.Context, a agent.Agent, path string, key versionKey) versionResult {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	stdout, stderr, err := v.run.Run(ctx, path, "--version")
	got := versionResult{key: key}

	switch {
	case ctx.Err() != nil:
		got.err = fmt.Sprintf("%s --version didn't answer in time", a.Command)

		return got
	case err != nil:
		got.err = fmt.Sprintf("%s --version failed: %s", a.Command, failure(stderr, err))
	default:
		got.version = versionPattern.FindString(execpath.FirstLine(stdout))
		if got.version == "" {
			got.err = fmt.Sprintf("%s --version printed no version", a.Command)
		}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.known == nil {
		v.known = map[agent.ID]versionResult{}
	}

	v.known[a.ID] = got

	return got
}

// failure says why a run failed: the first line it wrote to stderr, or the error.
func failure(stderr []byte, err error) string {
	if line := execpath.FirstLine(stderr); line != "" {
		return line
	}

	return err.Error()
}

// buildOf returns the key of the build at path, following links to the real file.
func buildOf(path string) (versionKey, error) {
	build, err := filepath.EvalSymlinks(path)
	if err != nil {
		return versionKey{}, fmt.Errorf("resolve %s: %w", path, err)
	}

	info, err := os.Stat(build)
	if err != nil {
		return versionKey{}, fmt.Errorf("read %s: %w", build, err)
	}

	return versionKey{path: build, size: info.Size(), mod: info.ModTime()}, nil
}

// versionText is how a bug report gives an agent's version: the version, "not
// installed", or "unknown". It never names a path or an error, which can hold the home
// directory.
func versionText(a AgentVersion) string {
	switch {
	case !a.Installed:
		return "not installed"
	case a.Version == "":
		return "unknown"
	default:
		return a.Version
	}
}
