# cmd/nilbox

`cmd/nilbox/` is the application entry point: a headless build by default, and the Wails
desktop app with the `desktop` build tag (`main_desktop.go`). Only the desktop bootstrap
imports Wails. `--data-dir=<absolute path>` (`appdata.FromArgs`, `appdata.DataDirFlag`),
read before anything else, runs the app on that data directory, its logs included, in place
of the user's own: the sandbox `magex app:fresh` and `magex app:dev fresh=true` open, which
`core.Options.Sandbox` tells the window (`appinfo.Info.Sandbox`, a **Test data** chip). A
reset the user asked for quits the app and opens it again from `PostShutdown`, once the
shell has closed the database: `open -n <bundle>` (`desktop.RelaunchArgs`, pure and tested on
Linux, which a run outside a `.app` bundle, as `magex app:dev`'s, skips), with the sandbox's
`--data-dir` when it ran on one. `main()` runs the desktop runtime's
`updater.HandleHelperMode()` first, since an update's helper is this binary run again,
before anything opens the data, and then, in both builds, `fence.RunHook`: a fenced agent
run's hook is this binary run again with `--fence-hook-v1=<run folder>`, which decides one
tool call against the run's policy and exits, reading no setting, opening no data, logging
nothing, and touching no window (`hookmode_test.go` runs the test binary's own `main` so).
Right after it comes `mcpserver.Serve`: Describe it's tool server is this binary run again
too, by Claude Code, with `--mcp-server-v1=<describe folder>` ([the tool
mode](../mcpserver.md)), which serves its tools on stdin and stdout until the session ends,
as the hook does opening no data, starting no logging, and touching no window
(`toolmode_test.go` runs the test binary's own `main` so, with an empty home, and finds
nothing created but the proposal).
A rehearsal build (`main.flavor=rehearsal`, `magex
app:rehearse`) always runs on its own data directory beside the user's
(`appdata.ResolveRehearsal`, named `brand.RehearsalDataDirName`, or `brand.RehearsalSlug`
on Linux, as the user's is named), whatever `--data-dir`
says, with the **Test data** chip, since the update opens the new version with no
arguments. `update_desktop.go` is the engine's `Installer` over the runtime's updater,
headless, whose one provider serves the zip from the update cache with its SHA-512
(`desktop.NewArtifact` never hands over a zip without one); its `Restart` waits up to 30
seconds for the helper to stand by, so it never runs on the main thread. The menu bar's
update rows are `menubar.UpdateRows` (an update that didn't install shows "<version>
didn't install", then Try Updating to <version> Again while `InstallFailed.Retryable`, or
else Download <version> Manually…, `UpdateManual`, which opens the release page through
`desktop.Config.Browser`, the runtime's `Browser.OpenURL`: the version found's, or, with
none found, the one that didn't install, `menubar.ManualURL`), their clicks
`Shell.UpdateAction`: a check runs as tracked work, and Restart to Update, Update to X…,
and Try Updating to X Again bring the window forward and emit
`event.UpdateRestartRequested` (`updates:restart`), so every restart runs the window's one
flow (`restartToUpdate` in `frontend/src/lib/useUpdate.ts`: download first, then
`Restart(false)`, whose refusal names the runs going for the window to ask about, sheets
counted then; with text a sheet hasn't sent, it asks first, naming the runs it lists; a
confirmation calls `Restart(false)` again and goes on to `Restart(true)` only when the
question named every run that refusal names, matched by id (`covers` in
`frontend/src/lib/update.ts`), else it asks again). What the banner, Settings → About,
and the move dialog offer is one pure rule (`offerOf` in `frontend/src/lib/update.ts`).
The window tells a refusal it answers on its
own (the last update still finishing, a restart finishing after a held quit, which it
waits out quietly, and an update that failed to install for good, which it offers only the
release page for) by the `update.RefusalError` code in its detail, never by its words. A
failure that stops the start shows on a bare app (`failStartup`, `startup_desktop.go`: no
window, no services, no database), whose dialogs `desktop.Startup` runs off the main thread
over `startupUI` (each a native alert, `Ask` waiting for its click; `alertButtons` maps
`desktop.Failure.Buttons`, Quit rightmost, the first action taking Return and Quit Escape,
since the runtime's macOS alert gives a button one or the other, never both). For data a
newer build saved (`store.ErrNewerSchema`), `newerData` asks `core.BareUpdate` for the
update that runs without the database (`update.Bare`, below) and offers its `Way`
(`desktop.StartupFailure` with a `desktop.Newer`): Update and Reopen for a release that
updates where it runs, `Bare.Latest` then `Bare.Update` through a `newInstaller` over the bare
app's own `app.Updater`, with `desktop.Updating` shown over it (Stop and Quit, Escape alone)
and `desktop.NoNewerRelease` or `desktop.UpdateFailed` (Try Again) shown over that when it
doesn't install; the README's install command (`desktop.InstallCommand`, which a test holds
to the README's, for `~/Applications` when the app is there or would move there,
`desktop.NewerCommands`) for a release that can't update where it runs; and the build command
for a build from source (`cd <clone> && git pull && magex app:run`, the clone the one whose
`bin/` holds the bundle, beside a `.git`), which never reads anything. A Copy shows the
dialog again, saying so (`Failure.Copied`). `startupUI.Quit` quits, and exits should a modal
alert hold the quit: an update's helper waits for the process to exit, and the bare app holds
nothing open. The dialog also offers the newest backup this build opens
(`desktop.RestoreOffer`, `FindRestore`; no key puts it back), offered still after an update
that didn't help, puts it back on a click (`desktop.PutBack`, `store.RestoreBackup`, a
failure worded by `desktop.PutBackFailure` with what to do), and opens the app again from
`PostShutdown` through `desktop.RelaunchArgs`, as a reset does, the `open -n` bounded to five
seconds (`relaunchWithin`, which every reopen from `PostShutdown` shares, `openAgain`).
A build that updates attaches the engine's `Mover` too (`desktop.Mover`, over
`execpath.Exec`): it copies the bundle with `ditto --noqtn` (`desktop.CopyArgs`), takes the
quarantine mark ditto still copies off every file of the copy, in Go (`move_darwin.go`;
nothing elsewhere), and reads a copy's mark there too (`translocates`: its own folder's,
without the flag a Finder move adds); checks a copy with `codesign --verify --deep
--strict --test-requirement =<requirement>` (`desktop.VerifyArgs`), the requirement the
app that runs designates, read from its process ID (`desktop.RequirementArgs`, `codesign
--display --requirements - <pid>`), so a copy signed otherwise fails, worded, and a
requirement that can't be read holds `update.ErrOwnSignature`; words a program that wrote
nothing by how it ended (an exit status, a signal, or no start); holds every quit while a
move runs (`Shell.HoldQuit`: `ShouldQuit` says no and shows the window, and the quit goes
ahead, once, as the last move lets go); and
`Shell.OpenAfterQuit` quits the normal way, which the quit guard holds while runs go,
keeping the copy for the quit the user lets through: **Keep running** (the bound
`scriptrun.Service.KeepRunning`, the Go-only `event.QuitKept`) forgets it.
`PostShutdown` runs the one command `Shell.RelaunchCommand` works out: the copy
(`desktop.OpenArgs`, `open -n <bundle>`, the bundle-path sibling of `RelaunchArgs`), else
this bundle after a reset, so the app never opens twice, within the same five seconds.
The window's one move is `useMoveToApplications` (`frontend/src/lib/useMove.ts`,
`components/MoveDialog.tsx`), which `useUpdateControl` holds: wherever `offerOf` offers
the move (`Offer.move`: a build that updates, `update.Location.Movable`, no restart
installing or finishing), the dialog asks on every launch, Not now for that launch, and
the update banner's offer buttons and Settings → About's location row offer it too. A
refused Open it turns the question back to the move, and as the work ends focus goes back
to Not now (`ConfirmDialog`'s `busy`). The quit's question, opened over the dialog, has the
keys: the topmost modal is the one opened last (`isTopmost` in `lib/useModalDialog.ts`),
wherever the page renders it.
