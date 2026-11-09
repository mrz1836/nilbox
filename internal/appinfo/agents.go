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

// versionTimeout bounds one command's --version, which a command that hangs would
// otherwise hold the About page on.
const versionTimeout = 5 * time.Second

// versionPattern finds a version in what a command's --version prints, as in
// "2.1.319 (Claude Code)", "codex-cli 0.163.0", or "gh version 2.83.1 (2026-10-01)".
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

// ToolVersion is the installed version of one tool the app runs, other than the coding
// agents, as its command reports it.
type ToolVersion struct {
	// Tool is its command, such as gh, and Name its name, such as GitHub CLI.
	Tool string `json:"tool"`
	Name string `json:"name"`

	// Installed reports that its command was found.
	Installed bool `json:"installed"`

	// Path is where its command is, with ~ for the home directory, or "".
	Path string `json:"path"`

	// Version is the version the command reports, such as 2.83.1, or "" when it could not
	// be read.
	Version string `json:"version"`

	// Error says why the version could not be read, or is "".
	Error string `json:"error"`
}

// program is a command whose version the About page shows: a coding agent or a tool.
type program struct {
	// key names it among the versions kept, and name and command are what it is called
	// and the command that runs it.
	key     string
	name    string
	command string
}

// tools are the tools, other than the coding agents, whose versions the About page and a
// bug report give: the GitHub CLI, whose sign-in the app borrows.
func tools() []program {
	return []program{{key: "gh", name: "GitHub CLI", command: "gh"}}
}

// installed is what is known of one program's command: whether it was found, where, and
// the version it reports, or why there is none.
type installed struct {
	found   bool
	path    string
	version string
	err     string
}

// versionKey identifies one build of a command: its real path, size, and modification
// time. A program that updates itself replaces its command, or points its link at a new
// one, which changes the key, so a version is never shown stale.
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

// versions reads the coding agents' and the tools' versions and keeps each one until its
// command changes.
type versions struct {
	finder execpath.Finder
	run    execpath.Runner
	home   string

	// calls runs one --version per program at a time, for every caller asking then.
	calls flight.Group[versionResult]

	// mu guards known, the versions kept, by program key.
	mu    sync.Mutex
	known map[string]versionResult
}

// all returns every agent's version, in the agents' order, reading the agents in
// parallel.
func (v *versions) all(ctx context.Context) []AgentVersion {
	agents := agent.All()
	out := make([]AgentVersion, len(agents))

	v.each(len(agents), func(i int) {
		a := agents[i]
		got := v.of(ctx, program{key: string(a.ID), name: a.Name, command: a.Command})
		out[i] = AgentVersion{
			Agent: a.ID, Name: a.Name, Installed: got.found, Path: got.path, Version: got.version, Error: got.err,
		}
	})

	return out
}

// tools returns every tool's version, in the tools' order, reading them in parallel.
func (v *versions) tools(ctx context.Context) []ToolVersion {
	list := tools()
	out := make([]ToolVersion, len(list))

	v.each(len(list), func(i int) {
		t := list[i]
		got := v.of(ctx, t)
		out[i] = ToolVersion{
			Tool: t.command, Name: t.name, Installed: got.found, Path: got.path, Version: got.version, Error: got.err,
		}
	})

	return out
}

// each runs read for each of n programs at once, and waits for them all.
func (*versions) each(n int, read func(i int)) {
	var wg sync.WaitGroup

	for i := range n {
		wg.Go(func() {
			read(i)
		})
	}

	wg.Wait()
}

// of returns one program's version: the one kept for its command's current build, or one
// read now.
func (v *versions) of(ctx context.Context, p program) installed {
	var out installed

	if v.finder == nil || v.run == nil {
		return out
	}

	path, err := v.finder.Find(p.command)
	if err != nil {
		return out
	}

	out.found, out.path = true, paths.Tilde(path, v.home)

	key, err := buildOf(path)
	if err != nil {
		out.err = fmt.Sprintf("couldn't read %s: %v", p.command, err)

		return out
	}

	got, ok := v.kept(p.key, key)
	if !ok {
		// A run cut short because its caller left returns that caller's error, so flight
		// counts it abandoned: a caller that joins it as it ends starts it again, rather than
		// taking the cut-short run as its answer.
		got, err = v.calls.Do(ctx, p.key, func(ctx context.Context) (versionResult, error) {
			return v.read(ctx, p, path, key), ctx.Err()
		})
		if err != nil {
			out.err = "the version check was cut short"

			return out
		}
	}

	out.version, out.err = got.version, got.err

	return out
}

// kept returns the version kept for a program, when it was read from the same build.
func (v *versions) kept(key string, build versionKey) (versionResult, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	got, ok := v.known[key]

	return got, ok && got.key == build
}

// read runs the program's `<command> --version` and keeps what it gave, unless the run
// was cut short, so the next ask tries again.
func (v *versions) read(ctx context.Context, p program, path string, key versionKey) versionResult {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	stdout, stderr, err := v.run.Run(ctx, path, "--version")
	got := versionResult{key: key}

	switch {
	case ctx.Err() != nil:
		got.err = fmt.Sprintf("%s --version didn't answer in time", p.command)

		return got
	case err != nil:
		got.err = fmt.Sprintf("%s --version failed: %s", p.command, failure(stderr, err))
	default:
		got.version = versionPattern.FindString(execpath.FirstLine(stdout))
		if got.version == "" {
			got.err = fmt.Sprintf("%s --version printed no version", p.command)
		}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.known == nil {
		v.known = map[string]versionResult{}
	}

	v.known[p.key] = got

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

// versionText is how a bug report gives a program's version: the version, "not
// installed", or "unknown". It never names a path or an error, which can hold the home
// directory.
func versionText(found bool, version string) string {
	switch {
	case !found:
		return "not installed"
	case version == "":
		return "unknown"
	default:
		return version
	}
}
