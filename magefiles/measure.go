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
	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
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
	_, exe, err := appBundle()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := measure.TimeStartups(ctx, startupRuns, func(ctx context.Context) (measure.Startup, error) {
		return measure.LaunchApp(ctx, exe)
	})
	if err != nil {
		return fmt.Errorf("measure startup: %w", err)
	}

	return printLines(report.Lines())
}

// Refresh times five live inbox refreshes through the app binary, against a scratch
// database, and reports the median and the API budget used at a five-minute cadence.
// Every request is a read made with your GitHub CLI login.
//
//	magex measure:refresh
func (Measure) Refresh() error {
	_, exe, err := appBundle()
	if err != nil {
		return err
	}

	return sh.RunV(exe, measure.RefreshFlag)
}

// Launch times one real launch in iTerm2, from the launch request to the agent running in
// the right directory with its prompt, on a placeholder pull request in a throwaway clone;
// the agent is stopped as soon as it is found. When macOS has not allowed this process to
// control iTerm2, it says so and succeeds without a number. agent= picks the coding agent,
// claude (the default) or codex.
//
//	magex measure:launch
//	magex measure:launch agent=codex
func (Measure) Launch() error {
	if runtime.GOOS != "darwin" {
		return errNeedsMacOS
	}

	id := agent.ID(mageArg("agent"))
	if _, err := agent.Lookup(id); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := measure.RunLaunch(ctx, measure.LaunchOptions{
		Home: home, Agent: id, Version: readBuildMeta().version,
	})
	if err != nil {
		return fmt.Errorf("measure the launch: %w", err)
	}

	if report.NeedsAutomation {
		say("NEEDS AUTOMATION PERMISSION: allow this terminal to control iTerm2 in System Settings → " +
			"Privacy & Security → Automation, then run magex measure:launch again")
	}

	return printLines(report.Lines())
}

// Size reports the size of the app binary and of the whole bundle.
//
//	magex measure:size
func (Measure) Size() error {
	bundle, exe, err := appBundle()
	if err != nil {
		return err
	}

	report, err := measure.Sizes(bundle, exe)
	if err != nil {
		return err
	}

	return printLines(report.Lines())
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

// appBundle returns the built app bundle and its executable, or an error naming the
// build step when it is missing.
func appBundle() (bundle, exe string, err error) {
	if runtime.GOOS != "darwin" {
		return "", "", errNeedsMacOS
	}

	bundle = filepath.Join("bin", brand.Name+".app")
	exe = filepath.Join(bundle, "Contents", "MacOS", brand.Slug)

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
func printLines(lines []string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
			return fmt.Errorf("print the report: %w", err)
		}
	}

	return nil
}

// say prints one line of target output.
func say(line string) {
	_, _ = fmt.Fprintln(os.Stdout, line)
}

// mageArg returns the value of the key=value argument magex handed the target for key,
// or "" when there is none.
func mageArg(key string) string {
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(mageArgsJSON)), &args); err != nil {
		return ""
	}

	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, key+"="); ok {
			return value
		}
	}

	return ""
}
