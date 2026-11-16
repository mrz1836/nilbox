import { createContext, use, useId, useState } from 'react'

import { chipOf, effectState, effectVerb, inActivity, routineName, targetOf, type Chip } from '../../lib/autopilot'
import { formatAgo, formatFull } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import { usePopover } from '../../lib/usePopover'
import { useQuery } from '../../lib/useQuery'
import {
  autopilotItemHistory,
  undoAutopilot,
  type AutopilotMatch,
  type AutopilotTarget,
  type InboxItem,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { AutopilotIcon } from '../icons'
import { PopoverPanel } from '../PopoverPanel'
import { RelativeTime } from '../RelativeTime'

// AutopilotRows is what the rows need of Autopilot, which the window provides around the
// inbox, and the Autopilot tab around a routine's Watching list: each routine's name by ID,
// for the marks "Snoozed by ⚡ ...", the newest match a routine has on each item, by item,
// for its ⚡ chip, and onShow, which opens the Autopilot tab at a match in Activity.
export interface AutopilotRows {
  names: ReadonlyMap<number, string>
  chips: ReadonlyMap<number, AutopilotMatch>
  onShow?: ((target: AutopilotTarget) => void) | undefined
}

// AutopilotRowsContext holds the rows' Autopilot; without it a row says nothing of
// Autopilot.
export const AutopilotRowsContext = createContext<AutopilotRows | null>(null)

// useRoutineName names the routine with id among those the window lists, "a routine" for
// one it doesn't.
export function useRoutineName(id: number): string {
  const rows = use(AutopilotRowsContext)

  return routineName(rows?.names ?? new Map<number, string>(), id)
}

// chipTones color a chip by what it says: the accent for a change made, amber while it waits
// for the user, red for one that gave up or failed.
const chipTones: Readonly<Record<Chip['tone'], string>> = {
  muted: 'text-muted',
  accent: 'text-accent',
  warning: 'text-warning',
  danger: 'text-danger',
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
        <span className={`shrink-0 font-semibold ${chipTones[chip.tone]}`}>{chip.label}</span>
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
          <span>
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
              className={`${quietButtonClass} ml-auto`}
            >
              Undo
            </button>
          )}
        </p>
      ))}
      {undo.error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {undo.error}
        </p>
      )}
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
      <p role="alert" className="text-xs text-danger">
        {history.error}
      </p>
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

export interface AutopilotChipProps {
  item: InboxItem
  // align puts the panel's left edge under the chip's, by default, or its right edge.
  align?: 'left' | 'right'
}

// AutopilotChip marks an item a routine touched, after its session pill: "⚡ Snoozed", "⚡
// Would run", what the newest of the routines' matches on it says (chipOf). It opens the
// item's history with the routines in a panel. An item no routine touched shows nothing.
export function AutopilotChip({ item, align = 'left' }: AutopilotChipProps) {
  const id = useId()
  const rows = use(AutopilotRowsContext)
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef, close } = popover
  const match = rows?.chips.get(item.id)

  if (rows === null || match === undefined) {
    return null
  }

  const chip = chipOf(match)
  const what = `${chip.label} · ${match.routine}`

  return (
    <span className="relative inline-flex min-w-0 shrink-0">
      <button
        ref={triggerRef}
        type="button"
        aria-label={`Autopilot: ${what}. Show what the routines did with it`}
        title={what}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`inline-flex items-center gap-1 rounded-full px-1.5 py-px text-[0.6875rem] leading-4 font-medium transition-colors hover:bg-raised aria-expanded:ring-1 aria-expanded:ring-border ${chipTones[chip.tone]}`}
      >
        <AutopilotIcon className="size-3 shrink-0" />
        {chip.label}
      </button>
      <PopoverPanel
        state={popover}
        align={align}
        id={id}
        role="dialog"
        aria-label="Autopilot history"
        className="z-20 flex max-h-[calc(100vh-8rem)] w-[24rem] flex-col gap-1 overflow-y-auto p-3 text-left shadow-black/25"
      >
        <p className="flex items-center gap-1.5 text-xs font-semibold text-text">
          <AutopilotIcon className="size-3.5 text-accent" />
          What the routines did with it
        </p>
        <ItemHistory
          itemId={item.id}
          onShow={
            rows.onShow === undefined
              ? undefined
              : (target) => {
                  close()
                  rows.onShow?.(target)
                }
          }
        />
      </PopoverPanel>
    </span>
  )
}
