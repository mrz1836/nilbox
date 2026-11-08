import { useEffect, useId, useRef, useState } from 'react'

import { prefersReducedMotion } from '../lib/motion'
import { sortByStatus, statusesOf, statusOrder } from '../lib/statusOrder'
import { tabOf, type TriagedList, type TriageTab } from '../lib/triage'
import { worktreeOf } from '../lib/itemWorktrees'
import type { ItemResolution, Worktree } from '../platform'
import { ArchiveIcon, ChevronDownIcon, ClockIcon } from './icons'
import type { EditActions } from './ItemMenu'
import { ItemRow, type LaunchOptions } from './ItemRow'
import { StatusSort } from './StatusSort'
import { TabPanel, tabPanelId, Tabs } from './Tabs'
import type { TriageActions } from './TriageControls'

// Reveal asks the inbox to show an item: its list opens at the item's tab, and its row
// scrolls into view, highlighted for a moment. seq tells one request from the next, so
// asking for the same item again shows it again.
export interface Reveal {
  id: number
  seq: number
}

// highlightMs is how long a revealed row stays highlighted.
const highlightMs = 2400

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

// ListOrder is how a list orders its own tab: by newest activity, or by project status in
// the user's order, and the calls that change either.
export interface ListOrder {
  byStatus: boolean
  // stored is the user's order of status names (see statusOrder).
  stored: readonly string[]
  onSortChange: (byStatus: boolean) => void
  onOrderChange: (order: readonly string[]) => void
}

export interface ListSectionProps {
  title: string
  // triaged is the list sorted into its tabs.
  triaged: TriagedList
  launch: LaunchOptions | null
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
  // worktrees holds each item's worktree, from the Worktrees view's latest listing (see
  // itemWorktrees).
  worktrees: ReadonlyMap<string, Worktree>
  triage: TriageActions
  // edits is what each row's menu does beyond triage (see ItemMenu).
  edits?: EditActions
  collapsed: boolean
  onCollapse: (collapsed: boolean) => void
  onFilesSettings?: (() => void) | undefined
  // reveal is an item in this list to show, or null.
  reveal: Reveal | null
  // order is how the list's own tab is ordered.
  order: ListOrder
}

// ListSection is one inbox list as tabs: the list itself, then what was snoozed and
// what was dismissed from it, each with its count. The toggle at the right of its header
// collapses it to just the tabs, which keep their counts; a collapsed list's rows are not
// rendered at all. A list whose items are in GitHub projects can sort its own tab by
// project status (StatusSort), left of the toggle.
export function ListSection({
  title,
  triaged,
  launch,
  resolutions,
  worktrees,
  triage,
  edits,
  collapsed,
  onCollapse,
  onFilesSettings,
  reveal,
  order,
}: ListSectionProps) {
  const id = useId()
  const [tab, setTab] = useState<TriageTab>('inbox')
  // The list's own tab can sort by project status; the statuses are those of its items,
  // and the order the user's, with each status they haven't placed placed by its name.
  const statuses = statusesOf(triaged.inbox)
  const byStatus = order.byStatus
  const ranked = statusOrder(
    order.stored,
    statuses.map((s) => s.name),
  )
  const items = tab === 'inbox' && byStatus ? sortByStatus(triaged.inbox, ranked) : triaged[tab]
  // The sort shows on the list's own tab while it has a status to sort by, or is sorted
  // by one, so it can always be turned back.
  const sortable = tab === 'inbox' && !collapsed && (statuses.length > 0 || byStatus)
  const panelRef = useRef<HTMLDivElement>(null)
  // revealed is the last reveal request handled, and highlight the row it highlights.
  const [revealed, setRevealed] = useState(0)
  const [highlight, setHighlight] = useState<number | null>(null)

  // A new request to show an item turns to the tab that holds it; the list itself is
  // opened by the inbox view. A request withdrawn, as leaving the inbox withdraws it, ends
  // its highlight, so coming back to the list, kept as it was left, shows nothing again.
  if (reveal !== null && reveal.seq !== revealed) {
    setRevealed(reveal.seq)
    setTab(tabOf(triaged, reveal.id) ?? 'inbox')
    setHighlight(reveal.id)
  } else if (reveal === null && highlight !== null) {
    setHighlight(null)
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

  const hiddenCount = (n: number) => (n > 0 ? String(n) : '')

  return (
    <section aria-label={title} className="rounded-lg border border-border bg-surface">
      <div className={`flex items-center gap-1 px-1.5 ${collapsed ? '' : 'border-b border-border'}`}>
        <Tabs
          baseId={id}
          label={title}
          value={tab}
          onChange={select}
          tabs={[
            { value: 'inbox', label: title, count: countText(triaged.inbox.length, triaged.total), strong: true },
            {
              value: 'snoozed',
              label: 'Snoozed',
              count: hiddenCount(triaged.snoozed.length),
              icon: <ClockIcon className="size-3.5" />,
            },
            {
              value: 'dismissed',
              label: 'Dismissed',
              count: hiddenCount(triaged.dismissed.length),
              icon: <ArchiveIcon className="size-3.5" />,
            },
          ]}
          className="flex flex-1 items-center gap-1"
          tabClassName="pt-2.5"
        />
        {sortable && (
          <StatusSort
            title={title}
            byStatus={byStatus}
            onSortChange={order.onSortChange}
            statuses={statuses}
            order={ranked}
            customized={order.stored.length > 0}
            onOrderChange={order.onOrderChange}
          />
        )}
        <button
          type="button"
          aria-expanded={!collapsed}
          aria-controls={tabPanelId(id)}
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
      <TabPanel ref={panelRef} baseId={id} value={tab} hidden={collapsed}>
        {collapsed ? null : items.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">{emptyText(tab, title)}</p>
        ) : (
          <ul>
            {items.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                launch={launch}
                resolution={resolutions.get(item.id) ?? null}
                worktree={worktreeOf(worktrees, item)}
                tab={tab}
                triage={triage}
                edits={edits}
                onFilesSettings={onFilesSettings}
                highlighted={item.id === highlight}
              />
            ))}
          </ul>
        )}
      </TabPanel>
    </section>
  )
}
