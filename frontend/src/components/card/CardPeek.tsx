import { use, useState, type ReactNode } from 'react'

import { chipOf } from '../../lib/autopilot'
import { CardPrefsContext } from '../../lib/cardPrefs'
import { counted, formatCount } from '../../lib/format'
import { agentName } from '../../lib/launch'
import type { Step, Verb } from '../../lib/nextStep'
import { sensitiveCount } from '../../lib/sensitive'
import { setupWords, sinceWhen, stateLabel, stateText } from '../../lib/sessions'
import { checksSignal, sizeOf } from '../../lib/signals'
import { capitalize, formatAge } from '../../lib/time'
import { isPinnedItself, isPinned, snoozeLength, type TriageTab } from '../../lib/triage'
import { openExternal, type AgentInfo, type AgentSession, type InboxItem, type ItemDetails } from '../../platform'
import { AutopilotRowsContext, useMarkedBy, useRoutineName } from '../autopilot/AutopilotChip'
import { ArchiveIcon, AutopilotIcon, ClockIcon, DotIcon, ExternalIcon, PinIcon, RingIcon } from '../icons'
import { LabelChip, ProgressBar, ProjectLink } from '../ItemSignals'
import { RelativeTime } from '../RelativeTime'
import { SensitiveFilesGroup } from '../SensitiveFiles'
import { openOnGitHub, type TriageActions } from '../TriageControls'
import { PrimaryVerb } from './CardActions'
import { mergeStepText, reviewStepText } from './StateTrack'

// buttonClass is each button of the peek's action bar but its primary.
const buttonClass =
  'inline-flex h-7 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-text transition-colors hover:bg-raised'

// Segment is one stretch of a bar, as wide as its share of the bar's total.
interface Segment {
  name: string
  value: number
  color: string
}

// SegmentBar is a thin bar split into segments in proportion: the checks by how they
// ended, or a change's lines added against removed. It is decoration: the words beside it
// say the numbers.
function SegmentBar({ segments }: { segments: readonly Segment[] }) {
  return (
    <span aria-hidden="true" className="my-1.25 flex h-1.5 gap-0.5 overflow-hidden rounded-full">
      {segments
        .filter((s) => s.value > 0)
        .map((s) => (
          <span
            key={s.name}
            data-segment={s.name}
            className={`block h-full ${s.color}`}
            style={{ flexGrow: s.value }}
          />
        ))}
    </span>
  )
}

// Fact is one fact of an area: what it is, muted, over what it says.
function Fact({ term, children }: { term: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[0.71875rem] text-muted">{term}</dt>
      <dd className="min-w-0 text-text">{children}</dd>
    </div>
  )
}

// Area is one of the peek's columns, under its heading. An area with nothing to say is
// left out by its caller.
function Area({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <section aria-label={heading} className="min-w-0">
      <h3 className="mb-2 text-[0.6875rem] font-semibold tracking-wider text-muted uppercase">{heading}</h3>
      <dl className="flex flex-col gap-3">{children}</dl>
    </section>
  )
}

// reviewMore is what a pull request's review fact adds after the review step's sentence:
// how many reviewers have reviewed, and that an approval is required, once the author
// pushed since the user's review. The reviews signal's own sentence already says both.
function reviewMore(d: ItemDetails): string {
  if (!d.pushedSinceReview) {
    return ''
  }

  const { reviewers, reviewed, decision } = d.reviews

  return [
    reviewers === 0
      ? 'no reviewers yet'
      : `${formatCount(reviewed)} of ${counted(reviewers, 'reviewer', 'reviewers')} reviewed`,
    decision === 'REVIEW_REQUIRED' ? 'approval required' : '',
  ]
    .filter((part) => part !== '')
    .join(' · ')
}

// StandsPR is where a pull request stands: its checks, as a bar of how they ended and the
// checks signal's sentence, its review, and whether it can merge, each the track's
// sentence with the numbers the track leaves out.
function StandsPR({ d }: { d: ItemDetails }) {
  const { passed, failed, awaiting, awaitingOnGitHub, pending, skipped } = d.checks
  const more = reviewMore(d)

  return (
    <Area heading="Where it stands">
      <Fact term="Checks">
        <SegmentBar
          segments={[
            { name: 'passed', value: passed, color: 'bg-success' },
            { name: 'failed', value: failed, color: 'bg-danger' },
            { name: 'awaiting', value: awaiting + awaitingOnGitHub, color: 'bg-warning' },
            { name: 'pending', value: pending, color: 'bg-warning' },
            { name: 'skipped', value: skipped, color: 'bg-border' },
          ]}
        />
        {checksSignal(d.checks).long}
      </Fact>
      <Fact term="Review">
        {reviewStepText(d)}
        {more !== '' && <span className="text-muted">{` · ${more}`}</span>}
      </Fact>
      <Fact term="Merge">{mergeStepText(d)}</Fact>
    </Area>
  )
}

// StandsIssue is where an issue stands: the projects it is in, each with its status and
// featured fields, and how far its sub-issues are. An issue with neither has no area.
function StandsIssue({ d }: { d: ItemDetails }) {
  const { total, completed } = d.subIssues

  if (d.projects.length === 0 && total === 0) {
    return null
  }

  return (
    <Area heading="Where it stands">
      {d.projects.length > 0 && (
        <Fact term="Project">
          <span className="flex flex-col items-start gap-1">
            {d.projects.map((project) => (
              <ProjectLink key={project.url} project={project} />
            ))}
          </span>
        </Fact>
      )}
      {total > 0 && (
        <Fact term="Sub-issues">
          <span className="flex items-center gap-2">
            <ProgressBar done={completed} total={total} className="w-24" />
            {`${formatCount(completed)} of ${formatCount(total)} done`}
          </span>
        </Fact>
      )}
    </Area>
  )
}

// Change is what a pull request changes: its size, as a bar of lines added against
// removed and the counts, its head branch, and its sensitive files by category.
function Change({ item, d }: { item: InboxItem; d: ItemDetails }) {
  const { additions, deletions, changedFiles, commits } = d
  const groups = item.files?.groups ?? []
  const danger = groups.some((g) => g.level === 'danger')

  return (
    <Area heading="The change">
      <Fact term={`Size ${sizeOf(additions + deletions)}`}>
        <SegmentBar
          segments={[
            { name: 'added', value: additions, color: 'bg-success' },
            { name: 'removed', value: deletions, color: 'bg-danger' },
          ]}
        />
        <span className="text-success">+{formatCount(additions)}</span>{' '}
        <span className="text-danger">−{formatCount(deletions)}</span>
        <span className="text-muted">
          {` · ${counted(changedFiles, 'file', 'files')} · ${counted(commits, 'commit', 'commits')}`}
        </span>
      </Fact>
      {item.branch !== '' && (
        <Fact term="Branch">
          <code className="font-mono break-all">{item.branch}</code>
          {item.isCrossRepository && <span className="text-muted"> · from a fork</span>}
        </Fact>
      )}
      {item.files !== null && groups.length > 0 && (
        <Fact
          term={
            <span className={danger ? 'text-danger' : 'text-warning'}>
              Sensitive files · {formatCount(sensitiveCount(item.files))}
            </span>
          }
        >
          <span className="mt-1 flex flex-col gap-2.5">
            {groups.map((group) => (
              <SensitiveFilesGroup key={group.category} group={group} />
            ))}
          </span>
        </Fact>
      )}
    </Area>
  )
}

// teamsWords name the teams a pull request asks to review, as the byline does.
function teamsWords(teams: readonly string[]): string {
  return teams.length === 0 ? 'a team' : teams.map((team) => `@${team}`).join(', ')
}

// whyWords say why an item is in the inbox, in full: how it asks for the user's review,
// or that the user opened it, then that it is assigned to them, and its comments.
function whyWords(item: InboxItem): string {
  const asked: Readonly<Record<InboxItem['reviewRequest'], string>> = {
    you: 'review requested from you',
    team: `review requested via ${teamsWords(item.reviewTeams)}`,
    pushed: 'the author pushed since you requested changes',
    none: item.authored ? 'you opened it' : '',
  }
  const comments = item.details?.comments ?? 0
  const parts = [
    asked[item.reviewRequest],
    item.assigned ? 'assigned to you' : '',
    comments > 0 ? counted(comments, 'comment', 'comments') : '',
  ].filter((part) => part !== '')

  return capitalize(parts.join(' · '))
}

// AutopilotFact is what the routines did with an item: the newest match's chip and its
// routine, and the routine that pinned it, if one did. It reads the rows' Autopilot
// itself, and says nothing without it.
function AutopilotFact({ item }: { item: InboxItem }) {
  const rows = use(AutopilotRowsContext)
  const match = rows?.chips.get(item.id)
  const pinnedBy = useMarkedBy(item.pinnedAt === '' ? 0 : item.pinnedBy)
  const pinner = useRoutineName(pinnedBy)

  if (match === undefined && pinnedBy === 0) {
    return null
  }

  return (
    <Fact term="Autopilot">
      {match !== undefined && (
        <span className="block">
          <AutopilotIcon className="inline size-3 align-[-0.125rem]" /> {match.routine}: {chipOf(match).label}
        </span>
      )}
      {pinnedBy > 0 && (
        <span className="block text-muted">
          Pinned by <AutopilotIcon className="inline size-3 align-[-0.125rem]" /> {pinner}
        </span>
      )}
    </Fact>
  )
}

// Around is what surrounds an item: its agent session, what the routines did with it, why
// it is here, and every label.
function Around({
  item,
  session,
  agents,
  onFilterBy,
}: {
  item: InboxItem
  session: AgentSession | null
  agents: readonly AgentInfo[]
  onFilterBy: ((key: string, value: string) => void) | undefined
}) {
  const why = whyWords(item)
  const labels = item.details?.labels ?? []
  const setup = session === null ? '' : setupWords(session)

  return (
    <Area heading="Around it">
      {session !== null && (
        <Fact term="Agent">
          <span className={`block ${stateText[session.state]}`}>
            {agentName(agents, session.agent)} · {stateLabel(session.state)} ·{' '}
            <RelativeTime iso={sinceWhen(session)} show={formatAge} />
          </span>
          {setup !== '' && <span className="block text-muted">{setup}</span>}
        </Fact>
      )}
      <AutopilotFact item={item} />
      {why !== '' && <Fact term="Why it’s here">{why}</Fact>}
      {labels.length > 0 && (
        <Fact term="Labels">
          <span className="mt-0.5 flex flex-wrap gap-1.25">
            {labels.map((label) => (
              <LabelChip key={label.name} label={label} onFilterBy={onFilterBy} />
            ))}
          </span>
        </Fact>
      )}
    </Area>
  )
}

// opensIt are the primaries that already open the item: the peek offers no Review or Read
// beside them.
const opensIt: readonly Verb[] = ['review', 'rereview', 'read', 'open', 'merge']

export interface CardPeekProps {
  // id is the peek's ID, which the card's Show details buttons name while it is open.
  id: string
  item: InboxItem
  tab: TriageTab
  // step is the card's next step (nextStep), whose primary the action bar repeats.
  step: Step
  // session is the card's session (cardSession), and sessions all the item's, newest
  // first.
  session: AgentSession | null
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  triage?: TriageActions | undefined
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
  onFilterBy?: ((key: string, value: string) => void) | undefined
  // launch is the agent's button that opens the launch options (LaunchControls' card
  // variant hands it over), or null while the launch settings load.
  launch: ReactNode
}

// CardPeek is a card opened in place: everything the card leaves out, under its two lines,
// read from what the card already has, so it asks GitHub for nothing. Where it stands
// (each step of the track with its numbers, or an issue's projects and sub-issues), the
// change (its size, branch, and sensitive files), and what is around it (the agent
// session, the routines, why it is here, every label), as three columns that become two,
// then one, as the list narrows; an area with nothing to say is left out. Its action bar
// repeats the primary, offers Review or Read beside a primary that doesn't open the item,
// the agent's launch options, the triage actions, GitHub, and, while Settings → Cards'
// Unread dots is on, Mark as read or Mark as unread.
export function CardPeek({
  id,
  item,
  tab,
  step,
  session,
  sessions,
  agents,
  triage,
  onOpen,
  onFilterBy,
  launch,
}: CardPeekProps) {
  const [error, setError] = useState('')
  const { unread: unreadDots } = use(CardPrefsContext)
  const d = item.details
  const isPR = item.kind === 'pr'

  const open = () => {
    if (onOpen === undefined) {
      openExternal(item.url)
    } else {
      onOpen(item, tab)
    }
  }

  return (
    <section
      id={id}
      aria-label="Details"
      className="relative z-10 col-span-full row-start-2 mt-3 grid grid-cols-3 gap-5.5 border-t border-dashed border-border bg-linear-to-b from-accent/4 to-transparent pt-4 pl-15.25 text-xs @max-4xl:grid-cols-2 @max-4xl:pl-4 @max-xl:grid-cols-1"
    >
      {d !== null && (isPR ? <StandsPR d={d} /> : <StandsIssue d={d} />)}
      {d !== null && isPR && <Change item={item} d={d} />}
      <Around item={item} session={session} agents={agents} onFilterBy={onFilterBy} />
      <div className="col-span-full flex flex-wrap items-center gap-1.5 border-t border-border pt-3">
        <PrimaryVerb
          item={item}
          tab={tab}
          step={step}
          session={session}
          sessions={sessions}
          agents={agents}
          triage={triage}
          onOpen={onOpen}
          onError={setError}
        />
        {!opensIt.includes(step.primary) && (
          <button type="button" onClick={open} className={buttonClass}>
            {isPR ? 'Review' : 'Read'}
          </button>
        )}
        {launch}
        <span className="flex-1" />
        {triage !== undefined && tab === 'inbox' && (
          <>
            <button
              type="button"
              title={`Snooze for ${snoozeLength(triage.snoozeDays)}`}
              onClick={() => {
                triage.onSnooze(item, triage.snoozeDays)
              }}
              className={buttonClass}
            >
              <ClockIcon className="size-3.5 text-muted" />
              Snooze
            </button>
            <button
              type="button"
              title="Dismiss: hide it until you restore it"
              onClick={() => {
                triage.onDismiss(item)
              }}
              className={buttonClass}
            >
              <ArchiveIcon className="size-3.5 text-muted" />
              Dismiss
            </button>
          </>
        )}
        {triage !== undefined && (isPinnedItself(item) || !isPinned(item)) && (
          <button
            type="button"
            onClick={() => {
              if (isPinnedItself(item)) {
                triage.onUnpin(item)
              } else {
                triage.onPin(item)
              }
            }}
            className={buttonClass}
          >
            <PinIcon filled={isPinnedItself(item)} className="size-3.5 text-muted" />
            {isPinnedItself(item) ? 'Unpin' : 'Pin'}
          </button>
        )}
        <button
          type="button"
          title="Open on GitHub"
          onClick={() => {
            openOnGitHub(item, triage)
          }}
          className={buttonClass}
        >
          <ExternalIcon className="size-3.5 text-muted" />
          GitHub
        </button>
        {triage !== undefined && unreadDots && (
          <button
            type="button"
            onClick={() => {
              if (item.unread) {
                triage.onMarkRead(item)
              } else {
                triage.onMarkUnread(item)
              }
            }}
            className={buttonClass}
          >
            {item.unread ? <RingIcon className="size-3.5 text-muted" /> : <DotIcon className="size-3.5 text-info" />}
            {item.unread ? 'Mark as read' : 'Mark as unread'}
          </button>
        )}
      </div>
      {error !== '' && (
        <p role="alert" className="col-span-full -mt-3 text-xs text-danger">
          {error}
        </p>
      )}
    </section>
  )
}
