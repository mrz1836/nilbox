import { useCallback, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'

import { formatAgo } from '../lib/age'
import { errorMessage } from '../lib/format'
import { formatWhen, isDismissed, triageList, type TriageTab } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxItem, InboxList, InboxSnapshot } from '../platform'
import { ArchiveIcon, ChevronDownIcon, ClockIcon } from './icons'
import { ItemRow, type LaunchOptions } from './ItemRow'
import { Toast, type ToastMessage } from './Toast'
import type { TriageActions } from './TriageControls'

export type { LaunchOptions } from './ItemRow'

export interface InboxViewProps {
  // inbox is the window's shared inbox state (useInbox).
  inbox: InboxState
  // launch is null until the settings and profiles have loaded; rows show no launch
  // controls until then.
  launch: LaunchOptions | null
  // snoozeDays is how long a row's Snooze button hides an item.
  snoozeDays: number
  // onSnoozeSettings opens the setting for the Snooze button's length.
  onSnoozeSettings: () => void
  // collapsed holds the list names of the collapsed lists. It is null until the stored
  // state loads; the lists wait for it, so a collapsed list never shows open first.
  collapsed: ReadonlySet<string> | null
  // onCollapse collapses or opens a list.
  onCollapse: (section: ListKey, collapsed: boolean) => void
}

// ListKey names an inbox list, as the snapshot does.
type ListKey = 'reviewRequested' | 'authored' | 'assigned'

const sections: readonly { key: ListKey; title: string }[] = [
  { key: 'reviewRequested', title: 'Review requested' },
  { key: 'authored', title: 'Authored' },
  { key: 'assigned', title: 'Assigned' },
]

// tabOrder is the order of a list's tabs, for the arrow keys.
const tabOrder: readonly TriageTab[] = ['inbox', 'snoozed', 'dismissed']

// countText is a tab's size, as "N of M" when GitHub matched more than it returned.
function countText(shown: number, total: number): string {
  return total > shown ? `${String(shown)} of ${String(total)}` : String(shown)
}

// emptyText is what an empty tab says.
function emptyText(tab: TriageTab, title: string): string {
  switch (tab) {
    case 'snoozed':
      return `Nothing snoozed. A snoozed item comes back to ${title} on its own when its time is up.`
    case 'dismissed':
      return 'Nothing dismissed. A dismissed item stays here until you restore it.'
    default:
      return 'Nothing here.'
  }
}

// firstMessage returns the first non-empty message.
function firstMessage(...messages: string[]): string {
  return messages.find((m) => m !== '') ?? ''
}

// findItem looks an item up in a snapshot's lists.
function findItem(snapshot: InboxSnapshot, id: number): InboxItem | undefined {
  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    const found = list.items?.find((item) => item.id === id)

    if (found !== undefined) {
      return found
    }
  }

  return undefined
}

interface TabProps {
  id: string
  panelId: string
  selected: boolean
  label: string
  count: string
  icon?: ReactNode
  // strong sets the list's own tab in the weight of a heading.
  strong?: boolean
  onSelect: () => void
  onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => void
}

// Tab is one of a list's tabs: its name and, when there is anything in it, its count.
function Tab({ id, panelId, selected, label, count, icon, strong = false, onSelect, onKeyDown }: TabProps) {
  return (
    <button
      type="button"
      role="tab"
      id={id}
      aria-label={count === '' ? label : `${label} ${count}`}
      aria-selected={selected}
      aria-controls={panelId}
      tabIndex={selected ? 0 : -1}
      onClick={onSelect}
      onKeyDown={onKeyDown}
      className="-mb-px flex items-center gap-1.5 border-b-2 border-transparent px-2.5 pt-2.5 pb-2 text-sm text-muted transition-colors hover:text-text aria-selected:border-accent aria-selected:text-text"
    >
      {icon}
      <span className={strong ? 'font-semibold' : 'font-medium'}>{label}</span>
      {count !== '' && (
        <span
          className={`rounded-full px-1.5 text-[11px] leading-[18px] font-medium tabular-nums ${selected ? 'bg-accent/15 text-text' : 'bg-raised text-muted'}`}
        >
          {count}
        </span>
      )}
    </button>
  )
}

interface ListSectionProps {
  title: string
  list: InboxList
  now: Date
  launch: LaunchOptions | null
  triage: TriageActions
  collapsed: boolean
  onCollapse: (collapsed: boolean) => void
}

// ListSection is one inbox list as tabs: the list itself, then what was snoozed and
// what was dismissed from it, each with its count. The toggle at the right of its header
// collapses it to just the tabs, which keep their counts.
function ListSection({ title, list, now, launch, triage, collapsed, onCollapse }: ListSectionProps) {
  const id = useId()
  const [tab, setTab] = useState<TriageTab>('inbox')
  const triaged = useMemo(() => triageList(list, now), [list, now])
  const items = triaged[tab]
  const tabId = (t: TriageTab) => `${id}-tab-${t}`
  const panelId = `${id}-panel`

  // select shows a tab; choosing a tab of a collapsed list opens the list to show it.
  const select = (t: TriageTab) => {
    setTab(t)

    if (collapsed) {
      onCollapse(false)
    }
  }

  // The arrow keys, Home, and End move between the tabs and select the one they reach.
  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const at = tabOrder.indexOf(tab)
    const moves: Readonly<Record<string, number>> = {
      ArrowRight: (at + 1) % tabOrder.length,
      ArrowLeft: (at - 1 + tabOrder.length) % tabOrder.length,
      Home: 0,
      End: tabOrder.length - 1,
    }
    const next = moves[e.key]

    if (next === undefined) {
      return
    }

    e.preventDefault()

    const target = tabOrder[next] ?? 'inbox'
    select(target)
    document.getElementById(tabId(target))?.focus()
  }

  const hiddenCount = (n: number) => (n > 0 ? String(n) : '')

  return (
    <section aria-label={title} className="rounded-lg border border-border bg-surface">
      <div className={`flex items-center gap-1 px-1.5 ${collapsed ? '' : 'border-b border-border'}`}>
        <div role="tablist" aria-label={title} className="flex flex-1 items-center gap-1">
          <Tab
            id={tabId('inbox')}
            panelId={panelId}
            selected={tab === 'inbox'}
            label={title}
            count={countText(triaged.inbox.length, triaged.total)}
            strong
            onSelect={() => {
              select('inbox')
            }}
            onKeyDown={onKeyDown}
          />
          <Tab
            id={tabId('snoozed')}
            panelId={panelId}
            selected={tab === 'snoozed'}
            label="Snoozed"
            count={hiddenCount(triaged.snoozed.length)}
            icon={<ClockIcon className="size-3.5" />}
            onSelect={() => {
              select('snoozed')
            }}
            onKeyDown={onKeyDown}
          />
          <Tab
            id={tabId('dismissed')}
            panelId={panelId}
            selected={tab === 'dismissed'}
            label="Dismissed"
            count={hiddenCount(triaged.dismissed.length)}
            icon={<ArchiveIcon className="size-3.5" />}
            onSelect={() => {
              select('dismissed')
            }}
            onKeyDown={onKeyDown}
          />
        </div>
        <button
          type="button"
          aria-expanded={!collapsed}
          aria-controls={panelId}
          aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${title}`}
          title={collapsed ? 'Expand' : 'Collapse'}
          onClick={() => {
            onCollapse(!collapsed)
          }}
          className="mr-1 rounded-md p-1 text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <ChevronDownIcon className={`size-4 transition-transform ${collapsed ? '-rotate-90' : ''}`} />
        </button>
      </div>
      <div role="tabpanel" id={panelId} aria-labelledby={tabId(tab)} hidden={collapsed}>
        {items.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">{emptyText(tab, title)}</p>
        ) : (
          <ul>
            {items.map((item) => (
              <ItemRow key={item.id} item={item} now={now} launch={launch} tab={tab} triage={triage} />
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

// InboxView shows the three inbox lists across the full width, with a refresh control and
// the time of the last refresh. Each list has tabs for what was snoozed or dismissed from
// it and collapses from its header, and each triage action is confirmed by a toast with an
// Undo. The account and the API budget live in the toolbar.
export function InboxView({ inbox, launch, snoozeDays, onSnoozeSettings, collapsed, onCollapse }: InboxViewProps) {
  const { snapshot, now, loadError, refreshError, refreshing, refresh, dismiss, snooze, restore, refreshItem } = inbox
  const error = firstMessage(refreshError, snapshot?.lastError ?? '', loadError)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const toastCount = useRef(0)

  const showToast = useCallback((message: Omit<ToastMessage, 'id'>) => {
    toastCount.current += 1
    setToast({ ...message, id: toastCount.current })
  }, [])

  const closeToast = useCallback(() => {
    setToast(null)
  }, [])

  const triage = useMemo<TriageActions>(() => {
    const failed = (what: string, item: InboxItem) => (err: unknown) => {
      showToast({ text: `Could not ${what} “${item.title}”`, detail: errorMessage(err), tone: 'error' })
    }

    // undo puts an item back the way it was before it was snoozed or dismissed.
    const undo = (item: InboxItem) => () => {
      closeToast()
      restore(item.id).catch(failed('undo the change to', item))
    }

    return {
      snoozeDays,
      onDismiss: (item) => {
        dismiss(item.id)
          .then(() => {
            showToast({ text: 'Dismissed', detail: item.title, undo: undo(item) })
          })
          .catch(failed('dismiss', item))
      },
      onSnooze: (item, days) => {
        snooze(item.id, days)
          .then((next) => {
            const until = findItem(next, item.id)?.snoozedUntil ?? ''
            const text = until === '' ? 'Snoozed' : `Snoozed until ${formatWhen(now, new Date(until))}`

            showToast({ text, detail: item.title, undo: undo(item) })
          })
          .catch(failed('snooze', item))
      },
      onRestore: (item) => {
        restore(item.id)
          .then(() => {
            showToast({ text: isDismissed(item) ? 'Restored' : 'Unsnoozed', detail: item.title })
          })
          .catch(failed('restore', item))
      },
      // A refreshed item that is still open updates in place; one that is not leaves the
      // inbox, which the toast explains.
      onRefresh: (item) =>
        refreshItem(item.id)
          .then((next) => {
            if (findItem(next, item.id) === undefined) {
              showToast({ text: 'No longer open on GitHub', detail: item.title })
            }
          })
          .catch(failed('refresh', item)),
      onChangeDefault: onSnoozeSettings,
    }
  }, [snoozeDays, now, dismiss, snooze, restore, refreshItem, showToast, closeToast, onSnoozeSettings])

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex items-center gap-3 text-sm">
        <button
          type="button"
          disabled={refreshing}
          onClick={refresh}
          className="rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised disabled:opacity-60"
        >
          {refreshing ? 'Refreshing…' : 'Refresh'}
        </button>
        <span className="text-muted">
          {snapshot === null || snapshot.lastRefreshedAt === '' ? (
            'Not refreshed yet'
          ) : (
            <>
              Last refreshed <time dateTime={snapshot.lastRefreshedAt}>{formatAgo(now, snapshot.lastRefreshedAt)}</time>
            </>
          )}
        </span>
      </div>
      {error !== '' && (
        <p role="alert" className="rounded-md border border-danger px-3 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      {snapshot !== null && snapshot.scopeWarning !== '' && (
        <p className="rounded-md border border-warning px-3 py-2 text-sm text-warning">{snapshot.scopeWarning}</p>
      )}
      {snapshot === null || collapsed === null
        ? loadError === '' && <p className="text-sm text-muted">Loading the inbox…</p>
        : sections.map(({ key, title }) => (
            <ListSection
              key={key}
              title={title}
              list={snapshot[key]}
              now={now}
              launch={launch}
              triage={triage}
              collapsed={collapsed.has(key)}
              onCollapse={(on) => {
                onCollapse(key, on)
              }}
            />
          ))}
      <Toast toast={toast} onClose={closeToast} />
    </div>
  )
}
