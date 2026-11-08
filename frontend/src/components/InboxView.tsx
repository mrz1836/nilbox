import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'

import { formatAgo } from '../lib/age'
import { isInboxNil } from '../lib/nil'
import { prefersReducedMotion } from '../lib/motion'
import { findItem, formatWhen, inboxCount, isDismissed, tabOf, triageList, type TriageTab } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxItem, InboxList, ListKey } from '../platform'
import { ArchiveIcon, ChevronDownIcon, ClockIcon } from './icons'
import { InboxNil } from './InboxNil'
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
  // onFilesSettings opens Settings → Sensitive files.
  onFilesSettings?: () => void
  // collapsed holds the list names of the collapsed lists. It is null until the stored
  // state loads; the lists wait for it, so a collapsed list never shows open first.
  collapsed: ReadonlySet<ListKey> | null
  // onCollapse collapses or opens a list.
  onCollapse: (section: ListKey, collapsed: boolean) => void
  // reveal is the item the menu bar or a notification last asked to show, or null.
  reveal?: Reveal | null
}

// Reveal asks the inbox to show an item: its list opens at the item's tab, and its row
// scrolls into view, highlighted for a moment. seq tells one request from the next, so
// asking for the same item again shows it again.
export interface Reveal {
  id: number
  seq: number
}

// highlightMs is how long a revealed row stays highlighted.
const highlightMs = 2400

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
  onFilesSettings?: (() => void) | undefined
  // reveal is an item in this list to show, or null.
  reveal: Reveal | null
}

// ListSection is one inbox list as tabs: the list itself, then what was snoozed and
// what was dismissed from it, each with its count. The toggle at the right of its header
// collapses it to just the tabs, which keep their counts.
function ListSection({
  title,
  list,
  now,
  launch,
  triage,
  collapsed,
  onCollapse,
  onFilesSettings,
  reveal,
}: ListSectionProps) {
  const id = useId()
  const [tab, setTab] = useState<TriageTab>('inbox')
  const triaged = useMemo(() => triageList(list, now), [list, now])
  const items = triaged[tab]
  const tabId = (t: TriageTab) => `${id}-tab-${t}`
  const panelId = `${id}-panel`
  const panelRef = useRef<HTMLDivElement>(null)
  // revealed is the last reveal request handled, and highlight the row it highlights.
  const [revealed, setRevealed] = useState(0)
  const [highlight, setHighlight] = useState<number | null>(null)

  // A new request to show an item turns to the tab that holds it; the list itself is
  // opened by the inbox view.
  if (reveal !== null && reveal.seq !== revealed) {
    setRevealed(reveal.seq)
    setTab(tabOf(triaged, reveal.id) ?? 'inbox')
    setHighlight(reveal.id)
  }

  // Once the list is open, the highlighted row scrolls into view, and the highlight fades.
  useEffect(() => {
    if (highlight === null || collapsed) {
      return undefined
    }

    const row = panelRef.current?.querySelector(`[data-item-id="${String(highlight)}"]`)

    if (row instanceof HTMLElement && 'scrollIntoView' in row) {
      row.scrollIntoView({ block: 'center', behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
    }

    const timer = setTimeout(() => {
      setHighlight(null)
    }, highlightMs)

    return () => {
      clearTimeout(timer)
    }
  }, [highlight, revealed, collapsed])

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
      <div ref={panelRef} role="tabpanel" id={panelId} aria-labelledby={tabId(tab)} hidden={collapsed}>
        {items.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">{emptyText(tab, title)}</p>
        ) : (
          <ul>
            {items.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                now={now}
                launch={launch}
                tab={tab}
                triage={triage}
                onFilesSettings={onFilesSettings}
                highlighted={item.id === highlight}
              />
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
// Undo. When every list is empty, the inbox throws a party above them (InboxNil). The
// account and the API budget live in the toolbar.
export function InboxView({
  inbox,
  launch,
  snoozeDays,
  onSnoozeSettings,
  onFilesSettings,
  collapsed,
  onCollapse,
  reveal = null,
}: InboxViewProps) {
  const {
    snapshot,
    now,
    loadError,
    refreshError,
    refreshing,
    refresh,
    dismiss,
    snooze,
    restore,
    pin,
    unpin,
    refreshItem,
  } = inbox
  const error = firstMessage(refreshError, snapshot?.lastError ?? '', loadError)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const count = useMemo(() => (snapshot === null ? 0 : inboxCount(snapshot, now)), [snapshot, now])
  const nil = useMemo(
    () => snapshot !== null && collapsed !== null && error === '' && isInboxNil(snapshot, now),
    [snapshot, collapsed, error, now],
  )
  // busy records that the inbox has held something while the page was open, so emptying it
  // from here throws the full party, while opening the page at nil only sprinkles.
  const [busy, setBusy] = useState(false)
  if (!busy && count > 0) {
    setBusy(true)
  }
  const toastCount = useRef(0)

  // revealIn is the first list that holds the item to show, the one that shows it.
  const revealIn = useMemo(
    () =>
      reveal === null || snapshot === null
        ? null
        : (sections.find(({ key }) => snapshot[key].items.some((item) => item.id === reveal.id))?.key ?? null),
    [reveal, snapshot],
  )
  // opened is the last reveal request whose list was opened, so a list the user collapses
  // afterwards stays collapsed.
  const opened = useRef(0)

  useEffect(() => {
    if (reveal === null || revealIn === null || collapsed === null || opened.current === reveal.seq) {
      return
    }

    opened.current = reveal.seq

    if (collapsed.has(revealIn)) {
      onCollapse(revealIn, false)
    }
  }, [reveal, revealIn, collapsed, onCollapse])

  const showToast = useCallback((message: Omit<ToastMessage, 'id'>) => {
    toastCount.current += 1
    setToast({ ...message, id: toastCount.current })
  }, [])

  const closeToast = useCallback(() => {
    setToast(null)
  }, [])

  const triage = useMemo<TriageActions>(() => {
    // failed reports a triage action the backend refused; the item is back as it was.
    const failed = (what: string, item: InboxItem, error: string) => {
      showToast({ text: `Could not ${what} “${item.title}”`, detail: error, tone: 'error' })
    }

    // undo puts an item back the way it was before it was snoozed or dismissed.
    const undo = (item: InboxItem) => () => {
      closeToast()
      void restore(item.id).then((result) => {
        if (!result.ok) {
          failed('undo the change to', item, result.error)
        }
      })
    }

    return {
      snoozeDays,
      onDismiss: (item) => {
        void dismiss(item.id).then((result) => {
          if (result.ok) {
            showToast({ text: 'Dismissed', detail: item.title, undo: undo(item) })
          } else {
            failed('dismiss', item, result.error)
          }
        })
      },
      onSnooze: (item, days) => {
        void snooze(item.id, days).then((result) => {
          if (!result.ok) {
            failed('snooze', item, result.error)

            return
          }

          const until = findItem(result.data, item.id)?.snoozedUntil ?? ''
          const text = until === '' ? 'Snoozed' : `Snoozed until ${formatWhen(now, new Date(until))}`

          showToast({ text, detail: item.title, undo: undo(item) })
        })
      },
      onRestore: (item) => {
        void restore(item.id).then((result) => {
          if (result.ok) {
            showToast({ text: isDismissed(item) ? 'Restored' : 'Unsnoozed', detail: item.title })
          } else {
            failed('restore', item, result.error)
          }
        })
      },
      // A pin shows at once, as the item moves to the top, so it needs no toast.
      onPin: (item) => {
        void pin(item.id).then((result) => {
          if (!result.ok) {
            failed('pin', item, result.error)
          }
        })
      },
      onUnpin: (item) => {
        void unpin(item.id).then((result) => {
          if (!result.ok) {
            failed('unpin', item, result.error)
          }
        })
      },
      // A refreshed item that is still open updates in place; one that is not leaves the
      // inbox, which the toast explains.
      onRefresh: async (item) => {
        const result = await refreshItem(item.id)

        if (!result.ok) {
          failed('refresh', item, result.error)
        } else if (findItem(result.data, item.id) === undefined) {
          showToast({ text: 'No longer open on GitHub', detail: item.title })
        }
      },
      onChangeDefault: onSnoozeSettings,
    }
  }, [snoozeDays, now, dismiss, snooze, restore, pin, unpin, refreshItem, showToast, closeToast, onSnoozeSettings])

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
      {nil && snapshot !== null && <InboxNil snapshot={snapshot} now={now} earned={busy} />}
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
              onFilesSettings={onFilesSettings}
              reveal={key === revealIn ? reveal : null}
            />
          ))}
      <Toast toast={toast} onClose={closeToast} />
    </div>
  )
}
