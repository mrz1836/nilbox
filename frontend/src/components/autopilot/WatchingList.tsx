import { use, useEffect, useEffectEvent, useRef, useState } from 'react'

import { inChunks } from '../../lib/autopilot'
import { worktreeOf } from '../../lib/itemWorktrees'
import { sessionsOf } from '../../lib/sessions'
import { useAutopilotChips, useOnAutopilot } from '../../lib/useAutopilot'
import { useQuery } from '../../lib/useQuery'
import { itemsByID, on, routineWatching, type InboxItem } from '../../platform'
import { ItemRow } from '../ItemRow'
import { AutopilotRowsContext } from './AutopilotChip'
import { useSheetHost, type RowsKit } from './useSheetHost'

export type { RowsKit } from './useSheetHost'

// readItems reads the stored items among ids, as many at a time as the backend reads
// (inChunks), so a routine that watches more never loses the rest, newest activity first.
export async function readItems(ids: readonly number[]): Promise<InboxItem[]> {
  const pages = await Promise.all(inChunks(ids).map((chunk) => itemsByID(chunk)))

  return pages.flat().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
}

export interface WatchingListProps {
  routineId: number
  kit: RowsKit
  // highlight is the item Activity's Open asked to show here, which the list marks and
  // scrolls into view once, then hands back (onHighlighted) so it never shows again on its
  // own; seq numbers the asks.
  highlight: { id: number; seq: number } | null
  onHighlighted: () => void
}

// WatchingList is a routine's Watching list: the items its watch finds now (those in the
// inbox lists it names and those its searches return, as the last refresh read them),
// newest activity first, as the inbox's own rows, with their signals, launch button, ⚡
// chip, and ⋯ menu. A row's sheet opens on a click and steps through the list. This is
// where an item that is in no inbox list shows up. The tab keys it by routine, so a list
// shows only its own routine's rows.
export function WatchingList({ routineId, kit, highlight, onHighlighted }: WatchingListProps) {
  const rows = use(AutopilotRowsContext)
  const ids = useQuery(() => routineWatching(routineId), [routineId])
  const key = (ids.data ?? []).join(',')
  const items = useQuery(() => readItems(key === '' ? [] : key.split(',').map(Number)), [key], {
    enabled: ids.data !== undefined,
  })
  const chips = useAutopilotChips(key)
  const list = items.data ?? []
  const host = useSheetHost({ kit, items: list, listTitle: 'Watching' })
  const listRef = useRef<HTMLUListElement>(null)
  // marked is the row Activity's Open last showed here, marked for as long as the list
  // stays, as the inbox marks a row a notification showed.
  const [marked, setMarked] = useState(0)

  useOnAutopilot(() => {
    void ids.refetch()
    void items.refetch()
  })

  // The rows' triage changes the stored items, which the inbox says it changed.
  const readAgain = useEffectEvent(() => {
    void items.refetch()
  })

  useEffect(
    () =>
      on('inbox:updated', () => {
        readAgain()
      }),
    [],
  )

  const listed = items.data !== undefined
  const asked = highlight !== null && listed && list.some((i) => i.id === highlight.id) ? highlight.id : 0
  const handBack = useEffectEvent(() => {
    onHighlighted()
  })

  if (asked !== 0 && asked !== marked) {
    setMarked(asked)
  }

  // Activity's Open marks its row and scrolls to it once, then lets the ask go.
  useEffect(() => {
    if (asked === 0) {
      return
    }

    const row = [...(listRef.current?.querySelectorAll('[data-item-id]') ?? [])].find(
      (el) => el.getAttribute('data-item-id') === String(asked),
    )

    row?.scrollIntoView({ block: 'center' })
    handBack()
  }, [asked])

  if (ids.error !== '' || items.error !== '') {
    return (
      <p role="alert" className="px-4 py-3 text-sm text-danger">
        {ids.error !== '' ? ids.error : items.error}
      </p>
    )
  }

  if (!listed) {
    return (
      <p role="status" className="px-4 py-3 text-sm text-muted">
        Reading what it watches…
      </p>
    )
  }

  return (
    <>
      {list.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">
          Nothing to watch right now. Its lists and searches find nothing open, as the last refresh read them.
        </p>
      ) : (
        <AutopilotRowsContext value={{ names: rows?.names ?? new Map(), chips, onShow: rows?.onShow }}>
          <ul ref={listRef} aria-label="Watching" className="rounded-lg border border-border bg-surface">
            {list.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                launch={kit.launch}
                resolution={host.resolutionOf(item.id)}
                worktree={worktreeOf(kit.worktrees, item)}
                sessions={sessionsOf(kit.sessions, item)}
                tab={host.tabOf(item)}
                triage={host.triage}
                edits={host.edits}
                onFilesSettings={kit.onFilesSettings}
                highlighted={item.id === marked}
                onOpen={host.open}
              />
            ))}
          </ul>
        </AutopilotRowsContext>
      )}
      {host.node}
    </>
  )
}
