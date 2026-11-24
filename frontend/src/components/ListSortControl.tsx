import { useEffect, useId, useRef, useState } from 'react'

import { moveStatus, statusKey, type Status } from '../lib/statusOrder'
import { usePopover } from '../lib/usePopover'
import type { ListSort } from '../platform'
import { iconButtonClass, quietButtonClass, Segmented, type SegmentOption } from './controls'
import { ChevronDownIcon, SortIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'

// sortOptions are the sorts every list offers, in the order the control shows them.
const sortOptions: readonly SegmentOption<ListSort>[] = [
  { value: 'recent', label: 'Recent', title: 'Newest activity first' },
  { value: 'repo', label: 'Repo', title: 'By repository, A–Z, newest first in each' },
  { value: 'author', label: 'Author', title: 'By author, A–Z, newest first in each' },
]

// statusOption is the sort a list offers while it has project statuses to sort by.
const statusOption: SegmentOption<ListSort> = {
  value: 'status',
  label: 'Status',
  title: 'By project status, in your status order',
}

// moveClass is a status's move up and move down buttons: the small icon button's shape,
// but lit with the page's background on hover, since the row they sit in turns raised
// under the pointer and the shared raised hover would vanish on it. An arrow that can't
// move takes no pointer and fades as a disabled control does.
const moveClass =
  'flex size-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-background hover:text-text disabled:pointer-events-none disabled:opacity-disabled'

export interface ListSortControlProps {
  // title is the list's, which names the controls.
  title: string
  // sort is how the list is sorted.
  sort: ListSort
  onSortChange: (sort: ListSort) => void
  // statuses are the statuses found in the list, with their colors. Status is offered
  // only while there are some, or while the list is already sorted by status.
  statuses: readonly Status[]
  // order is the whole order the list sorts by (see statusOrder), and customized
  // reports whether the user has placed any status in it.
  order: readonly string[]
  customized: boolean
  // onOrderChange stores a new order; an empty one resets it.
  onOrderChange: (order: readonly string[]) => void
}

// Move is the last move, so focus can follow the status that moved.
interface Move {
  key: string
  by: -1 | 1
  seq: number
}

// ListSortControl is a list's choice of sort: newest activity first, by repository or
// author A to Z, or, while the list has project statuses, by status. While sorted by
// status, a button beside it opens the status order: the statuses found in the list, top
// ranked first, each moved up or down with its arrows. A move is stored at once, and Reset
// puts every status back where its name places it.
export function ListSortControl({
  title,
  sort,
  onSortChange,
  statuses,
  order,
  customized,
  onOrderChange,
}: ListSortControlProps) {
  const byStatus = sort === 'status'
  const options = statuses.length > 0 || byStatus ? [...sortOptions, statusOption] : sortOptions
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, triggerRef, panelRef } = popover
  const colors = new Map(statuses.map((s) => [statusKey(s.name), s.color]))
  const shown = order.filter((name) => colors.has(statusKey(name)))
  const [moved, setMoved] = useState<Move | null>(null)
  const [announcement, setAnnouncement] = useState('')
  const seq = useRef(0)

  // Focus follows the status that moved, onto the same arrow, or the other one once the
  // status reaches an end.
  useEffect(() => {
    if (moved === null) {
      return
    }

    const row = panelRef.current?.querySelector(`[data-status="${CSS.escape(moved.key)}"]`)
    const same = row?.querySelector<HTMLButtonElement>(`[data-move="${String(moved.by)}"]`)
    const other = row?.querySelector<HTMLButtonElement>(`[data-move="${String(-moved.by)}"]`)

    if (same?.disabled === false) {
      same.focus()
    } else {
      other?.focus()
    }
  }, [moved, panelRef])

  const move = (name: string, by: -1 | 1) => {
    const next = moveStatus(order, shown, name, by)
    const at = next.filter((status) => colors.has(statusKey(status))).findIndex((s) => statusKey(s) === statusKey(name))

    seq.current += 1
    setMoved({ key: statusKey(name), by, seq: seq.current })
    setAnnouncement(`${name} moved to ${String(at + 1)} of ${String(shown.length)}`)
    onOrderChange(next)
  }

  return (
    <div className="relative flex shrink-0 items-center gap-1">
      <Segmented<ListSort>
        label={`Sort ${title} by`}
        size="sm"
        value={sort}
        options={options}
        onChange={onSortChange}
      />
      {byStatus && (
        <button
          ref={triggerRef}
          type="button"
          aria-label="Status order"
          title="Status order"
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-controls={open ? id : undefined}
          onClick={toggle}
          className={`${iconButtonClass} aria-expanded:bg-raised aria-expanded:text-text`}
        >
          <SortIcon className="size-4" />
        </button>
      )}
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="layer-popover w-72 outline-none"
      >
        <div className="flex items-center justify-between gap-2 px-3 pt-3">
          <h3 id={`${id}-title`} className="text-sm font-semibold">
            Status order
          </h3>
          <button
            type="button"
            disabled={!customized}
            title="Put every status back where its name places it"
            onClick={() => {
              setAnnouncement('Status order reset')
              onOrderChange([])
            }}
            className={quietButtonClass}
          >
            Reset
          </button>
        </div>
        <p className="px-3 pt-1 text-xs leading-relaxed text-muted">
          {title} lists issues by their project status, top first. Pinned items stay on top, and issues without a status
          come last.
        </p>
        <ol aria-label="Statuses, top ranked first" className="max-h-72 overflow-y-auto p-1.5">
          {shown.map((name, i) => {
            const color = colors.get(statusKey(name)) ?? ''

            return (
              <li
                key={statusKey(name)}
                data-status={statusKey(name)}
                className="flex items-center gap-2 rounded-md py-1 pr-1 pl-1.5 hover:bg-raised"
              >
                <span className="w-4 shrink-0 text-right text-xs text-muted tabular-nums">{i + 1}</span>
                <span
                  aria-hidden="true"
                  className={`size-2 shrink-0 rounded-full ${color === '' ? 'bg-muted' : ''}`}
                  style={color === '' ? undefined : { backgroundColor: color }}
                />
                <span className="min-w-0 flex-1 truncate text-sm">{name}</span>
                <button
                  type="button"
                  data-move="-1"
                  aria-label={`Move ${name} up`}
                  title="Move up"
                  disabled={i === 0}
                  onClick={() => {
                    move(name, -1)
                  }}
                  className={moveClass}
                >
                  <ChevronDownIcon className="size-3.5 rotate-180" />
                </button>
                <button
                  type="button"
                  data-move="1"
                  aria-label={`Move ${name} down`}
                  title="Move down"
                  disabled={i === shown.length - 1}
                  onClick={() => {
                    move(name, 1)
                  }}
                  className={moveClass}
                >
                  <ChevronDownIcon className="size-3.5" />
                </button>
              </li>
            )
          })}
        </ol>
        <p role="status" className="sr-only">
          {announcement}
        </p>
      </PopoverPanel>
    </div>
  )
}
