import { use, type ReactNode } from 'react'

import { HiddenLabelsContext } from '../../lib/labels'
import type { Step } from '../../lib/nextStep'
import { leaving, workWords, type RowWork, type StartWork } from '../../lib/rowWork'
import { formatAge } from '../../lib/time'
import { isPinned, type TriageTab } from '../../lib/triage'
import type { AgentInfo, AgentSession, InboxItem, Label } from '../../platform'
import { inkLinkClass } from '../controls'
import { Highlight } from '../Highlight'
import { ChevronDownIcon } from '../icons'
import { ItemMenu, type EditActions } from '../ItemMenu'
import { RelativeTime } from '../RelativeTime'
import { Spinner } from '../Spinner'
import { ghostClass, hoverClass, openOnGitHub, TriageControls, type TriageActions } from '../TriageControls'
import { CardActions, compactOutcomeClass } from './CardActions'
import { KindMark } from './CardAvatar'
import { LinkChip } from './CardLinks'
import { edgeBar, FilterText, PinGlyph, TriageTag, UnreadDot } from './CardParts'
import { CardStory } from './CardStory'
import { IssueGrade, IssueTrack } from './IssueState'
import { RiskIcon } from './RiskChip'
import { SizeGrade } from './SizeGlyph'
import { MiniTrack } from './StateTrack'

// labelDots is how many labels a compact card shows as dots after its title.
const labelDots = 6

// LabelDots are a compact card's labels as dots in their GitHub colors, up to six, every
// one's name in their tooltip and for a screen reader. The labels the user hid (Settings →
// Cards) are left out, as on a comfortable card.
function LabelDots({ labels: all }: { labels: readonly Label[] }) {
  const hidden = use(HiddenLabelsContext)
  const labels = all.filter((label) => !hidden(label.name))

  if (labels.length === 0) {
    return null
  }

  const names = labels.map((label) => label.name).join(', ')

  return (
    <span
      role="img"
      aria-label={`Labels: ${names}`}
      title={names}
      className="relative z-10 inline-flex shrink-0 items-center gap-0.5"
    >
      {labels.slice(0, labelDots).map((label) => (
        <span
          key={label.name}
          aria-hidden="true"
          data-label-dot
          className={`block size-1.75 rounded-full ring-[1.5px] ring-surface ${label.color === '' ? 'bg-muted' : ''}`}
          style={label.color === '' ? undefined : { backgroundColor: label.color }}
        />
      ))}
    </span>
  )
}

// Repo is a compact card's repository, its org dimmed, which filters the inbox by it.
function Repo({
  item,
  onFilterBy,
}: {
  item: InboxItem
  onFilterBy: ((key: string, value: string) => void) | undefined
}) {
  const slash = item.repo.indexOf('/')

  return (
    <span data-repo className="relative z-10 min-w-0 truncate text-xs text-muted @max-4xl:hidden">
      <FilterText filterKey="repo" value={item.repo} name="repository" onFilterBy={onFilterBy}>
        <span className="opacity-60">{item.repo.slice(0, slash + 1)}</span>
        {item.repo.slice(slash + 1)}
      </FilterText>
    </span>
  )
}

export interface CompactRowProps {
  item: InboxItem
  tab: TriageTab
  // step is the card's next step (nextStep), as ItemRow works it out for both densities.
  step: Step
  // session is the card's session (cardSession), and sessions all the item's, newest first.
  session: AgentSession | null
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  triage: TriageActions | undefined
  edits: EditActions | undefined
  highlighted: boolean
  // unread shows the unread dot: the item has activity the user hasn't seen, and Settings
  // → Cards' Unread dots is on.
  unread: boolean
  marks: string
  onFilterBy: ((key: string, value: string) => void) | undefined
  onOpen: ((item: InboxItem, tab: TriageTab) => void) | undefined
  // work is what the card is doing with GitHub, and start begins more (useRowWork).
  work: RowWork
  start: StartWork
  // peekId is the peek's ID, peeking whether it is open, and onTogglePeek opens or closes it.
  peekId: string
  peeking: boolean
  onTogglePeek: () => void
  // slot renders the card's agent slot (LaunchControls' card variant, or none while the
  // launch options load) framed by column, its outcome placed by outcomeClass, and the peek
  // while it is open.
  slot: (column: (agent?: ReactNode) => ReactNode, outcomeClass: string) => ReactNode
}

// CompactRow is an inbox card on one line, for Settings → Cards' Compact density: the same
// next step (nextStep) as the comfortable card, a column per question, so a list reads down
// each column. After the edge, the item's kind (KindMark) and its repository; the title,
// its number, its labels as dots (LabelDots), its pin, and its unread dot; the story as a chip tinted with
// the edge's color (CardStory); the risk as its shield alone (RiskIcon); where it stands as
// three dots (MiniTrack), or an issue's sub-issues or status (IssueTrack); its size grade,
// or an issue's first featured field; its age; and its go button, the primary's words in
// its tone (CardActions' compact go). While the card is lit (card-lit in index.css: the
// pointer on it, keyboard focus in it, or a menu or panel it opened open), its hover row
// shows just left of the go button: the triage, the agent's slot (LaunchControls' card
// variant, kept mounted under opacity so its dialogs keep their state), Show details, and
// the menu.
// The stretched toggle, the peek, a launch's outcome, and the busy note are the
// comfortable card's. A list narrower than 56rem leaves out the repository and the size,
// and one narrower than 48rem the story and the age.
export function CompactRow({
  item,
  tab,
  step,
  session,
  sessions,
  agents,
  triage,
  edits,
  highlighted,
  unread,
  marks,
  onFilterBy,
  onOpen,
  work,
  start,
  peekId,
  peeking,
  onTogglePeek,
  slot,
}: CompactRowProps) {
  const opens = onOpen !== undefined
  const going = leaving(work)
  const isPR = item.kind === 'pr'

  // hoverRow is the card's hover row, holding the agent's slot when there is one.
  const hoverRow = (agent?: ReactNode) => (
    <div
      data-hover-row
      className={`${hoverClass(work)} absolute inset-y-1 right-0 z-20 col-[1/-2] row-[1/2] w-max rounded-lg bg-surface p-0.5 shadow-float ring-1 ring-border`}
    >
      {triage !== undefined && (
        <TriageControls item={item} tab={tab} actions={triage} work={work} start={start} mode="card" />
      )}
      {agent}
      <button
        type="button"
        aria-label="Show details"
        title="Show details"
        aria-expanded={peeking}
        aria-controls={peeking ? peekId : undefined}
        onClick={onTogglePeek}
        className={ghostClass}
      >
        <ChevronDownIcon className={`size-3.5 transition-transform ${peeking ? 'rotate-180' : ''}`} />
      </button>
      {triage !== undefined && edits !== undefined && (
        <ItemMenu item={item} tab={tab} triage={triage} edits={edits} work={work} start={start} />
      )}
    </div>
  )

  // column is the hover row, then the go button in the card's last column.
  const column = (agent?: ReactNode) => (
    <>
      {hoverRow(agent)}
      <CardActions
        item={item}
        tab={tab}
        step={step}
        session={session}
        sessions={sessions}
        agents={agents}
        triage={triage}
        onOpen={onOpen}
        onNudge={edits?.onNudge}
        compact
      />
    </>
  )

  return (
    <li
      data-item-id={item.id}
      data-highlighted={highlighted || undefined}
      data-rule={step.rule}
      data-density="compact"
      aria-busy={going || undefined}
      className="item-row group relative grid grid-cols-[0.1875rem_1rem_9.375rem_minmax(0,1fr)_auto_1.125rem_3.125rem_1.625rem_2rem_7.25rem] grid-rows-[2.375rem] items-center gap-x-3 border-b border-border pr-3 transition-colors last:border-b-0 hover:bg-raised/40 has-[>section]:pb-3 data-highlighted:bg-accent/10 @max-4xl:grid-cols-[0.1875rem_1rem_minmax(0,1fr)_auto_1.125rem_3.125rem_2rem_7.25rem] @max-3xl:grid-cols-[0.1875rem_1rem_minmax(0,1fr)_1.125rem_3.125rem_7.25rem]"
    >
      <span
        aria-hidden="true"
        data-edge={step.edge}
        className={`h-5.5 self-center rounded-r-[0.1875rem] ${edgeBar[step.edge]}`}
      />
      <KindMark item={item} />
      <Repo item={item} onFilterBy={onFilterBy} />
      <div className="flex min-w-0 items-center gap-2">
        <button
          type="button"
          data-card-title
          title={opens ? `${item.title}\n${isPR ? 'Review' : 'Read'} it here; ⌘-click opens GitHub` : item.title}
          onClick={(e) => {
            if (opens && !e.metaKey) {
              onOpen(item, tab)
            } else {
              openOnGitHub(item, triage)
            }
          }}
          className={`${inkLinkClass} relative z-10 min-w-[6ch] truncate text-left text-body font-medium`}
        >
          <Highlight text={item.title} marks={marks} />
        </button>
        {/* The stretched toggle, as on a comfortable card: a click on the card's empty space
            opens its peek, and every control and everything with a title sits above it. */}
        <button
          type="button"
          tabIndex={-1}
          aria-label="Show details"
          aria-expanded={peeking}
          aria-controls={peeking ? peekId : undefined}
          onClick={onTogglePeek}
          className="absolute inset-0 z-0"
        />
        <span className="shrink-0 text-xs text-muted tabular-nums">#{item.number}</span>
        <LinkChip item={item} compact />
        {item.details !== null && <LabelDots labels={item.details.labels} />}
        {isPinned(item) && <PinGlyph item={item} rules={triage?.repoRules} />}
        {unread && <UnreadDot />}
      </div>
      <div className="flex min-w-0 @max-3xl:hidden">
        <CardStory story={step.story} edge={step.edge} chip>
          <TriageTag item={item} tab={tab} />
        </CardStory>
      </div>
      <div className="grid place-items-center">
        <RiskIcon item={item} />
      </div>
      <div className="flex justify-end">{isPR ? <MiniTrack item={item} /> : <IssueTrack item={item} />}</div>
      <div className="min-w-0 @max-4xl:hidden">{isPR ? <SizeGrade item={item} /> : <IssueGrade item={item} />}</div>
      <span className="text-right text-xs text-muted tabular-nums @max-3xl:hidden">
        <RelativeTime iso={item.updatedAt} show={formatAge} />
      </span>
      {slot(column, compactOutcomeClass)}
      {going && (
        <div data-row-status className="absolute inset-0 z-30 flex items-center justify-center">
          <p
            role="status"
            className="flex items-center gap-2 rounded-full border border-border bg-surface px-3 py-1 text-xs font-medium text-text shadow-float motion-safe:animate-pop-in"
          >
            <Spinner className="size-3.5" />
            {workWords[work]}
          </p>
        </div>
      )}
    </li>
  )
}
