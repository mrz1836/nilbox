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
	"runtime"
	"slices"
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
)

var (
	// errNeedsMacOS is returned by targets that build the macOS app bundle.
	errNeedsMacOS = errors.New("this target builds the macOS app and requires macOS")

	// errBindingsDrift is returned when the committed bindings differ from the generated ones.
	errBindingsDrift = errors.New("frontend/bindings is out of date; run magex app:bindings")

	// errNoLintVersion is returned when the golangci-lint pin cannot be found.
	errNoLintVersion = errors.New("the golangci-lint version is not pinned")
)

// App groups the targets for the desktop app: bindings, lint, dev run, and the macOS
// bundle.
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

	app := exec.CommandContext(ctx, "go", "run", "-tags", desktopTag, "./cmd/"+brand.Slug) //nolint:gosec // argv only, no shell

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
// brand files, builds the frontend, compiles the desktop binary, adds the Info.plist with
// this build's version, and signs the bundle ad hoc for local use. Distribution signing
// and notarization are not part of this target.
//
//	magex app:build
func (App) Build() error {
	if runtime.GOOS != "darwin" {
		return errNeedsMacOS
	}

	if err := brandgen.Check("."); err != nil {
		return fmt.Errorf("generated brand files: %w", err)
	}

	if err := (Frontend{}).Build(); err != nil {
		return err
	}

	bundle := filepath.Join("bin", brand.Name+".app")
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

	if err := writeInfoPlist(filepath.Join(contents, "Info.plist"), meta.bundleVersion); err != nil {
		return err
	}

	if err := sh.RunV("codesign", "--force", "--sign", "-", bundle); err != nil {
		return fmt.Errorf("sign the bundle ad hoc: %w", err)
	}

	return reportSize(bundle, exe)
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

	return trimTrailingSpace(out)
}

// trimTrailingSpace strips trailing spaces and tabs from every line of the files under
// root. The generator leaves them on blank doc-comment lines, which the repository's
// whitespace check rejects; trimming here keeps the committed bindings, a fresh
// generation, and the drift check identical.
func trimTrailingSpace(root string) error {
	files, err := treeFiles(root)
	if err != nil {
		return err
	}

	for rel, body := range files {
		lines := bytes.Split(body, []byte("\n"))
		for i, line := range lines {
			lines[i] = bytes.TrimRight(line, " \t")
		}

		trimmed := bytes.Join(lines, []byte("\n"))
		if bytes.Equal(trimmed, body) {
			continue
		}

		if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), trimmed, 0o644); err != nil { //nolint:gosec // generated sources, world-readable
			return fmt.Errorf("trim %s: %w", rel, err)
		}
	}

	return nil
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

// lintVersion returns the pinned golangci-lint version: the environment's value when set,
// else the MAGE-X env file's.
func lintVersion() (string, error) {
	if v := os.Getenv(lintVersionKey); v != "" {
		return v, nil
	}

	f, err := os.Open(mageXEnvFile)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", mageXEnvFile, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), lintVersionKey+"="); ok && v != "" {
			return v, nil
		}
	}

	if err = scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", mageXEnvFile, err)
	}

	return "", fmt.Errorf("%w: %s has no %s", errNoLintVersion, mageXEnvFile, lintVersionKey)
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
	bundleVersion string // the numeric version macOS shows, e.g. 0.1.0
}

// readBuildMeta derives the version metadata from git; outside a git checkout it falls
// back to development values.
func readBuildMeta() buildMeta {
	meta := buildMeta{version: "dev", commit: "none", date: time.Now().UTC().Format(time.RFC3339), bundleVersion: "0.0.0"}

	if out := gitOutput("describe", "--tags", "--always", "--dirty"); out != "" {
		meta.version = out
	}

	if out := gitOutput("rev-parse", "--short", "HEAD"); out != "" {
		meta.commit = out
	}

	if out := gitOutput("describe", "--tags", "--abbrev=0"); out != "" {
		meta.bundleVersion = strings.TrimPrefix(out, "v")
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

// buildDesktopBinary compiles the production desktop binary into exe.
func buildDesktopBinary(exe string, meta buildMeta) error {
	minVersion := "-mmacosx-version-min=" + macOSMinVersion
	env := map[string]string{
		"CGO_ENABLED":              "1",
		"CGO_CFLAGS":               minVersion,
		"CGO_LDFLAGS":              minVersion,
		"MACOSX_DEPLOYMENT_TARGET": macOSMinVersion,
	}

	ldflags := fmt.Sprintf("-s -w -X main.version=%s -X main.commit=%s -X main.buildDate=%s",
		meta.version, meta.commit, meta.date)

	err := sh.RunWithV(env, "go", "build",
		"-tags", desktopTag+",production", "-trimpath", "-buildvcs=false",
		"-ldflags", ldflags, "-o", exe, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("build the desktop binary: %w", err)
	}

	return nil
}

// writeInfoPlist copies the generated Info.plist into the bundle and stamps the build's
// version into the copy; the committed file keeps its placeholder version.
func writeInfoPlist(dst, version string) error {
	body, err := os.ReadFile(filepath.Join("build", "darwin", "Info.plist"))
	if err != nil {
		return fmt.Errorf("read the Info.plist: %w", err)
	}

	if err = os.WriteFile(dst, body, 0o644); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("write the bundle Info.plist: %w", err)
	}

	for _, key := range []string{"CFBundleShortVersionString", "CFBundleVersion"} {
		if err = sh.Run("plutil", "-replace", key, "-string", version, dst); err != nil {
			return fmt.Errorf("stamp %s: %w", key, err)
		}
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
