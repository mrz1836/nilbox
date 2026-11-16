import { useEffect, useRef, useState, type ReactNode } from 'react'

import { itemText, needsYou, runLabel, skipLabel } from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import { useMutation } from '../../lib/useMutation'
import type { ReadinessState } from '../../lib/useAutopilot'
import {
  approveMatches,
  cloneRepo,
  cloneTarget,
  skipMatch,
  type AutopilotItemRef,
  type AutopilotMatch,
  type CloneTarget,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, captionClass, primaryButtonClass, quietButtonClass } from '../controls'
import { HoldDetails, holdTitle } from '../HoldDetails'
import { AlertIcon, ClockIcon, RefreshIcon } from '../icons'
import { Spinner } from '../Spinner'

export interface NeedsYouSectionProps {
  // matches are the open matches that haven't started, newest first: NeedsYou's.
  matches: readonly AutopilotMatch[]
  // routine shows one routine's alone, or every routine's for 0.
  routine: number
  // error is why they couldn't be read, or ''.
  error: string
  // readiness is this Mac's, whose Check again a card that can't start yet offers.
  readiness: ReadinessState
  // highlight is the match a notification asked to show, whose card is marked and
  // scrolled into view; seq numbers the asks, so asking again scrolls again.
  highlight: { matchId: number; seq: number } | null
  // agentFor names the agent a match's routine runs, for a hold's words.
  agentFor: (m: AutopilotMatch) => string
  // onOpen shows the match's item, as Activity's Open does.
  onOpen: (item: AutopilotItemRef, routineId: number) => void
  // onToast says what an action did, or why it couldn't.
  onToast: (text: string, tone?: 'error') => void
  // onChanged hears an action go through, so the list is read again at once.
  onChanged: () => void
}

interface CardProps extends Pick<NeedsYouSectionProps, 'agentFor' | 'onOpen' | 'onToast' | 'onChanged'> {
  match: AutopilotMatch
  highlighted: boolean
  // children are the card's own buttons, before Skip and Open.
  actions: (run: (what: () => Promise<string>) => void, pending: boolean) => ReactNode
}

// Card is one match that needs the user: the item, the routine, what it waits on, a
// hold's details, and its buttons: its own, then Skip and Open.
function Card({ match: m, highlighted, agentFor, actions, onOpen, onToast, onChanged }: CardProps) {
  const act = useMutation((what: () => Promise<string>) => what())
  const run = (what: () => Promise<string>) => {
    void act.run(what).then((result) => {
      if (result.ok) {
        onChanged()

        if (result.data !== '') {
          onToast(result.data)
        }
      } else {
        onToast(result.error, 'error')
      }
    })
  }
  const hold = m.hold
  // A worktree with uncommitted changes holds the start whatever the hold's reason says.
  const held = hold === null ? null : m.blockedBy === 'dirty_worktree' ? { ...hold, reason: 'dirty' } : hold
  const name = itemText(m.item)

  return (
    <li
      data-match={m.id}
      data-highlighted={highlighted || undefined}
      aria-label={`${m.routine} on ${name}`}
      className="flex flex-col gap-2 rounded-lg border border-border bg-surface px-3 py-2.5 data-highlighted:border-accent/60 data-highlighted:bg-accent/10"
    >
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="shrink-0 font-mono text-[0.75rem] text-muted">{name}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium" title={m.item.title}>
          {m.item.title}
        </span>
        <span className="max-w-56 shrink-0 truncate text-xs text-muted" title={m.routine}>
          {m.routine}
        </span>
      </div>
      {m.words !== '' && <p className="text-sm text-text">{m.words}</p>}
      {held !== null && (m.blockedBy === 'hold' || m.blockedBy === 'dirty_worktree') && (
        <div className="flex flex-col gap-1.5 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-xs text-muted">
          <p className="font-medium text-text">{holdTitle(held)}</p>
          <HoldDetails hold={held} agentName={agentFor(m)} go="run it" />
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {actions(run, act.pending)}
        <button
          type="button"
          disabled={act.pending}
          onClick={() => {
            run(() => skipMatch(m.id).then(() => `Skipped it. ${m.routine} waits for the next time it fires.`))
          }}
          className={quietButtonClass}
        >
          {skipLabel(m)}
        </button>
        {(m.item.id > 0 || m.item.url !== '') && (
          <button
            type="button"
            aria-label={`Open ${name}`}
            title={m.item.title}
            onClick={() => {
              onOpen(m.item, m.routineId)
            }}
            className={quietButtonClass}
          >
            Open
          </button>
        )}
        {act.pending && <Spinner className="size-3.5" />}
      </div>
    </li>
  )
}

// Cloning is the clone a no-clone card asked to make before its match runs.
interface Cloning {
  match: AutopilotMatch
  target: CloneTarget
}

// NeedsYouSection is Activity's Needs you, above the history: the matches waiting for the
// user's OK (Run, which for a hold confirms exactly the hold shown, or Clone and run where
// the repository has no clone here), those that can't start until something on this Mac
// is fixed (Check again), each with Skip and Open, and a short list of those queued, which
// start by themselves. It shows nothing while nothing waits.
export function NeedsYouSection({
  matches,
  routine,
  error,
  readiness,
  highlight,
  agentFor,
  onOpen,
  onToast,
  onChanged,
}: NeedsYouSectionProps) {
  const listRef = useRef<HTMLDivElement>(null)
  const groups = needsYou(matches, routine)
  const [cloning, setCloning] = useState<Cloning | null>(null)
  const clone = useMutation(async (c: Cloning) => {
    await cloneRepo(c.match.item.id)
    await approveMatches([c.match.id])
  })
  const shown = [...groups.waiting, ...groups.blocked, ...groups.queued]
  const marked = highlight !== null && shown.some((m) => m.id === highlight.matchId) ? highlight : null

  useEffect(() => {
    if (marked === null) {
      return
    }

    const card = [...(listRef.current?.querySelectorAll('[data-match]') ?? [])].find(
      (el) => el.getAttribute('data-match') === String(marked.matchId),
    )

    card?.scrollIntoView({ block: 'center' })
  }, [marked])

  if (shown.length === 0 && error === '') {
    return null
  }

  const shared = { agentFor, onOpen, onToast, onChanged }

  // askClone reads where the repository would be cloned, and asks before cloning it; a
  // repository cloned since runs at once.
  const askClone = async (m: AutopilotMatch) => {
    const target = await attempt(cloneTarget(m.item.id))

    if (!target.ok) {
      onToast(target.error, 'error')

      return
    }

    if (target.data.cloned) {
      const approved = await attempt(approveMatches([m.id]))

      onChanged()
      onToast(approved.ok ? `${m.routine} runs once it can.` : approved.error, approved.ok ? undefined : 'error')

      return
    }

    if (target.data.taken) {
      onToast(`Something is already at ${target.data.dir}, so ${target.data.repo} can’t be cloned there.`, 'error')

      return
    }

    clone.reset()
    setCloning({ match: m, target: target.data })
  }

  const confirmClone = async (c: Cloning) => {
    const result = await clone.run(c)

    if (result.ok) {
      setCloning(null)
      onChanged()
      onToast(`Cloned ${c.target.repo}. ${c.match.routine} runs once it can.`)
    }
  }

  return (
    <div ref={listRef} className="flex flex-col gap-4">
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          Couldn’t read what waits for you: {error}
        </p>
      )}
      {(groups.waiting.length > 0 || groups.blocked.length > 0) && (
        <section aria-label="Needs you" className="flex flex-col gap-3">
          <h3 className="text-sm font-semibold">Needs you</h3>
          {groups.waiting.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <p className={captionClass}>Waiting for your OK</p>
              <ul aria-label="Waiting for your OK" className="flex flex-col gap-2">
                {groups.waiting.map((m) => (
                  <Card
                    key={m.id}
                    match={m}
                    highlighted={marked?.matchId === m.id}
                    {...shared}
                    actions={(run, pending) =>
                      m.blockedBy === 'no_clone' ? (
                        <button
                          type="button"
                          disabled={pending}
                          onClick={() => {
                            void askClone(m)
                          }}
                          className={primaryButtonClass}
                        >
                          Clone and run
                        </button>
                      ) : (
                        <button
                          type="button"
                          disabled={pending}
                          onClick={() => {
                            run(() => approveMatches([m.id]).then(() => `${m.routine} runs once it can.`))
                          }}
                          className={primaryButtonClass}
                        >
                          {runLabel(m)}
                        </button>
                      )
                    }
                  />
                ))}
              </ul>
            </div>
          )}
          {groups.blocked.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <p className={captionClass}>Can’t start yet</p>
              <ul aria-label="Can’t start yet" className="flex flex-col gap-2">
                {groups.blocked.map((m) => (
                  <Card
                    key={m.id}
                    match={m}
                    highlighted={marked?.matchId === m.id}
                    {...shared}
                    actions={() => (
                      <button
                        type="button"
                        disabled={readiness.checking}
                        title="Check this Mac again: it asks GitHub too, which costs a point"
                        onClick={readiness.check}
                        className={buttonClass}
                      >
                        {readiness.checking ? (
                          <Spinner className="size-3.5" />
                        ) : (
                          <RefreshIcon className="size-3.5 text-muted" />
                        )}
                        {readiness.checking ? 'Checking…' : 'Check again'}
                      </button>
                    )}
                  />
                ))}
              </ul>
              {readiness.error !== '' && (
                <p role="alert" className="flex items-center gap-1.5 text-xs text-danger">
                  <AlertIcon className="size-3.5 shrink-0" />
                  {readiness.error}
                </p>
              )}
            </div>
          )}
        </section>
      )}
      {groups.queued.length > 0 && (
        <section aria-label="Queued" className="flex flex-col gap-1.5">
          <h3 className="flex items-baseline gap-2 text-sm font-semibold">
            Queued
            <span className="text-xs font-normal text-muted">Each starts by itself once what it waits on frees.</span>
          </h3>
          <ul className="flex flex-col">
            {groups.queued.map((m) => (
              <li
                key={m.id}
                data-match={m.id}
                data-highlighted={marked?.matchId === m.id || undefined}
                className="flex min-w-0 items-center gap-3 rounded-md px-3 py-1 text-sm data-highlighted:bg-accent/10"
              >
                <ClockIcon className="size-3.5 shrink-0 text-muted" />
                <span className="shrink-0 font-mono text-[0.75rem] text-muted">{itemText(m.item)}</span>
                <span className="min-w-0 flex-1 truncate text-xs text-muted" title={m.item.title}>
                  {m.words}
                </span>
                <span className="hidden max-w-56 shrink-0 truncate text-xs text-muted lg:inline" title={m.routine}>
                  {m.routine}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {cloning !== null && (
        <ConfirmDialog
          title={`Clone ${cloning.target.repo}?`}
          confirmLabel="Clone and run"
          tone="primary"
          busy={clone.pending}
          onCancel={() => {
            setCloning(null)
          }}
          onConfirm={() => {
            void confirmClone(cloning)
          }}
        >
          <p>
            This runs{' '}
            <code className="font-mono text-text">{`gh repo clone ${cloning.target.repo} ${cloning.target.dir}`}</code>,
            then {cloning.match.routine} runs on {itemText(cloning.match.item)} once it can.
          </p>
          {clone.error !== '' && (
            <p role="alert" className="mt-2 text-xs text-danger">
              {clone.error}
            </p>
          )}
        </ConfirmDialog>
      )}
    </div>
  )
}
