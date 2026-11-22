import { createContext, use, useId, useState } from 'react'

import {
  chipOf,
  chipView,
  effectState,
  effectVerb,
  inActivity,
  plannedText,
  routineName,
  routineTarget,
  targetOf,
  upcomingChipText,
  upcomingLine,
  upcomingTitle,
} from '../../lib/autopilot'
import { terminalLabel } from '../../lib/terminals'
import { formatAgo, formatFull } from '../../lib/time'
import { toneText } from '../../lib/tone'
import { useMutation } from '../../lib/useMutation'
import { useNowAs } from '../../lib/useNow'
import { usePopover } from '../../lib/usePopover'
import { useQuery } from '../../lib/useQuery'
import {
  autopilotItemHistory,
  leaveItemAlone,
  letRoutineActAgain,
  undoAutopilot,
  type AutopilotMatch,
  type AutopilotTarget,
  type AutopilotUpcoming,
  type InboxItem,
  type TerminalID,
} from '../../platform'
import { ErrorText, quietButtonClass } from '../controls'
import { AutopilotIcon } from '../icons'
import { PopoverPanel } from '../PopoverPanel'
import { RelativeTime } from '../RelativeTime'

// AutopilotRows is what the rows need of Autopilot, which the window provides around the
// inbox, and the Autopilot tab around a routine's Watching list: each routine's name by ID,
// for the marks "Snoozed by ⚡ ...", the newest match a routine has on each item, by item,
// for its ⚡ chip, the routines that will run on each item by themselves, which the chip
// says before they do, onShow, which opens the Autopilot tab at a match in Activity, at a
// routine, or at a run's view, and onWatch, which opens the run view of an agent's
// background run by its session, as the session panel's and the launch button's Watch do.
export interface AutopilotRows {
  names: ReadonlyMap<number, string>
  chips: ReadonlyMap<number, AutopilotMatch>
  upcoming?: ReadonlyMap<number, readonly AutopilotUpcoming[]> | undefined
  onShow?: ((target: AutopilotTarget) => void) | undefined
  onWatch?: ((sessionId: string) => void) | undefined
  // terminal is the terminal chosen for launches, which Take over opens a run's
  // conversation in, or null until the settings load.
  terminal?: TerminalID | null | undefined
}

// useWatch is what opens a background run's view by its session, or undefined without the
// rows' Autopilot, as while Autopilot (beta) is off.
export function useWatch(): ((sessionId: string) => void) | undefined {
  return use(AutopilotRowsContext)?.onWatch
}

// useTakeOverTerminal names the terminal Take over opens a run's conversation in: the one
// chosen for launches, or "your terminal" while it isn't known.
export function useTakeOverTerminal(): string {
  const terminal = use(AutopilotRowsContext)?.terminal

  return terminalLabel(terminal, 'your terminal')
}

// AutopilotRowsContext holds the rows' Autopilot; without it, as while Autopilot (beta) is
// off, a row says nothing of Autopilot: no chip, and no routine's mark.
export const AutopilotRowsContext = createContext<AutopilotRows | null>(null)

// useMarkedBy is the routine with id that set an item aside or pinned it, as a row marks
// it ("Snoozed by ⚡ ..."): 0, so the row marks none, without the rows' Autopilot.
export function useMarkedBy(id: number): number {
  return use(AutopilotRowsContext) === null ? 0 : id
}

// useRoutineName names the routine with id among those the window lists, "a routine" for
// one it doesn't.
export function useRoutineName(id: number): string {
  const rows = use(AutopilotRowsContext)

  return routineName(rows?.names ?? new Map<number, string>(), id)
}

interface HistoryEntryProps {
  match: AutopilotMatch
  onShow: ((target: AutopilotTarget) => void) | undefined
  onUndone: (words: string) => void
}

// HistoryEntry is one of an item's matches in its history: the routine, what it did and
// when, each local step's change with where it stands and its Undo, and a way to see it in
// Activity.
function HistoryEntry({ match, onShow, onUndone }: HistoryEntryProps) {
  const chip = chipOf(match)
  const undo = useMutation((ids: number[]) => undoAutopilot(ids))

  return (
    <li className="flex flex-col gap-1 border-b border-border py-2 last:border-b-0">
      <p className="flex min-w-0 items-baseline gap-2 text-xs">
        <span className={`shrink-0 font-semibold ${toneText[chip.tone]}`}>{chip.label}</span>
        <span className="min-w-0 truncate font-medium text-text" title={match.routine}>
          {match.routine}
        </span>
        <span className="ml-auto shrink-0 text-muted" title={formatFull(match.createdAt)}>
          <RelativeTime iso={match.createdAt} show={formatAgo} />
        </span>
      </p>
      {match.words !== '' && <p className="text-xs text-muted">{match.words}</p>}
      {match.effects.map((effect) => (
        <p key={effect.id} className="flex items-center gap-2 text-xs text-muted">
          <span className="min-w-0">
            {effectVerb(effect.action)} · {effectState(effect)}
          </span>
          {effect.canUndo && (
            <button
              type="button"
              disabled={undo.pending}
              onClick={() => {
                void undo.run([effect.id]).then((result) => {
                  if (result.ok) {
                    onUndone(result.data.words)
                  }
                })
              }}
              className={`${quietButtonClass} ml-auto shrink-0`}
            >
              Undo
            </button>
          )}
        </p>
      ))}
      <ErrorText error={undo.error} />
      {onShow !== undefined && inActivity(match) && (
        <button
          type="button"
          onClick={() => {
            onShow(targetOf(match))
          }}
          className={`${quietButtonClass} self-start`}
        >
          Show in Activity
        </button>
      )}
    </li>
  )
}

// ItemHistory is an item's history with the routines, newest first, read as the chip's
// panel opens: what each did, and Undo for a change still standing.
function ItemHistory({ itemId, onShow }: { itemId: number; onShow: AutopilotRows['onShow'] }) {
  const history = useQuery(() => autopilotItemHistory(itemId), [itemId])
  const [told, setTold] = useState('')

  if (history.data === undefined) {
    return history.error === '' ? (
      <p role="status" className="text-xs text-muted">
        Reading its history…
      </p>
    ) : (
      <ErrorText error={history.error} />
    )
  }

  return (
    <>
      {told !== '' && (
        <p role="status" className="text-xs text-text">
          {told}
        </p>
      )}
      <ul aria-label="What the routines did" className="flex flex-col">
        {history.data.matches.map((match) => (
          <HistoryEntry
            key={match.id}
            match={match}
            onShow={onShow}
            onUndone={(words) => {
              setTold(words)
              void history.refetch()
            }}
          />
        ))}
      </ul>
    </>
  )
}

// noUpcoming is an item no routine will run on, shared so it stays the same.
const noUpcoming: readonly AutopilotUpcoming[] = []

interface UpNextEntryProps {
  itemId: number
  upcoming: AutopilotUpcoming
  onShow: AutopilotRows['onShow']
}

// UpNextEntry is one routine that will run on the item: its name, how and when it runs,
// what it will do, and the ways to see it (Open routine) or stop it running there (Leave
// this one alone), which turns to Undo once it does, the line staying, so the user sees
// what changed and can take it back.
function UpNextEntry({ itemId, upcoming: u, onShow }: UpNextEntryProps) {
  const leave = useMutation(() => leaveItemAlone(u.routineId, itemId))
  const back = useMutation(() => letRoutineActAgain(u.routineId, itemId))
  const [left, setLeft] = useState(false)
  const line = useNowAs((now) => upcomingLine(u, now))
  const planned = plannedText(u.steps)
  const busy = leave.pending || back.pending

  return (
    <li className="flex flex-col gap-1 border-b border-border py-2 last:border-b-0">
      <p className="truncate text-xs font-medium text-text" title={u.routine}>
        {u.routine}
      </p>
      {left ? (
        <p role="status" className="text-xs text-muted">
          It leaves this one alone now, and never runs on it until you let it act again.
        </p>
      ) : (
        <>
          <p className="text-xs text-text">{line}</p>
          {planned !== '' && <p className="text-xs text-muted">{`Then: ${planned}`}</p>}
        </>
      )}
      <ErrorText error={leave.error || back.error} />
      <div className="flex flex-wrap items-center gap-1">
        {left ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              void back.run().then((result) => {
                if (result.ok) {
                  setLeft(false)
                }
              })
            }}
            className={quietButtonClass}
          >
            Undo
          </button>
        ) : (
          <button
            type="button"
            disabled={busy}
            title="It won't run on this item, now or later, until you let it act again from Activity"
            onClick={() => {
              void leave.run().then((result) => {
                if (result.ok) {
                  setLeft(true)
                }
              })
            }}
            className={quietButtonClass}
          >
            Leave this one alone
          </button>
        )}
        {onShow !== undefined && (
          <button
            type="button"
            onClick={() => {
              onShow(routineTarget(u.routineId))
            }}
            className={quietButtonClass}
          >
            Open routine
          </button>
        )}
      </div>
    </li>
  )
}

// UpNext is what will run on the item by itself, routine by routine, as the chip's panel
// shows it first.
function UpNext({
  itemId,
  list,
  onShow,
}: {
  itemId: number
  list: readonly AutopilotUpcoming[]
  onShow: AutopilotRows['onShow']
}) {
  return (
    <section aria-label="Up next" className="flex flex-col">
      <p className="flex items-center gap-1.5 text-xs font-semibold text-text">
        <AutopilotIcon className="size-3.5 shrink-0 text-accent" />
        Up next
      </p>
      <ul aria-label="What will run on it" className="flex flex-col">
        {list.map((u) => (
          <UpNextEntry key={u.routineId} itemId={itemId} upcoming={u} onShow={onShow} />
        ))}
      </ul>
    </section>
  )
}

// UpcomingLabel is what the chip says of the routine that runs next, kept current as the
// clock moves on ("Runs in 2m" counts down), and nothing in a list narrower than 48rem.
function UpcomingLabel({ upcoming }: { upcoming: AutopilotUpcoming }) {
  const text = useNowAs((now) => upcomingChipText(upcoming, now))

  return <span className="@max-3xl:hidden">{text}</span>
}

export interface AutopilotChipProps {
  item: InboxItem
  // align puts the panel's left edge under the chip's, by default, or its right edge.
  align?: 'left' | 'right'
}

// AutopilotChip marks an item, on its card's second line, with what Autopilot does there,
// now, next, or before (chipView): a routine's match while it is open (waiting on the user
// or a limit, or at work), "⚡ Needs you"; else the routines that will run on it by
// themselves, dashed, as what is still to come, saying when ("⚡ Runs when its checks pass",
// "⚡ Runs in 2m", counting down); else what the newest of the routines' matches on it says
// (chipOf), "⚡ Snoozed". In a list narrower than 48rem, the ⚡ alone. It opens a panel:
// Up next, what will run, each with Open routine and Leave this one alone; then the item's
// history with the routines. The panel keeps Up next as it opened while it stays open, so a
// routine told to leave the item alone stays there with Undo rather than vanishing, and
// wraps its own text: the card's line, which it opens in, doesn't wrap, and that would
// carry into the panel and run its sentences past its edge. The panel is never wider than
// the window. An item nothing touched, and nothing will, shows nothing.
export function AutopilotChip({ item, align = 'left' }: AutopilotChipProps) {
  const id = useId()
  const rows = use(AutopilotRowsContext)
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef, close } = popover
  const [opened, setOpened] = useState(noUpcoming)
  const live = rows?.upcoming?.get(item.id) ?? noUpcoming
  const match = rows?.chips.get(item.id)
  const next = open ? opened : live
  const view = chipView(match, next)
  const title = useNowAs((now) => (view?.kind === 'upcoming' ? upcomingTitle(view.upcoming, now) : ''))

  if (rows === null || view === null) {
    return null
  }

  const onShow =
    rows.onShow === undefined
      ? undefined
      : (target: AutopilotTarget) => {
          close()
          rows.onShow?.(target)
        }
  // chip is what the chip says of a match; null when it says what runs next.
  const chip = view.kind === 'match' ? chipOf(view.match) : null
  const upcoming = chip === null
  const what = view.kind === 'match' && chip !== null ? `${chip.label} · ${view.match.routine}` : title

  return (
    <span className="relative inline-flex min-w-0 shrink-0">
      <button
        ref={triggerRef}
        type="button"
        aria-label={
          upcoming
            ? `Autopilot: ${what}. Show what runs next`
            : `Autopilot: ${what}. Show what the routines did with it`
        }
        title={what}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        data-upcoming={upcoming ? '' : undefined}
        onClick={() => {
          if (!open) {
            setOpened(live)
          }

          toggle()
        }}
        className={`inline-flex items-center gap-1 rounded-full px-1.5 py-px text-caption leading-4 font-medium transition-colors ${
          upcoming
            ? 'text-accent outline-1 -outline-offset-1 outline-accent/55 outline-dashed hover:bg-accent/10 aria-expanded:outline-solid'
            : `hover:bg-raised aria-expanded:ring-1 aria-expanded:ring-border ${toneText[chip.tone]}`
        }`}
      >
        <AutopilotIcon className="size-3 shrink-0" />
        {view.kind === 'upcoming' ? (
          <UpcomingLabel upcoming={view.next} />
        ) : (
          <span className="@max-3xl:hidden">{chip?.label}</span>
        )}
      </button>
      <PopoverPanel
        state={popover}
        align={align}
        elevation="card"
        id={id}
        role="dialog"
        aria-label={next.length > 0 ? 'Autopilot' : 'Autopilot history'}
        className="layer-popover flex max-h-[calc(100vh-8rem)] w-[24rem] max-w-[calc(100vw-2rem)] flex-col gap-2 overflow-y-auto p-3 text-left break-words whitespace-normal"
      >
        {next.length > 0 && <UpNext itemId={item.id} list={next} onShow={onShow} />}
        {match !== undefined && (
          <section aria-label="History" className="flex flex-col gap-1">
            <p className="flex items-center gap-1.5 text-xs font-semibold text-text">
              <AutopilotIcon className="size-3.5 shrink-0 text-accent" />
              What the routines did with it
            </p>
            <ItemHistory itemId={item.id} onShow={onShow} />
          </section>
        )}
      </PopoverPanel>
    </span>
  )
}
