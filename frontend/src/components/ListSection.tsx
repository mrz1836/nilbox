import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import { formatCount } from '../lib/format'
import type { FilteredList } from '../lib/inboxFilter'
import { ofKind, shownItems, statusRanking } from '../lib/listOrder'
import { prefersReducedMotion } from '../lib/motion'
import { statusesOf } from '../lib/statusOrder'
import { tabOf, triageTabs, type TriagedList, type TriageTab } from '../lib/triage'
import { worktreeOf } from '../lib/itemWorktrees'
import { sessionsOf } from '../lib/sessions'
import type { AgentSession, InboxItem, ItemResolution, ListKind, Worktree } from '../platform'
import { quietButtonClass, Segmented } from './controls'
import { ArchiveIcon, ChevronDownIcon, ClockIcon, MuteIcon } from './icons'
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

// tabNames name the tabs in the hint that points to the matches in another.
const tabNames: Readonly<Record<TriageTab, string>> = {
  inbox: 'the list',
  snoozed: 'Snoozed',
  dismissed: 'Dismissed',
  muted: 'Muted',
}

// emptyText is what an empty tab says.
function emptyText(tab: TriageTab, title: string): string {
  switch (tab) {
    case 'snoozed':
      return `Nothing snoozed. A snoozed item comes back to ${title} on its own when its time is up.`
    case 'dismissed':
      return 'Nothing dismissed. A dismissed item stays here until you restore it.'
    case 'muted':
      return 'Nothing muted. Mute a repository or an org from a card’s menu, or in Settings → Repositories, and its items wait here.'
    default:
      return 'Nothing here.'
  }
}

// kindWords name the items a list shows only one kind of, for its empty tab.
const kindWords: Readonly<Record<Exclude<ListKind, 'all'>, string>> = {
  pr: 'pull requests',
  issue: 'issues',
}

// ListOrder is how a list orders its own tab, by newest activity or by project status in
// the user's order, and which kind of item it shows there, and the calls that change them.
export interface ListOrder {
  byStatus: boolean
  // stored is the user's order of status names (see statusOrder).
  stored: readonly string[]
  // kind is every item ('all'), or only the list's pull requests or its issues.
  kind: ListKind
  onSortChange: (byStatus: boolean) => void
  onOrderChange: (order: readonly string[]) => void
  onKindChange: (kind: ListKind) => void
}

export interface ListSectionProps {
  title: string
  // triaged is the list sorted into its tabs.
  triaged: TriagedList
  launch: LaunchOptions | null
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
  // sessions holds each item's agent sessions, newest first, by itemKey (see
  // itemSessions).
  sessions: ReadonlyMap<string, readonly AgentSession[]>
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
  // matched is what the inbox's filter leaves of each tab, while one is on; the list then
  // shows only those items, and hides itself when no tab has any. triaged stays whole,
  // for the statuses the list sorts by and for finding a revealed item's tab.
  matched?: FilteredList | undefined
  // marks is what each card highlights in its title (see Highlight).
  marks?: string
  // onFilterBy adds a filter to the inbox's query, or takes it away, as a card's
  // repository, author, and labels do when clicked.
  onFilterBy?: ((key: string, value: string) => void) | undefined
  // onOpen opens a card of this list in its sheet (see ItemRow).
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
}

// ListSection is one inbox list as tabs: the list itself, then what was snoozed, what was
// dismissed, and what is in a muted repository or org, each with its count. The toggle at the right of its header
// collapses it to just the tabs, which keep their counts; a collapsed list's rows are not
// rendered at all. A list whose items are in GitHub projects can sort its own tab by
// project status (StatusSort), left of the toggle, and a list that holds both pull requests
// and issues can show only one kind on its own tab, left of that, its count then reading
// "N of M".
//
// While the inbox is filtered, each tab shows only its matching items, in its own order,
// and counts them; a tab with none points to the tabs that have some. A list with no
// match anywhere is hidden but stays mounted, so it comes back as it was.
export function ListSection({
  title,
  triaged,
  launch,
  resolutions,
  worktrees,
  sessions,
  triage,
  edits,
  collapsed,
  onCollapse,
  onFilesSettings,
  reveal,
  order,
  matched,
  marks = '',
  onFilterBy,
  onOpen,
}: ListSectionProps) {
  const id = useId()
  const [tab, setTab] = useState<TriageTab>('inbox')
  // The list's own tab can sort by project status; the statuses are those of its items,
  // and the order the user's, with each status they haven't placed placed by its name.
  const statuses = statusesOf(triaged.inbox)
  const byStatus = order.byStatus
  const ranked = statusRanking(triaged, order.stored)
  const shown = matched ?? triaged
  // The sheets step through the cards in this same order (shownItems).
  const items = shownItems(triaged, tab, matched, order)
  const filtering = matched !== undefined
  // elsewhere are the other tabs that hold matches, for a tab that holds none.
  const elsewhere = filtering ? triageTabs.filter((t) => t !== tab && matched[t].length > 0) : []
  // The sort shows on the list's own tab while it has a status to sort by, or is sorted
  // by one, so it can always be turned back.
  const sortable = tab === 'inbox' && !collapsed && (statuses.length > 0 || byStatus)
  // The kind shows on the list's own tab while it holds both kinds, or shows only one, so
  // it can always be turned back; narrowed says it shows only one.
  const narrowed = order.kind !== 'all'
  const bothKinds = triaged.inbox.some((i) => i.kind === 'pr') && triaged.inbox.some((i) => i.kind === 'issue')
  const kindable = tab === 'inbox' && !collapsed && (bothKinds || narrowed)
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

  // A card asked for that the list's kind leaves out, as one a notification opens, shows
  // every item again, so its row is there to see.
  const showRevealed = useEffectEvent((id: number) => {
    const card = triaged.inbox.find((i) => i.id === id)

    if (card !== undefined && narrowed && card.kind !== order.kind) {
      order.onKindChange('all')
    }
  })

  useEffect(() => {
    if (highlight !== null) {
      showRevealed(highlight)
    }
  }, [highlight, revealed])

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
  const listCount =
    filtering || narrowed
      ? `${formatCount(ofKind(shown.inbox, order.kind).length)} of ${formatCount(triaged.inbox.length)}`
      : countText(triaged.inbox.length, triaged.total)

  return (
    <section
      aria-label={title}
      hidden={filtering && triageTabs.every((t) => matched[t].length === 0)}
      className="rounded-lg border border-border bg-surface"
    >
      <div className={`flex items-center gap-1 px-1.5 ${collapsed ? '' : 'border-b border-border'}`}>
        <Tabs
          baseId={id}
          label={title}
          value={tab}
          onChange={select}
          tabs={[
            { value: 'inbox', label: title, count: listCount, strong: true, filtered: filtering || narrowed },
            {
              value: 'snoozed',
              label: 'Snoozed',
              count: hiddenCount(shown.snoozed.length),
              icon: <ClockIcon className="size-3.5" />,
              filtered: filtering,
            },
            {
              value: 'dismissed',
              label: 'Dismissed',
              count: hiddenCount(shown.dismissed.length),
              icon: <ArchiveIcon className="size-3.5" />,
              filtered: filtering,
            },
            {
              value: 'muted',
              label: 'Muted',
              count: hiddenCount(shown.muted.length),
              icon: <MuteIcon className="size-3.5" />,
              filtered: filtering,
            },
          ]}
          className="flex flex-1 items-center gap-1"
          tabClassName="pt-2.5"
        />
        {kindable && (
          <Segmented<ListKind>
            label={`Show in ${title}`}
            size="sm"
            value={order.kind}
            options={[
              { value: 'all', label: 'All', title: 'Pull requests and issues' },
              { value: 'pr', label: 'PRs', title: 'Only pull requests' },
              { value: 'issue', label: 'Issues', title: 'Only issues' },
            ]}
            onChange={order.onKindChange}
          />
        )}
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
        {/* A filtered list shows open whatever its stored state, so it has nothing to
            toggle until the filter is off. */}
        {!filtering && (
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
        )}
      </div>
      <TabPanel ref={panelRef} baseId={id} value={tab} hidden={collapsed}>
        {collapsed ? null : items.length === 0 && filtering ? (
          <p className="flex flex-wrap items-center gap-x-1.5 px-4 py-3 text-sm text-muted">
            No matches here
            {elsewhere.map((t) => (
              <span key={t} className="flex items-center gap-1.5">
                <span aria-hidden="true">·</span>
                <button
                  type="button"
                  onClick={() => {
                    select(t)
                  }}
                  className="font-medium text-text hover:text-accent hover:underline"
                >
                  {formatCount(matched[t].length)} in {tabNames[t]}
                </button>
              </span>
            ))}
          </p>
        ) : items.length === 0 && tab === 'inbox' && order.kind !== 'all' ? (
          <p className="flex flex-wrap items-center gap-x-1.5 px-4 py-3 text-sm text-muted">
            No {kindWords[order.kind]} here.
            <button
              type="button"
              onClick={() => {
                order.onKindChange('all')
              }}
              className={quietButtonClass}
            >
              Show all
            </button>
          </p>
        ) : items.length === 0 ? (
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
                sessions={sessionsOf(sessions, item)}
                tab={tab}
                triage={triage}
                edits={edits}
                onFilesSettings={onFilesSettings}
                highlighted={item.id === highlight}
                marks={marks}
                onFilterBy={onFilterBy}
                onOpen={onOpen}
              />
            ))}
          </ul>
        )}
      </TabPanel>
    </section>
  )
}
