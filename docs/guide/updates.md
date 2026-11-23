# Updates

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/update`](../architecture/update.md), [`internal/updateprefs`](../architecture/updateprefs.md), [`cmd/nilbox`](../architecture/cmd/nilbox.md) · See also: [Rehearsing an update](../development/rehearsing-an-update.md)

A release of the app updates itself: it checks for a new version on its own, downloads it
quietly, checks it against the key built into the app, and installs it on one click, after
backing your data up. Nothing in your Applications folder changes until you click. The window
offers it in a banner and in Settings → About, and the menu bar beside them ([Menu bar and
notifications](menu-bar.md)).

## How it works

- **Checking.** About a minute after the app opens, then whenever six hours have passed on the
  clock (a Mac that slept checks at its next look, every fifteen minutes), the app reads the
  latest release's `manifest-darwin.json` from GitHub, giving up after a minute. That's one GET
  to GitHub's release download hosts (`github.com`, which redirects to
  `release-assets.githubusercontent.com`), with no token and nothing of yours, and no API
  budget. A latest release with no manifest yet reads as up to date. A read that reached GitHub
  counts as a check even when the manifest is refused; one that never got an answer doesn't.
  The menu bar's **Check for Updates…** checks at once, whatever **When a new version is out**
  says, and shows how it went for a minute: **Up to date (0.2.0)**, **Check failed** (hover for
  why), or the version it found.
- **Trusting it.** The manifest is taken only when its Ed25519 signature verifies with the key
  the app pins (`update.ReleaseKey`), it is for this Mac's build (`darwin-universal`), and its
  version is a release, not a prerelease, newer than the one running. Anything else is an error
  the app keeps, and nothing is downloaded.
- **Downloading.** With **Download it and tell me**, the zip the manifest names is downloaded
  from its release's tag (the second GET), at most 200 MiB and within ten minutes, into
  `<data>/updates/<version>/`, beside its manifest. Only a zip whose size and SHA-512 match the
  manifest is kept; every start checks a kept update again, so one waiting is ready at once. A
  download that fails is tried again at the next due check, not before, unless you click
  **Check for Updates** or **Update**, which try at once. A newer release replaces the one
  waiting.
- **Pulling a bad release.** Deleting a release's `manifest-darwin.json` (GitHub then answers
  404) withdraws the update: the next check of every install finds no manifest, reads as up to
  date, and throws away an update downloaded and waiting, so nobody installs it.
- **Where the app is.** Only a copy in `/Applications` or `~/Applications`, in a folder it can
  write, updates itself (a rehearsal build also in its own folder, `bin/rehearsal/`). Anywhere
  else (the disk image, Downloads, a translocated copy) it still checks and tells you, but
  downloads nothing: the banner says the version is *available* (never *ready*, even for one
  downloaded before the app ran from there) and offers **Move to Applications**, as Settings →
  About does, and **Update** isn't offered. On every launch from there, the app also asks once
  ([Moving to Applications](#moving-to-applications)).
- **Telling you.** In the window, a slim banner under the toolbar: *"Nilbox 0.3.0 is
  ready."* **Restart to update** · **What's new** · **Later** (*"…is available"* with **Update**
  under Just tell me). **Later** hides it for a day; a newer version shows it again. Once per
  version, also a notification (**New version**, which Settings → Notifications can turn off)
  that brings the window forward with the banner showing, Later or not, as soon as the update is
  ready (or, where it isn't downloaded, available), and keeps an entry in the notification
  history (a click on either shows the banner again, or Settings → About when the banner has
  nothing to show); and in the menu bar as **Restart to Update (0.3.0)**. A download of its own, a
  check, and a failure on its own never show a banner or a notification: Settings → About says
  how they went, and the next look tries again. With **Download it and tell me**, a version
  whose download failed stays out of the banner too, while About and the menu bar offer
  **Update** for it.
- **Restarting.** Every restart starts in the window: the banner's **Restart to update**,
  About's, and the menu bar's **Restart to Update**, **Update to 0.3.0…**, and **Try Updating
  to 0.3.0 Again**, which bring the window forward first, and show the banner again if
  **Later** hid it, so how the restart ends shows there. A restart asked for while one is
  already under way, or finishing, asks nothing and starts nothing. With
  Just tell me it downloads first, with the download's progress in the banner (**Later** hides
  it while the download goes on). It asks first, in one question, when restarting would lose
  something: a run going in the app, named, or text you haven't sent in a review or issue
  sheet. An agent's background run that keeps to a budget of its own isn't stopped: the
  question says it carries on through the restart, and the new version picks it up. It stops
  only runs it named: confirming asks the app again first, which refuses
  before backing anything up while runs go, and a run it didn't name (one it couldn't list,
  one going when it asked about your text alone, or one that started while it asked) is
  asked about again. If the restart waited for a check that found a newer version, it
  downloads that one and restarts into it, once, without asking again what you already
  answered. Then the app checks the downloaded zip again (a
  damaged one is thrown away and downloaded again), backs up the database beside it
  (`<data>/nilbox.db.backup-<stamp>-v<schema>`, the newest three kept, as before a migration;
  a backup the update took under ten minutes ago is used again), stages the update beside
  itself, in a `wails-update-*` folder, and quits. The update's helper, the app's own program
  run again, waits for the app to quit (up to 30 seconds), copies the old app aside as
  `Nilbox.app.bak`, moves the new one into its place, checks the new one's code signature
  (`codesign --verify --deep --strict`, held to what the old one passed), opens it, and
  removes the copy and the staging folder. It opens the new version with `open -n`, which
  hands it the old one's environment: a variable set where the old one started (a terminal,
  say) reaches the new one too. In a rehearsal on one Mac (`case=normal`), the new version
  was running 2.8 seconds after the click. A restart that fails before the app quits leaves
  the update ready and says why in the banner (**Try again** · **Download manually**, which
  opens the version's release page, with its disk image).
- **A quit held back.** If something holds back the quit a restart asks for, the update is
  already staged and its helper is waiting, so the app sees itself out: the banner says
  *"Finishing the update… Nilbox will reopen in a moment."*, with nothing to click, the menu
  bar **Finishing the update…** (with Check for Updates… and the restart off), and a restart
  asked for meanwhile waits quietly. The app asks to quit again every five seconds, four
  times, and if it still runs 25 seconds after the restart started the update's helper
  (timed from then, not from when the held quit came back), it closes its data in order
  (waiting at most three seconds for that) and exits, before the helper's 30 seconds run
  out. The staging folder and the note the new version reads are kept, so the update
  installs as usual. Each step is in the app's log.
- **After the restart.** The new version's first start reads how it went, from a note the old
  one left: updated, which a toast says once (*"Updated to 0.3.0"* · **What's new**), or, when
  the old version opens again instead, why it didn't install, in words, in the banner (**Try
  again** · **Download manually** · **Details**) and in Settings → About, with **Show log in
  Finder** for the helper's log, kept as `<data>/logs/update-<version>.log`. The version that
  failed is offered only as **Try again** (or not at all, below), never as a plain **Restart
  to update**, until a newer one replaces it, which the banner names (*"…still on 0.2.0. 0.3.1
  is ready."*) beside its **Restart to update** or **Update**. **Download manually** opens the
  release page of the version that failed, also before a check has found it again. Either
  way, it removes what the update left behind (the staging folder and `.bak` beside the app,
  and the helper's files in the temporary folder), each by its exact name. While the update's helper
  may still be at work (its log not ended, under three minutes since the restart), the start
  leaves its backup, staging folder, and log alone and looks again later; until then
  **Update** and restarting wait, and the banner says quietly that the last update is still
  finishing, which can take a minute or two, then offers the update again. A log that ends
  with the helper putting the old app back counts as ended once `Nilbox.app.bak` is gone.
- **An update that didn't install.** The menu bar says **0.3.0 didn't install**, with why when
  you hover, and what comes next depends on why:
  - When the moment was at fault (the app didn't quit in time, macOS didn't let it be
    replaced or couldn't open the new version, it couldn't be copied aside, or the log
    doesn't say), the update stays downloaded: the banner and Settings → About offer **Try
    again** and **Download manually**, and the menu **Try Updating to 0.3.0 Again**, which
    restarts in the window as the banner does.
  - When the update itself was at fault (the new version failed its signature check, or
    couldn't be installed), it would fail the same way every time. The app
    throws the download away and remembers the release, by its version and SHA-512, as one
    that doesn't install (the last five, in `update.uninstallable`): no check downloads or
    offers it again. The banner and Settings → About offer **Download manually** alone,
    never **Try again**, and the menu **Download 0.3.0 Manually…**; each opens its release
    page. A newer version, or the same version built again (another SHA-512), is offered as
    usual.
  - When no version is found yet (before the first check, or when the download kept
    can't be read), the menu's line still shows. **Check for Updates…** is the way to try
    again there, as **Try again** is in the window, and an update that failed for good still
    gets **Download 0.3.0 Manually…**, which, like the window's, opens 0.3.0's own release
    page, never the running version's.

**Settings → About** shows the build's updates under its version: where the update stands (*"Up
to date · checked 12m ago"*, *"Downloading 0.3.0… 42%"*, *"0.3.0 is ready"*, *"Last check
failed: …"*, or why a build doesn't update), **Check for Updates** with its result in place,
and what the banner offers, by the same rule: **Restart to update** or **Update** once a newer
version is found, **Try again** and **Download manually** after one that didn't install, and
**Download manually** alone after one that would fail the same way again. Where the app can't
update itself, it says why, with **Move to Applications** wherever moving it helps, as the
banner and the move dialog offer it.

## What a check sends

Two GETs at most, both to GitHub's release download hosts: the latest release's manifest,
`https://github.com/mrz1836/nilbox/releases/latest/download/manifest-darwin.json`, and, once a newer
version checks out, its zip, from the URL of that release's tag. Each sends nothing but the request
for that file: no token, no cookie, and nothing of yours, and neither spends API budget. **Update
and Reopen**, in the dialog shown when a newer version saved your data (*Data a newer version
saved*, below), sends the same two, on your click alone. A rehearsal build reads its releases from a
folder and sends nothing.

## Firewalls

Updates need `github.com`, which redirects each download to `release-assets.githubusercontent.com`
(and, for some releases, `objects.githubusercontent.com`). Blocking any of them only means a check
or a download fails, which the window and the menu say; nothing else changes.

## Where things live

| What | Where |
|---|---|
| The update waiting for a restart | `<data>/updates/<version>/`: its zip and manifest, removed once installed or replaced, or once it failed to install for good |
| The releases that don't install | `update.uninstallable` in the database: the last five, each a version and its SHA-512, never downloaded or offered again |
| The backup taken before each install | `<data>/nilbox.db.backup-<stamp>-v<schema>`, beside the database; the newest three are kept, the migrations' backups among them |
| The update's staging folder | `wails-update-*` beside the app, removed after the swap, or by the next start |
| The old app during the swap | `Nilbox.app.bak` beside the app, removed once the new one opens |
| The helper's log | The temporary folder (`$TMPDIR/wails-update-<pid>.log`) while it runs; removed after an update that installed, and kept as `<data>/logs/update-<version>.log` after one that didn't |


**When a new version is out** (Settings → About) is **Download it and tell me** (the default),
**Just tell me** (no download until you click **Update**, or the menu bar's **Update to
0.3.0…**, which downloads and restarts), or **Don't check** (no checks on their own). **Check
for Updates** works in every mode.

## The three flavors

What a build does is stamped into it (`main.flavor`):

| Flavor | Made by | Updates | Data |
|---|---|---|---|
| `source` | `magex app:build`, `app:run`, `app:dev`, as [Nilbox Dev](../development/build-targets.md#nilbox-dev-a-build-from-source) (`bin/Nilbox Dev.app`, `com.mrz1836.nilbox.dev`) | Never: "Updates are off in builds made from source." | Yours, the same as the release's, or a sandbox's with `--data-dir` |
| `release` | `magex app:package`, as Nilbox (`Nilbox.app`, `com.mrz1836.nilbox`) | From GitHub's latest release, trusting only the pinned key. Off while no key is pinned. | Yours, or a sandbox's with `--data-dir` |
| `rehearsal` | `magex app:rehearse`, as Nilbox, with the release's bundle ID | From a local folder, trusting the key the build was given, to try an update end to end on one Mac; in an Applications folder or in the folder it was built in, `bin/rehearsal/` | Always its own, `~/Library/Application Support/Nilbox Rehearsal`, beside yours, whatever `--data-dir` says, with the **Test data** chip |

A rehearsal build keeps its own data because the update opens the new version with no
arguments: a `--data-dir` would not survive the swap, and the new version would open on your
data.

## Data a newer version saved

A version can't open data a newer version has migrated, as after running a build from source and
then the release again. It says *Nilbox needs an update*, and that your data is safe and untouched,
and offers the way forward its build has. Nothing on this path opens, migrates, or changes the
database: the newer version opens it as it is.

- **A release in an Applications folder** offers **Update and Reopen** (Return) and **Quit**
  (Escape). A click reads the latest release's manifest; when it is newer than the running
  version, the app downloads its zip, checks it exactly as an update does (the pinned key, the
  zip's size and SHA-512, again before staging it), stages it beside itself, and quits, and the
  update's helper swaps it in and opens it, as **Restart to update** does. *Updating Nilbox…*
  shows meanwhile, with **Stop and Quit** (Escape alone, so a second Return never stops it).
  The same two GETs as a check, on your click alone, whatever **When a new version is out**
  says (the setting lives in the database it can't open). What a normal restart does around
  the install it skips: no backup (the database isn't touched), and no note for the new
  version to read, so it shows no *Updated* toast. A download or install that fails says why,
  in words, with **Try Again**, **Copy Install Command**, and **Quit**; nothing was installed,
  and the download is kept only once it checked out. The manifest doesn't say which data a
  release opens, so when the newer release can't open it either, that release shows this
  dialog again, and its **Update and Reopen** finds nothing newer.
- **When no published release is newer** than the running version, it says so plainly: *No
  published release can open this data yet; it was saved by a newer build, likely one built
  from source.* It shows the way forward: build Nilbox from source on the latest master
  (`git pull && magex app:run`, in your clone), or, once a newer release is out, **Update and
  Reopen** or the README's install command, with **Copy Build Command**, **Copy Install
  Command**, and **Quit**.
- **A release that can't update itself where it runs** (outside an Applications folder, or with
  updates off) offers **Copy Install Command**: the README's install command, for
  `~/Applications` when that's where the app is (or would move to), to paste into Terminal
  once you quit.
- **A build from source** never downloads anything. It says the data is newer than this build
  and offers **Copy Command**: `cd <clone> && git pull && magex app:run`, with the clone the
  build runs from when it's in the clone's `bin/` (else `git pull && magex app:run`).

A **Copy** button shows the dialog again, saying the command was copied (or, when the clipboard
refused it, where the README holds it). When a backup this version can open is beside the
database (one whose name ends `-v<schema>`, or `-v<schema>-<n>`, with a schema it has; a copy
renamed to end otherwise, such as `-v4-copy`, is never offered), the dialog also offers **Use my
data from Nov 14, 3:02 PM**, the newest such backup, and keeps offering it after an update that
didn't help. That sets the newer database aside as `nilbox.db.broken-<stamp>` (with its `-wal`
and `-shm`, kept until you remove it), copies the backup into its place, and opens the app again
on it. What changed since the backup won't be there; the newer data stays beside it, and
**Restore from a backup** on the Prompts page can bring its prompts back. No key ever puts the
backup back: Return takes the dialog's way forward and Escape quits (with nothing else to
offer, Escape quits and Return does nothing; with nothing at all, Return quits). If the backup
can't be put back, the dialog says what to do: when another copy of Nilbox still has the data
open, nothing was changed, so quit that copy and open Nilbox again; when the newer data was set
aside but the backup couldn't take its place (rare), no data is in place, and the dialog names
the set-aside file and its folder, to rename it `nilbox.db` before opening Nilbox again.

## Troubleshooting

- **Check failed.** Hover the menu's line for why. Most often GitHub can't be reached, or a
  firewall blocks the hosts above. The next look tries again.
- **Download failed: it didn't match the release's manifest.** The zip changed after the
  release was signed, or arrived damaged. Nothing is kept or installed, and a later look tries
  again.
- **The update didn't install.** The old version opens again and says why; the helper's log is
  in `<data>/logs/update-<version>.log`. The app and your data are as they were. The menu
  bar offers **Try Updating to 0.3.0 Again** when trying again may work, or **Download 0.3.0
  Manually…** when the update itself was at fault (see *An update that didn't install*).
- **Finishing the update…** The app exits on its own within 28 seconds of the restart
  starting the update's helper, and the new version opens. Should the helper give up first
  (it waits 30 seconds), it installs nothing and opens the old version again: it says it
  didn't quit in time, and the menu offers **Try Updating to 0.3.0 Again**.
- **A broken install.** If the swap fails and putting the old app back fails too (rare), the
  old app is `Nilbox.app.bak` beside where the app was. Quit Nilbox, delete what is left of
  `Nilbox.app`, and rename `Nilbox.app.bak` to `Nilbox.app`, or install Nilbox again from its
  disk image. Your data is untouched either way.
- **It says to move the app to Applications first.** Only a copy in `/Applications` or
  `~/Applications`, in a folder it can write, replaces itself; a copy on the disk image, in
  Downloads, or translocated by macOS checks and tells you, and downloads nothing. Click **Move
  to Applications** ([Moving to Applications](#moving-to-applications)). If something else
  named `Nilbox.app` is in Applications, rename or remove it, then try again.

## Moving to Applications

A release or rehearsal build that runs where it can't update itself (from the disk image, from
Downloads, or from the hidden, read-only copy macOS runs a downloaded app from until it is moved,
App Translocation) asks, on every launch until it is moved: *"Move to Applications? Nilbox can
only update itself from your Applications folder. It will reopen from there."* **Move to
Applications** · **Not now**. **Not now** puts the question away for that launch; the update
banner and Settings → About keep **Move to Applications**, which moves at once; all three
offer it by the same rule. A build from source never asks. A copy in an Applications folder your account can't change isn't offered a
move either: About says the account is why.

- **Where it goes.** `/Applications/Nilbox.app` when you can write to `/Applications`, else
  `~/Applications/Nilbox.app` (a standard user's), making `~/Applications` when it is missing.
- **A copy already there.** One whose `Info.plist` names the app's bundle identifier is never
  replaced: the dialog says *"Nilbox is already in Applications."* **Open it** · **Not now**.
  **Open it** opens that copy, and an older one then updates itself, once two checks pass.
  First, the copy must open where it is: one still marked as downloaded that you never moved
  with Finder (copied there in Terminal, with `cp` or `mv`) would open from a hidden folder,
  where it can't update itself, so the dialog asks you to move it to the Trash and move this
  copy instead. The mark is left as it is, since you never opened that copy. Second, it must
  be signed as this copy is (`codesign --verify --deep --strict` with the designated
  requirement of the app that runs, `--test-requirement`, read from its process, so it holds
  however the app was moved while it runs): a release's is the release identity's, the same
  for every release, so a build from source, which never updates itself, is refused the same
  way. A rehearsal signed ad hoc only opens a copy of the same build. If the app can't read its
  own signature, the dialog says so and asks you to open that copy yourself, never to throw it
  away. The app you are running, dragged into Applications while it runs, is never offered as
  a copy of its own. Anything else there (a link,
  a file, another app, a folder that isn't an app) stops the move, and the dialog says what is
  in the way. When **Open it** is refused, the dialog asks to move again, with why beside it.
- **The move.** The app copies itself, with `ditto --noqtn`, into a hidden folder of its own
  beside where it goes (`.Nilbox.app.moving-<number>`, in the same Applications folder), takes
  the quarantine mark a download carries off the copy (`ditto` copies it as it copies every
  extended attribute, and a marked copy would be translocated again), and checks it there, as
  **Open it** checks a copy already there. Only then is it renamed into place, never over
  anything that appeared there meanwhile, so a copy cut off partway, or never checked, is
  never where the app goes. A copy that fails, doesn't pass the check, or can't be put in
  place is removed with its hidden folder, that folder alone, and the dialog says why; nothing
  else changes. Before it copies anything, the move marks its hidden folder with the app's
  bundle identifier and its process. A hidden folder a move cut off by a crash, a power cut, or
  a force quit left there, however far the copy had got, is removed by the next move once the
  copy of the app that made it no longer runs (or, when that can't be told, once it is older
  than a move takes); one holding anything else, or not marked, is left as it is. While it
  works, the dialog says *Moving to Applications…* and holds its buttons, and every quit waits
  for it (the window comes forward, where the move shows), going ahead once the move ends: the
  copy and its check stop after five minutes, and the steps around them (clearing what
  earlier moves left, taking the mark off, the rename, removing a copy that failed) take
  moments more. A Mac that sleeps through a move doesn't count the sleep against those five
  minutes.
- **The reopen.** The app then quits the normal way and opens the copy once it has quit
  (`open -n`, given five seconds, as the reopen after a reset is, so it never holds the app's
  shutdown). While runs go in the app, the quit asks first, as every quit does; **Keep
  running** leaves the copy in Applications and this copy running, and a later quit opens
  nothing: to open the copy then, click **Move to Applications** again, which offers **Open
  it**. Only the copy opens, once, even when a reset was asked for in the same run: the copy
  carries the reset out. If the copy doesn't open, open it from Applications yourself.
- **Not during an update's restart.** A restart into an update goes first: while one installs,
  or finishes after a held quit, nothing offers the move, and a move asked for anyway is
  refused with words that say so.
- **The copy you opened stays.** Nothing deletes the copy you ran: the dialog says *"The copy
  you opened stays where it is, and you can delete it."*, with **Show in Finder** (`open -R`)
  where Finder can show it. A translocated copy runs from a folder macOS hides, so the dialog
  offers no **Show in Finder** there; delete the copy you downloaded from Downloads.
