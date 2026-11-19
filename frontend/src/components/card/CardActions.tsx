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

// compactOutcomeClass places such a line under a compact card's one line, from its
// repository to its right edge, with room under it before the next card.
export const compactOutcomeClass = 'relative z-10 col-[3/-1] mb-2'

// verbShape is every primary button's shape.
const verbShape =
  'inline-flex h-7.5 shrink-0 items-center gap-1.5 rounded-lg border px-2.75 text-[0.8125rem] font-semibold whitespace-nowrap transition-colors disabled:cursor-default disabled:opacity-60'

// loud is a primary outlined in its edge's color, with its text in it, that fills with the
// color while the card is lit (card-lit in index.css: the pointer on it, keyboard focus in
// it, or a menu or panel it opened open): white text on it, or the surface's on the
// success and warning colors, which are bright in the dark theme. Each is spelled out in
// full so Tailwind finds it.
const loud: Readonly<Record<Edge, string>> = {
  urgent: 'border-warning/45 text-warning card-lit:border-transparent card-lit:bg-warning card-lit:text-surface',
  blocked: 'border-danger/45 text-danger card-lit:border-transparent card-lit:bg-danger card-lit:text-white',
  yours: 'border-accent/45 text-accent card-lit:border-transparent card-lit:bg-accent card-lit:text-white',
  ready: 'border-success/45 text-success card-lit:border-transparent card-lit:bg-success card-lit:text-surface',
  agent: 'border-info/45 text-info card-lit:border-transparent card-lit:bg-info card-lit:text-white',
  waiting: 'border-border text-muted card-lit:bg-raised',
  none: 'border-border text-muted card-lit:bg-raised',
}

// quiet is a primary that isn't the card's urgent move: outlined in the border color,
// filling with the raised color.
const quiet = 'border-border text-text card-lit:bg-raised'

// pills are the primaries that stand for an agent: Show terminal and Watch for one that
// waits on the user, and What happened for one that failed.
const pills = {
  warning: 'border-warning/45 bg-warning/14 text-warning hover:bg-warning/22',
  danger: 'border-danger/30 bg-danger/10 text-danger hover:bg-danger/18',
} as const

// goShape is a compact card's go button: the primary's words alone, no outline and no
// icon.
const goShape =
  'inline-flex h-6.5 shrink-0 items-center rounded-md px-2.25 text-[0.78125rem] font-semibold whitespace-nowrap transition-colors disabled:cursor-default disabled:opacity-60'

// goTones are the go button's colors: its words in the edge's color, filled with it while
// the card is lit (white text on it, or the surface's on the success and warning colors);
// a quiet one, or one waiting on others, muted and filling with the raised color. Each is
// spelled out in full so Tailwind finds it.
const goTones: Readonly<Record<Edge, string>> = {
  urgent: 'text-warning card-lit:bg-warning card-lit:text-surface',
  blocked: 'text-danger card-lit:bg-danger card-lit:text-white',
  yours: 'text-accent card-lit:bg-accent card-lit:text-white',
  ready: 'text-success card-lit:bg-success card-lit:text-surface',
  agent: 'text-info card-lit:bg-info card-lit:text-white',
  waiting: 'text-muted card-lit:bg-raised card-lit:text-text',
  none: 'text-muted card-lit:bg-raised card-lit:text-text',
}

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
  nudge: 'Nudge',
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
  // onNudge opens the Nudge dialog for the item, for the nudge verb; without it the verb
  // opens the item, as Open does.
  onNudge?: ((item: InboxItem) => void) | undefined
  onError: (message: string) => void
  // compact draws the primary as a compact card's go button (goShape): its words alone,
  // in its tone, even for an agent's pill.
  compact?: boolean
}

// loudEdge is the edge whose color a loud primary takes: its card's, but the user's own
// for a nudge, which is the user's move on a card that still waits on its author.
function loudEdge(step: Step): Edge {
  return step.primary === 'nudge' ? 'yours' : step.edge
}

// tone is a primary's colors: quiet, or loud in its edge's color.
function tone(step: Step): string {
  return step.quiet ? quiet : loud[loudEdge(step)]
}

// verbClass is a primary's shape and colors: in a compact card, the go button in the
// edge's tone (muted when quiet); else the verb's outline in its tone, or pill, the colors
// of an agent's pill.
function verbClass(step: Step, compact: boolean, pill?: string): string {
  if (compact) {
    return `${goShape} ${goTones[step.quiet ? 'waiting' : loudEdge(step)]}`
  }

  return `${verbShape} ${pill ?? tone(step)}`
}

// DecideVerb opens the routine's Needs you card in the Autopilot tab. It reads the rows'
// Autopilot itself, so only it renders again as Autopilot's matches change.
function DecideVerb({ item, step, compact = false }: PrimaryVerbProps) {
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
      className={verbClass(step, compact)}
    >
      {labels.decide}
    </button>
  )
}

// TerminalVerb brings the terminal tab of an agent waiting on the user forward, breathing
// so it is seen (a compact card's go button holds still); it waits until the session can
// show its tab.
function TerminalVerb({
  step,
  session,
  onError,
  compact,
}: {
  step: Step
  session: AgentSession
  onError: (message: string) => void
  compact: boolean
}) {
  return (
    <button
      type="button"
      data-verb="terminal"
      disabled={!session.canFocus}
      onClick={() => {
        void attempt(focusSession(session.id)).then((shown) => {
          onError(shown.ok ? '' : shown.error)
        })
      }}
      className={
        compact ? verbClass(step, true) : `${verbClass(step, false, pills.warning)} motion-safe:animate-breathe`
      }
    >
      {!compact && <HandIcon className="size-3.5" />}
      {labels.terminal}
    </button>
  )
}

// WatchVerb opens the run view of an agent's background run waiting on the user, or,
// where there is none to open (Autopilot's Watching list), the item's sessions.
function WatchVerb({
  step,
  session,
  sessions,
  agents,
  compact,
}: {
  step: Step
  session: AgentSession
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  compact: boolean
}) {
  const watch = useWatch()
  const className = verbClass(step, compact, pills.warning)

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
  const { item, tab, step, session, sessions, agents, triage, onOpen, onNudge, onError, compact = false } = props
  const verb = step.primary

  // opens is the primary that opens the item, in its sheet or on GitHub, with shown's
  // words.
  const opens = (shown: Verb) => (
    <button
      type="button"
      data-verb={shown}
      onClick={() => {
        if (onOpen === undefined) {
          openExternal(item.url)
        } else {
          onOpen(item, tab)
        }
      }}
      className={verbClass(step, compact)}
    >
      {shown === 'merge' && !compact && <MergeIcon className="size-3.5" />}
      {labels[shown]}
    </button>
  )

  switch (verb) {
    case 'nudge':
      return onNudge === undefined ? (
        opens('open')
      ) : (
        <button
          type="button"
          data-verb="nudge"
          title={`Ask @${item.author} about the changes you requested, in a comment you see before it’s posted`}
          onClick={() => {
            onNudge(item)
          }}
          className={verbClass(step, compact)}
        >
          {labels.nudge}
        </button>
      )
    case 'decide':
      return <DecideVerb {...props} />
    case 'terminal':
      return session === null ? null : (
        <TerminalVerb step={step} session={session} onError={onError} compact={compact} />
      )
    case 'watch':
      return session === null ? null : (
        <WatchVerb step={step} session={session} sessions={sessions} agents={agents} compact={compact} />
      )
    case 'sessions':
      return (
        <SessionsButton
          sessions={sessions}
          agents={agents}
          align="right"
          className={verbClass(step, compact, pills.danger)}
        >
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
          className={verbClass(step, compact)}
        >
          {labels[verb]}
        </button>
      )
    }
    default:
      return opens(verb)
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
  // onNudge opens the Nudge dialog for the item, which the nudge verb asks for.
  onNudge?: ((item: InboxItem) => void) | undefined
  // agent is the agent's slot beside the primary (LaunchControls in its card variant).
  agent?: ReactNode
  // compact draws the column as a compact card's go button, whose failure shows under the
  // card's one line (compactOutcomeClass); the agent's slot is in its hover row instead.
  compact?: boolean
}

// CardActions is a card's action column: the human's next move (the primary verb), then
// the agent's beside it. The primary is loud, in the edge's color, when it is the card's
// urgent move, and quiet otherwise. It renders two grid children: the column, and, when
// showing a terminal fails, the failure under the card's two lines (cardOutcomeClass).
//
// The column is two slots, so every card's controls line up down the list: the primary's,
// as wide as the longest common primary (--card-primary, on the card), the primary at its
// start, widening for a longer one (Show terminal, What happened), which comes only beside
// the agent's ▾; and the agent's, the rest of the column, its control at its end, so the ▾
// closing it sits at the same place on every card and the control narrows within the slot,
// never over the primary. In a list narrower than 48rem the primary's slot fits its primary alone, and an
// agent's pill drops to its mark (its words stay for screen readers): the column always
// holds the primary, so a pill in it always has a second button beside it. A panel open
// from the column rises over the card's later parts, such as its peek. A compact card's
// column is its go button alone, at its end.
export function CardActions({
  item,
  tab,
  step,
  session,
  sessions,
  agents,
  triage,
  onOpen,
  onNudge,
  agent,
  compact = false,
}: CardActionsProps) {
  const [error, setError] = useState('')
  const primary = (
    <PrimaryVerb
      item={item}
      tab={tab}
      step={step}
      session={session}
      sessions={sessions}
      agents={agents}
      triage={triage}
      onOpen={onOpen}
      onNudge={onNudge}
      onError={setError}
      compact={compact}
    />
  )

  return (
    <>
      {compact ? (
        <div
          data-guide="actions"
          className="relative z-10 flex min-w-0 items-center justify-end gap-1.5 has-[[aria-expanded=true]]:z-20 @max-3xl:[&:has([data-pill-label])_[data-pill-label]]:sr-only"
        >
          {primary}
        </div>
      ) : (
        <div
          data-guide="actions"
          className="relative z-10 grid min-w-0 grid-cols-[minmax(var(--card-primary),max-content)_minmax(0,1fr)] items-center gap-x-1.5 has-[[aria-expanded=true]]:z-20 @max-3xl:[&:has([data-pill-label])_[data-pill-label]]:sr-only"
        >
          <div data-primary-slot className="flex min-w-0 justify-start">
            {primary}
          </div>
          <div data-agent-slot className="flex min-w-0 justify-end">
            {agent}
          </div>
        </div>
      )}
      {error !== '' && (
        <p role="alert" className={`${compact ? compactOutcomeClass : cardOutcomeClass} text-xs text-danger`}>
          {error}
        </p>
      )}
    </>
  )
}
