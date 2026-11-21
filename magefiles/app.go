//go:build mage

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/magefile/mage/mg"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// goosMacOS is runtime.GOOS on macOS, the one system the macOS-only targets run on.
	goosMacOS = "darwin"

	// wailsModule is the desktop runtime's Go module; go.mod pins its version, and the
	// wails3 CLI always runs at that same version.
	wailsModule = "github.com/wailsapp/wails/v3"

	// bindingsDir holds the committed TypeScript bindings for the bound services.
	bindingsDir = "frontend/bindings"

	// distIndex is the built frontend's entry page, which the desktop embed needs.
	distIndex = "frontend/dist/index.html"

	// desktopTag selects the desktop shell; without it the command builds headless.
	desktopTag = "desktop"

	// devtoolsTag compiles in the inspector (internal/devtools), which only a build from
	// source carries: app:dev's and the bundle app:build makes. A release, or a rehearsal
	// of one, never does (buildTags).
	devtoolsTag = "devtools"

	// devServerURL is the frontend dev server (see frontend/vite.config.ts).
	devServerURL = "http://127.0.0.1:9245"

	// macOSMinVersion is the oldest macOS the app supports; Go's own objects target it.
	macOSMinVersion = "13.3"

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

	// devBundle is the bundle a build from source goes into (app:build, app:run, app:open,
	// app:fresh, app:reset, measure:*), named for brand.DevName, so it is never taken for the
	// release in an Applications folder. It runs on the user's own data, as the release does.
	devBundle = "bin/" + brand.DevName + ".app"

	// legacyBundle is where a build from source went before it was named brand.DevName: under
	// the release's name and bundle ID, which Spotlight and macOS took for the release.
	legacyBundle = "bin/" + brand.Name + ".app"
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
	return hostRunner().generateBindings(bindingsDir)
}

// BindingsCheck regenerates the bindings into a temporary directory and fails, listing
// the files, when they differ from the committed ones.
//
//	magex app:bindingsCheck
func (App) BindingsCheck() error {
	return hostRunner().checkBindings()
}

// checkBindings does App.BindingsCheck's work.
func (r runner) checkBindings() error {
	tmp, err := os.MkdirTemp(r.tmp, brand.Slug+"-bindings-")
	if err != nil {
		return fmt.Errorf("create a temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	fresh := filepath.Join(tmp, "bindings")
	if err = r.generateBindings(fresh); err != nil {
		return err
	}

	drift, err := treeDiff(r.path(bindingsDir), fresh)
	if err != nil {
		return err
	}

	if len(drift) > 0 {
		return fmt.Errorf("%w:\n  %s", errBindingsDrift, strings.Join(drift, "\n  "))
	}

	r.say("✅ frontend/bindings is up to date")

	return nil
}

// Lint lints the desktop-tagged Go code (the desktop main and the frontend embed) with
// the golangci-lint version MAGE-X pins; `magex lint` covers everything else.
//
//	magex app:lint
func (App) Lint() error {
	return hostRunner().lintDesktop()
}

// lintDesktop does App.Lint's work.
func (r runner) lintDesktop() error {
	if err := r.ensureDist(); err != nil {
		return err
	}

	version, err := r.lintVersion()
	if err != nil {
		return err
	}

	onPath := r.lintOnPath(version)

	// The desktop code builds two ways, with the inspector and without it, and each is
	// linted.
	for _, tags := range desktopTagSets() {
		args := []string{
			"run", "--build-tags", tags, "--config", ".golangci.json", "--timeout", "10m",
			"./cmd/" + brand.Slug + "/...", "./frontend/",
		}

		if onPath {
			err = r.runV("golangci-lint", args...)
		} else {
			err = r.runV("go", append([]string{"run", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@" + version}, args...)...)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// desktopTagSets are the build tags the desktop code is linted and tested with: the
// desktop shell as a release builds it, and with the inspector a build from source
// carries.
func desktopTagSets() []string {
	return []string{desktopTag, desktopTag + "," + devtoolsTag}
}

// Dev runs the desktop app against the frontend dev server, with hot reload, and stops
// the dev server when the app exits. With fresh=true it runs on a fresh sandbox data
// folder, as app:fresh opens, so the first-run setup shows: for building the setup itself.
//
//	magex app:dev
//	magex app:dev fresh=true
func (App) Dev() error {
	r := hostRunner()

	appArgs, err := r.prepareDev(r.mageArg("fresh") == "true", appdata.Resolve)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return r.runDev(ctx, appArgs)
}

// prepareDev does what App.Dev does before it starts anything: it installs the frontend's
// dependencies, gives the desktop embed a page, and returns the app's arguments
// (devAppArgs), finding the user's data with resolve.
func (r runner) prepareDev(fresh bool, resolve func() (appdata.Paths, error)) ([]string, error) {
	if err := r.installFrontend(); err != nil {
		return nil, err
	}

	if err := r.ensureDist(); err != nil {
		return nil, err
	}

	return r.devAppArgs(fresh, resolve)
}

// runDev starts the frontend dev server, then runs the desktop app against it with
// appArgs until the app exits or ctx ends, and stops the dev server then.
func (r runner) runDev(ctx context.Context, appArgs []string) error {
	vite, err := r.pnpmCommand(ctx, "dev")
	if err != nil {
		return err
	}

	vite.Stdout = r.stdout
	vite.Stderr = r.stderr
	vite.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // stop pnpm and the server it starts together

	if err = vite.Start(); err != nil {
		return fmt.Errorf("start the frontend dev server: %w", err)
	}

	defer stopProcessGroup(vite)

	ldflags := versionFlags(r.readBuildMeta(ctx)) + r.quietLinkerFlags(ctx)

	goArgs := append([]string{"run", "-tags", buildTags(""), "-ldflags", ldflags, "./cmd/" + brand.Slug}, appArgs...)
	app := r.cmd(ctx, []string{"FRONTEND_DEVSERVER_URL=" + devServerURL, "CGO_ENABLED=1"}, "go", goArgs...)

	app.Stdout = r.stdout
	app.Stderr = r.stderr

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

// devAppArgs are the arguments app:dev passes the app: none, or, with fresh, --data-dir
// naming a fresh sandbox data folder, made as app:fresh makes one from the user's data,
// which resolve finds.
func (r runner) devAppArgs(fresh bool, resolve func() (appdata.Paths, error)) ([]string, error) {
	if !fresh {
		return nil, nil
	}

	paths, err := resolve()
	if err != nil {
		return nil, fmt.Errorf("find the app's data: %w", err)
	}

	dir, err := freshDataDir(paths, r.tempDir())
	if err != nil {
		return nil, err
	}

	r.say("Running on fresh data in " + dir + "; your own data is untouched.")

	return []string{appdata.DataDirFlag + "=" + dir}, nil
}

// Build builds the production macOS app bundle from source, bin/<DevName>.app: it checks
// the generated brand files and app icons, builds the frontend, compiles the desktop binary,
// adds the Info.plist with this build's version, the build from source's name and bundle ID,
// and the app icon, and signs the bundle ad hoc for local use. Then it removes
// bin/<Name>.app, where builds from source went before they were named apart from the
// release, unless a copy still runs from it. Distribution signing and notarization are not
// part of this target.
//
//	magex app:build
func (App) Build() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	return hostRunner().buildApp()
}

// buildApp does App.Build's work once it knows it is on macOS.
func (r runner) buildApp() error {
	if err := r.buildBundle(r.path(devBundle)); err != nil {
		return err
	}

	return r.removeLegacyBundle(r.path(legacyBundle))
}

// removeLegacyBundle removes legacy, the folder an earlier build from source went into
// under the release's name and bundle ID, so Spotlight and Open With stop offering it for
// the release. A copy still running from it is left alone, and named, for the next build
// to remove; nothing there, or a link, is nothing to remove.
func (r runner) removeLegacyBundle(legacy string) error {
	info, err := os.Lstat(legacy)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("look for the earlier build %s: %w", legacy, err)
	case !info.IsDir():
		return nil
	}

	exe, err := filepath.Abs(bundleExecutable(legacy))
	if err != nil {
		return fmt.Errorf("resolve %s: %w", legacy, err)
	}

	pids, err := r.appProcesses(exe)
	if err != nil {
		return err
	}

	if len(pids) > 0 {
		r.say(legacy + ", an earlier build from source under " + brand.Name + "'s own name, still runs: quit it, " +
			"and the next build removes it.")

		return nil
	}

	if err = os.RemoveAll(legacy); err != nil {
		return fmt.Errorf("remove the earlier build %s: %w", legacy, err)
	}

	r.say("Removed " + legacy + ", an earlier build from source under " + brand.Name + "'s own name and bundle ID.")

	return nil
}

// Open opens the macOS app bundle, bin/<DevName>.app, building it first when it is missing.
// It opens the bundle as it is, so use `magex app:run` to pick up a change; when the app
// is already running, macOS brings its window to the front instead.
//
//	magex app:open
func (App) Open() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	return hostRunner().openApp()
}

// openApp does App.Open's work once it knows it is on macOS.
func (r runner) openApp() error {
	bundle, _, err := r.builtBundle()
	if err != nil {
		return err
	}

	return r.runV("open", bundle)
}

// Run builds the macOS app bundle from source and opens it. It quits a copy of that build
// already running first, so the fresh build is the one that opens; otherwise macOS would
// bring the old copy to the front. The release is another app to macOS, and is left alone.
//
//	magex app:run
func (App) Run() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	return hostRunner().runApp()
}

// runApp does App.Run's work once it knows it is on macOS.
func (r runner) runApp() error {
	if err := r.buildApp(); err != nil {
		return err
	}

	bundle, exe, err := r.appBundle()
	if err != nil {
		return err
	}

	quit, err := r.quitRunningApp(exe, appQuitTimeout)
	if err != nil {
		return err
	}

	if quit {
		return r.openAfterQuit(bundle, appReopenTimeout)
	}

	return r.runV("open", bundle)
}

// Test runs the desktop-tagged tests of the desktop command, which run the desktop
// runtime itself (a bound method's error reaching the window worded, for one); `magex
// test` builds without the tag, so it never runs them.
//
//	magex app:test
func (App) Test() error {
	return hostRunner().testDesktop()
}

// testDesktop does App.Test's work.
func (r runner) testDesktop() error {
	if err := r.ensureDist(); err != nil {
		return err
	}

	for _, tags := range desktopTagSets() {
		if err := r.runV("go", "test", "-tags", tags, "./cmd/"+brand.Slug+"/"); err != nil {
			return err
		}
	}

	return nil
}

// Check runs every desktop check: the bindings drift check, the desktop lint, the
// desktop tests, and the bundle build.
//
//	magex app:check
func (App) Check() error {
	return hostRunner().checkDesktop(runtime.GOOS)
}

// checkDesktop does App.Check's work on goos, the system it runs on, whose bundle build,
// as App.Build's, needs macOS.
func (r runner) checkDesktop(goos string) error {
	if err := r.checkBindings(); err != nil {
		return err
	}

	if err := r.lintDesktop(); err != nil {
		return err
	}

	if err := r.testDesktop(); err != nil {
		return err
	}

	if goos != goosMacOS {
		return errNeedsMacOS
	}

	return r.buildApp()
}

// ensureDist writes a placeholder frontend/dist/index.html when the frontend has not been
// built, because the desktop embed must match at least one file.
func (r runner) ensureDist() error {
	index := r.path(distIndex)
	if _, err := os.Stat(index); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(index), 0o755); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("create frontend/dist: %w", err)
	}

	if err := os.WriteFile(index, []byte(placeholderPage), 0o644); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("write the placeholder frontend: %w", err)
	}

	return nil
}

// lintVersion returns the golangci-lint version MAGE-X pins.
func (r runner) lintVersion() (string, error) {
	return r.pinnedVersion(mageXEnvFile, lintVersionKey)
}

// pinnedVersion returns the tool version that key pins: the environment's value when set,
// else the first non-empty value file, in the checkout, assigns it.
func (r runner) pinnedVersion(file, key string) (string, error) {
	if v := r.getenv(key); v != "" {
		return v, nil
	}

	f, err := os.Open(r.path(file))
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
func (r runner) lintOnPath(version string) bool {
	out, err := r.output("golangci-lint", "version")
	if err != nil {
		return false
	}

	return strings.Contains(out, "version "+strings.TrimPrefix(version, "v")+" ")
}
