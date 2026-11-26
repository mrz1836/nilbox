# The Autopilot tab

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`frontend/src/components/autopilot`](../../../frontend/src/components/autopilot), [`internal/autopilot`](../../../internal/autopilot) · Guide: [Autopilot](../../guide/autopilot/README.md), [Building a routine](../../guide/autopilot/building-a-routine.md)

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
  background run offers **Watch it** and **Take over**, never Start another. The run view takes
  the item's sessions (`useSheetHost`'s `RowsKit.sessions`, App's `useSessions`) and finds the
  run's session among them by `sessionId`, and the session that carries its conversation on by
  its `carriedBy` (`lib/sessions.ts`'s `carrierOf`): it offers **Show in terminal**
  (`focusSession`) for the run's own while it `canFocus` (a run in the terminal), else for the
  carrier while that one does, and holds back the backend's `canTakeOver` while anything carries
  the conversation on, as the session panel does; with no sessions followed, it offers what the
  backend says.

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
