package testutil

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeToolEnvPrefix starts the environment variable that tells a fake tool, by its name,
// where its script is; fakeGrandchildEnv carries a fake grandchild's script itself.
const (
	fakeToolEnvPrefix = "FAKE_TOOL_"
	fakeGrandchildEnv = "FAKE_TOOL_GRANDCHILD"
)

// FakeRun is what a fake tool does when it runs, in this order: ignore signals, start a
// grandchild, report its stdin or open descriptors, change Files, write Stdout and Stderr,
// sleep, and exit with Exit. A recorded fixture's {"stdout", "stderr", "exit"} reads
// straight into it (see LoadFakeRun).
type FakeRun struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`

	// Files are changed in order before it writes, as a tool that writes, moves, or
	// removes files does: go-pre-commit installing a hook, or fixing a file.
	Files []FakeFile `json:"files,omitempty"`

	// Sleep is how long it waits after writing, before it exits.
	Sleep time.Duration `json:"sleep,omitempty"`

	// IgnoreSignals makes it ignore SIGINT, SIGTERM, and SIGHUP, so only SIGKILL ends it.
	IgnoreSignals bool `json:"ignoreSignals,omitempty"`

	// Grandchild, when set, is started first, sharing the fake's output.
	Grandchild *FakeGrandchild `json:"grandchild,omitempty"`

	// ReportStdin writes "stdin: <n> bytes, devnull: <bool>" to stdout: what it read
	// from stdin, and whether stdin is /dev/null.
	ReportStdin bool `json:"reportStdin,omitempty"`

	// ReportFDs writes "fds: 0,1,2" to stdout: every descriptor open in it.
	ReportFDs bool `json:"reportFds,omitempty"`
}

// FakeFile is one change a fake tool makes to the file at Path, absolute or relative to the
// folder the tool runs in, such as the clone a run starts in: it renames From to Path when
// From is set, removes Path when Remove is set, and otherwise writes Content to Path with
// Mode (0o644 when zero), keeping the mode of a file already there, as os.WriteFile does.
type FakeFile struct {
	Path    string      `json:"path"`
	Content string      `json:"content,omitempty"`
	Mode    os.FileMode `json:"mode,omitempty"`
	From    string      `json:"from,omitempty"`
	Remove  bool        `json:"remove,omitempty"`
}

// change makes the change f describes.
func (f FakeFile) change() error {
	switch {
	case f.From != "":
		return os.Rename(f.From, f.Path)
	case f.Remove:
		return os.Remove(f.Path)
	default:
		return os.WriteFile(f.Path, []byte(f.Content), cmp.Or(f.Mode, 0o644))
	}
}

// FakeGrandchild is a process a fake tool leaves behind: it writes its PID to PIDFile,
// then sleeps, holding the fake's output open.
type FakeGrandchild struct {
	PIDFile string        `json:"pidFile"`
	Sleep   time.Duration `json:"sleep"`

	// Setsid starts it in a session of its own, outside the fake's process group, as
	// an agent or daemon does.
	Setsid bool `json:"setsid,omitempty"`

	// IgnoreSignals makes it ignore SIGINT, SIGTERM, and SIGHUP.
	IgnoreSignals bool `json:"ignoreSignals,omitempty"`
}

// FakeTool makes a fake tool called name: a symbolic link in a new temporary folder that
// points at this test binary, which plays run when started under that name. It returns
// the link's path and the environment entry the code under test must pass to it, so tests
// stay parallel; the package's TestMain must call FakeToolMain first. No script is
// written, and no executable either, so parallel tests never meet ETXTBSY.
func FakeTool(t testing.TB, name string, run FakeRun) (path, env string) {
	t.Helper()

	exe, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	path = filepath.Join(dir, name)
	require.NoError(t, os.Symlink(exe, path))

	script := filepath.Join(dir, name+".fake.json")
	body, err := json.Marshal(run)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(script, body, 0o600))

	return path, fakeToolEnv(name) + "=" + script
}

// LoadFakeRun reads a recorded fixture, {"stdout", "stderr", "exit"} with any other
// fields ignored, as a FakeRun.
func LoadFakeRun(t testing.TB, path string) FakeRun {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	var run FakeRun
	require.NoError(t, json.Unmarshal(body, &run))

	return run
}

// FakeToolMain plays a fake tool and exits when this test binary was started as one;
// otherwise it returns at once. Call it first in TestMain.
func FakeToolMain() {
	if script := os.Getenv(fakeGrandchildEnv); script != "" {
		os.Exit(playGrandchild(script))
	}

	script := os.Getenv(fakeToolEnv(filepath.Base(os.Args[0])))
	if script == "" {
		return
	}

	body, err := os.ReadFile(script) //nolint:gosec // the script FakeTool wrote
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake tool:", err)

		os.Exit(1)
	}

	var run FakeRun
	if err = json.Unmarshal(body, &run); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake tool:", err)

		os.Exit(1)
	}

	os.Exit(play(run))
}

// fakeToolEnv returns the environment variable naming the fake tool name's script.
func fakeToolEnv(name string) string {
	return fakeToolEnvPrefix + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}

		return '_'
	}, name)
}

// play does what run says and returns the exit status.
func play(run FakeRun) int {
	if run.IgnoreSignals {
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	}

	if run.Grandchild != nil {
		if err := startGrandchild(*run.Grandchild); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "fake tool:", err)

			return 1
		}
	}

	if run.ReportStdin {
		reportStdin()
	}

	if run.ReportFDs {
		_, _ = fmt.Fprintf(os.Stdout, "fds: %s\n", openFDs())
	}

	for _, f := range run.Files {
		if err := f.change(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "fake tool:", err)

			return 1
		}
	}

	_, _ = os.Stdout.WriteString(run.Stdout)
	_, _ = os.Stderr.WriteString(run.Stderr)

	time.Sleep(run.Sleep)

	return run.Exit
}

// startGrandchild starts this binary again as g, sharing this process's output, and
// leaves it running.
func startGrandchild(g FakeGrandchild) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	body, err := json.Marshal(g)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe) //nolint:gosec,noctx // test support: the grandchild outlives this process on purpose

	cmd.Env = append(os.Environ(), fakeGrandchildEnv+"="+string(body))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = grandchildAttr(g.Setsid)

	return cmd.Start()
}

// playGrandchild records the grandchild's PID, then sleeps.
func playGrandchild(script string) int {
	var g FakeGrandchild
	if err := json.Unmarshal([]byte(script), &g); err != nil {
		return 1
	}

	if g.IgnoreSignals {
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	}

	if err := os.WriteFile(g.PIDFile, []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
		return 1
	}

	time.Sleep(g.Sleep)

	return 0
}

// reportStdin reads stdin to its end and says how much it read and whether it is
// /dev/null.
func reportStdin() {
	n, _ := os.Stdin.Read(make([]byte, 1))

	stdin, inErr := os.Stdin.Stat()
	devNull, nullErr := os.Stat(os.DevNull)
	same := inErr == nil && nullErr == nil && os.SameFile(stdin, devNull)

	_, _ = fmt.Fprintf(os.Stdout, "stdin: %d bytes, devnull: %v\n", n, same)
}
