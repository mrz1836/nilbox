//go:build mage

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
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

	ldflags := versionFlags(readBuildMeta())

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

// buildBundle builds the production app bundle at bundle, the steps App.Build runs once
// it knows it is on macOS.
func buildBundle(bundle string) error {
	if err := checkBrandFiles(); err != nil {
		return err
	}

	if err := (Frontend{}).Build(); err != nil {
		return err
	}

	contents := filepath.Join(bundle, "Contents")
	exe := filepath.Join(contents, "MacOS", brand.Slug)

	if err := os.RemoveAll(bundle); err != nil {
		return fmt.Errorf("remove the previous bundle: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("create the bundle: %w", err)
	}

	meta := readBuildMeta()

	if err := buildDesktopBinary(exe, meta); err != nil {
		return err
	}

	if err := writeInfoPlist(filepath.Join(contents, "Info.plist"), meta); err != nil {
		return err
	}

	if err := copyIconSet(filepath.Join(contents, "Resources")); err != nil {
		return err
	}

	if err := sh.RunV("codesign", "--force", "--sign", "-", bundle); err != nil {
		return fmt.Errorf("sign the bundle ad hoc: %w", err)
	}

	return reportSize(bundle, exe)
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

// quitRunningApp asks every process running exe to quit, with SIGTERM, which the app
// handles as a normal quit, and waits up to timeout until they have exited. It reports
// whether any was running.
func quitRunningApp(exe string, timeout time.Duration) (bool, error) {
	pids, err := appProcesses(exe)
	if err != nil || len(pids) == 0 {
		return false, err
	}

	fmt.Printf("Quitting the running %s...\n", brand.Name) //nolint:forbidigo // mage target output

	for _, pid := range pids {
		if err = syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return true, fmt.Errorf("quit the running app (pid %d): %w", pid, err)
		}
	}

	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(appQuitPoll) {
		if pids, err = appProcesses(exe); err != nil || len(pids) == 0 {
			return true, err
		}
	}

	return true, errStillRunning
}

// openAfterQuit opens bundle just after a running copy of the app quit. LaunchServices
// can still list the old copy for a moment after its process exits, and an open in that
// window fails with error -600 (procNotFound), so it tries again until timeout, quietly,
// and reports the last failure only if none succeeds.
func openAfterQuit(bundle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		out, err := exec.CommandContext(context.Background(), "open", bundle).CombinedOutput() //nolint:gosec // argv only, no shell
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("open %s: %w: %s", bundle, err, strings.TrimSpace(string(out)))
		}

		time.Sleep(appReopenPoll)
	}
}

// appProcesses returns the IDs of the processes whose command line starts with exe.
func appProcesses(exe string) ([]int, error) {
	pattern := "^" + regexp.QuoteMeta(exe) + "( |$)"

	out, err := exec.CommandContext(context.Background(), "pgrep", "-f", pattern).Output() //nolint:gosec // argv only, no shell
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 { // pgrep exits 1 when nothing matches
			return nil, nil
		}

		return nil, fmt.Errorf("find the running app: %w", err)
	}

	var pids []int

	for field := range strings.FieldsSeq(string(out)) {
		pid, convErr := strconv.Atoi(field)
		if convErr != nil {
			return nil, fmt.Errorf("parse the pgrep output %q: %w", field, convErr)
		}

		pids = append(pids, pid)
	}

	return pids, nil
}

// Check runs every desktop check: the bindings drift check, the desktop lint, and the
// bundle build.
//
//	magex app:check
func (App) Check() error {
	if err := (App{}).BindingsCheck(); err != nil {
		return err
	}

	if err := (App{}).Lint(); err != nil {
		return err
	}

	return (App{}).Build()
}

// generateBindings writes the TypeScript bindings for the desktop command into out,
// running the wails3 CLI at the version go.mod pins.
func generateBindings(out string) error {
	if err := ensureDist(); err != nil {
		return err
	}

	cli, err := wailsCLI()
	if err != nil {
		return err
	}

	err = sh.RunV("go", "run", cli, "generate", "bindings",
		"-ts", "-i", "-f", "-tags "+desktopTag, "-d", out, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("generate bindings: %w", err)
	}

	return tidyBindings(out)
}

// tidyBindings rewrites each generated file under root the way the repository keeps it
// (see trimTrailingSpace and unionEnums), so the committed bindings, a fresh generation,
// and the drift check stay identical. A file that is already tidy is left untouched.
func tidyBindings(root string) error {
	files, err := treeFiles(root)
	if err != nil {
		return err
	}

	for rel, body := range files {
		tidy := unionEnums(trimTrailingSpace(body))
		if bytes.Equal(tidy, body) {
			continue
		}

		if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), tidy, 0o644); err != nil { //nolint:gosec // generated sources, world-readable
			return fmt.Errorf("tidy %s: %w", rel, err)
		}
	}

	return nil
}

// trimTrailingSpace strips trailing spaces and tabs from every line of body. The
// generator leaves them on blank doc-comment lines, which the repository's whitespace
// check rejects.
func trimTrailingSpace(body []byte) []byte {
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		lines[i] = bytes.TrimRight(line, " \t")
	}

	return bytes.Join(lines, []byte("\n"))
}

// enumZero is the member the generator adds to every enum for the Go zero value.
const enumZero = "$zero"

var (
	// enumStart matches the first line of a generated enum, capturing its name.
	enumStart = regexp.MustCompile(`^export enum ([\w$]+) \{$`)

	// enumMember matches one member of a generated enum, capturing its indent, name,
	// and value.
	enumMember = regexp.MustCompile(`^(\s+)([\w$]+) = (.+),$`)
)

// unionEnums rewrites each enum in a generated file as a type that is the union of its
// values, and a const object of the same name that maps each Go constant's name to its
// value. The generator renders a Go string type with constants as a TypeScript enum,
// which the frontend's erasableSyntaxOnly rejects and which a plain 'pr' is not
// assignable to; the union keeps the values checked while literals still type-check.
// The enum's $zero member is left out: it is not one of the constants, and a type whose
// zero value means something declares a constant for it.
func unionEnums(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		start := enumStart.FindStringSubmatch(lines[i])
		if start == nil {
			out = append(out, lines[i])

			continue
		}

		end := slices.Index(lines[i+1:], "};")
		if end < 0 {
			// Not an enum as the generator writes one: leave the rest as it is.
			out = append(out, lines[i:]...)

			break
		}

		out = append(out, unionEnum(start[1], lines[i+1:i+1+end])...)
		i += end + 1
	}

	return []byte(strings.Join(out, "\n"))
}

// unionEnum returns the union type and the const object for the enum named name whose
// body (the lines between its braces) is given. Each member keeps the comments above
// it; the $zero member goes with its comment and the blank lines after it.
func unionEnum(name string, body []string) []string {
	var (
		members, pending []string
		values           []string
		afterZero        bool
	)

	for _, line := range body {
		member := enumMember.FindStringSubmatch(line)

		switch {
		case member == nil && afterZero && strings.TrimSpace(line) == "":
			continue
		case member == nil:
			pending = append(pending, line)
		case member[2] == enumZero:
			pending, afterZero = nil, true
		default:
			members = append(members, pending...)
			members = append(members, member[1]+member[2]+": "+member[3]+",")
			pending, afterZero = nil, false

			if !slices.Contains(values, member[3]) {
				values = append(values, member[3])
			}
		}
	}

	members = append(members, pending...)

	out := make([]string, 0, len(members)+8)
	out = append(out,
		"export type "+name+" = "+strings.Join(values, " | ")+";",
		"",
		"/**",
		" * The values of "+name+", by the names of their Go constants.",
		" */",
		"export const "+name+" = {",
	)
	out = append(out, members...)

	return append(out, "} as const;")
}

// wailsCLI returns the go-run package path of the wails3 CLI at the pinned version.
func wailsCLI() (string, error) {
	version, err := sh.Output("go", "list", "-m", "-f", "{{.Version}}", wailsModule)
	if err != nil {
		return "", fmt.Errorf("read the pinned %s version: %w", wailsModule, err)
	}

	return wailsModule + "/cmd/wails3@" + strings.TrimSpace(version), nil
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

// treeDiff compares two directory trees by relative path and content, and returns one
// line per added, removed, or changed file.
func treeDiff(want, got string) ([]string, error) {
	wantFiles, err := treeFiles(want)
	if err != nil {
		return nil, err
	}

	gotFiles, err := treeFiles(got)
	if err != nil {
		return nil, err
	}

	var drift []string

	for rel, body := range gotFiles {
		old, ok := wantFiles[rel]

		switch {
		case !ok:
			drift = append(drift, "missing: "+rel)
		case !bytes.Equal(old, body):
			drift = append(drift, "changed: "+rel)
		}
	}

	for rel := range wantFiles {
		if _, ok := gotFiles[rel]; !ok {
			drift = append(drift, "stale: "+rel)
		}
	}

	slices.Sort(drift)

	return drift, nil
}

// treeFiles reads every regular file under root, keyed by slash-separated relative path.
// A missing root is an empty tree.
func treeFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		body, err := os.ReadFile(path) //nolint:gosec // walking this repository's own generated files
		if err != nil {
			return err
		}

		files[filepath.ToSlash(rel)] = body

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return files, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}

	return files, nil
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

// stopProcessGroup stops a process started in its own group, and everything it started.
func stopProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(devServerStopTimeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// buildMeta is the version metadata stamped into a build.
type buildMeta struct {
	version       string // the build's version, e.g. v0.1.0-3-gabc1234
	commit        string // the short commit hash
	date          string // the build time, RFC 3339 UTC
	bundleVersion string // the release version macOS shows, e.g. 0.1.0 (CFBundleShortVersionString)
	bundleBuild   string // the build number, the commit count, e.g. 42 (CFBundleVersion)
}

// readBuildMeta derives the version metadata from git; outside a git checkout it falls
// back to development values.
//
// The build number is the commit count, so it grows with every commit, tagged or not.
// macOS keys its icon caches on the bundle version (CFBundleVersion), and a build number
// that never changed would leave Stage Manager and Finder showing a stale icon.
func readBuildMeta() buildMeta {
	meta := buildMeta{
		version:       "dev",
		commit:        "none",
		date:          time.Now().UTC().Format(time.RFC3339),
		bundleVersion: brandgen.PlaceholderVersion,
		bundleBuild:   brandgen.PlaceholderVersion,
	}

	if out := gitOutput("describe", "--tags", "--always", "--dirty"); out != "" {
		meta.version = out
	}

	if out := gitOutput("rev-parse", "--short", "HEAD"); out != "" {
		meta.commit = out
	}

	if out := gitOutput("describe", "--tags", "--abbrev=0"); out != "" {
		meta.bundleVersion = strings.TrimPrefix(out, "v")
	}

	if out := gitOutput("rev-list", "--count", "HEAD"); out != "" {
		meta.bundleBuild = out
	}

	return meta
}

// gitOutput returns git's trimmed output, or "" when git fails (for example, describe in
// a repository without tags); its error output is discarded.
func gitOutput(args ...string) string {
	out, err := exec.CommandContext(context.Background(), "git", args...).Output() //nolint:gosec // argv only, no shell
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// versionFlags returns the linker flags that stamp the build's version metadata into the
// desktop command, which reports it on the About page.
func versionFlags(meta buildMeta) string {
	return fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.buildDate=%s -X main.release=%s -X main.buildNumber=%s",
		meta.version, meta.commit, meta.date, meta.bundleVersion, meta.bundleBuild)
}

// buildDesktopBinary compiles the production desktop binary into exe.
func buildDesktopBinary(exe string, meta buildMeta) error {
	minVersion := "-mmacosx-version-min=" + macOSMinVersion
	env := map[string]string{
		"CGO_ENABLED":              "1",
		"CGO_CFLAGS":               minVersion,
		"CGO_LDFLAGS":              minVersion,
		"MACOSX_DEPLOYMENT_TARGET": macOSMinVersion,
	}

	err := sh.RunWithV(env, "go", "build",
		"-tags", desktopTag+",production", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w "+versionFlags(meta), "-o", exe, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("build the desktop binary: %w", err)
	}

	return nil
}

// writeInfoPlist copies the generated Info.plist into the bundle and stamps the build's
// release version and build number into the copy; the committed file keeps its
// placeholder version.
func writeInfoPlist(dst string, meta buildMeta) error {
	body, err := os.ReadFile(filepath.Join("build", "darwin", "Info.plist"))
	if err != nil {
		return fmt.Errorf("read the Info.plist: %w", err)
	}

	if err = os.WriteFile(dst, body, 0o644); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("write the bundle Info.plist: %w", err)
	}

	stamps := []struct{ key, value string }{
		{"CFBundleShortVersionString", meta.bundleVersion},
		{"CFBundleVersion", meta.bundleBuild},
	}

	for _, s := range stamps {
		if err = sh.Run("plutil", "-replace", s.key, "-string", s.value, dst); err != nil {
			return fmt.Errorf("stamp %s: %w", s.key, err)
		}
	}

	return nil
}

// checkBrandFiles fails when a file generated from internal/brand, or an app icon
// rendered from its artwork, is out of date.
func checkBrandFiles() error {
	if err := brandgen.Check("."); err != nil {
		return fmt.Errorf("generated brand files: %w", err)
	}

	if err := brandgen.CheckIcons("."); err != nil {
		return fmt.Errorf("app icons: %w", err)
	}

	return nil
}

// copyIconSet copies the committed app icon set into the bundle's resources directory,
// under the name the Info.plist gives it.
func copyIconSet(resources string) error {
	body, err := os.ReadFile(filepath.FromSlash(brandgen.IconSetPath))
	if err != nil {
		return fmt.Errorf("read the app icon set: %w", err)
	}

	if err = os.MkdirAll(resources, 0o755); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("create the bundle resources: %w", err)
	}

	dst := filepath.Join(resources, brandgen.IconFile+".icns")
	if err = os.WriteFile(dst, body, 0o644); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("write the bundle app icon: %w", err)
	}

	return nil
}

// reportSize prints the built bundle's path and its binary's size.
func reportSize(bundle, exe string) error {
	info, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("read the binary size: %w", err)
	}

	//nolint:forbidigo // mage target output
	fmt.Printf("✅ Built %s (binary %d bytes, %.1f MB)\n", bundle, info.Size(), float64(info.Size())/1e6)

	return nil
}
