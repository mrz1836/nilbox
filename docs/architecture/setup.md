# internal/setup

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/setup`](../../internal/setup) · Guide: [First-run setup](../guide/first-run-setup.md), [Starting over](../guide/starting-over.md)

`internal/setup/` is the first-run setup's service: where the user is in it and whether it
is done, kept in `setup.done` and `setup.step` (a `Step`, a named string type), which it
alone writes through its narrow `Memory` (core's `prefs.Store`), and which a backup never
holds. `setup.Adopt`, never bound, decides once per database at startup (core's
`adoptSetup`) whether the setup shows: while no `setup.*` key is stored, done when the inbox
ever signed in (`inbox.Store.ViewerLogin`, an install from before the setup), else not; once
decided it never decides again, and a state that was never stored reads as done, so a
passing problem never traps the user. `SetStep` remembers the step, `Finish` marks it done
and emits the Go-only `event.SetupFinished` (the shell then asks to allow banners, which it
held while the setup showed at launch), and `Restart` opens it again from Welcome (Settings
→ About → Run setup again). Its checks: `GitHub` reads the GitHub CLI's sign-in through its
`Account` (core: `github.WithToken` and `(*github.Client).FetchSignIn`, the one-point viewer
query `FetchViewer` sends, widened to `login name url`, with the token's `X-OAuth-Scopes`)
into one of six `GitHubState`s with the command that fixes it, never an error but a check
the window gave up on; `Terminals` says which terminals are installed
(`terminal.Registry.Installed`, over `/Applications` and `~/Applications`; Terminal always
is); `Signals` counts the clones, every repository location's through the shared
`localrepos.Reader`, that hold each sign the Tools step looks for (a `go.mod`, mage-x's signs,
`toolsigns.MageX`, the list Deps reads too, go-pre-commit's config, go-fortress's workflow),
fixed paths with `os.Lstat` alone. `Reset`,
once the user typed `ResetWord`, refuses while runs go in the app (its `Runs`), saves a
backup first through its `Saver`, refusing the reset when that fails, writes the request
(`appdata.RequestReset`, `<data>/reset-request.json`), and emits the Go-only
`event.ResetRequested`, which the shell answers by letting the quit through and quitting to
open again. It reaches nothing else, and the boundary test keeps it from the inbox, the
settings, the store, the runs, and the backup, which core adapts.
