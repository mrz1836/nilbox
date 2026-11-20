import { useState, type ReactNode } from 'react'

import type { Result } from '../../../lib/call'
import { outcomeText, settleWords, type CardPart, type Settled } from '../../../lib/describeit'
import type {
  AutopilotCatalog,
  AutopilotDraft,
  AutopilotMode,
  DescribeCard,
  KitOutcomeKind,
  KitPart,
  KitRisk,
  Settings,
} from '../../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../../controls'
import { AlertIcon, CheckIcon } from '../../icons'

// CardKit is what every card reads from Describe it beside its own proposal.
export interface CardKit {
  // defaultMode is the mode new routines start in, as Settings says.
  defaultMode: AutopilotMode
  // catalog is what a routine can do, undefined until it is read: Test's headline names
  // the kinds of item a routine acts on from it.
  catalog: AutopilotCatalog | undefined
  // onEditInBuilder opens the builder on a new routine's draft; onShowRoutine shows one of
  // the user's routines.
  onEditInBuilder: (draft: AutopilotDraft) => void
  onShowRoutine: (id: number) => void
  // onSettled hears a part dealt with, so what shows the card reads it again.
  onSettled: () => void
  // onSettingsSaved hears the settings as a card's settings or rules change left them, so
  // the window's own (Settings, the tab's Pause Autopilot) show the change.
  onSettingsSaved: (settings: Settings) => void
  // describeDelay is how long a routine's draft rests before it is read, in milliseconds.
  describeDelay: number
}

// RowProps are what each row of a card takes: the card, its part, what the user did with
// it once they did, whether Apply all holds every button, and how the row records what the
// user does.
export interface RowProps<P extends CardPart> {
  card: DescribeCard
  p: P
  outcome: Settled | undefined
  held: boolean
  onSettle: (p: CardPart, s: Settled) => Promise<Result<void>>
  kit: CardKit
}

// Act is a row's buttons' work: busy while one runs, and why the last one failed.
export interface Act {
  busy: boolean
  error: string
  // act does what a button does (apply, resolving with the routine it saved, 0 for none;
  // an error of '' is a question closed unanswered, which changes nothing), then records
  // it as kind.
  act: (kind: KitOutcomeKind, apply?: () => Promise<Result<number>>) => Promise<void>
}

// useAct runs a row's buttons: each applies its part, if it applies anything, and records
// what the user did.
export function useAct(p: CardPart, onSettle: RowProps<CardPart>['onSettle']): Act {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const act = async (kind: KitOutcomeKind, apply?: () => Promise<Result<number>>) => {
    setBusy(true)
    setError('')

    const applied: Result<number> = apply === undefined ? { ok: true, data: 0 } : await apply()

    if (!applied.ok) {
      setBusy(false)
      setError(applied.error)

      return
    }

    const settled = await onSettle(p, { kind, words: settleWords(p, kind), routineId: applied.data })

    setBusy(false)

    if (!settled.ok) {
      setError(
        kind === 'discarded'
          ? `It couldn’t be discarded: ${settled.error}`
          : `It’s done, but Claude won’t hear of it: ${settled.error}`,
      )
    }
  }

  return { busy, error, act }
}

export interface OutcomeLineProps {
  part: KitPart
  outcome: Settled
  onShowRoutine: (id: number) => void
}

// OutcomeLine is what a row says once it is dealt with, in place of its buttons: ✓ Added,
// switched off · Show it.
export function OutcomeLine({ part, outcome, onShowRoutine }: OutcomeLineProps) {
  const done = outcome.kind === 'added' || outcome.kind === 'saved' || outcome.kind === 'applied'
  const shows = outcome.routineId > 0 && (outcome.kind === 'added' || outcome.kind === 'saved')

  return (
    <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted">
      {done && <CheckIcon className="size-3.5 text-success" />}
      <span className={done ? 'text-text' : ''}>{outcomeText(part, outcome.kind)}</span>
      {shows && (
        <>
          <span aria-hidden="true">·</span>
          <button
            type="button"
            onClick={() => {
              onShowRoutine(outcome.routineId)
            }}
            className={quietButtonClass}
          >
            Show it
          </button>
        </>
      )}
    </p>
  )
}

// Risks are what a part changes that the user should read before applying it, worked out
// from what it changes, never from Claude's words: a warning as a ⚠ line, a caution as a
// quiet note.
export function Risks({ risks }: { risks: readonly KitRisk[] }) {
  if (risks.length === 0) {
    return null
  }

  return (
    <ul aria-label="Before you apply it" className="flex flex-col gap-0.5">
      {risks.map((r) =>
        r.level === 'warning' ? (
          <li key={r.text} className="flex items-start gap-1.5 text-xs text-warning">
            <AlertIcon className="mt-0.5 size-3 shrink-0" />
            <span>
              <span className="sr-only">Warning: </span>
              {r.text}
            </span>
          </li>
        ) : (
          <li key={r.text} className="text-xs text-muted">
            {r.text}
          </li>
        ),
      )}
    </ul>
  )
}

// Problems are what keeps a part from being applied, each a sentence, and what the user can
// do about them.
export function Problems({ problems }: { problems: readonly string[] }) {
  if (problems.length === 0) {
    return null
  }

  return (
    <div className="flex flex-col gap-0.5">
      <ul aria-label="What keeps it from being applied" className="flex flex-col gap-0.5">
        {problems.map((text) => (
          <li key={text} className="flex items-start gap-1.5 text-xs text-danger">
            <AlertIcon className="mt-0.5 size-3 shrink-0" />
            <span>{text}</span>
          </li>
        ))}
      </ul>
      <CantApply />
    </div>
  )
}

// CantApply says a part with problems can't be applied, and what to do instead.
export function CantApply() {
  return <p className="text-xs text-muted">It can’t be applied like this: ask Claude to fix it, or discard it.</p>
}

export interface RowActionsProps {
  // label is the part's own button's words (Add, Save changes, Apply), and blocked whether
  // its problems hold it back.
  label: string
  blocked: boolean
  busy: boolean
  onApply: () => void
  onDiscard: () => void
  // children are the row's other buttons, between its own and Discard.
  children?: ReactNode
  error: string
}

// RowActions are a row's buttons: its own, the row's others, and Discard, with why the
// last one failed.
export function RowActions({ label, blocked, busy, onApply, onDiscard, children, error }: RowActionsProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" disabled={blocked || busy} onClick={onApply} className={primaryButtonClass}>
          {label}
        </button>
        {children}
        <button type="button" disabled={busy} onClick={onDiscard} className={buttonClass}>
          Discard
        </button>
      </div>
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}

// rowClass lays a card's row out.
export const rowClass = 'flex flex-col gap-2 border-t border-border pt-3'

// RowTitle is a row's heading, with what kind of change it is beside it.
export function RowTitle({
  id,
  title,
  kind,
  children,
}: {
  id: string
  title: string
  kind: string
  children?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-baseline gap-2">
      <h4 id={id} className="text-sm font-semibold text-text">
        {title}
      </h4>
      <span className="rounded-full border border-border px-2 py-0.5 text-[0.6875rem] whitespace-nowrap text-muted">
        {kind}
      </span>
      {children}
    </div>
  )
}
