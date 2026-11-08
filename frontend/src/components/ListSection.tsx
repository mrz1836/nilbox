import { useEffect, useId, useRef, useState } from 'react'

import { prefersReducedMotion } from '../lib/motion'
import { tabOf, type TriagedList, type TriageTab } from '../lib/triage'
import type { ItemResolution } from '../platform'
import { ArchiveIcon, ChevronDownIcon, ClockIcon } from './icons'
import { ItemRow, type LaunchOptions } from './ItemRow'
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

export interface ListSectionProps {
  title: string
  // triaged is the list sorted into its tabs.
  triaged: TriagedList
  launch: LaunchOptions | null
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
  triage: TriageActions
  collapsed: boolean
  onCollapse: (collapsed: boolean) => void
  onFilesSettings?: (() => void) | undefined
  // reveal is an item in this list to show, or null.
  reveal: Reveal | null
}

// ListSection is one inbox list as tabs: the list itself, then what was snoozed and
// what was dismissed from it, each with its count. The toggle at the right of its header
// collapses it to just the tabs, which keep their counts; a collapsed list's rows are not
// rendered at all.
export function ListSection({
  title,
  triaged,
  launch,
  resolutions,
  triage,
  collapsed,
  onCollapse,
  onFilesSettings,
  reveal,
}: ListSectionProps) {
  const id = useId()
  const [tab, setTab] = useState<TriageTab>('inbox')
  const items = triaged[tab]
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
                tab={tab}
                triage={triage}
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
