package integrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errExit stands for a program that exited non-zero, and errNoProgram for one that isn't
// there.
var (
	errExit      = errors.New("exit status 1")
	errNoProgram = errors.New("no such program")
)

// The versions the tests' copies report: the recorded release, and a newer one.
const (
	recordedMagex = "1.29.5"
	newerMagex    = "1.30.2"
)

// answer is what a fake copy prints when run.
type answer struct {
	stdout, stderr string
	err            error

	// hang makes it wait until its time limit ends.
	hang bool
}

// magexSays is what a magex at version prints.
func magexSays(version string) answer {
	return answer{stdout: "Version Information\n  Version:      " + version + "\n  Commit:       9a61f25\n"}
}

// machine is a fake machine: tool folders in a temporary folder, and what each copy
// prints, so no test looks in the real tool folders or runs a real tool.
type machine struct {
	t    *testing.T
	root string
	home string

	// pathA and pathB are on the shell's PATH, in that order; brew and goBin are searched
	// after it, and goBin is go install's.
	pathA, pathB, brew, goBin string

	mu      sync.Mutex
	answers map[string]answer
	envs    map[string][]string
	args    map[string][]string
}

func newMachine(t *testing.T) *machine {
	t.Helper()

	root := t.TempDir()
	m := &machine{
		t: t, root: root, home: filepath.Join(root, "home"),
		pathA: filepath.Join(root, "usr", "local", "bin"), pathB: filepath.Join(root, "home", ".local", "bin"),
		brew: filepath.Join(root, "opt", "homebrew", "bin"), goBin: filepath.Join(root, "home", "go", "bin"),
		answers: map[string]answer{}, envs: map[string][]string{}, args: map[string][]string{},
	}

	for _, dir := range []string{m.pathA, m.pathB, m.brew, m.goBin} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	return m
}

// dirs is where the machine's tools are looked for.
func (m *machine) dirs() searchDirs {
	return searchDirs{
		path: []string{m.pathA, m.pathB}, extra: []string{m.brew, m.goBin}, goBins: goFolders{dirs: []string{m.goBin}},
	}
}

// put writes an executable magex in dir that answers a.
func (m *machine) put(dir string, a answer) string {
	m.t.Helper()

	path := filepath.Join(dir, "magex")
	require.NoError(m.t, os.MkdirAll(dir, 0o700))
	testutil.WriteFile(m.t, path, "")
	require.NoError(m.t, os.Chmod(path, 0o700)) //nolint:gosec // a fake tool must be executable

	m.mu.Lock()
	m.answers[path] = a
	m.mu.Unlock()

	return path
}

// link makes a symbolic link at path to target, answering as target does.
func (m *machine) link(path, target string) {
	m.t.Helper()

	require.NoError(m.t, os.Symlink(target, path))

	m.mu.Lock()
	m.answers[path] = m.answers[target]
	m.mu.Unlock()
}

// run answers as the copy at path, recording the environment it was given.
func (m *machine) run(ctx context.Context, path string, args, env []string) ([]byte, []byte, error) {
	m.mu.Lock()
	a, ok := m.answers[path]
	m.envs[path] = env
	m.args[path] = args
	m.mu.Unlock()

	// The copies the test puts are empty files that answer as recorded. A file with
	// something in it, one an install downloaded or placed over a put copy, prints what it
	// holds, so a replaced copy answers as the new program.
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 { //nolint:gosec // the test's own temporary files
		return data, nil, nil
	}

	if !ok {
		return nil, nil, errNoProgram
	}

	if a.hang {
		<-ctx.Done()

		return nil, nil, ctx.Err()
	}

	if filepath.Base(path) != "go" {
		assert.Equal(m.t, []string{"--version"}, args)
	}

	return []byte(a.stdout), []byte(a.stderr), a.err
}

// detect looks for magex on the machine.
func (m *machine) detect(t *testing.T, chosen string) detection {
	t.Helper()

	return detector{def: MageX(), dirs: m.dirs(), env: []string{"HOME=" + m.home}, home: m.home, run: m.run}.detect(t.Context(), chosen)
}

// newerRelease is a latest release newer than the recorded one.
func newerRelease() latestRecord {
	return latestRecord{Version: newerMagex}
}

func TestDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(m *machine) (chosen string)
		latest latestRecord
		check  func(t *testing.T, m *machine, d detection, state State)
	}{
		{
			name:  "found",
			setup: func(m *machine) string { m.put(m.pathB, magexSays(recordedMagex)); return "" },
			check: func(t *testing.T, m *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.Equal(t, filepath.Join(m.pathB, "magex"), d.selected.Path)
				assert.Equal(t, "~/.local/bin/magex", d.selected.DisplayPath)
				assert.Equal(t, Version{Raw: recordedMagex, Commit: "9a61f25"}, d.selected.Version)
				assert.Equal(t, SourceRelease, d.selected.Source)
				assert.True(t, d.selected.OnPath)
				assert.False(t, d.selected.Link)
				assert.Empty(t, d.others)
				assert.NotZero(t, d.stat.Inode, "its inode is kept, to tell a replaced file")
			},
		},
		{
			name:   "update available",
			setup:  func(m *machine) string { m.put(m.pathB, magexSays(recordedMagex)); return "" },
			latest: newerRelease(),
			check: func(t *testing.T, _ *machine, _ detection, state State) {
				t.Helper()

				assert.Equal(t, StateUpdate, state)
			},
		},
		{
			name:   "too old",
			setup:  func(m *machine) string { m.put(m.pathB, magexSays("1.20.0")); return "" },
			latest: newerRelease(),
			check: func(t *testing.T, _ *machine, _ detection, state State) {
				t.Helper()

				assert.Equal(t, StateTooOld, state, "older than the minimum, before any update")
			},
		},
		{
			name: "won't run",
			setup: func(m *machine) string {
				m.put(m.pathB, answer{stderr: "\ndyld: Library not loaded: libfoo\nmore\n", err: errExit})
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateWontRun, state)
				assert.False(t, d.selected.Runs)
				assert.Equal(t, "dyld: Library not loaded: libfoo", d.selected.Error, "its first error line")
			},
		},
		{
			name: "won't run: no version",
			setup: func(m *machine) string {
				m.put(m.pathB, answer{stdout: "MAGE-X banner\nUsage: magex [target]\n"})
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateWontRun, state)
				assert.Equal(t, "it printed no version", d.selected.Error, "never its banner")
			},
		},
		{
			name: "won't run: no version, with a warning",
			setup: func(m *machine) string {
				m.put(m.pathB, answer{stdout: "MAGE-X banner\n", stderr: "warning: unknown flag --version\n"})
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, _ State) {
				t.Helper()

				assert.Equal(t, "warning: unknown flag --version", d.selected.Error)
			},
		},
		{
			name: "missing",
			setup: func(m *machine) string {
				testutil.WriteFile(m.t, filepath.Join(m.pathA, "magex"), "") // not executable
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateMissing, state)
				assert.False(t, d.has)
			},
		},
		{
			name: "chosen copy, shadowed",
			setup: func(m *machine) string {
				m.put(m.pathA, magexSays("1.27.0"))
				return m.put(filepath.Join(m.root, "custom"), magexSays(newerMagex))
			},
			latest: newerRelease(),
			check: func(t *testing.T, m *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.True(t, d.chosen)
				assert.Equal(t, filepath.Join(m.root, "custom", "magex"), d.selected.Path)
				assert.False(t, d.selected.OnPath, "the shell doesn't find the chosen copy")
				require.Len(t, d.others, 1)
				assert.Equal(t, filepath.Join(m.pathA, "magex"), d.others[0].Path)
				assert.Equal(t, "1.27.0", d.others[0].Version.Raw, "each other copy is run for its version")
				assert.True(t, d.others[0].Shadowing, "the user's terminal runs this one, not the one in use")
			},
		},
		{
			name: "chosen copy gone",
			setup: func(m *machine) string {
				m.put(m.pathB, magexSays(recordedMagex))
				return filepath.Join(m.root, "custom", "magex")
			},
			check: func(t *testing.T, m *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.True(t, d.chosenMissing)
				assert.False(t, d.chosen)
				assert.Equal(t, filepath.Join(m.pathB, "magex"), d.selected.Path, "the one the shell finds instead")
			},
		},
		{
			name: "a link to a development build",
			setup: func(m *machine) string {
				target := m.put(filepath.Join(m.home, "projects", "mage-x", "bin"),
					magexSays("1.29.6-0.20261105041738-9a61f25f00aa+dirty"))
				m.link(filepath.Join(m.pathB, "magex"), target)

				return ""
			},
			latest: newerRelease(),
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state, "a development build is never offered an update")
				assert.True(t, d.selected.Link)
				assert.Equal(t, "~/projects/mage-x/bin/magex", d.selected.Target)
				assert.Equal(t, SourceDevelopment, d.selected.Source)
			},
		},
		{
			name: "development build",
			setup: func(m *machine) string {
				m.put(m.pathA, magexSays("dev"))
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state, "never too old: its version can't say")
				assert.Equal(t, SourceDevelopment, d.selected.Source)
			},
		},
		{
			name: "Homebrew's Cellar",
			setup: func(m *machine) string {
				target := m.put(filepath.Join(m.root, "opt", "homebrew", "Cellar", "magex", recordedMagex, "bin"),
					magexSays(recordedMagex))
				m.link(filepath.Join(m.brew, "magex"), target)

				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.Equal(t, SourceHomebrew, d.selected.Source)
				assert.True(t, d.selected.Link)
				assert.False(t, d.selected.OnPath, "found in the well-known folders, after the PATH")
			},
		},
		{
			name: "GOBIN",
			setup: func(m *machine) string {
				m.put(m.goBin, magexSays(recordedMagex))
				return ""
			},
			check: func(t *testing.T, _ *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.Equal(t, SourceGoInstall, d.selected.Source)
			},
		},
		{
			name: "the first on the PATH, with the others after it",
			setup: func(m *machine) string {
				m.put(m.pathA, magexSays("1.30.0"))
				m.put(m.pathB, magexSays(recordedMagex))
				m.put(m.goBin, magexSays("1.27.0"))
				m.link(filepath.Join(m.brew, "magex"), filepath.Join(m.pathA, "magex"))

				return ""
			},
			check: func(t *testing.T, m *machine, d detection, state State) {
				t.Helper()

				assert.Equal(t, StateFound, state)
				assert.Equal(t, filepath.Join(m.pathA, "magex"), d.selected.Path)

				others := make([]string, 0, len(d.others))
				for _, c := range d.others {
					others = append(others, c.DisplayPath+" "+c.Version.Raw+" "+string(c.Source))
					assert.False(t, c.Shadowing, "the copy in use is the one the terminal runs")
				}

				assert.Equal(t, []string{"~/.local/bin/magex 1.29.5 release", "~/go/bin/magex 1.27.0 goInstall"}, others,
					"in the order found, and a link to a copy already found isn't another copy")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newMachine(t)
			d := m.detect(t, tt.setup(m))
			tt.check(t, m, d, stateOf(MageX(), d, tt.latest))
		})
	}
}

func TestDetectionRunsWithTheToolsQuietEnvironment(t *testing.T) {
	t.Parallel()

	m := newMachine(t)
	path := m.put(m.pathA, magexSays(recordedMagex))

	m.detect(t, "")

	m.mu.Lock()
	env := m.envs[path]
	m.mu.Unlock()

	assert.Equal(t, []string{"HOME=" + m.home, "MAGEX_NO_UPDATE_CHECK=1", "NO_COLOR=1"}, env,
		"the shell's environment, then no update banner and no color")
}

func TestDetectionGivesUpOnACopyThatHangs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := newMachine(t)
		m.put(m.pathA, answer{hang: true})

		d := m.detect(t, "")

		assert.Equal(t, StateWontRun, stateOf(MageX(), d, latestRecord{}))
		assert.Equal(t, "it didn't answer within 5 seconds", d.selected.Error)
	})
}

func TestDetectionRunsRealPrograms(t *testing.T) {
	t.Parallel()

	found, foundEnv := testutil.FakeTool(t, "magex", testutil.LoadFakeRun(t, filepath.Join("testdata", "magex-1.29.5", "version.json")))
	broken, brokenEnv := testutil.FakeTool(t, "go-pre-commit", testutil.FakeRun{Stderr: "go-pre-commit: bad CPU type\n", Exit: 3})
	env := append(os.Environ(), foundEnv, brokenEnv)
	dirs := searchDirs{path: []string{filepath.Dir(found), filepath.Dir(broken)}}

	got := detectAll(t.Context(), Definitions(), nil, dirs, env, t.TempDir(), execRun)

	magex := got["mage-x"]
	require.True(t, magex.has)
	assert.True(t, magex.selected.Runs)
	assert.Equal(t, recordedMagex, magex.selected.Version.Raw, "the recorded banner, read from a real program's output")

	gopc := got["go-pre-commit"]
	require.True(t, gopc.has)
	assert.False(t, gopc.selected.Runs)
	assert.Equal(t, "go-pre-commit: bad CPU type", gopc.selected.Error)
}

func TestDirsFrom(t *testing.T) {
	t.Parallel()

	home := "/Users/example"

	// The well-known tool folders are execpath's, which differ by OS (macOS has some, Linux
	// none), so they come from it rather than from this test.
	wellKnown := filepath.SplitList(execpath.FallbackPath(home, ""))

	tests := []struct {
		name       string
		env        []string
		wantPath   []string
		wantGoBins goFolders
	}{
		{
			"no go settings",
			[]string{"PATH=/usr/bin:relative/bin::/bin:/usr/bin"},
			[]string{"/usr/bin", "/bin"},
			goFolders{dirs: []string{"/Users/example/go/bin"}},
		},
		{
			"GOBIN, with GOPATH's default too",
			[]string{"GOBIN=/opt/go/bin"},
			[]string{},
			goFolders{dirs: []string{"/opt/go/bin", "/Users/example/go/bin"}},
		},
		{
			"GOPATH",
			[]string{"GOPATH=/opt/go:/srv/go:"},
			[]string{},
			goFolders{dirs: []string{"/opt/go/bin", "/srv/go/bin", "/Users/example/go/bin"}},
		},
		{
			"every one, relative ones apart",
			[]string{"GOBIN= rel ", "GOPATH=/opt/go:../tools"},
			[]string{},
			goFolders{dirs: []string{"/opt/go/bin", "/Users/example/go/bin"}, rel: []string{"rel", "../tools/bin"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dirsFrom(tt.env, home)
			assert.Equal(t, tt.wantPath, got.path, "the shell's PATH, absolute folders once each")
			assert.Equal(t, tt.wantGoBins, got.goBins)

			assert.Equal(t, absDirs(append(slices.Clone(wellKnown), tt.wantGoBins.dirs...)), got.extra,
				"the well-known folders, then go install's, each once")
		})
	}
}

func TestGoFoldersHaveOneRule(t *testing.T) {
	t.Parallel()

	root := testutil.RealTempDir(t)
	work := filepath.Join(root, "work")
	linked := filepath.Join(root, "linked")
	require.NoError(t, os.MkdirAll(filepath.Join(work, "bin"), 0o700))
	require.NoError(t, os.Symlink(work, linked))

	var bins goFolders

	bins.add(filepath.Join(root, "gobin"), linked+string(filepath.ListSeparator)+"..")
	bins.add(filepath.Join(root, "gobin"), "")

	assert.Equal(t, []string{filepath.Join(root, "gobin"), filepath.Join(linked, "bin")}, bins.dirs, "each once")
	assert.True(t, bins.has(filepath.Join(root, "gobin")), "$GOBIN")
	assert.True(t, bins.has(filepath.Join(work, "bin")), "a $GOPATH folder's bin, through a link")
	assert.True(t, bins.has(filepath.Join(root, "tools", "bin")), "../bin, taken from a bin folder itself")
	assert.False(t, bins.has(filepath.Join(root, "tools")))
}

func TestDetectionRunsAtMostFourCopiesAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := newMachine(t)

		dirs := make([]string, 0, 6)

		for i := range 6 {
			dir := filepath.Join(m.root, "bin"+strconv.Itoa(i))
			dirs = append(dirs, dir)
			m.put(dir, magexSays(recordedMagex))
			executable(t, filepath.Join(dir, "go-pre-commit"))
		}

		var (
			mu            sync.Mutex
			running, most int
		)

		run := func(context.Context, string, []string, []string) ([]byte, []byte, error) {
			mu.Lock()
			running++
			most = max(most, running)
			mu.Unlock()

			time.Sleep(time.Second)

			mu.Lock()
			running--
			mu.Unlock()

			return []byte(magexSays(recordedMagex).stdout), nil, nil
		}

		got := detectAll(t.Context(), Definitions(), nil, searchDirs{path: dirs}, nil, m.home, run)

		require.True(t, got["mage-x"].has)
		require.True(t, got["go-pre-commit"].has)
		assert.Len(t, got["mage-x"].others, 5)
		assert.Equal(t, versionParallel, most, "four at a time across both tools, never more")
	})
}

// executable writes an empty executable file at path.
func executable(t *testing.T, path string) {
	t.Helper()

	testutil.WriteFile(t, path, "")
	require.NoError(t, os.Chmod(path, 0o700)) //nolint:gosec // a fake tool must be executable
}
