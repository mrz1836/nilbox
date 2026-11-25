import { Fragment, memo, use, useId, useState, type ReactNode } from 'react'

import { CardPrefsContext } from '../lib/cardPrefs'
import { counted } from '../lib/format'
import { labelsShown, useLabelRoom } from '../lib/labelFit'
import { HiddenLabelsContext } from '../lib/labels'
import { agentName, type LaunchOptions } from '../lib/launch'
import { autopilotState, nextStep } from '../lib/nextStep'
import { nudgeDue, nudgeWords } from '../lib/nudge'
import { leavingWords, useRowWork } from '../lib/rowWork'
import { cardSession, endedShownMs, sinceWhen } from '../lib/sessions'
import { formatAge, formatAgo } from '../lib/time'
import { isBackFromSnooze, isPinned, type TriageTab } from '../lib/triage'
import { useNowAs } from '../lib/useNow'
import type { AgentInfo, AgentSession, InboxItem, ItemResolution, Label, Worktree } from '../platform'
import { AutopilotChip } from './autopilot/AutopilotChip'
import { CardActions, cardOutcomeClass } from './card/CardActions'
import { CardAvatar } from './card/CardAvatar'
import { LinkChip } from './card/CardLinks'
import { MergeBotChip, mergeBotOf } from './card/MergeBotChip'
import { CompactRow } from './card/CompactRow'
import { edgeBar, FilterText, PinGlyph, tagClass, TriageTag, UnreadDot } from './card/CardParts'
import { CardPeek } from './card/CardPeek'
import { CardStory } from './card/CardStory'
import { IssueState } from './card/IssueState'
import { RiskChip, trustTitle } from './card/RiskChip'
import { SizeGlyph } from './card/SizeGlyph'
import { StateTrack } from './card/StateTrack'
import { ghostButtonClass, inkLinkClass } from './controls'
import { Highlight } from './Highlight'
import { ChevronDownIcon, ExternalIcon, WorktreeIcon } from './icons'
import { ItemMenu, type EditActions } from './ItemMenu'
import { LabelChip } from './ItemSignals'
import { LaunchControls } from './LaunchControls'
import { RelativeTime } from './RelativeTime'
import { Spinner } from './Spinner'
import { hoverClass, mutedTarget, openOnGitHub, TriageControls, type TriageActions } from './TriageControls'

export interface ItemRowProps {
  item: InboxItem
  // launch is null until the launch settings have loaded; the row has no agent slot until
  // then.
  launch: LaunchOptions | null
  // resolution is the item's resolved launch settings, or null while they load.
  resolution?: ItemResolution | null
  // worktree is the item's worktree, as the Worktrees view last listed it, if it has one.
  worktree?: Worktree | undefined
  // sessions are the item's agent sessions, newest first, if it has any.
  sessions?: readonly AgentSession[] | undefined
  // tab is the list tab the row is in: the inbox, Snoozed, Dismissed, or Muted.
  tab?: TriageTab
  // triage is what the row's snooze, dismiss, and restore buttons do; without it the row
  // has none.
  triage?: TriageActions
  // edits is what the row's menu does beyond triage: copy the link and the edits on
  // GitHub. The menu shows with the triage buttons, when the row has both.
  edits?: EditActions | undefined
  // onFilesSettings opens Settings → Sensitive files from the sensitive-file panel.
  onFilesSettings?: (() => void) | undefined
  // highlighted marks the row the menu bar or a notification just asked to show.
  highlighted?: boolean
  // marks is what the inbox's filter looks for, which the title highlights; a plain
  // string, so the row renders again only when it changes.
  marks?: string
  // onFilterBy adds a filter to the inbox's query, or takes it away: the row's
  // repository, author, and labels offer it when clicked. Without it they are plain text.
  onFilterBy?: ((key: string, value: string) => void) | undefined
  // onOpen opens the item in its sheet, from the row's tab: the review sheet for a pull
  // request, the issue sheet for an issue. Its title does, unless ⌘ is held, and a mark
  // beside it opens GitHub. Without it the title opens GitHub.
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
  // autopilot is where a routine stands on the item, as its list reads it from the rows'
  // Autopilot (rowAutopilot): "needs:<routine>", "working:<routine>", or ''. A plain
  // string, so an Autopilot update renders only the rows it changes; the row never reads
  // the rows' Autopilot itself.
  autopilot?: string
}

// TrustTag marks an item by who opened it: a trusted bot quietly, and an untrusted bot or
// an outside contributor as a warning, since their items launch in plan mode. Items from
// the repository's own people carry no tag.
export function TrustTag({ item }: { item: InboxItem }) {
  if (item.trust === 'bot') {
    return (
      <span
        title="A trusted bot: launches use the profile's permissions. Manage trusted bots in Settings → Trust."
        className={`${tagClass} text-muted`}
      >
        bot
      </span>
    )
  }

  if (item.trusted) {
    return null
  }

  return (
    <span title={trustTitle(item)} className={`${tagClass} text-warning`}>
      {item.authorIsBot ? 'untrusted bot' : 'outside contributor'}
    </span>
  )
}

// WorktreeTag marks an item that has a worktree, as the Worktrees view last listed it: its
// uncommitted files, and the agent and age of its latest session, as in "worktree · 2
// changes · Codex 3h ago".
export function WorktreeTag({ wt, agents }: { wt: Worktree; agents: readonly AgentInfo[] }) {
  const parts = ['worktree']

  if (wt.dirtyCount > 0) {
    parts.push(counted(wt.dirtyCount, 'change', 'changes'))
  }

  const busy = wt.inUse.length > 0
  const last = !busy && wt.agent !== '' && wt.lastLaunchedAt !== ''

  return (
    <span title={wt.displayPath} className={`${tagClass} shrink-0 text-muted`}>
      {parts.join(' · ')}
      {busy && ' · in use'}
      {last && (
        <>
          {` · ${agentName(agents, wt.agent)} `}
          <RelativeTime iso={wt.lastLaunchedAt} show={formatAgo} />
        </>
      )}
    </span>
  )
}

// WorktreeChip marks a card whose worktree, as the Worktrees view last listed it, has
// uncommitted changes, or has a session running in it; its title is the worktree's path.
function WorktreeChip({ wt }: { wt: Worktree }) {
  const words = wt.dirtyCount > 0 ? counted(wt.dirtyCount, 'change', 'changes') : 'in use'

  return (
    <span
      title={wt.displayPath}
      className="inline-flex shrink-0 items-center gap-1 rounded-full border border-border px-1.5 py-px text-caption leading-4 font-medium text-muted"
    >
      <WorktreeIcon className="size-3 shrink-0" />
      <span className="@max-3xl:sr-only">{words}</span>
    </span>
  )
}

// CardChip is one of a card's chips after its risk chip: words name it in the count the
// chips past the second fold into.
export interface CardChip {
  key: string
  words: string
  node: ReactNode
}

// shownChips is how many chips a card shows after its risk chip, before folding the rest
// into "+N".
const shownChips = 2

// CardChips are a card's chips after its risk chip: the first two, then a quiet "+N"
// whose title names the rest.
export function CardChips({ chips }: { chips: readonly CardChip[] }) {
  const rest = chips.slice(shownChips)
  const names = rest.map((chip) => chip.words).join(', ')

  return (
    <>
      {chips.slice(0, shownChips).map((chip) => (
        <Fragment key={chip.key}>{chip.node}</Fragment>
      ))}
      {rest.length > 0 && (
        <span title={names} className="shrink-0 text-caption font-medium">
          +{rest.length}
          <span className="sr-only"> more: {names}</span>
        </span>
      )}
    </>
  )
}

// cardLabels is how many labels a card shows after its title before folding the rest
// into "+N".
const cardLabels = 2

// CardLabels are the first labels of a card after its title, with their GitHub colors,
// and the count of the rest, named in its tooltip. The labels the user hid (Settings →
// Cards) are left out before the first are taken, so a hidden one never takes a visible
// one's place or counts in the +N. A label shows whole or folds into the +N, never cut
// off: they take the room the title leaves, or a third of the line from a long title,
// which is cut short for them, and the first always shows (labelsShown). A list narrower
// than 56rem leaves them out.
//
// A label that folds stays laid out at its whole width, out of the line's flow, unseen,
// and out of reach of the keyboard and a screen reader, and so does the +N when none
// fold, as a "+1" drawn by CSS rather than written in the card's text: the line measures
// them as they are wherever they would be (useLabelRoom).
function CardLabels({
  labels: all,
  onFilterBy,
}: {
  labels: readonly Label[]
  onFilterBy: ((key: string, value: string) => void) | undefined
}) {
  const hidden = use(HiddenLabelsContext)
  const [fit, room] = useLabelRoom()
  const labels = all.filter((label) => !hidden(label.name))

  if (labels.length === 0) {
    return null
  }

  const shown = labelsShown(room, labels.length, cardLabels)
  const rest = labels.slice(shown)
  const names = rest.map((label) => label.name).join(', ')

  return (
    <span ref={fit} className="relative z-10 flex shrink-0 items-center gap-1 @max-4xl:hidden">
      <span className="sr-only">Labels:</span>
      {labels.slice(0, cardLabels).map((label, i) => (
        <span
          key={label.name}
          data-label-chip
          inert={i >= shown}
          className={i < shown ? 'flex shrink-0' : 'invisible absolute flex w-max'}
        >
          <LabelChip label={label} onFilterBy={onFilterBy} />
        </span>
      ))}
      {rest.length > 0 ? (
        <span data-label-count title={names} className="shrink-0 text-caption text-muted tabular-nums">
          +{rest.length}
          <span className="sr-only"> more: {names}</span>
        </span>
      ) : (
        <span
          data-label-count
          aria-hidden="true"
          className="invisible absolute w-max text-caption tabular-nums before:content-['+1']"
        />
      )}
    </span>
  )
}

// teamsText names the teams a pull request asks to review, in the byline: "@platform".
function teamsText(teams: readonly string[]): string {
  return teams.length === 0 ? 'a team' : teams.map((team) => `@${team}`).join(', ')
}

// Byline is where a card's item is and who opened it, after its story: the repository
// (its org dimmed, left out in a list narrower than 36rem) and number, the author ("you"
// for the user's own), why it is the user's ("via @team" for a review asked of a team,
// "assigned"), and how long ago it last moved. It truncates before the story does.
function Byline({
  item,
  onFilterBy,
}: {
  item: InboxItem
  onFilterBy: ((key: string, value: string) => void) | undefined
}) {
  const slash = item.repo.indexOf('/')
  const org = item.repo.slice(0, slash + 1)
  const name = item.repo.slice(slash + 1)
  const reasons = [
    item.reviewRequest === 'team' ? `via ${teamsText(item.reviewTeams)}` : '',
    item.assigned ? 'assigned' : '',
  ].filter((reason) => reason !== '')

  return (
    <span data-guide="byline" className="relative z-10 min-w-0 truncate">
      <span aria-hidden="true" className="mr-2 opacity-60">
        ·
      </span>
      <FilterText filterKey="repo" value={item.repo} name="repository" onFilterBy={onFilterBy}>
        <span className="opacity-60 @max-xl:hidden">{org}</span>
        {name}
      </FilterText>
      {` #${String(item.number)} · `}
      <FilterText
        filterKey="author"
        value={item.author}
        name="author"
        onFilterBy={onFilterBy}
        full={item.authored ? `${item.author} (you)` : item.author}
      >
        {item.authored ? <span className="font-semibold text-text">you</span> : item.author}
      </FilterText>
      {reasons.map((reason) => ` · ${reason}`).join('')}
      {' · '}
      <RelativeTime iso={item.updatedAt} show={formatAge} />
    </span>
  )
}

// ItemRow is one inbox card, a Signal card two lines high. Down its left side, the edge's
// color says whose move it is (nextStep); then the author's avatar with the item's kind
// (CardAvatar). Its first line is the title, after the unread dot while the item has
// activity the user hasn't seen (and Settings → Cards' Unread dots is on) and a pin when
// pinned, with up to two labels; its second, the story in the edge's color (CardStory: one sentence of why it is
// here, or, in the Snoozed and Dismissed tabs, when it comes back or was dismissed, then,
// muted, where it stands), the byline, then the risk chip, the link to the item GitHub links it to (LinkChip: an
// issue's fix, or the issue a pull request closes), the auto-merge chip (MergeBotChip: a listed bot approved the
// pull request, so it merges itself once its checks pass), the ⚡ chip (AutopilotChip), and the worktree chip. The state
// column shows where a pull request stands (StateTrack, over its size, SizeGlyph) or an
// issue (IssueState); the action column, the human's next move and the agent's slot beside
// it (CardActions, with LaunchControls' card variant), each in a slot of its own: the
// primary's as wide as the longest common primary (--card-primary: 5.625rem fits
// Re-review; a longer one, which comes only beside the agent's ▾, widens it), the agent's
// the rest of the column (--card-actions), so the primaries start, and the agent's ▾
// ends, at the same place on every card. At full width the agent's slot (14.75rem) fits its
// common pills whole, such as "Claude working · 59m" with its terminal button. The pointer or keyboard focus on
// the card, or a menu or panel it opened, lights it (card-lit in index.css): the state
// column fades for the triage buttons, Show details, and the menu of everything else
// (ItemMenu), which stay while the card is busy. A click on the card's empty space, or
// on Show details, opens the peek under its two lines (CardPeek), and a launch's outcome
// spans the card below. The card does one thing with GitHub at a time, which its buttons
// and its menu share (useRowWork). While an edit that will likely take it out of the list
// runs, the card fades and blurs under a note saying what GitHub is doing, and takes no
// clicks, so the wait shows however slow the network is; it comes back if the item stays.
// An issue GitHub is closing because its fix just merged (item.closing) does the same,
// under Fix merged · closing on GitHub…, until the inbox reads it closed (leavingWords).
// Long titles are cut short with an ellipsis, so every card keeps the same shape, and the
// card narrows with its list (@container on the list). A card always renders, on screen
// or off (item-row in index.css says why). With Settings → Cards' Density compact
// (CardPrefsContext), the card is CompactRow instead: one line from the same step, with
// the same toggle, peek, agent slot, and busy note.
//
// The card reads the clock only for plain values (its session, whether a failed session
// is fresh, whether it is back from a snooze, and whether a nudge of a quiet author is due,
// with what its story says of the wait), so a tick renders it only when one changes, and never reads the rows' Autopilot itself: its list hands it the plain
// autopilot string. The row is wrapped in memo: the inbox renders its rows in a loop,
// where the React Compiler can't keep each row's element from one render to the next, so
// without memo a change to one item would render every row in its list again.
export const ItemRow = memo(function ItemRow({
  item,
  launch,
  resolution = null,
  worktree,
  sessions,
  tab = 'inbox',
  triage,
  edits,
  onFilesSettings,
  highlighted = false,
  marks = '',
  onFilterBy,
  onOpen,
  autopilot = '',
}: ItemRowProps) {
  const opens = onOpen !== undefined
  const [work, start] = useRowWork()
  const going = leavingWords(work, item)
  const peekId = useId()
  const [peeking, setPeeking] = useState(false)
  const sessionId = useNowAs((now) => cardSession(sessions, now)?.id ?? null)
  const sessionFresh = useNowAs((now) => {
    const s = cardSession(sessions, now)

    return s?.state === 'failed' && now.getTime() - Date.parse(sinceWhen(s)) < endedShownMs
  })
  const backFromSnooze = useNowAs((now) => item.snoozedUntil !== '' && isBackFromSnooze(item, now))
  const prefs = use(CardPrefsContext)
  // A nudge is due only where the card can post one (its edits' onNudge), and never while
  // Settings → Reviewing's Offer a nudge after is Off.
  const onNudge = edits?.onNudge
  const nudgeAfter = onNudge === undefined ? 0 : prefs.nudgeAfterHours
  const nudgeIsDue = useNowAs((now) => nudgeDue(item, now, nudgeAfter))
  const nudgeSaid = useNowAs((now) => nudgeWords(item, now, nudgeAfter))
  const unread = prefs.unread && item.unread
  const session = sessions?.find((s) => s.id === sessionId) ?? null
  const agents = launch?.agents ?? []
  const routine = autopilotState(autopilot)
  const step = nextStep({
    item,
    tab,
    session,
    sessionFresh,
    backFromSnooze,
    nudge: { due: nudgeIsDue, words: nudgeSaid },
    autopilot: routine,
    agent: session === null ? (resolution?.agent.name ?? '') : agentName(agents, session.agent),
    mutedTarget: tab !== 'muted' ? '' : triage === undefined ? item.repo : mutedTarget(item, triage.repoRules),
  })
  // The story is the routine's while it waits on the user, or works with no agent session
  // at work, so the ⚡ chip would only say it again.
  const sessionWorking = session?.state === 'starting' || session?.state === 'working'
  const routineStory = step.rule === 'autopilot_needs_you' || (step.rule === 'agent_working' && !sessionWorking)
  const chips: CardChip[] = []
  const mergeBot = mergeBotOf(item)

  // A listed auto-merge bot's approval comes first: it says the pull request merges itself.
  if (mergeBot !== '') {
    chips.push({ key: 'mergebot', words: 'Auto-merge', node: <MergeBotChip bot={mergeBot} /> })
  }

  if (!routineStory) {
    chips.push({ key: 'autopilot', words: 'Autopilot', node: <AutopilotChip item={item} /> })
  }

  if (worktree !== undefined && (worktree.dirtyCount > 0 || worktree.inUse.length > 0)) {
    chips.push({ key: 'worktree', words: worktree.displayPath, node: <WorktreeChip wt={worktree} /> })
  }

  const togglePeek = () => {
    setPeeking(!peeking)
  }

  const actions = (agent?: ReactNode) => (
    <CardActions
      item={item}
      tab={tab}
      step={step}
      session={session}
      sessions={sessions ?? []}
      agents={agents}
      triage={triage}
      onOpen={onOpen}
      onNudge={onNudge}
      agent={agent}
    />
  )

  const peek = (launchButton: ReactNode) => (
    <CardPeek
      id={peekId}
      item={item}
      tab={tab}
      step={step}
      session={session}
      sessions={sessions ?? []}
      agents={agents}
      triage={triage}
      onOpen={onOpen}
      onNudge={onNudge}
      onFilterBy={onFilterBy}
      launch={launchButton}
    />
  )

  // slot renders the agent's slot, LaunchControls' card variant, framed by column (the
  // card's action column, or a compact card's hover row and go button), its outcome placed
  // by outcomeClass, then the peek while it is open. Until the launch options load, column
  // frames no slot.
  const slot = (column: (agent?: ReactNode) => ReactNode, outcomeClass: string) =>
    launch === null ? (
      <>
        {column()}
        {peeking && peek(null)}
      </>
    ) : (
      <LaunchControls
        variant="card"
        item={item}
        resolution={resolution}
        {...launch}
        running={session}
        sessionFresh={sessionFresh}
        sessions={sessions}
        situation={step.situation}
        outcomeClass={outcomeClass}
        onOpen={
          onOpen === undefined
            ? undefined
            : () => {
                onOpen(item, tab)
              }
        }
        column={column}
        peek={peeking ? peek : undefined}
      />
    )

  if (prefs.density === 'compact') {
    return (
      <CompactRow
        item={item}
        tab={tab}
        step={step}
        session={session}
        sessions={sessions ?? []}
        agents={agents}
        triage={triage}
        edits={edits}
        highlighted={highlighted}
        unread={unread}
        marks={marks}
        onFilterBy={onFilterBy}
        onOpen={onOpen}
        work={work}
        start={start}
        peekId={peekId}
        peeking={peeking}
        onTogglePeek={togglePeek}
        slot={slot}
      />
    )
  }

  return (
    <li
      data-item-id={item.id}
      data-highlighted={highlighted || undefined}
      data-rule={step.rule}
      aria-busy={going !== '' || undefined}
      className="item-row group relative grid grid-cols-[0.1875rem_1.875rem_minmax(0,1fr)_var(--card-state)_var(--card-actions)] items-center gap-x-3.5 border-b border-border py-3 pr-4 transition-colors [--card-actions:20.75rem] [--card-primary:5.625rem] [--card-state:8.25rem] last:border-b-0 hover:bg-raised/40 data-highlighted:bg-accent/10 @max-5xl:[--card-actions:16.875rem] @max-4xl:[--card-state:4.75rem] @max-3xl:[--card-actions:10.75rem] @max-3xl:[--card-primary:auto] @max-xl:grid-cols-[0.1875rem_1.875rem_minmax(0,1fr)_var(--card-actions)]"
    >
      <span
        aria-hidden="true"
        data-edge={step.edge}
        data-guide="edge"
        className={`-my-0.5 self-stretch rounded-r-[0.1875rem] ${edgeBar[step.edge]}`}
      />
      <CardAvatar item={item} />
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          {unread && <UnreadDot />}
          {isPinned(item) && <PinGlyph item={item} rules={triage?.repoRules} />}
          {item.isDraft && <span className="shrink-0 text-xs font-semibold text-muted">Draft</span>}
          <button
            type="button"
            data-guide="title"
            data-card-title
            title={
              opens
                ? `${item.title}\n${item.kind === 'pr' ? 'Review' : 'Read'} it here; ⌘-click opens GitHub`
                : item.title
            }
            onClick={(e) => {
              if (opens && !e.metaKey) {
                onOpen(item, tab)
              } else {
                openOnGitHub(item, triage)
              }
            }}
            className={`${inkLinkClass} relative z-10 min-w-[8ch] truncate text-left text-sm font-semibold`}
          >
            <Highlight text={item.title} marks={marks} />
          </button>
          {/* The stretched toggle: a click on the card's empty space opens its peek. Every
              control, and everything with a title, sits above it (relative z-10). Keyboard
              users reach Show details instead, which keyboard focus in the card reveals. */}
          <button
            type="button"
            tabIndex={-1}
            aria-label="Show details"
            aria-expanded={peeking}
            aria-controls={peeking ? peekId : undefined}
            onClick={togglePeek}
            className="absolute inset-0 z-0"
          />
          {opens && (
            <button
              type="button"
              aria-label="Open on GitHub"
              title="Open on GitHub"
              onClick={() => {
                openOnGitHub(item, triage)
              }}
              className="relative z-10 shrink-0 rounded-sm text-muted/70 opacity-0 transition-opacity group-hover:opacity-100 hover:text-accent focus-visible:opacity-100"
            >
              <ExternalIcon className="size-3" />
            </button>
          )}
          {item.details !== null && <CardLabels labels={item.details.labels} onFilterBy={onFilterBy} />}
        </div>
        {/* The second line clips at the card's edge, but not while a chip's panel is open
            or keyboard focus is in it, so the panel and the focus ring show whole. */}
        <div className="mt-1 flex min-w-0 items-center gap-2 overflow-hidden text-xs whitespace-nowrap text-muted has-[:focus-visible]:overflow-visible has-[[aria-expanded=true]]:overflow-visible">
          <CardStory story={step.story} edge={step.edge}>
            <TriageTag item={item} tab={tab} />
          </CardStory>
          <Byline item={item} onFilterBy={onFilterBy} />
          <span className="relative z-10 inline-flex shrink-0 items-center gap-1.5 empty:hidden has-[[aria-expanded=true]]:z-30">
            <RiskChip item={item} onFilesSettings={onFilesSettings} />
            <LinkChip item={item} />
            <CardChips chips={chips} />
          </span>
        </div>
      </div>
      <div
        data-state-column
        className={`flex flex-col items-end justify-center gap-1.25 transition-opacity @max-xl:hidden card-lit:opacity-0 ${work === null ? '' : 'opacity-0'}`}
      >
        {item.kind === 'pr' ? (
          <>
            <StateTrack item={item} />
            <div className="@max-4xl:hidden">
              <SizeGlyph item={item} />
            </div>
          </>
        ) : (
          <IssueState item={item} />
        )}
      </div>
      {/* The triage, over the state column's place in the card's first row (over the end of
          the body once a narrow list drops the column), on a backing tinted like the hovered
          card, the row's full height so it covers the end of both lines. Positioned
          absolutely, it takes no cell of its own: its grid area, both lines named (an auto
          line would reach the card's edge), is only what it is placed in. */}
      <div
        data-triage
        className={`${hoverClass(work)} absolute inset-y-0 right-0 z-20 col-[4/5] row-[1/2] w-max rounded-lg wash-raised p-0.5 @max-xl:col-[3/4]`}
      >
        <button
          type="button"
          aria-label="Show details"
          title="Show details"
          aria-expanded={peeking}
          aria-controls={peeking ? peekId : undefined}
          onClick={togglePeek}
          className={ghostButtonClass}
        >
          <ChevronDownIcon className={`size-3.5 transition-transform ${peeking ? 'rotate-180' : ''}`} />
        </button>
        {triage !== undefined && (
          <TriageControls item={item} tab={tab} actions={triage} work={work} start={start} mode="card" />
        )}
        {triage !== undefined && edits !== undefined && (
          <ItemMenu item={item} tab={tab} triage={triage} edits={edits} work={work} start={start} />
        )}
      </div>
      {slot(actions, cardOutcomeClass)}
      {going !== '' && (
        <div data-row-status className="absolute inset-0 z-30 flex items-center justify-center">
          <p
            role="status"
            className="flex items-center gap-2 rounded-full border border-border bg-surface px-3 py-1 text-xs font-medium text-text shadow-float motion-safe:animate-pop-in"
          >
            <Spinner className="size-3.5" />
            {going}
          </p>
        </div>
      )}
    </li>
  )
})
