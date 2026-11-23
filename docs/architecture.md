# Architecture

These are the design notes of every package, one section each. The project guide,
[`CLAUDE.md`](../CLAUDE.md), holds only the rules that apply everywhere and lists each package in
one line of its Layout, linking here. Read a package's section before changing the package, and
keep it in step with the change: a package's design belongs here, never in the guide, whose size
a guard test caps (`TestProjectGuideStaysSmall` in `internal/repoguard`).

The sections follow the guide's Layout:

- **The app and its shell**: [`cmd/nilbox`](#cmdnilbox), [`internal/brand`](#internalbrand),
  [`internal/core`](#internalcore),
  [`internal/event` and `internal/desktop`](#internalevent-and-internaldesktop),
  [`internal/appinfo`](#internalappinfo), [`internal/store`](#internalstore) (self-heal, and what
  outlives the database)
- [**Shared building blocks**](#shared-building-blocks): `agent`, `procs`, `execpath`,
  `shellenv`, `clock`, `periodic`, `flight`, `paths`, `progress`, `textutil`, and the test support
- **GitHub, the inbox, and notifications**:
  [`internal/budget` and `internal/github`](#internalbudget-and-internalgithub),
  [`internal/inbox`](#internalinbox), [`internal/reporules`](#internalreporules),
  [`internal/threadrules`](#internalthreadrules), [`internal/ghthreads`](#internalghthreads),
  [`internal/statusprefs`](#internalstatusprefs), [`internal/svcstatus`](#internalsvcstatus)
- **The sheets**: [`internal/review`](#internalreview), [`internal/issues`](#internalissues),
  [`internal/timeline`](#internaltimeline), [`internal/avatars`](#internalavatars),
  [`internal/people`](#internalpeople), [`internal/assignees`](#internalassignees),
  [`internal/bump`](#internalbump)
- **Agents and launches**: [`internal/launcher`](#internallauncher),
  [`internal/sessions`](#internalsessions)
- **Clones and worktrees**: [`internal/localrepos`](#internallocalrepos),
  [`internal/worktrees`](#internalworktrees), [`internal/gitread`](#internalgitread)
- **Integrations and runs**: [`internal/integrations`](#internalintegrations),
  [`internal/scriptrun`](#internalscriptrun), [`internal/release`](#internalrelease),
  [`internal/hooks`](#internalhooks), [`internal/scripts`](#internalscripts)
- **Updates**: [`internal/update`](#internalupdate), [`internal/updateprefs`](#internalupdateprefs),
  [`internal/semver`](#internalsemver)
- **Autopilot**: [`internal/autopilot`](#internalautopilot) (its prefs, the background runner
  `internal/agentrun` in [Agents in the background](#agents-in-the-background), and
  [the Autopilot tab](#the-autopilot-tab)), [`internal/autopilot/draft`](#internalautopilotdraft),
  [`internal/agentrun/stream`](#internalagentrunstream), [`internal/fence`](#internalfence)
- **Describe it**: [`internal/autopilot/kit`](#internalautopilotkit),
  [`internal/describeit`](#internaldescribeit) (with
  [what the conversation spike found](#what-the-conversation-spike-found)),
  [`internal/mcpserver`](#internalmcpserver)
- **Backup and setup**: [`internal/backup`](#internalbackup), [`internal/setup`](#internalsetup)
- **The window**: [`frontend`](#frontend)

## cmd/nilbox

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
mode](#internalmcpserver)), which serves its tools on stdin and stdout until the session ends,
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

## internal/brand

`internal/brand/` holds product identity, design tokens, and the logo mark. The palette tests
enforce WCAG AA contrast for every text color in both themes. `internal/brandgen/` generates
the frontend theme tokens (`frontend/src/generated/theme.css`, whose `@theme` block resets
Tailwind's own palette before it declares them), `build/darwin/Info.plist`, and the icon
artwork (the app icon `build/appicon.svg` and the menu bar icon `build/trayicon.svg`) from it
(`magex brand:generate`); `magex brand:icons` renders the artwork into the committed icons
(macOS, needs `rsvg-convert`). A build from source (`magex app:build`, so `app:run` and the targets
that open its bundle) is "Nilbox Dev", `bin/Nilbox Dev.app` with `brand.DevName` and
`brand.DevBundleID`, which `devStamps` (`magefiles/bundle.go`) stamps over the bundle's copy
of the generated `Info.plist` alone, so macOS (Spotlight, notifications, privacy prompts, Open
With) never takes it for the release; it keeps the release's executable name and data
directory (`brand.DataDirName`), so it shares the user's data and must not run beside the
release. A release and a rehearsal keep `brand.Name` and `brand.BundleID`, and `app:build`
removes an earlier build's `bin/Nilbox.app` once nothing runs from it.

`build/` holds that generated artwork and `build/appicon.svg.sha256`, the stamp of which
artwork the icons were rendered from. `build/darwin/` holds the bundle's generated
`Info.plist` and the rendered icon set, `icons.icns`.

## internal/core

`internal/core/` assembles the app and builds every service the window binds: settings,
profiles, prompts, launch defaults, inbox, review, issues, the profile pictures, launcher,
worktrees, notifications, app info, the API budget, sessions, integrations, script runs,
release, hooks, repository scripts, the Deps page ([internal/manifests](#internalmanifests)),
the mirror of the user's GitHub notifications, the
backup of the user's setup, the service status check, the first-run setup, the app's updates,
Autopilot's routines, and [Describe it](#internaldescribeit).
Some building blocks are built once and handed to every service that needs them, so what they
hold is shared: the clone locks (`clonelock.Set`), the profile pictures (`avatars.Service`), and
the assignee Finder both sheets offer people to assign through (`assignees.Finder`, over the
People directory, adapted by `assigneesDirectory` in `adapters_assignees.go`), so a
repository's assignable users are read at most once at a time, whichever sheet asks.
Every `internal/*` package is plain Go, built and tested on Linux without CGO.

## internal/event and internal/desktop

`internal/event/` holds the app's events, one Go type each with its name; services emit them
into the one `event.Sink` they are given. `internal/desktop/` is the shell's logic: it hears
those events, keeps the menu bar current with what `internal/menubar` works out (its
Autopilot group, while Autopilot (beta) is on and a routine exists, with Pause Autopilot as
the tray's one checkbox row, `Shell.AutopilotAction`), decides the window's close
(`Shell.WindowClosing`, the pure `CloseOf`): it goes through while the shell stops, hides
the window while the icon shows or while Autopilot has a routine on (none counts while
Autopilot (beta) is off) or an agent working (`HidesOnClose`, from Autopilot's state as the
shell last read it), and otherwise keeps the window and quits the normal way, so
`ShouldQuit` asks in it while runs go (the last window's close never quits by itself:
`ApplicationShouldTerminateAfterLastWindowClosed` stays off, since AppKit asks it as the
window hides too), runs the
scheduler, the checks follower (`inbox.ChecksFollower`: the listed pull requests whose
checks were running, but a muted repository's, and those a routine's search follows, 50 a
round at most, the listed first, read 25 to a request side by side through
`(*github.Client).FetchItems`, and nothing while none runs; a pull request whose workflow
runs GitHub holds for approval, `item_details.checks_awaiting` and
`checks_awaiting_on_github`, which its card marks held in place of the rollup's state,
isn't running, and waits for the whole refresh), the
merge follower (`inbox.MergeFollower`: a few seconds after each merge the inbox learns of,
its own through the Actor or one a read finds, the listed issues the merged pull request
closes, read again every five seconds while GitHub still has one open, and the repository's
other open pull requests on the branch merged into, read again as the checks follower reads
its own, and again while GitHub answers `UNKNOWN`, three rounds each at most, and nothing
while no merge happened), the
Release page's checks follower (`release.ChecksFollower`, below), the sweeper (these four
as often as Settings → Refreshing says, each through its own `periodic.Job`, which runs
`periodic.RunEvery` and which core wakes on the Go-only `event.IntervalsChanged` the settings
setter emits, so a change applies at once), the GitHub
notifications' `ghthreads.Syncer` (woken the same way), the budget's `Saver`, the sessions watcher, the Release page's
refresher, and the Hooks and Deps pages' scans (`hooks.Scans`, `manifests.Scans`) as tracked
work, starts the deferred work
(`core.Deferred`: the login-shell read first; the tool finder, the runs, the release
`Follower`, the service status `Checker`, and the [push watch](#internalpushwatch), woken by
`event.IntervalsChanged` too, beside it) once after the first paint, never on the cold
start, and shuts down in order (timers, then the work, then the database). It declares small interfaces that the Wails glue
in `cmd/nilbox` implements, and imports no Wails. A window event is registered in
`main_desktop.go` with `application.RegisterEvent`, which generates its TypeScript type and
name in `frontend/bindings`; a repository test checks that every window event is registered.

## internal/appinfo

Settings → About's **Report a bug** opens `.github/ISSUE_TEMPLATE/bug_report.yml` and fills in its
`environment` field: the versions, then the settings and integrations, which core words
(`internal/core/bugreport.go`, `appinfo.StateReader`) with nothing personal in them, a name or
path of the user's own told only as the default or the user's own, or counted; its test fails on
any. The settings are worded one line per page of Settings, under the page's name and in the
sidebar's order (`internal/core/bugreport_settings.go`), from Appearance to Labs, whose line,
whether Autopilot (beta) is on, is always there. The settings kept outside the settings service
are among them: the default prompts and replies (each a starter, the user's own, none, or a
missing prompt), the profiles and the launch defaults (counted), and, in Autopilot's section,
Describe it's agent. Autopilot's section shows while Autopilot is on or the user has a routine.
`TestABugReportWordsEverySetting` changes each field of `settings.Settings` in turn and fails
when the report doesn't change with it, so a new setting is worded there or listed in
`bugReportLeavesOut` with the reason it stays out (a catalog, what the window remembers, a
path, or a setting another section words); it fails on a key that names no field too. A full
report's link stays under 8,000 characters (`TestABugReportStaysShortEnoughToOpen`). The form is
synced, so if a sync renames that field, the button stops filling
it in. The `internal/appinfo` tests read the form and fail when that happens. The account
menu's **Report a bug** opens the same link, and its **Send feedback** opens GitHub's own blank
issue (`template=BLANK_ISSUE`, `appinfo.FeedbackURL`) with a `[Feedback]` title, a few prompts,
and the app's version filled in; a synced `config.yml` with `blank_issues_enabled: false` would
turn that link into the template chooser. The `magefiles`
tests likewise fail if the synced `10-mage-x.env` stops pinning golangci-lint, which `magex
app:lint` reads.

## internal/devtools

The inspector a build from source carries, which the `magex inspect:*` targets talk to
(`magefiles/inspect.go`; the user's side is in
[the inspector](desktop.md#the-inspector)). It is plain Go, tested on Linux; the desktop side
is `cmd/nilbox/devtools_desktop.go` and, for the picture, `devtools_darwin.go`.

**Only builds from source.** Every file that imports it builds only with the `devtools` tag,
and magex passes that tag to a build from source alone: `buildTags` in
`magefiles/bundle.go` gives it to `app:dev` and to the bundle `app:build` makes (no flavor),
never to a release or a rehearsal, which the magefiles' tests pin, and
`TestOnlyBuildsFromSourceCarryTheInspector` (`internal/repoguard/devtools_test.go`) fails on a
file that imports it and builds without the tag. A release's `devKit`
(`cmd/nilbox/nodevtools_desktop.go`) does nothing, so the bootstrap reads the same either way.
`app:lint` and `app:test` lint and test the desktop code both ways.

**The socket.** `Listen` listens on `devtools.sock` in the data folder, mode 0600, after
removing a socket a run left behind; one another running copy answers on is left alone
(`ErrInUse`), and the inspector stays off, which the log says. A connection carries one
request, one JSON line (`Request`: a command and string arguments, as magex passes them), and
gets JSON lines back (`Line`), ending with `end` or `error`. Each line is written within ten
seconds, so a client that stops reading never holds the app up, and a client closing its end
ends a command that follows. It speaks no HTTP: only `internal/github` imports `net/http`.

**What it keeps.** `Feed` keeps the last entries of one kind and hands each new one to its
followers, never waiting on a slow one. The log is kept by `LogHandler`, which the bootstrap
adds beside the app's handlers, from Debug up whatever level they write at; the events by
`Tee`, which wraps the relay the services emit into; the page's console and the user's trail
by the agent. `stream` sends a feed's last entries, or several feeds' in time order (`watch`),
then follows: it starts following before it reads what is kept, so nothing between the two is
missed.

**The page.** `agent.js`, embedded, is the agent. The bootstrap runs it in the main window,
the first the app opens (`app.Window.OnCreate`), on every page load
(`events.Common.WindowRuntimeReady`, then `ExecJS`), and `Page.Eval` runs it again ahead of
each evaluation, so a page that lost it gets it back; it installs once. The agent posts to the
app through the web view's message handler, each message prefixed `devtools:` (`Prefix`); the
app's `RawMessageHandler`, set only in a build with the inspector, hands each message from the
main frame to `Page.Receive`. An evaluation's code goes to the agent as a JSON string with an
ID; the agent compiles it as an expression, or else as statements, runs it with its helpers as
`nb`, and posts back the value, made safe for JSON (an element as its description, a cycle cut
short, depth and size capped), or what it threw. The trail names a control by its label or text,
anything else by its label alone or its text when it holds no other element, and keystrokes in
a field only as typing in it, once per pause, never the keys. A card's description reads whether
the web view draws it and what is inside it (`checkVisibility`), which is what
`content-visibility` once got wrong (index.css says why no inbox or notification row uses it).
The agent runs outside the frontend's sources, so `frontend/src/test/devtoolsAgent.test.ts`
runs it in jsdom.

**Pointing.** ⌥⌘P in the window, the menu bar's Point at an Element (`Page.StartPointing`),
or `nb.point` (`inspect:point`) turns pointing on. The agent's listeners are on the window in
the capture phase, so they hear every key and press before the page does, a sheet's
`useModalDialog` (which listens on the document) included: ⌥⌘P works over a sheet, and Escape,
swallowed there, never closes one. While pointing, the agent outlines the element under the
pointer, or, with ↑ and ↓, one around it or back in (kept while the pointer stays inside it),
and swallows every press and click, so the app never acts; a click opens a box for a note on
what is outlined, and Enter posts a `note`. Everything it draws is one element, the layer,
appended inside the open dialog the pointer is over (else the last `dialog:modal`, else the
body) and shown as a `popover="manual"`: a modal dialog leaves the rest of the page inert, and
the top layer draws the popover over it. Every event from inside the layer, and focus moving
into it or back out, stops at the window, so the app hears neither the note's keys nor focus
leaving it. The note carries the trail's target and where, the element as `describe` has it,
its computed styles that are set, the text around it, the view (the `nav` items marked pressed
or current), and, when the fiber React keeps on the element has a `_debugOwner` (the
development build of `app:dev`), the component chain from the fibers' `return`s and the first
`/src/` frame of `_debugStack`, its line the compiled module's and so approximate. The agent
hides the layer before it reads the page and posts. `Page.Receive` numbers the note and puts it
on the trail (kind `note`), then, on a goroutine of its own, since the picture needs the main
thread the message came on, takes a picture with the shot `AttachShot` gave it, tells the agent
to show itself again (`noted`), boxes the element on it (`Outline`, scaled by the picture's
width over the viewport's), and keeps the note after the one before it.

**Notes and the off switch.** `Notes` keeps the last 50 notes in `InspectorDir`'s `notes/`,
each as `<n>.json` and `<n>.png`, written through a file beside it and renamed, and reads them
back as the app starts (`Page.Open`, from `devKit.start`), so numbering goes on from the last
one. A note is open until `inspect:notes done=` marks it; `inspect:notes` shows the open ones,
and `follow=true` streams each new one, which is how an agent hears of a note as it is left.
`settings.json` beside them keeps the one setting, whether pointing may start. `Page.sync`
tells the agent the state (`InspectorState`) as the page loads and after each change, and
`OnChange` tells the desktop side, which redraws the menu bar's menu when the setting changes.
While it is off, the agent leaves ⌥⌘P to the app and `inspect:point` refuses (`ErrPointingOff`).
The window's Settings reach the agent, not Go: `platform.inspector()` returns
`window.__appDevtools.inspector`, whose choices the agent posts (`inspector` messages), and
`InspectorCard` in About renders only when it is there. A release has no agent, so the card
renders nothing and adds no listener, its `devKit.menu` and `redrawMenu` do nothing, and it
writes no `devtools/`. "Find a setting" leaves the card out, since its index ships in a release.

**The picture.** `devKit.shot` has the web view take a picture of what it draws
(`WKWebView takeSnapshotWithConfiguration`), as PNG, on the main thread, and waits for it on a
thread of its own; one that takes longer than the request allows is left to finish.

**The database.** `store.ReadOnlyQuery` and `store.Tables` read the live database over a
connection of their own, `mode=ro` and `query_only`, and run one statement that reads (SELECT,
WITH, EXPLAIN, VALUES, or a PRAGMA that sets nothing): the one way a package other than a
table's owner reads a table, for a build from source alone, and never to write.

**Trust.** The socket is the user's alone, and anything the user runs could already read the
data folder. But a script the inspector runs can do whatever the window can, writes to GitHub
through the bound services among them, which is why only a build from source carries it.

## internal/store

**Migrations and squashes.** The schema is the migrations in `internal/store/migrate/migrations/`,
each recorded with its file's checksum once it runs. A migration a build has shipped is never
edited or deleted: a database that ran it would read as a history this build disagrees with and
be set aside. To combine shipped migrations into fewer files, squash them, which carries every
user's database over without touching its data:

1. Make sure the users run the newest release; only the histories a squash freezes carry over.
2. Freeze each history users' databases may have, before deleting anything:
   `go test ./internal/store/migrate -run TestFreezeASquashedHistory -freeze NNNN`, NNNN the
   newest version of the history (the newest release's, and the tip's when a migration came
   after it). It writes `squashed/NNNN.json` (each version, name, and checksum) and
   `squashed/NNNN.schema` (the schema those migrations build, `schemaOf`'s form: comments,
   whitespace, case, and quotes left out, a table's columns and constraints one per line).
3. Write the base: whole `CREATE`s at the next hundred above every version any build shipped
   (`0100`, then `0200`), the columns in each table's order, an added column after the last
   one and before the table's constraints, as SQLite's `ADD COLUMN` writes it, and no data
   steps (they fixed rows older builds stored). A migration after the newest release stays a
   file of its own after the base.
4. Set each history's `through` (the newest base migration that builds its schema) and its
   `about`, delete the old files and their per-migration tests, and run `magex db:generate`:
   the generated code must not change but for comments that name a migration.

`TestEverySquashedHistoryBuildsTheSchemaOfItsBase` builds the base through each `through` and
compares it with the frozen `.schema`, and a database whose history is exactly a frozen one is
carried over by `migrate.Apply`: backed up as `-v<its newest>`, its history rows swapped for the
base's in one transaction, nothing run against its tables, then the later migrations applied. Any
other history of versions below the base is `migrate.ErrSquashedHistory`, which `store.Open` sets
aside as `ResetHistory`. An older build that opens a database carried over finds versions above
its own and refuses it as a newer build's, offering the backup taken before the carry over
(`store.KnownSchemas`, which `desktop.RestoreOffer` reads, knows the frozen histories' versions
too). A later squash drops the histories it can't carry over (their `through` is gone), so a
database two squashes old is set aside.

**Rebuilding a table.** SQLite can't change a CHECK, so a migration that widens one makes the
table again inside its own transaction: a new table with the wider checks, its columns in their
order (so the generated code doesn't change) and every other constraint and default as before,
every row copied as it is under explicit column lists, the old table dropped and the new one
renamed, and its indexes made again. `0112_repo_sessions` does that to `agent_sessions`, which
no table references and no trigger or view names, for the sessions a repository's launch
starts (no item: kind `''`, number 0, item 0, and the worktree it made, never `''`;
[internal/sessions](#internalsessions)). A table another references takes one step more, which
`0111_repo_prompts` takes for `prompts`, whose `applies_to` may now be `'repo'` (with `CHECK
(applies_to <> 'repo' OR purpose = 'agent')`): foreign keys stay on, and `launch_defaults`
names prompts in six `ON DELETE RESTRICT` columns, which refuse the old table's drop at once,
deferred or not. So the launch defaults that name a prompt set their six columns aside in an
explicitly typed scratch table (`launch_defaults_prompt_ids`) and clear them; the new table
takes every prompt under its own id, the old one is dropped and the new one renamed (a foreign
key names its table by name, so the six name the new one once it is called `prompts`); each
column takes its value back through a correlated subquery; and the scratch table goes. Each
migration's test fills the table first (for `0111`, every one of the six references) and checks
that every row and column comes through as it was, that the new checks hold, and, for `0111`,
that `pragma_foreign_key_check` finds nothing and a prompt a launch default names still can't be
deleted.

**Self-heal.** `store.Open` backs the database up before applying pending migrations. A file
that is not a database or is damaged, a schema the migrations did not create, or a history this
build disagrees with is renamed, with its `-wal` and `-shm` files, to
`<path>.broken-<UTC stamp>` once no other process has it open; a fresh database starts in its
place, and `Opened.Reset` reaches the window as a notice. A database from a newer build is
refused with `store.ErrNewerSchema` and left untouched, and the startup dialog offers this
build's way forward without opening it again (an update through `update.Bare`, or the command
that installs or builds a newer build; see [cmd/nilbox](#cmdnilbox)). Any other failure (a lock, a
permission, a full disk, a failed migration) is a plain error, and nothing is moved. A reset
the user asked for (Settings → About, or `magex app:reset`, each writing
`appdata.RequestReset`'s request) is carried out by the next start, in core's `openData`,
before anything opens the database: `store.OpenFresh` waits about five seconds for another
copy of the app to let go, then sets the database aside as `<path>.reset-<UTC stamp>`
(`ResetRequested`, no error) for a fresh one, putting it back when the fresh one fails;
`store.PruneResetCopies` keeps the newest three, Describe it's conversations and proposals,
which name routines that are gone, are removed (`forgetConversations`; its plugin, the plugin's
link, and `agent.json` stay), and the request is cleared. A database still
held is a busy error, the startup dialog's, with the request kept for the next start; with
no database file there is nothing to set aside, and a request that can't be read is logged
and dropped, never carried out. `store.Backup` takes a backup of the open database whenever
asked, as an update's restart does before it installs, named and pruned exactly as the
migrations' (`migrate.Backup`: `<path>.backup-<UTC stamp>-v<schema>`, the newest three of
both kept; `store.Snapshots` reads the schema only from a name ending `-v<digits>` or
`-v<digits>-<n>`, so a copy renamed `-v4-copy` names none), and `store.RestoreBackup` puts one of those backups back in the database's place
for an older build that finds a newer schema: it refuses a file that isn't one of the
database's own backups, waits for another copy to let go, sets the database aside as
`<path>.broken-<UTC stamp>` with its `-wal` and `-shm`, and copies the backup in, putting the
database back when the copy fails.

**Prompts, launch defaults, and the user's scripts outlive the database.** They are what the user made, which
GitHub can't give back. `prompts.LibraryFile` rewrites `prompts.json` beside the database
after every change to the library, and `launchdefaults.File` rewrites
`launch-defaults.json` after every change to the launch defaults or the library, since it
names prompts by nickname: each row's six default prompts, the kinds' and the situations'
(`launchdefaults.Kept`, the four situations' left out when they inherit, so an older copy
inherits them), which `promptsOf` lists once for the copy, a restore, and a backup's
import. At startup, when the library is new, `prompts.Restore` brings it
back before `Seed` runs, from the database set aside or else from `prompts.json`, and core
(`internal/core/kept.go`) restores the launch defaults from the same place, matched to the
restored prompts. Core writes each copy at startup only when it is missing or the data was
just restored, so a loss in the database never overwrites a copy. The Autopilot routines
keep a copy too: `autopilot.File` rewrites `autopilot.json` (the export format) after every
change to a routine, and when `autopilot_routines` holds no row at all, deleted ones
included, `autopilot.Restore` brings them back after the prompts and the launch defaults,
from the database set aside or else from the copy, each switched off, its spec stored as it
was (a newer build's stays broken, never rewritten); a routine names its prompts by
nickname, which the restored library keeps. What neither copy nor a restore carries is the
user's confirmation that a routine may merge past the rules (`autopilot_routines.past_rules_hash`,
added by migration `0110_routine_past_rules`, which `Restore` never reads): a restored routine
with a tick asks for it again. The user's own scripts
have no copy, but come back from a database set aside, whatever the reason (`scripts.Restore`,
beside the restore of the prompts, only into an empty list, each checked as `Save` checks
it), so a reset keeps them. Agent profiles need no
copy: discovery finds them on every start, and nothing in the app edits one. `store.OpenSnapshot` reads a kept database of any
schema, read-only and tolerant of missing columns, for the restore and for **Restore from a
backup**. Import and restore go through one planner (`internal/prompts/importer.go`), which
never changes a prompt the user wrote or edited, and checks every imported setting the way
the editor does. A starter's copy whose text the user never changed gets a later build's
text: a change to a starter's text keeps the text it replaces, byte for byte, in the
starter's `Earlier` (pinned by SHA-256 in `TestEarlierStarterTextsAreAsShipped`), and
`Seed` gives every copy still holding one the new text, keeping its title, nickname, and
settings, as an import does a file's; a copy whose text the user changed, or a duplicate, is
never touched. A starter reply becomes its slot's default reply (`prompts.ReplySlot`: `approve`,
`approve_bots`, `request_changes`, `comment`, and `nudge`, each in the pref
`prompts.default.reply.<slot>`, `approve_bots` as `approve.bots`) as `Seed` adds it, when the slot
has none, so a library from a build before a slot existed gets the slot's starter and its
default once: the **Nudge** reply (`reply-nudge`, a comment reply for pull requests) that a
card's Nudge posts, which the review sheet's comment replies carry as `review.ReplyList.Nudge`,
so the nudge dialog fills it in through the same `FillReply`. After the starters, `Seed` fills
the app's prompt for each situation a pull request can be in (`prompts.DefaultSlot`'s
`checks_failing`, `conflicts`, `changes_requested`, and `rereview`, the prefs
`prompts.default.<slot>`) once, from the library's copy of the
slot's starter when it has one, and records the slot in `prompts.seededSlots` either way, so a
slot the user cleared, or whose starter they deleted, stays as they left it; unlike a kind's, a
situation's default never falls back to another prompt. The record travels with the library
like `prompts.seeded`: a whole library's `prompts.Bundle` carries it as `slotsSeeded`
(`prompts.json`, an export, a backup's prompts section), `snapshotRecords` reads it and the
four situations' prefs out of a database set aside, and `prompts.Restore` stores it, so a reset
or a restore never fills a cleared slot again, while a library restored from a file without
it, as an older build's, has each slot filled once. A starter's command names a value a repository, a clone's config, or an
author chose only when it is plain (`.BranchIsPlain`, `.RemoteIsPlain`, worked out from the
raw value in `Data.sanitized`, since sanitizing can hide characters), and then through the
`shellquote` template function (`TestStarterCommandsAreInert`). The file format is `prompts.Bundle`, specified in
[`docs/prompt-files.md`](docs/prompt-files.md); keep the two in step, and raise
`BundleVersion` only for a change an older build would misread. An import offers the file's
defaults (`ImportPreview.Defaults`, each a `DefaultOffer` where its prompt fits the slot and it
would change something) and applies the chosen ones in the transaction of the prompts,
through the nickname links `importInto` returns. The user's whole setup, these three
included, also leaves the app as a backup (`internal/backup`, its format in
[`docs/backup-files.md`](docs/backup-files.md)), whose prompts, launch defaults, and
Autopilot sections are exactly these three copies' formats; keep the backup's in step with
them.

**Prompts for repositories.** A prompt may apply to repositories (`prompts.AppliesToRepo`,
`applies_to = 'repo'`, which the window calls Repositories; migration `0111_repo_prompts`,
above): an agent prompt alone, never a reply (the table's check, the editor's
`checkReplyAppliesTo`, and an import, which reads a reply's `repo` as `pr`), which a
repository's launch fills in for a clone rather than for an item, as the Deps page's Upgrade
does ([internal/launcher](#internallauncher)). It covers no kind of item (`AppliesTo.Covers`),
so no launch on a pull request or an issue fills one in, nor can one be a kind's or a
situation's default (`checkAppliesTo`), and `Prompt.IsRepo` tells one. Its text is checked
with the sample repository (`SampleRepo`: acme-org/example-repo, `main`, and `origin`, and
again with no default branch or remote, each in the clone and in a worktree named
`deps-20260115`), and previewed with it, an item the editor picked set aside with a note
(`repoPreviewData`); a launch fills it in from `RepoData` (the repository's names, its default
branch, and the clone's remote) and `WithWorktree` (the worktree the launch made, by the name
it was made under), never `WithSession`, which names an item's worktree. Every variable but the
repository's and the session's (`.Repo`, `.Owner`, `.RepoName`, `.DefaultBranch`, `.Remote`,
`.RemoteIsPlain`, `.InWorktree`, `.Worktree`, `.Profile`, and `.Me`, `repoVariables`) is
`Variable.ItemOnly`, always empty there, and the checker warns where a prompt for repositories
names one, as it warns of a pull request's variable in a prompt for issues. A prompt a routine
runs can't be made one (`checkRepoSwitch`, at the applies-to field; an import never saves one
over a starter's copy a routine runs either, `takesPlace`), since a routine runs its prompts on
pull requests and issues; one already for repositories stays free to change. Two starters are
for repositories (`starters_repo.go`, added after the others): **Upgrade dependencies**
(`deps-minor`, nicknamed Upgrade deps), which keeps each dependency within its major version
and lists the newer majors it left alone in the pull request, and **Upgrade dependencies, majors
too** (`deps-major`, Upgrade majors too), which keeps only the majors whose checks pass after a
reasonable fix. Each starts in a worktree, out of plan mode, and has the agent learn the
repository's rules, upgrade its Go modules (`magex deps:update all-modules stable-only`, with
`allow-major` for majors, where magex is set up, else `go get -u ./...` and `go mod tidy`) and
its JavaScript packages (with the lockfile's manager, never another), run the repository's own
checks, commit, push with `git push -u <remote> HEAD`, and open the pull request with `gh pr
create --repo <repo>` against the default branch; the remote is named only under the
`.RemoteIsPlain` guard (origin when none is known), and `--base <default>` only when
`shellquote` leaves the default branch as it is (`defaultBranchOK`), each through `shellquote`,
so a remote that isn't plain gets no push, and the session commits and stops. Prompts for
repositories raised `BundleVersion` to 4: an older build reads an `appliesTo` it doesn't know
as `any`, so it would import a prompt for repositories as one for both kinds of item, and a
file of an earlier version holds none. Describe it leaves the prompts for repositories and
their starters out of its snapshot (core's `itemPrompts` and `itemStarters`), as no routine
runs one, and the draft's check treats a prompt written for no kind of item as a problem,
whatever the routine acts on (`kindsProblem`).

## Shared building blocks

Services share their building blocks rather than keep copies: `internal/repo` and
`internal/item` name repositories, pull requests and issues, and their worktrees;
`internal/agent` is the backend registry of the coding agents, one spec each (Claude Code,
Codex) with its command, config variable and markers, effort levels, models, permission
modes, reserved flags, and how a launch's options become its flags, a background run's
fence included (`AccessFence`, which `claude_fence.go` turns into Claude Code's preset flags
and `--settings`, and `codex_fence.go` into Codex's sandbox, writable folders, and `-c`
fence hook; `CheckFenced`, the flags no background run's extras set; `KeepsPreset`, each
spec's own: Codex keeps every preset but GitHub tasks only), its version, sign-in, and
login commands with their parsers (`ParseVersion`, `ParseSignIn`, which keep no email), the
API key variables a launch unsets unless the user's setting lets a key through (`KeyEnv`),
the status page part it depends on (`StatusPart`), the conversation half Describe it talks
with Claude Code through (`conversation.go`, `claude_conversation.go`: a turn's flags,
`ConversationArgs`, which take no extra arguments, its MCP config, `ConversationFiles`, the
plugin's files and commands, `PluginFiles` and `PluginArgs`, the tool names' prefixes, and
`claudeConversationFloor`, 2.1.328, which `claudeConversationReviewed` holds every flag,
command, and file key to; [internal/describeit](#internaldescribeit) has the argv), and its
headless half for a run
in the background (`HeadlessArgs` around the fence's own flags, `HeadlessEnv`, `HeadlessFiles`
for a schema read from a file in its strict form, `StrictSchema`, `Headless` with what it can
do there, which `Agent.Background` shows the builder, and the version whose flags it needs,
the fence's floor: Codex's `codex exec --ignore-user-config --ignore-rules`, so no branch's or
user's Codex config, MCP server, or hook loads, and its Take over `codex resume <thread>`), so adding an agent adds a spec and the launcher,
profiles, and editors read the rest from it (`TestOnlyTheAgentsHomesSpellTheirFlags` fails
on an agent's flag or program name spelled anywhere else but its parsers and observers); `internal/procs` lists
the running processes' working directories (`lsof` on macOS, `/proc` on Linux), which tell
a worktree some session is in, reads a process's start time (`StartedAt`), which tells a
process from a stranger that reused its PID, and probes a PID as running, gone, or unknown
(`Probe`, which never signals one `ValidPID` refuses and calls it unknown, so an agent's
worktree lock naming one is kept); `internal/execpath` finds and runs external
programs (`Finder`, `Runner`; `Search.Path` searches the login shell's PATH), holds a program's
standard output to `Exec.Limit` bytes when that is set (one that prints more is stopped, and
`Run` fails with `ErrOutputLimit`, returning none of it, so output a repository controls, as
gitread's tree, log, and blob reads, can't grow the app's memory without bound), and starts a
long-lived one in its own process group (`Start`, `Proc`), writing straight into a file the
app tails and stopping the whole group with a signal ladder; nothing else starts a process
(`TestOnlyExecpathStartsProcesses`, with named exceptions), and nothing runs `go install` or
`sudo` (`TestNoGoInstall`, `TestNoSudo`); `internal/shellenv` reads the user's login shell's
environment, after the first paint, and never logs a variable's name or value; and
`internal/clock`, `internal/periodic`, `internal/flight`, `internal/paths`,
`internal/progress`, and `internal/textutil` hold the clock (and `clock.Sleep`, the shared
wait that ends with its context), the interval runner, one call per key at a time, `~`
paths, a long read's progress told to the window a few times a second (the Release page's
list, the window's worktrees listing, `event.WorktreesProgress`, the Deps page's read of the
clones, `event.ManifestsProgress`, and every whole refresh of
the inbox, `event.InboxProgress`, beside `inbox.Snapshot.Refreshing`), and text helpers,
among them the app's one POSIX shell quoting: `textutil.ShellQuote`, which always quotes,
for every word of a script the app writes, of the ssh command it hands git, and for the
program of Repo scripts' chain line unless it is a path from `/`, and
`textutil.ShellWord`, which leaves a word plain only when every POSIX shell reads it as
itself (ASCII letters, digits, and `=-_./:,+@%`, not starting with `=`), for the commands
the window shows or copies and the chain line's folders and arguments.
`internal/toolsigns` names the files that show a clone is set up for a tool, each a path from
the clone's top with slashes, a folder counting whatever it holds: `MageX`, mage-x's
(`.github/env/10-mage-x.env`, `.mage.yaml`, or a `magefiles` folder). It is a leaf that imports
nothing of the app's, so the setup's Tools step, which counts the clones that hold them, and
Deps, which finds them in the tree of the commit it reads, share the list without either
importing the other.
A file a repository or an agent controls (a clone's `go.mod`, `t3.json`, `tasks.json`,
magefiles, go-pre-commit config and hooks, the signing files, the `.git` file and
`commondir` the sweep's count of worktrees follows, an agent's transcript) is read
only through `paths.OpenRegular` (`ReadHead`, `ReadTail`): a regular file, or a link to one,
opened without waiting and checked again once open, so a named pipe swapped in is refused
(`paths.ErrNotRegular`) rather than hanging the read.
`internal/testutil` holds the fakes and fixtures the test suites share (a fresh database, an
isolated git, `Commit` for a fixture file, git templates built once per test binary, the
`Planter`, which plants fake programs in a clone's own config for the guard tests, recorded
runs of real tools, a runner, finder, process lister, clock, event sink, log, fake tools, and
`TerminalScript`, the shell a fake terminal runs a run's command under); only tests
may import it. `internal/scriptrun/scriptruntest` holds the runner the
hooks, scripts, and release suites share, built on it: a real `scriptrun.Engine` and its
Service over a fresh database, the next start over the same history (`Restart`, `Next`),
a fake terminal app that runs each run meant for the user's terminal under `/bin/sh`
(`Terminal`, through `WithTerminal`; `Close` hangs it up mid-run), and `Ended` and `Await`,
which look again only when the engine's `Changed` says the runs changed, never on a timer
(`AwaitShell`, for the shell the engine notes without telling, alone polls); only tests
may import it either. `internal/github/githubtest` holds the fakes of `internal/github` the
suites share, which `internal/testutil` cannot hold as `internal/github` imports it: `Tokens`,
a token source that lends in turn, fails as told, and records what it was told to forget;
`Calls`, a fake GitHub's requests counted by name and in order; and `BadCredentials`,
GitHub refusing the token; only tests may import it too.
`internal/localrepos/localrepostest` holds the fake clone root the hooks, scripts, and
release suites share (`Clones`: the roots, the clones listed in them, and each clone's
origin, each read counted, failed, or hooked as a test says); only tests may import it as
well. `internal/assignees/assigneestest` holds the in-memory `Directory` both sheets' suites
hand their assignee Finder, which keeps each read and records who was assigned and which reads
failed. The repoguard guards on SQL and on the git verbs the app runs treat every
test-support package as they treat `_test.go` files.

## internal/budget and internal/github

`internal/budget/` keeps the GitHub API budget current. The one shared `github.Client` hands
every response's rate-limit reading, and what the request spent, to its `Tracker`
(`github.RateObserver`), whose state the `Saver` keeps across runs. Every GitHub request goes
through `(*github.Client).do`, apart from three reads that aren't the API and carry no token:
a profile picture (`FetchAvatar`, by the URL GitHub gives: the user's own, and a bot author's,
which the item fields ask GitHub for, since the avatar host answers a bot's login with a
generated pattern; and anyone's by login through `FetchLoginAvatar`, which builds the avatar
host's URL itself and refuses a login no user could have, such as a bot's; either way https on
the avatar host alone, every redirect on it too), a public release file (`DownloadAsset`, https only, its
host and every redirect's on `DownloadHosts`), and a status page's summary (`FetchStatus`:
GitHub's, Claude's, or OpenAI's, https on one of `StatusHosts` alone, a clean absolute path,
every redirect on the request's own host, at most 1 MiB, conditional on its last ETag, so a
304 carries no body). Never build an HTTP request anywhere else, nor run
`gh api`;
`internal/repoguard/githubrequests_test.go` fails the build if anything does. Every write goes
through the `WriteGate`, a REST one through `edit` and a GraphQL mutation through `mutate`
(`postGraphQL` refuses a mutation), but the chore lane's: marking a notification thread done
or read, sent only by a rule the user turned on or by the user's click, goes through `chore`
and the client's own `ChoreGate` (nil refuses every chore), whose `AllowChore` paces it apart
from the edits, with counts of its own (50 a minute, 300 an hour, a second apart), the same
reserve, and the same hold, so a wait GitHub asks of either lane stops both;
`internal/repoguard/githubwrites_test.go` keeps it so. The notifications listing is
conditional (page 1 sent with its last ETag as `If-None-Match`, through `do`'s `withETag`:
measured in November 2026, github.com answered `If-Modified-Since` with a charged 200 and
`If-None-Match` with a free 304), and a 304 spends nothing, but says only that page 1 is as it
was; `internal/repoguard/githubnotifications_test.go` holds the client to `GET /notifications`
and `DELETE` and `PATCH` on `/notifications/threads/{id}`. Every request carries what it was
sent for (`github.WithPurpose`: the inbox's refresh, the routines' searches, the checks
follower, or Autopilot's own reads), and the `Tracker` splits each budget's spend by it
(`Reading.Purposes`, which add up to `Spent`); the budget menu says Autopilot's share in one
line, only once it spent something.

**Held workflow runs.** GitHub's approve endpoint (`POST .../actions/runs/{id}/approve`,
`ApproveWorkflowRun`) takes only "a fork pull request" or a run "queued by the Actions bot", in
its own words, and refuses any other run it holds, as one a review (`pull_request_review`)
started in the base repository, even on a fork's pull request. The details fragment reads, of
each head commit suite, its creator's login and its run's event, and the pull request's
`isCrossRepository`, none of which costs a point (measured in November 2026), and `suiteHold`
tells the held runs (suites that ended `ACTION_REQUIRED` with a run) apart: a `pull_request` run
on a fork's pull request, or one whose suite `github-actions[bot]` created, is approvable
(`Checks.Awaiting`); any other waits for approval on GitHub (`Checks.AwaitingOnGitHub`,
`UnrunSuite.OnGitHub`). The fragment reads each run's workflow's name too (`workflowRun {
workflow { name } }`, one object, which GitHub's point formula never counts), and
`Checks.Held` lists the held runs with it, for the ignored workflows (internal/heldruns). A
run waiting on a deployment environment's reviewers hasn't ended, so it is neither: its job's
check counts as running. A refusal for what the run is still comes back
as a 403 or 422 like any other, with no code of its own, so `approveError` tells it by GitHub's
message ("fork pull request") and wraps `ErrRunNotApprovable`, keeping GitHub's words.

**How an issue is planned.** The details fragment reads, of each issue, what GitHub's sidebar
shows beyond its projects (`issueInfoFields`, `github.IssueInfo`): its type, milestone, parent,
the open issues it is blocked by and blocking (`issueDependenciesSummary`), and its values in
its organization's issue fields (`issueFieldValues`, single select, multi select, number,
text, and date). All are single objects but the field values, one connection per issue, and a
batch's price stays where it was (measured in November 2026: 1 point for 25 issues with them
and without, 2 with the projects either way). Each kind of field value aliases its value apart
(`number`, `text`, `date`) so values of different types never share a name. Because the
fragment is shared, a card's own read and the issue sheet's view read them too. The inbox
stores them as one JSON object, `item_details.issue_info` (migration `0104_issue_info`), and the card's
peek shows them as **The issue**, in the column a pull request's change takes.

**An issue's closing pull requests.** The issue's part of the fragment also reads the first
five open or merged pull requests that close it once they merge (`closingPullsField`,
GitHub's `closedByPullRequestsReferences`, aliased `linkedPulls` so it never meets the issue
sheet's own `fixes`, which take other arguments), each with who opened it and whether its
branch is in a fork. Measured in November 2026, it leaves a batch's price where it was (1
point for 25 issues, 2 with the projects); the same link read on a pull request's side
(`closingIssuesReferences`) would cost a point more a batch, so it is read on the issue's side
alone. The inbox stores them in `item_details.closing_pulls` (migration `0105_closing_pulls`),
with the author, and decides each one's trust as the snapshot is built, with the trust
policy of the moment: the user's own, or one the policy trusts (a fork's never is). In the
window, `lib/cardLinks` indexes the lists: an issue's card links to its closing pull
requests, and a pull request's card finds the listed issues that name it, so a pull
request's own read asks for nothing. A trusted fix turns an issue's story to **Fix in
progress** or **Fix merged** (`nextStep`); an untrusted one only links.

**Auto-merge bots.** Many orgs run a GitHub App of their own (such as `acme-org-automerge`)
that approves a bot's pull request and merges it once its checks pass. The fragment's
`latestReviews(first: 50) { nodes { state body author { __typename login } } }` reads each
latest review's author with its type, scalars GitHub's point formula doesn't count, so the
read costs no request and no point more. `github.ItemDetails.ApprovingBots` lists the bots whose
latest review is `APPROVED`, by login, lower-cased (a person whose login looks like a bot's,
a bot that only commented, and a dismissed approval never count), and the inbox stores them
as `item_details.approving_bots` (migration `0114_approving_bots`; a row stored before it
reads `[]` until the next details read). The match happens as the snapshot is built: the
`trust` package declares the setting, `trust.mergeBotsOn` (off by default) and
`trust.mergeBots` (empty; no build ships a name), which settings alone writes
(`SetMergeBotsOn`, `SetMergeBots`, normalized as the trusted bots are, in `Portable`), and
`trust.MergeBots.Match` names the first listed bot that approved as `Details.MergeBot`. Both
keys are in the snapshot's `settingsKey`, so a change builds the next snapshot from what is
stored and asks GitHub nothing. The card shows an **Auto-merge** chip first among its chips
(`MergeBotChip`), the compact row its icon, and the review sheet a tag beside the trust tag;
`nextStep` and the state track are left alone, since the bot usually turns on GitHub's own
auto-merge too, which the track already shows.

**A card's comment count.** GitHub's `totalCommentsCount` counts a pull request's comments
and its review comments, but not a review's own text, such as a bot's note as it approves,
which the review sheet's Comments tab counts (`commentCount`). The same `latestReviews` read
takes each review's `body` (text, no point more), and `ItemDetails.ReviewTexts` counts the
latest reviews that say something, a pending draft left out. The inbox stores it apart, as
`item_details.review_texts` (migration `0115_review_texts`), and adds it to `comments` only
as the snapshot is built (`Details.Comments`), so `comments` stays GitHub's own count, the
one a snooze's wake on comments compares. It counts one review per reviewer, the latest, so a
reviewer's earlier reviews with text count only in the sheet. The text itself is never kept.

**Who an item is assigned to.** The inbox query's searches match through GitHub's search
index, which trails an edit, while a node's fields are read live, so the details are what can
tell an item the user left on GitHub from one the search still returns. The fragment reads a
pull request's review requests already, and each pull request's and issue's assignees too:
`assignees(first: 10) { totalCount nodes { login } }`, every assignee GitHub allows
(`MaxAssignees`), the selection the issue sheet's and the review sheet's own queries ask for,
so GraphQL merges the two where they spread the fragment. `ItemDetails.Assignees` lists the
logins in GitHub's order, never nil when GitHub brought them all, and nil when the read can't
tell: the field absent or null, or fewer logins than `totalCount` (`allLogins`). It is never
stored; the inbox only decides which lists an item is in with it (see
[internal/inbox](#internalinbox)). Measured with `rateLimit(dryRun: true)` in November 2026,
it costs no point: a batch of 25 costs 1 point with it and without, and a read of 100 items 5
either way, where in the inbox query's assigned search it would double that search's price, 1
point to 2. The dry run gave GitHub's documented price for a batch of 50 issues with their
projects, 4 points, so it counts nested lists as a real read does.

**Reviewers.** The review sheet's reviewer edit asks everyone it adds with one REST request
and takes every request it drops off with one more (`RequestReviewers`, `POST
.../requested_reviewers`, and `RemoveReviewRequests`, `DELETE` at the same path), each through
`edit`, people and teams together: `reviewers` always, empty or not, since GitHub's `DELETE`
requires it, and `team_reviewers` when there are any. Both share one sender (`reviewEdit`) with
the sheet's own `RequestReview` and `RemoveReviewRequest`, whose bodies stay as they were and
keep their words about "you". Nothing is sent without someone to ask, a login GitHub could have
issued (`ErrInvalidLogin`), and a team slug of letters, digits, dots, hyphens, and underscores
(`ErrInvalidTeam`). GitHub answers with who the pull request asks after the edit: a login or
slug asked for that it doesn't list, or dropped that it still lists, is a change GitHub ignored
(`ErrEditIgnored`), named in the user's words ("GitHub didn't ask octo-other, acme-org/web to
review acme-org/example-repo#42.", a team as owner/slug), and a 422, as for someone without
access to the repository or the pull request's author, is worded with GitHub's own message
(`reviewersError`). The sheet's one read (`FetchPullReview`) also reads, at no extra point
(measured in November 2026), the reviewers GitHub suggests (`suggestedReviewers`, whose
`isAuthor` is `SuggestedReviewer.ChangedFiles` and `isCommenter` `Commented`), each latest
review's author with their type, name, and association (`Review.AuthorType`, `AuthorName`,
`Association`), and the names of who it asks to review, under the same `reviewRequests(first:
50)` the details fragment asks for, which GraphQL merges into one field: `PullReview.Requested`
holds the people of them (never a bot or a mannequin) and `RequestedTeams` the teams.
`FetchRepoPeople` reads who works on a pull request's repository, by the pull request's node:
whether an organization owns it, and its 50 most recently updated pull requests, each with its
author, each reviewer's latest review (`latestReviews(first: 10)`), and who it asks
(`reviewRequests(first: 10)`), every author with their association; people only, and a pull
request GitHub can't find is `ErrPullGone`. It costs one point (measured in November 2026: 101
connection nodes, which GitHub's formula rounds to one). `FetchRepoTeams` reads an
organization's repository's teams (`GET /repos/{owner}/{repo}/teams?per_page=100`, through
`do`, one REST point); GitHub refuses (403) or hides (404) them from an account it won't show
them to, as it hides them from one that isn't a member, which is `ErrTeamsHidden`.
`FetchMergeState` reads, by node, what a merge checks right before it (the merge state, the
head, open, draft, merge queue, auto-merge, and `viewerCanMergeAsAdmin`), one point.
`FetchOwnMergeState` sends the same query with `$own` set, which adds, under
`@include(if: $own)`, what a routine's merge of the viewer's own pull request past the rules
checks (`OwnMergeFields`, `ownmerge.go`): the viewer's login, `viewerDidAuthor`,
`reviewDecision`, the head's `statusCheckRollup` (its state and first hundred contexts), the
newest hundred commits' `signature { isValid signer { login } }`, the first hundred review
threads, and the base branch's classic protection and ruleset rules (the first fifty, with a
required-checks rule's contexts). Every list keeps GitHub's count beside its page, and a null
reads as not read, so `ChecksWhole`, `CommitsWhole`, `ThreadsWhole`, and `RulesWhole` fail
closed; any GraphQL error but the pull request's being gone fails the read. It still costs one
point (measured in November 2026 on a pull request with 39 checks, one signed commit, and a
ruleset of five rules); every other merge sends `$own` false and reads what it always did.

**Assignees.** Both sheets assign and unassign people with at most two REST requests at the
issues endpoint, where GitHub keeps a pull request's assignees too: `RemoveAssignees` takes
everyone off in one `DELETE .../issues/{number}/assignees`, and `AddAssignees` assigns everyone
in one `POST` at the same path, both through `edit` and one shared sender (`assigneesEdit`),
which `RemoveAssignee`, the inbox's Unassign me, uses too and keeps its words about "you".
Every login is checked before anything is sent (`ErrInvalidLogin`). GitHub answers with the
item as it stands after the edit, whose assignees the result holds (`Edited.Assignees`, never
nil after an assignee edit): a login added that it doesn't list, or taken off that it still
lists, is a change GitHub ignored (`ErrEditIgnored`), named in the user's words ("GitHub kept
octo-other assigned to acme-org/example-repo#42."). GitHub assigns at most ten people
(`MaxAssignees`). `FetchAssignable(kind, id, query)` reads who an issue's or a pull request's
repository can assign, by the item's node (`... on RepositoryNode { repository {
assignableUsers(first: 100, query: $query) { totalCount nodes { login name } } } }`), `""` for
everyone: the first page (`AssignableMax`) and how many match (`AssignableUsers.Total`), one
point; an item GitHub can't find is `ErrIssueGone` or `ErrPullGone`, by kind, through the same
`itemRepoRead` the labels read uses. The review sheet's read (`FetchPullReview`) asks for the
pull request's assignees too (`assignees(first: 10) { totalCount nodes { login } }`, all of
them, since GitHub assigns ten at most), into `PullReview.Assignees`; the read still costs one
point (GitHub's dry run, in November 2026: 2,722 nodes, ten more than without them), and so
does `FetchAssignable` (100 nodes).

## internal/inbox

These notes cover what the inbox does for Autopilot: the routines' searches, which each
refresh reads after the inbox's own four searches, and the reads Autopilot makes through it.
The inbox's own refresh, triage, edits, and the review sheet's Actor are described in the
package's doc comments.

**Two lists from four searches.** The refresh reads three searches (review requested,
authored, assigned) and stores each one's flag on the item (`reason_review_requested`,
`reason_authored`, `reason_assigned`), and `inbox.beyond.*` keeps what each matched past its
page. A fourth (`reviewed`: `reviewed-by:@me -author:@me`, the most recently updated first)
lists nothing by itself: submitting a review takes the user's request off a pull request, so
one its author pushed to without asking again would leave the inbox for good. It returns each
pull request with its head commit and the user's own verdict (`github.Verdict`, from
`latestOpinionatedReviews`), which costs the query no point (measured in November 2026). It
stores `reason_reviewed` (the raw match) and, when the verdict requested changes at a commit
other than the head (`github.Item.PushedSinceChanges`, any head move by someone else: a push,
a rebase, or Update branch), `reason_pushed`, which lists the pull request in Review requested.
Commits the user wrote never count, as a merge of the base they pushed to fix its checks: the
fourth search can't say who wrote the commits between the verdict and the head without a point
more every refresh, so the refresh asks (`coverOwnPushes`, `(*github.Client).FetchCommitAuthors`:
the newest 20 commits of each, with the login each author's email matches, one point a batch of
50) only about those whose head moved past the verdict, once per verdict and head, which
`ownPushes` keeps in memory while the app runs. When the user wrote every commit after the
verdict's (`ownSince`; an author matching no account is someone else, and so is a verdict older
than the commits read), the refresh moves the verdict's commit onto the head before it stores
it, so `viewer_review_commit` holds the newest commit the verdict covers, which only this rule
reads; a read that fails leaves it pushed to, and is asked again next time. It doesn't
ask GitHub for `review:changes_requested`, which matches the overall decision, someone
else's request included. Each row keeps the verdict (`viewer_review_state`,
`viewer_review_commit`), so the card says **Pushed since your review** in any list
(`Details.PushedSinceReview`, also while `review_left_at` holds it back), and hands the
card the verdict itself (`Details.ViewerVerdict`: `changes`, `approved`, or `""` with none,
read from `viewer_review_state`, so it costs no request), which lets a card whose user
requested changes and whose author hasn't pushed since say it waits on the author, and one the
user approved say so; one past the fourth search's page holds none, and its card falls back to
the other rules. A pull
request the fourth search returns alone and not pushed to is in no list: its details are never read
(`nodeIDs`, `fileTargets`), and no file check runs. `UpsertItem` gates `reason_pushed` by
`review_left_at` as it gates the request, so a review the user leaves from the inbox
(`recordReview`: `LeaveReview`, and `NoteViewerReview`, the new verdict at the head they
saw) takes it out at once and a refresh whose search trails it keeps it out until the pull
request moves. A read of the pull request alone reads the verdict too, at no extra point
(`opinionsField` in the item query beside one node, never in the followers' batches, where
it would cost a point more per 25; `ItemUpdate.VerdictRead` says it was asked, so a nil verdict
is none rather than unread): `RefreshItem` (a card's refresh, `Rereads` after an agent session
stops working, Autopilot's fresh read) and the review sheet's `ReadPull`. Its writer stores it
in the read's transaction (`noteReadVerdict`, `verdicts.go`) only when the token's account,
as the last refresh learned it (`Service.viewers`, never a request), is the item's, since
GitHub names the viewer's verdict by the token. Each row keeps when its verdict was submitted
(`viewer_review_at`, moving and cleared with the verdict) and when the newest verdict seen was
(`viewer_review_seen_at`, which only rises; migration `0101_inbox`). The same
review (state and second) is left as stored, so a commit `coverOwnPushes` moved stays moved;
an older one is left too (a review from the inbox since the read went out); any other is
stored whole, and none clears it (a dismissed review). A read never lists a pull request as
pushed to, which takes the whole refresh's question of who pushed, but one whose verdict is no
longer pushed past (an approval, changes requested at its head, or none) leaves Review
requested at once (`LeavePushed`, unstamped: the fourth search reads the verdict live and
agrees). A replay of the read over a whole refresh that outlived it stores its verdict again,
since the refresh's search may be older. A pull request newly pushed to arrives as `ArrivalReviewRequested` with
`Arrival.Pushed` only when it moved since the last refresh, so the first refresh after an
upgrade tells no burst. `RemoveReviewRequest` still refuses one with no request
(`ReviewRequestPushed`). The review sheet's **Re-request my review**, on the user's click
alone, asks GitHub for the user's review again through the `Actor`, which records it at once
(`Acted.Rerequested`, `recordRerequest`, one transaction): who GitHub answered the pull
request asks now (`setReviewRequests`), and `RejoinReview`, which sets
`reason_review_requested`, clears `review_left_at`, and stamps `review_requested_at` with the
later of GitHub's time of the edit and the item's, leaving `reason_pushed`, so the card reads
as an ordinary request (`ReviewRequestYou`). The sheet's reviewer edit, which asks others, is
recorded at once too, but only its answer (`Acted.RequestsEdited`, `recordRequests`, one
transaction): who GitHub answered the pull request asks after the edit's last step
(`setReviewRequests`), and nothing else, so it stays in whatever list it was in; two tries of
one edit keep the later answer (`Acted.with`). A pull request pushed to since that asks the user
by name again, as the details last read it (`review_users`), whoever asked, is
`Details.ReviewRequestedAgain`, and its card says **Review re-requested** in place of
**Pushed since your review**: at once after the user's own re-request, and through a refresh
whose search trails it, which lists it as pushed to again until the next. It was listed
already, so the next refresh tells no arrival; it tells the outcome sink nothing (`pullOutcomes`), since it is no review,
merge, or close. Autopilot's facts and searches keep GitHub's own three lists. The
snapshot alone partitions them (`addItems`): an item that asks for the user's review, or was
pushed to since the user requested changes, is in `ReviewRequested`, whatever else it is,
and one the user opened or is assigned is otherwise in `Yours`, with `Item.Authored` and
`Item.Assigned` saying which, so no item is in both and the lists' counts add up to
`NeedsYou`. `Yours.Total` adds both searches' counts
past their pages, an upper bound once either was cut. The window keys its sections by the
list (`settings.ListKey`); `settings.AdoptSections` carried the former Authored and Assigned
lists' window keys over to Yours once, and an older backup's inbox order is read the same way
(`adoptFormerOrder`). Autopilot's sources and conditions keep the three reasons
(`catalog.ListSource`), worded as reasons rather than lists.

**Lists left or joined on GitHub.** The searches match through GitHub's index, which trails an
edit, while each item's fields are read live, so for a while after the user leaves an item on
github.com the search still returns it, with its new activity, and a refresh that believed it
would list it again as just moved. `dropClosed` already reads the live state for a close or a
merge, and the inbox's own Unassign me and Remove me as reviewer stamp `assign_left_at` and
`review_left_at` (see `UpsertItem`), but those stamps cover only what the user does from the
inbox. A whole refresh reads each listed item's assignees with its details, at no point more
(`ItemDetails.Assignees`, see [internal/budget and internal/github](#internalbudget-and-internalgithub)),
and once every listed item's details are in hand (after `readMoreDetails`, before the file
checks), `dropLeft` takes out of the assigned search's results the items no longer assigned to
the viewer (`unassigned`, ignoring case) and out of Review requested's the pull requests that
no longer ask the viewer (`answeredReview`), each counted out of its list's total (`countOut`),
as `dropClosed` does. An item without details keeps the search's word, and so does one whose
read can't tell: assignees the read didn't bring in full, or a pull request that asks a team,
lists no requests, or lists `MaxReviewRequests`. The other lists and the pushed flag are left
alone, and an item in no list leaves the inbox as it would once the index caught up, with no
arrival and no stamp, so an assignment or a request made again between the details read and the
search is believed at the next refresh. A single-item read that knows the account (`RefreshItem`,
the checks follower, the review sheet's `ReadPull`, and the issue sheet's `ReadIssue`) does the
same for the assigned list as `leaveAnsweredReview` does for Review requested: `leaveUnassigned`
runs `LeaveUnassigned`, which clears `reason_assigned` and `assigned_at` of an item in the list
and stamps `assign_left_at` with its `updatedAt` as read, so a lagging search keeps it out until
it moves, and tells no outcome. The other way round, a sheet's assignee picker offers the user
themselves, and an edit that assigns them (the issue sheet's `Edit`, the review sheet's
`EditAssignees`: the user among those added, and in GitHub's answer) reports
`Acted.SelfAssigned` with GitHub's time of the edit, which `recordSelfAssigned` records at once
(`JoinAssigned`, one transaction): it sets `reason_assigned`, clears `assign_left_at`, and stamps
`assigned_at` with that time (now without one) unless it holds one. The card says Assigned to you
at once, and the next refresh, finding it listed before, tells no `ArrivalAssigned` of the user's
own assignment. As with Re-request my review, a refresh that lands inside the index's lag leaves
it out once, and the one after it, which lists it, tells it as an arrival. Assigning only others
records nothing, so someone else's assignment of the user still arrives.

**Nudges.** `Nudge` (`edits.go`), on the user's click alone, posts a comment on the
conversation of an open pull request someone else opened (`(*github.Client).AddComment`, one
REST request through the `WriteGate`'s `edit`, and no read: the account check is the one every
edit makes, a request only for a token no refresh has used). It runs under the item's lock,
like `Unassign`, and refuses an issue (`ErrNotPull`), the user's own pull request or one closed
or merged (`ErrNotNudgeable`), and an empty text (`ErrEmptyNudge`). Once GitHub has posted it,
one transaction records when (`NoteNudge`, `items.nudged_at`, migration `0107_nudges`; GitHub's
time for the comment, now without one) and counts the comment on the card at once
(`CountOwnComment`, `item_details.comments + 1`), so a snooze the user takes right after
records a baseline that already holds it, and a comments wake rule never wakes on the user's
own comment. A store that fails after GitHub posted says so in the window ("GitHub posted the
comment, but…"), and the next refresh catches the count up. `Item.NudgedAt` shows `nudged_at`
only while it is later than the user's standing verdict (`viewer_review_at`), so a nudge from a
past round of review reads as none. The result, `Nudged`, holds the snapshot and the
comment's address. The window decides when to offer one (`lib/nudge.ts`) and fills the text
in locally from the prompt library's default **Nudge** reply, so a nudge spends one REST
request and nothing else.

**A refresh the window can watch.** `runRefresh` sets `Service.refreshing`, which every
snapshot read reports (`Snapshot.Refreshing`, set on the copy returned, never on the one kept),
and tells its phases as `event.InboxProgress` through a `progress.Tracker` (the search; the
details of the items new since the last refresh, counted per answered batch through
`github.DetailOptions.Progress`; the file checks, counted per pull request), whoever started
it. It tells no `InboxUpdated` as it starts, so the engine's wake on that stays as it was, and
it ends the tracker and clears the flag before the `changed()` that ends it, on success and
failure, so the update's snapshot says it ended and no progress follows it.

**The searches, after the refresh's store.** `Config.Watches` (`WatchSource`) gives the
searches of the enabled routines of the account signed in, each key once (none while Autopilot
(beta) is off, when no copy holds the watches either: `WatchHolder`), from the snapshot
Autopilot's service keeps (core's `autopilotWatches`, which builds it again first when the
account changed, and can't answer for the account while that leaves another account's: the
refresh's ctx ended, a read failed, or the account changed again). `runRefresh` hands the
watch pass's worker (`WatchPasses`, a desktop worker the shell starts before the scheduler) a
job as the refresh starts, and the pass waits, on the worker's goroutine and outside the
refresh's `errgroup`, until the refresh has stored the inbox and told the window (the job's
release); only then do its requests go out, so they never share GitHub's time with the inbox
query (beside it, the heavier searches cost the inbox's store about 0.3 s, measured in
November 2026), and a search that fails, or a request that does, never fails the refresh: the
refresh stores and returns on the inbox's own requests, and never waits for the pass. One pass
runs at a time: a refresh that finds one running hands over no job (the scheduler never skips
a tick; `periodic.RunEvery` waits after each refresh). No job is handed over before an account
is known, nor while the source can't answer for it, so every search keeps its rows and last
answer rather than being read again as new, nor, once the searches' rows are gone, while no
routine has a search. While the GraphQL budget the inbox query just left is under a quarter of
its size (and won't refill first), the pass sends no request and only records
`PausedLowBudget`, keeping every row. A pass that fails is logged, and one that panics is
recovered; either way its `WatchPass` is kept and `AwaitPass` told (`endPass`, in a `defer`).

**The request.** `github.FetchWatches` packs the searches into requests by how many results
each returned last time (`inbox.watches` keeps each search's last count; a new one counts as a
full page), up to about 120 results and ten searches a request, at most four requests side by
side, a point each. GitHub resolves a request's aliases one after another and answers one that
runs past about 10 seconds with a 502 and an HTML body: measured in November 2026, five searches
of 295 results in one request failed that way every time. A request that fails, as with that
502 or with errors and no data (`EXCESSIVE_PAGINATION`), fails each of its searches; an error
whose path stops at one alias or its `nodes` fails that search alone; one whose path goes on
to a result (`["w0", "nodes", 3, ...]`) leaves that result out and marks the search `Partial`;
one with no path marks every search of its request `Partial`, keeping the data. A query that
can't be written into a request (not UTF-8) fails its search before anything is sent, and the
others go on. Only a refused token fails the call, so `WithToken` borrows a fresh one. The searches read a lean fragment (`WatchFields`): what the
inbox stores of an item (its own fields, its state, when it closed), its head commit, base
branch, and the checks' overall state (finished checks don't move `updatedAt`, so the state
must be in the search), and its repository's node ID, owner, name, and archived flag.
`mergeable`, `mergeStateStatus`, `reviewDecision`, and the repository's other fields cost
GitHub time per result, so they come with the details, read by ID.

**The pass.** Once the inbox is stored and the requests answered, `planPass` gathers the open
results (a closed or merged one, which the index can trail, only records its state, and takes
the item out of the open results another search returned; an archived repository's is left
out), each node once with the searches that returned it and whether any follows the checks,
and compares each with what is stored (`ListWatchBaseline`, a read, no lock). A listed item
has its details from the refresh, and only its rows are stored; one a later read stored (its
`fetched_at` or its details' after the pass's request went out) is left as that read stored
it, and keeps no row when that read left it closed; one stored merged keeps no row, since
nothing comes back from a merge and the search's open is older; any other is stored, and its
details read when `needsDetails` says: no details; another update time, draft flag, or state;
a pull request's other head commit, base branch, or checks' state, checks still running at the
last read, or a merge state GitHub hadn't worked out; or, new to the watches, details missing
or older than `detailsStaleAfter`. Details at least `detailsStaleAfter` (30 minutes) old, and
nothing else, are read as room allows (`fillStale`, oldest first): in the room the moved items
leave in the last request, or one request when none moved, so they never add more than one,
and with some 300 watched items each comes round about every 20 passes. The details are read
with the repository whole (`DetailOptions.Repository`, the `DetailRepo` fragment), with no
lock, `watchDetailsBatch` (15) to a request (`DetailOptions.Batch`), the requests that
succeeded kept when one fails (`DetailOptions.Partial`): GitHub reads an item it hasn't lately
more slowly, and four requests of 25 newly watched pull requests side by side ran past its
10-second limit (measured in November 2026), while of 15 none did.

`storePass` then writes, in one transaction under `order.mu`. It first reads again what is
stored of every result (`recheck`, `ListWatchBaseline` inside the transaction), since an
item's own read, the follower, a whole refresh, or another service's state (`NoteStates`) may
have stored it while the pass read GitHub: an item whose card, details, state, or lists
changed since the pass compared it, or that was stored after its request went out, is newer,
and keeps what that read stored, only its rows written while it is open; one stored merged,
or closed by such a read, or, for a pass, whose rows went since (an unlist takes a closed or
gone item out of every search; a test's results have no rows, which a pass may drop for any
reason), is gone: its rows are deleted, never written, and it isn't told as changed; one
deleted since is stored again as new. Then each item (`UpsertWatchedItem`, which never
touches the reason flags or the times an item arrived in a list, and never writes a state over
`MERGED`; its repository through `EnsureRepo`, which keeps what is stored, or whole from the
details), its details, its rows (`UpsertWatch`), and each search's state. A stored item whose
details the pass meant to read, or whose card moved, but whose details didn't come back,
keeps its card, so its old update time makes the next pass read it again, and isn't told as
changed; a new one is stored without them, told once a pass reads them. A search answered in
full replaces its rows (`DeleteUnseenWatches`), a truncated or `Partial` one keeps the rows it
didn't return, stale, for `staleTailFor` (a week), and one that failed marks every row stale
(`MarkSearchStale`); the rows of a search no routine has are deleted (`DeleteWatchesNotIn`).
The closed results' state is recorded through `SetRefStateBefore`, read before the pass's
request went out, so an item stored since keeps the newer read. Under the same lock, the pass
records in the write order the number its fetch took for every item whose card or state it
wrote (`writeOrder.stated`, through `orderStored`): an item's own read or a follower round
that went out before the pass and lands after it is superseded, as by a whole refresh, and
stores nothing over them. A refresh still fetching that started before the pass's request
applies again the items whose card and details the pass stored, and the states it wrote (a
`stateWrite` each, through `SetItemState`), on top of its own (the replays of `order.go`).
`stated` drops what is no later than the whole refresh before the last, so it stays as small
as what was stored lately. Then `Config.Changed` hears the items stored as changed,
outside every lock, which core turns into the Go-only `event.InboxItemsChanged`; the
follower's rounds, an item's own read, and an action on it tell it too. Each pass's
`WatchPass` (its time in all and after the inbox's store, its requests and points, its results
and details read) is logged at Debug and kept for `Routines.LastWatchPass` and
`Routines.AwaitPass`, which the refresh measurement waits on.

**What keeps watched items out of the inbox.** Every query that reads the three reason flags
is decided in `reasonFlagDecisions` (`flags_test.go`), and `TestEveryReasonFlagQueryIsDecided`
fails on a query it doesn't list:
the lists, the snapshot, and the arrivals read listed items alone, so a watched item is in no
list, no count, and not the menu bar's; `PruneFiles` keeps a watched item's file check;
`ListRepoCounts` counts watched items, so a repository the user only watches can have launch
defaults; `ListRunningChecks` adds the watched pull requests a search follows
(`item_watches.follow`, set for routines whose trigger reads checks), but only while this copy
reads the routines' searches (`watched`, `holdsWatches`: so in none while Autopilot (beta) is
off, when what the searches stored is stale), and `followable` leaves
out a muted repository's only for a listed one no search follows, since a routine watches a
muted repository on purpose, and takes the listed ones before those only a search follows
when a round holds more than `followMax` (50); a round reads them `followBatch` (25) to a
request, side by side, storing what the answered requests read when another fails, since
GitHub works out each one's merge state as it answers; `GetEditItem` says whether an item is
watched, so the Actor acts on a watched item's sheet as on a listed one's, while the review
and issue sheets' reads take any stored item.

**The follow of a merge** (`merges.go`). A merge into a branch changes whether every other
open pull request on it merges cleanly, and GitHub works that out again only once asked,
answering `UNKNOWN` for a few seconds after the branch moved, so the cards would say what was
true before the merge until the next whole refresh. Every merge the inbox learns of is noted
(`noteMerged`, which never waits): the Actor's (`recordActed`, the review sheet's merge and
Autopilot's Merge it, into the base `GetEditItem` reads), a stored single read that finds a pull
request merged (the follower's `storeFollowed`, `RefreshItem`, and the Actor's `storeRead`,
`noteMergedRead`, into the base its details read), and a whole refresh's pull requests the
search still listed though they merged (`noteMergedClosed`), whatever `tellOutcomes` says. The
`mergeQueue` keeps one follow per account and repository (its name compared ignoring case),
with the bases merged into (any base when one wasn't known), and remembers each merged pull
request for `mergeRecent` (an hour) by node ID, so a read that sees it merged again starts no
follow. A follow's first round is due `mergeSettle` (10 s) after its latest merge, so two
merges in a row share it. The `MergeFollower`, a desktop worker in its own select loop
(recovering and logging a panic; its timer tests run in a `testing/synctest` bubble), runs
each round due: `ListOpenPullsInRepo` lists the account's open pull requests in the
repository on a base merged into whose details were read before the first round was due, or
that GitHub left `UNKNOWN`, the listed ones neither dismissed nor snoozed and those a routine's
search follows while this copy holds the watches (as `ListRunningChecks`, and `followable`
leaves out a muted repository's listed ones and takes the first `followMax`); `followRows`
reads them `followBatch` to a request, side by side, under their locks, as the checks follower
reads its own, with `github.PurposeFollower`, then settles and tells the window as a follower
round does. A round whose stored answers left GitHub still working one out, or that failed,
has the follow read again `mergeRetry` (30 s) later, at most `mergeRounds` (3) rounds in all;
a merge noted while a round ran keeps its own round. It costs about a point per 25 pull
requests a round, and nothing while no merge happened.

**The issues a merge closes** (`closes.go`). GitHub closes an issue a merged pull request
closes (its `ClosingPull`) a second or two after the merge, so the issue's card would say a
fix is on its way to an issue already closed until the next whole refresh. Every merge
`noteMerged` notes the first time also owes its issues a follow in the `closeQueue`, one per
account and merged pull request (its `item.Ref.Key`), which wakes the `MergeFollower` on the
merge queue's channel; the follower runs these rounds first, since a card waits on them. A
round is due `closeSettle` (5 s) after the merge: `ListClosingIssues` lists the account's open
listed issues, whatever their triage, since a closed one leaves every tab, whose
`closing_pulls` are not empty, `closedBy` keeps those naming the merged pull request (its
repository compared ignoring case, so an issue in another repository counts too), and
`followRows` reads and stores them as the merge's own follow reads its pull requests
(`followRow.Kind` names an issue, so the outcome told is one). A stored answer that finds an
issue closed takes it out of the lists, as any read does; one that finds it still open, or a
round that failed, has the follow read again `closeRetry` (5 s) later, at most `closeRounds`
(3) rounds, so an issue a merge into a branch other than the default leaves open waits for the
whole refresh. The inbox's own store says which issues name the pull request, so a merge none
names asks GitHub nothing, and one some name costs about a point a round.

**A bot author's picture.** The item fields every inbox read shares (`ItemFields`) ask a bot
author for its picture's URL, at no point more, and `items.author_avatar_url` keeps it, `''` for
any other author (migration `0101_inbox`): the refresh's `UpsertItem` and an item's
own read (`UpdateItem`) write it, a routine's search (`UpsertWatchedItem`, whose lean fields
don't ask) leaves it, and `UpdateItem` keeps the stored URL when a read brought none for the
same author, since a whole refresh applies a pass's read again over its own. The lists and a
routine's Watching list carry it as `Item.AuthorAvatarURL`, which the window hands the avatars
service for a bot's picture on its card.

**The seen stamp** (`seen.go`, migration `0101_inbox`). `items.seen_activity_at` is the
activity on an item the user last saw, and `Item.Unread`, in the lists and a routine's Watching
list alike, is true while it is NULL or `item_updated_at` is later; the window shows it as a dot
on the card while **Unread dots** (`cards.unread`) is on.
Besides the marks, only a
new row's insert writes it: a whole refresh inserts NULL, unread, except on an account's first
load (`noteFirstLoad`: no item stored for it before that refresh, `HasItems`), when it inserts
the item's activity, so a first sign-in isn't a wall of dots; a routine's search
(`UpsertWatchedItem`) inserts the activity, since a watched item isn't news; and no `ON
CONFLICT` update and no `UpdateItem` touches it, so newer activity from any read makes a card
the user saw unread. `StoreFixture` follows the refresh's rule. The bound `MarkSeen(ids)` sets
each of the signed-in account's rows among ids to the later of its activity and now (the
injected clock), so a read that lands after the mark with activity from before it stays seen:
opening a sheet runs `Open` and then a `Refresh` that can store a newer `item_updated_at`, which
a stamp of the activity alone would leave unread while the sheet is open. `MarkUnseen(id)` sets
one row to NULL. Another account's rows are left alone. A mark that changed a row empties the
snapshot cache, so the next `Snapshot` shows it, but emits no `InboxUpdated`, which would wake
Autopilot's engine (`engineOnInbox`) on every sheet opening; the window changes the card
optimistically and reverts it on an error, as it does triage. The window, not the `Actor`,
counts what the user saw and did as seen: it marks an item as its sheet opens (from the inbox
or a routine's Watching list) and again for the item a sheet steps away from or closes on,
which catches what the sheet read while open and the user's own actions in it, as the user
opens it on GitHub, and after a card's own edits on GitHub succeed, whatever the setting, which
gates only the dot and Mark as read and Mark as unread. The `Actor` is shared with Autopilot,
whose steps aren't the user's, and most of its actions record no time of GitHub's, so it can't
say which activity was the user's own. Something the user does on github.com itself reads as
unread, since GitHub's last activity doesn't say who acted. Nothing of it goes to GitHub.

**State, arrivals, and the prune.** `items.state` and `closed_at` are written by every read
that carries them: the inbox's searches (`UpsertItem`), an item's own read and the follower's
(`UpdateItem`, `UnlistItem`, which also drops a closed or gone item's watch rows), the closed
items a refresh's searches returned (`SetRefState`), the routines' searches and a test's
(`SetRefStateBefore` for their closed results), and the states the worktree sweep and the
notifications mirror read (`Store.NoteStates`, through core's `itemStates` and `stateNoter`).
Every one of them stamps `fetched_at`, and none writes over a `MERGED`, which is final: not
`UpsertItem`, `UpdateItem`, `UnlistItem`, `UpsertWatchedItem`, `SetRefState`,
`SetRefStateBefore`, or `SetItemState`. A close can open again, so it is ordered rather than
guarded: another service numbers its read in the inbox's write order as it goes out
(`Store.StartRead`, a `StateRead`; core hands the Service its Store as `Config.Store`, so the
two share one order), and `NoteStates` stores by it as a pass does: an item stored at or after
the read went out keeps that newer read (`SetRefStateBefore`), an item's own read that went
out before it stores nothing over it (`stated`), and a whole refresh still fetching that went
out before it applies the state again on top of its own. `review_requested_at` and `assigned_at`
keep when the inbox first saw an item ask for the user's review and assigned to them while it
stays so, cleared once it leaves (`ClearArrivalStamps`, and every query that takes it out), for
the routines' fire keys. Each whole refresh prunes, in its own transaction, the items in no
list, watched by no search, with no triage, that no read brought for 30 days (`PruneItems`);
what else refers to an item comes from `Config.Keep` (`ItemKeeper`), read before the
transaction, so a later owner of a reference, as a routine's ledger, extends the rule there.
Core hands it `inbox.Keepers` (one failing fails the whole, and then the refresh prunes
nothing, keeping everything, logs why, and goes on) over the agent sessions' items (`sessions.Store.KeptItems`: `agent_sessions.item_id` has no
foreign key, so a pruned item would leave a dangling id, or a reused one) and the
notifications' (`notify.Store.KeptItems`: `notifications.item_id` would be cleared, and the
entry would no longer open its item). A worktree record and a GitHub notification thread name
their item by repository and number, so a pruned item comes back for them with the next read.
The rows that hang off an item by its id go with it, by their foreign keys' cascade: its
details, file check, and check names, and the review sheet's `review_diffs` (with
`review_diff_files`) and the issue sheet's `issue_views`, which are only caches and are read
again on the next visit. (An item with watch rows or local state is never pruned.)

**Held workflow runs** (migration `0101_inbox`). A
details read stores the runs GitHub holds for approval apart (`github.Checks`):
`item_details.checks_awaiting` counts those the user can approve from the review sheet,
`checks_awaiting_on_github` those GitHub approves only on its site, and `checks_held` lists
each with its workflow's name. The snapshot counts them again as each card is built, with
`heldruns.Count` over the held runs, the stored `merge_state`, and **Ignored workflows**
(`checks.ignoredWorkflows`, in the snapshot cache's `settingsKey`, so a change of the list
builds the next snapshot again from what is stored and asks GitHub nothing; the window reads
it again as it saves the settings, `inboxKey`): a run of an ignored workflow never counts, and
one held on GitHub counts only while GitHub reports the merge `BLOCKED`. The snapshot's
`Checks.Awaiting` and `AwaitingOnGitHub` carry what counts to the card, whose **{n} run(s)
await your approval** (rule 6) counts the first alone, while its track marks both held and
says the second as **{n} workflow run(s) waiting for approval on GitHub**; a run that doesn't
count is in neither, so its checks read as they are, its worst detail and rule 15 never see
it, and a fork's approved pull request whose only held run is one a review started reads
**Approved · ready to merge**. A routine's facts count every held run
(`checks.awaitingApproval`), ignored or not, since either way the checks haven't all run. A
row stored before 0023 counts every held run as approvable, and one stored before 0024 has
no names, so its runs match no ignored workflow, until the next whole refresh reads its
details again.

**The reads for Autopilot.** The bound Service adds only `ItemsByID`, the rows of a routine's
Watching list (listed and watched items alike, as a snapshot shows them). Everything else is
off the window: `Store.FactsRows` (one query, `ListFactsRows`, into `facts.Row`, with the
repository rules applied), `Store.FactsEnv`, `Store.WatchedItems` (a routine's lists and its
searches' rows, stale ones included), `Store.Suggest` and `KnownOwners` (the builder's
suggestions, labels and check names counted in Go from their JSON columns), `Store.WatchStatus`,
and, through `inbox.Routines`, `SearchNow` (a draft's searches read now and stored as a pass
stores them, `recheck` and the write order included, with no rows, so the prune takes them in
time; the details of an item no search watches are read only when missing, older than
`detailsStaleAfter`, or its state moved, since the passes keep the watched ones current and a
test only looks, and its card is stored all the same; nothing is sent while the budget is
under a quarter, `ErrLowBudget`) and `ReadChecks` (`github.FetchChecks` into `item_checks`,
returning what it cost, which a test adds to its points, and `ErrLowBudget` too).

**A routine's triage** (`triage_routine.go`, migration `0103_autopilot`). Autopilot's
local steps run the inbox's own triage through `inbox.Routines`, never the bound Service:
`Triage` runs a Pin, a Snooze (for the routine's length, recording the item's activity as
the user's Snooze does, so the user's wake rules end it early), a Dismiss, or a Restore on
one item, marked with the routine (`item_local_state.set_aside_by` for a dismissal, a
snooze, or a routine's Restore, and `pinned_by` for a pin: two columns, since a pin is
independent of a snooze or a dismissal on the same row, each `REFERENCES
autopilot_routines ON DELETE SET NULL`). In one transaction it reads the item's local state
(`LocalState`), works out the change, hands it to the request's `Record` before writing
it, so Autopilot's effect and the change are stored together or not at all, then writes it.
Each step is a compare-and-set: `RoutineTriage.Expect` is the state the routine decided on
(`Routines.LocalStates`, which the engine reads before an item's facts), and a step whose
part of the triage (the pin, or the dismissal and the snooze) differs now changes nothing
(`KeptChanged`), nor does one whose routine's own mark, on either part, was there when it
decided and is gone now (`lostMark`: the user unpinned what the routine pinned before its
next step landed), so the user's own change in between always wins. A routine's Snooze and
Dismiss never replace what the user set aside: they leave an item the user dismissed or
snoozed (still set aside, with no routine's mark) as the user left it (`KeptYours`); its
Pin leaves a pinned item (`KeptPinned`), and its Dismiss a dismissed one (`KeptDismissed`).
Its Restore is for bringing an item back to the user's attention when the routine's
conditions hold, the user's own snooze or dismissal included, and has nothing to do on an
item not set aside (`KeptNotSetAside`); each that changes nothing records nothing. A
routine's Restore keeps its mark on the row with nothing set aside, which `PruneState` then
keeps, so its Undo can tell the item is still as it left it, until the user's own triage of
the item clears it, or a whole refresh drops it once it is older than Autopilot's history
(`PruneRestoreMarks`, `autopilot.historyDays`, read through `autopilotprefs`), unless
Autopilot still offers that Restore's Undo (`Config.Marks`, a `MarkKeeper`: core's
`engineWaker`, answered by `Engine.KeptMarks` from the effects neither undone nor taken
back, read before the refresh's transaction; one that can't be read drops no mark); it tells the
outcome sink of the restore, as the user's does, so the mirror of the GitHub notifications
takes a dismissal's clears back, and no other routine step tells anything. `UndoTriage`
puts back, in one transaction and newest first, each change's state before, only where the
item is still as the change left it (`asLeftBy`: the pin and who pinned it, or the
dismissal, the snooze's end, and who set them, so the user's own triage, a snooze the wake
rules ended, or a later step leaves it as it is), and hands its `record` which it put back;
a routine named in a state put back that is gone for good is dropped from it. Undoing a
routine's Restore of the user's own dismissal tells the outcome sink of that dismissal again,
at its own time, so the mirror records it as the user's and clears by the user's own rules,
as before the Restore; no other Undo tells anything. Two edges of that, accepted: a clear the
mirror marked kept while the routine's Restore stood stays kept after the Undo, so it sends
less, never more; and a dismissal whose routine was since deleted for good reads as the
user's (its mark is gone), so undoing a later Restore of it lets the user's own rules clear
that thread, only ever after the user's own Undo. `AsLeft` asks the same without writing,
for Activity's Undo. The snapshot carries the marks (`Item.SetAsideBy`, `Item.PinnedBy`).
Every triage path of the user's own clears the mark it changes (`SetTriage`, `Pin`, `Unpin`,
and `Restore` all write it), so "Snoozed by a routine" never outlives the user's own
re-snooze, takes its time once its transaction holds the write lock, so a reversal's time
comes after every routine's step that committed before it, and tells the outcome sink before
the window and the engine hear the inbox changed; one that takes back what a routine did
tells the outcome sink with
`Outcome.Reversed` (the routine, the item, and what it did): a Restore of what a routine
dismissed or snoozed, still in effect; a Snooze over that (`OutcomeSnoozed`, told only then);
and an Unpin of what a routine pinned (`OutcomeUnpinned`, only then). Setting aside again
what a routine's Restore brought back is the user handling the item, never a reversal, and
going further than a routine (a dismissal over its snooze, the user's pin over its pin)
takes nothing back either, nor does snoozing again what a routine snoozed once that snooze
has ended.

**Check names.** `github.FetchChecks` reads each pull request's checks on its head commit, a
fragment of its own (`CheckFields`) modeled on the review sheet's `checkList` selection, with
each run's `startedAt` and `completedAt` and each status's `createdAt`, under one alias per
pull request (`node(id:)`) in batches of 25, four side by side, a point each: whether a check
is required takes the pull request's own ID (`isRequired(pullRequestId:)`), which a fragment
can't take, so each alias asks for it beside the fragment, over the same contexts in the same
order. A name repeats, once per event that ran it; `item_checks` stores every run, and
`facts.Build` dedupes them. `HeadChecks.Settled` is the newest completion once every check has
finished, stored as `settled_at`, by the facts' own rule (`HeadCheck.Running`): a check runs
while it is pending, or has no completion time and wasn't skipped. A head with more than a
page (100) of checks is read in part (`Complete` false): its names are partial, so a negation
over them is unknown, and it settles once every check read has finished and the rollup over
all of them (`HeadChecks.Rollup`, and the details' checks state for the facts) says the rest
have too, at the newest time read.

**The budget's purposes.** Each request's context names what it was sent for
(`github.WithPurpose`): the refresh is `PurposeInbox`, the watch pass `PurposeWatches`, the
checks follower and the follow of a merge `PurposeFollower`, and `SearchNow` and `ReadChecks`
`PurposeAutopilot`;
anything else is `PurposeOther`. The client hands the purpose to the `Tracker` with each
reading (`RateReading.Purpose`, which the tracker strips before it keeps the reading), and the
tracker splits each budget's spend by it (`Reading.Purposes`, kept across runs, a state saved
before the split, and a purpose this build doesn't know, counting as the rest), so the shares
add up to `Spent`. The budget menu's one line for Autopilot is `watches` and `autopilot`
together, shown only while they are above zero; the follower's rounds, which read the watched
pull requests a routine follows in the listed ones' requests, stay `follower`.

**What a repository's launch reads.** `Store.RepoFacts` (the query `GetRepoFacts`, beside
`GetRepoDefaultBranch`) reads what the last refresh stored about a repository, matched ignoring
case, from the database alone: its default branch, the viewer's permission, whether GitHub has
it archived, disabled, or locked, and why, and whether it is a fork, and of what (`RepoFacts`,
with `Found` false for one the inbox never saw). Core converts it field for field into the
launcher's `StoredRepo` (`repoFacts` in `adapters_launch.go`), which a repository's launch
checks ([internal/launcher](#internallauncher)), so the launcher never imports the inbox.

## internal/reporules

`internal/reporules/` holds the pinned and muted repositories and orgs (`repos.pinned` and
`repos.muted`, each a list of `owner` or `owner/name`, a target in one list at most), which
settings alone writes (`SetRepoRule`, emitting the Go-only `event.RepoRulesChanged`, which
core turns into an `event.InboxUpdated` and the Release `Refresher`'s `RulesChanged`) and
the inbox and Release read; `Rules.For` gives a repository's own rule before its org's. In
the inbox, each snapshot item carries its `RepoRule` (the rules are in the snapshot cache's
`settingsKey`, so a change rebuilds it), a pinned repository's items come after the items
pinned themselves, and a muted one's are in each list's Muted tab, never `NeedsYou` (so
out of the menu bar's count), told in no arrival, followed by no checks follower, and
given no file check by a refresh, though their details still come with it. It is a leaf:
it imports only `internal/repo` and `prefval`.

## internal/heldruns

`internal/heldruns/` decides which of a pull request's workflow runs that GitHub holds for
approval count toward where it stands, and declares **Ignored workflows**
(`checks.ignoredWorkflows`, a list of workflow names, matched whole and case-sensitively),
which settings alone writes (`SetIgnoredWorkflows`, in `Portable`) and the inbox and the
review sheet read. `Ignored` is the one rule: a run of a named workflow never counts,
whichever kind it is and whatever GitHub reports, and a run GitHub approves only on its
site (`github.HeldRun.OnGitHub`) doesn't count either, unless GitHub reports the merge
`BLOCKED`, when it may be what blocks it and counts again (the safety net, from the
`mergeStateStatus` the details and the sheet's read already carry, no request more). Every
other held run counts. `Count` counts a card's, and `Unnamed` stands in runs of no name for
details stored before the names were, which match no ignored workflow. It is a leaf: it
imports only `prefval`.

## internal/threadrules

`internal/threadrules/` holds the rules for the user's GitHub notifications (`threadrules.*`:
whether they are read, whether they are cleared on their own, marked done or read, by which
triggers, and whether a watched repository's or a bot's activity counts), which settings
alone writes (each rule's setter emitting the Go-only `event.ThreadRulesChanged` once it
is stored, which core's relay turns into the ghthreads `Syncer`'s `RulesChanged`), and the
pure `Decide`, which says whether one thread is cleared, skipped, or held at the moment its
item was finished; it is a leaf that imports only `internal/item` and `prefval`.

## internal/ghthreads

`internal/ghthreads/` mirrors the user's GitHub notifications (threads, in the REST API's
words) as the web inbox lists them, read and unread but not done, in `gh_threads`, which it
owns, one row per account and thread (its reason, unread, times, subject type and title,
repository, the pull request or issue `Thread.Ref` names, '' and 0 for any other subject,
and `latest_comment_url`, kept for the next check of who acted after a moment and never
shown or opened), and keeps the state of its passes in `ghthreads.sync`, a JSON pref only it
writes. Its writes to GitHub mark a thread done or read: clearing one (below), which sends
none until the user turns clearing on or clears one by hand, and the panel's own clicks
(`MarkDone`, `MarkRead`, below). A pass, one at a time (the Syncer's
and `CheckNow`'s share one), reads the rules through its `RuleSource` (core's
`ghThreadRules`, one read of the prefs): with `Read` off it sends nothing and borrows no
token, and marks the mirror stale (`walkNext`). Otherwise it borrows a token through
`github.WithToken`, learns its login with `FetchViewer` (remembered per token in a
`github.ViewerCache`, the inbox's building block too: one GraphQL point per new token), and
lists through its `Client` (`*github.Client`): page 1 with the ETag its last answer
carried, a 304 ending the listing at no cost, and a 200 reading on (`ThreadPage.NextPage`)
until a page whose oldest thread is no newer than the high-water mark, the newest
`updated_at` the last stored listing saw. A 304 covers page 1 alone, so every page is
walked, with no ETag, up to `github.MaxThreadPages` (the newest 1,000, the mirror
`Truncated` when GitHub lists a page past them, `ThreadPage.Capped`): at the first pass
after the Service is built, every `FullWalkEvery` (6 h), after the account changes (another
account's ETag and mark are never used, and its rows, never shown, are dropped once unseen
for 30 days), once reading is back on, and after a refusal; a pass that reads to the last
page counts as one. A 200 drops the account's rows it didn't list within the span it read
(done on GitHub): every one for a walk, and, for a pass that stopped at the mark, those
updated strictly after the oldest thread on its last page, since GitHub lists the newest
updated first; a thread updated while the pages are read may be dropped once, and listed
again by the next pass. A pass stores its listing in one `store.InTx` at its end, rows and
`ghthreads.sync` together, so one that fails partway stores nothing, and never an ETag or a
mark the pages it meant to read don't back. GitHub refusing the listing to the token
(`github.NotificationsRefused`: 404, or 403 but a rate limit) marks access refused ("Your
GitHub token can't read notifications."), and no listing is sent for `RefusedFor` (6 h)
unless `CheckNow` asks; any other failure is only the status's last error
(`uierr.Message`), and the next pass tries again. Then, a 304 pass included, it checks the
state of the pull requests and issues its threads are about, each once, in one
`FetchItemStates` call: those never checked, those whose thread updated since, and those
not known merged or closed and checked `StateRecheck` (30 min) ago; releases, discussions,
check suites, and commits are counted, never checked. One GitHub can't find is stamped
checked, its state kept, and a check that fails stores only the last error, the listing
standing. The `Syncer`, a desktop worker after the scheduler and woken with it
(`followed.intervals`), runs a pass at once, then the longer of its own refresh interval
(Settings → Refreshing, `refresh.notifications`, a minute to an hour) and GitHub's
`X-Poll-Interval` after the last pass ended, its own or `CheckNow`'s (a timed
pass that finds one ended while it waited for the running one lists nothing), in its own
select loop, recovering and logging a panic; its tests run in a `testing/synctest` bubble.
The bound `Snapshot` reads the database alone, for the account the last pass read: the
status (reading, the account, `Access`, the last pass and walk, the last error, when a
refused listing is sent again, the count, the unread count, and `Truncated`), and the
newest 20 `Unread`, none with a URL GitHub gave; with reading off, its lists are empty.
The bound `List` reads every thread the mirror holds for that account, read and unread,
each with its item's state as the last state check read it (`Thread.State`), for the
Notifications page, with the same status: `snapshot` and `List` share one row walk
(`listed`), so both count alike. The `Snapshot` also holds an unexported fingerprint of
every listed thread (its id, unread, state, title, reason, and update time), which JSON and
the bindings skip but `tell`'s comparison sees, so a pass that changes a thread past the
newest unread still tells the window.
`CheckNow` runs a pass once any running has ended, lifting the back-off and ignoring the
poll interval, and returns the `Snapshot`, whose status tells a pass that failed. A pass
that changes what `Snapshot` shows, the passes' times aside, emits `event.GHThreadsUpdated`
(`ghthreads:updated`), but one whose caller gave up tells nothing.
It clears the threads by the user's rules (`clear.go`), and owns `gh_thread_moments` and
`gh_thread_clears` for it. The inbox's `Config.Outcomes` is its `Settler` (core's
`ghThreadOutcomes` adapts `inbox.Outcome` to its own `Outcome`, never bound), whose
`Settle` records, in one transaction and without waiting, the moments the user finished an
item in the app (a merge, a close, a review that approves or requests changes, a
dismissal, taking themselves off), a review of the user's made anywhere else that the inbox
saw (an agent's gh, a browser: `OutcomeReviewed` from a read of the pull request alone or a
whole refresh, once per review and within two hours of it, at GitHub's time for it, so a
thread that moved after it isn't cleared), a merge's or a close's state as GitHub answered it
(`MERGED` or `CLOSED`, read now), and a restore's taking back of the dismissal's moment and
its queued clears (kept), then nudges the `Syncer`, which runs a pass on those items at
once, listing first only when the mirror is older than the poll interval (a minute at
least, `MinNudgeListing`); an item the inbox saw close or leave its lists has its state
checked. An item merged or closed, or that left the lists (`OutcomeKind.finishes`), is also
followed up (`nudges.follow`): GitHub tells of the merge or close a little after it, and of
what follows it (a bot's comment), and each brings a thread already marked done back, so the
`Syncer` runs a nudged run deciding the threads about the items followed up every
`FollowUpEvery` (2 minutes) until `FollowUpFor` (10 minutes) after the inbox told of each
(`takeFollowUp`, `untilFollowUp`, a timer of its own in the `Syncer`'s sleep). Each lists,
since the mirror is older than the poll interval by then, conditionally, so a page that
didn't change costs nothing; a thread that came back has its state checked again and clears
at once. A pass reads the account's threads, moments, and clears once (`decideInputs`),
and again only what a step changed: the threads once a state check stored an answer, and
the clears inside the transaction that queues, since a click may have queued or kept one
meanwhile; a send reads its one item's moments. After its listing and state checks, a
pass decides each pull request and issue thread with `threadrules.Decide` against every
moment that could apply: the user's own, and GitHub's own merge or close
(`outsideMoment`, only on a state read within `StateFreshFor`, an hour; one older that
would clear is read again first, but not while a backlog counted under the same rules is
held back over its cap, `BacklogRules` in `ghthreads.sync`: such threads count toward it
as they are, so a held backlog costs no state reads). A thread held back, unread and
updated after the moment, has who acted after it checked once per update of the thread,
and only while a bot's activity counts: the latest comment's author (`FetchLatestComment`,
one REST point, `MaxCommentChecks`, 150, a pass at most, the rest the next pass, the checks
stopping at the first answer that says GitHub is limiting the app's requests,
`github.RateLimited`), stored as `after_moment`, a person's sticking until the thread is
read (a person's comment followed by a bot's before the next pass reads as a bot's). A
latest-comment URL that is the subject's own is an event, not a comment, and needs no
request: on an item merged or closed it is the merge or the close itself, a bot's; on an
open one (a push, a request for review, an approval with no text) it counts as a
person's. A thread that clears is queued in the ledger before anything is sent, with its
trigger and action (`threadrules.action`: done, a `DELETE`, or read, a `PATCH`): a clear
the user's own action chose waits the Undo hold (`Hold`: the toast setting plus 3 s, 15 s
at least, through core's `ghThreadHolds`), one GitHub's state chose goes at once, and the
latest hold among the moments that clear wins. One the user kept (`Keep`), and one whose
clear failed, is not queued again on its own until its thread moves (`marks`); the user
may still clear it by hand. The first pass after clearing turns on or the rules change
(`RulesSeen`, a fingerprint in `ghthreads.sync`) queues at most `FirstCap` (25), any later
one `Cap` (150); one over its cap queues none and records the count as the `Backlog`. The
sends go one at a time through the client's chore lane, from a pass or from a run the
`Syncer`'s timer starts at the next `send_after` or when the lane lets the next go, which
lists nothing and lets go of the pass slot at the lane's first refusal, and each is checked
again first (`sendOne`): clearing still on (but for one by hand), reading on, GitHub not
refusing the listing, the token's login the row's (else the queue waits for the next
pass), the thread still listed at the `updated_at` it was decided on (else decided again;
one not listed is skipped, never sent), queued less than a day ago (else decided again),
and `Decide` still clearing it. A 404 counts as done; a refusal of the lane costs no
attempt; any other failure is tried again a minute on, three times at most, then failed. A
thread marked done leaves the mirror, and one marked read stays, read. Clearing turned off
(settings' Go-only `event.ThreadRulesChanged`, which core's relay turns into the `Syncer`'s
`RulesChanged`) skips every queued clear but the user's own at once, and reading turned off
skips them all and sends nothing. `ClearItemThreads` clears an item's threads by hand (no
hold, with clearing on or off, never a thread that isn't a pull request's or an issue's) and
says how many it queued, which Autopilot's Mark the GitHub notification done runs; `Keep`
takes queued clears back; and
`MarkDone` and `MarkRead` (`mark.go`) are the panel's Done and its opening of a line on
GitHub: any thread the mirror lists for the account, whatever its subject and whatever the
rules' action, sent at once through the chore lane (waiting out its pace up to
`MaxPaceWait`, else worded), holding the pass slot so no pass stores a listing read before
GitHub took the mark, after the token's login is checked against the account, each landing
in the mirror as GitHub takes it (done or gone: dropped; read: kept, read), recorded in no
ledger, and told to the window. The
`Snapshot` also lists the `Queued` clears, the `Backlog`, and the `Recent` 50 that settled.
Moments and settled clears are dropped 30 days on. It never imports the inbox, settings,
core, notify, the sheets, or the desktop shell, and the inbox never imports it (the
repoguard boundary test).

## internal/statusprefs

`internal/statusprefs/` is the service status check's catalog and the user's choices of
what it watches. Each `Provider` (GitHub, Claude, OpenAI) has its page, where its summary is
read (`Host`, `Path`, and `Format`: Statuspage's `summary.json`, or incident.io's documented
widget API, `/api/v1/summary`, since OpenAI's Statuspage-shaped summary leaves Codex out),
and the `Part`s a user may watch, each named by the app's own stable `PartID`
(`github.pulls`, `claude.api`, `openai.codex-cli`, ...) and matched to the page by its
component ids (`PageIDs`), so a provider that issues new ids needs only a catalog edit and
the stored choices survive it. The defaults are what the app depends on: GitHub's Git
Operations, API Requests, Issues, Pull Requests, and Actions; Claude's API and Claude Code;
and OpenAI's Codex API, Codex CLI, and Responses. The choices are stored as changes from the
defaults, as notifyprefs stores the banners (`status.providersOff`, `status.partsOff` for
the default parts unticked, `status.partsOn` for the others ticked), so a part a later build
adds with `Default` is watched without the user doing anything; settings alone writes them
(`SetStatusProvider`, `SetStatusPart`, each in one `prefs.Update`, emitting the Go-only
`event.StatusRulesChanged`). Adding a provider is one catalog entry and its host in
`github.DefaultStatusHosts`, and a parser only for a new format. It is a leaf: it imports
only `prefval`.

## internal/svcstatus

`internal/svcstatus/` follows those status pages, so the window can say when a part the
user watches has an incident. A check is one GET of a provider's summary through
`(*github.Client).FetchStatus`, outside any API, so it sends no token and costs no budget,
and is conditional on the page's last ETag (GitHub's and Claude's give one; a 304 keeps the
reading). The pure parsers (`statuspage.go`, `incidentio.go`) turn each format into one
reading of components and incidents on one severity scale (`State`: operational <
maintenance < minor < major < critical; `full_outage` and `major_outage` are critical, an
impact of none is minor, anything unknown minor), and the pure `judge` (`relevant.go`)
decides by component, never by a page's overall state, which also turns for a Copilot or
Codespaces incident: a component matches a picked part by its page id, or else by name and,
where the part names one, group (OpenAI lists two components named Login); the parts watched
that aren't operational are `Affected`; an incident counts when it names a part watched, or
names none and is major or critical (`Unscoped`), and every other is counted in `Others`; a
Statuspage page's parts watched that it no longer lists are `Unlisted`, logged once. Each
provider's last reading is kept raw, in memory alone, and judged against the choices as they
are on every `Status`, so ticking a part costs no request. `Config.Rules` reads the interval
and the choices in one read of the settings (core's `statusRules`, the defaults, logged, when
the settings can't be read). The bound `Status` serves every provider in catalog order, off
while not watched (switched off, no part ticked) or the check off, and `CheckNow` (the
popover's Check again) reads, in parallel, each watched provider whose last check ended
`FreshFor` (10 s) ago or more, one `flight` key per provider, so a timed pass and a click
share a request; a check that fails keeps the reading before it, with the reason in its
`Error`, and is logged, and each pass emits one `event.ServiceStatusUpdated`
(`servicestatus:updated`). The `Checker`, a deferred desktop worker in its own select loop
(recovering and logging a panic; its tests run in a `testing/synctest` bubble), checks every
watched provider after the first paint, then each once its last check ended as long ago as
Settings → Service status says (`refresh.serviceStatus`, 30 minutes by default, 5 minutes to
an hour, 0 off; woken on `event.IntervalsChanged`), taking along any watched provider
checked half an interval ago or more, so the providers share one timer; on
`event.StatusRulesChanged` (core's relay calls `RulesChanged`) it drops what it keeps of a
provider no longer watched, its ETag included, sets the alerts' baselines quietly, tells the
window, and checks a provider turned on at once. Alerts (`alerts.go`, in memory): the first
reading after a start, or after a change of the choices, only sets the baseline, so a
restart during an incident repeats no banner; a state that rises into minor, major, or
critical above the level already known tells its `Notifier` ("Claude: Partial outage", the
parts affected and the first incident, opening the incident), one that falls back to
operational or maintenance after it tells "Claude: Operational again", and nothing else
does; core's `statusAlerts` records each as a `service_status` notification about no item,
with its banner (on by default). It never imports notify, settings, core, the inbox, or the
desktop shell (the repoguard boundary test). The window shows nothing while every watched
provider is operational, unchecked, or the check is off; otherwise one amber (minor,
maintenance) or red (major, critical) warning sits before the budget gauge, opening a card
per provider in trouble (its parts affected, its incidents with their stages, how many
others don't touch what is watched, and its last error), and the account menu's Service
status submenu names each watched provider's state.

## internal/milestones

`internal/milestones/` keeps the local log of the inbox reaching nil and says what each time
is worth, so the party at nil can save its confetti for what means something. The log is one
preference, `milestones.log` (`KeyLog`), JSON through `store.EncodeJSON`/`DecodeJSON`, which
this package alone writes: `count` (the nils the user earned), `streak`, `best`, `lastDay`,
`confettiDay`, and `unlocked`, each achievement as `"kind:n"`. Days are local calendar days
(`YYYY-MM-DD`) from the injected `clock.Clock` in a `*time.Location` (`WithLocation`, default
`time.Local`); yesterday is computed at noon, so a day a clock change makes 23 or 25 hours
long still follows the one before. The one bound method, `Reached(Reach{Earned, Cleared,
Loud})`, reads and writes the log in one `prefs.Store.Update` (one IMMEDIATE transaction), so
two windows count both times and throw confetti once at most. Every call extends the streak:
the same day leaves it, the day after adds one, a gap starts it again, and a day before the
last (a clock or zone moved back) leaves it as it is; `best` follows. Only an earned call adds
to `count` and unlocks achievements (`Kind`, all its constants in `kind.go` so the bindings
write one union): every streak tier the streak has reached (3, 7, 14, 30, then every 30) and
count tier the count has (10, 25, 50, 100, then every 100) not unlocked before, so a tier
reached on a day the inbox was only found empty unlocks with the next earned nil; the first
nil; a sweep of `SweepAt` (10) or more cleared; and the badges for the hour and the day
(early 05:00–12:00, Friday, the weekend, owl 22:00–05:00). `Moment.Unlocked` is never nil and
lists them the most significant first (streak, count, first, sweep, badges, each kind's
highest tier first). `Confetti` is `Loud && len(Unlocked) > 0 && confettiDay != today`, and
stamps `confettiDay` when true, so the once-a-day cap holds across restarts and windows.
A stored log that can't be read starts afresh (logged), and a value no build writes (a
negative number, a day that isn't one) reads as never having happened. The log is history,
like the triage: a backup never holds it, and a reset clears it with the database. Core
builds the service beside the settings and binds it.

## internal/review

`internal/review/` is the review sheet's service. `Decide` is pure: from a pull request's
facts it works out every action's offer and the one primary, and `Act` runs it again before
any write. An offer with warnings asks first (`offers` sets `Offer.Confirm` for it), since
`Act` refuses it unless the request accepts them; the window confirms a merge with its
methods and any other action with none. Asking Dependabot to rebase is offered on every open
Dependabot pull request, enabled once the sheet read it, for a user with push access, while
the branch is behind or has conflicts; Update branch whenever GitHub's `viewerCanUpdateBranch`
lets the user (with a warning on Dependabot's), and, disabled with why, on any other pull
request GitHub reports as behind or the compare counts behind (`Facts.BehindBy`). Each is the
primary only while the pull request is behind (the rebase also with conflicts), never as a
fallback: with no primary and offers enabled, the sheet's button only opens the menu. How far
behind comes with the one read: `baseCompare: baseRef @include(if: $compare) { compare(headRef:
$compareHead) { behindBy } }`, with `$compareHead` the pull request's `refs/pull/<n>/head`
(`github.PullHeadRef`), which GitHub resolves for a fork's pull request too, where the branch's
name doesn't; the read still costs one point (measured in November 2026 on a same-repository
pull request and a fork's), and a compare GitHub can't resolve comes back null beside a
not-found error that leaves the rest of the answer, so `PullReview.BehindBy` is 0. The cache
keeps it (`pullContext.BehindBy`, 0 in an older build's), and `Review.Behind` (open, and
`viewerCanUpdateBranch`, `BEHIND`, or a count) with `Review.BehindBy` feed the sheet's strip
(`BehindBase`), which offers the rebase on Dependabot's, else Update branch, or why not, in
amber only while the readiness is `behind`. Update branch merges the base in (REST `PUT
.../update-branch`, pinned with `expected_head_sha`); it never rebases. Once the author pushed
since the user requested changes (`Facts.ChangesRequestedSince`: `YourReview` is
CHANGES_REQUESTED with a `Since`), `rerequest_review` ("Re-request my review") is offered on
someone else's pull request, disabled while the card's `review_users` name the user already
(`Facts.AskedAgain`) or without write access, never the button and run only from the menu or
the strip of what came since (`menuOnly`). `Act` asks once (`(*github.Client).RequestReview`,
REST `POST .../requested_reviewers` naming the account the inbox checked the token against,
`Target.Login`, through `edit`; GitHub's answer must name the user, else `ErrEditIgnored`),
never again across the token retry, and hands the inbox who GitHub asks now
(`Acted.Rerequested`, `ReviewRequests`, `RerequestedAt`), which puts the pull request back in
Review requested at once (`review_rerequested`). It dismisses nothing, sends no text, and
Autopilot's GitHub step can't reach it (`reviewActionOf` allows only the rebase, Update branch,
and a comment). Every write goes through the inbox's
`Actor` (the item lock, the account check, and the token retry, never bound, since its
methods take funcs), is pinned to the head the user saw (an approval reads the head first,
since GitHub takes a review of an older commit), and a merge first reads `mergeStateStatus`
fresh and merges only when GitHub says it can, so an admin's token never merges past branch
protection on its own: only `merge_bypass`
("merge past the rules") does, offered when GitHub sets `viewerCanMergeAsAdmin`, with each
rule it skips to accept, and never the sheet's button unless the user turns on
`review.bypassOwn` (`Prefs.BypassOwn`, which `Decide` takes with the merge method): then
`primary` puts it first on the user's own pull request that waits only on an approval
(`waitsOnReview`: BLOCKED, an approval required, no required check failing or pending, no
changes requested), and it still asks first. The one other `merge_bypass` is a routine's
(`ActRequest.Routine`, `json:"-"`, so the window can't send it; `pastrules.go`): `Act`
refuses it unless the read finds the user's own pull request in that same state, offered past
the rules (`OwnAwaitsApproval`, which the sheet also returns), warning of nothing but the
approval (`OwnApprovalSkipped`) and, with `RoutineMerge.Sensitive`, the sensitive files
(`SensitiveWarning`, `StrayWarning`); never takes its `Accept` for accepted failing checks;
and its guard reads `FetchOwnMergeState` instead and merges past the rules only when
`pastRules` finds every check holding: the viewer opened it; every commit, at most 100, has a
signature GitHub verified whose signer is the viewer; each ruleset rule is one it can account
for (`allowedRules`: the pull request rule, required checks and signatures, no force push, no
deletion, creation, the push-time file rules, Copilot's review, and a linear history for squash
or rebase) and classic protection neither requires deployments nor locks the branch; BLOCKED,
`viewerCanMergeAsAdmin`, and `REVIEW_REQUIRED`; the rollup SUCCESS with each required check
(the rules' and classic protection's) present by name and passed; and no review thread
unresolved. A list longer than its page, and a null, fail closed. GitHub still working it out
(UNKNOWN, a rollup pending or missing) is `ErrPastRulesNotYet`; anything else
`ErrPastRulesRefused`, which wraps `ErrNotOffered`. A state GitHub would merge anyway merges as
any other. An UNSTABLE pull request's readiness
line and merge warning say what GitHub counts as not passing: optional checks failing or
running, and the head commit's workflow runs that ended without a check (`Unrun`, read in
the review query from the head commit's own check suites, `headSuites: commits(last: 1)`,
which the details fragment asks for under the same alias and arguments, so GraphQL merges
the two and a fork whose rollup is null still shows them: a fork's runs awaiting approval,
each with its run's ID and page, or runs that failed to start), which the sheet names
beneath its checks, each a link to its run. Runs awaiting approval are approved with
`approve_runs` ("Approve workflows to run"), offered in a bar under the sheet's header and
in the menu, never the button: held back until the sheet read every one's run ID (a cache
an older build kept has none), for a user with write access, and asking first, with a
warning, when the pull request changes workflow files. A run GitHub approves only on its site
(`Unrun.OnGitHub`, from `github.UnrunSuite.OnGitHub`; see internal/github) is never offered
(`Facts.OnGitHub`): the bar lists it apart as waiting for approval on GitHub, with **Open on
GitHub** (the run's page, or the pull request's checks for several) and no button, and an
approval names only the others. A held run that doesn't count (`Unrun.Ignored`, marked as the sheet is built,
by `Open`, `Refresh`, `Rebuild`, and `Act` alike, from `heldruns.Ignored` with the settings'
ignored workflows and the merge state the sheet read) is left out of the readiness (an
UNSTABLE pull request whose only unpassing statuses are such runs reads **Ready to merge**),
never offered to approve nor named by an approval, and not in the bar: the checks list names
it quietly, a line a run (**{workflow} · held on GitHub** or **· held for approval**), with
Open on GitHub, and Ignore this workflow while it isn't named. A merge's warning still says
every held run, since GitHub still counts it. `Rebuild` builds the sheet again from the
cache, with no request and no new visit, which the window asks for when the list changes
while the sheet is open. `build` takes the inbox's stored state (`StoredPull.State`, the
item's `state`, which a merge or close the inbox recorded sets, as a routine's merge does,
dropping the cache): MERGED or CLOSED ends the sheet as a read that finds it would, so an
open sheet whose card leaves the list moves on, with no request. Should GitHub still refuse one for what it is
(`github.ErrRunNotApprovable`), `Act` remembers its run ID for as long as the app runs
(`Service.onGitHub`, in memory, never stored; `markOnGitHub` applies it to every cache the
sheet loads, over any read since), never sends it again, and goes on with the rest; with none
approved, the result is `runs_on_github` with a short note ("Auto-merge Trigger can only be
approved on GitHub"), not an error, and the sheet reads again to list it there. `Act`
approves the runs the cache
read at the head the user saw (`loadAct` refuses another head as `pushed_since`), one REST
`POST .../actions/runs/{id}/approve` each (`(*github.Client).ApproveWorkflowRun`, through
`edit`), never one twice across the token retry; once GitHub approved any, a refusal of the
rest is the result's `Reason` (`runs_approved`, "Approved 2 of 4 ..."), not an error. The
sheet stays open after it, with the draft, and reads again (`ReadAgain`) at once, then
three seconds apart up to four more times, until fewer runs are held and a check is
running; each read rewrites the card, which the checks follower then keeps current. Each step is reported as an
`event.ReviewProgress`, which the sheet shows while the user waits. Line comments are checked
against the cached diff (`hunks.go`) before anything is sent; a routine's Post the agent's
review first keeps only those the diff shows (`Fitter.Fit`, never bound, the same
`checkComment` over the cache alone, counting the rest), so its review never fails whole on a
line GitHub would refuse. `Decide` marks every offer with
what its text goes with (`Offer.Reviews`, `Offer.CommentsFirst`, and `Offer.CommentBlocked`,
why GitHub can't take the comment now), and `Act` and the window both go by those marks. A
merge with no review of its own (`merge_now`, `merge_bypass`, `auto_merge`) posts the
composer's text and line comments, when there are any (by `textutil.HasText`, which trims as
the window's `trim()` does), as a COMMENT review at the head the user saw, the way `comment` does
(never an approval, so it works on the user's own pull request), but only once the fresh merge
state says GitHub would merge (auto-merge: that it is open, not a draft, at that head), so a
refused merge strands no comment; then it merges exactly as it would without them: no
auto-merge fallback, and `merge_bypass` still needs its rules accepted. A comment that fails
merges nothing, and once one is posted nothing after it is an error: a merge GitHub won't make,
or any failure, is `commented_not_merged`, a result whose words, like `Result.Commented`, say
the comment was posted, so the sheet drops the text and never posts it twice. A close
(`close`) is offered on an open pull request, held back ("Wait for it to load") until the
sheet read it, then only when GitHub's own `viewerCanClose` says so (`Facts.CanClose`, read
by the review query and kept in the cache), and always confirmed. It is never the button:
the window runs it only when picked from the menu (`menuOnly`), never keeps it as the chosen
action or runs it with ⌘↩, counts it as nothing to do, and sends with it only text the user
wrote or picked, never a default reply. GitHub takes no head to pin a close to, so every try
(a retry with a fresh token included) reads the head first (`FetchMergeState`): one no
longer open is refused, and one pushed to is `pushed_since` with nothing sent, or, after the
comment, `commented_not_closed`. Then it posts the text and line comments as a COMMENT
review the same way, and closes with one REST `PATCH` (`(*github.Client).ClosePull`, whose
refusals `closeError` words as a close, never as a merge or a push). A comment that fails
closes nothing, and anything that fails after it is `commented_not_closed`, decided in one
place (`failedAfterComment`). A merge or a close takes the pull request out of every list
(`Acted.Merged`, `Acted.Closed`) and drops its cache. So does a read that finds one merged or
closed, as a card still listed can be until the next refresh: the inbox's `ReadPull` stores
its state from the answer, which takes it out of the lists and tells the window, and `Refresh`
(and `ReadAgain`) returns it built from that read alone (`endedRead`: its context, with no
files, since a pull request no longer open is never listed), its `State` saying `MERGED` or
`CLOSED`, as the issue sheet returns a closed issue. The window learns it from the read it
makes on every opening, with no request more (built from the card alone, with no cache, it
would read as open, and the window would read it twice more for a merge state GitHub never
works out). A read GitHub can't resolve still returns the card. It owns `review_diffs` and
`review_diff_files`, a bounded cache of the diffs it read (per pull request, in all, and by
age), pruned in the same transaction that writes. The one read of the pull request
(`FetchPullReview`, one point, measured) brings its conversation too: the newest hundred
entries of its timeline (comments, reviews with their first twenty line comments, each with
the last lines of its diff hunk, and events; its commits left out), counted by GitHub's
`filteredCount`, since a pull request's `totalCount` counts every type, and twenty when the
answer is too large, with the description's edited time and reactions. Reviews still
pending, and ones that only comment and hold nothing, are dropped; a review of another
commit than the head is marked `Outdated`. The context the cache keeps holds the
conversation (`timeline.Entries`), cut to fit 1 MiB (`fitContext`, `timeline.Cut` at 16
KiB, then 4 KiB, then nothing, each cut marked) on every write, and a read under 20 seconds
old is served again only while the card shows no activity since it (`recent`), so a new
comment always shows. `Open` starts a visit as the issue sheet's does: `review_diffs.seen_at`
holds GitHub's `updatedAt` of the read it shows, which only `Open` moves
(`Review.LastSeenAt`). `Earlier` reads the conversation's page before a cursor
(`FetchTimeline`) through the inbox's `PeekPull`, and stores it nowhere. The commits stay out
of the conversation: the same read lists the newest hundred (`commitList: commits(last:
100)`, each with its headline, its committed time, and its one author, never the authors'
list, which costs points) and every reviewer's latest verdict (`latestOpinionatedReviews`,
each with `viewerDidAuthor`), from which `github.Verdict` keeps the user's own: the latest
that approved or requested changes, which a later review that only comments, as a reply to a
line comment, doesn't hide as it hides `viewerLatestReview`. The whole read still costs one
point (measured in November 2026 on a pull request of 515 commits and 2,752 timeline items);
beside the inbox card's details the verdicts would cost a point more per batch of 25, so
the card never reads them. The cache keeps both (`pullContext.Commits`, each headline cut to
200 runes, never cut by `fitContext`; `Opinion`), and an older build's cache reads with none.
`yourReview` works out what happened since the user's verdict by where its commit is among
the commits, never by their dates, which a rebase keeps: found, the commits after it were
pushed since (`SincePushed`, with the count); missing from every commit, the branch was
rewritten (`SinceRewritten`); missing from the newest hundred only, it was rewritten when a
force push in the conversation follows the review, and is further back otherwise
(`SinceBeyond`); with no commits read, nothing is said. Commits the signed-in user wrote, every
one after the reviewed commit, as a merge of the base they pushed (Update branch's own merge
commit is the clicker's), are nothing since (`allBy`), as the inbox's `ownSince` judges them,
so neither the strip nor Re-request my review shows. The sheet shows them in its Commits tab, the third, and says so above the tabs
(`SinceYourReview`) while the pull request is open. The summary lists
each reviewer's latest review without its text, which is in the conversation, then who the
card says the pull request still asks to review (`withRequests`, from `review_users` and
`review_teams`, no request), above the checks, which show every failing one and fold the
rest under their counts once they pass `openChecks` rows. A merge that
went through tells its `Merges` the repository, without waiting (core: the Release page's
`Refresher`, which reads that repository's row again); a close tells it nothing. `EditLabels`
adds and takes off a pull request's labels as the issue sheet's edit changes an issue's, at
the same issues endpoint (`AddLabels`, `RemoveLabel`, which the client takes for a pull request
too): pinned to the head the request names (another is `PushedSince`, with nothing sent), for
triage access or more (`ErrCannotEditLabels`), through the Actor, each step sent once across the
token retry, and a step GitHub made standing (`LabelsResult`); Autopilot's labels action is its
caller. Nothing here lets an agent approve, merge, or close.

**An open sheet stays current with its card.** The inbox's refresh, its checks follower, a read
of one pull request (`RefreshItem`, a push from this Mac through `internal/pushwatch`), and the
sheet's own read all rewrite the card's details, while the sheet's cache moves only with the
sheet's own reads; the inbox never fills the cache, so a closed sheet costs nothing. Every build
(`Open`, `Refresh`, `Rebuild`, and `Act`'s decision) takes the title, labels, draft, review
decision, review requests, and head (`Stale`) from the card (`inbox.Store.ReviewItem`, whose
`GetReviewItem` reads the merge state and the held runs too, `heldruns.Unnamed` filling in the
runs of no name). When the card's details are newer than the cache's context
(`inboxNewer`: `DetailsAt` after `contextAt`, never against a context `expire` left without a
time), the card wins for what it holds: the summary's counts (`Review.Counts`, `Newer` set, the
card's rollup counts and their time), auto-merge, and the readiness's facts (`Facts.fromInbox`):
GitHub's merge state (but `UNKNOWN`, which leaves the cache's), the checks failing and running,
and the held runs, counted with the ignored workflows `markIgnored` keeps on the cache. The card
counts no check as required, so the required ones are none while the merge state says nothing
required holds the merge (`CLEAN`, `HAS_HOOKS`, `UNSTABLE`), and otherwise the cache's, capped at
the card's totals; an approval names only runs the cache read (`Approvable` capped at what is
asked, so it waits for a read while the card holds more), and runs that failed to start on another
head count for nothing. The checks list, the held runs (`Unrun`), and the conversation stay as the
sheet read them, and `Review.Moved` says the card's head, checks, or held runs moved since:
the cache keeps the read's own rollup counts beside the list (`pullContext.Counts`, the
`github.Checks` the card took from the same answer, nil in an older build's cache), and `Moved` is
any difference from the card's, which the window answers with one `Refresh` (see frontend). A
sheet never read shows the card's counts too, with its
readiness still waiting for the read. `recent` also wants the card's checks state, counts, and
held runs to match the cache's (`contextCounts.matches`): a check that ends moves no activity, so
a sheet reopened within 20 seconds of its read, after the checks follower rewrote the card, reads
again. A read replaces the whole context (`SetDiffContext`), its checks list with it, but the list
is the rollup's first hundred (`contexts(first: 100)`), so `Review.MoreChecks` counts what the
read's rollup counted past it, which the summary links to on GitHub.

**Who reviews it** (`reviewers.go`). The sheet asks people and teams to review a pull request,
and takes their requests off, from a picker that suggests the right people with no request
and offers only people who work on the repository, from the `People` directory it declares
(core adapts `*people.Service`, `reviewPeople` in `internal/core/review.go`; review never
imports it, and without one the sheet learns no one and suggests only GitHub's suggestions).
Every read of the sheet learns for free (`learn`, after `store`): who opened the pull request
and who reviewed it, people only, each with whether GitHub's association gives them access
(`hasAccess`: OWNER, MEMBER, or COLLABORATOR), who committed to it (each linked account once),
who and which teams it asks, and whom GitHub suggests; a failure is logged and never fails the
read. The cache keeps GitHub's suggestions (`pullContext.Suggested`, none in an older build's).
`ReviewerCandidates` is local alone, with no request: the suggestions first, in GitHub's
order, with what the directory knows of each, then the directory's people, the highest ranked
first, never the pull request's author, the signed-in user (who keeps Re-request my review and
Remove me as reviewer), or a login only a bot has, each once and at most 100 (`maxCandidates`,
the avatars' `MaxLogins`), with the repository's teams, how many pull requests its last read
read, when, and whether one is due (`Reviewers.Stale`). `LearnReviewers` is the seed: when the
directory says a read is due (never read, or a week old, and not within 30 minutes of a failed
one), it makes one `FetchRepoPeople` through the inbox's `Peek`, and, for an organization's
repository, one `FetchRepoTeams`, whose refusal or failure leaves the teams the app saw asked;
one runs at a time per repository (`Service.learns`, a `flight.Group` keyed by its name), so
the picker opened on two of its pull requests reads it once. It keeps who opened, reviewed, and
was asked on each pull request read, the teams list when GitHub gave it, the teams asked, and
the read (`MarkRead`), or records a failure (`MarkFailed`) and says so ("Couldn't learn who
works on acme-org/example-repo: ..."), then returns `ReviewerCandidates`. The sheet's extended
read and the seed read each cost one point (measured in November 2026). `EditReviewers` changes
who the pull request asks: names trimmed and taken once ignoring case (`namesOnce`), one both
added and taken off sent not at all (`namesApart`), people and teams apart, and at most two REST
requests through the Actor (`sendReviewers`: `"+"`, `RequestReviewers` for everyone added, then
`"-"`, `RemoveReviewRequests` for everyone taken off), each never sent twice across the token
retry. A review request depends on no commit, so it isn't pinned to the head, but it needs
`Review.CanEditReviewers`: open, read by the sheet, and triage access or more
(`github.CanTriage`, as GitHub's roles give the right to request reviews), else
`ErrCannotEditReviewers` with why; the user is refused (`ErrOwnReviewRequest`, with the sheet's
own action to use) and so is the author (`ErrAuthorReview`), before anything is sent. Each step
GitHub made hands the inbox who the pull request asks after it (`Acted.RequestsEdited`,
`ReviewRequests`), which rewrites the card at once, so the window rebuilds the sheet with no
read; nothing is `settle`d, since the cache holds no requests. A step that fails after one went
is the result's `Failed`, as a label edit's is, and a failure before any is the error. Each
person asked is kept as `picked` in the directory, so the people the user asks most rank up.

**Who it is assigned to** (`assignees.go`). The sheet reads a pull request's assignees with its
one read and keeps them in the cache (`pullContext.Assignees`, nil in an older build's, which
keeps them from being edited until the sheet reads the pull request again: `Review.Assignees`
is then empty and `CanEditAssignees` false, with "Reload the pull request" as why). It offers
people to assign through the `assignees.Finder` core hands both sheets (see internal/assignees):
`AssigneeCandidates` with no request, `LearnAssignees` when a read of the repository's
assignable users is due, and `SearchAssignees` for words, each made of the pull request as the
Finder sees it (`assigneeItem`: the repository, the user, the author when a person, and who
took part, its reviewers and whoever wrote what its conversation shows, bots left out) and of
the read the sheet makes (`readAssignable`: `FetchAssignable(KindPR)` through the inbox's
`Peek`, which stores nothing). Every read records who is assigned (`learn`, `Finder.Seen`).
`EditAssignees` changes who it is assigned to: names taken once and apart as a reviewer edit's
are, then at most two REST requests through the Actor (`sendAssignees`: `"-"`,
`RemoveAssignees` for everyone taken off, then `"+"`, `AddAssignees` for everyone added), each
never sent twice across the token retry; taking off first lets a swap work at GitHub's cap. It
needs `Review.CanEditAssignees` (as `CanEditReviewers`, with the assignees read), else
`ErrCannotEditAssignees` with why; taking the user off is refused (`ErrOwnAssignment`: that is
Unassign me, on the card), and so is an edit that would leave more than ten assigned
(`ErrTooManyAssignees`), before anything is sent. After each step GitHub made, the sheet
rewrites the cache's assignees with GitHub's answer (`cacheStore.setAssignees`: one
transaction of `GetDiff`, the new list, `fitContext`, and `SetDiffContext` with the row's own
`context_at`, so the next refresh is no sooner), and the window rebuilds the sheet with no
read; a cache it can't rewrite is expired instead (`settle`), so the next refresh reads it. The
inbox's card is not touched: it knows only whether the user is assigned, which its next refresh
reads. Each person assigned is recorded as assigned in the repository. A read already under way
when an edit lands can overwrite the rewritten list with the one it read; the next read puts it
right. `PeopleSummary` and `ForgetPeople` (`people.go`) count and empty the People directory
for Settings › Reviewing › People; forgetting answers with what is left, so the row needs no
second read.

## internal/issues

`internal/issues/` is the issue sheet's service, beside the review sheet's: neither imports
the other (the repoguard boundary test), and they share only the building blocks, the
conversation's among them (`internal/timeline`). `Decide`
is pure: from an issue's facts it works out where it stands (gone, closed as what and by
whom, locked, a fix merged or in progress, the user replied last, assigned, open), the
offers, and the one primary, and `Act` runs it again before any write. Comment is offered
always, but held back on a locked conversation for a user without write access; the two
closes (as completed, as not planned) wait for the sheet's first read ("Wait for it to
load"), are left out when GitHub's `viewerCanClose` says no, always confirm, and post the
user's text first (`Offer.CommentsFirst`, with `Offer.CommentBlocked` while a lock refuses
it). The button is Comment, or Close as completed once a fix merged. `Open` serves the issue
from its cache and the inbox card with no request, and starts a visit: `seen_at` holds
GitHub's `updatedAt` of the read it shows, which only `Open` moves, so what a later read
brings that is newer is new to the user (`Issue.LastSeenAt`, GitHub's own times, no clock
skew). `Refresh` reads the issue (one GraphQL point, `FetchIssueView`) unless a read under 20
seconds old matches the card's activity, `ReadAgain` whenever the user asks, one read per
issue at a time (`flight.Group`), through the inbox's `Actor` (`ReadIssue`), which rewrites
the card from the same answer and under whose lock the cache is written; a read that finds
the issue closed or gone is returned once, built from the read, and the cache dropped. One
found closed (its `State`) is how the window learns an issue was closed since the inbox's last
refresh, which closes its sheet (see frontend); one gone shows as gone in the sheet.
`Earlier` reads the timeline's page before a cursor (`FetchTimeline`, the review sheet's
query too) through `PeekIssue`, and stores it nowhere. Every action goes through `ActIssue` and tells each step as an
`event.IssueProgress`. A close is pinned to the comments the user saw (the earlier of the
request's `Seen` and the read's): every try, a token retry included, first reads the state
and the newest ten comments (`FetchIssueState`); a comment someone else wrote since, or ten
newer with more before them, is `new_activity` with nothing sent, and an issue someone closed
first is `already_closed` and leaves the inbox. Then the text is posted once, and the issue
closed with one REST `PATCH` (`(*github.Client).CloseIssue`; an answer that names another
account as the closer, or another reason than the one asked, means someone closed it first).
Once GitHub answered a step, nothing after it is an error: `failedAfterStep` makes a close
whose comment was posted `commented_not_closed`, and keeps any other outcome, logging the
inbox's own failure. After any action, under
the item's lock, the cache is dropped (a close) or marked out of date. It owns
`issue_views`, a bounded cache (1 MiB per issue, whose long texts are cut to 16 KiB, then 4
KiB, then emptied, each cut marked, by `timeline.Cut`; 200 issues, 32 MiB, and 14 days unused
in all), pruned in
the same transaction that writes. Unassign me is the inbox's own `Unassign`. `Replies` and
`FillReply` offer and fill in the prompt library's comment replies for issues (a comment
reply may apply to issues; a default reply, which only the review sheet starts from, must
apply to pull requests), through the reply filler core shares with the review sheet,
which refuses a reply for another kind of item. With triage access or more (`github.CanTriage`,
`Issue.CanEdit`, on an open issue the sheet has read), `Labels` lists the repository's
labels (one GraphQL point, one read at a time, kept in memory ten minutes), and
`AssigneeCandidates`, `LearnAssignees`, and `SearchAssignees` offer people to assign through
the `assignees.Finder` the review sheet shares (`assignees.go`: the issue as the Finder sees it,
`issueItem`, with its participants as the sheet last read them, and the read it makes,
`FetchAssignable(KindIssue)` through `PeekIssue`); every read records who is assigned
(`Finder.Seen`). `Edit` adds and removes labels and assignees through `ActIssue`: the labels
added, each label taken off, then one request that unassigns everyone taken off
(`RemoveAssignees`) before one that assigns everyone added (`AddAssignees`), so a swap works at
GitHub's cap of ten, past which an edit is refused before anything is sent. Each answer is
checked (GitHub ignores a change it won't make), no step is sent twice, it stops at the first
that fails, and it says how many GitHub made (`EditResult`), so the window reads the issue
again only then; each person assigned is recorded as assigned, and the user takes themselves
off with Unassign me alone.

## internal/timeline

`internal/timeline/` is the conversation both sheets show, an issue's and a pull request's:
its entries (`Entry`: comments, reviews with their line comments, and events), a page of
them (`Page`), and the items, labels, and reactions they name. `Entries` converts what the
GitHub client read, the viewer's own marked; `HunkTail` keeps a line comment's last four
lines of its diff hunk; `Cut` shortens the description and every text of the entries, each
cut marked, for a sheet's cache, whose own cap and steps stay its own; `NonNil` fills the
lists an older build's cache lacks. Its JSON is what the issue sheet's cache already held,
so no name or tag changes. It is a leaf: it imports only `github`, `item`, `clock`, and
`textutil`, never a service, the store, or either sheet (the repoguard boundary test).

## internal/avatars

`internal/avatars/` keeps the profile pictures the sheets and the inbox's cards show. The
sheets show people's (who opened the issue or pull request, an issue's assignees and
participants, a pull request's reviewers, and who wrote each comment and review shown in full,
never a bot), which the window asks for in one batch through the issue sheet's bound
`Avatars` (`Pictures`), for both sheets, once a sheet renders, so the conversation never waits
on one. The cards show their authors', people's and bots' alike, which the window asks for
through the service's own binding, `PicturesFor(people, bots)`: core builds the one service,
`Core.Avatars`, hands it to the issue sheet, and binds it, so both share its kept pictures and
its back-off. A person's is downloaded by login (`FetchLoginAvatar`); a bot's (`Bot{Login,
URL}`) by the URL the inbox stored with its item (`items.author_avatar_url`) through
`FetchAvatar`, which refuses a URL off the avatar host, since the host answers a bot's login
with a generated pattern rather than its app's picture. A bot is kept and answered under its
login with `[bot]` after it, which no person's login can hold, so it never takes the picture of
a person named after its app; a bot with no URL is skipped. It owns `avatars`, one row per
lower-cased key, at most `MaxLogins` (100) people and bots together a call, downloaded at most
four at a time, each once until it is a week old and shown again, a failed one (a refused URL
included) not tried again for 30 minutes, and bounded (30 days unused, 2,000 rows, 16 MiB),
pruned in the transaction that writes. The window gets them as data: URLs (`PersonAvatar` and
`card/CardAvatar`, `frontend/src/lib/pictures.ts`), so it loads nothing remote itself.

## internal/people

`internal/people/` is the app's local directory of who works on each repository, which the
review sheet's reviewer picker suggests from with no request (see internal/review), and of who
can be assigned there, which both sheets' assignee pickers suggest from (see
internal/assignees). It takes plain values, people only (the sheets leave bots and mannequins
out), keyed by the repository's `repo.Name`, and imports no GitHub client and no service; the
sheet learns into it from every read of a pull request (`Learn`) and from one read of the
repository's recent pull requests at most weekly (`LearnReviewers`). It owns six tables, these
four from migration `0106_people` and two of who can be assigned (below):
`people`, one row per login, lower-cased as GitHub compares them, with the login as GitHub
spells it now and the name, which an empty one never overwrites; `people_seen`, what a person
was seen doing, keyed by repository, person, pull request number, and role (`author`,
`reviewer`, `committer`, `requested`, `suggested`, or `picked`, the last when the user asked
them from the app), so reading one pull request again never counts anything twice, with
whether GitHub gave them access and GitHub's time of it (a row seen again keeps the access
either saw and the later time); `people_teams`, a repository's teams, as its teams list named
them (`listed`, which a new list replaces) or as a pull request asked one (`requested_at`); and
`people_reads`, when a repository's recent pull requests were last read, how many, and whether
an organization owns it, and when a read last failed.

A person counts as working on a repository (`Ranked` lists them) only with a row that has
access, an association of OWNER, MEMBER, or COLLABORATOR on a pull request they opened or a
review they left, or a `requested`, `suggested`, or `picked` row: someone with access asked
them, GitHub suggested them, or the user did. Commits and reviews without access add weight but
never count on their own, which keeps out the people GitHub would refuse to ask. `rank` is pure:
each row counts its role's weight (reviewer 3, picked 2.5, requested 2, author 2, suggested 1,
committer 1), halved every 60 days of its age (a time ahead of now counts as now), and people
sort by score, then the latest seen, then login, with the pull requests they reviewed, opened,
and committed to counted. Teams list the most recently asked first, then by slug. `Ranked.Stale`
says a read is due: never read, or read a week ago, but never within 30 minutes of a failed
read. It is bounded, pruned in the transaction that writes: per repository the rows of its
newest 400 pull request numbers, rows a year old at most, the latest 100,000 rows in all (by
row, so rows that share a time are cut too), and no person nothing names; a team neither listed
nor asked for in a year, and a read neither made nor failed in a year, go. Every stamp comes
from its injected clock, and an entry without a valid login, a pull request number, or a known
role is skipped.

**Who can be assigned** (`assignable.go`, migration `0108_people_assignable`).
`people_assignable` holds, per repository and
person, when a read of GitHub's assignable users last named them (`listed_at`) and when the app
last saw them assigned there (`assigned_at`, from a sheet's read or the user's own edit,
`SeenAssigned`); `people_assignable_reads` holds each read of the list, by its words
lower-cased and trimmed (`QueryKey`, `""` for the whole list): when it went through, how many
GitHub said match (`total`) and named (`listed`), and when it last failed. A person is one row
of `people` however many repositories they work in, so what the app learns of them anywhere
serves everywhere, and `PruneUnseenPeople` keeps whoever `people_assignable` names.
`KeepAssignable` keeps a read in one transaction: the people (a name only when the read gave
one), each marked listed, the read, and, for a whole list that named everyone (`listed >=
total`), everyone it left out unlisted. `Assignable` returns everyone the repository's rows
name with what the whole list's read says (`Complete`, and `Stale` as `Ranked`'s: due weekly,
never within 30 minutes of a failure). `Covers` reports that a search needs no request: a whole
list that named everyone covers every search; a search that named everyone it matched covers,
for a week (`coversFor`), every longer one that starts with its words; and any search answers
the same words for ten minutes (`repeatFor`). `OrgRanked` ranks, with `rank`, the latest 2,000
rows of who works on the owner's other repositories (access, or asked to review), each person
with the one they were last seen in (`Person.Where`). It is bounded too: rows neither listed
nor seen assigned in a year go, then the oldest past 100,000, a search's read after a week and
the whole list's after a year. `Summary` counts the people and the repositories anything names,
and `Forget` empties every table in one transaction (the people's rows take what names them
with them, by their foreign keys).

## internal/assignees

`internal/assignees/` is what both sheets' assignee pickers share: whom they offer to assign
to an issue or a pull request, in what order, and how that offer is learned and searched with
as few requests as it takes. It knows no store, no service, and neither sheet (the repoguard
boundary test): it reaches the People directory through its `Directory` interface (`Known`,
`Covers`, `Keep`, `MarkFailed`, `Assigned`), which core adapts, and GitHub only through the
`Read` each sheet hands it, one GraphQL point through that sheet's inbox footing. Core builds
one `Finder` for both sheets, so its one `flight.Group`, keyed by repository and words, makes
one read at a time whichever sheet asks, and checks again, as the read would start, that it is
still due. `Candidates` is local alone. `Learn` reads the whole list (`""`) when the directory
says it is stale, keeps it, or records a failure ("Couldn't learn who can be assigned in
acme-org/example-repo: ..."), and returns the list. `Search` filters what the directory holds
when `Covers` says it answers the words, else reads them once, keeps the people found, and
marks those the list holds nothing else of as `SourceFound`. `Seen` records who a read saw
assigned, or the user just assigned. A nil Finder offers no one and reads nothing; every list
is empty, never nil.

`rank` is pure: each person once, never a bot or a login GitHub couldn't have issued, at most
300 (enough for a whole list of 100 beside the item's own people, since a list that named
everyone is never searched past), in the order of their `Source`: the user (`you`), the author
(`author`, a person only), the participants (`participant`), the people assigned in the
repository before (`assigned_before`, the latest first), those who work on it (`works_here`,
as `Ranked` ranks them), those a search just found (`found`), the rest of those GitHub listed
(`listed`, by login), and, only while the whole list is cut short, those who work on the
owner's other repositories (`org`, with where). Once the whole list named everyone, someone
only the directory knows, not on it, is left out: GitHub would ignore their assignment. A query
keeps those whose login or name matches as the window's own filter matches (a field or a word
of one that starts with it first, then one that holds it), and everyone the search found.

## internal/bump

`internal/bump/` reads a dependency update from a bot's pull request title into an `Update`,
for the review sheet and the `.Dependency` prompt variables, and which bot's branch a
head branch is and the ecosystem a Dependabot branch names (`ParseBranch`, the
`dependabot.yml` words, `Ecosystems`), for Autopilot's conditions. It is pure and imports
no service.

## internal/launcher

`internal/launcher/` resolves a launch (the agent first, then that agent's profile, model,
and flags), plans it, and writes its files. `resolvePermission` alone chooses a launch's
access, a background run's fence (`Request.Fence`) included, below the trust rule: an
untrusted item without the override runs Read only, a fixed preset brings its own mode
whatever the profile says, and a fenced plan takes none of the profile's extra directories.
Its `FileChecker` is core's `agentFiles`: the inbox's file check with what agent runs'
commits changed merged in (`fence.MergeChanged`, the inbox's `item_agent_changes`), so a
run that wrote an agent file holds the next launch on its item at that head as
`HoldAgentFiles`, whoever opened it. `internal/launcher/terminal/` renders the launch
script, which only changes into the folder and runs the agent, and opens it in the user's
terminal app, one driver per terminal and OS. `terminal.WriteScript` writes every script the
app opens in a terminal into the launch folder, a session's and a followed run's alike (named
`pr-`, `issue-`, or `run-<number>`, or a repository's launch's `repo-<YYYYMMDD>`, pruned after
seven days by those prefixes alone); a
followed run's script runs its command under `script(1)`, recording its output, its shell's
PID, and its exit status in the run's files. `launcher.CommandOpener`, which core hands the
runs and never binds, opens only such a script, in the terminal chosen in Settings.
`Resume` takes `ResumeRequest.Access`: a background run's session resumes under its run's
fence (`ResumableSession.Fence`, the preset and the run's folder), taken over
(`Fence.TakenOver`, `agent.Access.TakenOver`: the same tools, rules, hook, and sandbox, a fixed
preset in `manual` rather than `dontAsk`, so the user is asked about the rest), unless the
user ticks the profile's own (`profile`), under the trust rule; a fence whose settings are
gone is `ErrNoFence`. The bound `TakeOver` stops a background run first through its
`TakeOvers` (core's `backgroundAgents`), then resumes it so, in its worktree, held back as a
launch is. `launcher.AgentEnv` is the one rule for every agent start's environment: the profile's config
variable set (or unset for the default profile), and, unless the user's per-agent setting
(`settings.Settings.ClaudeAPIKey`, `CodexAPIKey`, off by default) lets a key through, the
agent's `KeyEnv` unset, so the profile's own sign-in is used. An Autopilot routine's launch is
a click's launch with `LaunchRequest.Routine` set (Go-only, `json:"-"`), which only holds it
back more: its files are checked under the unattended scope, only `RoutineLaunch.Confirmed`
confirms a hold, a running session always holds it, a dirty worktree holds it (`HoldDirty`),
`FullAccessOverride` is ignored, and its prompt names the routine. A launch for a pull
request's situation (`LaunchRequest.Situation`, a `Situation` from `situation.go`, refused when
unknown and ignored on an issue) resolves its prompt from the repository's launch defaults'
column for it, the org's, then the app's (`prompts.DefaultPrompts`), and with none set, from
the pull request's own chain, so an org's prompt for pull requests never beats the app's for a
situation; `Resolution.Situations` names the four for a pull request, only to label the card's
agent button. `promptData` fills `.ReviewedCommit` from the inbox's `viewer_review_commit`
only when it is a commit's hex ID (`prompts.CommitID`). `internal/workspacetrust`
marks the launch folder trusted in each agent's own config (Claude's `.claude.json`, Codex's
`config.toml`, appended to and never rewritten).

**A repository's launch** (`repo.go`) starts a session on a repository rather than on an inbox
item, as the Deps page's Upgrade does. The bound `ResolveRepo`, `PreviewRepo`, and `LaunchRepo`
take a `RepoLaunchRequest` (the repository, its clone's folder as the window names it, the ID
of a prompt for repositories, and the launch menu's choices, but never a worktree's name, which
the launcher works out from the user's day, `Config.Location`, the machine's time zone unless
set) and return the shapes an item's launch does, so the window's launch controls serve both:
`RepoResolution` is a `Resolution` (item 0, no kind, trusted, the worktree always on, and the
prompt the launch's own, inheriting none) with a `Notice`, and `PreviewRepo` a `Preview`, its
worktree and prompt the ones it would make and fill in now. Each of the three checks the
repository, its clone, and the prompt afresh (`ResolveRepo` leaves the agent's program, the
terminal, and the worktree to the other two). The repository, by what the inbox last stored of
it (its `RepoFacts`, core's `repoFacts` over the inbox's `Store.RepoFacts`, so the launcher
stays off the inbox): one GitHub says is archived, disabled, or locked (with its lock reason in
words), or where the user's access is read or triage alone, is refused (`ErrRepoClosed`,
`ErrNoPushAccess`), while what isn't known (a repository the inbox never saw, or a permission
GitHub didn't give) lets the launch go ahead, saying so in the `Notice`, which also says a
fork's pull request opens on the fork itself. The clone: one the repository locations hold
(`ErrNotInLocations`), the clone's own folder rather than a linked worktree
(`localrepos.CheckAddable`), with a remote that points at the repository (`cloneOf`, which the
check of a linked clone shares). The prompt: only a prompt for
repositories, by its ID (`repoPrompt`: none, one that is gone, or one for items is
`ErrNoRepoPrompt`, worded). It is planned as an item's launch is, the launch defaults keyed by
the repository, and differs where a repository has no item: it is trusted, as the user's own
repository is, so no outside contributor's limit applies; no file is checked, so only a session
running in the folder it would start in holds it back, which `Alongside` confirms; it always
starts in a new worktree of its own (`WorktreeRequest.Task`, `deps-<YYYYMMDD>`, which the maker
takes or passes for the first free name past it; see [internal/worktrees](#internalworktrees)),
and its prompt is filled in again once the worktree is made (`fillRepo`: `prompts.RepoData`
with the default branch the worktree starts from and the clone's remote, then `WithWorktree`,
no item's field), since a launch made meanwhile may have taken the name planned; its files are
named `repo-<YYYYMMDD>` (`Plan.Task`; `checkSubject` holds a plan and its files to an item's
kind and positive number, or to a task with neither); its worktree has no record to `Touch`;
and its session is recorded with kind `''` and number 0, the prompt's name as its title, the
repository's page as its URL, and the worktree made, and is named `<nickname> · <name>` in the
agent (`repoSessionName`). A launch that fails once its worktree is made, as when the terminal
can't open its script, has the maker remove that worktree again with its own branch, where the
maker can (`WorktreeDiscarder`, core's over `worktrees.Maker.Discard`, the Worktrees tab's Remove,
refused while a session works there or it holds changes; a refusal is logged, and the launch's
own error is what comes back), since a retry makes another. `Resume` refuses such a session
(`ErrNoItem`, by its record's kind and number, `ResumableSession.Repo`), which has no item to
plan its resumption on. The launcher itself runs only the clone's read-only remote lookup; the
worktree maker fetches the default branch and makes the worktree.

## internal/sessions

`internal/sessions/` follows the agent sessions launches start. It owns `agent_sessions`, the
ledger the launcher records each launch in (through its `SessionLedger`) under an ID minted
before the launch, which Claude takes as `--session-id`. A `Watcher` (a desktop worker)
reconciles what each agent reports into it and emits `event.SessionsUpdated`; one `Observer`
per agent turns the agent's own shapes into one `State`: `observe/claude` runs `claude agents
--json` and reads the transcript tail, and `observe/codex` talks to Codex's app-server daemon
over `codex app-server proxy` with a read-only JSON-RPC client (its `Allowed` methods only, over
a WebSocket it speaks itself, since only `internal/github` speaks HTTP), falling back to the
rollout files. Each observer degrades to `StateUnknown` rather than guess, and a session's
process says whether it still runs. Following never sends a session anything, and nothing here
reaches the network. A session that stops working (idle, waiting on the user, ended, or
failed) hands its item to the inbox's `Rereads` (a desktop worker, through the `Rereader`
interface core wires), which reads it again, one GraphQL point each, one item at a time. A
session still working hands it over too once its agent finishes a command that changes
GitHub (`Observation.Acted`, from `fence.ChangesGitHub`): the Claude observer follows each
listed session's transcript from where it last read it (`commandScan`, `commands.go`: only
what the file gained, from its last 4 MB the first time, at most 4 MB a poll, lines over
1 MB passed over, after the `Lstat` that refuses a link and through `paths.OpenRegular`),
noting a Bash `tool_use` stamped at or after the launch that isn't `run_in_background`, and
telling it once its `tool_result` arrives without `is_error`; the Codex observer weighs the
`commandExecution` items of the turn it reads anyway (`status` completed, `exitCode` 0, each
once by turn and ID, none from its first read, which ran before the session was followed).
The Watcher reads `Acted` from the observations as made, before `Apply` drops one that
changes nothing, and throttles per item (`heedActed`, `ActedGap`, 30 s): the first at once,
any more within the gap once at its end, and a session that stops working drops one waiting,
since its own read covers it. A background run's session is its run's to report
(`Ledger.RunState`), never an observer's, so it gets only the read as it stops working. A
single-item read (`RefreshItem`, the checks follower, the review sheet's `ReadPull`) takes a
pull request out of Review requested once GitHub no longer asks the user, by name or
through a team, and an item out of the assigned list once it is no longer assigned to them,
as the issue sheet's `ReadIssue` does too, stamped like the inbox's own edits
(`review_left_at`, `assign_left_at`), so a lagging search keeps it out until it moves, and
`RefreshItem` and `ReadPull` read the user's own verdict with
it at no extra point, so an approval or a request for changes the agent left with gh reaches
the card at once and is told once as the user's review (see
[internal/inbox](#internalinbox)). `LastWords` reads a session's whole last message from the
agent's own files, on demand, for the review sheet, and `Usage` its token counts, for the
sessions panel (a `UsageReader` per agent: Claude Code's transcript, each assistant
message's usage once, read incrementally to 64 MB; Codex's last rollout token count; a
background run's session its run's own, through `RunTokens`, which core adapts over
Autopilot's ledger), never the network. Each record keeps what its launch
reported (the permissions it started with, what it did to the item's worktree, and what it
worked around), which the card's sessions panel shows in place of a line on the row after
the launch, and what came of its item on GitHub while it ran (`GitHubEvent`: approved,
changes requested, merged, or closed, with the review's commit and GitHub's time; migration
`0102_runs_and_caches`). That comes from the inbox's outcomes, never a request:
core's `sessionOutcomes`, the third sink of `outcomesFanOut`, late-bound once the sessions
are followed, turns each `inbox.Outcome` whose `GitHubFact` says a decided review, a merge,
or a close into a `GitHubNote` for `Ledger.NoteGitHub` (Go-only), which records it
(`NoteGitHubEvent`) on the item's sessions that ran at its time, launched by then and not
ended before, give or take `noteSkew` (10 s), keeping a later event over an earlier one, and
emits `event.SessionsUpdated` once when any changed. `ShowFolder` shows a session's folder in Finder through core's `open -R`
Revealer. The bound `Forget` deletes an ended session's record on the user's click (the
panel's ×), refusing one still running (`ErrLive`; failed and unknown count as running) and
counting one already gone as done, and emits `event.SessionsUpdated`; the prune is
otherwise the only thing that deletes a session.

**A repository's sessions.** A repository's launch (the Deps page's Upgrade) records its
session with no item: kind `''`, number 0, item 0, and the worktree its launch made, never
`''`, its title the prompt's name and its URL the repository's page, which the table's wider
checks take (`0112_repo_sessions`: `(kind = '') = (number = 0)`, and `kind <> '' OR (item_id = 0
AND worktree <> '')`). `Record.valid` takes either an item's record (a kind and a positive
number) or such a one (`validSubject`), so the launch's record is stored rather than dropped
with a log line. `Record.IsRepo` and `Session.IsRepo` tell one. It is followed as any session
is, found running in its worktree for a later launch's hold, but offers no resume (`CanResume`
false: the launcher resumes a session on its item), hands the inbox's rereads nothing whatever
its agent did on GitHub, gets no `GitHubEvent` (`NoteGitHub` passes it over), and is pruned
with its repository's own, its newest `KeepPerItem` kept apart from its items'. `Session.Label`
and `FullLabel` name what a session works on, as the menu bar's waiting rows and the session
banners show it: "example-repo #7" (or "acme-org/example-repo #7"), or, for a repository's,
the repository and the prompt's name, "example-repo · Upgrade dependencies", whose banner opens
the repository's page.

## internal/localrepos

`internal/localrepos` finds the user's clones, in the clone root (`clones.root`, the primary,
which new clones go in) and the other repository locations (`clones.locations`, which
settings alone writes: each a folder looked into one level deep as the clone root is, or a
single clone, its kind worked out once as it is added). `Roots` holds them, each expanded
and its links resolved once; `Roots.Clones` lists the clones in all of them at once,
reading no git, a clone two locations reach listed once, the clone root's first, and an
extra location it can't read skipped and named (`Found.Skipped`), while a clone root that
can't be read is an error. A linked worktree in a location is left out for its clone, as
`CheckAddable` refuses one as a location: for the clone when the locations list it too,
wherever, or else for the first of its worktrees (a bare repository's included), which
stands for it, so each page lists a clone once however many of its worktrees sit beside it; `Holds` says whether the locations hold a clone, which every
service checks before it changes one; `Key` is the clone root alone while there are no
others, so a cache keyed by it stays valid. `Survey` previews a folder before it is added,
with what to add instead (the folders inside one with no clones of its own, at most 100
subfolders and 5,000 entries looked at, or the folder of a clone that holds others), and
`Check` says how each stored location reads now. `Candidates` suggests folders for the
setup's Clones step: every folder directly in home that holds clones, whatever its name,
or the folders one level down inside it that do, and the same in GOPATH's and ghq's
layouts, 20,000 entries at most in all, the common names first. It never reads a hidden
folder, a link in home, macOS's own folders (Desktop, Documents, Downloads, Library, and
the rest, which macOS guards or which hold no clones), or a folder a disk is mounted on.
It reads the mounts from the kernel's table with `MNT_NOWAIT` (`mountPoints`), so it
never asks a share, a cloud drive, or a virtual machine's files, and never makes macOS
ask the user. The Clones step looks in GitHub Desktop's `~/Documents/GitHub` only on the
user's click (`PreviewCloneLocation`), and `Classify` words a folder macOS keeps the app
out of (`EPERM`) with where to allow it. No location is searched recursively.
One `Reader`, which core builds and hands every service that reads the locations, bounds
each read of another location than the clone root, the resolution of its links included,
to `DefaultReadLimit` (5 s) and keeps one of each going per location: a share that hangs
reads as `StateNotAnswering` and is skipped, its hung read joined until it ends, never
started again beside it. The clone root is always read directly.

## internal/pushwatch

`internal/pushwatch` notices a push made on this Mac to a pull request the inbox lists, so
its card catches up at once rather than at the next refresh. Git runs no hook after a push,
but a push that lands moves the clone's own `refs/remotes/origin/<branch>` to the commit it
pushed, whatever ran it. The `Watcher`, a deferred desktop worker (`Core.PushWatcher`),
follows that ref and hands a pull request whose branch moved to the inbox's `Rereads`
(`RefreshItem`, one point), as the sessions' Watcher does. It reads its pull requests, the
clones, each clone's origin, and its switch through its own `Targets`, `Clones`, `Origins`,
and `Switch`, which `internal/core/adapters_pushwatch.go` adapts; the repoguard boundary
test keeps it off the inbox, settings, the store, and GitHub.

- **What it watches.** `inbox.Store.PushTargets` (`ListPushTargets`; the Store's, so it is
  not bound): the signed-in account's listed open pull requests, neither dismissed nor
  snoozed, whose head branch is in their own repository (`is_cross_repository = 0`), a muted
  repository's or org's left out, the ones the user opened first, then by activity, each with
  its head commit as the details last read it. The Watcher keeps the first `MaxTargets`
  (200) whose branch `gitread.ValidBranch` takes, so a ref's path never leaves
  `refs/remotes/origin`. It reads them, and the switch, once a minute (`TargetsEvery`), and
  at once when woken: `pushesOnInbox` wakes it on every `event.InboxUpdated`, so a pull
  request newly listed is watched before it can be pushed to, and `event.IntervalsChanged`
  (which `SetWatchPushes` and a backup's import emit too) through core's `followSwitch`.
- **The clones.** Every `IndexEvery` (5 minutes) while there is a pull request to watch, it
  lists the clones in the clone root and the other locations (`Roots.Clones`, no git), each
  by its common git folder (`localrepos.CommonDir`), so a linked worktree listed beside its
  clone counts once, and a push from any worktree moves the one ref. A clone's origin is
  read with the Release page's `releaseOrigins` (one `git config --get-regexp`) the first
  time, and again only once its `config` file's stat changed; only a clone whose origin is
  the pull request's repository is watched, every one of them.
- **A look.** Every `Tick` (3 s) while some watched pull request has a clone, it `Lstat`s each
  watched ref file (anything but a regular file reads as missing), and reads one
  (`paths.ReadHead`, 128 bytes, a full commit ID alone or nothing) only once its stat changed:
  inode, size, and time, since git writes a moved ref as a new file, a lock renamed over it.
  The first look at a ref only learns where it is. A ref packed away (`git pack-refs`) or
  deleted keeps the commit last read, and only a ref read at another commit moved. A clone
  that keeps its refs in reftable (`reftable/tables.list`, which git replaces whenever any
  ref moves) is stat'ed by its table list, once a look, and its watched branches are read,
  once it changed, with gitread's allowlisted `rev-parse --verify --quiet
  refs/remotes/origin/<branch>`; without a `Git`, such a clone is not watched. Nothing is
  written to a clone or installed in it.
- **The read.** A ref that moved to a commit other than the head GitHub reported has every
  pull request on that branch read again `Settle` (2 s) after the look, so GitHub has taken
  the push in. Before each read the pull requests are read again, and one whose new commit
  GitHub reports now, or that is no longer watched, waits no more. While GitHub still
  reports another head, it is read again after 10 and then 30 seconds (`retryAfter`), three
  reads at most, then left to the refresh. A launched session's push reaches `Rereads`
  through the sessions' Watcher too, and the inbox's change after either read stops the
  watch's retries.
- **Off.** While `refresh.watchPushes` (Settings → Refreshing → **Notice pushes from this
  Mac**, on by default, carried by a backup) is off, it reads nothing, not even the list,
  until woken. Turned on again, it starts afresh, so a branch moved while it was off is the
  refresh's, and it keeps only the origins it read, which hold while their config files do.

## internal/worktrees

The app owns its worktrees: `internal/worktrees` makes them (one per item per clone, shared by
every agent, under `<the clone's folder>/.<slug>/worktrees/<clone>/`, so in a location as in
the clone root), records them in `internal/worklog`, checks every record against git on each
listing and sweep (a record whose clone's folder is gone too, as on a drive unplugged, is
kept), and removes
them safely. A listing runs git only in the clones worth listing (`worthListing`): one whose
common git folder registers a linked worktree or can't be read, and one a record names, read
without git by `linkedWorktrees`, which the sweep's count shares. A pull request's worktree
fetches its head, and, on every Ensure, made or found, its base into the remote-tracking ref
(`fetchBase`, the default branch's own fetch: best-effort, a notice when it fails, never a
checkout or a local branch moved), so an agent has a fresh base to rebase onto or compare
with. Only `internal/worktrees/create.go` fetches or runs `git worktree add` (Repo
scripts' pull, below, fetches only as `git pull` itself does), and nothing the app runs
changes a clone's own checkout; `TestNoGitCommandChangesAClone` in `internal/repoguard` fails
the build otherwise. There are two exceptions, each on the user's click. A tool the user
turned on: a release's magex switches to the branch, pulls, tags, and pushes the tag
(`internal/release`), and the app only starts it and checks afterwards where the tag and the
clone are. And Repo scripts' **Pull**: a fast-forward-only pull of the branch a clone is on
from the branch it follows on origin (never a merge or a rebase), held to
`internal/scripts/pull.go`, whose one pull argv the guard parses and holds to an exact list
of literal words (`--ff-only`, `--refmap=`, and `--no-verify-signatures` among them),
with no other option anywhere in the file and no other verb it bans. Every git command
there runs with a hooks folder's hooks, the file system monitor, and submodule recursion
off (`gitread.Hardening`, which the worktrees' `hardening` and the pull share), since a
worktree holds a pull request's own files, and a pull request's worktree is made only at
the head whose files the launch checked; a clone's own config-named hooks refuse every
command that could fire one (below). With core's `Guard`, the worktree service's own
commands on a clone or its worktrees are guarded too (`git.do`): the reads (`readOnly`:
listing the worktrees and branches, resolving a ref, counting commits or listing those not
on GitHub, an ancestor's check, and a commit's time, the logs with `--no-show-signature`
and `--no-mailmap`), which run no
filter or hook but could read an object a partial clone lacks, run with gitread's
`ReadEnv` (no transport, no lazy fetch, no optional lock, no replace ref), so a promisor
remote the clone names is never asked; every other (a status, which runs a file's clean
filter, a removal, a prune, an unlock, and a branch's deletion, which fires
reference-transaction) runs with gitread's `LocalSetup` for its folder, worked out once
per operation and one folder at a time (`localCache`, a `flight.Group` per folder):
every filter driver of the clone's own off, Git LFS's too, no optional lock, no replace
ref, no transport, and refused before git runs, with a `guardError` worded for the
window, when the clone's own config holds a setting gitread refuses. A removal, whose
own status runs inside the worktree, weighs the worktree's config too (its
`config.worktree`), and runs with the filters both define off. Such a worktree shows the
refusal as its `Error`, which also takes away its removal (`CanRemove`), so no sweep or
cleanup removes it, and a removal fails with the same words. `create.go`'s one fetch argv (`fetchArgs`:
`fetch --no-tags --refmap= --upload-pack=git-upload-pack <remote> <refspec>`, so it moves
only the ref its refspec names, never one the clone's own `remote.<name>.fetch` maps,
which the repoguard test holds to exactly those words), its `ls-remote --symref` (with
`--upload-pack=git-upload-pack`), and its `git worktree add` also run under gitread's
guards, through the worktrees `Guard` core gives every `Service` (the same `gitread.Git`
as Release's): an Ensure that builds a worktree works out one `RemoteSetupFor` its
item's remote, on its first guarded command, under the clone's lock (`git.session`), and
`git.reach` runs a command that reaches the remote with it and `git.write` one that writes
files with its `Checkout` (no transport, but a partial clone's checkout may fetch from its
promisor remotes, which are weighed too). A checkout weighs only what it can reach: when
the setup refused the remote itself (a `RefusedError`), `git.write` works out
`CheckoutSetup` instead, so a worktree from the last fetched copy, or on a branch the
clone has, is still made, while a partial clone's promisors, and rerouting settings Git
LFS would follow, still refuse it. Each runs as `git -C <clone> <words>` with that
setup's environment, which holds every setting (`GIT_CONFIG_COUNT`), `GIT_DIR`, and
`GIT_WORK_TREE`, and the setup's own git, through `Config.GitWith`, core's runner with an
environment of its own. Core gives the worktrees gitread's `GitFinder` as their `Finder`,
so every other worktree command runs that same git: one clone is never handled by two
git builds. A clone whose own config holds a setting gitread refuses (a hook, a filter,
`core.worktree`, an include, ...), a remote helper, or a rerouted remote is refused
before git runs (a `guardError` holding gitread's), in `gitread.Message`'s words as a
sentence, naming the settings or the remote or saying why the setup couldn't be worked
out (a config git can't read, no git), which the launch's error shows; a fetch that
failed after the clone's own ssh command, password program, or credential helpers were
set aside says which (`gitread.SetAsideNote`, a `gitread.SetAsideError` in the chain).
The launcher
reaches the `Maker` through its
`WorktreeMaker` interface; no agent's own `--worktree` flag is used. A branch is deleted only
with the conditional `git update-ref -d <ref> <oid>` (the repoguard tests ban `git branch
-d/-D`): by `create.go`, of a branch it just fetched at a head the launch didn't check, and
otherwise only through `internal/worktrees/branches.go`, when the user
ticked it (or, for the automatic cleanup, the user's rules pick it), every commit on it is on
GitHub, and neither the user's protected patterns (`internal/branchrules`) nor a remote's
`HEAD` keeps it. The one exception is the cleanup of merged branches: a branch kept only
for its commits not on GitHub (`MergedBranch.Unpushed`) can be deleted there anyway, but
only once the user asks for such branches to be offered, ticks it (never ticked to start
with), and confirms a second dialog naming each and what it holds; pinned to the commit
shown, whose result names that commit. A branch checked out, protected, or a remote's
default never is. Every job that changes a clone holds that clone's lock from
`internal/clonelock`, one `Set` that core builds and hands to each, keyed by
`paths.Resolved`. Lock `act` before a clone's lock, never after. Worktree changes wait only
for one another. A run (a release, checks, a script) is never waited for: a launch or a
click fails at once with a sentence naming the run, and the sweep and a listing's
forgetting of a record skip the clone until next time.

A listing reads each worktree's status once (`status --porcelain=v1 -z
--untracked-files=all`, read by `readStatus`): every file it names counts toward the row's
`DirtyCount`, and the first 12 by path are its `Files`, each with git's two status letters
and its path (a rename or a copy by its new one, its source skipped), which the view shows
before it reads the rest. Only those 12 are kept as the status is read, each file past them
only counted, so a worktree with tens of thousands of untracked files costs little more
than the count. A clean worktree's `Files` is `[]`, and so is one whose status couldn't be
read. Keeping them runs no git of its own.

**Linking a worktree to its item.** A worktree is linked to its pull request or issue by its
record, by the name of the folder the app or Claude Code made it in (`pr-<n>`, `issue-<n>`),
or by an item's own branch (`worktree-pr-<n>`): `Worktree.LinkedBy` is `name` then. A linked
worktree with none of these, on a local branch pushed to one of the clone's GitHub remotes,
is linked by that branch (`branch`, `branchlink.go`): through the remote-tracking branch of
the same name, its upstream's remote's first when it follows one by that name (an upstream
of another name is the branch it started from, its base, not its head), then origin's, then
the other remotes' by name. A remote's default branch, a detached HEAD, and a remote that
isn't on GitHub never link. Of the pull requests whose head branch has that name in that
same repository (`headRepository`), so a fork's branch of the same name never does, it links
to the open one, when there is one, else to the one updated last, merged or closed. Each clone's refs are read once a listing (`refsArgs`:
`for-each-ref` over `refs/heads/` and `refs/remotes/`, each with its commit, upstream,
symbolic target, and worktree, a read with `ReadEnv`), and the branches a removal offers are
judged from the same read (`markRepo`), so linking by branch runs no git of its own; a clone
whose refs can't be read links nothing by branch and offers no branch. A remote's name may
hold a slash, so a remote-tracking branch is the branch of the clone's remote, of every one
it names with a URL (`localrepos.LocalRepo.Names`, from the same config read as its GitHub
remotes), whose name is the longest that begins it (`withOwners`):
`refs/remotes/origin/mirror/main` is a remote called `origin/mirror`'s `main`, never
origin's `mirror/main`, and a branch of a remote the clone doesn't name is no one's. So is a
remote's HEAD: `refs/remotes/origin/mirror/HEAD` names the default branch of the remote
called `origin/mirror`, once the clone names that remote, which nothing links through nor
looks for copies on. GitHub is asked in the
listing's request for the items' states: `github.FetchLinkStates` adds one aliased
`repository { pullRequests(headRefName: ..., states: [OPEN, MERGED, CLOSED], first: 20,
orderBy: UPDATED_AT DESC) }` per branch to the states query, at most fifty of each a request,
so branches beside items cost no request, and branches alone one (twenty pull requests a
branch, so the repository's own isn't lost behind forks' of a branch of the same name
updated since, still one point a request). The answer is kept in
memory by repository, branch, and the commit the clone saw it at (`headCache`): the next
listing asks about that pull request by number with the items, and about the branch only
once it moves, or, at most every `headRecheck` (10 minutes), while no pull request heads it
or the one that does is done (one opened since, or a branch reused for another, with or
without a push). A pull request GitHub no longer answers as that branch's is forgotten, and
the branch asked about again. A worktree linked by its branch is never recorded as its
item's (`adopt`), so a launch on the item never resumes it; it offers no pull request's
branch with its removal (`BranchPullRequest` stays `name`'s); and the sweep removes it only
when the app or Claude Code made it (`Managed`) and, as always, its HEAD is the merged pull
request's head.

**A task's worktree** (`task.go`). A repository's launch asks for a worktree for a task on the
repository itself rather than an item's (`EnsureRequest.Task`: its base name, `deps-<YYYYMMDD>`,
with no kind, number, base, or head, `checkSubject`). It is made new every time, as an issue's
is, from the repository's default branch as just fetched, through the default branch's fetch
and `git worktree add --no-track -b worktree-<name>`, so no new argv (`Ensured.Task` is the
name taken, and `DefaultBranch` the branch it starts from, or, for `Plan`, which asks no
remote, the request's or the one the clone last learned from its remote's HEAD), and it is
never resumed, adopted, attached, or recorded: one reused could build on commits a merged pull
request already took, and a record names an item, so it would link the worktree by name and
keep its branch from relating it to its pull request.
Its name is the first free one of the base, then `<base>-2` up to `<base>-99` (`freeTaskName`;
past those, `ErrNoTaskName`, worded): a name is taken while anything is at its managed path, or
may be, as when the path can't be looked at; while git has a worktree at that path or on its
branch, its folder there or gone; while the clone has that local branch; and while it has a
remote-tracking branch of that name on any remote, so one left from an earlier task, pushed and
since merged, never relates the new worktree to the old pull request. `Plan` says the name it
would take now, asking no remote, and `Ensure` takes the first free one as it makes the
worktree. Calls for a task never share a run (no `calls` grouping, which would hand two quick
launches one worktree): the clone's lock runs them one after the other, so a second the same day
takes the next name. It is made only from the clone's own folder, the main worktree git lists
first (`ErrNotMainWorktree`), so it sits beside the clone's other managed worktrees, where a
listing and the sweep find it as the app's (`Managed`, `OwnBranch`). One its launch made but
couldn't start a session in is taken away again by `Maker.Discard`, as `Remove` takes one away
(refused while a session works there or it holds changes, its branch deleted only while nothing
on it would be lost), so the next launch takes its name back. Once its session pushes the
branch under its own name (`git push -u <remote> HEAD`) and opens a pull request, a listing
links it by that branch (`branch`, above), and the sweep removes it once that pull request
merges with its HEAD as the head, as for any worktree the app made and a pushed branch links.
The limit: a fetch of the user's own that prunes the remote-tracking branch, as after GitHub
deletes the merged branch, before a listing sees the merge leaves the worktree linked to
nothing, and it is then removed by hand.

**Copies of a worktree's commits** (`copies.go`). A coding agent's sub-session works in a
worktree of its own, on a branch no one pushes, and the session that started it copies its
commits (a cherry-pick or a rebase) onto its own branch and pushes that. A copy keeps the
commit's change (git's patch ID), author, author time, and subject, but not its commit ID,
so the original counts as not on GitHub (`Unpushed` counts by ID, and keeps doing so). Each
listing compares the commits not on GitHub (U) of every linked worktree that has any with
the commits on the clone's pushed branches: the remote-tracking branches of its GitHub
remotes, each remote's default branch aside. Per clone, one log reads U of every worktree
with at most 200, with each commit's parents, author time, author email, and subject
(`copyUnpushedArgs`: `log --no-show-signature --no-mailmap --max-count=10000
--format=%H%x00%P%x00%at%x00%ae%x00%s <HEAD>... --not --remotes`, with `--ignore-missing` and
the commit they were made at from GitHub when they have one, one log per such commit), each
worktree's own U then walked by its parents within it; and one more log reads the pushed
commits not in the default branch whose commit time is at most a day before the oldest of
those author times (`copyCandidatesArgs`: `log ... --no-merges --source --max-count=20000
--format=%H%x00%S%x00%at%x00%ae%x00%s --since=@<seconds> --remotes --not
refs/remotes/<remote>/<default>`; a copy keeps the author time and is made later). When that
log reaches its cap, only the worktrees that need its window (the oldest) can't be compared,
and one more log, from the next oldest worktree's window, serves the rest, so one worktree
left for months never keeps its clone's others from being compared. A pushed commit with a
commit's author time, email, and subject is its likely copy. The log credits each pushed
commit to one ref only (`--source`), often the lowest of a stack of branches, or a remote's
HEAD, so the pushed branches it credits with likely copies, most first and at most three,
only order the comparisons: for each, one `rev-list --left-right --no-merges
<tip>...<HEAD>` (`copySidesArgs`, commit IDs only) lists the commits the branch has that
HEAD hasn't (`<`) and back (`>`), and every likely copy on the branch's side is one there,
whatever ref the log credited it to, so a branch built on another counts the copies the two
share, even those of a default branch no copy is looked for on. No
patch ID decides anything: git's leave out every space inside a changed line, a hunk's
position, and the mark of a missing last newline, so a copy that was only re-indented, or
lost a space from a string, would pass, though in Python, YAML, a Makefile, or a string
literal that is a change. So each pair is compared byte for byte (`copypatch.go`): the changes of the commit and its likely
copies are read in one more log for the comparison (`copyPatchesArgs`: `log
--no-show-signature --no-mailmap --no-walk=unsorted --root -p --no-ext-diff --no-textconv
--no-color --no-renames --full-index --unified=3 --format=%x00%H <commit>...`, a read) and
compared whole but for a text file's blob IDs (its `index` line keeps only its mode) and the
line numbers of each hunk's `@@` header (and the text git adds after it): spaces, context
lines, `\ No newline at end of file`, modes, and file headers all count. A binary file's
change is the same only when its blobs before and after are, its `index` line whole. An
empty change, one that doesn't read as git's per-file diffs, or one git left out is no
copy's, and a log that fails leaves the worktree not copied, with why. A verdict on a pair
of commits is kept in memory for good (`copyCache.pairs`), as commits never change. A
commit with a likely copy there whose change is the same has an identical copy; one with
likely copies there whose changes differ an edited copy, which never counts (an edited
change isn't the commit's); any other none. A copy made on a moved base whose context lines
differ is edited too. A worktree each of
whose commits has an identical copy on one branch is fully copied onto it, and links to that
branch's pull request, asked about as a branch link is, when nothing else links it
(`LinkCopy`); the pull request of the branch a worktree copied only in part is asked about
the same way, without linking it. `Worktree.Copies` says what was found: the branch (`ref`,
as in `origin/feature-x`, and `branch`, its name on its remote as `withOwners` works it out,
`main` for `origin/mirror/main` of a remote called `origin/mirror`, or `''` when no remote
the clone names is its), how many commits (`count`), and how many have an identical copy,
an edited one, or none, with `note` saying why they couldn't be compared, or why identical
copies on several branches don't count; `pr` is the number of the pull request the copies
went into (the one `ref`'s branch heads, or, when `recorded`, the one the record names; 0
when none is known), and `gone` says `ref`'s remote-tracking branch is gone, as it can be
only when `recorded`. Its zero value means there was nothing to compare with. Each read is a read (`readOnly`, with `ReadEnv`, so an object a partial clone lacks
fails it rather than being fetched), and the allowlist holds each to its exact words: commit
IDs, a remote-tracking branch, and a whole number of seconds. Anything that goes wrong leaves
the worktree not fully copied and never fails the listing: git refusing a read, or warning
on its way (its answer may then be wrong), a log
reaching its cap, more than 200 commits, a merge among U, a count that changed meanwhile,
or a comparison that leaves a commit out. What a comparison found is kept in memory
(`copyCache`) by the worktree's HEAD, the commit it was made at from GitHub, its clone's
GitHub remotes, and every remote-tracking branch and where it points
(`cloneRefs.fingerprint`), so a listing after which nothing of those changed runs no git to
compare, and a worktree that is gone is forgotten, as is every worktree of a clone the
window's listing, which lists every clone with a linked worktree, didn't list (the cache
starts over, too, past 4,096 worktrees); git that couldn't be run at all is tried again next
time. `Details` marks each of its commits not on GitHub with its copy
(`UnpushedCommit.Copy`, `identical` or `edited`, and `CopyOID`): what the listing found,
when one read of the clone's refs and one of its remotes say nothing changed since (the
listing's key: its HEAD, the commit it was made at, the GitHub remotes and every remote's
name, and the refs' fingerprint), or else a comparison of its own; none when the refs or the
remotes can't be read. `Details.Copies` (`CopyComparison`) says how that
went, so a blank mark is never mistaken for an unknown one: `compared` is true when the
comparison ran to completion (the pushed commits read, every branch holding likely copies
compared and confirmed byte for byte), or there was no pushed branch to compare with;
`ref` is the branch compared with, `""` when no pushed commit was a likely copy of any, and
`branch` its name on its remote, as `Copies.branch` says it;
`identical` and `edited` count the commits; and `note` says why it couldn't run. So
`compared` with `identical` at 0 means no copy is on a pushed branch now (the branch was
deleted, or force-pushed without them), while `compared` false means only that this is
unknown. It reads no copy record, so a worktree its record vouches for
(`Worktree.Copies.recorded`) may compare with nothing; its zero value is for a worktree
with nothing not on GitHub.

**The copy record** (`copyrecords.go`, migration `0109_worktree_copies`). A worktree fully
copied onto a branch stays so only while the branch is there: a squash merge leaves the
copies out of the default branch, and deleting the branch leaves them on no remote-tracking
branch. The pull request keeps them (GitHub keeps every pull request's head commit), so when
a listing finds a worktree fully copied onto a branch whose pull request it knows, it records
that pull request in `worklog` (`worklog.Copy`, the `worktree_copies` table, one row per
worktree by its resolved directory): the worktree's HEAD, the branch, the pull request, the
commits not on GitHub, and `copy_tip`, the branch's commit, which moves on with the branch
while it still holds every copy, so it is most likely the pull request's last head. A record
that already says all a new one would isn't written again. Each listing checks the records
against the worktrees it listed, as the launch records are: one whose worktree is gone (or
its folder), or whose HEAD isn't the record's, is dropped, and so is one whose clone is gone
from a folder that is still there; one whose clone wasn't listed, or couldn't be, is kept.
Removing a worktree drops its record too. The worklog store stamps a record's
`created_at` (kept while the HEAD is the same) and `updated_at` with its own clock.

**Copied work counts as on GitHub** (`copyjudge.go`). `OnGitHub` is `Unpushed == 0`, or HEAD
being the linked pull request's head, or the worktree's copies counting (`copyState`), while
`Unpushed` keeps the count by commit ID. They count when this listing found every commit not
on GitHub with an identical copy on one pushed branch of a GitHub remote: on GitHub as that
branch. Or when the copy record holds, and either its branch is gone or its pull request
merged: the record describes the worktree (HEAD is the record's); it covers every commit the worktree has that isn't on
GitHub, each being one whose identical copy the record vouches for or on the record's branch
commit by ID (`judgeCoverage`: no git when this listing's comparison read those commits and
all are the record's, else one `log` per record branch commit, `copyUnpushedArgs` with that
commit after `--remotes` and `--ignore-missing`, kept in memory by the worktree's HEAD, the
remote-tracking branches, and the record; this is what keeps the commits a worktree started
from on a branch since squash-merged covered, while a commit of another branch deleted
unmerged is never vouched for); the record's pull request was read in this listing's request
(asked by number, with the items, only for a covering record); and that pull request's head
is the record's `copy_tip` or has it (`prHolds`: `merge-base --is-ancestor`, which a commit
the clone lacks fails; a yes kept for good, a no while the remote-tracking branches are the
same). `Copies.Recorded` says so then, with `Identical` at `Count`, and a worktree nothing
else links is linked to that pull request (`LinkCopy`). A merged pull request vouches
whatever became of its branch, deleted or force-pushed without the copies; while it is
open and its branch is there, only the copies this listing finds on the branch count, so a
branch force-pushed without them makes them count for nothing. A record that covers less
than the worktree holds links nothing. `stageOf` is unchanged: a merged or closed item,
on GitHub, and clean is finished. The record's pull request merging with every copy
(`copyState.merged`: the record holds, and its merged pull request's head has `copy_tip`) is
what the cleanup waits for: `autoCleanable` takes a worktree whose work is on GitHub only as
copies only then, and one linked by its copies only when that is the pull request it is
linked to, and only when the app or Claude Code made it; never while the pull request is
open, and never by a branch's name. Its own branch, and a pull request's branch it has
checked out, are deletable then too (`headSafety`), still subject to the protected patterns,
a remote's default branch, and the conditional `update-ref -d`; while the pull request is
open, their reason says the commits are on GitHub only as copies in a pull request that
hasn't merged with them.

The rest of what a worktree's drawer shows comes from `Details(repoPath, path)`, which the
window calls when the drawer first opens. It first checks the request as the removals do
(`checkedGit`, then `entryOf`, which lists the clone's worktrees, then `ErrPrunable` for a
folder that's gone), and fails when the request can't be checked or isn't about a linked
worktree of a clone the repository locations hold, or when its context ends. Past that,
every read it can't make is a sentence in `Errors`, not an error. It runs three reads side
by side:

- the status, every time, so the drawer shows the files as they are now: the first by
  path, at most 200, and `FileCount`, all of them;
- the count of the commits not on GitHub, every time, the listing's own, and only when
  it is over 0, their list (`log --no-show-signature --no-mailmap --max-count=200
  --format=%H %ct %s HEAD --not --remotes`, or with `--ignore-missing` and the commit the
  worktree was made at after `--remotes`, as `unpushedPastArgs` counts them;
  `allowedUnpushedLog` takes HEAD as the tip and a full commit ID as that commit, and
  nothing else);
- origin's default branch as the clone last fetched it and how many commits it has that
  HEAD doesn't (`Base`, `Behind`, -1 when unknown), through the `BaseReader` core gives,
  gitread's `OriginHead`, `TrackingBranch`, and `CommitsBetween`, in steps, each only once
  the one before found what it needs.

When the app made the worktree comes from its record (`CreatedAt`). Where several records
name one folder, `recordsByDir` picks the newest that holds the commit the worktree was
made at, or else the newest, for the listing's count (`fetchedHeads`) and `Details` alike.
`Details` never asks GitHub, takes no lock, and emits nothing.

`OpenTerminal(repoPath, path)` and `Reveal(repoPath, path)` check the request as `Details`
does, so a worktree whose folder is gone is refused with `ErrPrunable`, then open the
terminal chosen for launches in the worktree through the service's own `FolderOpener`, which
runs nothing there (without one, OpenTerminal fails with `ErrNoTerminal`, worded for the
window), or show it in Finder with `open -R` (off macOS there is no `Revealer`, and Reveal
fails with `ErrNoFinder`, likewise worded). The package never imports `internal/scriptrun`.
Core builds the worktree service before the terminal, which needs the launcher, which needs
the worktrees, so the service's `FolderOpener` is a core adapter (`worktreeTerminal`) filled
in once the terminal is built, before any work starts, which opens the folder as
`scriptrun.FolderScript`.

## internal/gitread

`internal/gitread/` is the read-only git that release, hooks, Repo scripts, and Deps share: an
exact-argv allowlist plus validating predicates (a branch, a tag, a commit, a `v*` pattern, an
absolute clean folder) that refuses anything else with `ErrGitCommandNotAllowed` before git
runs, and typed helpers over
it (the current branch, origin's default branch as the clone last learned it from its own
`refs/remotes/origin/HEAD`, an operation in progress, uncommitted changes, each dirty file with what
the index holds for it, unpushed commits, the commits between two, tags here and on origin, signing,
the hooks folder, a commit's tree, the newest commit to change a manifest, a blob: `OriginHead`,
`HookPaths`, `IndexLocked`, `Tree`, `LastChange`, ...).
Every command runs as `git -C <dir> -c core.fsmonitor=false -c submodule.recurse=false` with
`GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1`, and an empty
`GIT_ALLOW_PROTOCOL` (no read reaches a transport, so a partial clone's missing object is never
fetched through a URL the clone names), without `GIT_CONFIG` (which would hide the clone's
settings from the guards' reads), never touches `core.hooksPath`, and an `ls-remote` gets a
15-second limit and a batch-mode ssh unless the user's environment or own config picks one. A
`status`, which reads files and so would run their filter drivers, first reads the drivers
with `config --show-scope` and runs with every one the clone's own config defines turned off,
Git LFS's too, since git-lfs's clean writes into the clone (its clean, smudge, and process
emptied and required off),
and with `--ignore-submodules=dirty`, so it runs no status inside a submodule, whose own config
could name one. A diff against the worktree (`WorktreeDiff`, the run view's Diff) reads files
too, and runs the same way (`readsFiles`), with `--no-ext-diff` and `--no-textconv`, which every
diff passes, so no external diff or textconv the clone names runs either. An `ls-remote` likewise first reads, with `config --show-scope`, the settings
it weighs, and origin's URL with `ls-remote --get-url` (which asks origin nothing): a remote
helper (a `<name>::` URL after `insteadOf`, or `remote.origin.vcs`) is refused
(`ErrRemoteHelper`), since git falls back to an alias the clone defines; only the file, http,
https, and ssh transports run, as the user's own config and environment allow them, through
`GIT_ALLOW_PROTOCOL`, which beats every config; the clone's own ssh command, `core.askPass`
(git runs it even with `GIT_TERMINAL_PROMPT=0`), and credential helpers give way to the
user's own; an http or https origin the clone's own config reroutes (its TLS checks,
certificates, proxy, `remote.origin.proxy`, or resolved address) is refused
(`ErrCloneRoutes`), since the user's credentials, cookies, and headers for the real host
could go elsewhere; no cookie is saved, and a cookie file the clone names is dropped; and
origin is asked for git's own upload-pack with `--upload-pack` (git keeps the first
`uploadpack` it reads, so a `-c` can't replace the clone's). An `ls-remote` that fails after setting anything aside returns a `SetAsideError`
naming it, which `Message` (the words the Release sheet and Repo scripts' branch status
share for every gitread failure) gives as the likely cause and where to set the clone's own
settings instead: the user's global config, with an `includeIf` for the folder, which counts
as the user's own. Only the system, global, and environment (`command`) scopes are the
user's. Reading a clone runs no program the clone names (the user's own, such as Git LFS's,
still run). Its allowlist test asserts that no entry holds a verb `TestNoGitCommandChangesAClone`
guards. For the git commands that do change a clone, run elsewhere (Repo scripts' pull,
and worktree creation's fetches, `ls-remote --symref`, and `git worktree add`), it hands
over what they run with rather than running them: `RemoteSetup` (origin) and
`RemoteSetupFor` (a named remote, whose own URL and remote helper it weighs), for one that
reaches a remote, and `CheckoutSetup`, for one that writes files and reaches no remote.
Each reads the clone's whole config once, includes followed, with `config --show-scope
--show-origin -z --list`, and weighs every setting of the clone's own (the local and
worktree scopes, and whatever they include) by the three classes in `cloneconfig.go`, the
one place they are listed. Inert (`inertKeys`): what git init, clone, remote, branch,
submodule, sparse-checkout, and Git LFS write, the notes editors and git clients keep, and
git's many-files mode, which none of these commands reads as a program, a place to write,
or a way to reach a host: `core.{repositoryformatversion, filemode, bare, logallrefupdates,
ignorecase, precomposeunicode, symlinks, autocrlf, eol, sparseCheckout, sparseCheckoutCone}`,
`index.sparse`; `feature.manyFiles` and the settings it turns on through git 2.56,
`index.version`, `index.skipHash`, and `core.untrackedCache` (each allowed alone too), which
users of large repositories set for speed and which, with any value, only choose how git
writes the index in the clone's git folder (`feature.experimental`, whose group changes from
release to release, stays refused); `remote.<n>.{url, fetch, pushurl, tagopt, prune, promisor,
partialclonefilter, gh-resolved, gtserviceaccountidentifier}`; `branch.<n>.<any>`;
`extensions.{partialclone, worktreeconfig, objectformat, refstorage}`; `user.{name, email,
signingkey}`, `commit.gpgsign`, `tag.gpgsign`, `gpg.format`; `pull.{rebase, ff}`,
`fetch.prune`; `submodule.<n>.{url, active}`, `submodule.active`;
`lfs.repositoryformatversion`, `lfs.<url>.{access, locksverify}`; `rerere.{enabled,
autoupdate}`; `log.excludeDecoration`, `color.*`, `gui.*`; and `mergetool.*`,
`difftool.*`, `merge.tool`, `diff.tool` (and their gui ones), which only `git mergetool`
and `git difftool` read, and which the app never runs (`TestNoGitCommandRunsAMergeOrDiffTool`);
`remote.<n>.github-pr-remote` among a remote's notes; and what users and their tools write
for commands the app never runs, `push.*`, `rebase.*`, and `status.*` (no subsection; the
worktrees' own status is `--porcelain` with its own `--untracked-files`), `gitflow.*`,
`init.defaultBranch`, `merge.ff` (the pull's `--ff-only` beats it), and
`merge.conflictStyle`, none of which names a program, a path to write, or a host
(`core.editor`, `sequence.editor`, and `diff.*` stay refused). Neutralized (`neutralizedKeys`), as
the setup never lets the clone's own value reach git: `core.sshCommand`, `core.askPass`,
`credential.helper` (for every URL or one), and the cookie settings, which the remote
guard sets aside for the user's own in every setup (a checkout's Git LFS asks for
credentials too), a failure then saying which (`Setup.SetAside`, `SetAsideNote`), and of
the rest of `credential.*` only `username` and `useHttpPath` (`protectProtocol` and
`sanitizePrompt`, which a command-line setting can't beat for one URL, refuse, and every
setup sets both on again, `credentialHardening`); `url.*.insteadOf` and `pushInsteadOf`, as each remote
reached is weighed by the URL git will use; `http.*` and `remote.<n>.proxy`, whose
rerouting keys (`routeKeys`, a remote's proxy) refuse a command that reaches an http or
https remote, or one Git LFS may run in, whatever origin's scheme (`cloneRoutes`:
git-lfs reaches its server over https with the user's credentials), with
`ErrCloneRoutes`; `protocol.*`, which `GIT_ALLOW_PROTOCOL` beats; `core.hooksPath`,
`core.fsmonitor`, and `submodule.recurse`, which every setup sets again (husky writes the
first); and `maintenance.*` and `gc.auto`, as every setup turns automatic maintenance off.
Refused, before anything runs, with a `CloneConfigError` naming the settings and where
each is set, which `Message` words with where to set them instead (the user's global
config, with an `includeIf` for the folder): everything else, `refusedKeys`
(`branch.<n>.mergeOptions`) among them, so a hook (`hook.<n>.*` runs whatever it says), a
filter but Git LFS's own with exactly its own commands (`lfsSettings`, `--skip` ones
too), `core.worktree`, `include.*` and `includeIf.*` (an `includeIf` of
`gitdir:**/.git/worktrees/**` or `onbranch:` reaches only a worktree being made),
`core.alternateRefsCommand`, `gc.*` but `gc.auto` (`gc.recentObjectsHook` runs a program),
`lfs.customtransfer.*`, `lfs.standalonetransferagent`, `lfs.url`, `remote.<n>.{uploadpack,
vcs}`, a merge or diff driver, and any key a later git adds. `CloneConfig` runs that check
alone, for Repo scripts' branch status. The user's own scopes (system, global, and the
environment's) are never weighed, so an `includeIf` in the user's global config still
counts as theirs. Past the allowlist, the setup checks that git takes the folder as its
clone's top folder, comparing the two as files (`os.SameFile`, so a clone root whose case
differs from the disk's still matches; `ErrWorkTree`), weighs the remotes the command
reaches exactly as an ls-remote's guard does (a `RefusedError` naming the remote), and
returns a `Setup`: git as `GitFinder` finds it, the finder core hands the worktrees too
(`ErrNoGit` when it can't); an ls-remote's environment (for `CheckoutSetup`, and a
setup's `Checkout`, the same allowing no transport) with `GIT_DIR` and `GIT_WORK_TREE`
set to the clone's folders, which beat its config, and `GIT_NO_REPLACE_OBJECTS=1`, so a
replace ref can't have a checkout of the commit the launch checked write another's
files; and every setting, `Hardening` (`core.hooksPath` at an empty folder, so no hooks
folder's hook runs, and `hook.<event>.enabled=false` for every event git fires,
`hookEvents`, which git 2.56 documents as turning off every hook of the event, the
user's own config hooks too, but which a git whose config hooks predate that switch may
read as one hook's switch: the clone's own `hook.*` settings are refused regardless; no
fsmonitor or submodule recursion), `fetchHardening` (`core.alternateRefsCommand=true`,
so a fetch from a clone with alternates runs nothing to learn their refs, an empty one
making git try to run "" on every fetch; `maintenance.auto=false` and `gc.auto=0`, so no
fetch or merge starts maintenance), `credentialHardening` (`credential.protectProtocol`
and `sanitizePrompt` on), the remote guard's (the clone's own password program
and credential helpers set aside, no cookie saved), and any filter driver of the clone's
own but Git LFS's own turned off, passed through `GIT_CONFIG_COUNT` after any the
environment passes (`Setup.Settings` lists them), so the command's argv holds only its
own words. `LocalSetup` works out a `Local` for a command in a clone or a worktree that
reaches no remote, the worktree service's own status, removal, prune, unlock, and branch
deletion: a read's environment (no transport, no lazy fetch, no optional lock, no
replace ref), `Hardening`, `fetchHardening`, and every filter driver of the clone's own
off, Git LFS's too, with `Local.Refused` saying when the clone's own config holds a
setting a setup would refuse. In a partial
clone (`extensions.partialClone`, `remote.<r>.promisor`) either may fetch a missing file's
contents: its promisor remotes are weighed as a remote is, and the setup leaves
`GIT_NO_LAZY_FETCH` out and allows the user's transports (`Setup.Partial`). Only an
ls-remote has `RemoteTimeout`, whose end is `ErrRemoteTimeout`, which `Message` words as
origin not answering in time. `ValidBranch` is the branch rule a command's argument must
pass.

For Deps it reads a commit's contents from the clone's objects alone, never its worktree:
`TrackingCommit` (`rev-parse --verify --quiet refs/remotes/origin/<branch>^{commit}`, origin's
branch as the clone last learned it, peeled, so the answer is always a commit's ID, or '' when
the clone has no such ref); `Tree` (`ls-tree -r -z -l --full-tree <commit>`: every file of the
commit's tree, each subfolder's included, with its mode, its object's type and ID, and a blob's
size, as `TreeEntry`, kept as the caller's `keep` says; `TreeEntry.Regular` is a blob that isn't
a link, a blob the clone doesn't hold, which git lists with the size `BAD` as a partial clone's
unfetched blobs are, is kept `Missing`, and an entry of a shape it doesn't know is skipped);
`LastChange` (`log -1 --no-merges
--no-show-signature --no-mailmap --no-ext-diff --no-textconv --format=%ct%x01%s <commit> --
:(literal)<lockfile> :(literal)<manifest>`, the lockfile left out when there is none: the newest
commit to change either, its committer time read as UTC and its subject, as `Commit`, or none,
and in a shallow clone never older than its shallow commits); `Blob` (`cat-file blob <oid>`,
through no filter); `Shallow` (`rev-parse --is-shallow-repository`); and `FetchHead`, where git
keeps the `FETCH_HEAD` every fetch writes (`rev-parse --git-path FETCH_HEAD`, now among
`gitPathNames`), whether or not it is there. None of them runs a program the clone names: no
signature is checked, and no external diff or textconv runs. A manifest's and a lockfile's
paths have slots of their own (`slotLiteralManifest`, `slotLiteralLockfile`), validated by
`ValidManifestPath` and `ValidLockfilePath`, which Deps holds what it reads to (`treePath`: a
`go.mod` or `package.json`, or a `go.sum`, `pnpm-lock.yaml`, `package-lock.json`, `yarn.lock`,
`bun.lock`, or `bun.lockb`, relative and clean, no part empty, `.` or `..`, or starting with `-`
or `:`, valid UTF-8 with no control character or backslash, at most 1,024 bytes), and each is
passed to git only after `:(literal)`, so no wildcard or other magic is read in it; the lockfile
comes first in the two-path form, so that form without its last word is no other entry. A
tree's listing, a log, and a blob's contents are the repository's to make as large as it likes,
so each is capped (`outputLimit`: 64 MiB, 64 KiB, and a mebibyte and 64 KiB): gitread's own
runner, an `execpath.Exec` given that `Limit`, stops git once it prints more, holding none of
the output, and any other runner, as a test's, is held to it once it returns (`limitedRunner`);
either way the read fails with `execpath.ErrOutputLimit`, which `Message` words as git printing
more than can be read here. `DefaultBranch` is the rule Repo scripts and Deps share for a
repository's default branch, asking no one: the one the app stored, else the one the clone's
`refs/remotes/origin/HEAD` names (`OriginHead`), else the first of `main` and `master` the
caller's guess accepts (Repo scripts': the one the clone is on; Deps': the one the clone has of
origin's), else ''.

## internal/integrations

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

## internal/scriptrun

`internal/scriptrun/` runs the commands the integrations start (releases, hooks actions,
checks, scripts, a clone's pull, and a tool's own update), and a routine's agent in the
background (`KindAgent`: its own retention and log age, its stdin and folder, never on the
Runs page or told as a failure), and follows them, in the app or in the user's terminal, and owns
`script_runs`. The `Engine` is never bound, since a `Spec` holds functions: `Start` takes the
clone's lock first and fails at once when the clone is busy, runs the program through
`execpath.Start` in its own process group, writing straight into its log in
`<data>/script-runs` (four runs at a time in the app; a release never waits for a slot), and
ends it with a per-kind timeout or the stop ladder. A spec's `Before`, run under the lock and
a slot, may work the command out there, and its `Progress` says how far the run has got and
whether Stop is let through now: the bound `Run` carries it, `Stop` refuses with the kind's
words (`ErrStopRefused`), quitting signals a run only once its kind lets it stop (within
`QuitKillAfter`; one still held back is left going for the next start), and
`Config.Progressed` hears each change with the run's ID, which core sends the window as
`event.ScriptRunProgress` (`scriptruns:progress`), never the whole list's
`event.ScriptRunsUpdated`. A spec's `Recorded` is told the outcome only once the run's
end is recorded, so a record that fails tells nothing twice, and its `Released` once the run
has let go of its clone, so a page that reads the clone again finds it free. After
`Released`, once the run has let go of its clone and its slot, the engine's own
`Config.Failed` hears each run whose end is recorded as failed (one stopped at its time limit
included), of every kind, in the app or the terminal, started now or settled after a restart
(by a `Verifier` or by how it ended), and never one that couldn't start (its start's error
says so), one the user or the quit stopped, one a hang-up, Control-C, or SIGTERM ended (exit
129, 130, or 143, unless the engine sent it at the time limit), one interrupted, one that
succeeded, or one left `Unsettled`: once, since the record that ends a run changes only a
row still recorded as going. It gets a `scriptrun.Failure`, worded there ("Checks failed:
Checks · all files", the run's folder in place of a repository it lacks, with the run's
summary or "Timed out after 30 minutes."), which core (`runFailures`) hands the notify
`Dispatcher`'s `Gather` as a `run_failed` notification about no item: recorded at once, its
banner held `GatherWindow` (2 s) with those of the failures that follow, so a burst past
`MaxBanners` (Install and update all, a restart's reconcile) shows one summary, under the
same rules as every kind's banner; core's `Close` drops the banners still held (the history
keeps their entries) and cuts short a batch already showing (each batch shows within the
gathering's lifetime, which `Close` ends), waiting for it to end, before the database
closes. The
entry keeps the run's ID and its recorded start as RFC 3339 (`notifications.run_id` and
`run_started`, `Entry.RunID`/`RunStarted`, `Notification.RunID`/`RunStarted`, and
`event.RevealItem`'s for a banner's click), and the window opens it in Scripts → Runs only
once the run with that ID says it started then, since Clear history lets an ID be handed out
again; otherwise, or while the Scripts tab is gone, a toast says why. scriptrun never imports
notify (the repoguard boundary test). A spec's `Keep`,
which its `Before` may fill in, is written as JSON once `Before` has run into the run's own
`<id>.kept` beside its log, never into its record or the window's view; `Engine.Kept` reads it
back for a kind's `Verifier`, and it is removed once the run's end is recorded (it stays while
a record fails, for the next start) and with the run's other files. Its `Run` is a deferred
desktop worker: it reconciles the runs left from before a restart (a live group whose start
time matches is orphaned and followed again; one that is gone was interrupted; a terminal
run is told apart by its PID and exit files; every one is held under a placeholder lock and
registered before any is settled, and a kind's `Verifier`, registered with
`RegisterVerifier`, gives such a run its `Callbacks` (its output reader, progress, `Finish`,
`Recorded`, and `Released`, which a `Spec` embeds) again from its record's intent and what
it kept, built by the same helper as the kind's `Spec`, so it is read and verified as it
ends, as tracked work) and, as the app quits, stops every run in the app and records it
stopped, unless its `Finish` returns `Unsettled` (a release whose check of GitHub the quit
cut off), which leaves it recorded as going for the next start. `Active` is the desktop
shell's quit guard (`desktop.QuitGuard`): while a run goes in
the app, `Shell.ShouldQuit` holds a quit and the window asks (`event.QuitRequested`), until the
bound `Service.QuitAnyway` emits the Go-only `event.QuitConfirmed`, or `LetThemFinish`, offered
while every agent's run going may outlive the quit (`Run.OutlivesQuit`), emits it with
`Engine.LetFinish` set, so `StopAll` lets those runs go, still recorded as going for the next
start; `QuitAnyway` and `KeepRunning` clear it. The Service's
`OpenInTerminal` opens the user's terminal in a run's folder and runs nothing there, and
`ShowLog` shows a run's log in Finder, both by the run's ID alone. `InUse` tells an install a
tool is running, never counting a `KindUpdate` run, which is the tool updating itself (it
holds its copy's folder, not a clone, for up to ten minutes), and `Updating` names such a run
going now, one from before a restart included. `Amend` rewrites an ended run's summary and
its kind's own result, in one transaction with the read its change is given, as a release's
follow of its workflow does, and `Ended` lists a kind's ended runs, from which that follow
resumes. The history keeps 200 runs and 512 MB of logs, and cuts a long log to its
head and tail; Run again works the command out afresh from its source, never for a release
or a tool's update.

## internal/release

`internal/release/` is the Release page's service. `Candidates` lists every clone in the
repository locations whose origin is on GitHub, grouped as unreleased, up to date, no releases yet,
can't release here, or muted (`GroupMuted`: a clone of a repository muted as the list is read
is listed with its name and folder alone and never asked about, a row muted later keeps what
GitHub said and is served regrouped, through `arranged`, which also puts pinned ones first in
their group, and unmuting a row never read reads that repository alone, as a release's patch
does, `liftMuted`; the rules are read through its `RuleSource` on every call), from two rounds of batched GraphQL (`(*github.Client).FetchReleaseStatus`,
then `FetchReleaseCompare` for the repositories with a version, 15 to a query, adapted in core),
and reads each unreleased clone's state with `gitread`, never asking origin. It serves the
last list however old it is: the whole list is read again only on the page's **Read GitHub
again**, for a list from other locations, for one out of date, or with none yet (and
behind a list kept from a past start, below), never on a timer. A refresh that fails keeps
the last list, with its failure, until the user reads it again. A refresh tells the window
how far it has got, at most four times a second (`event.ReleasesProgress`: finding the
clones, each round's repositories as GitHub answers each batch, then the unreleased
clones), and each list that lands is kept in the `release.lastCandidates` pref with the
locations' `Key`. After a restart, the first `Candidates` serves that list at once, marked
`Kept`, while the `Refresher` (a desktop worker) reads GitHub again behind the page and
`event.ReleasesUpdated` says when it lands; a list from other locations is never served,
and a refresh that fails or is cut short keeps nothing. `version.go` works the next version
out exactly as magex does, tested against a table recorded from the real magex. A release
follows the review sheet's template: the `Gatherer` reads its `Facts` at once (magex through
a `ToolSource`, GitHub's status of the one repository with its newest commit's checks and
statuses, which the checks line names when they fail, in `GitHub.RepoStatus`'s one query
(the list's batches read the rollup's state and its per-state counts, under the alias
`counts`, never the checks one by one; still one point a batch), origin's refs with one
`ls-remote`, and the clone with `gitread`), and the pure `Decide` works out the sheet's `Offer`: every
check, every bump's version over the clone's and origin's tags, the Major gate, and the
argv, `push=true` (never the bare word) plus `expect=`, `json=true`, and `ff-only` only for a
magex whose `Definition.Features` has them. The bound `Prepare` takes the sheet's choice and,
within `FactsFor`, decides again on the facts the sheet was shown, so a choice asks GitHub
nothing; the bound `Start` hands `scriptrun` a release whose `Before`, under the clone's lock,
gathers the facts once (origin and the clone afresh, GitHub's from the sheet while under
`FactsFor` old) and decides there what magex runs, so a release that isn't ready, whose
version moved, or that the sheet should never have sent (a major without its typed version,
its go.mod box, or a magex with `expect=`, logged as a bug) never runs. The run's
`OutputReader` turns magex's lines into steps, warnings, and unrecognized lines (the run's
result leaves out the warning every release has, that the newest version tag isn't on HEAD;
the log keeps it, and the window's log view leaves out magex's JSON result line), and its
`Finish` verifies the release whatever magex's exit status: `ls-remote` for the tag, peeled to
its commit, then the clone's own tag, its branch, and any rebase in progress, into a `Result`
(released, not pushed, failed with `ExplainFailure`'s heading, or unverified). Only this
run's tag succeeds: origin's must point at the commit magex's JSON result names, or the
clone's tag that magex said it made; any other is someone else's (`Taken`). The commands it
offers to copy are shell-quoted. `release.Verifier`, which core registers for releases,
verifies one left from before a restart the same way. The reader's `Progress` turns Stop off
from magex's switch to the branch until its pull ends, and asks first once the tag exists; a
release that reaches GitHub is told once its outcome is recorded (the run's `Recorded`),
through its `Notifier` (core: the notify `Dispatcher`'s `Notify`, a `release_done`
notification about no item, and its banner). A release that ends doesn't put the page's
whole list out of date: its `Finish` hands the repository to the `Refresher` and emits
`event.ReleasesUpdated` at once, and the list reads as `Refreshing` until the patch lands.
The `Refresher` reads the repository alone again (its status, a comparison only when its
newest version isn't on its newest commit, and its clones' state, telling the window how
far it has got with `event.ReleasesProgress`), once any refresh under way has landed (one
read of the list at a time), patches those rows into the list, however old, which stays as
current as it was and is kept for the next start, and emits `event.ReleasesUpdated` again.
A refresh that began after the release ended has read the repository already, so the patch
asks GitHub nothing. A list it can't patch (none yet, other locations', out of date,
kept from a past start, one whose last refresh failed, without that repository, or GitHub
failing) goes out of date instead, as it does when the `Refresher` isn't running or stops
with a patch still queued, and a refresh under way when a list goes out of date lands out
of date too. A merge in the review sheet is patched in the same way: the review service
tells its `Merges` (core wires the `Refresher`'s `Merged`) the repository a pull request
merged into, and the `Refresher` reads it again, but only while the list could be patched
then, so a merge never puts the list out of date. The `ChecksFollower`, a desktop
worker as often as Settings → Refreshing → **Release checks** says (`refresh.releaseChecks`,
45 seconds by default, 0 off, woken on `event.IntervalsChanged`), reads again the rows
whose default branch's newest commit's checks run (`PENDING` or `EXPECTED`), of no
repository muted now: at most `followChecksMax` (15, one status batch and at most one
comparison batch, about two points) a round, the longest unread first, each left alone
while read within `followChecksFresh` (25 s, by a refresh, a patch, or the round before,
`Service.readAt`), and none whose newest commit is older than `followChecksFor` (2 h), so
a check never reported can't spend the budget for good. The one exception is a `PENDING`
row worked out while its commit was no older than that (`Candidate.workedAt`, stamped by
`candidateFor`): it gets one more read after, the first that can find it stuck (below), and
its row, worked out later, isn't followed again, stuck or still running. It asks GitHub
nothing while none runs, and follows only a list it could patch (`patchable`: read from
GitHub in this run, from the locations as set now, current, its last read not failed),
after the read of the list under way (`startReading`). It reads each row as a patch does
(`readRows`: status, comparison, and clone state, so a push the checks run for shows too),
but quietly: no `event.ReleasesProgress`, never `Refreshing`, and only a row that changed
keeps the list and emits `event.ReleasesUpdated` (the rows it read land in the list either
way, so each one's `workedAt` is current). A round GitHub fails is logged and leaves the
list current, never out of date; the next round tries again.

**Checks stuck on a run that never started.** A check run GitHub queues and never starts,
as an app's check with no workflow (Dependabot's config check, say), keeps the rollup
`PENDING` until the next commit, though the Actions page never lists it. `checksAt`
(`checks.go`), the one rule, used by `candidateFor` for the list and by `Gatherer.repo` for
the sheet, each with its own clock, reads such a commit's checks as `ChecksStuck` (`STUCK`,
never GitHub's own): the rollup is `PENDING`, so nothing failed; at least one check run is
`QUEUED` (`RepoStatus.Queued`, from `ReleaseStatus.HeadQueued`); no other check is unfinished
(`RepoStatus.Running`: a run in progress, waiting, or pending, a status pending or expected,
and a state this build doesn't know, so nothing really running is hidden); and the commit is
more than `followChecksFor` old. An `EXPECTED` rollup, a required check never reported, is
left as it is. The list reads a stuck row as ready (a green edge, no pulse, an amber ✓, and
`Candidate.Stuck` for *checks passed, 1 never started*). The sheet's checks line warns and
names the queued runs as the failing line names its checks (`checkNames`; the core adapter
fills `RepoStatus.QueuedChecks` from the sheet's one query), and its **View on GitHub** opens
the run's own page (`github.Check.RunURL`, the CheckRun's `url`, which only the release read
asks for; its `detailsUrl` can be the app's own site) when exactly one never started, else
the commit's checks page. A running line's **View on GitHub** opens the commit's checks page
too.

The bound `ReleaseResult` reads a run's outcome back as a typed `Result`, and the bound
`OfferAfterMerge` answers the review sheet's offer after a merge from local state alone: the
`integrations.offerAfterMerge` setting, magex, the default branch the Release page or the
inbox already holds, and a clone with that origin, from the page's list or else only the
folders inside the clone root and each folder location named as the repository is, then
each clone location (one git command each), never a scan of a location.
The `Follower`, a deferred desktop worker, follows each release on GitHub to *published*: a
release's `Finish` (its `Verifier`'s too) starts a `Follow` in its `Result` (its state,
`until`, and `polls`) when the Service has `Workflows` and `Records`, and its `Recorded` hands
it to the `Follower` once the outcome is kept. It polls a minute after a follow starts or
resumes, then every three minutes, at most `FollowPolls` (20) over `FollowFor` (45 minutes),
on the real clock for its limits and its waits alike (`clock.Sleep`; tests run it in a
`testing/synctest` bubble). Each poll is `(*github.Client).FetchReleaseProgress` (core's
`ReleaseWorkflow`): the tag's release, one GraphQL query through `postGraphQL`, and, until
it is published, the tag's own workflow runs, one REST request through `do` (`GET
.../actions/runs` by the push event, the tag as the run's branch, and the tag's commit, each
checked again on the answer), so the budget counts both. *Published* (`publishedAt` set, not
a draft) wins; while one of the tag's runs goes on, the follow goes on; once they all ended,
one that failed, timed out, or never started ends it as failed, named by its workflow; all
of them ending well two polls in a row with no release published ends it as
`unpublished`, and no run of the tag's by the third poll as `noWorkflow`. Each poll that
leaves the follow going is written into the run's record with `scriptrun.Engine.Amend` (an
ended run's summary and outcome, in one transaction with its read, and only while the
record still says this follow goes on). An end is settled the same way, written again a
few times a moment apart without asking GitHub again, and, if it still can't be, logged
and let go of for the next start's resume. *Published* (`Notifier.Published`,
`release_published`, opening the release) and *failed* (`Notifier.WorkflowFailed`,
`release_failed`, opening the run, or else the tag's Actions runs) are told only once the
record holds them, with a context the quit doesn't end; `unpublished`, `noWorkflow`, and
the limits get the result's line alone, no banner. As it starts, it resumes every release
whose record says it follows (`Engine.Ended`, which reads only a kind's ended runs), within
the same limits; a quit leaves the follow recorded for the next start.

## internal/hooks

`internal/hooks/` is the Hooks page's service. `Repos` scans, four at a time, every clone
in the repository locations with a go-pre-commit config inside it (a `.github/env/*.env` file, or
`.github/.env.base`): where git runs its hooks (gitread's `HookPaths`), its `pre-commit` hook
(missing, go-pre-commit's by its marker, or another tool's, named husky, pre-commit, or
lefthook from its text), and `ENABLE_GO_PRE_COMMIT` and the checks' switches from the config
files themselves. Only a regular file, or a link to one, is read, a config file and a hook
alike, and each is opened without waiting and checked again once open (`paths.OpenRegular`),
so a named pipe swapped in never hangs a scan. A hooks folder outside both the clone's git folder and its work tree is
shared, and the scan counts the clones that use it. Only go-pre-commit's `status` says whether
its own hook is current, a byte comparison, so it runs in a scratch repository of the app's own
that holds the hook's bytes, never in a clone, whose `.env` files go-pre-commit would load into
its environment, `PATH` included, before it runs git; its answer is kept per copy and per
hook's SHA-256, so status runs once for bytes no one judged. A scan runs apart from the
window's call that started it, within `ScanTimeout`, and `Scans`, a desktop worker, holds
every `Repos` call, its scan and its reads of the database included, to the app's lifetime:
the quit ends every one under way and refuses later ones, and the worker returns once they
have, so the shell waits for them before it closes the database. The scan is kept
five minutes, a clone an action or a check ran in is scanned again on the next read, and
`event.HooksUpdated` tells the window once the run has let go of the clone (the spec's
`Released`), so the reread finds it free and the run ended. `Install`, `Update`, `Reinstall`, `Replace`,
`Uninstall`, and `RunChecks` hand `scriptrun` a `KindHooks` or `KindChecks` run whose `Before`,
under the clone's lock, reads the clone again and refuses a shared hooks folder the request
doesn't confirm. An action's `Finish` reads what it did from the hook files, never from the
exit status alone (1.13 exits 0 when another tool's hook stops it); a checks run's compares
git's porcelain v2 status and each dirty file's SHA-256, taken before and after, so a fix to a
file that was already dirty shows. The `Before` keeps what it read beside the run's record
(`scriptrun`'s `Spec.Keep`, never its intent: an action's hooks and the copy of go-pre-commit
that ran, with its version's features; a checks run's config switches and its snapshot,
hashed or, past `maxHashed` dirty files, by size and time), so the `Verifier`, which core
registers for both kinds, judges a run left from before a restart as its `Finish` would,
once the shell's environment is read (`shellenv.Await`): the hook an action left unchanged is
judged only while the copy that ran is still the one in use, and a run that kept nothing
says it can't tell. A `Finish` whose step (git's status, go-pre-commit's) the quit cuts short
leaves the run `Unsettled`, for the next start; one whose steps all ended stands.
`InstallAll` installs or updates one clone at a time, each its own run in the app, and
skips another tool's hook, a shared folder, and a busy clone. The readers know 1.13.1's
text, recorded in `testdata/go-pre-commit-1.13.1`, and read `status --json` and `run
--json` once `Definition.Features` says the copy has them. The page carries the PATH
warning (the hook finds go-pre-commit only on the PATH or in `$(go env GOPATH)/bin`) and,
until `FeatureVerifiedTools`, the note that the checks install their own tools, folded
once a checks run ended (`hooks.checksRan`, which hooks alone writes). The bound
`ActionResult` and `ChecksResult` read a run's outcome back typed, and its `Rerunner`, which
core registers for both kinds, works Run again out afresh from the run's intent. A row's
**Open in terminal** (`OpenTerminal`) opens the terminal the runs open in at the clone and
runs nothing there, and its **Open the config** (`ShowConfig`) shows `.github/env/` (or
`.github/.env.base`) in Finder, each only for a clone the repository locations hold.

## internal/scripts

`internal/scripts/` is Repo scripts' service. While the Scripts integration is off (its own
switch, whose automatic is on while mage-x or go-pre-commit is), `List` returns the page
marked off and `Run` and Run again refuse. Otherwise `List` reads a clone's sources in the
page's order (the user's own scripts, then `t3.json`'s `scripts`, the
subset of `.vscode/tasks.json` the app can run, read through `jsonc.go`, and magex's
targets), as the `scripts.repoFiles` and `scripts.magexTargets` switches in
`integrationprefs` turn them on, and runs nothing in a clone: the files are read, magex's
built-ins come from `magex help:commands json=true` run in a scratch folder of the app's
own (a clone's `.github/env` files would override magex's environment, `PATH` included),
kept per copy, a list that failed too, until another copy or `List`'s refresh, and the
repository's own targets from parsing `magefiles/` with `go/parser` by magex's own rules,
kept until a file there changes. An entry the app can't run is listed greyed with the
reason in words: an editor variable such as `${file}`, `${command:…}` or `${config:…}`, a
password input, another task type, a character that hides what runs (a control character
but tab, DEL, or a bidirectional text control, in any command, argument, folder,
variable, shell, or input value), a task shell outside the POSIX family (sh, bash, zsh,
dash, ksh, mksh) given arguments to quote, a process task setting variables for a program
whose path holds an `=`, magex's own updaters (`update`, `update:*`, `version:update`, and
their aliases: Settings → Integrations updates magex), and the custom targets while
`magefiles/` holds a link to a folder. A pickString option shows its value beside a label
that differs (`Option.Shown`). A command that tags a version (`version:bump`,
`version:tag`, `git:tag`, `git:tagupdate`, `git:tagremove`, or a magex alias of one) points
to Release (`Anyway` runs it), checked again on the command as it will run; goreleaser's
`release:*` targets are plain built-ins. A command that upgrades the repository's
dependencies in the clone itself, on whatever branch it is on, leaving the change uncommitted,
points to Deps the same way (`Entry.Deps`, `ErrDeps`, whose words offer Deps or running it
anyway), since Deps does it in a fresh worktree, with a pull request: magex's `deps:update` or
`mod:update`, by name or a magex alias of one; `go get` with `-u` or `-u=patch`; a package
manager's own upgrade command, after any of its flags (npm's or pnpm's `update`, `up`, or
`upgrade`, Yarn's `upgrade`, `up`, or `upgrade-interactive`, and Bun's `update`, never its
`upgrade`, which upgrades Bun itself); and `ncu` or `npm-check-updates`, at any version, with
`-u`, `--upgrade`, `-i`, or `--interactive` (`guard.go`, each program by its name alone,
quoted, or at the end of a path; a script run by name, as `pnpm run update`, is the
repository's own). `deps:tidy`, which only tidies, stays a plain built-in, and
`deps:update` left the page's chips (`commonTargets`). `RunRequest.Anyway` runs either kind,
and a run's intent keeps it, so Run again runs a command run anyway the same way and refuses
one recorded without it. Both checks read a command's words as it runs joined into one line
(`bumps`, `upgrades`), inputs and aliases resolved, so a program and its arguments read as the
same command written in a shell would. A command from the repository's files asks before
its first run and whenever it changes (D13): its trust is the SHA-256 of the canonical
JSON of its effective form, `${workspaceFolder}` and `${env:…}` as written, a custom magex
target's holding every file under `magefiles/` by its bytes (only those: not `go.mod` or the
packages it imports, the known limit), recorded in `script_trust` under the clone's origin
with the display text trusted; magex's built-ins need none. `Run` hands `scriptrun` a
`KindScript` run whose `Before`, under the clone's lock, reads the source again from its
bytes, refuses a command whose hash isn't the one worked out as the run was asked for,
works the command out, refuses a version bump or a dependency upgrade not run anyway, then one
that isn't the one the user confirmed (or, with no confirmation, the one they trusted), and
only then remembers a confirmation they asked to keep, so what runs is what they saw. A
refusal that asks is a `QuestionError` whose `Question` holds the command with the chosen
inputs filled in; `desktop.MarshalError` sends any error's `Detail` to the window beside its
message. A repository's shell command (a `t3.json` script or a shell task, which assume POSIX
quoting, and which the app quotes for one) runs as `[$SHELL, -c, command]` when the login shell is
of the POSIX family (sh, bash, zsh, dash, ksh, mksh), else under `/bin/sh` (a task's
`options.shell` as written, with `-c` after its arguments as VS Code adds it), a process as
its argv, a magex target as `[magex, target, key=value…]`
with magex's `RunEnv`, and a `dependsOn` chain, or a part in another folder, as one `/bin/sh
-c` line of shell-quoted parts joined by `&&`. Its `Rerunner` works Run again out from the
run's intent and asks nothing, so it runs a repository command only while a trust record
under the clone's current origin matches it; anything else is run from the page, which asks.
The user's own scripts live in `user_scripts`, which it owns, in their order (positions
0, 1, 2, numbered again in the `store.InTx` of every create, delete, duplicate, or move).
The bound `Save` creates one (ID 0, at the end) or edits one, checking that the name and
command are there, on one line, with no character that hides what runs, that the scope
(`all`, `go`, or `repos`), the place (an `integrationprefs.RunsIn`, `''` for "As set in
Settings"; `validPlace` checks it and a run request's alike), and the icon (t3.json's
five) are known, and that a `repos` scope names at least one owner/name, kept once in any
case; `Delete`, `Duplicate` ("<name> copy", right after the original), and `Move` (up or
down past the nearest script shown in the clone named, so the move shows on that page)
change the order, and `RepoNames` lists the clones' GitHub repositories for the editor's
Chosen list and, through core, the launch defaults' targets (the Pinned and muted field
and Add defaults), reading origins four at a time and keeping each until the clone's
`.git/config` changes, forgetting a clone no longer listed, and starting no more reads
once the window cancels the call. `List` shows the ones whose scope includes the clone:
every clone, one with a `go.mod` at its root, or its origin among the chosen
repositories, in any case. They need no trust (D13), keep the hidden character guard,
point a version bump to Release and a dependency upgrade to Deps, and run exactly as
written, no `${…}` filled in, as `[$SHELL, -c, command]` in the user's login shell whatever
its family (fish and tcsh included, since the line is theirs and the app quotes none of it;
`/bin/sh` only for a login shell that isn't an absolute path), in the clone, with the run env
plus `Variables()`, which the page also carries (`Page.Variables`) for the editor:
`<SLUG>_REPO`, `<SLUG>_REPO_DIR`, and `<SLUG>_DEFAULT_BRANCH`, named from `brand.Slug`,
set alone in the terminal. The default branch, and so the page's *From branch* note,
is the one the app stored, else the one the clone's `refs/remotes/origin/HEAD` names
(gitread's `OriginHead`, which never asks origin), else `main` or `master` when the clone
is on one, else '' (`gitread.DefaultBranch`, the rule Deps shares, its guess here the branch
the clone is on). Their own place beats the setting, and a request's place beats both.
A run's `Before` compares the SHA-256 of what the saved script runs (its name, command,
place, and Ask before running) with the one worked out as the run was asked for, and
refuses one saved again in between ("changed as it was starting"). Run again works the
command out afresh from the saved script by its ID and refuses one deleted, no longer
shown there, or set to ask before running (`ErrAsksFirst`), which only the page asks:
Ask before running is the window's confirmation, which `Run` leaves to the user.
The bound `BranchStatus` tells the header beside the branch where the clone's branch
stands against the branch it follows on origin (`BranchState`: up to date, behind, ahead,
diverged, detached, no upstream on origin, git in the middle of something, unknown,
refused, not asked, or off while the Scripts integration is, which reads nothing, with a
worded note), read through its `BranchGit` (gitread) from the clone's own refs and one
`RemoteBranch` ls-remote of the followed branch alone (`ls-remote --heads origin
refs/heads/<branch>`, never every branch and tag), never a fetch, and only for an origin
on GitHub unless refresh is set (the user's click): another host's origin is
`BranchNotAsked` until the user checks, then served from the kept answer; a clone the pull would
refuse for its own config (gitread's `CloneConfig`, read locally, before origin is asked)
is `BranchRefused`; every note built from a gitread failure goes through `gitNote`
(`uierr.New` of gitread's words, read with `uierr.Message`); origin's answer is kept per
clone and branch for `OriginKept` (five minutes) unless refreshed, the clone itself is
read on every call, and an origin commit the clone lacks counts as behind. Only behind is
offered **Pull**, and a refused pull's error is the whole sentence the window shows (it
names the branch), a run whose record can't be read letting go of the button. The bound `Pull`
(`pull.go`) returns a `scriptrun.KindPull` run's ID (thirty minutes' limit), in the app,
under the clone's lock, of the branch the last status was worked out on, an upstream
`gitread.ValidBranch` passes: its `Before` refuses a clone now on another branch, one
whose branch follows none on origin, and one git is in the middle of something in, and
works out with `gitread.RemoteSetup`, asking origin nothing (the pull's own fetch is the
one round trip), what git runs with: a clone whose own config holds a setting gitread
refuses is refused, naming the settings; otherwise gitread's own git, an ls-remote's
environment (`GIT_TERMINAL_PROMPT=0`, the user's transports in `GIT_ALLOW_PROTOCOL`, the
user's ssh command or a batch-mode one, and `GIT_NO_LAZY_FETCH=1` but in a partial clone,
whose promisor remotes are weighed too), `GIT_DIR` and `GIT_WORK_TREE`, and every setting
(`gitread.Hardening`, no alternate refs command, no automatic maintenance, the clone's own
password program and credential helpers set aside, no cookie saved) passed through
`GIT_CONFIG_COUNT`. Its intent (`PullIntent`) is recorded once `Before` ran, with the
upstream and the branch's commit it read. It
runs `git pull --ff-only --no-rebase --refmap= --no-tags --no-autostash
--no-recurse-submodules --no-verify-signatures --no-stat --upload-pack=git-upload-pack
origin +refs/heads/<b>:refs/remotes/origin/<b>` as argv (which
`TestNoGitCommandChangesAClone` parses and holds to that exact list), nothing else in it, with
`LC_ALL=C` so git speaks the window's language, the environment handed to the runner
through the spec's `BeforeEnv`: the fetch updates that one remote-tracking ref, never one the
clone's own `remote.origin.fetch` maps. Stop is held back (`StopRefused`) for the whole
run, since git writes its output only as it exits, and so is a quit, which leaves it going
for the next start; only the kind's limit cuts it off, and a pull it cuts off is read
again (`cutOff`: the branch it is on, its commit against the one before, whether
`index.lock` is left, through gitread's `IndexLocked`, and what git is in the middle of)
and said to be cut off partway, the clone perhaps partly updated, with `git status` in a
terminal to see where it stands. Its `Finish` checks the clone is still on the
branch (a switch while git ran fails the run, saying git may have pulled into the other),
reads the branch back and words the summary (updated from one commit to another, how many
commits, already up to date, or git's last error line, with any of the clone's own
settings set aside, which may be why), and
keeps the commit git fetched as origin's for the next status, leaving when origin was
asked as it was, or forgets origin's answer when that can't be told. A pull whose end wasn't seen is settled from the branch alone:
moved on from the commit `Before` read is pulled. Core registers the `Verifier`
(`scripts.NewVerifier`) for pulls, so one left going at a quit comes back after a
restart with Stop held again and that same `Finish`. Run again pulls the same branch, its
`Before` checking everything afresh.

**A routine's run of one of the user's scripts.** `Routines` (`routines.go`, its own type,
never bound, since a `RoutineRun` holds a function) is how Autopilot's Run one of my scripts
reaches the user's own scripts: `Mine` lists them, each with its look (the hash a run's
`Before` compares, of its name, command, place, and Ask before running) and where it shows,
in words; `Run` works out the very spec `Run` does for one of them in a clone, with the place
always the app, whatever the script says, refuses it when its look isn't the one the routine
checked (`ErrChanged`), never runs one anyway (a version bump or a dependency upgrade is
refused, `ErrRelease` or `ErrDeps`, checked on the command as it runs, aliases resolved), and
sets its `Finish` (which keeps how the run ended, the user's Stop,
its time limit, or the quit) and its `Recorded`, which tells the routine the outcome once it is
recorded (`RoutineEnd`). Ask before running, which the window asks of the user for `Run`, is
Autopilot's to ask before it calls this one (**Schedules, the digest, the script step, and a
routine's week**, under `internal/autopilot`).

## internal/manifests

`internal/manifests/` is the Deps page's service. `List` reads every clone in the repository
locations whose `origin` is on GitHub (core's `hooksClones`, the Hooks page's: the roots, the
clones in them, and each one's origin through `localrepos.Inspect`) and whose repository the
user didn't mute, itself or its org (its `RuleSource`, core's `releaseRules`, the rules Release
reads; rules that can't be read are logged, and the last ones read stand), and lists each with
its Go modules' `go.mod` and its JavaScript packages' `package.json` files and how long ago
their dependencies last changed, stalest first by when each one's age counts from
(`Clone.AgeFrom`, below), those with none last, a pinned one marked (`Clone.Pinned`) for the
window to put first. The muted clones, those whose origin isn't
on GitHub or can't be read, and those read that hold no manifest are counted (`Page.Muted`,
`WithoutGitHub`, `WithoutModules`), never listed; a clone that couldn't be read is listed with
why (`Clone.Error`). Everything is read from the clone's own git objects at one commit, never
its worktree or a package registry, through gitread's allowlist
([internal/gitread](#internalgitread)), so reading a clone runs no program it names and reaches
no network, and nothing it reads is stored: what it reads is kept in memory alone, and all it
keeps in the database is the user's own marks up to date (below).

**The commit read.** A clone is read at origin's default branch as it last fetched it (gitread's
`TrackingCommit` of `refs/remotes/origin/<default>`; `Clone.Tip` says `origin/<default>`, and
`Commit` names the commit), so a clone that is behind, or on another branch, still shows what is
on GitHub, at the commit an upgrade starts from. The default branch is worked out by the rule
Repo scripts shares (`gitread.DefaultBranch`): the one the app stored (its `DefaultBranches`,
which core fills with the inbox's store), else the one the clone's `refs/remotes/origin/HEAD`
names, else the first of origin's `main` and `master` the clone has, that guess's own read kept
so it reads once. A clone with no such ref (one that never fetched the branch, say) or no
default branch known is read at HEAD, which its row says (`AtHead`, and `AtHeadWhy` in words).

**A clone's read.** One `git ls-tree -r -z -l --full-tree <commit>` (gitread's `Tree`) lists
every file with its object and size, and the picker (`tree.go`) keeps magex's signs
(`toolsigns.MageX`) and, outside every `vendor`, `node_modules`, and `testdata` folder at any
depth, each `go.mod`, `package.json`, `go.sum`, JavaScript lockfile, `go.work`, and
`pnpm-workspace.yaml`. Only a regular file counts (`TreeEntry.Regular`: never a link or a
submodule), and a manifest or lockfile whose path gitread won't pass to git
(`ValidManifestPath`, `ValidLockfilePath`) is left out. The manifests are read the shallowest
first, at most 200 (`maxModules`; a clone with more is `Truncated`). A Go module's lockfile is the
`go.sum` beside its `go.mod`; a package's is the nearest of `pnpm-lock.yaml`,
`package-lock.json`, `yarn.lock`, `bun.lock`, and `bun.lockb`, in its folder or one above, up to
the tree's top, the one its own manager writes first in each folder when its `packageManager`
field names one. The modules are read four at a time (`moduleParallel`), so a monorepo's
hundred histories fit the clone's time. A module's age (`ChangedAt`, with its commit's subject,
one line of at most 200 runes) is the newest commit, merges left out, to change its manifest or
its lockfile, one `git log -1` each (gitread's `LastChange`). Its manifest, read with `git
cat-file blob` (`Blob`) only when the clone holds it and the tree gives it at most a mebibyte
(`maxManifest`), gives a Go module's module path, `go`
directive, toolchain, and how many modules it requires directly and as `// indirect`, and a
package's name, package manager (its `packageManager` field's, else its lockfile's writer),
workspace patterns, and how many dependencies, dev, peer, and optional dependencies it names:
counts, never the names. `go.mod` is parsed by hand, line by line by go.mod's grammar, the
single-line and block forms alike (`gomod.go`: golang.org/x/mod is no dependency of the app's,
and a directive it doesn't read is skipped, its block too, so a newer Go's `go.mod` still reads),
and `package.json` with `encoding/json` (`packagejson.go`: a byte order mark read past, each
field kept raw, so one of a type npm doesn't use reads as unset, and at most 100 workspace
patterns); every word kept is one line of plain text, capped, and both parsers are fuzzed
(`FuzzParseGoMod`, `FuzzParsePackageJSON`). What can't be read is said in the module's `Problem`
(a manifest too large to read, one a partial clone never fetched, whose history is still read
but whose contents no read here fetches, a `package.json` that isn't a JSON object, a `go.mod`
that names no module, a log or a blob git refused, or one the clone's time ran out before). The
clone's tools (`Tools`: magex by its signs, and pnpm,
npm, Yarn, and Bun by the managers its packages use, pnpm's workspace file counting for pnpm)
and its workspace files (`Workspaces`, its `go.work` and `pnpm-workspace.yaml`) come from the
same tree. A shallow clone (`Shallow`, gitread's `Shallow`) has dates that stop at its shallow
commits, and when the clone last fetched (`FetchedAt`) is the modification time of its
`FETCH_HEAD` (gitread's `FetchHead` says where git keeps it, which `os.Lstat` looks at, never
into).

**What counts.** A clone is as old as its stalest module that counts (`Module.Uncounted`, set
as the module is read, and kept with it): a manifest under a `fixtures` or `__fixtures__` folder
(`UncountedFixture`), or else an `examples` or `example` folder (`UncountedExample`), at any
depth, by its path alone (`uncountedAt`, beside the picker's skipped folders), and a manifest read
that names no dependency of any kind (`UncountedNoDeps`: a `go.mod` with no `require`, direct or
`// indirect`, a `package.json` with no dependencies, dev, peer, or optional dependencies) are
still listed, and still read in full, but don't count. A manifest that couldn't be read (too
large, one a partial clone lacks, one that doesn't parse, a `go.mod` that names no module, a blob
git refused, or one the clone's time ran out before) still counts, unless its folder says
otherwise, since nothing says it names nothing. A clone's modules are sorted with those that count
first, then the rest, each part stalest first and those whose age isn't known last; `Counted`
says how many count, and `Oldest` is the stalest counted one's `ChangedAt`, "" when none counts.
A clone none of whose modules counts has no age: the window puts it in Fresh, for good, since
nothing in it can go stale, rather than in Stale with the ages unknown.

**Marks up to date.** The user may mark a repository up to date after checking it
(`MarkChecked(ctx, "owner/name")`, a row's or a drawer's Mark up to date; `ClearChecked` undoes
it), a triage of their own, as snoozing an item is. The marks are the one table the package owns,
`deps_checks` (`0113_deps_checks`: the repository, case-insensitive, and `checked_at`, one row
each, an unmark deleting it), written and read through its own sqlc queries
(`internal/manifests/internal/queries`) over the database core hands it (`Config.DB`; without
one nothing is kept), never through the store package. A mark is the repository's, so every
clone of it shares it. It is never part of a read of the clones: every page served, by `List`,
`MarkChecked`, or `ClearChecked`, reads the marks again (one query) and puts them on the last
read's page (`withChecks`): `Clone.CheckedAt`, and `AgeFrom`, the later of `Oldest` and the mark,
so a mark makes the clone fresh and it ages back into Aging and Stale a month and three after,
while a counted manifest that changes after the mark wins; then sorts the clones by `AgeFrom`.
A clone that couldn't be read has no `AgeFrom`, marked or not, so a read that failed stays in
Stale. Marks that can't be read are logged, and the last ones read stand (`lastChecks`), as the
pinned and muted repositories do; a mark or an unmark that can't be written fails with the
repository in its words, and a name that isn't `owner/name` with `ErrInvalidRequest`. Marking
reads no clone, so the page the window gets back moves the clone at once.

**What is kept.** What a clone's read found is kept in memory (`keptClone`) by its folder
resolved (`paths.Resolved`) and the commit read, with where its `FETCH_HEAD` is and its time, so
reading the clone again at the same commit runs no git beyond what found the commit: one
rev-parse when the app stored the default branch, and one more, origin's HEAD, when it didn't.
Whether it is shallow is read again only once it fetched since. A read in which a git read
failed in a way the next may not is not kept, but one git cut off for printing more than its cap
(`execpath.ErrOutputLimit`) is, since it would again at that commit; a clone no longer read
keeps nothing. The page itself is served as it is for `FreshFor` (five minutes) while the
repository locations and the pinned and muted repositories are as it was read under; `List`
with refresh set reads every clone again, as does any call once the page is older or either
changed.

**A read of the clones.** One runs per set of locations at a time (a `flight.Group`; a caller
that comes meanwhile gets its answer), apart from the window's call that started it, within
`ScanTimeout` (five minutes): a caller whose context ends stops waiting, and the read goes on and
is kept for the next. It reads four clones at a time (`scanParallel`), each within a minute
(`cloneTimeout`), its origin's read included, so one on a disk that stopped answering doesn't
hold up the rest; a clone whose time runs out keeps the modules it read, says of each other one
that it wasn't read, and is read again whole by the next scan, its read not kept. It tells the
window how far it has got (`event.ManifestsProgress`,
`manifests:progress`, through `internal/progress`, at most four times a second: finding the
clones, then reading them, counted). A read that fails keeps the last page, with why
(`Page.Error`), unless there is none to keep or the locations changed; one cut short keeps
nothing. `Scans`, a desktop worker (core's `ManifestsScans`), holds every `List` call and every
read to the app's lifetime: the quit ends every one under way and refuses later ones, and the
worker returns once they have, so the shell never leaves one reading behind it.

**The terminal and Finder.** A row's **Open in terminal** (`OpenTerminal(dir)`) opens the
terminal chosen for launches in a clone the repository locations hold (`localrepos.ByFolder`,
the clone named by its folder with `~`), through its `FolderOpener` (core's
`worktreeTerminal`), running nothing there, or fails with `ErrNoTerminal` without one. A
module's **Reveal in Finder** (`Reveal(dir, path)`) shows a clone's manifest or lockfile in
Finder through its `Revealer` (core's `open -R`; off macOS there is none, and it fails with
`ErrNoFinder`): the path must pass `ValidManifestPath` or `ValidLockfilePath`, checked before
anything is read, and the file must be in the clone's checkout now and inside the clone once
its links are resolved; Finder only shows it, so nothing reads or runs it, and a named pipe
there is shown, never opened. A row's Upgrade is a repository's launch
([internal/launcher](#internallauncher)). The repoguard boundary test keeps the package off the
store, the GitHub client, settings, the inbox, core, scriptrun, the launcher, the worktrees,
release, hooks, scripts, setup, integrations, the desktop shell, and notify: it reads the clones
through gitread and its own interfaces, which core adapts, keeps its marks through its own
queries rather than the store, starts no run, and asks nothing of GitHub or a registry.

## internal/update

`internal/update/` is how the app updates itself. It holds the manifest each
release attaches (`manifest-darwin.json`, `ManifestName`): its JSON (`Manifest`, `Encode`, and
`Parse`, which refuses one over 16 KiB, a field the format lacks, or anything after it), the
fixed lines its Ed25519 signature covers (`SignedMessage`: the format, the platform, the
version, the zip's name, its size, and its SHA-512, so none can change without the key),
`Sign` and `Verify` (every field held to its form, so none can pass for another: the version
a release's, as semver allows it, the zip's name exactly `FileName`, its size at most 200
MiB), `Digest` and `CheckFile` (a zip's size and SHA-512 against a manifest `Verify`
accepted), the keys (`ParseKey`: raw base64 or a PKIX PEM block, never a private key and
never an Ed25519 point of small order, under which a signature could be forged, which
`Verify` refuses too; `ParsePrivateKey` and `EncodePrivateKey`, PKCS#8), the release's asset
names (`AssetNames`, from `brand.Name`), and `ReleaseKey`, the public key the app pins (a
new one strands every copy that pins the old). Stdlib crypto only, no service, no HTTP (the repoguard
boundary test). `magefiles/release.go` builds and checks releases with it (`magex
app:package`, `app:verify`), and `magefiles/keygen.go` makes the release key (`magex
app:keygen`), so the packaging and the app agree on what a valid release is; while
`ReleaseKey` is empty, `app:package` and `app:verify` refuse, and a test pins the SHA-256 of
the constant, so changing the trust root takes two deliberate edits.
The `Engine` (never bound; `Service` is the window's face, `Worker` its timed checks) checks,
downloads, and installs the app's updates. What a build does is its `Flavor`'s, which core
works out from `core.Options` (`updateSource`): a source build is off ("Updates are off in
builds made from source."), its `Status` still telling the mode the user chose; a release
reads GitHub's latest release through core's `githubFeed` over
`(*github.Client).DownloadAsset` (the manifest from the latest release's download link, at
most 16 KiB, a 404, `github.ErrDownloadMissing`, reading as no release; the zip from its
tag's, `ZipURL`, at most 200 MiB) and trusts `ReleaseKey` alone, whatever `UpdateFeed` and
`UpdateKey` say, and is off while it is empty; a rehearsal reads a local folder
(`FolderFeed`, its failures worded with no path) and trusts its own raw base64 key (never a
PEM block, never a point of small order). The running version must pass `ValidVersion`, or
updates are off. A check reads the manifest within `ManifestLimit` (a minute) and takes it
only once `Verify` passes, its platform is the build's, its version a release's (no
prerelease) and newer than the running one (`internal/semver`); no release, or none newer,
is up to date, and an update waiting that the releases no longer offer is dropped, so
deleting a release's manifest withdraws it from every install (the lever for pulling a bad
release). Where the app can update itself is `Location` (`OK` only in `/Applications` or
`~/Applications`, in a folder it can write); elsewhere checks still run and tell, nothing
downloads on its own, and `Download` and `Restart` refuse with `ErrLocation` ("Move
Nilbox to Applications to update it."). `Location.Movable` says a move helps (a build that
updates, its bundle outside every Applications folder), and the bound
`MoveToApplications` moves it: the target worked out again (`/Applications` when writable,
else `~/Applications`, made when missing), a copy of the app already there, by its
`Info.plist`'s bundle identifier (`bundleID`, never a link followed), never replaced
(`MoveExists`, for `OpenInApplications`, which checks that copy's signature against the
running app's requirement and opens it), anything else there refused (`ErrTargetTaken`), and
otherwise the copy made and checked in a hidden folder of the move's own beside the target
(`stage`, `stagePrefix`, its marker and an empty bundle in it), through the `Mover` the shell
attaches (`AttachMover`), and only then renamed into the target, never over anything that
appeared there (`place`, `renameNew`: RENAME_EXCL on macOS), so a copy cut off or never
checked is never where the app goes, and a failed copy's removal (`discard`, that folder
alone) never touches what was there; a later move removes such a folder a cut-off move left
(`sweepLeftovers`: named so, marked before the copy began by `stageMarker`, the bundle ID and
the move's process, holding nothing else but the copy however far it got, and its maker this
run or gone by `procs.Probe`, or, when that can't be told, older than `leftoverAfter`); then
the Mover quits the normal way and opens the copy once the app has quit (`MoveMoved`).
`OpenInApplications` refuses a copy macOS would open from a hidden folder
(`Mover.Translocates`, `ErrQuarantined`), leaving its mark, before the check, and a check that
couldn't read the app's own signature (`ErrOwnSignature`) blames no copy. The bundle that runs
is never its own copy, by the folder it started from too (`isSelf`), so one dragged into
Applications while it runs still reads as itself. Every move holds the app's quit from its
start to its end (`Mover.HoldQuit`): its copy and check within `MoveLimit`, the steps around
them (the sweep, the unmarking, the rename, a failed copy's removal) moments more. One move
runs at a time (`ErrMoving`), its copy bounded by `MoveLimit` and never cut short by the
window's call, and none starts
while a restart into an update installs or finishes after a held quit, which goes first (`ErrUpdating`,
with `ErrFinishing` for the latter, each worded for the window); the bundle that runs is never
touched, and `ShowAppInFinder` shows it, but for a translocated one. With downloads on, the zip goes into
`<data>/updates/<version>/` (`Cache`, `appdata.Paths.Updates`) through a temporary file, hashed
as it arrives, stopped at the manifest's size, within `DownloadLimit` (10 minutes), its
progress told as `event.UpdatesUpdated` a few times a second (`internal/progress`); only a
zip matching its manifest is kept, with its manifest beside it, and every start checks each
kept update again, so one waiting is ready at once. A background download that failed is
remembered by its version and SHA-512 and tried again only at the next due check, never on
the ticks between (a click on Update tries, and so does Check for Updates, which forgets
the failure and has the `Worker` download at once). One operation (a check, a download, a
restart) runs at a time. A failure changes no state: it is the status's `LastError`, worded
with no raw error, path, or URL in it (`reachWords`, `downloadWords`, and the restart's own
sentences), and a check that reached the releases is the last check, a manifest it refused
included. The `Worker` (deferred work) settles the start first: it reads the outcome of the
last restart from `update.pendingInstall` (`JustUpdated` when the running build is the
version it installed, newer than the one it ran from, or, with no record, newer than
`update.lastRunRelease`, as after an install by hand; `InstallFailed` when it still runs the
version it ran from, with the runtime's helper log, `<TMPDIR>/wails-update-<pid>.log`,
copied to `logs/update-<version>.log` and worded from the messages it ends on, which tell
the helper's exit code, never repeating a path, and `Retryable` unless the update itself
was at fault: a payload failure, the new version failing its signature check or a staged
update not installable, fails the same way every time, so its cache entry is dropped and
its version and SHA-512 (`pendingInstall.sha512`) go into `update.uninstallable` (the
last five), which no check downloads, tells, or offers again, and `Download` and
`Restart` refuse (`ErrUninstallable`), while a newer version, or the same one with
another SHA-512, goes on as usual; any other ending (the app not quitting in time, the
swap's attempts or the copy aside failing, `open -n` of the new version failing to start,
which says nothing of the update, a missing or unknown log) keeps the cache for a
retry), then cleans up by exact name only (beside
the bundle, `<bundle>.bak` and the `wails-update-*` folders holding this app's update or
nothing; in the temporary folder, the last install's helper `.log`, any other process's
once `HelperWindow` old and only when its first line names this bundle as the target, and
the `.ready` files of those processes alone), then loads the cache. While the last install
is under `HelperWindow` (3 minutes) old and its helper's log hasn't ended on a final
message (one ending on the old app being put back counts once `<bundle>.bak` is gone),
the helper may still be restoring the old app, so the start settles nothing (no
outcome, no cleanup, the record kept) and the `Worker` settles again once it may have
ended; meanwhile the bound `Download` and `Restart` refuse with `ErrSettling` ("The last
update is still finishing. Try again in a moment."), and `CheckNow` checks but downloads
nothing. It checks a minute later (`FirstCheckAfter`), or at once on a Wake within that
minute, then, through `periodic.RunEvery` on a
15-minute tick, whenever six hours passed on the wall clock (`CheckEvery`), since timers
stand still while a Mac sleeps; a Wake (the settings' Go-only `event.UpdatesChanged`, or a
manual check that found a version to download) has it look at once. A timed check tells
each version once (`update.notified`, through its `Notifier`: core's `updateNotices`, an
`update_ready` notification about no item, which opens nothing, so a click brings the window
forward), once it is ready, or available where it doesn't download; Don't check makes none;
`CheckNow` works in every mode, downloads nothing in Don't check or Just tell me, and marks
what it finds told without a notification once it is available or ready, so a download it
started that fails still lets a later check tell of it. `Restart(confirmed)` refuses while
runs go in the app, unless confirmed, with a `RunsGoingError` whose `Detail` (`{"runsGoing":
[...]}`, through `desktop.MarshalError`) names them, each with whether it carries on through
the restart (`Run.Outlives`: an agent's run that keeps to a budget of its own, which the
restart lets go through `Runs.LetFinish` just before it lets the quit through, and stops
letting go should the restart fail after); it checks the zip against its manifest
again (`CheckFile`; a damaged or missing one is dropped, back to available, downloaded
again when downloads are on, and the refusal says so), backs the database up first
(`store.Backup`, through its `DataBackup`, reusing one it took under `BackupReuse`, 10
minutes, ago) and stops when that fails, stages the update through the `Installer` the
shell attaches (`Attach`), checks again, unless confirmed, that no run started meanwhile,
records `update.pendingInstall` (`{from, to, at, pid, sha512}`) only then, so a quit while
the update is staged leaves no record of an install that never began, and emits
`event.QuitConfirmed{Restarting: true}` before `Installer.Restart`, inside which the app
exits: the shell then lets every quit through, whatever runs go, since a held quit would
leave the runtime's helper to reopen this copy beside the new one. A failure before the
quit, or a `Restart` that returns an error, goes back to ready with `LastError`, clears
the record even once the call's context ended, removes the staging folder, and, once the
quit was let through, emits the Go-only `event.UpdateRestartFailed`, which has the shell
hold a quit while runs go again. A `Restart` that returns nil with the app still running
(a quit something held) left the helper standing by, which can't be called back, so the
restart is finishing (`StateFinishing`, "Finishing the update…"): the staging folder and
the record stay, a later restart is refused (`ErrFinishing`), and the Go-only
`event.UpdateQuitHeld` has the shell see the app out (`Shell.finishRestart`): every quit
stays let through, it asks to quit again every `desktop.FinishQuitEvery` (5 s),
`FinishQuitTries` (4) times, off the main thread, and, the app still running
`FinishWithin` (25 s) after `UpdateQuitHeld.HelperStarted` (stamped by the engine just
before `Installer.Restart`, since the helper's wait starts inside it, and at once when
that is already past), stops the shell in order (closing the database, waited for at
most `FinishStopWithin`, 3 s) and exits (`desktop.Config.Exit`, `os.Exit` but in tests),
before the helper's 30-second wait ends, logging each step. `HideBanner` (Later) hides the banner of the version found for a day, `ShowBanner`
lifts it, `AckUpdated` forgets the toast, and `ShowInstallLog` shows the kept log in
Finder. It writes only the `update.*` state keys (`update.lastCheck`, `update.notified`,
`update.bannerHiddenUntil`, `update.lastRunRelease`, `update.pendingInstall`,
`update.uninstallable`), never
`update.mode`, and imports no service, no store or prefs, and no HTTP: core adapts the
feed, the preferences (`Memory`), the backup, the runs (`Runs`, over the run engine), and
the notifications (the repoguard boundary test). `store.RestoreBackup` copies a backup into
a temporary file beside the database before it sets the database aside and renames the
copy into place, putting the database back when the rename fails; with no database there,
it removes a `-wal` or `-shm` left behind first. Every open removes the copies a restore
cut short left (`.<db>.restore-<digits>.tmp`, regular files five minutes old or more).

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

## internal/updateprefs

`internal/updateprefs/` declares the update setting, `update.mode` (`Mode`: `download`, the
default, "Download it and tell me"; `notify`, "Just tell me"; `off`, "Don't check"), which
settings alone writes (`SetUpdateMode`, emitting the Go-only `event.UpdatesChanged`) and
backups carry (`updateMode`), and the update package reads. It is a leaf that imports nothing
of the app's.

## internal/semver

`internal/semver/` reads semantic versions and compares them by precedence (`Parse`,
`Compare`, `Version.Prerelease`), held to semver 2.0 (no leading zero in a number, a
prerelease's numeric identifiers included), for the integrations' tool versions and the
app's updates alike. It is a leaf.

## internal/autopilot

`internal/autopilot/` is Autopilot, which watches the user's items and runs their routines.
Its root package keeps the routines and serves them to the window (**The routines, kept**,
below); beneath it is the routines core, five pure leaves. They read nothing,
start nothing, and keep no clock: the moment a decision is for comes with it. They import
only `item`, `repo`, `trust`, `sensitive`, `bump`, `clock`, `textutil`, the standard
library, and one another, in this order: `catalog` and `watch` at the bottom, `spec` over
both, `facts` over `catalog`, and `eval` over all three. `TestLeafImports` in
`internal/repoguard/deps_test.go` holds each leaf's direct imports to that list, so a leaf
can never reach the store, GitHub, a process, or a service.

**`catalog`** is the vocabulary: every name a spec stores is one of its constants, and the
builder renders what it lists. Its parts:

- the triggers (`Triggers`: each one's event, label, the kinds of item it fires for, what
  one occurrence is, and its options with their ranges and defaults);
- the fields (`Fields`: `<group>.<name>` IDs in seven groups, item, repo, pr, checks,
  files, dep, and history, each with its type, the kinds it applies to, its choices, how
  a list's patterns match, globs or `.gitignore`'s, its read cost, and its words for the
  sentence);
- the operators per type (`Ops`, with arity and negation; a list of choices takes only the
  set operators, `Takes`);
- the steps (`Steps`: each with the `Dispatcher` that runs it, triage, notices, github,
  agent, scripts, verify, merging, or review, whether it runs at once (`Local`) or follows
  the routine's mode (`AsksFirst`), and `Offered`, which every step and action is in this
  build: one a later catalog lists before its dispatcher is built stays unoffered);
- a schedule's styles (`ScheduleStyles`: each match on its own, or one digest) and how often
  it fires (`Everies`); the ways and methods Merge it merges (`MergeHows`, `MergeMethods`, the
  repository's own first), the ways Post the agent's review posts (`ReviewAses`), and what
  Tell me can say (`TellWhens`), each with its label and help, as `All()` serves them;
- the Only if outcomes, the tools presets (`MayPush`, `ReadOnly`), the agent step's
  settings (a budget in tokens among them, `budgetTokens`), the other steps' whole-number
  settings (`StepSettings`: `snooze.days`, `waitForChecks.settleMinutes`, and
  `waitForChecks.giveUpHours`), the limits, and their ranges, which the builder reads its
  bounds and defaults from rather than keeping its own.

`GitHubActions` lists the GitHub step's four actions (rebase, updateBranch, reply, labels),
each offered, with how far it can be taken back (`GitHubActionByKind` reads one); Mark the
GitHub notification done is a local step of its own. The catalog has no entry
for a close, approving workflow runs, `merge_bypass`, or the sheet's combined approve and
merge: `TestCatalogHasNoForbiddenAction` checks every ID it lists, and
`TestNoAutopilotCodeSpellsAForbiddenAction` every string literal the autopilot packages
spell. Merging and posting an agent's review are steps of their own, held in by their own
checks. Lookups by ID (`FieldByID`, `OpByID`, `TriggerByEvent`, ...) read one index built
once, since the evaluator makes them for every condition on every item. Three of the
catalog's names (Restore, Merge it and the merge method, the rebase action and method) are
spelled as git verbs; `TestNoGitCommandChangesAClone` lets their declarations in
`catalog/steps.go` through, and nothing else.

**`watch`** turns a search, as the builder writes it from fields (kind, authors, where,
involving, labels, notLabels, base, headPrefix, state, and qualifiers typed by hand), into
the GitHub query a refresh sends. Every query adds `is:open archived:false
sort:updated-desc` (without the sort, GitHub orders newest-created first, so "the 100 most
recently updated" would not hold); is scoped, by a `user:`, `org:`, or `repo:` qualifier
that isn't negated, or one whose value is `@me`; quotes its labels, as one comma list each
for any of them (`label:"a","b"`) and for none of them (`-label:"a","b"`), refusing a label
holding a comma; quotes any value holding a space of any kind, Unicode's included; passes
the head branch's prefix through exactly, quoted, since GitHub reads `head:` as a plain
prefix, refusing in a branch any space, since the base goes in unquoted; writes a bot as
`author:app/<slug>` (the bare slug matches nothing, so
`Normalize` writes `dependabot`, `renovate`, and `github-actions` as apps and `x[bot]` as
`app/x`); refuses a hand-typed qualifier it doesn't list, free text, and the
search's own (`is:` but public, private, locked, or unlocked, `sort:`, `archived:`, `in:`);
and is at
most 256 characters, refused over that with the hint to split it. GitHub doesn't error on
a longer query, it answers nothing, which is why the cap holds the whole query. "Your
account" (`@me` in Where) becomes `user:<login>` in `Build`, so a routine follows
whichever account is signed in, while `Check`, which `spec.Validate` calls, builds with the
longest login GitHub allows, so a search it passes fits any account. The query is written
in one canonical order from the normalized search, so it is its own dedup key
(`Query.Key`).

No value can smuggle in a qualifier. GitHub's search has no escape for a quote inside
quotes, so no value of any field (a label, an author, a place, a branch, a hand-typed
qualifier's value) may hold what `unsearchable` names: a quote, a backslash (which a
reader could take for an escape of the closing quote, letting what follows go live), the
fullwidth quote and backslash (which a reader folding Unicode's compatibility forms reads
as those two), a control character (a newline and a tab among them), or a line or
paragraph separator. An apostrophe is fine, since GitHub quotes only with double quotes.
Each field refuses such a value with its own problem (`labels[1]`, `base`, `more`, ...),
and so does the build itself: every token carries the field it came from, and `build`
checks each one again as it writes the query (`token.check`), so a check a field missed
fails the query rather than sending it. The writer (`token.String`) also takes the quotes
and backslashes out of every value it writes (`quoteless`), whoever calls it, so not even a
token that skipped both checks can end its quotes early. `FuzzTokenString` reads what it
writes as GitHub's documented syntax does, and as a reader that takes a backslash for an
escape would, and finds one qualifier, the token's own, between the query's neighbors;
`TestHostileValuesAddNoQualifier` does the same for each field through `Build`.

**`spec`** is a routine's definition, one JSON document with a `version`: the watch's
sources (the inbox, by the reasons items are in it, or a search), one trigger with its options,
the conditions (`all` or
`any`, with one level of groups), the steps in order, each optionally carrying `onlyIf`,
the limits, the routine's own banner switches, and the tools it requires. The mode, the
on or off switch, the name, and the account are its row's, never the spec's. An option or
a limit left out takes its default (a nil pointer), so the builder can tell a value set
from one inherited. Format version 1 holds every step kind (notify, pin, snooze, dismiss,
restore, markDone, github, agent, waitForChecks, merge, postReview, tellMe), so a later
build adds the code that runs a step, never a format change, but one: Run one of my scripts
(`script`, the user's script by its ID and the name it was picked under) came later, as a key
an older build refuses, so it reads such a routine as broken; `TestSchemaGolden` pins every
key and every closed set of values.

- `Decode` is strict: it walks the document beside the `Spec` type and holds every
  object's keys, byte for byte, to its struct's JSON names, since encoding/json would
  match a key ignoring case, Unicode's folding included (`Trigger` beside `trigger`, or
  `ſteps` for `steps`); and it refuses a key given twice (which encoding/json would read
  as the last), a value of the wrong type, anything after the document, nesting past 16
  levels, and more than 64 KiB. `DecodeExact[T]` is the same strict walk for any type, which
  Describe it's [kit](#internalautopilotkit) reads its files and proposals with.
- `ClosedSets` gathers every closed set of values the format holds (the watch's, the
  trigger's, the conditions', the steps', and the limits'), each from the catalog's, watch's,
  or item's own lists, never a copy: `Validate` checks every value against them, kit's schema
  enumerates them, and `TestEveryNamedStringTypeHasAClosedSet` holds every field of the format
  whose type is a named string type to one, so no vocabulary enters the format without its
  values.
- `Normalize` trims every text, sorts and dedupes every list that is a set (text compared
  ignoring case, the first spelling kept), writes a choice as the catalog spells it, keeps
  a condition written the same way twice in one list once (its value compared exactly, so
  the regexes `\d` and `\D` stay two), and keeps the user's order of the sources, the
  conditions, the steps, a GitHub step's actions, and the fallbacks.
- `Validate` returns every problem, each naming its part and field (`Problem.Path`:
  `watch.sources[0].search.where`), and holds the steps to their order: one agent step at
  most; Wait for the checks only after an agent step in the background that may push, and
  never only if it didn't push; Merge it only after a Wait for the checks that runs only if
  the agent pushed, so the pass it follows is on the agent's own commit and never a flaky
  check run again, with `onlyIf: "passed"`, or, in a spec with no agent step and no wait
  (`Spec.Agentless`), on the trigger `checks_passed`, once per head commit, with no Only if
  and no rebase, update of the branch, or script step before it (`agentlessMerge`, from
  `validator.event`, `oncePer`, and `before.changesBranch`); `allowBypass` and
  `allowSensitive` only there, with `how: "whenMergeable"`, and `allowSensitive` only with
  `allowBypass` (`pastRules`; `TestOnlyMergeItNamesABypass` holds them the format's only keys
  naming a bypass); Post the agent's
  review only after a read-only agent step in the background; Allow approving only with
  `as: "verdict"`; only a notice after an agent step in the terminal, whose end the app
  can't see; and an Only if only on an outcome an earlier step ends with (the checks', the
  latest Wait for the checks'; the push and the verdict, the agent step's).
- `Load` reads the version first, so a newer build's spec says it is newer rather than
  unreadable, then decodes, normalizes, and validates. A spec it can't load returns a
  `*BrokenError` (`ReasonNewer`, `ReasonUnreadable`, `ReasonInvalid`), so its routine is
  broken, not fatal; an older version is migrated in `migrate`, which has nothing to bring
  forward yet. A routine stored before a rule tightened (a label holding a fullwidth
  quote, say, which `watch` refuses) loads this way, broken as invalid, its problem
  naming the field for the builder to show, its spec never rewritten, and no query built
  from it.
- `Sentence` writes the one sentence every place shows, from `Phrase` for each condition,
  with `PromptNames` naming a starter as the library does; `TestSentenceGolden` pins each
  template's. A value it shows (a label, a condition's value, a search's author, place, or
  branch) goes in bare when it is one plain word, and otherwise quoted and escaped as Go
  quotes a string (`strconv.Quote`), so a value's own quote or backslash can never
  unbalance the sentence's quotes, and a newline or a bidirectional mark shows as its
  escape. The hand-typed qualifiers go in as typed while their quotes pair up and they
  hold no backslash and nothing that doesn't print, and otherwise quoted whole.
  `FuzzSentenceQuotes` holds every quoted stretch to read back, as Go reads a quoted
  string, as the value it quotes.

**`facts`** holds one item's values for the evaluator, built by the pure `Build` from a
plain `Row`, which the inbox's query fills: the item, its repository, its triage, and, once
read, its details, its head commit's check names, and its changed files, each part nil
until it is. Each value is known, unknown (with why, and the `Need` the engine would read to
know it: details, checks, or files), or not applying to the item (a pull request's field on
an issue). A list read only in part is known and `Partial`. Check names and files count only
when read at the head commit the details name. A check's name is `workflow / job` as GitHub
shows it (an app's run, such as CodeQL's or OSV-Scanner's, and a status have no workflow),
each name once though GitHub lists one per event that ran it, failed when any of its runs on
the head commit failed; the checks are done, and so can settle, only once every one has
finished, at the newest completion time (a skipped run may say it completed before it
started, so start times never count). The dependency fields apply only to a pull request
Dependabot or Renovate opened (by its author's type and login), read from its title by
`bump.Parse` and its ecosystem from its head branch by `bump.ParseBranch`; a title that
doesn't read as an update leaves them unknown. The author's trust is unknown while their
association isn't read, since they may be a member, but for a fork's pull request, an
outside contributor's whoever opened it, and a bot's, which the trusted list decides.
Every text is made one line and capped (`textutil.OneLine`).

**`eval`** decides. `Evaluate(spec, facts, history, now)` returns a `Decision`: `Match`
(yes, no, or unknown), the `FireKey` while the trigger holds, the `Trace` (the trigger's
step, then each condition that ran, with what it compared and the actual value), and the
`Needs` of an unknown decision. `Prepare(spec)` works out once what deciding the spec takes
from the spec alone (the order each list of conditions runs in, each condition's path and
phrase, the trigger's words), and its `Plan.Evaluate` decides an item byte for byte as
`Evaluate` does, traces included; `Evaluate` prepares the spec for its one decision, and a
caller deciding a routine on many items, as the engine's cycle does, prepares it once.
Conditions are three-valued: one on a value not known yet is unknown whatever its operator,
its negation too; one on a field that doesn't apply is no;
a partial list's find is sure and its miss unknown; an "all" operator over an empty list
is no, never vacuously yes (a pull request that changes no file doesn't have every changed
path under docs/). A regex or a path pattern is compiled once, in a cache every evaluation
shares, not on every item of every cycle. `All` is no at its first no, `Any` yes
at its first yes, and a list stops there; the order conditions run in is their read cost,
then what each says, never where it sits, so shuffling them never changes a decision. The
`History` is the ledger's: the routine's tries and last outcome, the head it first saw, the
heads routines pushed, the comments it last counted, and a click's token. Fire keys name
the event and the occurrence (`checks_failed:commit:<oid>`, `labeled:label:<name>`,
`new_comments:comments:<n>`, `schedule:<date>`, `manual:<click>`, ...), and the routine's
own once-per (item, head commit, or day) replaces the trigger's. A key holds only what is
known: Falls behind waits for the base branch to be read, so one occurrence never fires
twice, without its base and then with it. Checks fail and Checks pass wait for the checks
to settle, the newest completion at least the settle time ago. A trigger that doesn't hold
yet but will by itself says so (`Decision.Pending`, its `When`, its `At` when the clock
alone decides it, and the `FireKey` it fires for then, with the routine's own once-per):
Checks pass while its checks run (none failed, none waiting on a maintainer) or settle;
Checks fail, waiting for every check, once one failed while the rest run or settle; a
schedule due later today; Goes quiet within a day of its spell (`soonQuiet`). A trigger
that waits on an event no one can foresee is never pending. `Plan.Conditions` decides the
conditions alone, as `Evaluate` runs them, and `Plan.ConditionsWhenDue` decides them as the
item will stand once a pending trigger holds, with the facts it makes so (`Pending.Assume`:
Checks pass waiting on running checks makes them all passed, `allPassed`; Checks fail waiting
for the rest makes them finished and failing, `allFinished`), which the engine asks of a
pending decision. The property tests (`FuzzShuffledConditionsDecideAlike`, `FuzzNegationOfAnUnknownIsUnknown`,
`FuzzNeverYesOnAnUnknownItNeeds`) run over generated specs and facts; the last checks that
reading what was missing never turns a yes or a no into anything else.

### The routines, kept

The root package owns `autopilot_routines` (migration `0100_settings`): one row
per routine, its spec one JSON document (`spec.Encode`, compact) beside its `spec_version`,
and, as columns, never spec, its name, description, switch (`enabled`, with `enabled_at`
while on), mode (`dryRun`, `ask`, or `auto`, checked in Go, so a later mode needs no
rebuild; one this build doesn't know reads as Dry run), the account that made it
(`account_login`, NOCASE), the template it came from, its place (`sort_order`, 0, 1, 2, and
so on, numbered again in the transaction of every change that adds, deletes, or moves one),
its last error, and the broken reason it was last written with. A delete is soft
(`deleted_at`, and a check keeps a deleted routine off), so the history that names a routine
keeps its name and sentence. Names are unique, ignoring case in any alphabet, among the
routines not deleted: the [draft leaf](#internalautopilotdraft)'s `NameKey` folds a name in
Go (each rune to the smallest of its `unicode.SimpleFold` orbit, so two names share a key
exactly when `strings.EqualFold` says they match), every comparison of names uses it, and the
row stores it as `name_key` under a partial unique index, since SQLite's NOCASE folds ASCII
alone; a worded check runs before the index would refuse. Its queries are in
`internal/autopilot/internal/queries`, generated by sqlc.

**The bound Service** (`List`, `Get`, `Save`, `Delete`, `Duplicate`, `SetEnabled`, `SetMode`,
`Catalog`, `RoutineNames`, and Easy mode's `Templates`, `FromItem`, `DraftReadiness`,
`DeclineGraduate`, `Export`, `PreviewImport`, and `Import`, **Easy mode**, below) never returns a
nil list, and words every refusal with `uierr`.

- Every read loads each spec with `spec.Load`: one that won't load (`spec.BrokenError`:
  newer, unreadable, or invalid) is listed as broken, with `Broken` in the window's words
  (*Made by a newer Nilbox: update to edit it*) and an invalid one's `Problems`, normalized
  so its lists are empty rather than null, and the list carries on with the rest. No read
  writes anything, and nothing rewrites a broken spec: such a routine can't be switched on
  or change its mode, a newer build's can't be saved over, and only switching it off,
  deleting, duplicating, and renumbering the list touch its row.
- `Save` normalizes the draft's spec, encodes it, and loads it back, so the spec stored is
  checked as every load checks it; problems come back as a `ProblemsError`, whose `Detail`
  (`{"problems": [...]}`, through `desktop.MarshalError`) lets the builder show each where it
  is. A new routine starts off, at the end, in the draft's mode or else the one Settings says
  new routines start in (`Prefs`, core's adapter over `autopilotprefs`; Ask first to begin
  with), and belongs to the account signed in (`Accounts`, the inbox's viewer login). One
  saved before keeps its switch, place, and template, and its mode unless the draft names one;
  a routine of no account (made before any account signed in, or brought back from a copy or
  a backup, which name none) becomes the editor's. It also refuses, as a `ProblemsError`, a
  step naming a prompt the library doesn't have (by its starter's key, or its nickname, ignoring
  case), a review reply where an agent's prompt goes or the other way about, or a prompt that
  doesn't apply to every kind of item the routine acts on (its watch's kinds that its trigger
  fires for), as a prompt for repositories applies to none, each at the step's prompt field
  (`Library`, core's `promptLibrary` over the
  prompt library); with no library, or one that can't be read (logged), it refuses none.
  `SetEnabled`, `Duplicate`, a restore, and an import don't check them again; the engine does,
  right before each start (`promptHold`, the same `draft.StepPromptProblems`), so a prompt deleted,
  made a reply, or narrowed to the other kind after the save keeps the run from starting
  (`blocked: not_ready`, *Its prompt can't run: ...*). A prompt a routine names can't be deleted
  (the prompt library's `RoutineSource`, core's `routinePrompts` over `PromptFollower.Uses`),
  nor made a prompt for repositories (the editor's `checkRepoSwitch`), and renaming one
  rewrites its nickname in every routine that names it by that nickname
  (`PromptFollower.Renamed`, told by `prompts.Config.Renamed`, in one transaction, each spec
  compared-and-set on the one read); a starter's key follows its prompt on its own.
- A step's **Where** must be a place this build runs an agent in (`catalog.Places`,
  `Offers.Places`: the background and the terminal, both offered): one it can't is a problem
  at `steps[i].agent.where` (the draft leaf's `Offers.placeProblems`), in `Describe` and `Save`
  alike, never a Dry run hold, so a routine is never saved to wait for a later build; a
  routine stored before with such a step isn't ready on this Mac (its `place:` need,
  **Readiness**, below).
- `Describe(draft)` reads a draft as the builder edits it, through the draft leaf's `Read`,
  whose checks `Save` shares (`draft.Problems`), saving nothing and asking GitHub nothing:
  the spec normalized, encoded, and loaded as `Save` does, its problems (the spec's,
  then the prompts'), each with its part and field, the steps this build runs only in Dry run,
  its warnings (a Pin, Snooze, Dismiss, or Restore over a watch that is only searches; on
  Autopilot, an agent in the terminal or with the profile's own permissions, unless the routine
  is held in Dry run), and, only while it has no problem, its sentence, so the builder keeps
  the last one that read.
- A routine whose steps include a kind the catalog doesn't offer (`Step.Offered`, or a
  GitHub step with an action `GitHubActions` doesn't offer) runs in Dry run alone:
  `Routine.DryRunOnly` names the kinds, `Save` stores it as `dryRun` whatever the mode, and
  `SetMode` refuses Ask first and Autopilot for it with a sentence naming the steps. This
  build offers every step and action, so no routine is held there; the rule is the seam for a
  step a later catalog lists before its dispatcher is built.
- `List` marks a routine of another account than the one signed in (`OtherAccount`; logins
  compare ignoring case, and with no one signed in none is another's). It doesn't run.
  `List` also gives each routine its `Figures`, for its status line, from three reads of the
  ledger (the grouped `ListRoutineFigures`, then `ListRoutineLastRuns` and `ListMatchHeads`
  for the match each routine ran last): how many matches it recorded today (since local
  midnight in the clock's zone), real ones and a Dry run's apart, how many wait for the user,
  and when it last ran: when its match that ran (done, or closed after one of its steps ran;
  in Dry run, would have) that closed last closed, which is when it ran, however long it waited (for the user, or under Pause)
  after it was recorded, and whether that was a Dry run's, so a newer one that waits or was
  skipped never reads as *ran*. The counts take the matches
  `ListLatestItemMatches` counts: open, closed as done, skipped, or dismissed, or closed after
  one of its steps ran (a step whose status in `steps` is `done`, read with `json_each`: a
  reply sent, then a merge no longer offered); a baseline, and a match that closed before it
  acted, count in none of them.
- `RoutineNames` gives every routine's name by its ID, the deleted ones included
  (`RoutineName.Deleted`), so the window still names a routine the history, a mark, or a chip
  names after it is gone.
- `Duplicate` makes an off copy right after the routine, named "<name> copy" (then "copy 2",
  and so on), the account signed in's. `SetEnabled` keeps `enabled_at` when switching on a
  routine that is on. `Catalog` is `catalog.All()`.
- `Sentence` names a starter prompt by the nickname of the user's copy of it (`Names`, core's
  adapter over the prompt library).

**The copy and the restore.** After every change to a routine, the Service writes its copy,
`autopilot.json` beside the database (`File`, `appdata.Paths.Autopilot`, user-only through
`appdata.WriteFile`), every routine not deleted in the export format:

```text
{ "format": "<slug>-autopilot/1", "exportedAt", "app",
  "routines": [ { "name", "description", "mode", "enabled", "template", "spec": {…} } ] }
```

`DecodeBundle` refuses another format and a newer version (`ErrNewerBundle`), and takes a
field a later build adds. A bundle never holds an account, or a routine's history. An
export the Autopilot tab writes may also hold `prompts`, the prompts its routines name by
nickname in the prompts' own format (**Easy mode**, below); the copy and a backup never do.
`Restore` runs only while `autopilot_routines` holds no row at all, the deleted ones
included, so a routine the user deleted is never brought back: from the database set aside
when it has the table (`store.OpenSnapshot`, tolerant of missing columns, keeping each
routine's account), and otherwise from the copy, every routine not deleted, in its order,
in one transaction, each switched off, its mode as the source says (a step this build can't
run keeps it in Dry run), its spec stored as `Save` stores one (normalized, then encoded)
when it loads, and otherwise compacted but as it was, so a newer build's stays broken and
unrewritten; a spec that isn't JSON at all is left out, and two of one name are told
apart by a number. Core (`internal/core/kept.go`) runs it after the prompts and the launch
defaults, so the prompts a routine names by nickname are back first, and writes the copy at
startup only when it is missing or routines were just restored, so a loss in the database
never overwrites it. The window's restore notice counts them (`appinfo.Restored.Routines`).

**Backups.** The Autopilot section (`backup.SectionAutopilot`) is the export format,
through `NewBackupPart`, a type of its own that core adapts in `adapters_backup.go`, never a
method of the bound Service. `PlanBackup` leaves out, with a note, a routine with no name, a
second of a name, and one whose spec won't load (as a newer build's); a mode this build
doesn't know takes the mode new routines start in. Routines are matched by name
(`draft.NameKey`): one whose definition, description, and mode are the file's is left as it is, on
or off, under either mode; any other is saved over and switched off, and one this Mac lacks
is added, off, at the end (Merge) or in the file's order (Replace, which also deletes,
softly, every routine the file doesn't name). A routine here that a newer build wrote is
never saved over or deleted by an import, under either mode: it is left as it is, with a note
(*made by a newer Nilbox: left as it is*), as `Save` refuses to save over it. A spec that
loads is stored normalized, as on a restore. `BackupDone` writes the copy again. The window
lists the category as **Autopilot routines**, beside the others.

**The settings** are `internal/autopilotprefs`'s, a leaf that imports only `prefval`:
`autopilot.enabled` (Autopilot (beta), off: see **Autopilot (beta)**, below),
`autopilot.paused` (off), `autopilot.defaultMode` (`ask`), `autopilot.agentsAtOnce` (2, 1 to
3, so one of the run engine's four slots stays free for the user's scripts),
`autopilot.runsPerDay` (20, 1 to 200), `autopilot.actionsPerDay` (500, 1 to 2,000),
`autopilot.actionsPerHour` (120, 10 to 300, under the write gate's 400 an hour),
`autopilot.dailyCap` and `autopilot.dailyCapUnit` (0 for none, else 1 to 500 dollars or
100,000 to 50,000,000 tokens; `usd` or `tokens`), `autopilot.keepAwake` (off),
`autopilot.holdOnOutage` (on), `autopilot.catchUpHours` (24, 1 to 168),
`autopilot.historyDays` (90, 7 to 365), `autopilot.logDays` (30, 1 to 90, never more than
the history), and `autopilot.mergePastRules` (Let routines merge your own pull requests past
the rules, off: see *Past the rules* under Merge it, below). `Decode` reads a value out of range as its default, a cap that doesn't fit its
unit as none, and logs longer than the history as long as it. Settings alone writes them:
each `SetAutopilot…` setter reads them and writes the change in the transaction that reads
the settings back, refuses what `Config.Check` refuses with a worded sentence, writes only
the keys that change (`Writes`: a setting never touched keeps following its default), and
emits the Go-only `event.AutopilotChanged`; a shorter history shortens the logs with it, and
logs past the history are refused. Backups carry them as `Portable.Autopilot`, the switch
(`enabled`) among them, the history and the logs checked together against this Mac's, but
`mergePastRules`, which neither `PortableAutopilot` nor `autopilotFields` holds, so no backup
sets it either way; nor can Describe it, whose `SettingsChange` has no field for it and whose
decoder names it the user's alone. The bug
report's Labs line always says whether Autopilot is on. While it is on, or the user has a
routine (not deleted), the report states the settings, then Describe it's agent, with the
routines counted by mode, by switch, broken, and another account's, none named; while it is
off and the user has no routine, it says nothing more of Autopilot.

**Autopilot (beta).** `autopilot.enabled` (`SetAutopilotEnabled`, Settings → Labs) is off on
every install until the user turns it on, an install from before it included, since a key
never stored reads as its default; Prefs that can't be read count as off. Off, Autopilot is
inert and invisible, its routines, ledger, and copy left as they are:

- the snapshot of the searches is empty (`rebuildWatches` leaves every routine out), and
  `Searches.HoldsWatches` is false (`Engine.Enabled` and the lock), so the inbox's watch pass
  sends nothing and the checks follower follows none of the routines' items (its
  `ListRunningChecks` takes `watched`, `holdsWatches`, so the rows a search stored before stay,
  stale, and unfollowed);
- the engine (**The engine and the ledger**, below) takes no lock, runs no cycle, applies no
  command (`ErrOff`, *Autopilot is off. Turn it on in Settings → Labs.*), and makes no GitHub
  request; `Test`, and `Matching` through it, are refused the same way before they read
  anything;
- `Glance.On` is false and `Glance.Enabled` 0, so `KeepsRunning` counts only an agent run
  still going, and core's `ShellAutopilot` hands the menu bar no Autopilot group;
- the window (**The Autopilot tab**, below) shows none of it.

The engine reads the switch on every wake (`follow`, on the loop; core's relay wakes it on
`event.AutopilotChanged`), and a command reads it again before it is applied. Turned on, it
starts as a start does (`turnOn`: `takeLock`, which reads its state and checks the runs
going); its first cycle waits for a watch pass from then on (`onSince`), since what the
searches stored before is stale, and asks the inbox for one at once (`WatchNow`); and the
state's last refresh is the one it saw before it was off, so being off longer than the
catch-up setting begins a catch-up (`watchGaps`), as a quit that long would. Turned off
(`turnOff`), it starts nothing new from then on and stops nothing: it drains (`drain`), still
applying what its workers bring and settling the runs going on its ticks, and lets go of the
lock once no worker's result, no check of the runs, no run's end left to write, and no run
the ledger records as going (one whose end is on its way still is) remains. `Engine.Enabled` is the one check every start
goes through: `dispatch` asks it on the worker right before a step is handed to its
dispatcher (the agent `Runner` included), and a step that comes just after Autopilot was
turned off isn't handed over, and waits as one that never started (`ErrOff` in
`dispatchFailed`: a run ends `not_started`, counting as no try, and the match is queued).

### The searches, and the builder's reads of the inbox

**The snapshot.** After every change to a routine (`changed`, a backup's import included) and
once at startup (core's `assemble`, before the scheduler starts), the Service builds again
the searches of the enabled routines of the account signed in that load: each search built
for that account (`watch.Build`, "your account" written as its login), each key once with the
routines that share it, and `Follow` set when one of their triggers reads the checks (Checks
fail, Checks pass). It keeps them in an atomic value, which `Searches.Current` reads at once,
never waiting on anything; `Searches` (`NewSearches`) is the inbox's face of it, never bound.
Builds run one at a time (`watchSet.build`, a one-slot channel held from the read of the
routines to the store), so the last to read stores last: two side by side, a change's and a
new account's, could otherwise store the older read over the newer. A routine that is off,
broken, another account's, or whose search won't build is left out, and every routine while
Autopilot (beta) is off; the engine builds the snapshot again as it is turned on or off.

**`Suggest`** returns what the stored items hold for the builder (`Items.Suggestions`): the
people and the bots who opened them, the owners of their repositories, their labels, and
their check names, each most common first, no list nil.

**`Watching(id)`** returns the IDs of the items the routine watches and counts, newest activity
first: those in the inbox with the reasons it names, and those its searches' rows hold, stale ones
included, so a search that failed still shows its items (`Items.WatchedItems`), less those its
conditions rule out (`counted`). Each item's conditions are decided on its stored facts
(`Items.Facts`) and the routine's history there, built from the ledger as the engine's cycle
builds it (`histories`), with `eval.Plan.Conditions`, which leaves the trigger out: an item
waiting on it, such as a pull request whose checks still run, stays, and so does one whose
decision waits on a read (check names, files), since only a definite no rules an item out. It
never asks GitHub. The inbox's bound `ItemsByID` turns the IDs into rows.

**`Test(draft)`** tries a draft on its items now, changing no routine and running nothing: its
spec is checked as `Save` checks it (a `ProblemsError` with each problem); its searches are
read from GitHub (`Items.SearchNow`) unless a test read them within a minute (`testCache`,
by query, answers that failed never kept), their items stored as the watch pass stores them,
with no rows; each search is checked for an owner the inbox has never seen (`KnownOwners`),
since GitHub answers an unknown user, org, or repository with nothing and no error; the
routine is evaluated on each item (`eval.Evaluate`, with no history yet) from its facts
(`Items.Facts`), and the check names of the pull requests whose decision waits on them are
read, one batch of 25 (`Items.ReadChecks`), before they are decided again; the matches come
first, then the unknowns. The result says what the test spent (`Points`: the searches, the
details they read, and the check names, the searches costing nothing again when `Cached`) and
what the draft's searches would add to each refresh (`Costs.Estimate`, core's `watchCosts`:
the requests the refresh packs them into, by what each matched, at most a page, and a full
page for one that failed, at what the last watch pass cost a request, at the refresh's pace,
as a share of the GraphQL budget). While the GraphQL budget is under a quarter of its size the
inbox sends nothing (`ErrLowBudget`, from `SearchNow` or, when every search came from the
cache, `ReadChecks`): the result says so (`LowBudget`), each search it had no answer for says
why, no check name is read, and the lists' items are still decided. A search that matched
more than a page warns that only the newest 100 are checked; each item says whether an inbox
list holds it (`Listed`), and when most don't, each triage step gets a warning (`Warnings`, at
the step), since triage changes only what the inbox shows; a watch of searches alone gets none,
since `Describe` warns each such step of it already. For a saved routine (`d.ID`), each item
says what the ledger holds of the occurrence it fires for now (`Record`, in the window's words:
*It already ran on this.*, *Waiting for your OK.*, that a Dry run recorded it, or why it closed
in Activity's own words, *Closed: it merged.*, from `reasonWords`, which a test holds to the
window's `reasonChips`), read from `ListRoutineMatchKeys`; a match with no record is one it
would run on.

**`MatchStored(draft)`** counts what a draft matches now among the stored items of the inbox's
lists it watches (`Items.WatchedItems`), that its routine has no record of and doesn't leave
alone, by kind (`Matching`: the pull requests and issues apart, `Unknown` for an item whose
decision waits on a read, and `Incomplete` for a draft that also watches a search, whose
items are `Test`'s to read, or more than 200 items): a Describe it card's *Matches 3 pull
requests in your inbox now*. It decides from what the inbox stored alone, asks GitHub nothing,
works with Autopilot off, and refuses a draft with problems as `Save` does.

**The window's builder** (`frontend/src/components/autopilot/builder`) renders from
`Catalog`, its pickers fed by `Suggest`, and holds the draft in the Autopilot tab, which stays
mounted while another tab shows, so a draft survives switching tabs. It calls `Describe` once
editing rests (300 ms), and `Test` only on the user's click, once the draft has no problem,
never as a routine opens or as it is edited, since a test's searches cost points; a test of an
earlier watch, trigger, conditions, or Once per stays shown, marked as such, its warnings
shown only while the steps are as tested (they are by step), and its cost line, once the
searches changed, said to be the last test's. `Test`'s minute-long cache keeps a second test
of the same searches from costing points again.
Saving a routine that is on goes through the same question as its switch (`useRoutineChange`'s
`save`): `Matching` with the draft, nothing asked only when nothing matches, nothing waits to
be told, and everything was read (`Matching.Incomplete`: a search with no answer or past a
page, a budget too low, or more items than a test tries), and `Save` with the answer as its
`Current`. The window's catalog fixture (`frontend/src/test/autopilotCatalog.json`) is the
catalog itself, written and checked by `TestTheWindowsCatalogFixtureIsTheCatalog` (`-update`
rewrites it).

### The engine and the ledger

**The ledger** (migration `0103_autopilot`, all of it Autopilot's) records what each
routine did with each item it matched. `autopilot_matches` holds one row per routine, item,
fire key, and Dry run (unique), so an existing row blocks a repeat: once per occurrence holds
across refreshes, check re-runs, and restarts, and a Dry run's record never blocks a real
run. Each row keeps a copy of its item (repository, kind, number, title, URL; `item_id` is set
NULL once the item is gone), the decision's trace as it was, the steps it started with and
how each went (`steps`, JSON, with `step` where it is: a routine saved with other steps never
changes what a match has done), the spec's fingerprint (`spec_hash`), the head it was decided
on, whether auto-merge was on, the user's OK (`approved`), and how many runs it started
(`attempt`). A row is open (`waiting`, `blocked`, `queued`, `running`, `verifying`, with
`blocked_by`) or closed (`done`, `skipped`, `dismissed`, `closed`, `baseline`, with `reason`),
and every change is a compare-and-set on the state it was read in (`UpdateMatch ... WHERE
state = was`). Only the routine's own outcomes close a row: it ran (`done`), the user skipped
it (`dismissed`), it ran out of tries or met the outside contributor rule (`skipped`), or the
item moved on (`closed`: merged, closed, gone, left the watch, its fire key moved, no longer
matches, the routine changed or was switched off, the item's sheet no longer offers the
action). Everything that only delays a start keeps it open, retried every cycle. A row that
closed without ever running (no longer matches, left the watch, moved on, the routine changed
or was switched off, `Reason.reopens`) opens again when the same occurrence matches again, but
only if none of its steps ever started (`reopensRow`: no run, no step moved past, none running,
done, or failed): a row that started a step, a local one included, is the occurrence's record,
which only Try again opens again, and `Matching` counts it as recorded.
`autopilot_runs` holds one row per run a match starts (an agent's, in the background or the
terminal, or a GitHub action the app sends), with every column the run view, the caps, and the
stats read, and a partial unique index on `item_id` for the active statuses (starting,
running, stopping), so a second active run on an item fails in the database whatever the
engine's own checks say. `autopilot_tries` keeps when Try again last reset a routine's count
on an item, `autopilot_baselines` what a routine first saw of an item (its head, its comment
count, for New commits' first commit and New comments), `autopilot_hands_off` the items a
routine leaves alone, and `autopilot_effects` what its
local steps did to their items' triage (**Local actions**, below).
`autopilot_matches` is indexed by `item_id`, which every read by
item (`LatestMatches` and `ItemHistory`) searches. The engine's own state,
`autopilot.engine` (a JSON pref only the engine writes, `enginestate.go`), holds when Pause
began, the last refresh it saw, when the last catch-up began and the routines still in it, the
last cycle, each GitHub routine's first-pass fingerprint, the routines whose current matches
become their baseline, the problems, and when the retention last ran; a deleted or
switched-off routine's entries are dropped.

**The engine** (`NewEngine`, never bound; the Service is its face) is a deferred desktop worker
with one loop of its own (`engine.go`), the only writer of matches and the only thing that
starts anything. It wakes on what the inbox stores (core's `engineOnInbox` for a whole
refresh's `event.InboxUpdated`, and the `Changed` seam), on a change of Autopilot's settings
(core's relay turns `event.AutopilotChanged` into `Wake`) or of a routine, on what the inbox
saw happen to items (core's `outcomesFanOut` tells it beside the mirror of the GitHub
notifications, without waiting), on its 1-minute tick, on a window's command, and on a
worker's result; what comes during a cycle folds into one more. A cycle (`cycle.go`) reads
SQLite and evaluates, milliseconds: the enabled routines of the account signed in (one that
won't load is a problem, and skipped), the items each watches, their facts, the open matches'
items' states, the runs on them, and the day's counts; then it decides each routine on each
item (with the `eval.Plan` it prepares once per routine each cycle, `routineRun.decide`, and
the ledger's `History`: tries since the last reset, the last
outcome, the head it first saw, the comments it last counted, and the commits routines' runs
pushed), records each new match, closes what moved on, and moves each open match through its
steps (`advance.go`). Everything slow runs on a worker that hands its result back to the loop
(a mailbox that never blocks a worker): check names for the candidates whose decision waits
on them, the states of items that left a watch (read before anything closes), the read of an
item right before a start, a dispatch, a stop. So a command is applied within a fraction of a
second, mid-cycle or not. Each cycle is logged (its length, the routines, the items, the
matches, the starts, the closes, the reads), kept as `Engine().LastCycle`, and told to the
window as `autopilot:updated`, at most four times a second. A cycle that panics is recovered
and logged, and the loop holds off cycles for a minute; commands are still answered.

- **The first cycle after a start** waits for a refresh that stored after the engine was built
  and, when the routines have searches, for a watch pass after it (after Autopilot (beta) was
  last turned on, `onSince`), so the engine never acts on what the database held when the app
  was last open, or while it was off; when a refresh came but no pass (the very first one,
  before an account was stored, or Autopilot just turned on), it asks the inbox for the
  searches at once (`Routines.WatchNow`, a pass outside a whole refresh). Commands don't wait
  for it.
- **The lock** (`lock.go`) is a `flock` on `<data>/autopilot.lock` (`appdata.Paths.AutopilotLock`),
  tried as the engine is built (in `assemble`, so the copy that will hold it sends the searches
  from its first refresh) and on every tick, both only while Autopilot (beta) is on (**Autopilot
  (beta)**, above): only the holder runs cycles and applies commands
  (another copy refuses them, `ErrElsewhere`, and `Engine().Elsewhere` says so), and only the
  holder sends the routines' searches at all (`Searches.HoldsWatches`, which the inbox's
  `WatchHolder` reads before a watch pass), so two copies on one data folder never replace each
  other's rows; the holder builds its snapshot of the searches again from the database on every
  tick, which catches a routine changed in the other copy. A copy that doesn't hold it reads
  `Glance` on every tick and sends `autopilot:updated` when it reads otherwise than on the tick
  before (`followElsewhere`), since it runs no cycle that would: a Pause or a routine switched
  on in the holder reaches its menu bar and its window's close within a minute. The system
  lets go of the lock when the process ends.
- **The runs going** are checked when the lock is taken and on every tick (`reconcile`), but
  those a worker of this copy is starting or dispatching, and those whose `RunEnded` is on its
  way. A GitHub action still recorded as going was cut off by a quit or a crash, maybe after
  GitHub took it, so it is never sent again on its own: it ends interrupted, and its match waits
  for the user, worded so they check the item on GitHub first. An agent's run is asked of its
  runner (`Runner.Alive`, a `Liveness`), and taken as gone or never started only once two checks
  a tick apart found it so, as a runner may let go of a run a moment before its end is delivered
  (the `Runner` contract keeps a run `LiveAlive` until then). One both checks found the runner
  positively never started (`LiveNever`, still `starting`, as a start the quit cut off before it
  reached the runner) goes back to the queue, its match with it; any other found gone ends
  interrupted. `LiveNever` must hold across restarts and never come from a history that forgets
  runs (a runner that prunes its records answers `LiveGone` or `LiveUnknown` for a run it no
  longer knows), so a run that started and crashed is never run again on its own. A runner's
  end that comes after the engine recorded the run interrupted is kept over that record
  (`ReviseRunEnd`), its cost and commit with it, and moves the match on while it still waits on
  that interruption. One the runner can't say of (`LiveUnknown`, or an error) is asked again on
  the next tick, and ends interrupted once it has been unknown past the longest time limit a run
  may have and a grace (`unknownFor`), so no run holds its item's slot, an agent's slot, or a
  reservation for good. A start the runner refuses as it closes (`ErrClosed`) stays queued, and
  no agent starts in this run of the engine after it. A start that never happened, either way,
  ends `not_started`: it counts as no try and no run (`Attempt` is taken back, so the match is
  decided again like any queued one), and toward none of the day's counts or the daily cap.
  A run's end and its match's move are written in one transaction, a dispatch's answer too; an
  end that can't be written is tried again on the next tick, and each tick also moves on any
  match still `running` on a run the ledger records as ended (`unstrand`), by the end it keeps,
  or as interrupted when that end can't say (a stop, whoever asked for it). A stop that fails is
  tried again a tick later, never at once, and its reason is kept: a run that ends stopped
  anyway closes its match for what the stop was for (merged, closed, gone).
- **A catch-up** begins after a gap longer than Settings' catch-up hours (with the app closed or
  open, the Mac asleep): at once, on a cycle that finds the last successful refresh that long
  ago, before any refresh came; at the first successful refresh that long after the one before;
  or at a Resume after a pause as long. One gap begins one catch-up. Every routine on then is in
  it, and each leaves it on its own once a cycle has decided on what it watches read after the
  catch-up began (each of its searches answered with no error, and a refresh, when it watches
  the inbox's lists or has no search): until then its agent and GitHub steps wait for the user
  (`catch_up`), and a routine whose own search keeps failing says so, while the others go on.
- **A start** goes through, in Go, in this order (`gate`): the user's OK for a routine that asks
  first, and in a catch-up an Autopilot routine's too; no one signed in to GitHub (the last
  refresh found no token, or GitHub refused it: `signed_out`), Pause, and the routine's active
  hours, all three of which hold a local step too; the tries (`skipped: tries`), the routine's runs a day, the agent runs
  a day, the GitHub actions a day, and the wait between tries, none of which a manual run counts
  toward; a free slot and the daily cap and the routine's own budget, reserved in their unit,
  for an agent; the outside contributor rule (an agent closes `skipped`, a GitHub action waits);
  a fork's branch for a run that pushes; and one active run per item. Then the item is read
  fresh on a worker (`ReadItem`, the inbox's `RefreshItem`) and the `Holds` the later parts
  register are asked; the cycle after that read, which evaluated the item on what it stored
  (a moved head closes the match as moved on), records the run and the reservation in one
  transaction and hands the step to its dispatcher. A local step runs at once, through its
  dispatcher, held back only by a sign-out, Pause, and the active hours. A day runs from local
  midnight to local midnight, as the clock tells local time: a run's cost belongs to the day it
  started, and every run still working counts what it reserved against today's cap whatever
  day it started.
- **The dispatch seam** (`dispatch.go`): a narrow `Dispatcher` per part, keyed by the catalog's
  dispatcher names (`EngineConfig.Dispatchers`: triage, notices, github, verify, merging,
  review), and the agent part's `Runner` (`Dispatch` starts a run, `Stop` and `StopAll` stop at
  once, `Alive`), whose ends come back through `Engine.RunEnded`. A `Job` carries the match,
  the run, the routine, the item's copy, the head it is pinned to, the step, and the budget;
  a `Result` says it runs on, how it ended (an Only if outcome), whether GitHub took a write,
  and when the item's sheet no longer offers the action or offers it with warnings. A step with
  no dispatcher registered can't run, and the Service keeps a routine with such a step in Dry
  run (`Offers`, the catalog's own by default, which a test hands in with fakes). The triage
  steps are offered: the engine registers its own triage dispatcher over `EngineConfig.Triage`
  (core's `inboxTriage`, **Local actions**, below) unless `Dispatchers` names one; so are the
  notices (Notify me and Tell me, **Notices**, below), whose dispatcher core registers, the
  agent step, whose `Runner` core registers (**Agents in the terminal**, below), and the
  GitHub step and Mark the GitHub notification done (**The GitHub step**, below), whose
  dispatcher the engine registers over the Service's `GitHubActions`; a routine with any other
  step still runs in Dry run, and the engine records what it would run. A `Result` may also say
  the step didn't start and what the match waits on instead (`Wait`, `WaitWords`, and a `Hold`
  the user confirms), or that it goes back to the queue (`Requeue`), and what a launch started
  (`Launched`, recorded on the run, `RecordRunLaunch`). For the notices, a job also carries
  when its match was made, the routine's own banner switches (`Notices`), why it matched
  (`Reason`, from the trace), and how the steps before it went (`Before`). `dispatch` hands a
  job over only while `Engine.Enabled` says Autopilot (beta) is on, asked on the worker right
  before `Dispatch`, so no part (the agent runner's starts included) is handed a step after it
  was turned off; the freshness read before an agent's start or a GitHub action asks it too, so
  nothing is read from GitHub for a start Autopilot turned off since.
- **A run's end** moves its match on: an interrupted one waits for the user (`interrupted`,
  counting no try), one the user stopped closes `dismissed`, one the engine stopped because its
  item merged or closed closes `closed` (counting no try), one out of usage (`RunOutOfUsage`)
  rests its profile and goes back to the queue at the same step (counting no try, **Profile
  fallback**, below), and one that ended goes to its next step, straight to `verifying` when it
  pushed and Wait for the checks is next, so the push, which moves the item's head, never
  closes it as having moved on. A match that started a run is closed only by its item being
  done with; its fire key moves by design. While one of a routine's matches on an item is
  running or verifying (`pass.active`), another of its matches there waits (`settling`).
- **Settle what moved on:** an open match whose item merged, closed, or is gone closes; one
  whose item left every watch of its routine has its state read (`ReadStates`, one request, one
  at a time per item, again every half hour), so it closes as merged or closed when it did, and
  closes as having left the watch only while every search of the routine last answered in full:
  a search that failed, was truncated, or was read in part keeps its rows, stale, and even once
  the inbox drops a truncated tail, its items are stale, not gone; one whose fire key moved
  closes as moved on, one the routine no longer matches as no longer matches (or, after an edit,
  as the routine changed), and the rest take the routine's new steps. A running agent whose item
  merged or closed is stopped through the runner; a verifying match closes, and one whose item
  is still there is judged (**Verify**, below), its routine switched off or not.
- **The first pass** of a routine with a GitHub action, after it is switched on, leaves Dry run,
  changes mode, or its spec changes (a fingerprint of the spec, when it was last switched on,
  and its mode, so switching it off and on in the other copy between two cycles counts too;
  recorded only for a live, enabled routine), waits until its searches were read since, then
  queues at most `FirstCap` (25) matches; over that, none, and they wait as a `backlog` for
  Approve.
- **Apply to current matches:** `SetEnabled`, `SetMode`, and `Save` (`Draft.Current`) take what
  to do with what the routine matches now and has no record of (`Matching` counts them, as
  `Test` reads them, and says when the count may be short, `Incomplete`): `run` them too, or
  `fromNow`, applied as one command with the change, so no cycle runs between; the engine then
  records those matches as `baseline` once the routine's searches were read since, and nothing
  runs on them. `fromNow` is refused, with why, in a copy that doesn't run the engine (it hasn't
  started yet, or another copy holds it), rather than dropped, but only for a change the
  question applies to (switching on, leaving Dry run, a save that changes the Watch, When, If,
  or Once per of a routine that is on, `watchChanged`, since Once per decides each match's
  fire key, each told before the change is made); any other change, and `run`, is made there at
  once.
- **Problems** (`upkeep.go`): a search GitHub fails, and an enabled routine that won't load, are
  recorded once each, keyed by routine and problem, with when they began, in the engine's state
  and `Engine().Problems`, and told through a `ProblemSink` once (core registers
  `NewProblemNotices`, below), with the routine's name, when it was made, and its own banner
  switch for problems; one that couldn't be told stays untold and is told on a later cycle,
  and one that ends is forgotten, so it is told again when it comes back.
- **Retention:** once an hour, a closed match closed longer ago than the history's days goes,
  with its runs, but only once its item is known to be done with (gone, merged or closed, or out
  of every list and watch since then): deleting the record of an item still open would let it
  fire again. Open matches never go. `ClearHistory` does the same whatever a row's age, but
  "out of every list and watch" still means for the history's days, since the app reads no more
  of an item once it left them; it keeps a closed match that may still block its occurrence
  without its trace and summaries. What deleted routines
  kept of their items goes, and a deleted routine nothing refers to goes for good. The inbox's
  prune keeps every item a match or a run refers to (`KeptItems`, one of core's `itemKeepers`).

**The window's actions** are commands the loop applies, then answers (`commands.go`), so a
double click, a banner's click racing a cycle, or an Approve during a refresh changes a match
once: `Approve` (an Ask first match's OK, a catch-up's, a backlog's, an interrupted run's),
`Skip` (refused for a running one), `RunNow` (once per click, on an item its conditions match,
counting toward no cap or try), `TryAgain` (resets the tries and opens again what gave up on
the occurrence that still holds), `LetActAgain`, `Undo` (**Local actions**, below), and
`ClearHistory`. A command before the loop runs is refused (`ErrNotRunning`), and one while
another copy holds the engine
(`ErrElsewhere`). `StopRun` and `StopAll` go straight to the runner, never queued behind the
loop, so they work before the first refresh, offline, or signed out. The reads (`activity.go`)
read the database alone: `Activity` (a page of matches before an ID, by local day, with their
runs and effects, each day's grouped lines of local steps, and, on the first page, the items
the routines leave alone), `ActivityFrom` (the same, of the matches from an ID on and before
another, 0 for up to the newest, at most 500, `ListMatchesFrom`: a stretch of the pages the
window has loaded read again in one call, by its own bounds, so their grouped lines join by
`Key` and nothing falls between them; `More` and `Next` say where the rest of the stretch
begins, and only a stretch up to the newest says what the routines leave alone),
`ItemHistory`,
`LatestMatches` (for each item among the ids it is
given, at most 500, its newest match that did something there or waits to, in one query,
`ListLatestItemMatches`: open, or closed as done, skipped, or dismissed, never a baseline or
one that closed as the item or the routine moved on before it acted, with its runs and
effects, for the chip on every row; the window asks in chunks of 500, `inChunks`),
`Upcoming` (for the items among the ids, the routines that will run there by themselves,
from the engine's memory, none while Autopilot is off or paused, signed out, or run by
another copy; see *What runs next* below), `Explain`
(the decision now, and what the ledger knows:
the record for the occurrence, the tries used, not in the watch, left alone), and `Engine`. No
result holds a nil list.

### What runs next (`upcoming.go`)

What the user sees is the ⚡ chip's *What runs next* in `docs/desktop.md`. As a cycle decides
each routine on each item it watches (`evaluateItem`), a decision whose trigger is pending
goes to `noteUpcoming`: the routine acts (not Dry run), has no record of the pending
`FireKey` but one that reopens (`reopensRow`), so a commit it ran on whose checks run again
says nothing, and its `Plan.ConditionsWhenDue` is a sure yes; the item is one it doesn't leave
alone, which `evaluateItem` checked first. Each becomes an `Upcoming` (the routine, its mode
and whether it waits for an OK, `waitsForOK`: Ask first with a step `asksFirst` names, its
trigger, `When` and `At`, and its steps as a match's show before they start,
`plannedSteps`, once a cycle). A pass that decided every routine replaces the engine's map
by item whole (`keepUpcoming`, under `Engine.mu`); turning Autopilot off empties it. So
`Service.Upcoming` is a map lookup, no database, no GitHub, and the `autopilot:updated`
every cycle sends keeps the window's copy current. `LeaveAlone(routine, item)` inserts the
routine's hands-off row (`whyLeftAlone`), which Activity's Left alone line and
`LetActAgain` already handle, forgets the item's entry at once (`dropUpcoming`), and wakes
the engine.

### Verify, tries, the loop guard, and profile fallback

What the user sees is in `docs/desktop.md`'s *Closing the loop: Wait for the checks*.

- **Verify** (`verify.go`) is the engine's own (`catalog.DispatchVerify` names no registered
  dispatcher): each cycle judges every verifying match from the facts the cycle read (the
  open matches' items are read beside the watched ones, so one that left every watch is
  judged too) through the pure `judgeVerify`: the run's commit (`run_commit`, or its worktree's
  `head_after` once that is the pull request's head, for a report that didn't say), the head
  it started from, the pull request's head, its check counts, its checks' names and when the
  last finished (`ChecksDone`), and when its details were read (`facts.Facts.DetailsAt`, from
  `item_details.fetched_at`). The verdict (`Verdict`: fixed, still failing, unknown,
  overtaken, push rejected, no push) is the wait step's (`stepRun.Verdict`, with its Only if
  outcome, `passed` or `stillFails`) and the run's (`autopilot_runs.verdict` and
  `verified_at`, `SetRunVerdict`), written in one transaction with the match moving to its
  next step; the step's settle and give-up times are its own (`WaitStep.Settle`, `GiveUp`).
  While it waits, it asks only for what it lacks: the check names once none runs
  (`ReadChecks`, at most a minute apart), and the item itself (`ReadItem`, one point, at most
  every `verifyReadAfter`, two minutes, and only a minute after the run ended, so the read the
  run's end asks for comes first) while its push isn't seen, its details are that old, or no
  check has appeared. A routine with a Wait for the checks follows the checks of what its
  searches return (`WatchSearch.Follow`), so the checks follower reads it every round and
  verify reads nothing itself. A verdict on a fix's checks is told once as **Checks after a
  fix** (`EngineConfig.Checks`, a `NoticeChecks` keyed by the match and its run); a still
  failing verdict on the routine's last try says it gave up (`gaveUpWords`).
- **Tries** are a routine's counted runs on an item since its last Try again, across fire
  keys, so a Dependabot rebase, a new key, resets nothing (for agent runs as for GitHub
  actions); an interrupted run, one stopped for its item, one that sent nothing, and one out of
  usage count as none (`endCounts`). The occurrence after the last try closes `skipped:
  tries`, *Gave up after 2 tries.*, which `TryAgain` reopens.
- **The loop guard:** `eval.History.Working` reports an agent of any routine's working on the
  item (a run starting, running, or stopping), and New commits is unknown while it does,
  unless the trigger lets a routine's own pushes through, so a commit the agent pushes
  mid-run fires nothing before its run's end records it among `OwnHeads`.
- **Profile fallback** (`fallback.go`, core's `backgroundAgents.dispatch`): an end out of
  usage carries its limit (`RunEnd.Limit`: its type and when it resets, from
  `agentrun.Outcome.Limit`), and the engine rests the run's agent and profile (as its launch
  recorded them) until then, an hour when it doesn't say, in its own state
  (`engineState.Resting`, so a rest outlives a restart). Every agent step's `Job.Resting`, and
  so its `RunRequest`, holds the profiles resting now; the runner plans the step's chain in
  order (the step itself, then each fallback, `fallbackStep`: that agent's profile for every
  repository, the step's model and effort only for a fallback of the agent the step resolved
  to) and starts the first that isn't resting. A fallback whose plan says its agent can't run
  in the background here (`cantRunHere`: a preset it can't keep, as Codex can't keep GitHub
  tasks only, no background half, or no program) is passed over; the step's own profile never
  is. With nothing left it answers `Wait` (`out_of_usage`, queued, or `not_ready` when it
  passed a fallback over) with `RetryAt`, the soonest rest's end, and the engine holds the
  match until then (`heldBack.until`) without reading the item. Readiness marks what only a
  fallback needs (`Need.Fallback`), which is only ever a warning.

### Merge it and Post the agent's review

What the user sees is in `docs/desktop.md`'s *Merge it and Post the agent's review*.

- **The seam.** Both are GitHub writes of their own (`catalog.DispatchMerge`,
  `DispatchReview`), registered beside the GitHub step over the same `GitHubActions`
  (`registerWrites`), so each is its own `autopilot_runs` row (`runner = github`), recorded
  before it is sent, paced by `pace` and the GitHub actions a day, and never sent twice on its
  own (`writesGitHub`). The dispatcher's `ReadItem` is the sheet's read, and the job carries
  the commit it is pinned to (`followUp`): for Merge it, `mergeHead`, the commit the step's
  Wait for the checks passed on (`stepRun.Commit`, which `applyVerdict` records), or, with
  no agent step and no wait, the match's own head, only while its fire key is that commit's
  `checks_passed` key (`agentlessHead`, `eval.CommitKey`: a click's run, keyed `manual:`,
  merges nothing), `Job.AutoMerge`, whether to turn auto-merge back on (`autoMergeBack`:
  `MergeHow` says so, and it was on as the routine matched,
  `autopilot_matches.auto_merge_at_match`), and `Job.PastReview`, `Sensitive`, and
  `PastReviewWhy` (`pastReview`, below); for Post the agent's review, `agentReviewOf`, the review
  the read-only agent's report held (`agentrun.Report.Review`, kept to `ReportSchema`'s
  `review` object, carried on `RunEnd.Review` into the agent step's `stepRun.Review`) and the
  commit its run started at, and `Job.Review`, what `reviewEvent` decided to post. A run whose
  agent wrote no review skips the step (`noReview`).
- **The guards in Go,** whatever the spec says. `mergeRules`, first in the gate, closes a
  Merge it on an outside contributor's pull request (`not_offered`); a review on one waits
  `outside_contributor`, as a GitHub action does. `reviewEvent` is pure: a comment unless the
  step posts its verdict and the user didn't ask for a comment (`stepRun.PostAs`,
  `Service.PostAsComment`); an approval only with `AllowApprove`, a member's or a trusted
  bot's pull request (`guardOf`, from `FieldTrust`), every file checked and none sensitive
  (`FieldSensitive`, known and not partial), and no routine's run that recorded a commit
  there (`pushedTo`); else it posts as a comment, saying which guard held it.
- **An agentless merge is decided again** (`recheck`, second in the gate): a match that ran a
  step before its Merge it is never settled again by `closeMoved`, so right before the merge
  (and again on the cycle after its fresh read) the routine's decision on the item must be yes
  on the match's own fire key. A yes on another key closes it `moved_on`; a no whose trigger
  held (a condition, such as a label taken off) `no_longer_matches`; anything else, a trigger
  that doesn't hold now (checks running again) or a condition not known yet, waits
  `settling`, reading nothing.
- **Past the rules** (`pastrules.go`). `pastReview` lets a Merge it go past the rules only
  with every consent the user gave and the engine can see: the step's `AllowBypass`
  (`AllowSensitive` for sensitive files); a routine's own run (`matchWhy`: not a click's, at
  the commit its checks passed on); `autopilotprefs.Config.MergePastRules` on; the routine's
  `past_rules_hash` equal to the full SHA-256 of its stored spec (`accepted`), and the match's
  steps that routine's (`SpecHash`); and no routine's run having pushed to it (`pushedTo`,
  as for an approval). A step that allows it but may not says why (`PastReviewWhy`); one that
  doesn't allow it says nothing. When the why is the switch (`PastRulesOffWords`), the job
  says so (`Job.PastRulesOff`, `MergeSend.PastRulesOff`), and a merge that needs to go past
  the rules comes back `GitHubPastRulesOff` (`Result.PastRulesOff`): `unsent` puts the step
  back, as one that sent nothing, and the match waits `blocked` on `past_rules_off`
  (`pastRulesWaitWords`), at that step, its earlier steps kept as done; the gate
  `pastRulesSwitch` keeps it waiting, with no read of the item, while the switch is off, and
  lets the merge go again once it is on. Any other `not_offered` ends its step `skipped`
  (*Not sent: …*) as it closes the match, so no step stays `running` on a closed match. An
  earlier build closed such a match instead, `not_offered` with `PastRulesOffWords`:
  `resumePastRules`, once per engine (`Engine.resumed`, on its first cycle), sets each one
  closed within the last week (`ListClosedMatches`) waiting on `past_rules_off` again, at its
  merge, for the gates to decide (recheck closes one whose pull request moved on). `Service.Save` stores the hash only with
  `Draft.AcceptPastRules`, or when the new spec text is the very one accepted
  (`pastRulesToWrite`), refuses a spec with a tick otherwise as a problem at its first
  `merge.allowBypass` (`ProblemsError`), and clears it for a spec with no tick; `Routine.PastRules`
  (`allowed`, `needsTick`, or `""`) says where it stands. Duplicate, Restore, and Import
  insert none; a backup's import clears it; the prompt rename's follower carries it only while
  it stood; no export, backup, or `autopilot.json` holds it.
- **Core** (`internal/core/adapters_followups.go`, `githubSteps.Merge` and `PostReview`):
  each reads through the review sheet (`Refresh`, serving the read just made), then decides
  on it. A merge of the user's own pull request that lacks only an approval
  (`Review.OwnAwaitsApproval`), with `PastReview` or `PastReviewWhy` set, goes to
  `pastReview` (`adapters_pastreview.go`, the only file of core that names
  `review.ActionMergeBypass`, each request there with `Routine` set, which
  `TestOnlyThePastReviewAdapterMergesPastTheRules` parses core to hold): `PastReviewWhy`
  closes it `not_offered` with the reason, or, with `PastRulesOff`, answers `GitHubPastRulesOff`
  (`pastRulesOff`), which waits for the switch; else it reads the switch again
  (`autopilotSettings`, off answering `GitHubPastRulesOff` too), refuses an offer warning of more than the approval and, as the step
  allows, sensitive files (`pastReviewOffer`), and sends `merge_bypass` with
  `review.RoutineMerge`; `ErrPastRulesNotYet` comes back `GitHubNotYet`, which waits
  `settling` and is tried a tick later, never asking the user, and a merge past the rules says
  *Squash-merged #12 past the rules: it lacked only an approval from someone else.* Any other
  merge goes on with the pure `mergeDecision` or `reviewDecision`: a head other than the pinned one
  is not offered; auto-merge already on is nothing; a merge the sheet won't send but would
  approve and merge (`waitsOnReview`) is `GitHubNeedsReview`, which waits
  `review_required` for the user, never approving; and anything else goes only as the sheet
  offers it now (`pullOffer`, warnings only the user accepted). A merge first asks the inbox
  what the agent's commits at that head changed (`agentChanges.AgentChanged`, the fence's
  check): a sensitive group, or a check not complete, merges nothing. A review's line
  comments are fitted to the cached diff (`review.Fitter`, never bound: the sheet's own
  check of each against its hunks, `checkComment`, the rest left out and counted, so a review
  never fails whole on a line GitHub would refuse). Then the sheet's `Act` sends it under
  the inbox's `Actor` and the write gate: only `merge_now` (`mergeMethodOf`, the step's
  method) or `auto_merge`, and only `comment`, `request_changes`, or `approve`;
  `TestMergeAndReviewReachNoForbiddenAction` parses `adapters_followups.go` and fails on
  `merge_bypass`, `approve_auto_merge`, a close, approving workflow runs, or sending
  `approve_merge`, whose offer it only reads. A merge GitHub refuses, by the merge state the
  sheet reads right before it or by the merge itself (`github.ErrNotMergeable`), merged
  nothing, so `mergeSent` makes it not offered, in the sheet's words, never a failure after
  which GitHub may have taken it.
- **Ask first** (`askFirst`): both steps ask again once reached (`followsRun`, the wait's
  words from `followWords`), whatever the routine's OK to start, a click's run's too, and `Approve` records the
  OK on the step (`stepRun.OK`), so the card shows what the run came to. The window's card
  reads it from `MatchStep` (`Merge`, the merge's words; `AutoMerge`; `Review`, `ReviewAs`,
  `AllowApprove`, `PostAs`).
- **Stats** count a sent merge as `Merges` and a posted review as `Reviews`, by the step's
  kind, never as a GitHub action, and the review's run ends with the event it posted as its
  step's outcome (`approve`, `requestChanges`), which the ⚡ chip reads.

### Agents in the terminal, readiness, and Needs your OK

What the user sees is in `docs/desktop.md`'s
[Agents in your terminal](desktop.md#agents-in-your-terminal), [Needs you](desktop.md#needs-you),
and [This Mac](desktop.md#this-mac).

**The start path.** Every agent start goes through the engine's one path, never another: the
gate (`advance_gates.go`), in Go, cheapest first, then on a worker what holds it back on this
Mac, then the fresh read, then the runner:

1. `gate`: the user's OK, a sign-out, Pause, the active hours, the tries and the day's counts, a
   free slot and the daily cap, the item's own rules (`itemRules`: an outside contributor's item
   is skipped, `skipped: outside_contributor`, unless `EngineConfig.Outside` allows its
   repository, a repository's own choice over its org's, core's `outsideRules` over
   `launchdefaults.Index.OutsideAllowed`; allowed, it still plans only, since nothing a routine
   builds sets `FullAccessOverride`), a fork's branch for a run that pushes, one active run per
   item, and this Mac's readiness (`readyFor`, from the last checks, never waiting on one: a need
   not checked yet waits `queued: starting` and asks for a check; one not met waits `blocked:
   not_ready`).
2. On a worker (`readFresh`): the step's prompt, checked against the library again
   (`promptHold`); then `EngineConfig.Holds` (core's `agentHolds`: the item's own launch
   resolved, its agent's program and its profile checked cheaply, the last sign-in check kept;
   while Settings' **Hold agent starts while their provider reports an outage** is on, the
   agent's `StatusPart` major or critical in `svcstatus.Status`, `queued: provider_outage`; a
   repository with no clone, `waiting: no_clone`; a tool the clone's marker files say it needs
   (`go.mod`, `pnpm-lock.yaml`, `magefiles`, `.mage.yaml`, `Makefile`, `Cargo.toml`,
   `pyproject.toml`, by `os.Lstat` alone) missing from the login shell's PATH, `blocked:
   missing_tool`); and only once nothing held it, the item read fresh (`ReadItem`, one point).
3. The cycle after the read records the run and hands the step to the `Runner`, whose `Result`
   may say it didn't start (`Wait`, `Requeue`): the run ends `not_started` (no run, no try), and
   the match waits on what held it (`heldAt`), an OK given before a hold taken back.

**Agents in the terminal.** Core's `agentRuns` (`internal/core/adapters_agents.go`) is the
`Runner`: a step `In my terminal` is the
launcher's own `Launch` of `routineRequest`, the request a click would make with the routine's
choices in the launch menu's place (the step's agent, profile, model, and effort, a per-org or
per-repository row of the routine's over them, a repository's over its org's; the prompt by its
starter's key or its nickname; plan mode and the worktree), and `launcher.RoutineLaunch` naming
the routine. `RoutineLaunch` only ever holds a launch back more than a click: its pull request's
files are checked under the unattended scope (`FileChecker.CheckFilesUnattended`, the inbox's
`Routines.CheckFilesUnattended`: every pull request the user didn't open, trusted bots' and
members' included, `sensitive.CheckedUnattended`), a file hold is confirmed only by
`RoutineLaunch.Confirmed` (never the request's own `Confirmed`), a session running in the
folder always holds it (`Alongside` is ignored), a worktree with uncommitted changes holds it
(`HoldDirty`, keyed by the worktree, its head, and the count), `FullAccessOverride` is ignored
(`personsChoices`), and its prompt names the routine (`.Routine`, `.Trigger`, `.Attempt`;
`.Unattended` stays false in the terminal). A hold comes back as the match's wait (`heldResult`:
agent files or unchecked files `waiting: hold` with the hold, a dirty worktree `waiting:
dirty_worktree`, a session `queued: busy_worktree`); `worktrees.ErrHeadMoved` requeues it. A
launch that opened ends the run done at once, recording its session, agent, profile, clone,
and worktree (`Launched`): the session is the user's from there, which the sessions ledger
follows as any, and a later start finds it in the folder (the launcher's running hold).
`Alive` answers gone for a terminal run still recorded as going, so one a crash cut off
mid-launch is never launched again on its own (the engine's `alive` takes a terminal run its
runner never started as gone). A step in the background goes to the background runner
(**Agents in the background**, below).

**A start held back on a session.** A start its runner held back on something working where it
would start (a queued wait, `busy_worktree`) isn't tried again until a tick has passed
(`holdBack`, `backOff`): each try reads the item from GitHub first, so a session in the worktree
would otherwise cost a point every cycle until it ends. The user's OK tries it again at once.

**The user's OK of a hold.** A held step keeps its `Hold` in the match's steps (`stepRun.Hold`),
which `Match.Hold` shows the window while the match waits on it; `Approve` adds exactly that
hold's `Key` to the step's `Confirmed` (`confirmHold`), which the next `Job` carries
(`Job.Confirmed`, `RunRequest.Confirmed`) into `RoutineLaunch.Confirmed`. A routine never
confirms a hold on its own, and a key names what the user saw (the head commit for files, the
file count for a dirty worktree), so a push or a new file since holds it again.

**Readiness** (`ready.go`). `Needs(spec, LaunchDefaults)` is pure: GitHub for every routine;
banners for a notice; for an agent step its place (`place:`), the terminal for a terminal run,
each agent and profile it may run (its own, its per-org rows', and its fallbacks', Inherit as
the app's default agent and that agent's default profile), `gh` and `git`; and what it
`Requires`. A `ReadinessChecker` (core's `readyChecks`) checks each need: GitHub from the inbox's
last refresh, or `setup.GitHub` (one point) on Check again; a program on the login shell's PATH;
an agent by its spec's `VersionArgs` (`agent.ParseVersion`); a profile by its folder and its
spec's `SignInArgs` (`agent.ParseSignIn`: Claude Code's `auth status` JSON, keeping only
`loggedIn`, the method, `subscriptionType`, and the expiry, never the email; Codex's `login
status`), run with the profile's `ConfigEnv` and without the agent's `KeyEnv` unless the user's
API key setting is on (then a key in the shell's environment is ready by itself); the terminal
chosen; and the banners (a warning only). An agent's version and a profile's sign-in are kept
ten minutes unless fresh. A routine is ready when each need is ok or a warning. The Service and
the engine share the results (`readiness`): the engine checks on a worker as it starts, when a
routine is saved or switched on (`Service.changed` marks it due), and every ten minutes; the
bound `Readiness` serves them (checking once when none ran) and `CheckReadiness` runs every check
again, fresh, then wakes the engine. A need not met is a problem (`ProblemNotReady`, keyed by the
routine and the need), told once.

**Needs your OK.** Every save of a match that comes to wait for the user (`saveIn` →
`noteWait`), a Dry run's never, is told at the cycle's end (`tellWaits`, on a worker) as a
`NoticeNeedsYou` through `EngineConfig.NeedsYou` (core's notices sink), keyed by the match, its
creation, the wait, and the try, so a cycle that sees it again, or a restart, tells nothing
twice; read again first, so one no longer waiting is told nothing; several at once are a burst.
Its target is the match, which the window's Needs you card is. The bound `NeedsYou` lists the
open matches that haven't started (waiting, blocked, queued), real ones alone, newest first,
for that section.

**Run a routine** (`RunNow(id, itemID, anyway)`): a manual match, once per click, counting
toward no cap or try, only on an item of a kind it acts on (a click holds whatever the trigger,
so the kinds are checked first); with `anyway`, on such an item its conditions don't match too,
under a manual fire key. Every limit of a start still holds it. Without `anyway`, an item its
conditions don't match is refused with a `RefusalError` (`refusal.go`) whose code is
`RefusalNoMatch`, its words naming the first condition that fails and its chain holding
`ErrNoMatch`; `desktop.MarshalError` sends the window `{"code": "noMatch"}` beside them, as
`update.RefusalError`'s codes go, so the window offers Run anyway from the code, never the
words, and never asks `Explain` for it. Every other refusal, the same `ErrNoMatch` for a match
that is gone and a run, anyway or not, on a kind it doesn't act on among them, carries no code.

**Preview** (`PreviewRun(draft, itemID)`) is `Previewer` (core's `runPreviews`): the launcher's
own `Preview` of the same `routineRequest` a start would launch, with where each setting came
from (`routine` for the routine's own), so what it shows is what runs.

### Agents in the background

What the user sees is in `docs/desktop.md`'s
[Agents in the background](desktop.md#agents-in-the-background).

A step `In the background` goes through the start path above exactly as one in the terminal
does, to the same `Runner`: core's `backgroundAgents` (`internal/core/adapters_agentrun.go`)
takes a step in the background itself and hands one in the terminal to `agentRuns`. Its
`Dispatch` is a launch's, with three differences:

1. **The plan.** `launcher.Backgrounds.Plan` (`launcher.NewBackgrounds`, a type of its own, so
   the bound launcher never plans one) plans the same `routineRequest` a terminal run would
   launch, with no terminal: the item's worktree, always, made at the head the match was
   decided on; the fence's access in place of the profile's (`Request.Fence`, the step's tools
   preset and the run's folder, which `resolvePermission` turns into `agent.AccessFence`: an
   outside contributor's item Read only whatever the fence says, a preset its agent can't
   keep `ErrFenceNotKept`, a start that waits `blocked: not_ready`); the profile's extra
   directories left out; and the agent's headless flags around the fence's own
   (`agent.HeadlessArgs`: `-p --output-format stream-json --verbose --permission-prompts none
   --session-id=<id>`, the model and effort, the fence's access flags, `--max-budget-usd`,
   `--json-schema` for the report, `--append-system-prompt` with the unattended note, and
   `--system-prompt-snapshot off`), a profile's extra arguments refused where they set what
   the fence sets or would loosen it (`agent.CheckFenced`). Its holds are a launch's own (`heldBack`): a
   session or a run in the worktree, the pull request's files under the unattended scope, and
   a dirty worktree, each confirmed only by the routine's user; after `EnsureWorktree`,
   `Backgrounds.Made` holds the worktree made as `heldBack` held the one planned, and a
   worktree off the head the plan checked waits (`waiting: dirty_worktree`). Then
   `Backgrounds.Fenced` works the run's fence out for the worktree made (`agent.PrepareFence`
   over `fence.Spec`: the preset, the worktree and its tools, `fence.RepoTools`, the item, its
   head branch and the clone's remote for a push, the step's own `Allow` and `Deny`, the run's
   folder, home, the data folder, and the profile's config folder, whose hook is the app's own
   executable in its hook mode), rebuilds the argv with the fence's line added to the
   unattended note (`FencedRun.Note`), and core writes its files into the run's folder
   (`FencedRun.Write`: the policy, the agent's settings, and a sandboxed preset's check
   folder); a push preset on a fork's pull request waits `blocked: cross_repository`. The
   prompt is filled in with `.Unattended` true and passed on stdin from the same folder
   (`<data>/agent-runs/<session>/prompt.md`).
2. **The environment** is the login shell's (or the app's with the finder's folders while it
   can't be read), with the plan's changes on top (`launcher.AgentEnv`: the profile's config
   folder or, for the default profile, none; no API key unless the user's setting lets one
   through), the config folder never named as the agent's default one, and the variables an
   agent session sets for itself dropped (`agent.HeadlessEnv`: `CLAUDECODE`, `CLAUDE_CODE_*`,
   `CLAUDE_EFFORT`, `CLAUDE_PID`, `CLAUDE_TMPDIR`).
3. **The run** is `internal/agentrun`'s. The `Runner` starts it as a `scriptrun.KindAgent` run
   (its own retention, `KeepAgentRuns` and `KeepAgentLogBytes`, and the age Settings' **Run
   logs kept** gives, apart from the scripts' runs, never listed on the Runs page: `Filter`
   leaves the kind out unless `WithAgents`), holding the worktree's lock (`clonelock.KindAgent`,
   keyed by the worktree, so two items of one clone run side by side), whose `Before` checks
   under that lock that the worktree holds no more changes than the user confirmed and that no
   session the app follows works there. It records the run as a session in the sessions
   ledger (`agent_sessions.run_id`, which the `Watcher` leaves to the run: `Ledger.RunState`
   moves its state from the stream, and hands the item to `Rereads` once it stops working), so
   the item's pill and panel follow it; the worktrees listing and the launcher's running hold
   read its folder (`RunFolders`), since the process list leaves out the app's own children.
   `Dispatch` returns `Running` with what it started (`Launched`, `ScriptRunID` included), and
   the run's tokens reach the ledger every 30 seconds (`Engine.RunUsage`).

**The stream** (the leaf [`internal/agentrun/stream`](#internalagentrunstream): `stream.go`,
`claude.go`, and Codex's `codex.go`, the runner picking one by the run's agent, `parserFor`)
splits the log into lines itself (one is read up to 64 MB, a longer one counted and skipped),
keeps the last 2,000 events (each text cut
to 8 KB, `runevent.MaxText`) and the progress (the latest step, the todo list, tokens counted
once per message), and counts a message it doesn't know (`Progress.Unknown`). Each event
carries where its line starts in the log and which of the line's events it is (`Offset`,
`Part`, together its key) and the latest time a line gave (`At`, from the assistant's and the
user's messages' `timestamp`). `runevent` is the leaf both sides share; the window asks for a
run's events after the last it has (`Service.RunEvents`) on each `event.AutopilotRun`
(`autopilot:run`, at most four a second a run).

**The run view** (`agentrun/view.go`, `autopilot/runview.go`). `RunView` is the ledger's run
and its match; `RunEvents` follows a working run from the runner's memory, whose first page
says what its log holds before the events kept (`Page.Earlier`, `Before`, the first event's
line); a run that ended, or one the runner doesn't follow, is its log's last page
(`Runner.EventsBefore` at the end), and `RunEventsBefore` reads the page before an offset:
`readBack` reads the log back a 256 KB chunk at a time (`scriptrun.Service.Output`) until it
holds about 200 whole lines, or 1 MB and one (a line longer still is read whole, to its start),
drops the line the chunk started inside, keeps the last 200 lines, and parses them with a
parser started at that offset (`stream.Parser.From`), so a long log is read a page at a time,
never whole. A page of an ended run carries its `Ending` (`runevent.Ending`: the record's summary,
the agent's report or last message, whether it needs a person, its turns, time, models, commit,
changed files, and its last todo list, `Outcome.Todos`), and its progress the record's tokens.
`RunDiff` is the worktree's changes since the head the run started on, committed or not, with
the files git doesn't track (`gitread.WorktreeDiff`: `git diff --no-ext-diff --no-textconv
--no-color --no-renames --ignore-submodules=dirty <head> --`, run with the clone's own filter
drivers off as a status is, cut to 1 MB at a line's end, beside the porcelain v2 status's
untracked files), from the worktree the run's intent names (`Intent.Dir`). A run that isn't an
agent's in the background (one in the terminal, a script's, a GitHub action's) has no timeline
here: its events are an empty page (`followedElsewhere`), and the window says where it is
followed. `RunBySession` is the ledger's run a session is (`autopilot_runs.session_id`), which a
session's Watch opens.

**Take over** (`launcher.Service.TakeOver`, `internal/core/adapters_takeover.go`). A run's
session is resumable as any is (`ResumableSession.RunID`, and `Fence`, the run's preset and
folder from its record's intent, `agentrun.Runner.TakenOf`). `TakeOver` asks core's
`TakeOvers` first: it tells Autopilot's engine (`Engine.TakeOver`, a command applied on the loop
before the run's end can be: a run still working is marked, so its end closes its match done
as `taken_over`, counting as no try and telling no Agent finished; an ended one closes its match
so when the match still waits on its interruption or the user stopped it), stops the run while
it works (`Runner.Stop`), and waits for its end to be told (`Runner.Wait`, at most 30 seconds),
so the session is recorded ended and its conversation whole. Then it resumes the session as
`Resume` does, with `ResumeRequest.Access`: `fence` (the default for a run's session) resumes
under the run's own fence, taken over (`launcher.Fence.TakenOver`, `agent.Access.TakenOver`):
the same tools, rules, hook, and sandbox, in `manual` rather than `dontAsk` for a fixed preset,
so anything the fence doesn't allow asks the user (`fenced: <preset>; asks you about the rest`);
`profile` resumes with the profile's own access, under the trust rule. A fence whose settings
are gone from the run's folder is `ErrNoFence`. Background runs pass
`--system-prompt-snapshot off`, so a resume replays no unattended note, and the resumed
session is a terminal session like any (`ResumedFrom` the run's), which the launcher's running
hold and the engine's busy check follow from there.

**The end.** A run is judged by the agent's `result` line, never its exit status (a Control-C
ends with `error_during_execution` and exit 0; `judge`, `outcome.go`): its time limit, the
quit (interrupted, so its match comes back under Needs you), the user's Stop, and a token
budget reached say so first; then `error_max_budget_usd` is out of budget, a result in error
after a `rate_limit_event` `rejected` is out of usage (with the limit's type and its reset),
any other error is the agent's own words, and success is *Finished:* with the report's summary
(`structured_output`, `ReportSchema`) or the agent's last message. The cost and the models are
the result's (`total_cost_usd`, `modelUsage`); a run on the user's plan (`apiKeySource: none`)
has an estimate (`Result.Estimated`, `autopilot_runs.cost_estimated`). The run's sandbox check
is read from its folder (`fence.Sandboxed` over its policy, `Outcome.Sandbox`): a sandbox that
leaked fails a run that ended on its own, whatever its result. A result line ends the
run whatever its program does next: the runner stops the group (`Callbacks.Over`), and after a
SIGKILL ends the processes still working in the worktree (`ProcSweeper`). At the end it reads
the worktree's commit and the files its commits changed (`gitread.Head`, `ChangedFiles`,
`git diff --name-only --no-renames`), which core notes on the item at the head the run started
from and its own last commit (`inbox.Store.NoteAgentChanges`), so a launch at either head holds
for an agent file it wrote. The
record holds the outcome; the runner tells `Engine.RunEnded` only once it does, and again from
the record after a restart (`Verify`, the kind's `Verifier`), so an end is never lost or told
twice. Each end done or failed is told once as **Agent finished** (`NoticeFinished`, through
`EngineConfig.Finished`), with its cost.

**Stopping and the quit.** `Stop` and `StopAll` reach the run engine at once
(`scriptrun.Engine.StopKind`, the stop ladder). The quit stops every run in the app unless the
user lets the agents finish: the quit dialog offers **Let them finish** while every agent's run
going may outlive the quit (`scriptrun.Run.OutlivesQuit`, a spec's `OutlivesQuit`: an agent
that keeps to a dollar budget of its own), and `scriptrun.Service.LetThemFinish` sets
`Engine.LetFinish` and quits; `QuitAnyway` and `KeepRunning` clear it. `StopAll` then lets such
runs go: each keeps its record as going, and the next start follows it again as an orphan,
stopping it at once when its time limit is past. A confirmed restart to update lets them go
too (`update.Runs.LetFinish`, set just before the quit is let through and cleared again when
the restart fails after it), and its question says which carry on (`update.Run.Outlives`).
With **Keep the Mac awake while agents work** on, `caffeinate -i -w <pid>` holds the Mac awake
for each run's program.

**Seams.** The outcome keeps the limit's type and reset (`Outcome.Limit`), which the profile
fallback rests a profile by. Another agent claims Take over with its spec's `Resume`
capability, its headless resume in `agent.Args`, and a fence it keeps in an interactive
session (`fenceKeeper`, `Fence.TakenOver`); the run view reads it through its stream parser
(`parserFor`), which every page and the memory share. Codex has all of them (below).

### Codex in the background

Codex runs in the background through the same planner, fence, runner, run view, and Take over
as Claude Code: everything it does differently is its spec's (`internal/agent/codex_headless.go`,
`codex_fence.go`) and its parser's (`internal/agentrun/stream/codex.go`), as measured on
codex-cli 0.163.0, its version floor (`codexFloor`, `Headless.MinVersion`).

- **The run.** `codex exec --json --ignore-user-config --ignore-rules`, the model and
  `model_reasoning_effort`, the fence's flags, `--output-schema <run folder>/report-schema.json`,
  the unattended note as `developer_instructions`, the extra arguments, and `-- -`, the prompt
  on stdin. `--ignore-user-config` leaves the profile's `config.toml` unread (its MCP servers,
  plugins, `notify` program, and the projects it trusts; the sign-in still comes from
  `CODEX_HOME`), so a branch's own `.codex/config.toml` and `.codex/hooks.json`, which Codex
  loads only in a folder that config trusts, never load: checked locally, a trusted branch's MCP
  command and its `hooks.json` hook ran, the latter under the hook bypass the fence needs, and
  with the flag neither did (`TestABranchsCodexConfigCantRunACommandInABackgroundRun` replays
  it). `--ignore-rules` leaves every execpolicy file unread. A Codex run reads no trust
  (`Headless.ReadsTrust` false), so `Backgrounds.Trust` grants none, and a Take over under the
  fence grants none either: Codex asks the user. Never `--approve-for-me` (it refuses
  `--sandbox` and lets a model approve escalations) or `--ephemeral` (the thread must be kept to
  be taken over). The report is the strict form of the schema (`agent.StrictSchema`:
  `additionalProperties` false, every property required, the optional ones nullable), written
  by `agent.HeadlessFiles`, which `Backgrounds.Fenced` adds to the run's files. The
  environment drops every `CODEX_*` variable but `CODEX_HOME`, which the plan sets per profile,
  and `CODEX_CA_CERTIFICATE`.
- **The fence.** Codex keeps every preset but GitHub tasks only (its sandbox cuts gh off from
  GitHub and its sign-in, and outside one only the hook would hold the run in): `KeepsPreset`
  says so, and the builder offers it off with that reason (`agent.Agent.Background.Presets`).
  Read only is `--sandbox read-only`, and since Codex reads through its shell, the policy lets
  its reading commands run (`fence.Spec.ShellReads`, set by its spec). The edit presets are
  `--sandbox workspace-write`, with `sandbox_workspace_write.writable_roots` naming the
  worktree's own git folder and the clone's `objects`, `refs`, and `logs` (so it can commit,
  never touching the clone's hooks or config) and the sandbox check's proof folder, and `/tmp`
  kept out (`exclude_slash_tmp`: workspace-write lets `/tmp` be written by default, which the
  sandbox check caught); Edit, test, and push turns the sandbox's network on for its one push (`network_access`), which reaches
  any host: the hook refuses the network tools, but a test program isn't held to GitHub and the
  registries as Claude Code's sandbox holds it. The profile's own is a click's access. Every
  preset passes `approval_policy="never"`, `web_search="disabled"`, and the fence hook as
  `-c hooks.PreToolUse=[{hooks=[{type="command",command=...}]}]` with
  `--dangerously-bypass-hook-trust`, without which a `-c` hook silently doesn't run; a hook
  with no matcher runs on every call. The hook's protocol is Claude Code's (`--fence-hook-v1`),
  and the policy knows Codex's tools: `Bash`, `apply_patch` (its patch's files decided as
  writes, `fence.PatchFiles`, so `AGENTS.md`, which the sandbox lets through, is refused), and
  `view_image`. Codex's `--json` never shows a call its hook refused, so the hook logs each in
  the run's folder (`fence.LogDenial`, `denials.jsonl`), which the run's outcome counts
  (`Outcome.Refused`, `Refusals`). The fence's own record of its flags is the run folder's
  `settings.json` (`codexFence`), which Take over finds the fence by, as for Claude Code. The
  argv needs the fence worked out for the worktree, so `agent.Fence` carries its `Policy` and
  `Hook` once `Backgrounds.Fenced` has them (`Access.Worked`, `launcher.Fence.Policy`), and Take
  over reads the policy back from the run's folder. This argv was run, locally and once, by a
  real codex-cli 0.163.0 against a stand-in for its provider (no usage spent): the app's own
  binary ran as the hook on every shell call, held commands until the sandbox check, which
  wrote its proof and was refused its probe, refused `git push origin HEAD:other` and `curl`,
  logged both, and let a write in the worktree through; Codex took the strict schema and the
  note as written.
- **The stream** (`agentrun/stream/codex.go`): `thread.started` names the thread, which Codex picks,
  so the runner links the run's session to it as soon as it comes
  (`SessionUpdate.AgentSessionID`, `sessions.RunUpdate.AgentSessionID`); items' starts and ends
  are the tools and their results (a command shown without the login shell Codex wraps it in);
  the last `agent_message` is the final message and, as JSON, the report; `turn.completed` is
  the end, with its tokens (no cost: a dollar budget or cap can't count it), `turn.failed` an
  error, and one with `usage_limit_reached`, `quota_exceeded`, a workspace's credits depleted,
  or "usage limit" is out of usage. Which limit and when it resets come from the thread's
  rollout (`agentrun.LimitReader`, core's `runLimits` over `observe/codex.Limits`: the last
  token count's `rate_limit_reached_type` and its window), so the profile fallback rests the
  profile until then. A stream with neither end was cut off: stopped, interrupted, or failed
  without a word. Its tokens come only as the turn ends (`Headless.UsageLive` false), so a
  token budget is checked then, and the builder says so; a dollar budget has no flag
  (`Headless.Budget` false), so its runs never outlive the quit and the builder says *Budget:
  not available*. After a SIGKILL, the processes it left in the worktree are swept, as Claude
  Code's.
- **Take over** is `codex resume <thread>` in the worktree, under the run's sandbox and
  writable folders again (a resume keeps no sandbox of its own), `--ask-for-approval on-request`
  so whatever the sandbox refuses is asked, the network off so a push asks first, and no hook,
  whose bypass would also run a trusted branch's hooks unreviewed in the interactive session,
  which can't leave the user's config unread. Its `developer_instructions` persist in the
  thread, so the resume's first message tells it someone answers now (`Options.TakeOver`,
  `codexTakeOverNote`), with the fence or the profile's permissions alike.

### Local actions: by a routine, Undo, and hands off

What the user sees is in `docs/desktop.md`'s
[Snooze, dismiss, and pin: by a routine](desktop.md#snooze-dismiss-and-pin-by-a-routine).

**The effects** (`effects.go`). The triage dispatcher (`triageDispatcher`) runs a match's Pin,
Snooze (its `days`, else three), Dismiss, or Restore through `Triage.Apply`, the inbox's own
triage for the routine (`inbox.Routines.Triage`, see `internal/inbox`'s **A routine's
triage**), whose record writes the `autopilot_effects` row in the triage's own transaction,
before its write: the match and step (unique), the routine, the item, the action, and the
item's local state before and after, as the inbox writes them (JSON, opaque here), with
`applied_at`. A step is a compare-and-set on the item's local state as its cycle read it,
before the item's facts (`EngineItems.LocalStates`, passed as `Job.Local`), and its record
checks, in the same transaction, that the routine isn't hands off the item, so the user's
own change, or reversal, while the step was on its way wins: `KeptChanged` ("Not done: the
item changed since this matched.", which also follows a snooze the wake rules ended or
another routine's step that landed first), or the routine left alone ("Left alone: you took
back what it did here.", the words that go with Let it act again). A step that changes
nothing records nothing, and its step's note says why ("You set it aside yourself, so it's
left as you left it."). A step a crash cut off after its change
committed runs again on the next start (`stepUnderWay`), finds its effect, and is done, so
nothing changes twice. Its limits are the local steps' (`runLocal`): a sign-out, Pause, and the
routine's active hours hold it, the match staying queued (`signed_out`, `paused`,
`active_hours`); the caps, the tries, the agents at once, the daily cap, readiness, the
outside contributor rule, and a catch-up never do; its own limit is the routine's Once per.
`Match.Effects` lists each match's, with `CanUndo` (neither undone nor taken back, and the
item still as it left it, `Triage.AsLeft`; false in a copy without the engine).

**Undo** (`Undo(ctx, ids []int64) (Undone, error)`, by effect: a match's `Effects`, or a
grouped line's `ActivityGroup.Undo`) is a command the loop applies. It reads the effects newest
first, skips those undone, taken back, or of an item gone (`Skipped`), and hands the rest to
`Triage.Undo` in one transaction, whose record marks each effect it put back `undone_at` and
leaves its routine hands off the item (`autopilot_hands_off`, "You undid what it did."). An
item that changed since stays as it is (`Left`). `Words` says it for a toast ("Put back 5 items.
1 changed since, so it's left as it is."). A GitHub action has no Undo.

**Hands off.** The user's own triage that takes back what a routine did reaches the engine
through the outcomes fan-out (`Outcome.Reversed`: the routine, the item, and what it did,
which core's `toEngineOutcomes` passes on), which tells the engine before the mirror of the
GitHub notifications, whose record can wait on a clear on its way to GitHub;
`Engine.Outcomes` records it at once, in whichever copy of the app the user did it, in one
transaction: that part's effects of the routine on the item (the pin, or the dismissal and
the snooze) applied by the outcome's time are `reversed_at`, so a step that landed after it
keeps its Undo, and the routine leaves the item alone (`InsertHandsOff`, with the words: "You
brought back what it snoozed.", "You snoozed it again yourself.", "You unpinned what it
pinned.", ...). Setting aside again what a routine's Restore brought back is no reversal:
only an Undo leaves such a routine hands off the item. A routine evaluates no item
it leaves alone, so it records no match there; an open match of it on the item that hasn't
started a run closes as `hands_off` ("Left alone: you took back what it did here."), which
opens again, if it never started a step, once the user lets it act again (`LetActAgain`).
`Explain` says so (`LeftAlone`), `Test` marks such items (`TestItem.LeftAlone`), and `Matching`
counts none of them.

**Activity's lines.** Each `ActivityDay` holds, beside its matches, a line per routine and
kind of local step among the page's matches, on the local day each step ran
(`ActivityGroup`: `Words` "Snoozed 6", the count, how many were undone and taken back, the
effects Undo would still put back, the matches it stands for, their items, and when the last
ran; `Key` names it across pages, whose matches may split a day). The first page also says
which items the routines leave alone (`ActivityPage.LeftAlone`, newest first, each with the
routine, the item's copy from its latest match, why, and since), for **Let it act again**.

**The simulator** runs the triage steps through the inbox's real triage (`sim.Triage`, the
adapter core's `inboxTriage` is, over the app the scenario built), and its scenarios include
the *Snooze passing bot PRs* template as `spec/testdata/templates` writes it (the inbox reasons,
Checks pass, a trusted bot's patch or minor update: snooze three days, once per head commit,
waking by the user's rules, at once in Ask first, a record alone in Dry run), Undo after the
user changed some of the items, and hands off and **Let it act again**.

**The simulator** (`internal/autopilot/sim`, test support only tests import) runs the real
engine and ledger over a real database, inside a `testing/synctest` bubble: a scripted GitHub
(`World`: items whose heads, checks, and states a scenario changes) that the inbox's own
service reads, so every item goes through the inbox's store; a clock with sleep gaps (`Sleep`
jumps with no tick, `Advance` lets the ticks run); the fake `Runner` and fake dispatchers, but
the real notices dispatcher and problem notices over a fake sink (`Notices`, which records each
notice and can fail); and crash-and-restart points over the same data (`Crash`, `Begin`,
`Restart`), and a second copy on the same data folder (`Copy`). A scripted pull request's
failed checks can be named (`FailNamed`), as *Tell me about security failures* needs, whose
template the simulator loads exactly as the gallery holds it. Its pool keeps one connection,
so a wait for the database is one the bubble sees. Each scenario ends with the ledger it
expects.

### The GitHub step

What the user sees is in `docs/desktop.md`'s
[What Autopilot may change on GitHub](desktop.md#what-autopilot-may-change-on-github).

**The seam** (`github.go`). The GitHub dispatcher (`NewGitHub`, which the engine registers
over the Service's `Config.GitHub` unless `EngineConfig.Dispatchers` names one) reaches the
app only through `GitHubActions`: `Read` (the item's sheet reads it again, one point, which
rewrites the inbox's card from the same answer), `Send` (one action, as its sheet sends it),
`MarkDone`, and `Preview`. Core's `githubSteps` (`internal/core/adapters_githubsteps.go`)
adapts the review sheet (`ReadAgain`, `Refresh`, `Act`, and `EditLabels`, its label edit of a
pull request at the issues endpoint), the issue sheet (`ReadAgain`, `Refresh`, `Act`, `Edit`),
the mirror of the GitHub notifications (`ClearItemThreads`, which Mark the GitHub notification
done runs to clear the item's threads), and
the reply filler the sheets share, each call marked `github.PurposeAutopilot`. A pull request's
actions are exactly `reviewActionOf`'s (`rebase_bot`, `update_branch`, `comment`), and
`TestTheGitHubStepReachesNoForbiddenAction` parses the adapter and fails on any other sheet
action it names, so no merge, approval, request for changes, close, or approval of workflow
runs is reachable from a GitHub step.

**One action, one run.** A GitHub step's actions go one at a time, each its own
`autopilot_runs` row (`runner = github`, `github_action` its kind), recorded before it is sent:
the step's stored state counts those that went (`stepRun.Action`), so a quit or a crash
interrupts only the one under way, which is never sent again on its own (`settleRun`), and the
user's OK sends that one alone. The read before each start is the sheet's (`itemReader`: the
dispatcher reads in place of the inbox's single-item read, so an action costs one point, not
two); the cycle after it decides the match again on what it stored, and the action is pinned
to the head that read found (`Job.Head`, `headNow`), which the cycle just decided on. The
adapter then decides from the sheet's own offers on that read (its `Refresh` serves it with no
request): absent or disabled is `NotOffered` (the match closes `not_offered`, with the sheet's
reason); offered with warnings the user hasn't accepted is `Warning` (the match waits
`warning`, its words the warnings); a head other than the one pinned is `PushedSince`, and a
sheet that couldn't read the item just before is `GitHubUnread`, which goes the same way. Only
then does it call the sheet's `Act` (`Accept` only for warnings the user accepted), whose own
`Decide` runs again, and whose refusals map the same way; the write gate's refusal before
anything was sent (`budget.RetryIn`, its pace, the reserve, or GitHub's hold) is `Held`. A
labels action sends only what changes the labels its read found, and says when there is
nothing (`GitHubNothing`).

**What each answer does** (`dispatchAnswered`): a sent action moves to the step's next, or
ends the step (`stepEnded`); `Warning`, `PushedSince`, and `Held` end the run `not_sent`,
counted as no try, and take back the attempt (`notSent`), so a match that sent nothing is
decided again like one that started nothing; `PushedSince` reads again before its next start,
and `Held` holds every GitHub action until the gate said the next may go (`holdWrites`,
`writes_held`). `Approve` of a match waiting on warnings records them on its step
(`stepRun.Accepted`), which the next send hands the sheet (`Job.Accept`), so a different
warning waits again; the routine's own Ask first OK accepts none. `ApproveBacklog` is Run on
them: the OK for every match of a routine waiting as a backlog. `SkipBacklog` is Skip them:
one command and one transaction that closes as `dismissed` (`you_skipped`) each of the matches
the card showed (its IDs) that still waits as the routine's backlog, a compare-and-set on what
the card showed, so a match recorded since still waits and one let go or closed meanwhile is
left as it is; when one can't be closed, none is.

**Limits.** A match is one try and one of its routine's runs a day however many runs it starts
(`history` and `dayCounts` count distinct matches; `limits` holds only a match's first run to
the tries, the routine's runs, and the wait), while the GitHub actions a day count each one
sent (a `not_sent` run counts toward nothing). Autopilot keeps under the write gate's edit
lane (60 a minute, 400 an hour): its GitHub actions an hour (`Config.ActionsPerHour`, 120 by
default, at most 300), and a sixth of that a minute (`actionsPerMinute`, at most
`MaxActionsPerMinute`, 40), from the runs started in the last minute and hour, manual ones too (`pace`, `writes_held`); the gate's own pace still holds
under it. An outside contributor's item waits for the user's OK in every mode
(`outside_contributor`), and the first pass's cap (`FirstCap`) holds a routine with a GitHub
action, as above. Mark the GitHub notification done is a local step: it runs at once, through
the chore lane, recorded as no run.

**Replies.** A reply action posts a comment's reply alone: `LibraryPrompt.ReplyFor` names the
review a library reply is written for, and `promptProblem` refuses an approval's (LGTM,
Update OK) or a request for changes' in `Describe` and `Save`; the engine checks the step's
replies again on its worker right before each action (`promptHold`, before the read, so a reply
changed since holds the action back as `blocked: not_ready` and spends no point), and the
adapter refuses one that isn't a comment's (`ErrNotACommentReply`) before filling it in.

**Preview** (`PreviewActions`, bound) builds what a draft's GitHub actions would send one item,
through `GitHubActions.Preview`: the rebase's comment, the branch's update, the reply filled in
for the item, or the labels it adds and takes off, with why one can't be built; it sends
nothing.

**The simulator** runs the GitHub step through the real dispatcher over a fake of the sheets
(`sim.GitHub`, over the scripted World: its read is the inbox's own single-item read, a send is
pinned to the World's head, and a scenario can withdraw an offer, add warnings, have the gate
refuse, or change the World as a send arrives), with a fake prompt library (`sim.Library`);
its scenarios include *Ask Dependabot to rebase what conflicts* as the template gallery holds
it.

### Notices, the menu bar, and the window's close

**The notices** (`notices.go`). The notices dispatcher (`NewNotices`, `catalog.DispatchNotices`)
runs Notify me and Tell me as local steps: each becomes a `Notice` handed to a `NoticeSink`,
which core adapts over the notifications (`internal/core/adapters_notices.go`,
`autopilotNotices`), so the engine never imports notify. Notify me tells why the match matched
(the trigger's words and each condition that held, with what it compared, on one line, at most
280 characters) and the user's own line; Tell me says it reached the step and how each step
before it ended, an outcome named as Only if names it (the richer words of what happened come
with the steps that make outcomes). Either is an alert (`NoticeAlert`) about the match's item,
headed by the routine's name, that opens the match's card, with when the match was made. Its
banner is the step's own choice (`NotifyStep.Banner`), else the routine's (`spec.Notices.Alert`),
else the settings'. A notice that can't be told fails its step, and the match says a step
failed. Each notice is told once whatever happens to the engine: its `Key` names it (a
step's by its match, when the match was made, the step, and the try; a problem's by the
problem and when it began, a start the engine saves before it tells it), and the
notifications keep one entry per key, so a step that runs again because a quit came before
the loop heard it was told, or a problem told again after a crash, adds no second entry or
banner. A cycle hands its notices over together as it ends (`pass.tellNotices`), each on a
worker: with more than one, or when any comes from a routine catching up, each is a `Burst`,
which core gathers
(`notify.Dispatcher.Tell` with gather, `GatherWindow`), so more than three at once show as one
banner (*4 new from Autopilot*, *4 routines need you*); a lone one shows at once. A Dry run
tells nothing. Problems go through `NewProblemNotices` over the same sink: a `NoticeProblem`
about no item, its line the routine's name ("A routine" when it can't be read) and its body the
problem, opening the routine, with when the routine was made; several untold at once are a
burst. The other kinds (`NoticeNeedsYou`, `NoticeFinished`, `NoticeChecks`) are declared for
the parts that make their moments. Core maps each kind to the notifications' own
(`noticeKinds`; a test holds the map to `notifyprefs.AutopilotKinds`).

**The notifications' side** (`internal/notify`, `internal/notifyprefs`). The five kinds,
`autopilot_alert`, `autopilot_needs_you`, `autopilot_finished`, `autopilot_checks`, and
`autopilot_problem`, are all on by default (`notifyprefs.AutopilotKinds`, `Kind.Autopilot`),
titled *Autopilot alert* (an alert's banner is headed by its routine, `Entry.Heading`), *Needs
your OK*, *Agent finished*, *Checks after a fix*, and *Autopilot problem*.
`notifications` (migration `0101_inbox`) has a `heading` and the Autopilot target
(`autopilot_routine`, `autopilot_match`, `autopilot_run`, `autopilot_created`, RFC 3339 of the
most specific one's creation, since Clear history hands IDs out again), and a `once_key`
under a partial unique index. An `Entry` now names
its item too (`ItemID`, `ItemKind`, `Number`; an item no longer stored is recorded without its
ID, as the log forgets an item that goes), so an Autopilot entry about an item is kept by the
inbox's prune (`KeptItems`), opens its item, and is marked read when that item's sheet opens,
as arrivals are. `Entry.Banner` is an entry's own choice of banner over its kind's switch (the
user's allowing banners holds either way). `Entry.Key` (the `once_key` column, unique while
set) keeps an entry once while the log holds it: a repeat is refused as told
(`ErrAlreadyTold` from `RecordEvent`), banner and all, and an entry with no key is never
refused. `Dispatcher.Tell(ctx, entry, gather)` records and shows (now, or gathered) and
returns the error recording failed with, nil for a repeat; `Notify` and `Gather` are it,
logged. The banner (`Banner.Autopilot`) and the click (`desktop.BannerData`,
`BannerClicked`, `event.RevealItem.Autopilot`, an `event.AutopilotTarget`, whose `Activity`
asks for Activity rather than one match, as the menu bar's row of what needs the user does)
carry the target. A summary of Autopilot's alone is titled *N new from Autopilot*.

**At a glance** (`glance.go`). The bound `Glance` reads the database alone: whether Autopilot
(beta) is on (`On`), the routines not deleted and those the engine runs (on, of the account
signed in, and loading; none while no one is signed in, the engine's last cycle found the
sign-in gone, or Autopilot is off), Pause Autopilot, the
open matches waiting for the user (`StateWaiting`), and the agent runs going (starting,
running, or stopping, in the background or the terminal, never a GitHub action's), oldest
first. `KeepsRunning` is a routine the engine runs or an agent working, so after a switch to
another account, whose routines don't run, or with Autopilot off, a close quits again as
before, but while an agent it started before still works.
The window reads it as it opens and again on each `autopilot:updated` (`useAutopilot`): the
Autopilot tab counts its agents working and marks its matches waiting with an amber dot. While
Autopilot (beta) is on, the window names Autopilot whether or not a routine exists: Settings →
Notifications lists the five switches, and the Notifications page offers the Autopilot type;
off, neither does.

**The menu bar** (`internal/menubar/autopilot.go`, pure). `Summarize(snap, waiting, autopilot,
now)` works out `Summary.Autopilot` from an `AutopilotState` (core's `ShellAutopilot` adapts
the glance, and hands an empty one while Autopilot (beta) is off, `shellAutopilotState`):
shown only while a routine exists, the first `PerSection` agent runs
("example-repo #88 · Claude · 6m", each opening its run) and how many more, the matches
waiting ("1 needs your OK", opening Activity), and whether Pause Autopilot is ticked;
`NextChange` is when a run's minutes next change. The count beside the icon stays the
inbox's. The tray (`cmd/nilbox/tray_desktop.go`) draws it after the lists, under its heading,
with the app's first checkbox row (`AddCheckbox`), and every click goes through
`Shell.AutopilotAction`: a run or Activity reveals its target, and Pause flips what the menu
showed through `desktop.Autopilot.SetPaused`, core's call to the settings' own
`SetAutopilotPaused`, the one writer of `autopilot.paused`, then reads Autopilot again and
draws the menu in full, so a write that failed takes back the tick the menu put on the
click itself.

**The shell** (`internal/desktop/autopilot.go`). The shell reads Autopilot's state at its
start and on every `autopilot:updated`, in its one redraw loop, whether or not the icon
shows. Core's relay sends one on each `AutopilotChanged`, so a Pause made in Settings reaches
the tick of the copy it was made in at once, whether or not that copy holds the engine; a
Pause made in another copy reaches a copy that doesn't hold the engine on its next engine
tick (`followElsewhere`, within a minute), and the copy that holds it on its next cycle. It
keeps what it last read of keeping the app running and of Pause. `HidesOnClose` answers at
once from that alone: yes while the icon shows, and yes while Autopilot has a routine on or an
agent working (no routine counts while Autopilot (beta) is off, `Glance.KeepsRunning`),
whatever the icon setting, since the close would otherwise quit the app and
quietly stop them; the Dock icon brings the window back (`ApplicationShouldHandleReopen`).
The window's close hook asks `Shell.WindowClosing`, whose pure rule is `CloseOf`
(`internal/desktop/close.go`): the close goes through while the shell is stopping (the
runtime's own shutdown closes the window once the services have stopped, so a quit's own close
never quits again), the window only hides while `HidesOnClose` says so, and otherwise the
window stays and the shell quits the normal way (`Quit`), so `ShouldQuit` asks about runs going,
or holds the quit for a move, in a window that still exists. The app never quits on the last
window's close by itself: `ApplicationShouldTerminateAfterLastWindowClosed` stays off
(`macAppOptions`), since AppKit asks it as the last visible window hides too, and with it on a
close that only hid the window ended the app a moment later. The window comes back with
nothing said about the hide (an earlier build's `ui.keptRunningTold` is left stored, and
nothing reads it). A click on an Autopilot notification or banner about an item opens that
item, as an inbox arrival's does
(`bannerOpens` falls back to the item, `opensAutopilot` is an entry about no item alone), and
one about no item, as a problem is, or on a menu row of the group, opens the Autopilot tab at
what its target names (**The Autopilot tab**, below).

### Easy mode: templates, from an item, why, export and import, graduating

What the user sees is in `docs/desktop.md`'s
[Templates and quick setup](desktop.md#templates-and-quick-setup) and
[Export and import](desktop.md#export-and-import).

**The gallery** (`gallery.go`). Each template is a spec in its own file, `gallery/<key>.json`,
embedded in the build, with its words in Go (`galleryInfo`: name, icon, summary, the mode it
suggests, its `Blanks`, and the `Signals` that suggest it); `Gallery` reads them, and
`TemplateByKey` one, which the simulator's scenarios load "exactly as the gallery holds it". A
blank is a part of the spec quick setup fills: a search's Where (`BlankWhere`, at a source) or
a condition's check names (`BlankChecks`, at a condition, `Required` when the spec is invalid
until it is filled). Every template names its prompts by starter, so every Mac has them. The
bound `Templates` serves them as this Mac sees them: the sentence (empty while a required blank
is), what this build can't run of it (the draft leaf's `Offers.Needs`: an agent's place, then
each step and GitHub action the build's `Offers` don't offer, in plain words, and `NeedsWords`
as a sentence;
none, in this build, which runs every template),
the starters it runs with whether the library holds each (`TemplatePrompt.InLibrary`, which the
prompts' `AddStarter` puts back), and why it is suggested (`suggestedWhy` over
`TemplateSignals`: the clones whose CI is go-fortress's, from setup's `Signals`; Dependabot's
pull requests in the inbox this week and bots' that ask for the user's review or are assigned
to them, from the
inbox's `BotPullRequests`, `CountBotPullRequests`; core's `templateSignals`), the suggested
first. Since the needs are worked out from the offers on every read, a template is never
locked: one with a step a later catalog adds before its dispatcher would say what it needs,
and work as that step is offered, with no change to it. A routine made from one is
an ordinary routine (`Draft.TemplateKey`), in the mode new routines start in. The tests hold
every template to its file, its blanks to its spec, its starters to `prompts.Starters`, its
fixture items (it matches each match and none of its misses, and each match is in its watch),
and its sentence to `testdata/gallery/<key>.golden`; core's `TestEveryShippedTemplateSavesCleanly`
saves each through `Describe` and `Save` over the real library, held back only by where its
agent runs while the background isn't offered.

**Quick setup's readiness.** `DraftReadiness(draft)` checks what an unsaved draft needs
(`Needs`), each answered from the checks kept within `readyFor` or checked again cheaply
(never `fresh`, so never GitHub's sign-in), the place by the build's offers.

**From an item** (`fromitem.go`). `FromItem(itemID)` reads the stored item's facts and fills a
`Draft` (never saved): a search of its kind, by its author (`@me` for the viewer, a bot as
`app/<slug>`), in its owner (`@me` for the viewer's own, else `org:<owner>`); Checks fail
(with `checks.failedNames` *contains any of* the names that failed), else Merge conflicts, else
Changes requested, else Appears, as the item stands; and an agent step running the item's own
default prompt (`PromptRef.ItemDefault`) where `Offers.PreferredPlace` says (the background
once offered, else the first place that is); named apart from the user's routines
(`itemRoutineName`, `freeName`), in the mode new routines start in. Its labels and its update
type come as `Suggestions`, and the draft's decision on the item now (`Match`, `Trace`).

**Why didn't a routine run?** `Explain` (`why.go`) adds `Routine` (Autopilot off or paused, the
routine off, another account's, in Dry run, and each problem readiness keeps for it), `Watch`
(for an item not in the watch, a sentence a source, from its facts: the lists it is and isn't
in, or the first search field it doesn't fit, `searchMiss`: kind, author, where, labels, base,
head prefix, draft; a search it fits says the next refresh brings it), and `Records`, every
match of the routine on the item, newest first, at most `maxExplainRecords`.

**Export and import** (`transfer.go`). `Export(path, ids, withPrompts)` writes the routines with
ids (all for none) as a `Bundle` through `appdata.WriteFile`, with `Bundle.Prompts` holding the
prompts they name by nickname in the prompts' own format when asked (`PromptTransfer`, core's
`routinePromptTransfer` over the prompts' `RoutinePrompts`, a type of its own, never bound:
`Bundled`, `Arrivals`, `AddNew`). `PreviewImport(path)` reads a bundle (`appdata.ReadFile`, at
most `MaxBundleBytes`) and returns its SHA-256, each routine's sentence or why it won't load and
the name it arrives under, each bundled prompt's `Arrives` (new, the same, or differs, the copy
here kept), and the `MissingRef`s: a prompt neither the library nor the bundle's new prompts
hold (with the library's prompts of its purpose as choices), an agent whose program this Mac
lacks (`Profiles`, core's `routineProfiles` over the profiles: it inherits by itself, its
profiles with it), and a profile none of its agents has (with their profiles as choices).
`Import` refuses a file whose SHA-256 changed (`ErrChanged`), adds the bundle's new prompts
first, maps each missing reference as the request's `RefChoice`s say, else to inherit
(`refMapper`: the item's own default prompt, the profile a click uses, a fallback dropped; a
reply with no choice stays as it is, and the engine's prompt check holds its runs), and stores
every routine through `keptFrom` and `insertKept`, switched off, the account signed in's, at the
end of the list under a free name, one that won't load as it was.

**Graduating** (`graduate.go`, migration `0100_settings`). `List` gives a routine
`Figures.Graduate` while it is on, the account signed in's, loading, in Ask first, with a step
that asks first, and its last `GraduateAfter` (5) answered real matches since
`graduate_declined_at` (`ListRoutineAsked`: approved or skipped by the user, newest first) are
each a clean approval (`cleanApproval`: approved, done as ran, no step failed, no agent that
pushed nothing, no checks still failing). `DeclineGraduate` stamps `graduate_declined_at`, so
the count starts again; nothing changes a mode but the user's `SetMode`.

### Schedules, the digest, the script step, and a routine's week

What the user sees is in `docs/desktop.md`'s [Building a routine](desktop.md#building-a-routine)
and [Testing a routine](desktop.md#testing-a-routine).

**Schedules.** On a schedule (`catalog.EventSchedule`, `spec.Schedule`) is a state the
evaluator decides from the moment it is given, as every trigger is: it holds from the
schedule's time of day, in that moment's own location, to the end of a day it is due (every
day, a weekday, or the week's day), under the fire key `schedule:<local date>`. So it needs
nothing of its own in the engine: the 1-minute tick runs a cycle, which reaches the time; a
time the Mac slept through holds when it wakes (the sim's `Sleep` jumps with no tick, then the
first cycle fires it), and only that day; daylight saving and a new time zone keep the time of
day (`time.Date` in the clock's location; the sim's `Clock.Travel`), and a clock set back after
it fired finds the day's key recorded. The scenarios in `sim/schedule_test.go` hold each case.
`Schedule.Style` (`catalog.ScheduleStyles`: `each`, written as none, or `digest`) says how
it tells of what it matched; `spec.Validate` holds a digest's Notify me to the first step, so
the matches a time makes, which run their first step in the cycle that records them, tell
together.

**The digest** (`digest.go`). As a cycle ends, `tellNotices` takes out the Notify me jobs of a
digest routine's matches (`pass.digests`; a manual run's tells on its own) and hands each
routine's as one job carrying a `Digest` to the notices dispatcher (`dispatchDigest`: one
`Dispatch`, its answer applied to each match, so each step is done once the notice is told).
`digestOf` is pure: it puts each item in the first group that holds of it (a pull request
failing, in conflict, with changes requested, behind, with checks running, a draft, approved,
else waiting for review; an issue quiet a week or more, else open; a value not known yet passes
on), counts the groups in that order, lists the items so, and names them by kind and, when one
bot opened all, by it. The notice (`digestNotice`) is an alert about no item, headed by the
routine, *3 Dependabot pull requests need you*, *2 failing, 1 waiting for review: acme-org/web
#88, …* (five named, the rest counted), keyed by the routine, when it was made, and a hash of
its matches, so a digest told again after a quit is told once and a later one is its own.

**Run one of my scripts** (`scripts.go`, `catalog.StepScript`, `spec.ScriptStep`: the
script's ID, and the name it was picked under, for the sentence and an export). It is an
asks-first step of the scripts part (`catalog.DispatchScripts`), recorded as a run under
runner `script` (`autopilot_runs`' check takes that runner) and handed to `EngineConfig.Scripts`, a `Runner`, as
an agent's is: Dispatch starts it and returns Running, and its end comes through `RunEnded`;
the engine routes a run's `Alive`, a Stop, and Stop all by its runner (`runnerFor`). Its limits
are a GitHub action's: the tries, the routine's runs a day, Pause, the active hours, an outside
contributor's item waiting for the user, and one run per item; it takes no agent's slot and
counts toward neither the agents' caps nor the GitHub actions' (`isAgentRunner`). The user's
own scripts reach the engine and the builder through `MyScripts` (core's `myScripts`, over
`scripts.Routines.Mine`), which the bound `MyScripts` lists, and which `Save` and `Describe`
check a step's ID against (the draft leaf's `scriptProblems`). **Ask before running** is the
engine's to honor,
since Repo scripts' Run leaves it to its window: the gate's first check (`scriptAsks`) holds a
script step whose script asks until the step's `Confirmed` holds `ScriptKey(id, look)`, the
script as the user saw it (its look hashes its name, command, place, and the setting), whatever
the mode, a manual run's and an approved match's too, waiting `script_asks` with the hold's key
on the step, which `Approve` confirms (`confirmHold`); a script edited since asks again.
Core's runner (`adapters_scriptsteps.go`, `scriptSteps`) checks it once more as it starts
(`ScriptAsks`), finds the item's clone as a launch does (`launcher.CloneTarget`), and runs it
through `scripts.Routines.Run`, the same spec Repo scripts' Run works out, always in the app,
refusing a script saved otherwise since its check (`ErrChanged`), its end told back from the
run engine's `Recorded` with how it ended (`RoutineEnd`, from its `Finish`: the user's Stop, its
time limit, the app's quit, which interrupts it). A busy clone waits queued, Scripts turned off
or a script that can't run waits not ready, no clone waits for the user, and a script gone or
not showing in the clone fails the step. A step names its script by its ID and the name it was
picked under: a script saved under another name waits not ready, and `Save` and `Describe` say
so (`renamedWords`), since an ID a routine brought from another Mac (an export, a backup) may
name another script here; the builder offers it to be picked again. After a restart, `Alive` reads the run engine's record
of the run's `script_run_id` and tells the engine an end it didn't hear.

**Run on the matches now** (`runon.go`). The bound `RunOnMatches(id, items)` is a command on the
loop: for each item the saved routine matches now and would run on (`TestItem.RunsNow`: no
record of its occurrence, a record that closed before it ran, or a baseline), it records the
routine's own match for that occurrence, approved, or opens that record again approved, so the
routine never runs on the occurrence twice and an Ask first routine doesn't ask again; every
other limit still holds it. A routine that is off, broken, or another account's is refused,
and so is a list longer than a test's.

**A routine's week** (`stats.go`). The bound `Stats(id)` reads the routine's runs started in
the last seven days with their matches' steps (`ListRoutineWeekRuns`) and counts, in Go
(`weekStats`), its agents' runs and its scripts', the fixes (an agent's run whose verdict
passed, or whose match's Wait for the checks after its pushed step passed, once per match),
merges, reviews posted, and GitHub actions GitHub took (each by the kind of the step its run
ran), and what the runs reported spending: the dollars, how many runs reported one, and how
many of those an estimate (`cost_estimated`, which a subscription's sign-in reports as the
API's price; the window says *about* for every figure), and the tokens. A run that never
started counts nowhere. A run in the terminal reports neither, and its session's tokens are
the session panel's (`internal/sessions`' `Usage`, from the agent's own files). A background
run's session reads its run's instead: `Usage` asks its `RunTokens` (core's
`backgroundTokens`, over `Engine.BackgroundTokens`, by the run engine's run through the
index `0016` adds on `script_run_id`), which the runner keeps current (`Engine.RunUsage`),
never the transcript, and marks them `FromRun`.

**The cycle's reads.** A cycle takes each routine's matches by occurrence and what it first saw
of each item from the ledger's keys the engine keeps in memory, reads the items each leaves
alone and its resets of the tries in two reads (`readLedger`, which Run on the matches now
shares), indexes each routine's last closed match by item once (`lastClosed`, which the
evaluator's history reads), and keeps its own state only when it changed beyond the last cycle
(`keepCycle`). `BenchmarkEngineCycle` measures it
([performance.md](performance.md#the-engines-cycle)).

**The ledger's keys in memory** (`ledgerindex.go`, `ledgerIndex`, the engine's ledger's `index`,
which only the loop reads or changes) hold every match with an item, by routine and occurrence
(`keyRow`: its ID, state, reason, and whether it started a step, `startedAny`, which is all
`record`, `reopensRow`, `runsNow`, and `lastClosed` read), and every baseline, by routine and
item. The engine is the ledger's only writer, so each of its writes notes what it wrote where
it writes it: the ledger's `insert` and `update` the match's key, `saveBaseline` the baseline
once its `:execrows` query says it wrote one, and the prune's `clear` the matches it deletes. A
write through `ledger.inTx` notes into that transaction's list (`begin`, keyed by its
`*queries.Queries`), applied once it commits and dropped when it rolls back (`end`), and a
write through a transaction the ledger didn't open has the keys built again. They are built
from the database (`ListMatchKeys`, `ListAllBaselines`) on their first use, whenever the
engine takes the lock (`takeLock`, since another copy of the app may have written the ledger
while it held it), and whenever one count of the rows (`CountLedgerKeys`), the only read a
steady cycle makes of them, differs from theirs: what changes these rows besides the engine
only deletes (the inbox's prune of an old item takes its baselines with it and sets its
matches' `item_id` NULL; the hourly prune's forgetting of deleted routines' baselines), so a
count tells it. The items a routine leaves alone are read every cycle, since the user's
reversal records them in whichever copy of the app it happens. `TestOnlyTheLedgerWritesWhatItsKeysHold`
holds every query that writes these rows to the one function that notes it, and
`EngineConfig.CheckLedger` has the engine compare the keys with the database (`diff`) after
every cycle, command, tick, and worker's result while it holds the lock: the simulator sets it
for every scenario and fails on any difference (`sim.drift`).

### The Autopilot tab

The window side lives in `frontend/src/components/autopilot/` (the tab itself,
`AutopilotView`, loaded on its first visit and kept mounted in an `Activity`) and
`frontend/src/lib/`; it reaches Go only through the platform module.

- **The run view** (`RunView`, keyed by its run, beside Activity as the Runs page's run panel
  is beside its list) opens on a target with a `runId` (Agent finished, the menu bar, the
  sheets' Autopilot line under `WorkLine`) and on Working's **Watch**; a session's Watch and the
  launch button's (`AutopilotRowsContext`'s `onWatch`, App's `watchSessionWith`) find the run by
  its session first (`RunBySession`). `useRunTimeline` holds its timeline: the first page, then
  what is new on each `autopilot:run` and `autopilot:updated`, one read at a time, until the
  run ends, and **Show earlier events** a page back from the first event shown
  (`RunEventsBefore`), each event once by its key (`lib/runView.ts`'s `joinEvents`), whose pure
  half also words the rows (`timelineRows`), the header, the plan's checklist, the diff, and the
  sheets' line (`sheetLine`). `TakeOverDialog` is the one take over the run view, the session
  panel, and the launch button's hold dialog share (`takeOverRun`), its box the profile's
  permissions; a hold it meets asks as `LaunchHoldDialog` does. `LaunchHoldDialog` with a
  background run offers **Watch it** and **Take over**, never Start another.

- **Autopilot (beta).** App reads the switch from the settings (`autopilotOn`, off until they
  load). Off, the window shows none of Autopilot: `AppHeader` gets `autopilot={null}` and
  draws no tab (a window on it shows the inbox, as one on a Scripts tab gone does), the rows
  get no `AutopilotRowsContext`, so no chip and no *by ⚡* mark (`useMarkedBy`), and no
  `LatestMatches` is read; the settings page leaves out Autopilot's section and group, and
  Settings → Notifications its banner switches (`hiddenOf`, `shownSection`, and the index's
  entries marked `autopilot`, which **Find a setting** leaves out too, `matchSettings(query,
  hidden)`), opening **Labs** where Autopilot's section was asked for; the Notifications page's
  Type offers no Autopilot (`AppNotificationsTab`'s `autopilot`), and a choice of it reads as
  every type; and `openAutopilot` says *Autopilot is off. Turn it on in Settings → Labs to see
  this.* rather than show the tab. **Labs** (`LabsSection`) holds the switch, **Autopilot
  (beta)**. On, everything is as below, and the tab's label and the section's heading carry a
  `BetaBadge` (rem text, the accent's tokens), beside the name, never in it, so the tab and
  the region keep their names.

- **`lib/autopilot.ts`** holds the words, all pure: the modes, the tab's badge
  (`autopilotBadge`: the glance's agents working, an amber dot for its matches waiting, a red
  one for the user's routines that are broken or have an engine problem, none for another
  account's, and `live`, `liveOf`: `working` while an agent runs, `on` while Autopilot is on,
  unpaused, signed in, run by this copy, with a routine on, else `off`, which `AppHeader`
  shows as a dot before the tab's name, a steady accent ring for `on`, `animate-breathe` under
  `motion-safe:` for `working`, with `onTitle` as the tooltip when nothing else is to say),
  when Autopilot runs (`cadenceOf`, from the glance, the engine's `LastCycle`, which every
  cycle's `autopilot:updated` keeps fresh, and `GitHubReads`: the settings' inbox and checks
  intervals, and the inbox's last read, which gives the next; `howWords` says how often, in
  the settings' numbers; `Cadence.tsx` shows it under Activity's title, with **Refresh now**
  through the inbox's `refresh`, and its first line in an enabled routine's pane, nothing
  while a banner says what stops everything), each
  routine's status line (`routineStatus`, from the routine's own `Figures`: its merge past the
  rules while the switch is off, in red (`pastRulesBlocked`, which also has the pane show
  `pastRulesOffText` with **Open Autopilot settings**), what waits, what it did today, what it
  would have done today in Dry run, else when it last ran or would have run), the chip (`chipOf`, from the newest match: a Dry run's *Would run*
  while it is open or done, an open state's words, a baseline's (a Dry run's too), a closed
  or skipped match's reason (a Dry run's too), each in words of its own, `reasonChips`, then
  a local step's verb or *Undone* and *Taken back*, a notice's *Told you*, else *Ran*),
  which matches Activity lists (`inActivity`: done, skipped, or
  dismissed, or closed after one of its steps ran, under Worth knowing; `notListedText` says
  what became of one it doesn't), the question
  (`matchingWords`), and Activity's lines (`activityLines`: each day's grouped lines of local
  steps in place of the matches they stand for, the matches that did something by
  themselves, and the items left alone, newest first, filtered by routine and by result,
  `filters` comparing the filter by value; open matches wait for the sections that show
  them). `mergeDays` joins a day, and a grouped line, two pages split, by the group's `Key`;
  `sameSecond` compares a target's time to a match's, since a target is written to the
  second. `inChunks` splits a list of ids into the backend's chunks of 500 (`maxIDs`), for
  every read by id the window sends.
- **`lib/useAutopilot.ts`** reads the glance, the routines, the engine, and every routine's
  name (`RoutineNames`, the deleted ones' too, so a mark or a line still names its routine)
  as the window opens and on every `autopilot:updated`, for App to share, and the routines
  again as the window's clock enters a new local day, so *today* turns over at midnight
  while nothing changes; `useAutopilotChips` reads `LatestMatches` for the ids a list
  shows, a chunk at a time (`all` and `andThen` in `lib/call.ts`, which keep the bound
  calls' cancel), and `useAutopilotUpcoming` reads `Upcoming` for the same ids, by item,
  again on every `autopilot:updated`; App and the Watching list hand both to the rows
  (`AutopilotRows.upcoming`). `AutopilotChip` shows `chipView`'s choice: an open match's
  chip, else the routines that will run (dashed, `upcomingChipText`, its countdown through
  `useNowAs`, `upcomingTitle` as its tooltip), else the newest match's; its panel's **Up
  next** (`UpNextEntry`: `upcomingLine`, `plannedText`, **Open routine** through
  `routineTarget`, **Leave this one alone** and its **Undo**) keeps the list it opened with
  while it stays open, above the item's history. **`lib/useAutopilotActivity.ts`** holds one pager per tab, outside React
  (`useSyncExternalStore`), which keeps Activity as stretches, newest first, each ending
  where the next begins: Activity's newest page (100 matches) as the tab first shows, and a
  page more by `next` as the user asks for more, each a stretch of its own, never merged.
  On `autopilot:updated` it reads the newest stretch alone again: the first page itself
  (`Activity(0, 100)`, 100 rows) while nothing past it is loaded or on its way, else
  `ActivityFrom` its own first match on (`ActivityFrom(lower, 0, 500)`: the first page and
  what was recorded since, one call); past 500, that page's newest 100 become the newest
  stretch and the rest is read again in slices of 500, so nothing loaded goes and the
  newest stretch starts a page long again. Every stretch is read again, each by its own
  bounds (`ActivityFrom(lower, upper, 500)`, one call a page loaded, in slices past 500),
  once a line's own action (Undo, Try again, Let it act again) goes through and each time
  the tab shows again, since its effects stop while it is hidden; until that read lands,
  `current` is false, and a request for a match waits for it. One read runs at a time with
  one more waiting, a read of every stretch drops a read of the newest under way and one
  waiting, hiding cancels every read, and a read that lands after a page more keeps that
  page. A read again that no longer finds the match the newest stretch begins at, as it was
  recorded, finds the history cleared (Clear history lets SQLite hand the IDs out again from
  1), and starts again from the newest page.
- **`AutopilotRowsContext`** (`AutopilotChip.tsx`) carries the routines' names and the chips
  to every row, App's around the inbox and the tab, and the Watching list's own chips around
  its rows. `ItemRow` never reads it: each list (`ListSection`, `WatchingList`) hands every
  row a plain `autopilot` string, `rowAutopilot` of the item's match (`"needs:<routine>"`,
  `"working:<routine>"`, or `''`), which the card's next step reads, so a memo'd `ItemRow`
  renders again only when that string changes, and otherwise only in the parts that read the
  context themselves: the chip, its marks (*Snoozed by ⚡ ...*, the pin's routine), **Decide**,
  and the open peek. The chip reads `ItemHistory` only as its panel opens.
- **`WatchingList`** reads `Watching`, then the inbox's `ItemsByID` a chunk at a time
  (`readItems`, again on `inbox:updated`), and renders the inbox's own `ItemRow`s, with the
  review and issue sheets over the list through `useSheetHost` (`useSheetHost.tsx`: the
  sheets, the launches resolved with `ResolveItems`, the toasts, and the rows' triage and
  edits, which are the inbox's, through `components/useItemActions.ts`, which `InboxView`
  shares). It is keyed by routine, and marks and scrolls to the row Activity's **Open** asks
  for once (`highlight`, a number per ask, handed back with `onHighlighted`), never opening
  anything itself. An ask it can't show is handed back as soon as it knows: once it has
  read what it watches without the item, or couldn't read it. The tab drops an ask as soon
  as another routine or Activity is chosen, or the question about unsaved changes that
  going to its routine asked is answered Keep editing, so none lands later unasked; while
  that question is open, the ask waits.
- **Open** on an Activity line goes to the item whatever became of its routine: its card in
  the inbox while a list there holds it; else its row in a routine that still watches it
  (`Watching`, the line's own routine first, local reads alone); else its sheet, through the
  tab's own `useSheetHost`, while the inbox stores it (`ItemsByID`, read again once on
  `inbox:updated`, which follows every change to the stored inbox, the sheet's own triage
  included, so the sheet offers what fits the item as it is); else its page on GitHub.
- **`useRoutineChange`** switches a routine, changes its mode, and saves the builder's draft
  over it: switching one on, taking one that is on out of Dry run, or saving a change to what
  one that is on watches, fires on, checks, or fires once per (`watchChanged`) first asks
  `Matching` (with the draft, for a save), and with any match, any it can't tell yet, or a
  count that may be short (`incomplete`) asks the question in a `ConfirmDialog` whose two
  buttons are the answers (`SetEnabled`, `SetMode`, or `Save` with `current`, `run` or
  `fromNow`) and whose `onDismiss` makes Escape neither. A request may name where its failure
  goes (the builder's footer, for a save), else it is a toast; `asking` says the question is
  open.
- **The builder** (`builder/`) is the routine itself: `RoutineBuilder` lays out the name, a new
  routine's mode, the sentence (`Describe`'s, the last one that read kept while the draft has
  problems; the last reading's problems and warnings, and Save, stand until the next one
  arrives), the blocks (`WatchBlock`, `WhenBlock`, `IfBlock`, `ThenBlock` with
  `AgentStepBlock`, `LimitsBlock`), `TestPanel`, and the footer with Save (⌘S, `isSaveKey`,
  listening only while the tab shows, and never while a dialog or a sheet is open,
  `anyModalOpen`) and Discard. Each block renders from the catalog, its numbers' bounds and
  defaults too (`settingRange` over `agentSettings`, `stepSettings`, and `limits`), and shows
  the problems and warnings at its own part and field (`problemsAt`), invalid controls
  described by them; any no block draws, as an imported spec can hold, show at the top
  (`unshown`). A test's findings are keyed by what decides them (`testedKeys`): its matches by
  the watch, trigger, conditions, and Once per; its searches' notes and cost by the watch; and
  its warnings, which are by step, by the steps too. `TokenField` is the keyboard-operable
  token list with suggestions, a combobox over a listbox, as `SearchField` is, leaving an input
  method's Enter and comma to it. `lib/routineBuilder.ts` holds its pure half: a draft and its
  comparison (`canonical`, which leaves out what the spec leaves out when stored), what fits
  where (`addable`, `onlyIfChoices`, from the catalog's ordering rules, as `Validate` holds
  them; `withKind`, which drops what only a pull request has from a search of issues;
  `newTrigger`, which stores the schedule On a schedule shows), the fields and operators a
  condition can take, and the words (`dryRunWhy`, `costLine`, `testHeadline`).
  `AgentStepBlock` reuses `LaunchOverrideFields` for the model and the effort, showing one
  agent's fields alone, no extra flags, and, in the background, plan mode and the worktree as
  a background run has them. `AutopilotView` holds the one draft (`Editing`, keyed by the
  routine or `NEW_ROUTINE`; **New routine** again keeps it), keys each routine's builder and
  pane by the routine so no state crosses over, asks before leaving unsaved changes, and shows
  a routine a save just made until the list read again holds it.
- **Requests.** App's `openAutopilot(target)` numbers an `AutopilotRequest` and shows the tab,
  for a notification, a menu bar row, or a chip's **Show in Activity** (offered only for a
  match Activity lists, `inActivity`). A target that names a match shows Activity
  unfiltered and reads earlier pages until one holds the match, then marks its line, says
  what became of it when Activity lists no such match (`notListedText`, as for a baseline),
  or says it is gone when none does or its time differs; one that names a routine alone
  selects it while its creation time matches, else says it is gone; any other shows
  Activity. A request that names an open match marks its card in Needs you.
- **Needs you** (`NeedsYouSection`, above Activity) reads `NeedsYou` on every
  `autopilot:updated`: a card per match waiting for the user's OK, its hold shown with the
  launch hold dialog's own parts (`HoldDetails`, shared with `LaunchHoldDialog`), with
  **Run** (`Approve`, which confirms exactly the hold shown: Run on the head, Run in the
  worktree anyway, Clone and run behind a confirmation) and **Skip**; a GitHub action's card
  says what it would send next (`githubAheadText`) and words its button by what it waits on
  (`githubGoLabel`: Send it, Send anyway, which accepts exactly the warnings its words show,
  Send it again), still `Approve`; each routine's first-pass backlog is one card
  (`needsYou`'s `backlogs`, **Matched at once**), with **Run on them** (`ApproveBacklog`) and
  **Skip them** (`SkipBacklog`, with the IDs the card shows, its toast `skippedBacklogText`); a
  card per match that can't start yet, with **Check again**
  (`CheckReadiness`), Skip, and Open; the matches queued; and, while this Mac can't run some routines, a banner that opens Settings →
  Autopilot. The routine list and a routine's header say *Not ready on this Mac* with what
  is missing, from `Readiness`, and a waiting match's chip reads *Needs you*.
- **This Mac** (Settings → Autopilot) lists `Readiness`'s checks, each with its mark, what it
  found, and its fix as a command to copy or as words, and **Check again**.
- **An item's ⋯ menu** (`RoutineMenu`) holds **Autopilot ▸**: **Run a routine ▸** (`RunNow`,
  through the platform's `runRoutineNow`; it offers **Run anyway**, with the refusal's words,
  only on a refusal whose detail is `{"code": "noMatch"}`, `autopilot.RefusalNoMatch`, which
  the routine not matching the item alone carries, so it asks nothing more; any other refusal,
  a Run anyway's included, is told as it is), **Make a routine from this…** (the window's `onMake`, which App turns into
  an `AutopilotRequest` with `fromItem`), and **Why didn't a routine run?**, which reads
  `Explain` and shows its `routine` and `watch` lines and, folded, its `records`. It reads the
  routines, the catalog, and `onMake` through one source made once (`MenuSource.hold`), so the
  rows don't render again when they change.
- **Easy mode** (`TemplateGallery`, `QuickSetup`, `RoutineTransfer`, and the pure
  `lib/templates.ts`). **Templates** in the nav (`TEMPLATES`) shows the gallery
  (`listTemplates`, suggested first), and the empty tab its first four beside Start from
  scratch, Start from an item, Every template, and Import routines…. A template's quick setup
  holds only its blanks (`blankValues`, `filledSpec`: a `TokenField` per blank, the builder's
  own suggestions), the mode (new routines' default, `modeHint` beside it), the sentence and
  problems from `Describe` (`quickProblems` leaves out what `Template.Needs` and a starter to
  add back already say), `DraftReadiness`, and the builder's `TestPanel`; Save calls `Save`
  with the template's key, and Customize opens the builder over the filled draft (`newFrom`).
  The quick setup holds its template by key, so the gallery read again after **Add it again**
  (`addStarterPrompt`) shows it as it is now. A request's `fromItem` (once per request, kept in
  a ref across the tab's hiding) reads `FromItem` and opens the builder with its suggestions
  (`RoutineBuilder`'s `suggested`, a button each under If) and the item pinned at the top of
  Test (`TestPanel`'s `pinned`, `pinnedFirst`); a request's `template` opens that quick setup
  once the gallery is read, as the first-run setup's `FinishStep` asks (`SuggestedTemplate`,
  only while Autopilot (beta) is on). `useRoutineTransfer` is Export (a routine's ⋯ menu, or
  every routine: a dialog with **Include the text of the prompts they run**, then the system's
  save dialog) and Import (the open dialog, `PreviewImport` in a dialog with a choice for each
  `MissingRef`, `choicesOf`, then `Import` with the file's SHA-256). A routine whose
  `Figures.Graduate` holds says *Put it on Autopilot?* on its row, and its pane offers **Put it
  on Autopilot** (the ordinary `SetMode`) and **Not now** (`DeclineGraduate`).
- **The builder's agent step** draws Where from `catalog.places` (a place this build can't
  run in is shown off, with why; a new agent step starts in the first place this build runs
  one, the background), and the Test
  panel's **Preview** shows `PreviewRun`'s plan with the launch menu's own `PlannedLaunch`
  and the trace (`Trace`).
- **Describe it** (`describe/`: `DescribeView`, `Composer`, `Transcript`, `ProposalCard` with
  its rows `RoutineChange`, `PromptChange`, `SettingsChange`, and `RulesChange`, and
  `OutsideProposals`; the pure `lib/describeit.ts`; and `lib/useConversation.ts`) is the
  nav's `DESCRIBE` (-3), kept mounted in an `Activity` once it has shown. The empty tab's box
  (`AskBox`, whose `describeSeesText` the composer shares), a routine's **Change with
  Claude…** (`routineMenu`, `RoutinePane`), and an item's **Describe a routine for this…** (an
  `AutopilotRequest` with `describe`) hand it a `DescribeRequest`, a message to send at once or
  what to attach, which it acts on once (`handled`). `useConversation` reads the conversation
  as it mounts and on each `describeit:updated`, one read at a time with one more waiting, and
  each turn's events after the last it has (`Events`) until the turn has ended, as
  `useRunTimeline` follows a run. `lib/describeit.ts` words a turn's events (`turnEntries`;
  `toolWords`, which knows the app's tools through either prefix, `mcp__<slug>__` in the app
  and `mcp__plugin_<slug>_<server>__` from the plugin, and takes a check's or a proposal's line
  from its result's first sentence), places each card after the line that proposed it
  (`cardPlaces`), folds the cards a later one revises (`revisedCards`), and applies a part
  through the bindings the builder, Prompts, and Settings use, never another way:
  `createPrompt` (`promptInput`, the library's defaults for the rest); `saveRoutine` for a new
  routine in the mode its row chose (`newRoutineDraft`), or, for a change, `useRoutineChange`'s
  request, which asks about what a routine that is on matches now (`changeDraft`, keeping the
  routine's own mode); a `setAutopilot…` setter for each setting it changes (`settingSetters`,
  a record over kit's `SettingKey`, so a key the window can't apply fails the typecheck); and
  `setTrustedBots`, `setRepoRule`, and `setIgnoredWorkflows` (`applyRules`, over `RuleKind`
  likewise). Each part's outcome is `Settle`d with its words (`settleWords`), and `applyAll`
  runs a card's parts in order and stops at the first that fails, saying which
  (`appliedWords`). A routine's row is quick setup's review (`DraftReview`'s `useDraftReview`:
  `Describe` once editing rests, `DraftReadiness`, `TestPanel` over `Test`, and `ModeChoice`
  with `claudeModeHint`), with `MatchStored`'s count for a draft that watches the inbox's lists
  alone (`storedMatchWords`). `OutsideProposals` reads `Proposals` again on each
  `describeit:proposals`, as the nav's count reads `Status.Pending`. Settings → Autopilot's
  **Claude Code** group is `settings/DescribeItSettings.tsx` (`Agent` and `SetAgent`;
  `Plugins`, `InstallPlugin`, and `RemovePlugin`).

## internal/autopilot/draft

`internal/autopilot/draft/` is the one validator of a routine's draft: the builder's
`Describe` and `Save`'s checks in the root package, and whatever checks a routine outside the
app's own process ([kit](#internalautopilotkit), and so [the tool mode](#internalmcpserver)),
all read a draft through it, so a routine reads the same wherever it is checked. It is a pure
leaf, held by `TestLeafImports` to the routines core's `catalog`, `spec`, and `watch`, `item`,
`agent` (what each agent can keep in the background), `fence`, `autopilotprefs`, `brand`,
`textutil`, and `uierr`: it reads no database, starts nothing, and never asks GitHub. What a
draft's checks read beyond the draft comes through the `Setup` it is handed (`Library`, the
prompts as a routine names them; `Scripts`, the user's own, read only for a spec with a script
step; `StarterNames`, for the sentence), a read that fails skipping its checks, and what this
build runs through `Offers` (nil is the catalog's own, `CatalogOffers`; a test hands in fakes).

- **A reading.** `Read(ctx, spec, mode, setup, offers)` returns what the builder shows
  (`Reading`): the spec as `Save` stores it (`Encode`: a spec of version 0 at this build's,
  normalized and encoded; `Load` loads it back as every load checks a stored one, returning a
  `spec.ValidationError`'s problems rather than failing), the spec's own problems or else
  `Problems`' (an agent step set to run where this build runs none, `Offers.placeProblems`; a
  background agent step whose own agent can't keep its tools preset; a prompt the library
  doesn't have, a reply where an agent's prompt goes or the other way about, or one that
  doesn't fit the kinds the routine acts on, as one written for no kind of item, a prompt for
  repositories, fits none (`kindsProblem`), `StepPromptProblems`, which the engine runs again
  before each start; a script the user no longer has under the name it was picked by), the
  steps it runs only in Dry run (`Offers.NotOffered`), its warnings, and, only while it has no
  problem, its sentence. A spec past the size a spec may be is an error, worded.
- **Names.** `CheckName` (one line of plain text, at most 80 runes, none of them `Hidden`: a
  control or a bidirectional character), `CheckDescription` (at most 500, line breaks allowed),
  and `CheckTemplate` check a routine's own fields as `Save` does. `NameKey` is the fold every
  comparison of routine names uses and the `name_key` column stores, and `FreeName` gives a
  taken name the first free "<name> 2", cut to fit (`FitName`), as the root does for a copy, an
  import, or a restore, and kit's check for a proposed routine.
- **The rest it shares** with the root: `ActsOn` (the kinds a routine acts on: those both its
  watch and its trigger take), `Offers.Needs` and `NeedsWords` (what a template needs that this
  build doesn't run), `Offers.PreferredPlace`, `RenamedWords`, and `Series`.
- **The root's side.** The autopilot package keeps its bound types (`Described`, `MyScript`)
  and aliases the leaf's unbound ones (`LibraryPrompt`, `ReplyFor`, `Offers`, the caps, and
  the errors), so no generated binding moved; `serviceSetup` is its live `Setup` (the prompt
  library and the user's scripts, a read that fails logged). `TestReadingMatchesDescribe` holds
  `Describe` and `draft.Read` over a `Setup` of plain slices to one reading of every draft the
  package's tests read and of every template, filled in.

## internal/agentrun/stream

`internal/agentrun/stream/` reads a coding agent's stream as its run writes it, Claude Code's
`claude -p --output-format stream-json --verbose` and Codex's `codex exec --json`, for the
background runner ([Agents in the background](#agents-in-the-background)) and Describe it's
turns alike. It is a leaf: `TestLeafImports` holds it to `runevent` alone, so its readers
reach no run engine or store. `Claude(dir)` and `Codex(dir)` each return a `Parser`, an
`io.Writer` fed the log in order that any goroutine may read: its events a page at a time
(`Page`), its progress, the agent's own end (`Result`), the provider's usage limit (`Limit`),
Codex's thread (`AgentSessionID`), and, for Claude Code, what its init line says of its own
setup (`Init`: its model, its version, where its key came from, the session, every tool, and
every MCP server with its status, neither list ever nil), which Describe it checks before a turn
goes on. `From` starts a parser at an offset, for the run view's pages, and `Over` says the
result line came. `agentrun` aliases `Result` and `Limit`, so nothing above it changed.

The conversation spike's recordings (`testdata/conversation/`) hold what a conversation's turns
add to a headless run's stream, which the parser reads alike: a `session_title_changed` note
before init when the session is named, which it notes and shows nothing of; several tool uses
of one message, each on a line of its own; the denials `dontAsk` makes of a tool no rule
allows, counted in `Result.Denials`; and, for a turn that resumes a conversation, a result whose
`total_cost_usd` and `modelUsage` are the conversation's running totals, so `Result.Cost` is
the session's, `Result.TurnCost` the turn's own (summed from
`model_usage_since_last_result`), and `Models` every turn's. A background run starts its own
session, so its two costs are one.

## internal/fence

`internal/fence/` keeps an unattended agent run inside what its routine's tools preset promises
(`Preset`: `readOnly`, `githubTasks`, `editTest`, `editTestPush`, `profile`, the catalog's own
words, which core converts as they are), whichever agent runs it. Each agent's spec in
`internal/agent` turns a preset into its own flags and rules; this package holds the part that
depends on no agent. It is a leaf: it imports only `sensitive`, `paths`, and `textutil`
(`TestLeafImports`), and starts no process (`TestTheFenceRunsNothing`).

- **The fence.** `Build(Spec)` works a run's `Fence` out from its worktree (its links resolved,
  and the path as given kept as `WorktreeAs` when that differs), its git folders (read from the
  worktree's `.git` file and `commondir` through `paths.ReadHead`, `GitDirs`, when the spec
  leaves them out), its item, the pull request's head branch and the clone's remote for its
  repository, the repository's tools (`RepoTools`, from the marker files by `os.Lstat` alone),
  the routine's extra commands, and the run's, home, data, and config folders: the `Policy` the
  hook enforces, and for the edit presets (`Sandboxed`) the `Sandbox` the agent's own sandbox
  holds commands to. It refuses a folder that isn't absolute or holds what a rule can't name
  (`ErrSpec`), a tool or an extra command that would loosen a hard line (`ErrLoosens`: git and
  gh by name alone, the network tools, the shells, and any command the policy itself denies),
  and a push it can't make exact.
- **The exact push.** A push preset allows one push, `git push <remote> HEAD:refs/heads/<branch>`
  or with the short `HEAD:<branch>`, both exact, and only for a pull request whose branch is in
  the repository itself (`ErrCrossRepository` otherwise, which the engine's own
  `cross_repository` block keeps from starting), with its branch known (`ErrNoBranch`) and both
  names plain (`PlainName`, the prompts' rule: `textutil.ShellSafe` and no leading `-`;
  `PlainBranch` adds git's refname rules, so the refspec names that branch alone; `ErrNotPlain`).
  The profile's own preset starts with no push where one can't be made exact, rather than
  refusing. The exact fetch of the pull request's head, `git fetch <remote> refs/pull/<n>/head`,
  goes with a plain remote.
- **The policy.** `Decide(Policy, Call, FS)` is the one pure function every agent's hook feeds
  (its `FS` says what is on disk; nil reads nothing). It reads a Bash call's command line as a
  POSIX shell splits it (`shell.go`: each simple command with its assignments, quote-free words,
  and redirections, the commands inside substitutions, process substitutions, groups, function
  bodies, and unquoted here-documents included, and a word the shell expands at run time marked
  dynamic; a line it can't read is refused) and holds every simple command to the hard lines: no
  network tool, no command run as someone else, no shell handed `-c`, no `eval`, `env -S`, or
  `flock -c`; no program named by a variable or a glob; no `GIT_*`, `GH_*`, or `GITHUB_TOKEN`
  assignment; for git, none of the options that change where it runs or what config it reads
  (`-C`, `-c`, `--git-dir`, ...), none that run another program (`--upload-pack`, ...), only the
  preset's subcommands (the reads for every preset; the worktree's own git work for the edit
  presets), the listing subcommands only listing (no branch, tag, remote, worktree, reflog, or
  config change), no branch made by checkout or switch, every push but the exact one refused,
  and no commit whose message, on the line or in its `-F` file, carries an AI co-author
  trailer; for gh, never a merge, review, close, reopen, release, API call, or the account's
  own settings, and under the fixed presets only on its own in the line (its reads run outside
  the sandbox), only the preset's commands, GitHub tasks' writes only on the run's own item (its
  number or URL, `--repo` its repository, the flags each takes, `gh run rerun` only with
  `--failed`), and the edit presets' `gh pr checkout` of the item alone; under the wrappers
  (`xargs`, `env`, `timeout`, ...) and `find -exec`, every git, gh, or forbidden program among
  their words; and the paths a command names: none private (`ReadDeny`: the credentials under
  home, the app's data, the agent's config), and when the command writes (a `>` redirection, or
  a program that isn't one of the readers), none in git's folders or a denied category. A file
  tool writes only inside the worktree, its path and its links' target alike, never inside a
  `.git` or git's folders, never a file of the denied categories (`DeniedCategories`: agent,
  workflows, and tooling, from `internal/sensitive`'s own patterns, matched ignoring case,
  whatever the user turned off); a read tool reads nothing private. Web tools, MCP tools, and
  the tools that start work elsewhere or later are refused. The routine's own denies refuse a
  command by its first words. An agent's patch (Codex's `apply_patch`, as its own tool or a
  command) is decided as the writes of every file it adds, updates, deletes, or moves to
  (`PatchFiles`; a patch whose files it can't tell, as in a here-document, is refused), Codex's
  `view_image` as a read, and its `web_search` as a web tool. Under Read only, an agent that
  reads through its shell (`Policy.ShellReads`, which its spec sets: Codex) runs the commands
  that only read (the readers and `shellReaders`, git's reads, gh's reads, `find` without an
  action, `sed` without `-i`, no writing redirection), held besides by its own read-only
  sandbox.
- **The sandbox check.** A sandboxed run's policy holds a `Canary`: `touch <proof> <probe>`, whose
  proof is in the run folder's `ProofDir`, which the sandbox lets commands write, and whose probe
  is beside it, which it must refuse. Until the proof exists, every command but the check is
  refused, its reason giving the exact line; once the probe exists, the sandbox didn't hold, and
  every call is refused. `Sandboxed` reads the outcome for the run's end (held, leaked,
  unchecked, or none), and `Fence.AgentNote` is the line the unattended note gains.
- **The agent's rules.** `AllowedCommands`, `DeniedCommands`, and `DeniedPaths` work out, from the
  policy, the lists an agent's own rules name, so every agent and the hook hold the same ones:
  for a fixed preset whose mode refuses what no rule allows, the repository's tools, everyday
  commands, git's subcommands, gh's reads, the exact fetch and push, and the routine's own; the
  hard lines as denies, with no push deny for a push preset (it would beat the one allow); and
  the denied files at any depth they name, under each worktree path, and everything in git's
  folders. The `Sandbox` lists the folders a command may write beside the worktree (its git
  folders, the tools' caches under home, and the caches the run's environment names when they
  are safe, `envCaches`), the git folders' hooks, config, and info and the worktree's
  root-level denied files it may not, the secrets it may not read, the hosts it may reach
  (`Domains`: GitHub and the package registries), and the commands that run outside it as one
  plain command each: the exact push and fetch, the item's `gh pr checkout`, and gh's reads.
- **The hook mode.** `RunHook(args, stdin, stderr)` runs when the arguments hold `HookFlag`
  (`--fence-hook-v1=<run folder>`; any `--fence-hook` flag is the hook mode, so a build refuses
  a protocol version it doesn't know rather than start as the app). `cmd/nilbox`'s `main` calls
  it first in both builds, right after the desktop build's `updater.HandleHelperMode()`, before
  anything reads a setting, opens the data, starts logging, or touches the window. Protocol 1 is
  Claude Code's PreToolUse hook: the call's JSON on stdin (at most 32 MiB), the run folder's
  `PolicyFile` (`fence.json`, at most 1 MiB, a regular file, strict JSON of `PolicyVersion`), and
  a refusal as exit status 2 with its reason on stderr; a call let through says nothing and goes
  on to the agent's own rules. Codex's PreToolUse hook speaks the same protocol (codex-cli
  0.163.0: `tool_name`, `tool_input.command`, `cwd`). A call, or a policy, it can't read is
  refused. It writes nothing but each refusal, appended to the run folder's `DenialsFile`
  (`denials.jsonl`, `LogDenial`: only in a folder that is there, never through a link, at most
  1 MiB), since an agent's stream may never show a refused call (Codex's doesn't);
  `ReadDenials` counts them and keeps the last 20 for the run's outcome.
  `WriteRun` writes a run's folder: the policy and the agent's own files, 0600 and never through
  a link, in a 0700 folder, with the sandbox check's proof folder for a sandboxed run.
- **What changes GitHub.** `ChangesGitHub(line)` (`github.go`) reads a command line with the
  fence's own reader and reports one that changes something on GitHub: `gh pr` merge, review,
  close, reopen, comment, edit, ready, or update-branch; `gh issue` close, reopen, comment, or
  edit; `gh api` with a method other than GET, with fields and no method, or a GraphQL
  mutation; or `git push`, past git's own options and their values. It looks past the
  wrappers and into a shell's `-c` line, as Codex runs every command, to `maxShellDepth`, and a
  line it can't read is false. The sessions' observers call it to read an item again while
  its session still works; it decides nothing for the fence.
- **What a run changed.** `CheckChanged(head, files, cfg)` checks the files a run's commits
  changed against the sensitive categories, the denied ones always on (at most `MaxChanged`
  files; more make it incomplete), and `MergeChanged` adds that to an item's own file check.
  The inbox keeps the paths per item and head (`item_agent_changes`, `NoteAgentChanges`, the
  newest 20 heads, going with their item), at the head the run started from and its own last
  commit, and `AgentChanged` reads them back as a check with the user's categories; core's
  `agentFiles` is the launcher's `FileChecker` over the inbox's with them merged, so an agent
  file a run wrote holds the next launch on the item at either head as `HoldAgentFiles`, whoever
  opened it.

Claude Code keeps every preset (`internal/agent/claude_fence.go`). `agent.AccessFence` carries
the preset, for the profile's own preset the click's access it starts from (`Fence.Base`), and
the run folder's settings file (`Access.InRun`). Read only is `--restricted --tools
Read,Grep,Glob`; GitHub tasks drops every settings source (`--setting-sources ""`) with `--tools
Bash,Read`; the edit presets drop them with `--tools Bash,Read,Edit,Write,Grep,Glob`; each of
those runs in `dontAsk`, so a profile's full access can't loosen it and a profile without a mode
can't break it. The profile's own keeps the user's settings alone (`--setting-sources user`) with
the click's flags. Every one adds `--strict-mcp-config` and `--settings <run folder>/settings.json`,
whose Go types (`claudeSettings`) hold the rules, the fence hook as a command line (the app's own
executable and `HookArg`, each quoted as one word, never the exec form, whose flag a build that
doesn't know it would drop; 30 seconds, `onFailure: block`), hooks left on, attribution off (and
the older `includeCoAuthoredBy`), project MCP servers off, bypass mode off for a fixed preset,
and for the edit presets the sandbox (on, a hard failure when it can't start, no unsandboxed
retry, and no auto-allow, so the rules still decide; local binding, for a test's server). A test
checks every preset's settings against Claude Code's settings schema, recorded in
`internal/agent/testdata/claude-settings.schema.json`; another fails when a preset's flags or
settings keys change without `claudeFenceReviewed` and `claudeFenceFloor` (2.1.319,
`FenceFloor`) reviewed. `agent.Args` refuses a fenced run extra directories (`ErrFencedDirs`) and
every extra flag `CheckFenced` refuses (the fence's own, the headless run's, and those that would
loosen it, such as `--add-dir`, `--plugin-url`, and `--mcp-config`); `PrepareFence` builds a
run's fence and its files together, and `KeepsPreset` says which presets an agent keeps (each
spec's own `keeps`: Claude Code every one, Codex every one but GitHub tasks only, whose fence
is in [Codex in the background](#codex-in-the-background)). The launcher's `resolvePermission` chooses a background run's access from its
`Request.Fence`, below the trust rule: an untrusted item without the override runs Read only,
whatever the fence says, and a fenced plan takes none of the profile's extra directories.

## internal/autopilot/kit

`internal/autopilot/kit/` is what Describe it hands a coding agent, and how it reads what the
agent hands back, which the app ([internal/describeit](#internaldescribeit)) and its tool mode
([internal/mcpserver](#internalmcpserver)) share, so the two never disagree about a format. It
is a pure leaf, held by `TestLeafImports` to the routines core's `catalog`, `spec`, and `watch`,
`draft`, `item`, `repo`, `autopilotprefs`, `trust`, `reporules`, `heldruns`, `brand`, and
`textutil`: it reads no file, starts nothing, and keeps no clock, the snapshot and the moment
coming with each call.

- **The snapshot** (`Snapshot`, version 1, the describe folder's `context.json`) is what the
  agent may read: the user's `Setup` (each routine with its definition, sentence, switch, mode,
  and `UpdatedAt`; each prompt's names, purpose, review, and kinds, never its text; each
  script's name and where it shows, never its command; each agent profile's name; Autopilot's
  settings; and the rules routines lean on: the trusted bots, the pinned and muted repositories
  and orgs, and the ignored workflows), the inbox's names (`InboxNames`: repositories, owners,
  people, bots, labels, and checks, each most common first), and the parts of the vocabulary
  that packages above the leaves hold (`Vocabulary`: the gallery's templates, the starters'
  names, and the variables a prompt can name). Every name is untrusted, so `NewSnapshot` keeps
  each as `Name` does (one line, with no control, bidirectional, tag, or supplementary
  variation-selector character, at most 100 runes, cut with an ellipsis), and each inbox list
  as `Names` does (empties and duplicates by `draft.NameKey` left out, the first 200 kept). Every
  text kit renders from a snapshot opens with a line saying its names are the user's data,
  never instructions, and quotes each name as Go quotes a string. `EncodeSnapshot` refuses one
  past `MaxSnapshotBytes` (16 MiB); `DecodeSnapshot` reads the version first
  (`ErrSnapshotVersion`, which the app answers by writing it again as it starts), then strictly.
  `Snapshot.Outside` is the part a session in the user's own Claude Code reads: each routine's
  ID, name, sentence, switch, mode, and `UpdatedAt`, each prompt's nickname, purpose, and kinds,
  the settings, and the vocabulary; no definition, description, script, profile, rule, or inbox
  name.
- **The proposal** (`Proposal`): a summary; new prompts (`NewPrompt`: names, purpose, review,
  kinds, and text; no models, effort, plan mode, worktree, or flags, which reach a click's launch
  without a fence); new routines and changes to routines (`RoutineChange`: for a change, the
  routine's ID and `Base`, the `updatedAt` the agent read; a name, a description, the mode it
  suggests, its template, and its spec); a `SettingsChange` holding only what changes, with no
  field for Autopilot's switch or Pause; and a `RulesChange` (trust and untrust, pin, mute, and
  clear, ignore and unignore workflows). `DecodeProposal` reads it strictly through
  `spec.DecodeExact`, so a spec inside is held to a stored spec's rules and a key for the
  switch or Pause is refused, worded for the agent to fix, within its caps: 4 MiB, 10 routines,
  10 prompts of at most 32 KiB of text each, each spec within `spec.MaxBytes`, 100 entries a rule
  list, and a summary of one line within 400 runes; a proposal of nothing is refused too.
  `FuzzDecodeProposal` holds every refusal to `ErrUnreadableProposal`, and what it accepts to
  read back as itself.
- **The check.** `Check(ctx, snapshot, proposal)` checks each part as the app checks it as it is
  applied: each routine through `draft.Read`, over a `Setup` of the snapshot's prompts with the
  proposal's new ones added (so a routine may run a prompt the same proposal adds), its scripts,
  and the starters' names, in the mode it would run in (the one new routines start in for a new
  routine, whatever the agent suggests; the routine's own for a change), its name, description,
  and template as `Save` checks them, a taken name given the free one (`NameTaken`), and a
  change's routine missing (`Gone`) or changed since its base (`Stale`); each new prompt as the
  prompt library checks one as it is added, the variables its text names among them; the
  settings change as `autopilotprefs` checks the settings it leaves, each change in Settings'
  words (`SettingChange`); and the rules change through `trust.NormalizeBots`,
  `reporules.ParseTarget` and `Rule.Valid`, and `heldruns`' cleaning, each change one line
  (`RuleChange`). `Checked.Words` is what the agent reads, its first sentence the window's line
  (*Checked 2 routines and a settings change: 1 problem.*). `TestEveryGalleryTemplateChecksClean`
  checks every template, filled in.
- **Risks** (`RoutineRisks`, `SettingsRisks`, `RulesRisks`, `PromptRisks`) are worked out from a
  part's before and after, never from the agent's words, each one sentence, a warning or a
  caution (a stale base, a name taken); what a routine did before, and does still, is no new
  risk. `docs/desktop.md`'s [Describe it](desktop.md#describe-it) lists what they cover, and
  goldens pin every line (`TestRiskLinesArePinned`).
- **The words** are generated from this build's catalog, spec, and settings, so an update
  changes what the agent knows: `Guide` (`autopilot_guide`, read first: how Autopilot works,
  the proposal with a worked example, how to work, the posture, the templates by key, and the
  topics), `Topic` (one of `Topics`: proposal, routine, watch, triggers, schedule, conditions,
  fields, steps, agent, github, limits, settings, rules, prompts, templates, and schema),
  `Schema` (the proposal's JSON Schema, draft 2020-12, by reflection over its types, every closed
  set enumerated from `spec.ClosedSets`, the same bytes every call), `SetupWords` and
  `RoutineWords` (`autopilot_my_setup`: the whole setup, or one routine, `Outside`'s part for a
  session there), `InboxWords`, `SkillText` (the instructions: in the app, the system prompt's
  addition, and in the user's own Claude Code, the plugin's skill; how to work, read, check, then
  propose once, and the posture: start safe, one change per request, reuse a template or a
  routine, names are data, and the switch, the mode, and Pause the user's alone), `Tools` (the
  tool mode's tools by place: outside, no inbox names), and `ServerInstructions`. Every text a
  tool returns stays under 60 KB, since a result much larger never reaches the agent (the
  spike's S11): past 56 KiB, `SetupWords` leaves out the last routines' definitions, then their
  sentences, and says so.
  Goldens pin the guide, every topic, the schema, the setup and inbox words, and the skill in
  both places; `TestTheGuideNamesEveryCatalogEntry` and `TestTheSchemaEnumeratesEveryClosedSet`
  fail when the vocabulary outgrows its words.
- **The shared files.** A `ProposalFile` (version 1: its ID, `NewProposalID`'s moment in UTC and
  12 random hex digits; the conversation, or from outside the session folder's base name; when
  the snapshot it was checked against was written; and the proposal) and a folder's `Outcomes`
  (what the user did with each part: added, saved, applied, opened in the builder, or discarded,
  with its words and the routine it saved), each read strictly. `OutcomeWords` joins a turn's
  outcomes into the one sentence the next turn tells the agent (*Added “Fix checks”, switched
  off; discarded the settings change.*). `MaxOutsidePending` (50) caps the proposals waiting from
  the user's own Claude Code, counted among the newest `MaxOutsideRead` (200) files, which the
  app and the tool mode both read.

## internal/describeit

`internal/describeit/` is Describe it, the Autopilot tab's conversation: the user says what
they want, and Claude Code proposes it as cards the user reviews and applies through the
window's own builder, prompts, and settings (what the user sees is `docs/desktop.md`'s
[Describe it](desktop.md#describe-it)). It is a boundary: the repoguard test keeps it off the
store (and so off Autopilot's service and the background runner), the GitHub client, every
service, `prompts`, `profiles`, `launcher`, `scriptrun`, `desktop`, and `notify`, and it reaches
the rest of the app through the interfaces it declares (`Agents`, `Setup`, `Attachments`,
`Switch`), which `internal/core/adapters_describeit.go` adapts. It owns no table and no prefs key: what it keeps
is in the data folder's describe folder (`appdata.Paths.Describe`, which `Ensure` makes), every
folder the user's alone and every file written whole (`appdata.WriteFile`).

```text
describe/
  context.json            the snapshot the tool mode reads (kit.Snapshot)
  instructions.md         kit.SkillText(kit.InApp), each turn's --append-system-prompt-file
  work/                   the one empty folder every turn runs in
  agent.json              the agent new conversations start with: {profile, model, effort}, "" inherits
  bin/<slug>-mcp          the link the plugin runs: the app's executable, in its tool mode
  plugin/                 the plugin's local marketplace (agent.PluginFiles)
  conversations/<uuid>/   conversation.json, mcp.json (the turn's --mcp-config), and each turn's
                          turn-NNNN.prompt.md (its stdin) and turn-NNNN.jsonl (its stream)
  proposals/<uuid>/       a conversation's proposals (kit.ProposalFile) and outcomes.json
  proposals/outside/      the same, from the user's own Claude Code
```

- **The bound methods.** `Status` (Claude Code found, at least `agent.ConversationFloor`, and
  signed in, else why and the fix, each a sentence; the agent new conversations start with,
  Inherit resolved; whether a turn runs; and `Pending`, the cards from outside with a part no
  one dealt with, the nav's count), `Agent` and `SetAgent` (Settings' choice, `agent.json`,
  refusing a profile Claude Code doesn't have, or a model or effort the agent's own checks
  refuse), `Conversation`, `NewConversation`, `Send`, `Stop`, `Events`, `Settle`,
  `DiscardProposal`, `Proposals`, `Plugins`, `InstallPlugin`, and `RemovePlugin`. None reads
  GitHub, and every refusal is worded with `uierr`.
- **A conversation** is one current conversation, kept in its `conversation.json` (`record`:
  its agent as chosen and as resolved, whether its session started, the outcomes a turn already
  told the agent of, and its turns), whose UUID is the agent's session ID. One that started more
  than 30 days ago is removed with its proposals as it is read or sent to, or as the worker
  readies the folder; `NewConversation` sets the current one aside (refused while a turn runs), and a start
  over removes every conversation and proposal (core's `forgetConversations`).
- **A turn.** `Send` reserves the one turn that may run (another is refused while it does, and
  every send once the app quits), plans it before its agent starts (its conversation, or a new
  one with `Agent`'s choice; the agent through `Agents.Conversation`, held to the floor; the
  app's own executable; the attachments through `Attachments`; and the snapshot written fresh
  and the instructions), then writes the turn's files and starts it with `execpath.Start` in a
  process group of its own: `agent.ConversationArgs` with the conversation's ID (`--session-id`,
  then `--resume` once the session started), `<brand.Name>: Describe it` as the session's name,
  the conversation's model and effort with Inherit resolved to the profile's own (`record.resolve`,
  what the header shows; a profile with none passes no flag), the turn's MCP config (`agent.ConversationFiles`, its one
  server `brand.Slug`, the app's executable with `mcpserver.Arg` and `mcpserver.ConversationArg`),
  the instructions' file, and a $2 budget (`turnBudgetUSD`); the prompt on stdin, opened with
  `paths.OpenRegular`, and stdout and stderr into the turn's log, in `work/`, with the
  environment core builds as for a background run (`launcher.AgentEnv` over the login shell's,
  then `agent.HeadlessEnv`). The prompt is first what the user did with the cards since the last
  turn (`kit.OutcomeWords`; `record.Told` keeps an outcome from being told twice), then their
  words (at most 20,000 characters), then what they attached (at most 10, each within 64 KB).
  A turn takes no run slot, no clone lock, no history row, and no quit hold, and counts toward
  neither the routines' runs a day nor the daily cap.
- **The follower** (`follow`) reads the turn's log every 100 ms into `stream.Claude`, tells the
  window (`event.DescribeItUpdated`, `describeit:updated`, with the conversation and the turn)
  at most every 250 ms, and notes each proposal file that appears in the conversation's folder.
  As the init line comes it checks the turn's own setup (`initProblem`): the app's server
  `connected`, and every tool and server its own (`agent.ConversationToolPrefix`), else it stops
  the turn at once, since a turn whose server failed improvises and spends (the spike's
  `turn-mcp-failed.jsonl`). It stops the turn with `execpath.StopLadder()` once the result line
  is in, on the user's `Stop`, or as the app quits (`shutdown`, which kills the group after 3
  seconds). The end (`finish`) is judged by the result line, never the exit status: done;
  stopped, by the user or the quit, whose SIGINT still writes a result in error (the spike's
  S7); or failed, worded (the setup was wrong, the usage limit, the budget, the agent's error,
  or the last line it wrote that isn't the stream's). A turn's cost is the result's `TurnCost`
  and the conversation's the latest `Cost`, the session's running total. A turn that got as
  far as init leaves the session started, so the next resumes it; one whose agent ended before
  init, on its own, names the session the other way next, so a conversation never sticks on a
  session Claude Code refused. A turn a crash left running is closed from its log as the worker
  readies the folder, or before the conversation's next turn.
- **Events** (`Events(id, turn, after)`) are a turn's events after a sequence number, a page at
  a time, as a background run's are: from the follower's parser while it runs (none, not done,
  while it gets ready), and once it ended, from its log read whole by a parser of its own, which
  numbers them alike.
- **Cards.** A card is a proposal file read as untrusted, since any process running as the user
  can write the folder: only a name the tool mode makes (`kit.ValidProposalID`), never a link
  (`Lstat`), within `kit.MaxProposalFileBytes`, through kit's strict decoder, naming its own ID
  and the folder it is in; then checked again with `kit.Check` against the live snapshot, so a
  card says what applying it would do now. A part the user dealt with is shown as it was dealt
  with instead (`asDealtWith`), since checked against the setup it changed itself it would find
  its own name taken or nothing left to change: no problem, warning, or risk, a routine it
  added or saved by that routine's name now, and settings and rules by what they set
  (`kit.SettingsSet`, `kit.RulesSet`). `Card.Proposal` is the proposal as written, which a
  part applies from where `Checked` holds nothing of its own (a description, the mode
  suggested, the template, a change's routine and base). `Settle` records what the user did
  with one part in the folder's `outcomes.json` (only a kind that fits the part, the first
  outcome standing), and `DiscardProposal` every part left; each tells the window
  (`describeit:updated` for a conversation's turn, `event.DescribeItProposals`,
  `describeit:proposals`, for one from outside). The window applies a part itself, through the
  bindings the builder, Prompts, and Settings use ([the Autopilot tab](#the-autopilot-tab)), so
  nothing here writes the setup.
- **The snapshot.** Core's `describeSetup` builds it (`kit.NewSnapshot`) from Autopilot's `List`,
  `MyScripts`, and `Suggest`, the prompt library (no text, and none of the prompts for
  repositories, which no routine runs: `itemPrompts`), the profiles, the settings
  (Autopilot's, the trusted bots, the repository rules, the ignored workflows),
  `inbox.Store.KnownRepositories` (the stored repositories that aren't archived, most items
  first, at most 200), and the vocabulary (`autopilot.Gallery`, `prompts.Starters` but those for
  repositories, `itemStarters`, and `prompts.Variables`); a part that can't be read fails it
  whole. The service keeps the live
  snapshot 2 seconds, forgets it on any change, and falls back on the last one read when the
  setup can't be read. It writes `context.json` before every turn, as the worker readies the
  folder, and after each burst of changes while Autopilot is on: core's `describeOnChanges`
  passes the app's events on and wakes the worker on `event.AutopilotUpdated`,
  `AutopilotChanged`, `RepoRulesChanged`, and `InboxUpdated`, which writes once the burst has
  been quiet for 500 ms, or after 3 seconds in all, so a session in the user's own Claude Code
  reads it current. The prompt library, the scripts, the trusted bots, and the ignored
  workflows emit no event when they change, so such a change reaches that session's snapshot
  with the next inbox refresh; a turn in the app writes the snapshot fresh first.
- **Attachments.** Core's `describeAttachments` words a routine as `kit.RoutineWords` does, and
  a stored item by its address and short facts (`inbox.Store.FactsRows`: kind, repository,
  author, labels, and a pull request's failed checks' names at its head), under a line saying
  its names are data and nothing else of the item is shared.
- **The agent.** Core's `describeAgents` finds Claude Code as a background run's start does:
  readiness's own checks of its program, its version, and the profile's sign-in, asked again at
  once when one holds the turn back, so a fix shows at once; the launch defaults' Claude Code
  profile for Inherit; and the environment `agentEnv` builds, with no API key unless the user's
  setting lets one through. A profile on this Mac that is only signed out still gets its
  program and environment, not ready: a turn is held back, while the plugin's commands, which
  need no sign-in, run.
- **The plugin.** `Plugin(command, args)` is the plugin (`agent.PluginSpec`: `brand.Slug` in the
  marketplace `<slug>-app`, version 1.0.0, which changes only with the tool mode's protocol; its
  skill `autopilot`, `kit.SkillText(kit.Outside)`; its server `autopilot`), which `WritePlugin`
  writes into `plugin/`, each file only when it differs. The worker rewrites it as it readies the
  folder, and Claude Code loads it in place from there, so an installed plugin reads this
  build's words at its next session. Its server runs `bin/<slug>-mcp`, a link to the app's
  executable that only an app in a stable place re-points (`Config.Stable`, core's `stableApp`:
  the update engine's location check, never a build from source), through a link renamed over
  it, so a build from source never leaves it on a temporary binary. `InstallPlugin` writes the
  folder, points the link (refusing, worded, when the app can't and no stable app did), and runs
  `claude plugin install <plugin> --marketplace <dir> --json` with the profile's environment;
  `RemovePlugin` uninstalls (`not_installed` is no failure) and removes the marketplace; and
  `Plugins` reads `claude plugin list --json` for each profile. Each command is
  `agent.PluginArgs`, read with `agent.ParsePluginResult` or `ParsePluginList`, within a minute.
- **From outside.** `Proposals` reads the outside folder's newest `kit.MaxOutsideRead` files,
  each a card checked against the live snapshot, newest first, leaving out those settled more
  than a day ago. While Autopilot is on, the worker looks at the folder every 3 seconds (a hash
  of each file's name, size, and time) and tells the window `describeit:proposals` when it
  changed, and, as it readies the folder, removes the cards settled more than 30 days ago with
  their outcomes.
- **The worker** (`Worker`, a deferred desktop worker, `Core.DescribeItWorker`) works only while
  Autopilot (beta) is on, which it reads through `Switch` (core's `describeSwitch`, Autopilot's
  settings as stored; none to read, or one that can't be read, is off): as the app starts, and
  again after each change it hears, since the switch's own event reaches it as
  `event.AutopilotUpdated`. Off, as on a fresh database, it writes nothing and follows nothing.
  Once on, it catches up: it readies the folder (a turn a crash left running closed from its
  log, old conversations pruned, and a proposals folder no conversation keeps once it is 30
  days old, the instructions and the snapshot written), keeps the plugin and its link current,
  follows the outside folder, and writes the snapshot after each burst of changes, until a
  burst finds Autopilot off again. It stops a running turn as the app quits, on or off.
- **The eval suite** (`evals/`: two cases with their graders, and mocks of the plugin's tools)
  is the plugin's own, run by hand with `magex autopilot:evals` (Claude Code's `claude plugin
  eval` over this build's plugin in a temporary folder, the baseline arm left out, $5 unless
  `budget=` says), never by the gate; `EvalText` fills in the names it leaves for the build.

### What the conversation spike found

The conversation half was settled by a spike on Claude Code 2.1.328: real `claude -p` turns in
empty scratch folders against a stdio MCP server, recorded into the tests' fixtures
(`internal/agentrun/stream/testdata/conversation/`, `internal/agent/testdata/conversation/`, and
`internal/mcpserver/testdata/mcp-client.jsonl`). The turn's argv follows from it, which a golden
pins (`TestTheConversationsArgv`):

```text
claude -p --output-format stream-json --verbose --permission-prompts none
  --session-id <uuid> | --resume <uuid>   --name "<brand.Name>: Describe it"
  [--model <model>] [--effort <effort>]
  --tools "" --restricted --strict-mcp-config --mcp-config <conversation>/mcp.json
  --permission-mode dontAsk --allowedTools mcp__<slug>
  --max-budget-usd 2 --append-system-prompt-file <describe>/instructions.md
  --system-prompt-snapshot off
```

- **Tools and isolation.** `--tools "" --restricted --strict-mcp-config --mcp-config` leaves a
  turn the server's tools alone, connected. A planted project `CLAUDE.md`, project settings with
  hooks and allow rules, a project `.mcp.json`, and a project skill never reached it, nor the
  user's own `CLAUDE.md`: `--restricted` drops the user's, the project's, and the local
  settings, while managed settings and `--settings` still apply. `--strict-mcp-config` drops a
  `--plugin-dir` plugin's own server, and with no tools there is no Skill tool, so in the app the
  skill's text is the system prompt's addition.
- **Permissions.** Under `--permission-mode dontAsk` the server's own tools are denied without
  an allow rule; `--allowedTools mcp__<server>` allows every one of them. `--tools`,
  `--mcp-config`, and `--allowedTools` take several values, so the prompt comes on stdin.
- **Resume and money.** `--resume` takes a session up again in a new process, which takes every
  flag again; with `--system-prompt-snapshot off` the instructions are read fresh each turn, and
  the prompt cache still carries across. `--max-budget-usd` holds one process, so one turn, and
  a resumed turn's result gives the session's running totals beside the turn's own.
- **Storage.** A session's transcript is kept per working folder in the profile's config, one
  project each, so every turn runs in the one `work/`.
- **Stopping.** A SIGINT mid-turn writes a result in error (`error_during_execution`,
  `aborted_streaming`) and exits 0, the MCP server going with the process group, and the next
  `--resume` goes on.
- **The MCP client** probes `server/discover` first (`-32601` makes it fall back to
  `initialize`), reads the server's `instructions`, sends ids as strings or numbers, shows the
  agent a result's `structuredContent` in place of its text, and ends the server with a signal,
  never EOF, so the tool mode never sends `structuredContent` and writes each proposal whole as
  it is made.
- **The plugin.** `claude plugin install <plugin> --marketplace <dir> --json` adds a local
  marketplace and installs from it in one step, asking nothing, and the plugin loads in place
  from that folder. In a session, its tools are `mcp__plugin_<plugin>_<server>__<tool>`, its
  server starts with every session, each call asks first, and its skill triggers from its
  description.
- **Sizes.** A 57 KB tool list was taken but weighed on every request, so input schemas stay
  small and the whole schema is a topic; a 400 KB tool result never reached the agent, so every
  result stays under 60 KB.
- **Visibility.** A running turn shows in `claude agents --json`; the sessions watcher follows
  only the sessions the app recorded, so it ignores turns, and `--name` names them in the user's
  own lists.
- **A server that failed.** The turn's init showed it `failed` with no tools, and Claude
  improvised and spent all the same, so the follower stops such a turn at once.

## internal/mcpserver

`internal/mcpserver/` is the app's tool mode: the app's own binary, run again by Claude Code as
a stdio MCP server (`cmd/nilbox`'s `main` calls `Serve` right after `fence.RunHook`), which
serves Describe it's tools to the in-app conversation and, through the plugin, to the user's own
Claude Code. It reads the snapshot the app wrote, answers with kit's words and checks, and writes
nothing but a proposal, into a folder the app made. It is both a leaf (`TestLeafImports`: `kit`,
`spec`, `appdata`'s whole-file writes, `paths`' readers, `brand`, and `textutil`) and a boundary
(no `execpath`, `scriptrun`, `launcher`, `logging`, store, or service), and
`TestTheToolServerRunsNothing` holds its own files to no `os/exec`, `execpath`, `net`, or
`database/sql`: it starts nothing, opens no database, reads no environment, makes no network
call, and writes no log.

- **The call.** `--mcp-server-v1=<describe folder>` (`Arg`), and in the app
  `--conversation=<uuid>` (`ConversationArg`), which makes it the conversation's (`kit.InApp`);
  without it, it serves the user's own Claude Code (`kit.Outside`), and names a proposal's
  `Where` by the base name of the folder the session runs in, as the system reports it, never
  the environment. The protocol's version is in the flag, and any `--mcp-server` flag is a call
  (`IsCall`), so a version this build doesn't know, a folder that isn't absolute, or a
  conversation that isn't a UUID is refused with a line on stderr and status 2 before it reads
  anything, never started as the app.
- **The protocol.** JSON-RPC 2.0 over stdin and stdout, one message a line of at most 4 MB (a
  longer one refused, and the next read whole), each answered as it comes, since the client
  never closes stdin; ids echoed as they came. It speaks MCP's dated versions up to 2025-11-25,
  the client's when it can; answers `initialize` (`capabilities: {tools: {}}`, `serverInfo` with
  `brand.Slug` and the protocol's version, `1`, and `kit.ServerInstructions`), `ping`,
  `tools/list` (one page), and `tools/call`; refuses every other request, `server/discover`
  among them, with `-32601`; and answers no notification. A tool's answer is text, with `isError`
  for a refusal the agent can act on, and never `structuredContent`.
  `TestTheRecordedClientIsServedInBothPlaces` replays the real client's lines.
- **The tools** are `kit.Tools(where)`: `autopilot_guide`, `autopilot_reference` (`{topic}`, one
  of `kit.Topics`), `autopilot_my_setup` (the whole setup, or `{routine}` for one),
  `autopilot_inbox_names` (in the app alone), `autopilot_check` and `autopilot_propose`
  (`{proposal}`), and `autopilot_proposal_status`. Each input schema stays small, since every
  request carries the list: a proposal is an object, its schema the `schema` topic. Arguments
  are read strictly (one object, the tool's own keys alone), and the snapshot is read again at
  each call (`paths.ReadHead` within `kit.MaxSnapshotBytes`, then `kit.DecodeSnapshot`); outside,
  the setup it gives is `Snapshot.Outside`'s, but a proposal is checked against the whole
  snapshot, as its card checks it again.
- **A proposal.** `autopilot_propose` decodes the proposal (`kit.DecodeProposal`), checks it
  (`kit.Check`), and refuses one with any problem, saying each; else it writes a
  `kit.ProposalFile` (its ID from the time and `crypto/rand`) with `appdata.WriteFile`, never
  over another file, into `proposals/<conversation>/` in the app or `proposals/outside/`, each
  only when the app made it and it is a folder, never a link. From outside it refuses one once
  `kit.MaxOutsidePending` (50) wait, counted as the app counts its cards: among the newest
  `kit.MaxOutsideRead` files, each one the app reads as a card from outside with a part the
  user hasn't dealt with. Its answer opens with the window's line (*Proposed a routine and a
  settings change.*), then gives the ID and says the proposal waits in the Autopilot tab.
  `autopilot_proposal_status` reads the folder's outcomes and says, newest first and within
  48 KiB, what the user did with each part and what still waits; outside, only of the proposals
  from a session in a folder of its own name.
- **Tests.** `cmd/nilbox`'s `toolmode_test.go` runs the binary in its tool mode with nothing but
  an empty home, in the app and from outside, and finds nothing created but the proposal; a
  describeit test serves the tool mode and checks that it refuses exactly when `Status.Pending`
  reaches 50.

## internal/backup

`internal/backup/` is Settings → Back up and restore's service, which owns no table. It
exports the user's setup to one `<slug>.backup` file (`File`, version 1, at most 16 MB,
written user-only through `appdata.WriteFile`), one section per category ticked: settings,
repositories, prompts (a `prompts.Bundle`, exactly as `prompts.json`), launch defaults (the
`launch-defaults.json` format), the user's scripts, and the Autopilot routines (the
`autopilot.json` format, each added or saved over arriving off, one a newer build wrote
never touched); and imports one back. It reaches each
category's owner only through a `Part` (`Export`, `Prepare` outside the transaction, `Apply`
inside it, `Done` once it commits), which `internal/core/adapters_backup.go` adapts over each
owner's `BackupPart` (`settings`, `prompts`, `launchdefaults`, `scripts`, `autopilot`), a type of its own
built with `NewBackupPart`, never a method of the bound Service, so the window can't reach a
method that takes a `*sql.Tx`; the owners import only backup's words (`Mode`, `Plan`, `Item`,
`Count`), and the boundary test forbids the reverse. `Preview` works every section out under
both modes (Merge, whose wire value is `combine`, and Replace), a section that can't be read
shown with why; `Import` reads the file again and refuses it when its SHA-256 differs from the
preview's (`ErrChanged`), saves the whole setup first to `<data>/backups/<slug> before import
<stamp>.json` (the newest `KeepBefore`, 5, kept), works each section out again, then applies
every one in one `store.InTx`, in `Sections` order, a `Clearer`'s rows (the launch defaults',
under Replace) deleted first so the prompts part may delete what they named; the prompts
part fills in the `Links` (each file nickname's ID here) the launch defaults part finds its
prompts through, else the library by nickname. Each owner checks what it imports as its own
setters or editors do (settings' `check…` helpers, shared with the setters), leaving out what
it refuses with a note: a setting the setter refuses, a profile not on this Mac, a copy of a
tool that isn't a program here, a location that doesn't exist, a launch default's prompt not
found (it inherits). Under Replace, prompts are matched by nickname and purpose, the rest
deleted but one a launch default the import keeps still names; scripts identical to the
file's keep their IDs. A backup never holds the GitHub sign-in, caches, mirrors, history,
per-item triage, agent profiles, script trust (a file from elsewhere could pre-approve a
repository's command), the window's memory, or tool detection. Its `Saver` (`NewSaver`, a
type of its own, never bound) saves the whole setup before a reset the user asked for, to
`<data>/backups/<slug> before reset <stamp>.json`, the newest `KeepBefore` of those kept apart
from the import's.

## internal/setup

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

## frontend

`frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`.
The React Compiler memoizes the build, so components hold no hand-written `useMemo` or
`useCallback`, and the gate's render check (`pnpm test:renders`) profiles the compiled
inbox. Tests stand in for Go only with the shared fake in `frontend/src/test/platform.tsx`.
Both sheets show a conversation with the same parts, never a copy of them:
`components/conversation/` (`Description`, `Timeline`, and `TimelineEntry`, which shows a
review with its line comments), `lib/timeline.ts` (its rows, what is new, a quote, and the
counts), and `lib/useTimeline.ts` (a visit's start and the earlier pages, joined). The review
sheet shows it in its Comments tab, beside Files (the description and the diff) and Commits
(`review/CommitList`, the commits since the user's review marked with the shared `Divider`
the conversation's New uses, and `review/SinceYourReview` above the tabs, `lib/commits.ts`),
each kept mounted in an `Activity` while another shows; the tab is the window's memory
(`ui.reviewTab`, which a backup never holds), and so is the order both sheets list a
conversation in (`ui.conversationOrder`, oldest or newest first, under the description),
which the inbox shares between them.
Neither sheet shows an item a read of it finds merged or closed on GitHub, as a card can be
until the inbox's next refresh: `useEnded` (`lib/sheet.ts`) hands it to the sheet's `onEnded`
once the read's `state` says so, unless an action runs or the sheet shows the notice one left
(the review sheet's pushed to since or runs approved, the issue sheet's, as the text a close
someone made first kept from being posted). Its host, `InboxView` or `useSheetHost`, moves on
as for any move: past the card the way the sheet stepped to it (`Came`, the way and the cards
it stepped through, `stepPast`, which goes the other way when none is left that way, since the
read took the card out of the list already), else, in the inbox, back to the sheet a link to it
was followed from, else closed; `useItemActions`' `ended` says so in a toast ("acme-org/api#12
was merged", with Open on GitHub). The read stored the item and told the window, so the card
leaves the list with no request more, and the move marks it read as any does.
`components/notifications/` is the Notifications page (`NotificationsView`, loaded on its
first visit), whose tabs are the app's own history (`AppNotificationsTab`) and the user's
GitHub notifications (`GitHubNotificationsTab`, with the Cleared view, `ClearedList`), each
listed by day (`DayGroups`); the toolbar's bell and GitHub button are quick views of it
and share its rows (`EntryRow`, `ThreadRow`), so a row is never copied. Its tab is the
window's memory (`ui.notificationsTab`); each tab's Show and Type live in App's `Nav`, so
the toolbar's field (`lib/notificationFilter.ts`) counts what the list shows. Opening the
bell marks nothing: an entry is marked read when opened, by its ✓, by a click on its
banner (`event.RevealItem.NotificationID`, checked against the entry's item, page, or run,
since Clear history hands ids out again), or when its item's sheet opens (`InboxView`'s
`onItemOpened`, once per opening).
`components/autopilot/` is the Autopilot tab (the routine list, Needs you's cards,
Activity, a routine's Watching list on the inbox's own rows and sheets, the ⚡ chip, an
item menu's Autopilot ▸, `RoutineMenu`, `RunView`, an agent's run beside Activity, with its
timeline a page at a time (`lib/useRunTimeline.ts`, `lib/runView.ts`), `TakeOverDialog`,
which the run view, the session panel, and the launch hold dialog share, and `builder/`, the
routine builder and its Test panel with Preview, whose pure half is
`lib/routineBuilder.ts`), its words in
`lib/autopilot.ts`; a hold's details are the launch hold dialog's own (`HoldDetails`), and
a planned launch the launch menu's; every row reads the routines' names and its chip through
`AutopilotRowsContext`, and the inbox's row actions are shared through
`components/useItemActions.ts`. The window shows any of Autopilot only while Autopilot
(beta) is on (Settings → Labs, `LabsSection`): off, there is no tab, no Autopilot settings
section or banner switch (`hiddenOf` in `lib/settingsIndex.ts`), no Autopilot type among
the Notifications page's filters, and no `AutopilotRowsContext`, so no chip or mark; on, the
tab's label and the section's heading carry a `BetaBadge`. Full design:
[docs/architecture.md](docs/architecture.md#the-autopilot-tab).
Theme colors come from the `--color-*` tokens `internal/brandgen` writes to
`frontend/src/generated/theme.css`, whose `@theme` block first resets Tailwind's own palette
(`--color-*: initial`), so a utility of Tailwind's, such as `text-white` or `bg-red-500`,
builds to nothing. What the components share besides is declared once in `index.css`'s
`@theme inline`, each as a literal, so a utility carries its value itself: the type scale
(`text-micro` to `text-heading`, eleven sizes in rem, none of which sets a line height), the
shadows (`shadow-menu`, `shadow-card`, `shadow-float`, `shadow-toast`, `shadow-terminal`,
`shadow-control`, `shadow-drawer`, and `inset-shadow-control`), the dim behind a modal dialog
(`backdrop:bg-scrim`, a literal since, before Safari 17.4, `::backdrop` sees none of the
page's custom properties), `paper` (white in both themes: a switch's knob, a radio button's
dot, a slider's thumb), and `opacity-disabled`, the one fade of a disabled control (0.6);
beside them, as `@utility` rules, the washes (`wash-success`, `wash-danger`, `wash-accent`,
`wash-warning`, and the neutral `wash-raised`, a color mixed into the surface) and
`layer-popover` (z-40, over the toolbar's z-30 and under a toast's z-50), the layer every
popover and menu panel opens on. A hue's tints take their alpha from one ladder (/5, /10, /14,
/22, /45, /60). `lib/tone.ts` gives a state's tone its text and fill classes (`toneText`,
`toneFill`), and a tint stays in the map of the domain that draws it. Checkboxes and radio
buttons take the `checkbox` and `radio` classes (`frontend/src/index.css`), which draw them in
the app's own style: macOS draws a native one grey whenever it counts the window as inactive.
Text is sized in rem, never px (`text-caption`, from the type scale, not `text-[11px]`): the
text size setting scales the root's font size, which only rem follows, and the shell passes
the stored size in the page's URL (`internal/desktop.PageURL`) so the first frame is already
at it. A repoguard test fails on px text. Only chrome lined up with the native window, such as
the toolbar's height and its inset beside the window controls, and hairlines stay in px.
Another, `TestFrontendStylesComeFromTokens`, fails on a style written around the tokens: a
Tailwind palette utility or variable, a color written into a class, a style, or a stylesheet
outside a token's own declaration, an arbitrary text size or backdrop, and a disabled fade
other than `opacity-disabled`. It has four exceptions, each with its reason and each matching
only the whole line it is for, eight lines in all: the checkbox's data-URL check and dash, the
slider thumb's shadow, the inbox-nil grid's masks, and the card guide's sample label colors.
ESLint fails on a checkbox or radio input in the window without its class
(`controlClassSyntax` in `eslint.config.js`), unless it is hidden (`sr-only`) or a switch.

**The build and its checks.** The root `tsconfig.json` only references three projects.
`tsconfig.app.json` checks `src/` and `bindings/` against the browser (lib ES2023 and DOM, the
`vite/client` types, and none of Node's, so the window's code can't name `process` or
`Buffer`); `tsconfig.node.json` checks `vite.config.ts` and the two Vitest configs against
Node (its types, no DOM); and `tsconfig.test-node.json` checks the one test that runs Node's
own modules, `src/test/devtoolsAgent.test.ts`, which the app's project leaves out, against
both (DOM and Node's types). All three keep the same strict flags and `noEmit`.
`pnpm typecheck` checks each with `tsc -p`, so nothing writes build info, and typescript-eslint's
`projectService` opens every linted file in the project that includes it, with no default
project to fall back on. `pnpm build` runs that typecheck before `vite build`, so
`magex frontend:build`, `app:build`, and a release check all three projects too. ESLint bans
importing `useMemo` and `useCallback` anywhere in `src/` (`noMemoHooks` in `eslint.config.js`).
The gate (`magex frontend:gate`) runs the format check, lint, the typecheck, the tests with
coverage, the render check, and the production build, which `chunkLimit` in `vite.config.ts`
fails on a JS chunk over `chunkSizeWarningLimit` (750 kB, above the biggest chunk, the entry,
which holds the inbox, about 467 kB), where Vite only warns; then it checks the first paint
against its budgets (`magefiles/firstpaint.go`: the entry, the shared chunk of about 377 kB it
preloads, which holds most of what the first paint and the pages share, and Rolldown's
runtime, held to 950,000 bytes of script, 325,000 gzipped, and the CSS to 140,000, 25,000
gzipped). The bundle targets Safari 16.4, the WebKit of the oldest supported macOS, 13.3.

**Shared hooks and helpers.** Each of these is the one way the pages do it, never a copy.
`useOn` (`lib/useOn.ts`) hears a window event while the component is mounted (or `enabled`)
and `useKeyDown` (`lib/keys.ts`) every key that bubbles up to the document or the window, each
calling the latest handler rendered, so a new one never subscribes again. `lib/dialogs.ts`
opens the system's file dialogs (`pickFileToOpen`, `pickFileToSave`): the path, '' on a cancel,
or the failure worded for the page. `logFailure` (`lib/call.ts`) runs a call whose failure the
user asked nothing about and writes it to the console; `terminalLabel` (`lib/terminals.ts`)
names a terminal app, or a fallback for one this build doesn't know; `Select<T>`
(`components/controls.tsx`) is the native select of a closed set of strings, handing
`onChange` one of its own options' values; and `unlisted` (`lib/unlisted.ts`) answers, from a
switch's default, a value Go may send that its union leaves out, so a constant Go adds fails
the typecheck until it has a case. `platform/cause.ts` (`causeOf`) is the one decoder of the
cause the runtime gives a bound method's error, the backend's words and its detail; it sits
apart from the platform module, so the fake leaves it in place. A settings section's list
changes through `useListChange` (`components/settings/useListChange.ts`), one at a time with
its controls held, a refusal keeping the draft it was made with. The toolbar's popover buttons
are `ToolbarPopoverButton`, their counts `CountDot` (`PopoverPanel.tsx`). Both sheets take
`SheetCommonProps` (`lib/sheet.ts`), which a host builds once; a host (`InboxView`, and
Autopilot's `useSheetHost`) hears its sheet move from one item to another through
`useSheetVisits`, which marks the item left and the one opened read once per opening;
`ReviewComposer` and `IssueComposer` both render `components/conversation/Composer.tsx`.
The shared classes are written out once, so Tailwind finds each whole:
`components/controls.tsx` holds the buttons (`buttonClass`, `primaryButtonClass`,
`quietButtonClass`, `smallButtonClass`, `ghostButtonClass`, and the rest), the icon buttons
(`iconButtonClass`, `smallIconButtonClass`, `dangerIconButtonClass`, and the toolbar's
`toolbarButtonClass` and `toolbarButtonShape`), the links (`linkClass`, and `inkLinkClass` for
a link in the text's color), the dialog shells (`modalClass`, `sheetDialogClass`), the
toolbar's bar (`toolbarClass`), and `ErrorText`, a refusal's words under its control;
`PopoverPanel.tsx` holds `panelClass`, a listbox's or menu's panel, and `PopoverPanel` floats
on a menu's shadow or a card chip's (`elevation`), moved sideways off the edge it lines up with
by `usePopover`'s `shift` (`shiftInto`: measured before it paints, inside every ancestor whose
overflow clips it and the window, a gap from their edges, so a right-aligned menu in a narrow
sidebar never opens past its edge); and `settings/rows.tsx` holds `groupClass`,
a settings card. `SearchBox` is the one plain search
field a page or a panel draws, over its repositories or whatever it lists, and `Terminal` the
terminal window, in the dark palette in either theme, that the inbox-nil party and the setup's
Finish log show.

**The launch dialogs.** `components/launchDialogs.tsx` defines each dialog a launch opens once,
for every control that opens it: the launch options (`LaunchDialog`, its `LaunchMenu`, and the
preview), the launch hold dialog, and Take over. Each is a chunk of its own, so the inbox's
first paint waits for none, and `LaunchControls`' first mount starts loading them all
(`preloadDialogs`). Each loader keeps the component it brought in, and each dialog chooses, once
as it mounts, that component when its code is in, else the `React.lazy` one: a lazy
component suspends on its first render even with its code loaded, which would show its
boundary's fallback and close a dialog already open. So a dialog drawn after its code came
never suspends, and a mounted one never changes component. `LaunchDialog`'s load waits for its
menu's.

**The tests' shared parts.** Beside the platform fake (whose `send` emits a window event inside
`act`), `src/test/` holds `async.ts` (`deferred`, a promise the test settles itself, and
`nextTask`), `stubs.ts` (`reduceMotion`, `followResizes`), `settings.tsx` (`renderSaving`, which
keeps the settings each save returns, as App does), and `clock.tsx` (`FIXED_NOW`, `isoAgo`, and
`renderAt`, a render on a clock standing still). Every test starts clean: `vitest.config.ts`'
`mockReset`, `restoreMocks`, and `unstubGlobals` put the fake's answers, the spies, and stubbed
globals back, and `test/setup.ts` unmounts and puts the real timers back after each test. So a
test stubs a global or turns fake timers on only in itself or a `beforeEach`, undoes neither,
and never undoes its describe's setup.

**Pickers and reviewers.** `components/Picker.tsx` is the one picker both sheets edit with: an
Edit button that opens a modal of rows to check and uncheck, filtered by its field, which
sends only what differs from the item (`changes`, by `Wanted`, what to have rather than what to
flip), moved out of `issue/IssuePickers.tsx`, whose label and assignee pickers build on it. A
row may carry an icon (an avatar) and a section heading; ↑ and ↓ move the active row from the
field (`aria-activedescendant`), Enter checks or unchecks it, ⌘↩ applies, and a busy note shows
under the `loading-bar`. `review/ReviewerPicker.tsx` builds the review sheet's on it, from
`ReviewerCandidates` (a local read the sheet makes while `canEditReviewers`): who the pull
request asks, GitHub's suggestions, the people who work on the repository, and its teams (keyed
`team:<slug>`), each with its picture and a hint from `lib/reviewers.ts` (what they did there),
filtered on the window alone by login, name, or slug, prefix matches first. Opened while the
directory says a read is due, it asks `LearnReviewers` once and says it is learning, keeping what
it knows and what the user checked; a failure is a quiet note. `lib/reviewers.ts` also turns the
checks into the edit's request, people and teams apart. The summary's reviewers offer the edit
inline too: a requested person's or team's row offers × to take the request off, and a
reviewer's (not the user's, the author's, or a bot's, which `ReviewEntry.mine` and `bot` tell)
↻ to ask them again, each row busy while its edit runs, and with no one asked it suggests up to
three people to ask in one click, with More… to open the picker. After an edit that GitHub made
any of, `ReviewSheet` builds the sheet again through `useReview`'s rebuild, which asks GitHub
nothing, since the inbox rewrote the card from GitHub's answer, and reads the candidates again
from the directory, which now ranks the people the user picked. `PeopleParts.tsx` holds what
the reviewers' and the assignees' lists share: a picker row's label (`personLabel`), the
row's hover button (`RowAction`), and the one-click chips (`SuggestionChips`).

**Assignees.** `Assignees.tsx` (`AssigneesSection`) is both sheets' list of who an item is
assigned to, in the review summary's small look or the issue details' (`look`): each person's
line with their picture and "(you)", and, while the user may edit (`AssigneesEditing`), Edit,
× on everyone's line but the user's (who takes themselves off with Unassign me), a line fading
while its edit runs, an inline alert when one fails, and, with no one assigned, up to three
one-click chips ("Assign yourself" first, then the author, `quickPicks`) and More…. It reads
whom to suggest itself (`AssigneeReads.candidates`, a local read) and again after each edit.
`AssigneePicker.tsx` builds the picker on `PickerDialog`: who it is assigned to first, the
user's row locked, then the sections `lib/assignees.ts` makes (`assigneeGroups`: Suggested,
Works on, Can be assigned in, In the owner, Found on GitHub), each row's hint saying why. Opened
while a read is due, it learns once; while the list is cut short (`searchesGitHub`), a pause of
300 ms in typing searches (`useQuery`'s `delay`), which asks GitHub only when Go finds nothing
kept answers the words, and its answer merges into the list (`mergeFound`). Pictures are read
for its first 100 people. The review sheet edits with `EditAssignees` and rebuilds with no read;
the issue sheet with its `Edit`, then reads the issue again. Settings › Reviewing › People shows
`PeopleSummary` and forgets it all (`ForgetPeople`) behind a confirmation.

**The inbox cards.** `ItemRow` (a `memo` export, which the render check counts) is a Signal
card: a grid of the edge, `card/CardAvatar`, the two-line body, the state column, and the
action column, its widths the `--card-state`, `--card-actions`, and `--card-primary`
properties on the row, which `@max-*` container variants narrow against the list's
`@container` (`ListSection`'s and `WatchingList`'s `<ul>`). `lib/nextStep.ts` is the pure
function that decides what a card puts first, the first matching rule winning: its edge, its
story (`card/CardStory`), its primary verb, whether that is quiet, and the situation (the
generated `launcher.Situation`) that picks the agent button's prompt. A pull request someone
else opened whose user's own verdict (`Details.viewerVerdict`) requests changes waits on its
author (`awaiting_author`, quiet, no situation; `lib/nudge.ts`' `awaitsAuthor`) until the
author pushes, and then asks for a re-review (`rereview`) before the rules for the user's own
pull request can call it theirs to fix. While it waits, its primary is `nudge`, loud in the
accent color on its grey edge, once a nudge is due (`StepInput.nudge`: `nudgeDue`, quiet for
`CardPrefs.nudgeAfterHours`, measured from the later of `updatedAt` and `nudgedAt`, never for a
bot), and its story says how long it has been quiet or when the user nudged them
(`nudgeWords`). The row reads both through `useNowAs`, and only where its edits can post a
nudge (`EditActions.onNudge`), so a host without one never shows the verb. `useItemActions`
holds the dialog (`components/NudgeDialog`'s `useNudge`, rendered by `InboxView` and
`useSheetHost`): it starts from the default Nudge reply (`ReplyList.nudge`, filled by
`FillReply`), posts through `InboxState.nudge`, which shows the snapshot it answers with,
marks the item read, and says so in a toast with View comment, or, with Then snooze ticked
(`CardPrefs.nudgeSnooze` starts it), snoozes it, the snooze's toast saying both. One the user approved says so (`approved_by_you`, quiet, no situation; with auto-merge
on, rule 16's story) until it is ready to merge, when rule 15 offers the merge, and its held
workflow runs ask for the user's approval as a review's do (`runs_held`, with Open). Neither
rule applies while a review request of the user's is open: the author asked again, so the
review rules decide. In the Snoozed and Dismissed tabs the story is the triage tag, then its
`state`, muted: the lead rules 1 to 19 give the item in its list. The row passes it every
input, and those that depend on the time (the card's session, whether a failed one is fresh,
back from a snooze, whether a nudge is due and its words) come from `useNowAs` as primitives,
so a clock tick renders no row.
The state column is `card/StateTrack` over `card/SizeGlyph` for a pull request, or
`card/IssueState`, without tooltips (the numbers are in the peek and the screen-reader
sentence), and `card/RiskChip` is the one chip for trust and sensitive files.
`card/CardActions` renders the primary and frames `LaunchControls`' `card` variant, which
follows the card's session (`cardSession`) and keeps its hold, take-over, and clone dialogs
mounted. The action column is two slots, so the controls line up down the list: the
primary's, as wide as the longest common primary (`--card-primary`, 5.625rem for Re-review;
a longer one, Show terminal or What happened, comes only beside the agent's ▾ and widens it,
`minmax(var(--card-primary), max-content)`; the primary's own width below `@3xl`), the
primary at its start, and the agent's, the rest of the column (`--card-actions`, 20.75rem at
full width, so its 14.75rem fits the common pills whole, "Claude working · 59m" with its
terminal button the widest; 16.875rem below `@5xl`), its control at its end. That control is one split control in every session state,
closed by the ▾: the launch, which opens the launch options as the ▾ does rather than
launching at once (the options' Open starts it), or a session's pill (and, while one works,
Show in terminal or Watch), the first part narrowing and cutting its words short, its age first, while the
marks and the ▾ keep their width, so nothing runs past a border. Its `column` and `peek`
render props keep its outcome a direct child of the row, so it spans `col-[3/-1]`, and hand
the peek its way to the launch options. With the card's
situation, its launch is named for the situation's prompt as the item's resolution lists it
(`Resolution.situations`, display only; an issue's is named for its own prompt), and the
launch request, the launch options' one re-resolve when they open, and their Preview all carry
the situation with `promptId` 0, so Go resolves that prompt and everything it brings; the card
itself asks nothing more, since the re-resolve on changed choices fires for a situation only
while the options show. Save as default writes the situation's field (`withChoices`). The four
situations' words and fields (the app's `DefaultPrompts` field, the launch defaults' column) are
one table, `situationInfo` in `lib/launch.ts`, which Settings → Launching, the launch defaults
form, the prompt library's usage words, and the import dialog read. The triage
(`TriageControls`' `card` mode, Show details, `ItemMenu`) is absolutely placed in the state
column's grid area, both lines named, over the end of the body once a narrow list drops the
column. A stretched toggle, the button right after the title (`absolute inset-0 z-0`,
`tabIndex` -1), opens `card/CardPeek` on a click of empty space; every control and every
titled element sits above it (`relative z-10`, and `z-20` while one of their panels is open,
since the panel's own `layer-popover` orders it only within its control), which keeps
`jsx-a11y` strict without a disable comment. The second line clips at the card's
edge, except while a chip's panel is open (its chips then `z-30`, over the triage) or
keyboard focus is in it, so the risk chip's and the ⚡ chip's panels show whole. The peek
reads only what the card has, so it costs no request.
The triage, the state column's fade, the primary's fill, and a compact card's hover row
follow `index.css`'s `card-lit` variant: the pointer on the card, keyboard focus in it
(`:focus-visible`, so the focus a click leaves on the stretched toggle doesn't count), or a
menu or panel it opened open (`[aria-haspopup][aria-expanded=true]`), besides the busy
card's own. A menu, panel, or dialog closing gives focus back to its button through
`lib/focusBack.ts` (`usePopover`, `useModalDialog`): ringed after a key, and after a pointer
with `focusVisible: false` (WebKit from Safari 18.4), so no ring is left behind and the card
goes dark once the pointer leaves. The halves of a split control (the agent's launch and its
▾, Snooze and its ▾) draw their focus rings inside themselves (`-outline-offset-2`), each
following its own shape within the control's border.
`card/CardAvatar` shows the author's picture through `Avatar.tsx`'s `Picture` (a circle, or
with `shape="square"` a bot's rounded square), whose stand-in, the initials or the bot mark,
shows until the picture decodes and when it fails. The pictures come from
`CardPicturesContext`, which App provides around the inbox alone (the Watching list's cards
keep their initials) from `useCardPictures` (`lib/pictures.ts`): keyed by the sorted authors
of every inbox item (people by login, bots by login and `authorAvatarUrl`), it asks the bound
avatars service's `PicturesFor` in requests of at most 25 authors, all at once, and merges each
answer as it lands, so a slow download holds back only its own; different authors cancel the
requests under way, and with Settings → Cards' Author pictures off, with its Density
compact (a compact card shows no avatar; turning compact cancels the requests under way), or
before the settings load, nothing is asked. Only `CardAvatar` reads the context, never `ItemRow`, so a picture
landing renders no row (the render check holds it). The cards' settings reach them as
`CardPrefs` (`lib/cardPrefs.ts`) through `CardPrefsContext`, which App builds from the
settings' primitives and provides around every page, so a settings save that leaves them as
they were renders no card. App hands the review sheet Settings → Reviewing's Ignored
workflows as `ignoring` (`IgnoredWorkflows`, `lib/ignoredWorkflows.ts`), through `InboxView`
and the Autopilot tab's `RowsKit` (null until the settings load, so nothing offers to ignore),
whose `save` stores a list through the window's settings queue and reads the inbox again; the sheet's **Ignore this workflow** saves, raises the host's toast
(`onToast`) with Undo, and `useReview` builds the sheet again with `Rebuild` once no read is
under way, so neither asks GitHub anything. The review sheet's card does the same: `useReview`
takes it as `cardKey` (its activity, title, draft, and the time its details were read, never its
triage), and a change builds the sheet again with `Rebuild` once no read is under way, once
however often it changed meanwhile, so the inbox's newer checks, readiness, labels, and review
requests reach an open sheet with no request (see internal/review). `listMark` (`lib/useReview.ts`)
marks what the sheet read of the checks, the held runs, and the conversation once it is older
than what the sheet shows (the card read the pull request since, `counts.newer`) or than
`oldAfter` (five minutes): faded under **Reading checks… · read 14m ago** while a read is under
way, and **Checks as read 14m ago** after one failed, or while the card's checks moved past the
list (`moved`); `ReadAge` (`review/ReviewLoading.tsx`) says it, and the summary counts the card's
checks (`countsSummary`) while they are newer than the list. A rebuild whose answer says `moved`
asks GitHub once, as `Refresh`, in the round after it, `readFloor` (20 seconds, the backend's
`refreshAfter`) after the sheet's last read at the soonest, on a timer the effect clears when the
sheet closes or steps on, and not while the reads after an approval run. Only a rebuild's answer
asks, never a read's: a read that fails is not made again until the card moves on, and the card's
changes during a read (`useQuery` cancels a request whose inputs change, so none is made while
one is under way) build the sheet once after it, which asks for one more read at most. The
window's own `focus` event (the browser's, as the setup's GitHub step uses, so no Go event) asks
once more, as `Refresh`, while the sheet's last read is older than `oldAfter` or it never read
it, but never while a read or a rebuild is under way, nor while the reads after an approval run.
Its `density` (Settings → Cards' Density, `cards.density`) picks the card: `ItemRow` reads
it and, compact, renders `card/CompactRow` from the same step, session, and clock reads, so
one set of rules drives both. A compact card is one 2.375rem grid row (`data-density` gives
`index.css` its `contain-intrinsic-size`) of a column per question: the edge, `KindMark`, the
repository, the title with its number, label dots, and pin, `CardStory` as a chip,
`RiskIcon`, `MiniTrack` (the track's steps as dots, from the same `trackOf`) or an issue's
`IssueTrack`, `SizeGrade` or `IssueGrade`, the age, and `CardActions`' compact go button.
Its hover row, placed absolutely over every column but the go button's, holds the triage,
the agent's slot (`LaunchControls`' `column`, which `ItemRow`'s `slot` hands either card),
Show details, and the menu, shown by opacity alone so the slot's dialogs keep their state.
The stretched toggle, the peek, the outcome, and the busy note are the comfortable card's,
and `card/CardParts` holds the parts both share (the edge's colors, `FilterText`,
`PinGlyph`, `TriageTag`). The render check runs in both densities with the same bounds.
The party at inbox nil is `InboxNil`, which `InboxView` shows once per party: one each time
the page reaches nil while it shows, or shows at nil. As each starts, `InboxView` reports it
to the milestones once (`reachedNil`, with `earned`, whether the inbox held something on this
visit, `cleared`, the most it held since the last party, and `loud`, Celebrations is
Milestones), and shows the party once they answer, a local write; an error shows it with no
moment, so no chip and no confetti. It picks the party's flourish then (`nextFlourish` in
`lib/nil.ts`, never the last one played, kept in a ref across visits, and no stamp without a
chip). `partyCompact` makes it the strip whenever a list shows below it, until the strip's run
asks for it in full, and always with Celebrations off. `InboxNil` throws confetti on its own
only for a moment whose `confetti` is set, in the Milestones mode; otherwise it plays its
flourish, each CSS alone in `nil.css` (`nil-glint`, `nil-hop`, `nil-burst`, `nil-spark`,
`nil-stamp`), held still with reduced motion, and with Celebrations off the strip carries
`nil-still`, which stops every animation in it. Its chip and the moment's quips come from
`lib/milestones.ts` (`achievementTitle`, `nilChip`, `momentQuip`), and its opening line from
`openingQuip` in `lib/nil.ts`. The Celebrations setting (`ui.celebrations`, a `settings.Celebrations`
choice) reaches the window through `lib/celebrations.ts`' context, which App provides from the
settings around everything, the setup included; `FinishStep` and `ReleaseResult` throw their
one-off bursts only in the Milestones mode (`throwsBursts`).
Its `unread` (Settings → Cards' Unread dots, `cards.unread`) shows `card/CardParts`'
`UnreadDot` while the item's `unread` holds, first on a comfortable card's first line and last
in a compact card's title cell, and offers Mark as read or Mark as unread in `ItemMenu`'s Inbox
actions and `CardPeek`'s action bar, which read `CardPrefsContext` themselves; off, it gates
only what shows, never a mark. The marks are `useInbox`'s `markRead(ids)` and `markUnread(id)`
(the bound `MarkSeen` and `MarkUnseen`), each shown at once as a `TriageChange` of `unread` and
reverted on an error, as triage is. A mark emits no `InboxUpdated`, so each reads the inbox back
once it is stored, numbered then, so no read asked before it shows the item as it was;
`applyTriage` returns the snapshot itself for a change that changes nothing, so marking a card
already read renders nothing. `marks` counts the stored marks, and a routine's Watching list
reads its rows again as it changes. The window, not the Actor, marks what the user saw and did:
`InboxView` and `useSheetHost`, as a sheet moves from one item to another (opening, stepping,
following a link, closing), mark the item it left and the one it opened in one call
(`useItemActions`' `seen`, which writes a failure to the console, since the user asked for
none of it); `openOnGitHub` (`TriageControls`) marks a card opened on GitHub from its title's
⌘-click, its ↗, the menu's Open on GitHub, or the peek's GitHub; and `useItemActions` marks a
card once Unassign me, Remove me as reviewer, or a nudge succeeds. Mark as read, Mark as unread, and the
opens on GitHub say a failure in a toast.
`card/CardGuide` teaches the card, a legend rather than a tour: the real `ItemRow`, drawn
from `lib/cardGuide.ts`'s fixed sample (`guideItem` and `guideSession`, their times counted
back from the clock as the guide opens, `guideResolution`, `guideLaunchOptions`) inside an
`inert` wrapper, with no triage, edits, or `onOpen`, its own `CardPrefsContext` (always
comfortable, Unread dots as set), an empty `CardPicturesContext`, and no
`AutopilotRowsContext`, so nothing in it takes a click or focus or asks for anything. In
full it numbers the parts the card marks with `data-guide` (`edge`, `avatar`, `title`,
`story`, `byline`, `risk`, `track`, `actions`, harmless on every card): each marker is placed
from its part's measured box against the sample's (the action column's first button, the
primary, for `actions`), measured again whenever the sample's box changes size
(`ResizeObserver`), and hidden until its part is laid out; then the legend. The boxes are
measured on the screen, where the dialog's pop-in still scales the sample as it opens, and a
transform changes no size, so each distance is divided by the box's scale (its measured
size over its `offsetWidth` and `offsetHeight`), or the markers furthest right would land
short of their parts.
Both variants end with the key (its track symbols take `card/StateTrack`'s
`trackStepClass`); in full, a second sample follows it (`NudgeSample`, `guideNudgeItem`): a
pull request whose author has been quiet two days since the user asked for changes, on a card
given inert edits with an `onNudge` and a threshold of a day, so it shows Nudge. `card/CardGuideDialog` shows the full guide in a modal; App owns whether it is
open, loads it on its first opening, so the inbox's chunk holds none of the guide, and
hands `onShowCardGuide` to `InboxView` (How to read a card, beside the last-refreshed time)
and `SettingsView` (Settings → Cards' row). Setup's Finish step shows the compact guide, and
App provides `CardPrefsContext` around the setup too. `useModalDialog`'s Tab wrap passes
over the controls of an inert part of a dialog, which take no focus.

**The Release page's rows.** `components/scripts/ReleaseRow.tsx` draws each group of the
Release page as a table, one line per repository: `releaseListClass` makes the group's `<ul>`
an `@container` and a grid of nine columns (the readiness edge, the checks, the repository,
the version, the count, the headline, the clone's notes, the age, and the action), and each
row, `CandidateRow` or `MutedRow` (its folder in the headline's place, Unmute in Release…'s),
is a `grid-cols-subgrid` of it across every column, each cell placed by its own column and
row classes, so the columns line up down the group and the headline takes the room the rest
leave. The rows follow the list's width, never the window's: under 56rem (`@max-4xl`) the
headline and the notes move to a second line under the repository, the edge spanning both,
and under 36rem (`@max-xl`) a row is a five-column grid of its own whose second line holds
the version, the headline, the notes, and the age (`metaClass`, `display: contents` while
they sit in the row's columns). `rowButtonClass` keeps Release… and Unmute quiet, muted words,
until the row is hovered or holds the focus (`group-hover/row`, `group-focus-within/row`).
A `CandidateRow` with Release… opens its sheet from a click anywhere on it: the `<li>` takes
the click itself, a pointer's shortcut for the button, which the keyboard and a screen reader
reach, rather than the inbox card's stretched button under raised cells, since nearly every
cell here has a tooltip that would otherwise have to sit above it and stop opening. Release…
stops its own click there, so the sheet opens once, and a click that ends a text selection
(`window.getSelection()`) opens nothing. A muted row's click does nothing: Unmute changes the
settings, so only its button does.
The pure half is `lib/release.ts`. `cloneNotes` turns a clone's state into `CloneNote`s,
each its `kind`, the few words the row shows (`text`), whether it `blocks` a release, and
its tooltip's `title`: uncommitted files, commits not pushed, a detached commit, a tag only
on this Mac, and a clone git can't read block; behind and a branch other than the default
don't, since magex pulls first and switches branches and back. `shownNotes` keeps only the
blockers while there are any, else behind, then the branch. `readiness` reads the edge of a
row with Release… (`releasable`), the first of failing checks, a blocking note, running
checks, passed checks, and none, and
`readinessCounts` counts a group's rows in that order for `ReadinessSummary`, the heading's
note, whose counts wrap onto a hidden second line so one that doesn't fit drops whole.
`sortCandidates` orders a group, whatever order the service sent, stably: the pinned
repositories first, then `newest` (the newest commit, then the name), `name` (`owner/name`
lowered, compared as the release service compares it), or `commits` (the most since the
newest version, then as `newest`). `sharedHeadlines` maps each headline to the repositories
of the group, as the search leaves it, whose newest commit it is, and a row shows the ×N
badge once `waveAt` (3) share it; `bumpKind` picks the count's feat or breaking chip from the
commits' headlines, as `bumpHint` words the sheet's hint. `ReleasePage` holds the sort the
user picks in local state over the stored one (`picked ?? releaseSort`), so the groups
reorder at once while App stores it with `settings.SetReleaseSort` (`scripts.releaseSort`)
and takes the settings it returns. `components/scripts/GroupHeading`, which Release, Hooks,
Repo scripts, and Deps share, is sticky at the top of the page's scroll container, over the page's
background, so a group's heading stays in view while its rows scroll under it, and its
`note` may be a node, given the rest of the heading's width, as the readiness summary is.

**The Deps page.** `components/scripts/DepsPage.tsx` shows the manifests service's page the way
the Release page shows its own. Its head counts the clones and modules and says when they were
read, or, in that place, how far a read has got (`readStep`, from `manifests:progress`), so no
row moves; then Read again, the clones left out (`leftOutText`), and, while a failed read keeps
the last page, how old that is. Above the groups sit a search over `owner/name` that opens every
group (`matchClones`), a chip for each kind of module that keeps only the clones with one
(`moduleKinds`, Go and JS, each counting the clones the search leaves), and the sort (Oldest,
A–Z, or Modules, the pinned repositories first in each, `sortClones`), held in local state over
the stored one while App stores it with `settings.SetDepsSort` (`scripts.depsSort`). The pure
half is `lib/manifests.ts`, which groups, sorts, and words what the service read and decides
nothing else. A clone's group is when its age counts from as the clock stands (`cloneGroup` over
`ageGroup` of its `ageFrom`, which the service works out from its stalest counted module and the
user's mark, a month being 30 days, as `lib/time` counts one: Stale over three months, Aging from
one, Fresh under one); a clone none of whose modules counts (`noneCounted`) goes in Fresh with a
green edge and no age; and a clone whose age isn't known (`ageFrom` '', as when its read failed)
goes in Stale, last in Oldest, with a muted edge, since nothing vouches for it and a folded Fresh
would hide it; the page renders again for the clock only once a clone changes group (`groupsAt`
through `useNowAs`). Oldest sorts by `ageFrom` too. Each group sits under the shared `GroupHeading`, Stale alone starting open
(`depsGroups`; the folds are kept in `scripts.sections` as `deps.stale`, `deps.aging`, and
`deps.fresh`), Stale shown even when empty, and its note is the legend of its rows' edges
(`DepsLegend` over `legendOf`: caution, warning, and success, and a muted "age unknown").
`DepsRow.tsx` draws a clone on one line of its group's grid (`depsListClass`, nine columns, each
row a subgrid of them, following the list's width, `@container`, as a Release row does): the
edge (`staleTone`), the repository, its modules and tools, its stalest manifest or its error,
its notes, the age (its stalest counted manifest's, a mark notwithstanding), then in one cell
the mark up to date (a quiet `CircleCheckIcon`, `data-checked` and green while the repository is
marked, offered by `canMark` to a clone read with a module that counts, and to any clone marked,
to unmark it) and Upgrade…, quiet until the row is hovered or holds the focus, and the chevron,
which, like a click on the row's empty space, unfolds `DepsDrawer.tsx`. The notes are the upgrade
an agent works on there (`upgradeSessions`, the sessions of number 0 and kind '' in
`useSessions`' `byItem`, the one shown picked as a card's is, `cardSession`), then when the user
marked it (`checkedAgo`, *checked 3d ago*, as of the clock, `checkedTitle` in its tooltip), then
`cloneNotes` (none counted, or age unknown, read at HEAD, shallow, not all read). The drawer lists
the modules a line each, those that count first, a module that doesn't marked *not counted* after
its path (`uncountedTitle` says why), each with its own Reveal in Finder, so Finder shows the file
its line names, then the workspace files, with Open in Terminal, Mark up to date (or Unmark up to
date), and Upgrade dependencies… at its foot; it reads nothing more. A mark calls
`MarkChecked` or `ClearChecked` through `useManifests`' `mark`, which shows the page the service
returns in place of the one shown, so the clone moves group at once without a read of the clones;
one under way holds every mark back (`marking`), and one that fails says why in the head
(`markError`). `ScriptsView`
reads the page through `lib/useManifests.ts` (`listManifests`, with refresh for Read again,
following `manifests:progress`) each time the tab shows while Scripts is on, whichever page is
open, for the rail's count of the clones in Stale (`staleCount`), and keeps one sheet or dialog
open at a time: a release request or a run it opens closes the Upgrade dialog. **Upgrade…** opens
`DepsUpgradeSheet.tsx`, a dialog in the launch options' own frame (`LaunchFrame`, which
`LaunchDialog` holds an item's launch options in too), as tall as what it holds, which reads the
prompt library, the agents, and the profiles; offers the prompts for repositories
(`promptApplies` with kind '', which offers nothing else), in its first section, headed as the
launch options' are: the two starters, `deps-minor` as Minor and patch, chosen, and `deps-major`
as Majors too, while the library keeps them, and the user's others under Another prompt, each
with an Edit that closes the dialog and opens that prompt on the Prompts tab (App's
`openPromptsAt` hands `PromptsView` a `PromptRequest`, which it opens as a library click would,
asking first over unsaved changes, and `PromptLibrary` scrolls the open prompt into sight);
resolves and plans the launch with `ResolveRepo` and `PreviewRepo` (`repoLaunchRequest`) as the
prompt, agent, or profile changes, the plan naming the worktree and its base in the dialog's
sentence and showing a refusal, such as every name for today's worktree being taken, before
Start, which waits for both and while either fails; shows the launch options' own settings
(`LaunchFields`, from `LaunchMenu.tsx`,
the worktree's sentence in place of its choice) and their preview (`LaunchPreviewSection`);
starts with `LaunchRepo`, a hold opening the launch hold dialog, whose Start another starts
beside the session (a session with no item named by its repository alone, never `#0`); and
closes once the upgrade starts, unless its launch worked something around, which it stays open
to say. The launch options take a `LaunchSubject` rather than an item: an item by its kind, or a
repository by kind '', its trust, and how its launch is planned (`preview`, and `key`, what
else the plan is made with), so a repository's launch reuses them, and for a repository they
offer only its prompts, show the worktree always on, and offer no situation, override, or save
as a default. Elsewhere, a prompt for repositories stays out of every item's list
(`promptApplies` with an item's kind), and the routine builder's agent and GitHub steps offer
only prompts written for items (`fitsKinds` in `builder/parts.tsx`), even before the routine
acts on any kind. The Prompts tab offers Repositories among what an agent prompt is for, never a
reply's (`PromptEditor`), lists such prompts under a group of their own (`PromptLibrary`),
previews them with the sample repository alone (`PromptPreview`'s `PreviewKind` `repo`), and
dims every variable a repository's launch leaves empty, marked Item only, or PR only for a pull
request's (`VariablesPanel`, by `PromptVariable.itemOnly`).

**The Worktrees page.** `components/WorktreesView.tsx` is `worktrees/PageHead` over a body that
scrolls on its own (`overflow-y-auto`, as `ReleasePage`'s does), so the clone headings stick in
it, and the head's progress, which takes the place of its line of counts, never moves a row. The
pure half is `lib/worktrees.ts` and `lib/worktreeParts.ts`, which word what the backend decided
and decide nothing themselves (the `can*` flags say what may be removed). `laneOf` puts a
worktree in one lane from the `Worktree` alone, so the toolbar's filter (`lib/worktreeFilter`,
which leaves the main checkout out and keeps a row showing an error) counts what the page
shows, and `attentionSort` orders a clone's rows by lane, then the newest commit. `stateOf`,
`workNotes`, and `agentLine` word a row's cells, `verbOf` its one verb, `removeWhy` why
Remove… or Force remove… can't run, and `safety` what removing it would cost, as `Words` data
the drawer's band, the confirmations, and the bulk confirmations' `SafetyLine` render.
`cloneSummary` is a clone's heading (its main checkout as a `MainNote`, its lanes counted),
`facetsOf` and `facetQuery` the head's chips, and `itemFacts` and `liveSessions` join what the
window already has, the inbox's items by `itemKey` and the followed sessions by their folder,
which add words and move no row to another lane. `lib/useWorktreeDetails` keeps the `Details`
read for one listing, which `useWorktrees` numbers (`listing`): a new number drops them (state
set while rendering), a ref dedupes the asks, so StrictMode's doubled effects make one call,
and an answer for an older listing is dropped. A failed ask leaves the ref, so the next read
asks again, but nothing retries on its own. An open drawer reads as it opens and after each
listing (`useEffectEvent` and an effect), and Force remove's confirmation as it mounts and
whenever a listing drops what it read while it's open, both through the page's one instance,
and `readable` keeps the main checkout, a folder that's gone, and one git couldn't read from
asking at all. `asReadNow` counts a worktree's files and commits as its details last did, and
the drawer's band and removal, the ⋯ menu's removals, and the confirmations all go by it, so a
read can hold a removal back but never allow one the `can*` flags refused. Details doesn't say
whether HEAD is the pull request's head (on GitHub for a fork's, though no remote-tracking
branch has it), so a count of commits equal to the listing's keeps the listing's `onGitHub`,
and one that changed counts as not on GitHub; `notOnGitHub` is that count, which the drawer's
column and Force remove's list go by, each saying where the rest are past the first 200 (`git
log`). Every clone shares one grid, `worktreeGridClass`
in `worktrees/layout.ts`, of nine columns, the verb's apart from the tools' so a launch's
control never widens them; each `CloneSection`, its `<ul>`, and each `WorktreeLine` row is a
`grid-cols-subgrid` of it, so the columns line up down the page, and the rows follow the
grid's width (`@container`), as the Release rows do. Nothing in the grid clips, for the sticky
headings and the menus' panels. A row's disclosure is its chevron, a `<button>` with
`aria-expanded` and `aria-controls` whose keys unfold (→) and fold (←) it and move between rows
(↑ ↓); the `<li>` takes a pointer's click as a shortcut for it, as a Release row does, and
leaves alone a click that ends a text selection or lands on a control, a menu, or a dialog a
control renders elsewhere, whose clicks still bubble through React. The page keeps the open
drawers as a set of paths, which outlives a listing. App hands it `onQuery`, which a chip
writes the toolbar's filter with, the sessions snapshot, the inbox's items, and
`launchFor(item, place)`, `LaunchControls`' `row` variant placed in the row's cells
(`LaunchPlace`).
