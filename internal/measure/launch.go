package measure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
)

// The launch fixture: a throwaway clone of a placeholder repository, and one pull request
// from an outside contributor in it, so the measured launch starts in plan mode.
const (
	fixtureOwner  = "acme-org"
	fixtureName   = "example-repo"
	fixtureRemote = "https://github.com/" + fixtureOwner + "/" + fixtureName + ".git"
	fixtureViewer = "octo-user"
	fixtureAuthor = "outside-user"
	fixtureBranch = "feature"
	fixtureTitle  = "Measure launch latency"
	fixtureNumber = 1
	fixtureID     = 1
)

// agentBin is the coding agent a launch runs.
const agentBin = "claude"

// Process inspection programs, at their macOS paths: some shells and the PATH of an app
// started from the Finder leave /usr/sbin out.
const (
	psBin   = "/bin/ps"
	lsofBin = "/usr/sbin/lsof"
	killBin = "/bin/kill"
)

const (
	// defaultAgentTimeout bounds the wait for the agent once the terminal accepted the
	// launch.
	defaultAgentTimeout = 20 * time.Second

	// defaultPoll is how often the process list is checked for the agent.
	defaultPoll = 100 * time.Millisecond
)

// The fixture's account, repository, and pull request.
const (
	seedAccountSQL = `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (?, ?, ?, ?)`
	seedRepoSQL    = `INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	seedItemSQL    = `INSERT INTO items (id, account_id, repo_id, kind, number, title, url, author_login,
	author_association, is_cross_repository, head_ref, item_created_at, item_updated_at,
	reason_review_requested, fetched_at)
VALUES (?, ?, ?, 'pr', ?, ?, ?, ?, 'CONTRIBUTOR', 1, ?, ?, ?, 1, ?)`
)

// ErrAgentNotStarted is returned when the terminal accepted the launch but no agent
// process appeared in the clone in time.
var ErrAgentNotStarted = errors.New("the agent did not start in the clone in time")

// LaunchOptions configure RunLaunch. Home is required.
type LaunchOptions struct {
	// Home is the user's home directory: the agent and its default profile are found there.
	Home string

	// Version is the build's version.
	Version string

	// TempDir is where the scratch clone and data directory are created; "" uses the
	// system default.
	TempDir string

	// AgentTimeout bounds the wait for the agent process; zero means 20 seconds.
	AgentTimeout time.Duration

	// Poll is how often the process list is checked; zero means every 100 ms.
	Poll time.Duration

	// Runner runs external programs: git, the terminal driver, ps, lsof, and kill. nil
	// uses the app's default.
	Runner core.Runner

	// Logger receives the app's log records; nil uses slog.Default().
	Logger *slog.Logger
}

// LaunchReport is one measured launch.
type LaunchReport struct {
	// Elapsed runs from the launch request to the agent process running in the clone with
	// its prompt.
	Elapsed time.Duration

	// PID is the agent process that was found, and then stopped.
	PID int

	// Terminal is the terminal the launch opened in.
	Terminal string

	// Permission summarizes the session's permissions.
	Permission string

	// NeedsAutomation reports that macOS did not let this process control the terminal:
	// the permission was refused, or its dialog was never answered. Nothing was measured.
	NeedsAutomation bool

	// Reason explains NeedsAutomation.
	Reason string
}

// Lines renders the report as key=value lines.
func (r LaunchReport) Lines() []string {
	if r.NeedsAutomation {
		return []string{"launch.needs_automation_permission=true", "launch.reason=" + r.Reason}
	}

	return []string{
		fmt.Sprintf("launch.ms=%s s=%s", milliseconds(r.Elapsed), seconds(r.Elapsed)),
		"launch.terminal=" + r.Terminal,
		"launch.permission=" + r.Permission,
		fmt.Sprintf("launch.pid=%d (stopped)", r.PID),
	}
}

// RunLaunch times one real launch in iTerm2: from the launch request to the agent running
// in the clone with its prompt loaded. It launches on an outside contributor's pull
// request in a throwaway clone of a placeholder repository, so the session starts in plan
// mode, and without a worktree, so the agent fetches nothing. The agent process is
// stopped as soon as it is found; the scratch clone and data directory are deleted.
func RunLaunch(ctx context.Context, opts LaunchOptions) (LaunchReport, error) {
	runner := deadlineRunner{inner: opts.Runner}
	if opts.Runner == nil {
		runner.inner = execpath.Runner{Finder: execpath.Finder{Home: opts.Home}}
	}

	dir, err := os.MkdirTemp(opts.TempDir, brand.Slug+"-measure-")
	if err != nil {
		return LaunchReport{}, fmt.Errorf("create the scratch directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	fx, err := newLaunchFixture(ctx, dir, opts, runner)
	if err != nil {
		return LaunchReport{}, err
	}

	defer fx.close()

	agent, err := execpath.Finder{Home: opts.Home}.Find(agentBin)
	if err != nil {
		return LaunchReport{}, fmt.Errorf("find the agent: %w", err)
	}

	timer := launchTimer{
		launch:  fx.launch,
		probe:   processProbe{run: runner},
		agent:   agent,
		clone:   fx.clone,
		timeout: cmpOr(opts.AgentTimeout, defaultAgentTimeout),
		poll:    cmpOr(opts.Poll, defaultPoll),
		now:     time.Now,
	}

	return timer.run(ctx)
}

// cmpOr returns v, or fallback when v is zero.
func cmpOr(v, fallback time.Duration) time.Duration {
	if v == 0 {
		return fallback
	}

	return v
}

// launchFixture is the app, started over a scratch data directory, with one launchable
// item whose repository is cloned under a scratch clone root.
type launchFixture struct {
	core  *core.Core
	clone string
}

// newLaunchFixture creates the clone, starts the app over a scratch data directory, and
// stores the fixture item, the clone root, and iTerm2 as the terminal.
func newLaunchFixture(ctx context.Context, dir string, opts LaunchOptions, runner core.Runner) (*launchFixture, error) {
	root := filepath.Join(dir, "clones")
	clone := filepath.Join(root, fixtureName)

	if err := initClone(ctx, runner, clone); err != nil {
		return nil, err
	}

	c, err := core.Bootstrap(ctx, core.Options{
		Paths:   appdata.NewPaths(filepath.Join(dir, "data")),
		Home:    opts.Home,
		Version: opts.Version,
		Runner:  runner,
		Logger:  opts.Logger,
	})
	if err != nil {
		return nil, err
	}

	if err = seed(ctx, c, root); err != nil {
		_ = c.Close()

		return nil, err
	}

	// Compare against the clone's real path: the agent's working directory is reported
	// with symbolic links resolved.
	if resolved, evalErr := filepath.EvalSymlinks(clone); evalErr == nil {
		clone = resolved
	}

	return &launchFixture{core: c, clone: clone}, nil
}

// launch starts a session on the fixture item in the clone itself, with no worktree.
func (f *launchFixture) launch(ctx context.Context) (launcher.LaunchResult, error) {
	noWorktree := false

	return f.core.Launcher.Launch(ctx, launcher.LaunchRequest{ItemID: fixtureID, Worktree: &noWorktree})
}

// close closes the app's database.
func (f *launchFixture) close() {
	_ = f.core.Close()
}

// initClone creates an empty git repository at clone whose origin is the placeholder
// repository. Nothing is fetched.
func initClone(ctx context.Context, runner core.Runner, clone string) error {
	if err := os.MkdirAll(clone, 0o700); err != nil {
		return fmt.Errorf("create the scratch clone: %w", err)
	}

	for _, args := range [][]string{
		{"-C", clone, "init", "--quiet"},
		{"-C", clone, "remote", "add", "origin", fixtureRemote},
	} {
		if _, stderr, err := runner.Run(ctx, "git", args...); err != nil {
			return fmt.Errorf("set up the scratch clone: %w%s", err, detail(string(stderr)))
		}
	}

	return nil
}

// seed stores the launch settings, makes sure the default profile exists (it does when the
// home directory has the agent's default config), and inserts the fixture item.
func seed(ctx context.Context, c *core.Core, root string) error {
	if err := c.Settings.SetCloneRoot(ctx, root); err != nil {
		return err
	}

	if err := c.Settings.SetTerminal(ctx, settings.TerminalITerm2); err != nil {
		return err
	}

	defaultProfile := profiles.Profile{
		Name:           profiles.DefaultName,
		PermissionMode: profiles.PermissionFull,
		AddDirs:        []string{},
		ExtraArgs:      []string{},
		Env:            map[string]string{},
	}

	if err := profiles.Sync(ctx, c.DB, []profiles.Profile{defaultProfile}); err != nil {
		return fmt.Errorf("store the default profile: %w", err)
	}

	now := time.Now().UTC()
	url := fmt.Sprintf("https://github.com/%s/%s/pull/%d", fixtureOwner, fixtureName, fixtureNumber)

	err := c.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(seedAccountSQL, fixtureID, fixtureViewer, now, now).Error; err != nil {
			return err
		}

		if err := tx.Exec(seedRepoSQL, fixtureID, fixtureOwner, fixtureName, now, now).Error; err != nil {
			return err
		}

		return tx.Exec(seedItemSQL, fixtureID, fixtureID, fixtureID, fixtureNumber, fixtureTitle, url,
			fixtureAuthor, fixtureBranch, now, now, now).Error
	})
	if err != nil {
		return fmt.Errorf("store the fixture item: %w", err)
	}

	return nil
}

// agentProbe finds and stops agent processes.
type agentProbe interface {
	// PIDs returns the processes running the agent binary.
	PIDs(ctx context.Context, agent string) ([]int, error)

	// Cwds returns the working directory of each of pids that still exists.
	Cwds(ctx context.Context, pids []int) (map[int]string, error)

	// Stop asks a process to exit.
	Stop(ctx context.Context, pid int) error
}

// launchTimer times one launch until the agent runs in the clone.
type launchTimer struct {
	launch  func(context.Context) (launcher.LaunchResult, error)
	probe   agentProbe
	agent   string
	clone   string
	timeout time.Duration
	poll    time.Duration
	now     func() time.Time
}

// run launches, waits for the agent, stops it, and reports the time in between. A
// terminal that macOS would not let this process control is reported, not failed.
func (t launchTimer) run(ctx context.Context) (LaunchReport, error) {
	before, err := t.probe.PIDs(ctx, t.agent)
	if err != nil {
		return LaunchReport{}, err
	}

	start := t.now()

	res, err := t.launch(ctx)
	if err != nil {
		if needsAutomation(err) {
			return LaunchReport{NeedsAutomation: true, Reason: err.Error()}, nil
		}

		return LaunchReport{}, fmt.Errorf("launch: %w", err)
	}

	pid, err := t.waitForAgent(ctx, before)
	if err != nil {
		return LaunchReport{}, err
	}

	report := LaunchReport{Elapsed: t.now().Sub(start), PID: pid, Terminal: res.Terminal, Permission: res.Permission}

	if err = t.probe.Stop(ctx, pid); err != nil {
		return report, fmt.Errorf("stop the measured agent (pid %d): %w", pid, err)
	}

	return report, nil
}

// waitForAgent polls until a new agent process runs in the clone, or the timeout passes.
// Processes running before the launch are ignored.
func (t launchTimer) waitForAgent(ctx context.Context, before []int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	ticker := time.NewTicker(t.poll)
	defer ticker.Stop()

	for {
		pid, found, err := t.findAgent(ctx, before)
		if err != nil && ctx.Err() == nil {
			return 0, err
		}

		if found {
			return pid, nil
		}

		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("%w (waited %s)", ErrAgentNotStarted, t.timeout)
		case <-ticker.C:
		}
	}
}

// findAgent returns a new agent process whose working directory is the clone or one of
// its agent worktrees.
func (t launchTimer) findAgent(ctx context.Context, before []int) (int, bool, error) {
	pids, err := t.probe.PIDs(ctx, t.agent)
	if err != nil {
		return 0, false, err
	}

	pids = slices.DeleteFunc(pids, func(pid int) bool { return slices.Contains(before, pid) })
	if len(pids) == 0 {
		return 0, false, nil
	}

	cwds, err := t.probe.Cwds(ctx, pids)
	if err != nil {
		return 0, false, err
	}

	for _, pid := range pids {
		if inClone(cwds[pid], t.clone) {
			return pid, true, nil
		}
	}

	return 0, false, nil
}

// inClone reports whether dir is the clone or under its agent worktree directory.
func inClone(dir, clone string) bool {
	if dir == "" {
		return false
	}

	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	worktrees := filepath.Join(clone, ".claude", "worktrees") + string(filepath.Separator)

	return dir == clone || strings.HasPrefix(dir, worktrees)
}

// needsAutomation reports whether a launch failed because macOS would not let this
// process control the terminal: refused outright, or a permission dialog left unanswered
// until the terminal driver gave up.
func needsAutomation(err error) bool {
	if errors.Is(err, launcher.ErrAutomationDenied) {
		return true
	}

	return errors.Is(err, launcher.ErrTerminalOpen) && errors.Is(err, context.DeadlineExceeded)
}

// deadlineRunner adds the context's error to a program's failure once its context has
// ended, so a terminal app that never answered is told apart from one that failed.
type deadlineRunner struct {
	inner core.Runner
}

// Run runs the program with the inner runner.
func (r deadlineRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	stdout, stderr, err = r.inner.Run(ctx, name, args...)
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w: %w", err, ctx.Err())
	}

	return stdout, stderr, err
}

// processProbe inspects processes with ps and lsof, and stops one with kill.
type processProbe struct {
	run core.Runner
}

// PIDs lists the processes whose command line starts with the agent binary.
func (p processProbe) PIDs(ctx context.Context, agent string) ([]int, error) {
	out, _, err := p.run.Run(ctx, psBin, "-axww", "-o", "pid=,args=")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}

	return parsePS(string(out), agent), nil
}

// Cwds reads the working directory of each process. lsof exits non-zero when one of them
// has already exited; what it printed for the others still counts.
func (p processProbe) Cwds(ctx context.Context, pids []int) (map[int]string, error) {
	list := make([]string, 0, len(pids))
	for _, pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}

	out, _, err := p.run.Run(ctx, lsofBin, "-a", "-p", strings.Join(list, ","), "-d", "cwd", "-Fn")

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("read working directories: %w", err)
	}

	return parseLsofCwds(string(out)), nil
}

// Stop sends the process SIGTERM.
func (p processProbe) Stop(ctx context.Context, pid int) error {
	if _, _, err := p.run.Run(ctx, killBin, "-TERM", strconv.Itoa(pid)); err != nil {
		return fmt.Errorf("signal the process: %w", err)
	}

	return nil
}

// parsePS reads `ps -o pid=,args=` output and returns the processes whose command line is
// the agent binary, alone or followed by arguments. Lines that do not start with a
// process ID (an argument's own line breaks) are skipped.
func parsePS(out, agent string) []int {
	var pids []int

	for line := range strings.SplitSeq(out, "\n") {
		pidText, args, ok := strings.Cut(strings.TrimLeft(line, " "), " ")
		if !ok {
			continue
		}

		pid, err := strconv.Atoi(pidText)
		if err != nil {
			continue
		}

		args = strings.TrimLeft(args, " ")
		if args == agent || strings.HasPrefix(args, agent+" ") {
			pids = append(pids, pid)
		}
	}

	return pids
}

// parseLsofCwds reads `lsof -d cwd -Fn` output: a "p<pid>" line starts each process and
// its first "n<path>" line names the working directory.
func parseLsofCwds(out string) map[int]string {
	cwds := map[int]string{}
	pid := 0

	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0:
			if _, seen := cwds[pid]; !seen {
				cwds[pid] = line[1:]
			}
		}
	}

	return cwds
}
