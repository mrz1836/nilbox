# frontend

[Docs](../../README.md) › [Architecture](../../README.md#architecture) · Code: [`frontend`](../../../frontend) · Guide: [Filtering](../../guide/filtering.md), [The window](../../guide/window.md)

**In this folder:**

- [The inbox cards](inbox-cards.md)
- [The Scripts tab's pages](scripts-pages.md)
- [The Worktrees page](worktrees-page.md)

`frontend/` is the React + TypeScript + Vite + Tailwind app. It reaches Go only through the
generated bindings in `frontend/bindings/`, funneled through `frontend/src/platform/`. The React
Compiler memoizes the build, so components hold no hand-written `useMemo` or `useCallback`, and the
gate's render check (`pnpm test:renders`) profiles the compiled inbox. Tests stand in for Go only
with the shared fake in `frontend/src/test/platform.tsx`.

Both sheets show a conversation with the same parts, never a copy of them:
`components/conversation/` (`Description`, `Timeline`, and `TimelineEntry`, which shows a review
with its line comments), `lib/timeline.ts` (its rows, what is new, a quote, and the counts), and
`lib/useTimeline.ts` (a visit's start and the earlier pages, joined). The review sheet shows it in
its Comments tab, beside Files (the description and the diff) and Commits (`review/CommitList`, the
commits since the user's review marked with the shared `Divider` the conversation's New uses, and
`review/SinceYourReview` above the tabs, `lib/commits.ts`), each kept mounted in an `Activity` while
another shows; the tab is the window's memory (`ui.reviewTab`, which a backup never holds), and so
is the order both sheets list a conversation in (`ui.conversationOrder`, oldest or newest first,
under the description), which the inbox shares between them.

Neither sheet shows an item a read of it finds merged or closed on GitHub, as a card can be until
the inbox's next refresh: `useEnded` (`lib/sheet.ts`) hands it to the sheet's `onEnded` once the
read's `state` says so, unless an action runs or the sheet shows the notice one left (the review
sheet's pushed to since or runs approved, the issue sheet's, as the text a close someone made first
kept from being posted). Its host, `InboxView` or `useSheetHost`, moves on as for any move: past the
card the way the sheet stepped to it (`Came`, the way and the cards it stepped through, `stepPast`,
which goes the other way when none is left that way, since the read took the card out of the list
already), else, in the inbox, back to the sheet a link to it was followed from, else closed;
`useItemActions`' `ended` says so in a toast ("acme-org/api#12 was merged", with Open on GitHub).
The read stored the item and told the window, so the card leaves the list with no request more, and
the move marks it read as any does.

`components/notifications/` is the Notifications page (`NotificationsView`, loaded on its first
visit), whose tabs are the app's own history (`AppNotificationsTab`) and the user's GitHub
notifications (`GitHubNotificationsTab`, with the Cleared view, `ClearedList`), each listed by day
(`DayGroups`); the toolbar's bell and GitHub button are quick views of it and share its rows
(`EntryRow`, `ThreadRow`), so a row is never copied. Its tab is the window's memory
(`ui.notificationsTab`); each tab's Show and Type live in App's `Nav`, so the toolbar's field
(`lib/notificationFilter.ts`) counts what the list shows. Opening the bell marks nothing: an entry
is marked read when opened, by its ✓, by a click on its banner (`event.RevealItem.NotificationID`,
checked against the entry's item, page, or run, since Clear history hands ids out again), or when
its item's sheet opens (`InboxView`'s `onItemOpened`, once per opening).

`components/autopilot/` is the Autopilot tab (the routine list, Needs you's cards, Activity, a
routine's Watching list on the inbox's own rows and sheets, the ⚡ chip, an item menu's Autopilot ▸,
`RoutineMenu`, `RunView`, an agent's run beside Activity, with its timeline a page at a time
(`lib/useRunTimeline.ts`, `lib/runView.ts`), `TakeOverDialog`, which the run view, the session
panel, and the launch hold dialog share, and `builder/`, the routine builder and its Test panel with
Preview, whose pure half is `lib/routineBuilder.ts`), its words in `lib/autopilot.ts`; a hold's
details are the launch hold dialog's own (`HoldDetails`), and a planned launch the launch menu's;
every row reads the routines' names and its chip through `AutopilotRowsContext`, and the inbox's row
actions are shared through `components/useItemActions.ts`. The window shows any of Autopilot only
while Autopilot (beta) is on (Settings → Labs, `LabsSection`): off, there is no tab, no Autopilot
settings section or banner switch (`hiddenOf` in `lib/settingsIndex.ts`), no Autopilot type among
the Notifications page's filters, and no `AutopilotRowsContext`, so no chip or mark; on, the tab's
label and the section's heading carry a `BetaBadge`. Full design: [The Autopilot
tab](../autopilot/autopilot-tab.md).

Theme colors come from the `--color-*` tokens `internal/brandgen` writes to
`frontend/src/generated/theme.css`, whose `@theme` block first resets Tailwind's own palette
(`--color-*: initial`), so a utility of Tailwind's, such as `text-white` or `bg-red-500`, builds to
nothing. What the components share besides is declared once in `index.css`'s `@theme inline`, each
as a literal, so a utility carries its value itself: the type scale (`text-micro` to `text-heading`,
eleven sizes in rem, none of which sets a line height), the shadows (`shadow-menu`, `shadow-card`,
`shadow-float`, `shadow-toast`, `shadow-terminal`, `shadow-control`, `shadow-drawer`, and
`inset-shadow-control`), the dim behind a modal dialog (`backdrop:bg-scrim`, a literal since, before
Safari 17.4, `::backdrop` sees none of the page's custom properties), `paper` (white in both themes:
a switch's knob, a radio button's dot, a slider's thumb), and `opacity-disabled`, the one fade of a
disabled control (0.6); beside them, as `@utility` rules, the washes (`wash-success`, `wash-danger`,
`wash-accent`, `wash-warning`, and the neutral `wash-raised`, a color mixed into the surface) and
`layer-popover` (z-40, over the toolbar's z-30 and under a toast's z-50), the layer every popover
and menu panel opens on. A hue's tints take their alpha from one ladder (/5, /10, /14, /22, /45,
/60). `lib/tone.ts` gives a state's tone its text and fill classes (`toneText`, `toneFill`), and a
tint stays in the map of the domain that draws it.

Checkboxes and radio buttons take the `checkbox` and `radio` classes (`frontend/src/index.css`),
which draw them in the app's own style: macOS draws a native one grey whenever it counts the window
as inactive. Text is sized in rem, never px (`text-caption`, from the type scale, not
`text-[11px]`): the text size setting scales the root's font size, which only rem follows, and the
shell passes the stored size in the page's URL (`internal/desktop.PageURL`) so the first frame is
already at it. A repoguard test fails on px text. Only chrome lined up with the native window, such
as the toolbar's height and its inset beside the window controls, and hairlines stay in px. Another,
`TestFrontendStylesComeFromTokens`, fails on a style written around the tokens: a Tailwind palette
utility or variable, a color written into a class, a style, or a stylesheet outside a token's own
declaration, an arbitrary text size or backdrop, and a disabled fade other than `opacity-disabled`.
It has four exceptions, each with its reason and each matching only the whole line it is for, eight
lines in all: the checkbox's data-URL check and dash, the slider thumb's shadow, the inbox-nil
grid's masks, and the card guide's sample label colors. ESLint fails on a checkbox or radio input in
the window without its class (`controlClassSyntax` in `eslint.config.js`), unless it is hidden
(`sr-only`) or a switch.

## The build and its checks

The root `tsconfig.json` only references three projects. `tsconfig.app.json` checks `src/` and
`bindings/` against the browser (lib ES2023 and DOM, the `vite/client` types, and none of Node's, so
the window's code can't name `process` or `Buffer`); `tsconfig.node.json` checks `vite.config.ts`
and the two Vitest configs against Node (its types, no DOM); and `tsconfig.test-node.json` checks
the one test that runs Node's own modules, `src/test/devtoolsAgent.test.ts`, which the app's project
leaves out, against both (DOM and Node's types). All three keep the same strict flags and `noEmit`.
`pnpm typecheck` checks each with `tsc -p`, so nothing writes build info, and typescript-eslint's
`projectService` opens every linted file in the project that includes it, with no default project to
fall back on. `pnpm build` runs that typecheck before `vite build`, so `magex frontend:build`,
`app:build`, and a release check all three projects too. ESLint bans importing `useMemo` and
`useCallback` anywhere in `src/` (`noMemoHooks` in `eslint.config.js`). The gate (`magex
frontend:gate`) runs the format check, lint, the typecheck, the tests with coverage, the render
check, and the production build, which `chunkLimit` in `vite.config.ts` fails on a JS chunk over
`chunkSizeWarningLimit` (750 kB, above the biggest chunk, the entry, which holds the inbox, about
473 kB), where Vite only warns; then it checks the first paint against its budgets
(`magefiles/firstpaint.go`: the entry, the shared chunk of about 394 kB it preloads, which holds
most of what the first paint and the pages share, and Rolldown's runtime, held to 950,000 bytes of
script, 325,000 gzipped, and the CSS to 140,000, 25,000 gzipped). The bundle targets Safari 16.4,
the WebKit of the oldest supported macOS, 13.3.

## Shared hooks and helpers

Each of these is the one way the pages do it, never a copy. `useOn` (`lib/useOn.ts`) hears a window
event while the component is mounted (or `enabled`) and `useKeyDown` (`lib/keys.ts`) every key that
bubbles up to the document or the window, each calling the latest handler rendered, so a new one
never subscribes again. `lib/dialogs.ts` opens the system's file dialogs (`pickFileToOpen`,
`pickFileToSave`): the path, '' on a cancel, or the failure worded for the page. `logFailure`
(`lib/call.ts`) runs a call whose failure the user asked nothing about and writes it to the console;
`terminalLabel` (`lib/terminals.ts`) names a terminal app, or a fallback for one this build doesn't
know; `Select<T>` (`components/controls.tsx`) is the native select of a closed set of strings,
handing `onChange` one of its own options' values; and `unlisted` (`lib/unlisted.ts`) answers, from
a switch's default, a value Go may send that its union leaves out, so a constant Go adds fails the
typecheck until it has a case. `platform/cause.ts` (`causeOf`) is the one decoder of the cause the
runtime gives a bound method's error, the backend's words and its detail; it sits apart from the
platform module, so the fake leaves it in place. A settings section's list changes through
`useListChange` (`components/settings/useListChange.ts`), one at a time with its controls held, a
refusal keeping the draft it was made with. The toolbar's popover buttons are
`ToolbarPopoverButton`, their counts `CountDot` (`PopoverPanel.tsx`). Both sheets take
`SheetCommonProps` (`lib/sheet.ts`), which a host builds once; a host (`InboxView`, and Autopilot's
`useSheetHost`) hears its sheet move from one item to another through `useSheetVisits`, which marks
the item left and the one opened read once per opening; `ReviewComposer` and `IssueComposer` both
render `components/conversation/Composer.tsx`. The shared classes are written out once, so Tailwind
finds each whole: `components/controls.tsx` holds the buttons (`buttonClass`, `primaryButtonClass`,
`quietButtonClass`, `smallButtonClass`, `ghostButtonClass`, and the rest), the icon buttons
(`iconButtonClass`, `smallIconButtonClass`, `dangerIconButtonClass`, and the toolbar's
`toolbarButtonClass` and `toolbarButtonShape`), the links (`linkClass`, and `inkLinkClass` for a
link in the text's color), the dialog shells (`modalClass`, `sheetDialogClass`), the toolbar's bar
(`toolbarClass`), and `ErrorText`, a refusal's words under its control; `PopoverPanel.tsx` holds
`panelClass`, a listbox's or menu's panel, and `PopoverPanel` floats on a menu's shadow or a card
chip's (`elevation`), moved sideways off the edge it lines up with by `usePopover`'s `shift`
(`shiftInto`: measured before it paints, inside every ancestor whose overflow clips it and the
window, a gap from their edges, so a right-aligned menu in a narrow sidebar never opens past its
edge); and `settings/rows.tsx` holds `groupClass`, a settings card. `SearchBox` is the one plain
search field a page or a panel draws, over its repositories or whatever it lists, and `Terminal` the
terminal window, in the dark palette in either theme, that the inbox-nil party and the setup's
Finish log show.

## The launch dialogs

`components/launchDialogs.tsx` defines each dialog a launch opens once, for every control that opens
it: the launch options (`LaunchDialog`, its `LaunchMenu`, and the preview), the launch hold dialog,
and Take over. Each is a chunk of its own, so the inbox's first paint waits for none, and
`LaunchControls`' first mount starts loading them all (`preloadDialogs`). Each loader keeps the
component it brought in, and each dialog chooses, once as it mounts, that component when its code is
in, else the `React.lazy` one: a lazy component suspends on its first render even with its code
loaded, which would show its boundary's fallback and close a dialog already open. So a dialog drawn
after its code came never suspends, and a mounted one never changes component. `LaunchDialog`'s load
waits for its menu's.

## The tests' shared parts

Beside the platform fake (whose `send` emits a window event inside `act`), `src/test/` holds
`async.ts` (`deferred`, a promise the test settles itself, and `nextTask`), `stubs.ts`
(`reduceMotion`, `followResizes`), `settings.tsx` (`renderSaving`, which keeps the settings each
save returns, as App does), and `clock.tsx` (`FIXED_NOW`, `isoAgo`, and `renderAt`, a render on a
clock standing still). Every test starts clean: `vitest.config.ts`' `mockReset`, `restoreMocks`, and
`unstubGlobals` put the fake's answers, the spies, and stubbed globals back, and `test/setup.ts`
unmounts and puts the real timers back after each test. So a test stubs a global or turns fake
timers on only in itself or a `beforeEach`, undoes neither, and never undoes its describe's setup.

## Pickers and reviewers

`components/Picker.tsx` is the one picker both sheets edit with: an Edit button that opens a modal
of rows to check and uncheck, filtered by its field, which sends only what differs from the item
(`changes`, by `Wanted`, what to have rather than what to flip), moved out of
`issue/IssuePickers.tsx`, whose label picker builds on it; `AssigneePicker.tsx` builds both sheets'
assignee picker on its `PickerDialog`. A row may carry an icon (an
avatar) and a section heading; ↑ and ↓ move the active row from the field (`aria-activedescendant`),
Enter checks or unchecks it, ⌘↩ applies, and a busy note shows under the `loading-bar`.
`review/ReviewerPicker.tsx` builds the review sheet's on it, from `ReviewerCandidates` (a local read
the sheet makes while `canEditReviewers`): who the pull request asks, GitHub's suggestions, the
people who work on the repository, and its teams (keyed `team:<slug>`), each with its picture and a
hint from `lib/reviewers.ts` (what they did there), filtered on the window alone by login, name, or
slug, prefix matches first. Opened while the directory says a read is due, it asks `LearnReviewers`
once and says it is learning, keeping what it knows and what the user checked; a failure is a quiet
note. `lib/reviewers.ts` also turns the checks into the edit's request, people and teams apart. The
summary's reviewers offer the edit inline too: a requested person's or team's row offers × to take
the request off, and a reviewer's (not the user's, the author's, or a bot's, which
`ReviewEntry.mine` and `bot` tell) ↻ to ask them again, each row busy while its edit runs, and with
no one asked it suggests up to three people to ask in one click, with More… to open the picker.
The user's own request is never the edit's (Go refuses it, `ErrOwnReviewRequest`, and the picker
locks their row): while the card says GitHub asks them by name (`reviewRequest` `you`),
`ReviewSheet`'s `removeMe` puts × on their row and Remove me as reviewer at the action menu's foot
(`ActionButton`'s `extra`), with or without the right to edit the others. It runs the inbox's own
edit through the host, as the issue sheet's Unassign me does: `InboxView`'s `removeReviewHere`
calls `useItemActions`' `removeReviewOn`, the card menu's path, and closes the sheet once GitHub
took the user off, the toast saying so; a refusal stays in the sheet as an alert.
After an edit that GitHub made any of, `ReviewSheet` builds the sheet again through `useReview`'s
rebuild, which asks GitHub nothing, since the inbox rewrote the card from GitHub's answer, and reads
the candidates again from the directory, which now ranks the people the user picked.
`PeopleParts.tsx` holds what the reviewers' and the assignees' lists share: a picker row's label
(`personLabel`), the row's hover button (`RowAction`), and the one-click chips (`SuggestionChips`).

## Assignees

`Assignees.tsx` (`AssigneesSection`) is both sheets' list of who an item is assigned to, in the
review summary's small look or the issue details' (`look`): each person's line with their picture
and "(you)", and, while the user may edit (`AssigneesEditing`), Edit, × on everyone's line but the
user's (who takes themselves off with Unassign me), a line fading while its edit runs, an inline
alert when one fails, and, with no one assigned, up to three one-click chips ("Assign yourself"
first, then the author, `quickPicks`) and More…. It reads whom to suggest itself
(`AssigneeReads.candidates`, a local read) and again after each edit. `AssigneePicker.tsx` builds
the picker on `PickerDialog`: who it is assigned to first, the user's row locked, then the sections
`lib/assignees.ts` makes (`assigneeGroups`: Suggested, Works on, Can be assigned in, In the owner,
Found on GitHub), each row's hint saying why. Opened while a read is due, it learns once; while the
list is cut short (`searchesGitHub`), a pause of 300 ms in typing searches (`useQuery`'s `delay`),
which asks GitHub only when Go finds nothing kept answers the words, and its answer merges into the
list (`mergeFound`). Pictures are read for its first 100 people. The review sheet edits with
`EditAssignees` and rebuilds with no read; the issue sheet with its `Edit`, then reads the issue
again. Settings › Reviewing › People shows `PeopleSummary` and forgets it all (`ForgetPeople`)
behind a confirmation.
