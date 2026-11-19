//go:build mage

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/magefile/mage/mg"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/measure"
)

// mageArgsJSON is where magex hands a custom target its key=value arguments, as a JSON
// list.
const mageArgsJSON = "MAGE_ARGS_JSON"

// startupRuns is how many launches measure:startup times: the first launch of a build,
// then five cold starts for the median.
const startupRuns = 6

// errNoBundle is returned when the app bundle has not been built.
var errNoBundle = errors.New("the app bundle is missing; run magex app:build first")

// Measure groups the targets that record the numbers in docs/performance.md. Each runs
// against the bundle magex app:build produced and prints key=value lines.
type Measure mg.Namespace

// Startup launches the app six times and reports each launch's time from process start
// to the first frame on screen: the first launch of the build on its own, then the median
// of the other five.
//
//	magex measure:startup
func (Measure) Startup() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return hostRunner().measureStartup(ctx, measure.LaunchApp)
}

// measureStartup does Measure.Startup's work once it knows it is on macOS: it times
// startupRuns launches of the bundle's executable, each one launch's.
func (r runner) measureStartup(ctx context.Context, launch func(ctx context.Context, exe string) (measure.Startup, error)) error {
	_, exe, err := r.appBundle()
	if err != nil {
		return err
	}

	report, err := measure.TimeStartups(ctx, startupRuns, func(ctx context.Context) (measure.Startup, error) {
		return launch(ctx, exe)
	})
	if err != nil {
		return fmt.Errorf("measure startup: %w", err)
	}

	return r.printLines(report.Lines())
}

// Refresh times five live inbox refreshes through the app binary, against a scratch
// database, and reports the median and the API budget used at a five-minute cadence.
// With searches=N (1 to 5), each refresh also reads N routine searches over your account
// beside the inbox's own, as routines would, and the report adds each watch pass's time
// after the inbox's store and its cost. Every request is a read made with your GitHub CLI
// login.
//
//	magex measure:refresh
//	magex measure:refresh searches=5
func (Measure) Refresh() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	return hostRunner().measureRefresh()
}

// measureRefresh does Measure.Refresh's work once it knows it is on macOS.
func (r runner) measureRefresh() error {
	_, exe, err := r.appBundle()
	if err != nil {
		return err
	}

	if searches := r.mageArg("searches"); searches != "" {
		return r.runV(exe, measure.RefreshFlag, measure.SearchesFlag+searches)
	}

	return r.runV(exe, measure.RefreshFlag)
}

// Launch times one real launch in iTerm2, from the launch request to the agent running in
// the right directory with its prompt, on a placeholder pull request in a throwaway clone;
// the agent is stopped as soon as it is found, and the time is split between the launch's
// steps. When macOS has not allowed this process to control iTerm2, it says so and
// succeeds without a number. agent= picks the coding agent, claude (the default) or codex.
//
//	magex measure:launch
//	magex measure:launch agent=codex
func (Measure) Launch() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return hostRunner().measureLaunch(ctx, os.UserHomeDir, measure.RunLaunch)
}

// measureLaunch does Measure.Launch's work once it knows it is on macOS: it measures the
// launch with run, for the agent its agent= argument names, from the home folder home
// finds.
func (r runner) measureLaunch(ctx context.Context, home func() (string, error),
	run func(ctx context.Context, opts measure.LaunchOptions) (measure.LaunchReport, error),
) error {
	id := agent.ID(r.mageArg("agent"))
	if _, err := agent.Lookup(id); err != nil {
		return err
	}

	dir, err := home()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}

	report, err := run(ctx, measure.LaunchOptions{
		Home: dir, Agent: id, Version: r.readBuildMeta(ctx).version,
	})
	if err != nil {
		return fmt.Errorf("measure the launch: %w", err)
	}

	if report.NeedsAutomation {
		r.say("NEEDS AUTOMATION PERMISSION: allow this terminal to control iTerm2 in System Settings → " +
			"Privacy & Security → Automation, then run magex measure:launch again")
	}

	return r.printLines(report.Lines())
}

// Size reports the size of the app binary and of the whole bundle.
//
//	magex measure:size
func (Measure) Size() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	return hostRunner().measureSize()
}

// measureSize does Measure.Size's work once it knows it is on macOS.
func (r runner) measureSize() error {
	bundle, exe, err := r.appBundle()
	if err != nil {
		return err
	}

	report, err := measure.Sizes(bundle, exe)
	if err != nil {
		return err
	}

	return r.printLines(report.Lines())
}

// All runs every measurement: size, startup, refresh, and launch.
//
//	magex measure:all
func (Measure) All() error {
	m := Measure{}

	for _, step := range []func() error{m.Size, m.Startup, m.Refresh, m.Launch} {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// appBundle returns the app bundle built from source in the checkout, devBundle, and its
// executable, or an error naming the build step when it is missing.
func (r runner) appBundle() (bundle, exe string, err error) {
	bundle = r.path(devBundle)
	exe = bundleExecutable(bundle)

	if _, err = os.Stat(exe); err != nil {
		return "", "", fmt.Errorf("%w (%s)", errNoBundle, exe)
	}

	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", exe, err)
	}

	return bundle, abs, nil
}

// printLines prints a report's lines.
func (r runner) printLines(lines []string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(r.stdout, line); err != nil {
			return fmt.Errorf("print the report: %w", err)
		}
	}

	return nil
}

// say prints one line of a step's output.
func (r runner) say(line string) {
	_, _ = fmt.Fprintln(r.stdout, line)
}

// mageArg returns the value of the key=value argument magex handed the target for key,
// or "" when there is none.
func (r runner) mageArg(key string) string {
	var args []string
	if err := json.Unmarshal([]byte(r.getenv(mageArgsJSON)), &args); err != nil {
		return ""
	}

	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, key+"="); ok {
			return value
		}
	}

	return ""
}
