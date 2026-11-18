import { use, useState, type ReactNode } from 'react'

import { targetOf } from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import type { Edge, Step, Verb } from '../../lib/nextStep'
import type { TriageTab } from '../../lib/triage'
import { focusSession, openExternal, type AgentInfo, type AgentSession, type InboxItem } from '../../platform'
import { AutopilotRowsContext, useWatch } from '../autopilot/AutopilotChip'
import { HandIcon, MergeIcon } from '../icons'
import { SessionsButton } from '../SessionTag'
import { bringBack, type TriageActions } from '../TriageControls'

// cardOutcomeClass places a line the card's actions report, a failure or a launch's
// outcome, under the card's two lines, from its body to its right edge: CardActions' own
// errors, and LaunchControls' outcome in the card (its outcomeClass). It sits above the
// card's stretched toggle, so its buttons take their own clicks.
export const cardOutcomeClass = 'relative z-10 col-[3/-1] mt-1.5'

// verbShape is every primary button's shape.
const verbShape =
  'inline-flex h-7.5 shrink-0 items-center gap-1.5 rounded-lg border px-2.75 text-[0.8125rem] font-semibold whitespace-nowrap transition-colors disabled:cursor-default disabled:opacity-60'

// loud is a primary outlined in its edge's color, with its text in it, that fills with the
// color while the pointer or focus is on the card: white text on it, or the surface's on
// the success and warning colors, which are bright in the dark theme. Each is spelled out
// in full so Tailwind finds it.
const loud: Readonly<Record<Edge, string>> = {
  urgent:
    'border-warning/45 text-warning group-focus-within:border-transparent group-focus-within:bg-warning group-focus-within:text-surface group-hover:border-transparent group-hover:bg-warning group-hover:text-surface',
  blocked:
    'border-danger/45 text-danger group-focus-within:border-transparent group-focus-within:bg-danger group-focus-within:text-white group-hover:border-transparent group-hover:bg-danger group-hover:text-white',
  yours:
    'border-accent/45 text-accent group-focus-within:border-transparent group-focus-within:bg-accent group-focus-within:text-white group-hover:border-transparent group-hover:bg-accent group-hover:text-white',
  ready:
    'border-success/45 text-success group-focus-within:border-transparent group-focus-within:bg-success group-focus-within:text-surface group-hover:border-transparent group-hover:bg-success group-hover:text-surface',
  agent:
    'border-info/45 text-info group-focus-within:border-transparent group-focus-within:bg-info group-focus-within:text-white group-hover:border-transparent group-hover:bg-info group-hover:text-white',
  waiting: 'border-border text-muted group-focus-within:bg-raised group-hover:bg-raised',
  none: 'border-border text-muted group-focus-within:bg-raised group-hover:bg-raised',
}

// quiet is a primary that isn't the card's urgent move: outlined in the border color,
// filling with the raised color.
const quiet = 'border-border text-text group-focus-within:bg-raised group-hover:bg-raised'

// pills are the primaries that stand for an agent: Show terminal and Watch for one that
// waits on the user, and What happened for one that failed.
const pills = {
  warning: 'border-warning/45 bg-warning/14 text-warning hover:bg-warning/22',
  danger: 'border-danger/30 bg-danger/10 text-danger hover:bg-danger/18',
} as const

// labels are each primary's words.
const labels: Readonly<Record<Verb, string>> = {
  review: 'Review',
  rereview: 'Re-review',
  read: 'Read',
  open: 'Open',
  merge: 'Merge',
  decide: 'Decide',
  terminal: 'Show terminal',
  watch: 'Watch',
  sessions: 'What happened',
  unsnooze: 'Unsnooze',
  restore: 'Restore',
  unmute: 'Unmute',
}

export interface PrimaryVerbProps {
  item: InboxItem
  tab: TriageTab
  step: Step
  session: AgentSession | null
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  triage: TriageActions | undefined
  onOpen: ((item: InboxItem, tab: TriageTab) => void) | undefined
  onError: (message: string) => void
}

// tone is a primary's colors: quiet, or loud in its edge's color.
function tone(step: Step): string {
  return step.quiet ? quiet : loud[step.edge]
}

// DecideVerb opens the routine's Needs you card in the Autopilot tab. It reads the rows'
// Autopilot itself, so only it renders again as Autopilot's matches change.
function DecideVerb({ item, step }: PrimaryVerbProps) {
  const rows = use(AutopilotRowsContext)
  const match = rows?.chips.get(item.id)
  const show = rows?.onShow
  const decide =
    match === undefined || show === undefined
      ? undefined
      : () => {
          show(targetOf(match))
        }

  return (
    <button
      type="button"
      data-verb="decide"
      disabled={decide === undefined}
      onClick={decide}
      className={`${verbShape} ${tone(step)}`}
    >
      {labels.decide}
    </button>
  )
}

// TerminalVerb brings the terminal tab of an agent waiting on the user forward, breathing
// so it is seen; it waits until the session can show its tab.
function TerminalVerb({ session, onError }: { session: AgentSession; onError: (message: string) => void }) {
  return (
    <button
      type="button"
      data-verb="terminal"
      data-moving
      disabled={!session.canFocus}
      onClick={() => {
        void attempt(focusSession(session.id)).then((shown) => {
          onError(shown.ok ? '' : shown.error)
        })
      }}
      className={`${verbShape} ${pills.warning} motion-safe:animate-breathe`}
    >
      <HandIcon className="size-3.5" />
      {labels.terminal}
    </button>
  )
}

// WatchVerb opens the run view of an agent's background run waiting on the user, or,
// where there is none to open (Autopilot's Watching list), the item's sessions.
function WatchVerb({
  session,
  sessions,
  agents,
}: {
  session: AgentSession
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
}) {
  const watch = useWatch()
  const className = `${verbShape} ${pills.warning}`

  if (watch === undefined) {
    return (
      <SessionsButton sessions={sessions} agents={agents} align="right" className={className}>
        {labels.watch}
      </SessionsButton>
    )
  }

  return (
    <button
      type="button"
      data-verb="watch"
      onClick={() => {
        watch(session.id)
      }}
      className={className}
    >
      {labels.watch}
    </button>
  )
}

// PrimaryVerb is the card's primary button for its step's verb, in its action column and
// in its peek. The verbs for an agent's session have none without the session.
export function PrimaryVerb(props: PrimaryVerbProps) {
  const { item, tab, step, session, sessions, agents, triage, onOpen, onError } = props
  const verb = step.primary

  switch (verb) {
    case 'decide':
      return <DecideVerb {...props} />
    case 'terminal':
      return session === null ? null : <TerminalVerb session={session} onError={onError} />
    case 'watch':
      return session === null ? null : <WatchVerb session={session} sessions={sessions} agents={agents} />
    case 'sessions':
      return (
        <SessionsButton sessions={sessions} agents={agents} align="right" className={`${verbShape} ${pills.danger}`}>
          {labels.sessions}
        </SessionsButton>
      )
    case 'unsnooze':
    case 'restore':
    case 'unmute': {
      const action = tab === 'inbox' || triage === undefined ? null : bringBack(item, tab, triage)

      return (
        <button
          type="button"
          data-verb={verb}
          aria-label={action?.name}
          title={action?.title}
          disabled={action === null}
          onClick={action?.run}
          className={`${verbShape} ${tone(step)}`}
        >
          {labels[verb]}
        </button>
      )
    }
    default:
      return (
        <button
          type="button"
          data-verb={verb}
          onClick={() => {
            if (onOpen === undefined) {
              openExternal(item.url)
            } else {
              onOpen(item, tab)
            }
          }}
          className={`${verbShape} ${tone(step)}`}
        >
          {verb === 'merge' && <MergeIcon className="size-3.5" />}
          {labels[verb]}
        </button>
      )
  }
}

export interface CardActionsProps {
  item: InboxItem
  tab: TriageTab
  // step is the card's next step (nextStep): its primary verb, its edge, and whether the
  // primary is quiet.
  step: Step
  // session is the card's session (cardSession), and sessions all the item's, newest
  // first, which the sessions panel lists.
  session: AgentSession | null
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  // triage brings a set-aside item back; without it the card can't.
  triage?: TriageActions | undefined
  // onOpen opens the item in its sheet, from the card's tab; without it the primaries that
  // open the item open it on GitHub.
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
  // agent is the agent's slot beside the primary (LaunchControls in its card variant).
  agent?: ReactNode
}

// CardActions is a card's action column: the human's next move (the primary verb), then
// the agent's beside it. The primary is loud, in the edge's color, when it is the card's
// urgent move, and quiet otherwise. It renders two grid children: the column, and, when
// showing a terminal fails, the failure under the card's two lines (cardOutcomeClass).
//
// In a list narrower than 48rem, an agent's pill beside the primary drops to its mark
// (its words stay for screen readers): the column always holds the primary, so a pill in
// it always has a second button beside it. A panel open from the column rises over the
// card's later parts, such as its peek.
export function CardActions({ item, tab, step, session, sessions, agents, triage, onOpen, agent }: CardActionsProps) {
  const [error, setError] = useState('')

  return (
    <>
      <div className="relative z-10 flex min-w-0 items-center justify-end gap-1.5 has-[[aria-expanded=true]]:z-20 @max-3xl:[&:has([data-pill-label])_[data-pill-label]]:sr-only">
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
        {agent}
      </div>
      {error !== '' && (
        <p role="alert" className={`${cardOutcomeClass} text-xs text-danger`}>
          {error}
        </p>
      )}
    </>
  )
}
