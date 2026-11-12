//go:build mage

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// wailsModule is the desktop runtime's Go module; go.mod pins its version, and the
	// wails3 CLI always runs at that same version.
	wailsModule = "github.com/wailsapp/wails/v3"

	// bindingsDir holds the committed TypeScript bindings for the bound services.
	bindingsDir = "frontend/bindings"

	// distIndex is the built frontend's entry page, which the desktop embed needs.
	distIndex = "frontend/dist/index.html"

	// desktopTag selects the desktop shell; without it the command builds headless.
	desktopTag = "desktop"

	// devServerURL is the frontend dev server (see frontend/vite.config.ts).
	devServerURL = "http://127.0.0.1:9245"

	// macOSMinVersion is the oldest macOS the app supports; Go's own objects target it.
	macOSMinVersion = "13.0"

	// lintVersionKey names the pinned golangci-lint version in the MAGE-X env file.
	lintVersionKey = "MAGE_X_GOLANGCI_LINT_VERSION"

	// mageXEnvFile holds the MAGE-X tool pins.
	mageXEnvFile = ".github/env/10-mage-x.env"

	// placeholderPage stands in for the built frontend until `pnpm build` runs, so the
	// desktop embed always has a file to match.
	placeholderPage = "<!doctype html>\n"

	// devServerStopTimeout bounds how long Dev waits for the dev server to exit.
	devServerStopTimeout = 5 * time.Second

	// appQuitTimeout bounds how long Run waits for a running copy of the app to quit.
	appQuitTimeout = 10 * time.Second

	// appQuitPoll is how often Run checks whether the running copy has quit.
	appQuitPoll = 100 * time.Millisecond

	// appReopenTimeout bounds how long Run keeps trying to open the fresh build after
	// quitting a running copy (see openAfterQuit).
	appReopenTimeout = 5 * time.Second

	// appReopenPoll is how long Run waits between those tries.
	appReopenPoll = 250 * time.Millisecond
)

var (
	// errNeedsMacOS is returned by targets that build the macOS app bundle.
	errNeedsMacOS = errors.New("this target builds the macOS app and requires macOS")

	// errBindingsDrift is returned when the committed bindings differ from the generated ones.
	errBindingsDrift = errors.New("frontend/bindings is out of date; run magex app:bindings")

	// errNotPinned is returned when a tool's version pin cannot be found.
	errNotPinned = errors.New("the tool version is not pinned")

	// errStillRunning is returned when a running copy of the app does not quit in time.
	errStillRunning = errors.New("the running app did not quit; quit it and run the target again")
)

// App groups the targets for the desktop app: bindings, lint, dev run, and building and
// opening the macOS bundle.
type App mg.Namespace

// Bindings regenerates the TypeScript bindings for the services the desktop shell binds.
//
//	magex app:bindings
func (App) Bindings() error {
	return generateBindings(bindingsDir)
}

// BindingsCheck regenerates the bindings into a temporary directory and fails, listing
// the files, when they differ from the committed ones.
//
//	magex app:bindingsCheck
func (App) BindingsCheck() error {
	tmp, err := os.MkdirTemp("", brand.Slug+"-bindings-")
	if err != nil {
		return fmt.Errorf("create a temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	fresh := filepath.Join(tmp, "bindings")
	if err = generateBindings(fresh); err != nil {
		return err
	}

	drift, err := treeDiff(bindingsDir, fresh)
	if err != nil {
		return err
	}

	if len(drift) > 0 {
		return fmt.Errorf("%w:\n  %s", errBindingsDrift, strings.Join(drift, "\n  "))
	}

	fmt.Println("✅ frontend/bindings is up to date") //nolint:forbidigo // mage target output

	return nil
}

// Lint lints the desktop-tagged Go code (the desktop main and the frontend embed) with
// the golangci-lint version MAGE-X pins; `magex lint` covers everything else.
//
//	magex app:lint
func (App) Lint() error {
	if err := ensureDist(); err != nil {
		return err
	}

	version, err := lintVersion()
	if err != nil {
		return err
	}

	args := []string{
		"run", "--build-tags", desktopTag, "--config", ".golangci.json", "--timeout", "10m",
		"./cmd/" + brand.Slug + "/...", "./frontend/",
	}

	if lintOnPath(version) {
		return sh.RunV("golangci-lint", args...)
	}

	return sh.RunV("go", append([]string{"run", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@" + version}, args...)...)
}

// Dev runs the desktop app against the frontend dev server, with hot reload, and stops
// the dev server when the app exits.
//
//	magex app:dev
func (App) Dev() error {
	if err := (Frontend{}).Install(); err != nil {
		return err
	}

	if err := ensureDist(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	vite, err := pnpmCommand(ctx, frontendDir, "dev")
	if err != nil {
		return err
	}

	vite.Stdout = os.Stdout
	vite.Stderr = os.Stderr
	vite.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // stop pnpm and the server it starts together

	if err = vite.Start(); err != nil {
		return fmt.Errorf("start the frontend dev server: %w", err)
	}

	defer stopProcessGroup(vite)

	ldflags := versionFlags(readBuildMeta()) + quietLinkerFlags()

	app := exec.CommandContext(ctx, "go", "run", "-tags", desktopTag, "-ldflags", ldflags, "./cmd/"+brand.Slug) //nolint:gosec // argv only, no shell

	app.Env = append(os.Environ(), "FRONTEND_DEVSERVER_URL="+devServerURL, "CGO_ENABLED=1")
	app.Stdout = os.Stdout
	app.Stderr = os.Stderr

	// go run starts the app as its own child, so stopping go run alone would leave the
	// app running: run both in one group and stop the group.
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	app.Cancel = func() error { return syscall.Kill(-app.Process.Pid, syscall.SIGTERM) }
	app.WaitDelay = devServerStopTimeout

	if err = app.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run the desktop app: %w", err)
	}

	return nil
}

// Build builds the production macOS app bundle, bin/<Name>.app: it checks the generated
// brand files and app icons, builds the frontend, compiles the desktop binary, adds the
// Info.plist with this build's version and the app icon, and signs the bundle ad hoc for
// local use. Distribution signing
// and notarization are not part of this target.
//
//	magex app:build
func (App) Build() error {
	if runtime.GOOS != "darwin" {
		return errNeedsMacOS
	}

	return buildBundle(filepath.Join("bin", brand.Name+".app"))
}

// Open opens the macOS app bundle, bin/<Name>.app, building it first when it is missing.
// It opens the bundle as it is, so use `magex app:run` to pick up a change; when the app
// is already running, macOS brings its window to the front instead.
//
//	magex app:open
func (App) Open() error {
	bundle, _, err := appBundle()
	if errors.Is(err, errNoBundle) {
		if err = (App{}).Build(); err != nil {
			return err
		}

		bundle, _, err = appBundle()
	}

	if err != nil {
		return err
	}

	return sh.RunV("open", bundle)
}

// Run builds the macOS app bundle and opens it. It quits a copy of the app that is
// already running first, so the fresh build is the one that opens; otherwise macOS would
// bring the old copy to the front.
//
//	magex app:run
func (App) Run() error {
	if err := (App{}).Build(); err != nil {
		return err
	}

	bundle, exe, err := appBundle()
	if err != nil {
		return err
	}

	quit, err := quitRunningApp(exe, appQuitTimeout)
	if err != nil {
		return err
	}

	if quit {
		return openAfterQuit(bundle, appReopenTimeout)
	}

	return sh.RunV("open", bundle)
}

// Test runs the desktop-tagged tests of the desktop command, which run the desktop
// runtime itself (a bound method's error reaching the window worded, for one); `magex
// test` builds without the tag, so it never runs them.
//
//	magex app:test
func (App) Test() error {
	if err := ensureDist(); err != nil {
		return err
	}

	return sh.RunV("go", "test", "-tags", desktopTag, "./cmd/"+brand.Slug+"/")
}

// Check runs every desktop check: the bindings drift check, the desktop lint, the
// desktop tests, and the bundle build.
//
//	magex app:check
func (App) Check() error {
	if err := (App{}).BindingsCheck(); err != nil {
		return err
	}

	if err := (App{}).Lint(); err != nil {
		return err
	}

	if err := (App{}).Test(); err != nil {
		return err
	}

	return (App{}).Build()
}

// ensureDist writes a placeholder frontend/dist/index.html when the frontend has not been
// built, because the desktop embed must match at least one file.
func ensureDist() error {
	if _, err := os.Stat(distIndex); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(distIndex), 0o755); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("create frontend/dist: %w", err)
	}

	if err := os.WriteFile(distIndex, []byte(placeholderPage), 0o644); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("write the placeholder frontend: %w", err)
	}

	return nil
}

// lintVersion returns the golangci-lint version MAGE-X pins.
func lintVersion() (string, error) {
	return pinnedVersion(mageXEnvFile, lintVersionKey)
}

// pinnedVersion returns the tool version that key pins: the environment's value when set,
// else the first non-empty value file assigns it.
func pinnedVersion(file, key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}

	f, err := os.Open(file) //nolint:gosec // one of this repository's own env files
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), key+"="); ok && v != "" {
			return v, nil
		}
	}

	if err = scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}

	return "", fmt.Errorf("%w: %s has no %s", errNotPinned, file, key)
}

// lintOnPath reports whether golangci-lint on PATH is the pinned version, so it can run
// directly instead of being built.
func lintOnPath(version string) bool {
	out, err := sh.Output("golangci-lint", "version")
	if err != nil {
		return false
	}

	return strings.Contains(out, "version "+strings.TrimPrefix(version, "v")+" ")
}
