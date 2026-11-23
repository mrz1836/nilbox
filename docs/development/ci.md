# How the desktop shell is kept out of CI

Everything except the window is plain Go in untagged `internal/*` packages, built, tested, and
linted on Linux with `CGO_ENABLED=0`. Only the desktop bootstrap needs Wails, CGO, and WebKit,
and it sits behind the `desktop` build tag:

- `cmd/nilbox/main.go` (`//go:build !desktop`) is the headless build, which prints the product
  identity. `magex build` and every untagged `go build ./...` build it.
- `cmd/nilbox/main_desktop.go` (`//go:build desktop`) is the Wails app. The window and its
  page are `ui_desktop.go`, the menu bar icon `tray_desktop.go`, and the notification banners
  `banners_darwin_desktop.go`. macOS window options live in `window_darwin.go`
  (`desktop && darwin`). Everything the shell decides is plain Go: what it does with each of
  the services' events (`internal/event`), when the menu bar is redrawn, the background work
  it runs and the order it shuts down in (`internal/desktop`), what the menu bar shows
  (`internal/menubar`), and every notification (`internal/notify`). The desktop files only
  draw and post what those packages decide.
- `frontend/assets_desktop.go` (`//go:build desktop`) embeds `frontend/dist`, so no untagged
  build needs a built frontend.
- MAGE-X discovers build tags and tests them in one combined pass, which would compile the
  desktop shell on Linux. `.github/env/90-project.env` therefore adds `desktop` (and Wails'
  `production` release tag) to `MAGE_X_AUTO_DISCOVER_BUILD_TAGS_EXCLUDE`. The same file leaves
  `frontend/` to Prettier, out of the Go formatter.
- A repository test fails if anything outside the desktop bootstrap imports Wails.
- The `magex` targets in `magefiles/` are tested under the `mage` build tag, which the combined
  pass includes. Each target checks the OS and reads its arguments, then runs its steps with a
  `runner` (`magefiles/runner.go`) that names the checkout they work in, where they print, the
  environment they read, and how they start a tool; the tests hand the steps one of their own,
  so they change nothing of the test process and run in parallel. Fakes played by the test
  binary (`testutil.FakeTool`), each call answered by its arguments, stand in for pnpm, go,
  rsvg-convert, and the macOS-only commands (`plutil`, `codesign`, `iconutil`, `open`, `pgrep`,
  and, for packaging a release, `lipo`, `ditto`, `hdiutil`, and `security`), so CI on Linux runs
  the bundle assembly, `app:open`, `app:run`, and `app:check`, the release packaging, and the
  icon rendering steps too.

CI runs the fortress workflows on ubuntu for the Go code, `.github/workflows/frontend.yml` for
`magex frontend:gate`, and `.github/workflows/sqlc.yml` for `magex db:check`. No check runs on
macOS, so `magex app:check` is a local step; only the release workflow's jobs run on macOS
([Publishing a release](publishing-a-release.md)).
