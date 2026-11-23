# internal/integrations

`internal/integrations/` finds the tools the app drives, mage-x's magex and go-pre-commit,
each one a `Definition` (its binary, releases, version parser, quiet environment, minimum
version, and the release that brought each upstream `Feature`). Detection looks on the login
shell's `PATH`, then in the well-known tool folders and go install's, runs every copy's
`--version` (four at a time), and works out where each came from; the `Worker` runs it as
deferred work after the shell read, again when woken (settings emits the Go-only
`event.IntegrationsChanged`, which core relays: a chosen copy wakes the finder, a flipped
switch only tells the window), and checks GitHub for newer releases once a day
(`(*github.Client).FetchLatestReleases`, adapted in core; **Check again** reuses an answer
less than ten minutes old). `Service.List` never waits: it
serves the last detection kept in `tools.<id>.seen`, marked as checking, until this run's
ends, and `event.ToolsUpdated` tells the window; a `List` that finds this run's detection
older than `staleAfter` (ten minutes) wakes the finder too. `Trash` moves a copy the last
detection found (the one in use when the app installed it, or any other) to the Trash
through Finder (`osascript`, the path passed as an argument, so Put Back works), never a
link or a Homebrew copy, and fails with `ErrAutomationDenied` while the user hasn't let the
app control Finder; `Reveal` shows such a copy in Finder (`open -R`). It writes the
`tools.*` preferences and only reads `integrations.*`, which settings writes through the leaf `internal/integrationprefs`
(the switches, automatic, on, or off, and the chosen copies; the leaf also declares the
window's memory of the Scripts tab, `integrations.announced`, `scripts.lastPage` (a
`ScriptsPage`, Deps' `PageDeps`, `deps`, among them), `scripts.lastRepo`, `scripts.sections`
(the Release, Hooks, Repo scripts, and Deps sections the user opened or folded, Deps' being
`deps.stale`, `deps.aging`, and `deps.fresh`), `scripts.releaseSort` (`KeyReleaseSort`, the
order the Release page lists each group in, a `ReleaseSort`: `newest`, the default, `name`, or
`commits`; `DecodeReleaseSort` reads any other value as newest, and `EncodeReleaseSort` refuses
one), and `scripts.depsSort` (`KeyDepsSort`, the order the Deps page lists each group of clones
in, a `DepsSort`: `oldest`, the default, `name`, or `modules`; `DecodeDepsSort` reads any other
value as oldest, and `EncodeDepsSort` refuses one, as `settings.SetDepsSort`, which stores it,
does), none of which a backup holds (`settings.Portable` leaves them out), and `scripts.showRuns`,
whether a run Repo scripts starts opens beside it, off by default, which settings stores for
the window alone), so settings never imports integrations. One rule says which folders are
go install's (`goFolders`), for a copy's
source and for its own updater alike: `$GOBIN`, each `$GOPATH` folder's bin, and
`~/go/bin`, from the shell's environment and, while `go` is on its `PATH`, from `go env
GOBIN GOPATH` too (run once per detection with `GOTOOLCHAIN=local`; one that fails leaves the
folders unsure, and then no copy updates itself). Every run of a tool gets
`Definition.RunEnv` (the shell's environment with the quiet variables and
`GIT_TERMINAL_PROMPT=0`, or the quiet variables alone in the terminal), detection's and an
install's `--version` runs included; release and hooks use it too. `Update` of a copy that can update itself runs the tool's own updater
(`Definition.UpdateArgs`: `magex update:install`, `go-pre-commit update`), which downloads
the latest release, checks it against the release's checksums, and renames it over the copy
it runs as: a copy from a GitHub release or the app, no link, whose version has
`FeatureSelfUpdate`, that it and its folder can be written, in no go install folder (magex's
updater would run `go install` there). Detection works that out once per look
(`detection.selfUpdates`, kept in `tools.<id>.seen`), so `List` reads no files for it. It is a
`scriptrun.KindUpdate` run in the app, in the copy's folder, holding that folder's lock;
its `Before` checks the copy again under the lock, the bound `Update` returns the run's ID for
the window to show, and its `Finish` detects again, reading the copy's version from that look
(the copy runs once), judges the update by the version alone (exit 0 with the updater saying
the copy is already up to date succeeds, saying so), and, before the look matches its copies
with the installs, records a copy the app installed as the app's (a new `tool_installs` row at
the same path); `Tool.SelfUpdate` and `event.ToolsUpdated` bring that to the card. A tool's
own update counts as updating it however it started (`scriptrun.Engine.Updating`, through
`Runs`): one from before a restart shows on the card and refuses every install, and core
registers `integrations.Verifier` to settle it as one that just ended. A self-update needs no
sign-in and no build named by the app, since the tool finds its own release and archive. It
asks nothing about replacing a copy the app didn't install: the tool replaces its own.
`Install`, `Reinstall`, and every other `Update` take the lock of the folder they place the
program in (`clonelock.KindInstall`, the app's one `Set`), so they never meet a tool's
update there, and hand the latest release to
`internal/toolinstall`, which downloads the archive and checksums file into a fresh private
folder under `<data>/downloads` (every file in it made and read through an `os.Root` of it,
so no name from GitHub reaches out), checks both against the checksums file and GitHub's digests,
extracts only the program's own entry, tries it, and places it so the target is never missing
(a copy beside it renamed over it, the old one put back if the new one won't run); it keeps no
records and speaks no HTTP, and integrations records each install in `tool_installs`, which
makes the copy read as installed by the app. Nothing replaces a Homebrew copy or a link, a
copy the app didn't install is replaced only after the user confirms, and a go install copy
gets a new copy in `~/.local/bin` instead; the app never runs `go install` or `sudo`. A run
using the tool (`scriptrun.Engine.InUse`, which leaves out the tool's own update) holds both
kinds of update back, nothing installs a tool while it updates itself, and nothing installs
before this run's first detection ends (the kept one is only shown, never acted on).
