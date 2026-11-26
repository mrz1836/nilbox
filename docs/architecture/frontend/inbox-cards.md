# The inbox cards

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`frontend`](README.md) · Code: [`frontend/src`](../../../frontend/src) · Guide: [Inbox cards](../../guide/inbox-cards.md)

`ItemRow` (a `memo` export, which the render check counts) is a Signal card: a grid of the edge,
`card/CardAvatar`, the two-line body, the state column, and the action column, its widths the
`--card-state`, `--card-actions`, and `--card-primary` properties on the row, which `@max-*`
container variants narrow against the list's `@container` (`ListSection`'s and `WatchingList`'s
`<ul>`). `lib/nextStep.ts` is the pure function that decides what a card puts first, the first
matching rule winning: its edge, its story (`card/CardStory`), its primary verb, whether that is
quiet, and the situation (the generated `launcher.Situation`) that picks the agent button's prompt.

A pull request someone else opened whose user's own verdict (`Details.viewerVerdict`) requests
changes waits on its author (`awaiting_author`, quiet, no situation; `lib/nudge.ts`' `awaitsAuthor`)
until the author pushes, and then asks for a re-review (`rereview`) before the rules for the user's
own pull request can call it theirs to fix. While it waits, its primary is `nudge`, loud in the
accent color on its grey edge, once a nudge is due (`StepInput.nudge`: `nudgeDue`, quiet for
`CardPrefs.nudgeAfterHours`, measured from the later of `updatedAt` and `nudgedAt`, never for a
bot), and its story says how long it has been quiet or when the user nudged them (`nudgeWords`). The
row reads both through `useNowAs`, and only where its edits can post a nudge
(`EditActions.onNudge`), so a host without one never shows the verb. `useItemActions` holds the
dialog (`components/NudgeDialog`'s `useNudge`, rendered by `InboxView` and `useSheetHost`): it
starts from the default Nudge reply (`ReplyList.nudge`, filled by `FillReply`), posts through
`InboxState.nudge`, which shows the snapshot it answers with, marks the item read, and says so in a
toast with View comment, or, with Then snooze ticked (`CardPrefs.nudgeSnooze` starts it), snoozes
it, the snooze's toast saying both. One the user approved says so (`approved_by_you`, quiet, no
situation; with auto-merge on, rule 16's story) until it is ready to merge, when rule 15 offers the
merge, and its held workflow runs ask for the user's approval as a review's do (`runs_held`, with
Open). Neither rule applies while a review request of the user's is open: the author asked again, so
the review rules decide.

In the Snoozed and Dismissed tabs the story is the triage tag, then its `state`, muted: the lead
rules 1 to 19 give the item in its list. The row passes it every input, and those that depend on the
time (the card's session, whether a failed one is fresh, back from a snooze, whether a nudge is due
and its words) come from `useNowAs` as primitives, so a clock tick renders no row.

The state column is `card/StateTrack` over `card/SizeGlyph` for a pull request, or
`card/IssueState`, without tooltips (the numbers are in the peek and the screen-reader sentence),
and `card/RiskChip` is the one chip for trust and sensitive files. `card/CardActions` renders the
primary and frames `LaunchControls`' `card` variant, which follows the card's session
(`cardSession`) and keeps its hold, take-over, and clone dialogs mounted. The card's session is the
newest running, else the newest that ended in the last day, unless it ended with nothing to read
(`silent`: no last message and no review, merge, or close; a failed one still shows) or the item
has moved past it (`superseded`): its review was left on a commit other than `Details.headOid`, or
`Item.reviewRequestedAt` is later than its end, as when the author pushes and asks again or the user
re-requests their own review. Then the slot offers a fresh launch, the situation's, in place of its
Findings. The action column is two
slots, so the controls line up down the list: the primary's, as wide as the longest common primary
(`--card-primary`, 5.625rem for Re-review; a longer one, Show terminal or What happened, comes only
beside the agent's ▾ and widens it, `minmax(var(--card-primary), max-content)`; the primary's own
width below `@3xl`), the primary at its start, and the agent's, the rest of the column
(`--card-actions`, 20.75rem at full width, so its 14.75rem fits the common pills whole, "Claude
working · 59m" with its terminal button the widest; 16.875rem below `@5xl`), its control at its end.
That control is one split control in every session state, closed by the ▾: the launch, which opens
the launch options as the ▾ does rather than launching at once (the options' Open starts it), or a
session's pill (and, while one works, Show in terminal or Watch), the first part narrowing and
cutting its words short, its age first, while the marks and the ▾ keep their width, so nothing runs
past a border. Its `column` and `peek` render props keep its outcome a direct child of the row, so
it spans `col-[3/-1]`, and hand the peek its way to the launch options.

With the card's situation, its launch is named for the situation's prompt as the item's resolution
lists it (`Resolution.situations`, display only; an issue's is named for its own prompt), and the
launch request, the launch options' one re-resolve when they open, and their Preview all carry the
situation with `promptId` 0, so Go resolves that prompt and everything it brings; the card itself
asks nothing more, since the re-resolve on changed choices fires for a situation only while the
options show. Save as default writes the situation's field (`withChoices`). The four situations'
words and fields (the app's `DefaultPrompts` field, the launch defaults' column) are one table,
`situationInfo` in `lib/launch.ts`, which Settings → Launching, the launch defaults form, the prompt
library's usage words, and the import dialog read.

The triage (`TriageControls`' `card` mode, Show details, `ItemMenu`) is absolutely placed in the
state column's grid area, both lines named, over the end of the body once a narrow list drops the
column. A stretched toggle, the button right after the title (`absolute inset-0 z-0`, `tabIndex`
-1), opens `card/CardPeek` on a click of empty space; every control and every titled element sits
above it (`relative z-10`, and `z-20` while one of their panels is open, since the panel's own
`layer-popover` orders it only within its control), which keeps `jsx-a11y` strict without a disable
comment. The second line clips at the card's edge, except while a chip's panel is open (its chips
then `z-30`, over the triage) or keyboard focus is in it, so the risk chip's and the ⚡ chip's panels
show whole. The peek reads only what the card has, so it costs no request.

The triage, the state column's fade, the primary's fill, and a compact card's hover row follow
`index.css`'s `card-lit` variant: the pointer on the card, keyboard focus in it (`:focus-visible`,
so the focus a click leaves on the stretched toggle doesn't count), or a menu or panel it opened
open (`[aria-haspopup][aria-expanded=true]`), besides the busy card's own. A menu, panel, or dialog
closing gives focus back to its button through `lib/focusBack.ts` (`usePopover`, `useModalDialog`):
ringed after a key, and after a pointer with `focusVisible: false` (WebKit from Safari 18.4), so no
ring is left behind and the card goes dark once the pointer leaves. The halves of a split control
(the agent's launch and its ▾, Snooze and its ▾) draw their focus rings inside themselves
(`-outline-offset-2`), each following its own shape within the control's border.

`card/CardAvatar` shows the author's picture through `Avatar.tsx`'s `Picture` (a circle, or with
`shape="square"` a bot's rounded square), whose stand-in, the initials or the bot mark, shows until
the picture decodes and when it fails. The pictures come from `CardPicturesContext`, which App
provides around the inbox alone (the Watching list's cards keep their initials) from
`useCardPictures` (`lib/pictures.ts`): keyed by the sorted authors of every inbox item (people by
login, bots by login and `authorAvatarUrl`), it asks the bound avatars service's `PicturesFor` in
requests of at most 25 authors, all at once, and merges each answer as it lands, so a slow download
holds back only its own; different authors cancel the requests under way, and with Settings → Cards'
Author pictures off, with its Density compact (a compact card shows no avatar; turning compact
cancels the requests under way), or before the settings load, nothing is asked. Only `CardAvatar`
reads the context, never `ItemRow`, so a picture landing renders no row (the render check holds it).

The cards' settings reach them as `CardPrefs` (`lib/cardPrefs.ts`) through `CardPrefsContext`, which
App builds from the settings' primitives and provides around every page, so a settings save that
leaves them as they were renders no card. App hands the review sheet Settings → Reviewing's Ignored
workflows as `ignoring` (`IgnoredWorkflows`, `lib/ignoredWorkflows.ts`), through `InboxView` and the
Autopilot tab's `RowsKit` (null until the settings load, so nothing offers to ignore), whose `save`
stores a list through the window's settings queue and reads the inbox again; the sheet's **Ignore
this workflow** saves, raises the host's toast (`onToast`) with Undo, and `useReview` builds the
sheet again with `Rebuild` once no read is under way, so neither asks GitHub anything. The review
sheet's card does the same: `useReview` takes it as `cardKey` (its activity, title, draft, and the
time its details were read, never its triage), and a change builds the sheet again with `Rebuild`
once no read is under way, once however often it changed meanwhile, so the inbox's newer checks,
readiness, labels, and review requests reach an open sheet with no request (see internal/review).

`listMark` (`lib/useReview.ts`) marks what the sheet read of the checks, the held runs, and the
conversation once it is older than what the sheet shows (the card read the pull request since,
`counts.newer`) or than `oldAfter` (five minutes): faded under **Reading checks… · read 14m ago**
while a read is under way, and **Checks as read 14m ago** after one failed, or while the card's
checks moved past the list (`moved`); `ReadAge` (`review/ReviewLoading.tsx`) says it, and the
summary counts the card's checks (`countsSummary`) while they are newer than the list. A rebuild
whose answer says `moved` asks GitHub once, as `Refresh`, in the round after it, `readFloor` (20
seconds, the backend's `refreshAfter`) after the sheet's last read at the soonest, on a timer the
effect clears when the sheet closes or steps on, and not while the reads after an approval run. Only
a rebuild's answer asks, never a read's: a read that fails is not made again until the card moves
on, and the card's changes during a read (`useQuery` cancels a request whose inputs change, so none
is made while one is under way) build the sheet once after it, which asks for one more read at most.
The window's own `focus` event (the browser's, as the setup's GitHub step uses, so no Go event) asks
once more, as `Refresh`, while the sheet's last read is older than `oldAfter` or it never read it,
but never while a read or a rebuild is under way, nor while the reads after an approval run.

Its `density` (Settings → Cards' Density, `cards.density`) picks the card: `ItemRow` reads it and,
compact, renders `card/CompactRow` from the same step, session, and clock reads, so one set of rules
drives both. A compact card is one 2.375rem grid row (`data-density` marks it for the tests and
the render check) of a column per question: the edge, `KindMark`, the repository, the title
with its number, label dots, and pin, `CardStory` as a chip, `RiskIcon`, `MiniTrack` (the track's
steps as dots, from the same `trackOf`) or an issue's `IssueTrack`, `SizeGrade` or `IssueGrade`, the
age, and `CardActions`' compact go button. Its hover row, placed absolutely over every column but
the go button's, holds the triage, the agent's slot (`LaunchControls`' `column`, which `ItemRow`'s
`slot` hands either card), Show details, and the menu, shown by opacity alone so the slot's dialogs
keep their state. The stretched toggle, the peek, the outcome, and the busy note are the comfortable
card's, and `card/CardParts` holds the parts both share (the edge's colors, `FilterText`,
`PinGlyph`, `TriageTag`). The render check runs in both densities with the same bounds.

The party at inbox nil is `InboxNil`, which `InboxView` shows once per party: one each time the page
reaches nil while it shows, or shows at nil. As each starts, `InboxView` reports it to the
milestones once (`reachedNil`, with `earned`, whether the inbox held something on this visit,
`cleared`, the most it held since the last party, and `loud`, Celebrations is Milestones), and shows
the party once they answer, a local write; an error shows it with no moment, so no chip and no
confetti. It picks the party's flourish then (`nextFlourish` in `lib/nil.ts`, never the last one
played, kept in a ref across visits, and no stamp without a chip). `partyCompact` makes it the strip
whenever a list shows below it, until the strip's run asks for it in full, and always with
Celebrations off. `InboxNil` throws confetti on its own only for a moment whose `confetti` is set,
in the Milestones mode; otherwise it plays its flourish, each CSS alone in `nil.css` (`nil-glint`,
`nil-hop`, `nil-burst`, `nil-spark`, `nil-stamp`), held still with reduced motion, and with
Celebrations off the strip carries `nil-still`, which stops every animation in it. Its chip and the
moment's quips come from `lib/milestones.ts` (`achievementTitle`, `nilChip`, `momentQuip`), and its
opening line from `openingQuip` in `lib/nil.ts`. The Celebrations setting (`ui.celebrations`, a
`settings.Celebrations` choice) reaches the window through `lib/celebrations.ts`' context, which App
provides from the settings around everything, the setup included; `FinishStep` and `ReleaseResult`
throw their one-off bursts only in the Milestones mode (`throwsBursts`).

Its `unread` (Settings → Cards' Unread dots, `cards.unread`) shows `card/CardParts`' `UnreadDot`
while the item's `unread` holds, first on a comfortable card's first line and last in a compact
card's title cell, and offers Mark as read or Mark as unread in `ItemMenu`'s Inbox actions and
`CardPeek`'s action bar, which read `CardPrefsContext` themselves; off, it gates only what shows,
never a mark. The marks are `useInbox`'s `markRead(ids)` and `markUnread(id)` (the bound `MarkSeen`
and `MarkUnseen`), each shown at once as a `TriageChange` of `unread` and reverted on an error, as
triage is. A mark emits no `InboxUpdated`, so each reads the inbox back once it is stored, numbered
then, so no read asked before it shows the item as it was; `applyTriage` returns the snapshot itself
for a change that changes nothing, so marking a card already read renders nothing. `marks` counts
the stored marks, and a routine's Watching list reads its rows again as it changes.

The window, not the Actor, marks what the user saw and did: `InboxView` and `useSheetHost`, as a
sheet moves from one item to another (opening, stepping, following a link, closing), mark the item
it left and the one it opened in one call (`useItemActions`' `seen`, which writes a failure to the
console, since the user asked for none of it); `openOnGitHub` (`TriageControls`) marks a card opened
on GitHub from its title's ⌘-click, its ↗, the menu's Open on GitHub, or the peek's GitHub; and
`useItemActions` marks a card once Unassign me, Remove me as reviewer, or a nudge succeeds. Mark as
read, Mark as unread, and the opens on GitHub say a failure in a toast.

`card/CardGuide` teaches the card, a legend rather than a tour: the real `ItemRow`, drawn from
`lib/cardGuide.ts`'s fixed sample (`guideItem` and `guideSession`, their times counted back from the
clock as the guide opens, `guideResolution`, `guideLaunchOptions`) inside an `inert` wrapper, with
no triage, edits, or `onOpen`, its own `CardPrefsContext` (always comfortable, Unread dots as set),
an empty `CardPicturesContext`, and no `AutopilotRowsContext`, so nothing in it takes a click or
focus or asks for anything. In full it numbers the parts the card marks with `data-guide` (`edge`,
`avatar`, `title`, `story`, `byline`, `risk`, `track`, `actions`, harmless on every card): each
marker is placed from its part's measured box against the sample's (the action column's first
button, the primary, for `actions`), measured again whenever the sample's box changes size
(`ResizeObserver`), and hidden until its part is laid out; then the legend. The boxes are measured
on the screen, where the dialog's pop-in still scales the sample as it opens, and a transform
changes no size, so each distance is divided by the box's scale (its measured size over its
`offsetWidth` and `offsetHeight`), or the markers furthest right would land short of their parts.
Both variants end with the key (its track symbols take `card/StateTrack`'s `trackStepClass`); in
full, a second sample follows it (`NudgeSample`, `guideNudgeItem`): a pull request whose author has
been quiet two days since the user asked for changes, on a card given inert edits with an `onNudge`
and a threshold of a day, so it shows Nudge.

`card/CardGuideDialog` shows the full guide in a modal; App owns whether it is open, loads it on its
first opening, so the inbox's chunk holds none of the guide, and hands `onShowCardGuide` to
`InboxView` (How to read a card, beside the last-refreshed time) and `SettingsView` (Settings →
Cards' row). Setup's Finish step shows the compact guide, and App provides `CardPrefsContext` around
the setup too. `useModalDialog`'s Tab wrap passes over the controls of an inert part of a dialog,
which take no focus.
