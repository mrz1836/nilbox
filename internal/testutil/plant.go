package testutil

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// AlternateRefs is the name of the planting of an alternate refs command, which a fetch
// runs only in a clone with alternates, so Plant gives a clone it plants it in an
// alternate object store to ask about.
const AlternateRefs = "evil-alternate-refs"

// Planting is one way a clone's own config could make git run a program of the clone's:
// the name of the fake program planted, and the settings that name it, each a key and a
// value in which %s stands for the fake's path.
type Planting struct {
	Name     string
	Settings [][2]string
}

// Keys returns the keys of p's settings as git lists them: the section and the name
// lowercased, a subsection as written.
func (p Planting) Keys() []string {
	keys := make([]string, 0, len(p.Settings))

	for _, s := range p.Settings {
		first, last := strings.Index(s[0], "."), strings.LastIndex(s[0], ".")
		keys = append(keys, strings.ToLower(s[0][:first])+s[0][first:last]+strings.ToLower(s[0][last:]))
	}

	return keys
}

// Planter plants programs in the config of clones a test made, the way a clone's own
// config could make git run one, for a guard test to show they never run. Each program is
// a fake (FakeTool) that writes a marker file, named after it, in the folder ran under
// home when it runs. The Planter keeps the environment git runs with, an isolated git's
// over home (GitEnv) with every fake's entry added, which a fake needs to run as one. It
// is safe for concurrent use.
type Planter struct {
	t    testing.TB
	home string

	mu  sync.Mutex
	env []string
}

// NewPlanter returns a Planter whose git runs isolated over home.
func NewPlanter(t testing.TB, home string) *Planter {
	return &Planter{t: t, home: home, env: GitEnv(home)}
}

// Environ returns a copy of the environment git runs with.
func (p *Planter) Environ() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.env)
}

// AddEnv adds entry, a KEY=value, to the environment git runs with.
func (p *Planter) AddEnv(entry string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.env = append(p.env, entry)
}

// Marker makes a fake program called name that writes ran/<name> under home when it runs,
// and returns its path and that file's.
func (p *Planter) Marker(name string) (program, marker string) {
	p.t.Helper()

	ran := filepath.Join(p.home, "ran")
	require.NoError(p.t, os.MkdirAll(ran, 0o700))

	marker = filepath.Join(ran, name)
	program, entry := FakeTool(p.t, name, FakeRun{Files: []FakeFile{{Path: marker, Content: "ran"}}})
	p.AddEnv(entry)

	return program, marker
}

// Plant sets planting in the clone at dir's own config, pointing at a Marker fake, and
// returns its marker file. The planting of AlternateRefs comes with an alternate object
// store, an empty repository's, for a fetch to ask it about.
func (p *Planter) Plant(dir string, planting Planting) string {
	p.t.Helper()

	program, marker := p.Marker(planting.Name)

	for _, s := range planting.Settings {
		RunGit(p.t, dir, "config", s[0], strings.ReplaceAll(s[1], "%s", program))
	}

	if planting.Name == AlternateRefs {
		alt := filepath.Join(p.t.TempDir(), "alt.git")
		RunGit(p.t, p.home, "init", "-q", "--bare", alt)
		WriteFile(p.t, filepath.Join(dir, ".git", "objects", "info", "alternates"), filepath.Join(alt, "objects")+"\n")
	}

	return marker
}

// PlantHooks points the clone at dir's own core.hooksPath at a folder whose hook called
// hook is a Marker fake, and returns its marker file.
func (p *Planter) PlantHooks(dir, hook string) string {
	p.t.Helper()

	program, marker := p.Marker(hook)
	RunGit(p.t, dir, "config", "core.hooksPath", filepath.Dir(program))

	return marker
}
