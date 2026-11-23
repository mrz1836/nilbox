# internal/update

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/update`](../../internal/update) · Guide: [Updates](../guide/updates.md) · See also: [Rehearsing an update](../development/rehearsing-an-update.md), [Publishing a release](../development/publishing-a-release.md)

`internal/update/` is how the app updates itself. It holds the manifest each release attaches
(`manifest-darwin.json`, `ManifestName`): its JSON (`Manifest`, `Encode`, and `Parse`, which refuses
one over 16 KiB, a field the format lacks, or anything after it), the fixed lines its Ed25519
signature covers (`SignedMessage`: the format, the platform, the version, the zip's name, its size,
and its SHA-512, so none can change without the key), `Sign` and `Verify` (every field held to its
form, so none can pass for another: the version a release's, as semver allows it, the zip's name
exactly `FileName`, its size at most 200 MiB), `Digest` and `CheckFile` (a zip's size and SHA-512
against a manifest `Verify` accepted), the keys (`ParseKey`: raw base64 or a PKIX PEM block, never a
private key and never an Ed25519 point of small order, under which a signature could be forged,
which `Verify` refuses too; `ParsePrivateKey` and `EncodePrivateKey`, PKCS#8), the release's asset
names (`AssetNames`, from `brand.Name`), and `ReleaseKey`, the public key the app pins (a new one
strands every copy that pins the old). Stdlib crypto only, no service, no HTTP (the repoguard
boundary test). `magefiles/release.go` builds and checks releases with it (`magex app:package`,
`app:verify`), and `magefiles/keygen.go` makes the release key (`magex app:keygen`), so the
packaging and the app agree on what a valid release is; while `ReleaseKey` is empty, `app:package`
and `app:verify` refuse, and a test pins the SHA-256 of the constant, so changing the trust root
takes two deliberate edits.

The `Engine` (never bound; `Service` is the window's face, `Worker` its timed checks) checks,
downloads, and installs the app's updates. What a build does is its `Flavor`'s, which core works out
from `core.Options` (`updateSource`): a source build is off ("Updates are off in builds made from
source."), its `Status` still telling the mode the user chose; a release reads GitHub's latest
release through core's `githubFeed` over `(*github.Client).DownloadAsset` (the manifest from the
latest release's download link, at most 16 KiB, a 404, `github.ErrDownloadMissing`, reading as no
release; the zip from its tag's, `ZipURL`, at most 200 MiB) and trusts `ReleaseKey` alone, whatever
`UpdateFeed` and `UpdateKey` say, and is off while it is empty; a rehearsal reads a local folder
(`FolderFeed`, its failures worded with no path) and trusts its own raw base64 key (never a PEM
block, never a point of small order). The running version must pass `ValidVersion`, or updates are
off.

A check reads the manifest within `ManifestLimit` (a minute) and takes it only once `Verify` passes,
its platform is the build's, its version a release's (no prerelease) and newer than the running one
(`internal/semver`); no release, or none newer, is up to date, and an update waiting that the
releases no longer offer is dropped, so deleting a release's manifest withdraws it from every
install (the lever for pulling a bad release).

Where the app can update itself is `Location` (`OK` only in `/Applications` or `~/Applications`, in
a folder it can write); elsewhere checks still run and tell, nothing downloads on its own, and
`Download` and `Restart` refuse with `ErrLocation` ("Move Nilbox to Applications to update it.").
`Location.Movable` says a move helps (a build that updates, its bundle outside every Applications
folder), and the bound `MoveToApplications` moves it: the target worked out again (`/Applications`
when writable, else `~/Applications`, made when missing), a copy of the app already there, by its
`Info.plist`'s bundle identifier (`bundleID`, never a link followed), never replaced (`MoveExists`,
for `OpenInApplications`, which checks that copy's signature against the running app's requirement
and opens it), anything else there refused (`ErrTargetTaken`), and otherwise the copy made and
checked in a hidden folder of the move's own beside the target (`stage`, `stagePrefix`, its marker
and an empty bundle in it), through the `Mover` the shell attaches (`AttachMover`), and only then
renamed into the target, never over anything that appeared there (`place`, `renameNew`: RENAME_EXCL
on macOS), so a copy cut off or never checked is never where the app goes, and a failed copy's
removal (`discard`, that folder alone) never touches what was there; a later move removes such a
folder a cut-off move left (`sweepLeftovers`: named so, marked before the copy began by
`stageMarker`, the bundle ID and the move's process, holding nothing else but the copy however far
it got, and its maker this run or gone by `procs.Probe`, or, when that can't be told, older than
`leftoverAfter`); then the Mover quits the normal way and opens the copy once the app has quit
(`MoveMoved`).

`OpenInApplications` refuses a copy macOS would open from a hidden folder (`Mover.Translocates`,
`ErrQuarantined`), leaving its mark, before the check, and a check that couldn't read the app's own
signature (`ErrOwnSignature`) blames no copy. The bundle that runs is never its own copy, by the
folder it started from too (`isSelf`), so one dragged into Applications while it runs still reads as
itself. Every move holds the app's quit from its start to its end (`Mover.HoldQuit`): its copy and
check within `MoveLimit`, the steps around them (the sweep, the unmarking, the rename, a failed
copy's removal) moments more. One move runs at a time (`ErrMoving`), its copy bounded by `MoveLimit`
and never cut short by the window's call, and none starts while a restart into an update installs or
finishes after a held quit, which goes first (`ErrUpdating`, with `ErrFinishing` for the latter,
each worded for the window); the bundle that runs is never touched, and `ShowAppInFinder` shows it,
but for a translocated one.

With downloads on, the zip goes into `<data>/updates/<version>/` (`Cache`, `appdata.Paths.Updates`)
through a temporary file, hashed as it arrives, stopped at the manifest's size, within
`DownloadLimit` (10 minutes), its progress told as `event.UpdatesUpdated` a few times a second
(`internal/progress`); only a zip matching its manifest is kept, with its manifest beside it, and
every start checks each kept update again, so one waiting is ready at once. A background download
that failed is remembered by its version and SHA-512 and tried again only at the next due check,
never on the ticks between (a click on Update tries, and so does Check for Updates, which forgets
the failure and has the `Worker` download at once). One operation (a check, a download, a restart)
runs at a time. A failure changes no state: it is the status's `LastError`, worded with no raw
error, path, or URL in it (`reachWords`, `downloadWords`, and the restart's own sentences), and a
check that reached the releases is the last check, a manifest it refused included.

The `Worker` (deferred work) settles the start first: it reads the outcome of the last restart from
`update.pendingInstall` (`JustUpdated` when the running build is the version it installed, newer
than the one it ran from, or, with no record, newer than `update.lastRunRelease`, as after an
install by hand; `InstallFailed` when it still runs the version it ran from, with the runtime's
helper log, `<TMPDIR>/wails-update-<pid>.log`, copied to `logs/update-<version>.log` and worded from
the messages it ends on, which tell the helper's exit code, never repeating a path, and `Retryable`
unless the update itself was at fault: a payload failure, the new version failing its signature
check or a staged update not installable, fails the same way every time, so its cache entry is
dropped and its version and SHA-512 (`pendingInstall.sha512`) go into `update.uninstallable` (the
last five), which no check downloads, tells, or offers again, and `Download` and `Restart` refuse
(`ErrUninstallable`), while a newer version, or the same one with another SHA-512, goes on as usual;
any other ending (the app not quitting in time, the swap's attempts or the copy aside failing, `open
-n` of the new version failing to start, which says nothing of the update, a missing or unknown log)
keeps the cache for a retry), then cleans up by exact name only (beside the bundle, `<bundle>.bak`
and the `wails-update-*` folders holding this app's update or nothing; in the temporary folder, the
last install's helper `.log`, any other process's once `HelperWindow` old and only when its first
line names this bundle as the target, and the `.ready` files of those processes alone), then loads
the cache.

While the last install is under `HelperWindow` (3 minutes) old and its helper's log hasn't ended on
a final message (one ending on the old app being put back counts once `<bundle>.bak` is gone), the
helper may still be restoring the old app, so the start settles nothing (no outcome, no cleanup, the
record kept) and the `Worker` settles again once it may have ended; meanwhile the bound `Download`
and `Restart` refuse with `ErrSettling` ("The last update is still finishing. Try again in a
moment."), and `CheckNow` checks but downloads nothing.

It checks a minute later (`FirstCheckAfter`), or at once on a Wake within that minute, then, through
`periodic.RunEvery` on a 15-minute tick, whenever six hours passed on the wall clock (`CheckEvery`),
since timers stand still while a Mac sleeps; a Wake (the settings' Go-only `event.UpdatesChanged`,
or a manual check that found a version to download) has it look at once. A timed check tells each
version once (`update.notified`, through its `Notifier`: core's `updateNotices`, an `update_ready`
notification about no item, which opens nothing, so a click brings the window forward), once it is
ready, or available where it doesn't download; Don't check makes none; `CheckNow` works in every
mode, downloads nothing in Don't check or Just tell me, and marks what it finds told without a
notification once it is available or ready, so a download it started that fails still lets a later
check tell of it.

`Restart(confirmed)` refuses while runs go in the app, unless confirmed, with a `RunsGoingError`
whose `Detail` (`{"runsGoing": [...]}`, through `desktop.MarshalError`) names them, each with
whether it carries on through the restart (`Run.Outlives`: an agent's run that keeps to a budget of
its own, which the restart lets go through `Runs.LetFinish` just before it lets the quit through,
and stops letting go should the restart fail after); it checks the zip against its manifest again
(`CheckFile`; a damaged or missing one is dropped, back to available, downloaded again when
downloads are on, and the refusal says so), backs the database up first (`store.Backup`, through its
`DataBackup`, reusing one it took under `BackupReuse`, 10 minutes, ago) and stops when that fails,
stages the update through the `Installer` the shell attaches (`Attach`), checks again, unless
confirmed, that no run started meanwhile, records `update.pendingInstall` (`{from, to, at, pid,
sha512}`) only then, so a quit while the update is staged leaves no record of an install that never
began, and emits `event.QuitConfirmed{Restarting: true}` before `Installer.Restart`, inside which
the app exits: the shell then lets every quit through, whatever runs go, since a held quit would
leave the runtime's helper to reopen this copy beside the new one.

A failure before the quit, or a `Restart` that returns an error, goes back to ready with
`LastError`, clears the record even once the call's context ended, removes the staging folder, and,
once the quit was let through, emits the Go-only `event.UpdateRestartFailed`, which has the shell
hold a quit while runs go again. A `Restart` that returns nil with the app still running (a quit
something held) left the helper standing by, which can't be called back, so the restart is finishing
(`StateFinishing`, "Finishing the update…"): the staging folder and the record stay, a later restart
is refused (`ErrFinishing`), and the Go-only `event.UpdateQuitHeld` has the shell see the app out
(`Shell.finishRestart`): every quit stays let through, it asks to quit again every
`desktop.FinishQuitEvery` (5 s), `FinishQuitTries` (4) times, off the main thread, and, the app
still running `FinishWithin` (25 s) after `UpdateQuitHeld.HelperStarted` (stamped by the engine just
before `Installer.Restart`, since the helper's wait starts inside it, and at once when that is
already past), stops the shell in order (closing the database, waited for at most
`FinishStopWithin`, 3 s) and exits (`desktop.Config.Exit`, `os.Exit` but in tests), before the
helper's 30-second wait ends, logging each step.

`HideBanner` (Later) hides the banner of the version found for a day, `ShowBanner` lifts it,
`AckUpdated` forgets the toast, and `ShowInstallLog` shows the kept log in Finder. It writes only
the `update.*` state keys (`update.lastCheck`, `update.notified`, `update.bannerHiddenUntil`,
`update.lastRunRelease`, `update.pendingInstall`, `update.uninstallable`), never `update.mode`, and
imports no service, no store or prefs, and no HTTP: core adapts the feed, the preferences
(`Memory`), the backup, the runs (`Runs`, over the run engine), and the notifications (the repoguard
boundary test).

`store.RestoreBackup` copies a backup into a temporary file beside the database before it sets the
database aside and renames the copy into place, putting the database back when the rename fails;
with no database there, it removes a `-wal` or `-shm` left behind first. Every open removes the
copies a restore cut short left (`.<db>.restore-<digits>.tmp`, regular files five minutes old or
more).

`Bare` is the update the startup dialog runs when a newer build saved the data
(`store.ErrNewerSchema`), before anything opens the database and without it: core builds it
(`core.BareUpdate`) over the same feed and key `updateSource` gives the engine (a GitHub
client of its own, for the release downloads alone), the update cache, and the bundle, with
`BareConfig` holding nothing the database keeps. Its `Way` is the engine's rule (`trust`,
which `resolve` shares) and `Location`: `WayUpdate` for a build that updates where it runs,
`WaySource` for a build from source, and `WayInstall` for any other. `Latest` is a check
(`readLatest`, which `Engine.latest` shares: the manifest within `ManifestLimit`, `accept`,
newer than the running version), and `Update` checks the manifest again, takes the cache's
copy of the zip when it is that release's and checks out (`Cache.check`) or else downloads it
(`Cache.Put`, within `DownloadLimit`), checks the zip again (`checkZip`), and has the
`Installer` stage it and restart, inside which the process exits; a failure before the
restart removes the staging folder (`removeStaging`) and is worded as the engine's
(`downloadWords`, its restart's sentences). It reads and writes no `update.*` key, takes no
backup, records no `update.pendingInstall` (the new version's start reads no outcome, so it
says nothing of the update), honors no `update.mode` (it runs on the user's click alone), and
in a build from source, or where the app can't replace itself, reads nothing (`ErrOff`,
`ErrLocation`). The manifest names no schema, and `Parse` refuses a field the format lacks, so
adding one would strand every shipped build: a newer release that can't open the data shows
the same dialog, whose check then finds nothing newer.
