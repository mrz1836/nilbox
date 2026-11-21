import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

import { attempt, type Result } from '../../../lib/call'
import {
  applyAll,
  appliedWords,
  applyParts,
  awaitsConfirming,
  cardParts,
  changeDraft,
  changedId,
  partKey,
  partProblems,
  pastRulesConfirmed,
  promptName,
  promptsRun,
  recorded,
  settlePart,
  type CardPart,
  type PromptPart,
  type RoutinePart,
  type Settled,
} from '../../../lib/describeit'
import { noTicks, type PastTicks } from '../../../lib/routineBuilder'
import { listRoutines, type AutopilotMode, type AutopilotRoutine, type DescribeCard } from '../../../platform'
import { buttonClass, captionClass, ErrorText, primaryButtonClass } from '../../controls'
import { useRoutineChange } from '../useRoutineChange'
import type { CardKit } from './parts'
import { PromptChange } from './PromptChange'
import { RoutineChange } from './RoutineChange'
import { RulesChange } from './RulesChange'
import { SettingsChange } from './SettingsChange'

export interface ProposalCardProps {
  card: DescribeCard
  kit: CardKit
  // source says where and when the card came from, for one from the user's own Claude
  // Code; undefined in the conversation, where its turn says.
  source?: ReactNode
  // onDiscard discards every part of the card no one dealt with, for one from the user's
  // own Claude Code, resolving with why it couldn't, or ''.
  onDiscard?: () => Promise<string>
}

// noop is what useRoutineChange hears of a change the card asked for: each request says
// itself what hears it.
function noop(): void {
  // Each request hears its own outcome.
}

// ProposalCard is one of Claude's proposals: what Claude says it does, then its parts in
// the order they apply (prompts, routines, settings, rules), each a row the user applies or
// discards on its own, its outcome in place of its buttons once settled (a routine's adding
// first the prompts it runs that the card adds and no one dealt with yet); and, for a card
// of several parts, Apply all, which applies those left in order and stops at the first
// that fails, saying which and why. A part with problems can't be applied, and the card
// says so; a routine whose Merge it may go past the rules waits, its row and Apply all
// alike, until the user confirms on its row each thing it may do.
export function ProposalCard({ card, kit, source, onDiscard }: ProposalCardProps) {
  const id = useId()
  const parts = cardParts(card)
  const [local, setLocal] = useState<ReadonlyMap<string, Settled>>(new Map())
  const [modes, setModes] = useState<ReadonlyMap<number, AutopilotMode>>(new Map())
  const [applying, setApplying] = useState(false)
  const [stopped, setStopped] = useState('')
  const [discarding, setDiscarding] = useState(false)
  // adding reports a routine's row adding the card's prompts it runs, before the routine:
  // the card's other rows wait, so none of those prompts is added twice.
  const [adding, setAdding] = useState(false)
  // ticked are the ticks the user confirmed on each routine's row, by its index, for one
  // whose Merge it may go past the rules.
  const [ticked, setTicked] = useState<ReadonlyMap<number, PastTicks>>(new Map())
  const changes = useRoutineChange(noop, noop)
  // waiting is the question a change waits on, resolving with how it went; asked, whether
  // the question was open the last time the card rendered.
  const waiting = useRef<((r: Result<AutopilotRoutine>) => void) | null>(null)
  const asked = useRef(false)

  // A question closed unanswered changes nothing: its change ends with an error of ''.
  useEffect(() => {
    const resolve = waiting.current

    if (asked.current && !changes.asking && resolve !== null) {
      waiting.current = null
      resolve({ ok: false, error: '' })
    }

    asked.current = changes.asking
  }, [changes.asking])

  const outcomeOf = (p: CardPart) => local.get(partKey(p)) ?? recorded(card, p)
  const modeOf = (p: RoutinePart) => modes.get(p.index) ?? kit.defaultMode
  const tickedOf = (p: CardPart) => (p.part === 'routine' ? (ticked.get(p.index) ?? noTicks) : noTicks)
  const accepted = (p: RoutinePart) => pastRulesConfirmed(p, tickedOf(p))
  const left = parts.filter((p) => outcomeOf(p) === undefined)
  const blocked = left.filter((p) => partProblems(p).length > 0)
  const unconfirmed = left.filter((p) => awaitsConfirming(p, tickedOf(p)))
  const held = applying || discarding || adding

  const record = (p: CardPart, s: Settled) => {
    setLocal((was) => new Map(was).set(partKey(p), s))
  }

  // settle records what the user did with a part, which Claude's next turn hears; one the
  // user discarded counts only once it is recorded, since discarding does nothing else.
  const settle = async (p: CardPart, s: Settled) => {
    const done = await settlePart(card, p, s)

    if (done.ok || s.kind !== 'discarded') {
      record(p, s)
    }

    kit.onSettled()

    return done
  }

  // saveChange saves a change to one of the user's routines, as it is now, with the user's
  // confirmation that it may go past the rules when accepted says so: the question about
  // what it matches now is asked when it is on and the change reaches what it watches,
  // fires on, checks, or fires once per.
  const saveChange = async (p: RoutinePart, accepted: boolean): Promise<Result<AutopilotRoutine>> => {
    const listed = await attempt(listRoutines())

    if (!listed.ok) {
      return listed
    }

    const routine = listed.data.find((r) => r.id === changedId(p))

    if (routine === undefined) {
      return {
        ok: false,
        error: `“${p.checked.before?.name ?? p.checked.name}” is gone, so the change can’t be saved.`,
      }
    }

    return new Promise((resolve) => {
      waiting.current = resolve
      changes.request(
        routine,
        { kind: 'save', draft: changeDraft(routine, p, accepted) },
        (saved) => {
          waiting.current = null
          resolve({ ok: true, data: saved })
        },
        (text) => {
          waiting.current = null
          resolve({ ok: false, error: text })
        },
      )
    })
  }

  const applyKit = { modeOf, accepted, saveChange, onSettled: record, onSettingsSaved: kit.onSettingsSaved }

  const applyEvery = async () => {
    setApplying(true)
    setStopped('')

    const done = await applyAll(card, { ...applyKit, settled: (p) => outcomeOf(p) !== undefined })

    setApplying(false)
    kit.onSettled()

    if (!done.ok) {
      setStopped(appliedWords(done))
    }
  }

  // addFirst adds the card's prompts a routine runs, before its row saves the routine, and
  // records each as Apply all does, resolving with where it stopped and why.
  const addFirst = async (prompts: readonly PromptPart[]): Promise<Result<void>> => {
    setAdding(true)

    const done = await applyParts(card, prompts, applyKit)

    setAdding(false)
    kit.onSettled()

    return done.ok ? { ok: true, data: undefined } : { ok: false, error: appliedWords(done) }
  }

  const discardAll = async (discard: () => Promise<string>) => {
    setDiscarding(true)
    setStopped(await discard())
    setDiscarding(false)
  }

  const row = (p: CardPart) => {
    const shared = { card, outcome: outcomeOf(p), held, onSettle: settle, kit }

    switch (p.part) {
      case 'prompt':
        return <PromptChange key={partKey(p)} p={p} {...shared} />
      case 'routine': {
        const first = promptsRun(p, left)

        return (
          <RoutineChange
            key={partKey(p)}
            p={p}
            {...shared}
            mode={modeOf(p)}
            onMode={(mode) => {
              setModes((was) => new Map(was).set(p.index, mode))
            }}
            saveChange={saveChange}
            saving={changes.busy !== 0 && changes.busy === changedId(p)}
            waitsOn={first.map((q) => promptName(q.checked.prompt))}
            addFirst={() => addFirst(first)}
            ticked={tickedOf(p)}
            onTick={(next) => {
              setTicked((was) => new Map(was).set(p.index, next))
            }}
          />
        )
      }
      case 'settings':
        return <SettingsChange key={partKey(p)} p={p} {...shared} />
      case 'rules':
        return <RulesChange key={partKey(p)} p={p} {...shared} />
    }
  }

  return (
    <article
      aria-labelledby={`${id}-says`}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface px-4 py-3"
    >
      {source}
      <header className="flex flex-col gap-1">
        <h3 className={captionClass}>Claude says</h3>
        <p id={`${id}-says`} className="text-sm leading-relaxed text-text">
          {card.summary}
        </p>
      </header>
      {parts.map(row)}
      {(parts.length > 1 || onDiscard !== undefined) && left.length > 0 && (
        <footer className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
          {parts.length > 1 && (
            <button
              type="button"
              disabled={held || blocked.length > 0 || unconfirmed.length > 0}
              onClick={() => {
                void applyEvery()
              }}
              className={primaryButtonClass}
            >
              {applying ? 'Applying…' : 'Apply all'}
            </button>
          )}
          {onDiscard !== undefined && (
            <button
              type="button"
              aria-label="Discard this card"
              disabled={held}
              onClick={() => {
                void discardAll(onDiscard)
              }}
              className={buttonClass}
            >
              Discard
            </button>
          )}
          {parts.length > 1 && blocked.length > 0 && (
            <p className="min-w-0 flex-1 text-xs text-muted">
              {blocked.length === 1
                ? 'One part has problems, so Apply all is off: apply the others one by one, or discard it.'
                : `${String(blocked.length)} parts have problems, so Apply all is off: apply the others one by one, or discard those.`}
            </p>
          )}
          {parts.length > 1 && blocked.length === 0 && unconfirmed.length > 0 && (
            <p className="min-w-0 flex-1 text-xs text-muted">
              {unconfirmed.length === 1
                ? 'Apply all waits until you confirm, on its row, what the routine may do past the rules.'
                : `Apply all waits until you confirm, on their rows, what ${String(unconfirmed.length)} routines may do past the rules.`}
            </p>
          )}
        </footer>
      )}
      <ErrorText error={stopped} />
      {changes.dialog}
    </article>
  )
}
