import { formatAge } from '../lib/age'
import type { InboxState } from '../lib/useInbox'
import type { InboxList } from '../platform'
import { ItemRow, type LaunchOptions } from './ItemRow'

export type { LaunchOptions } from './ItemRow'

export interface InboxViewProps {
  // inbox is the window's shared inbox state (useInbox).
  inbox: InboxState
  // launch is null until the settings and profiles have loaded; rows show no launch
  // controls until then.
  launch: LaunchOptions | null
}

type ListKey = 'reviewRequested' | 'authored' | 'assigned'

const sections: readonly { key: ListKey; title: string }[] = [
  { key: 'reviewRequested', title: 'Review requested' },
  { key: 'authored', title: 'Authored' },
  { key: 'assigned', title: 'Assigned' },
]

// countText is a list's size, as "N of M" when GitHub matched more than it returned.
function countText(list: InboxList): string {
  return list.total > list.shown ? `${String(list.shown)} of ${String(list.total)}` : String(list.shown)
}

// agoText reads a time as "5m ago" or "just now".
function agoText(now: Date, iso: string): string {
  const age = formatAge(now, iso)

  return age === 'just now' ? age : `${age} ago`
}

// firstMessage returns the first non-empty message.
function firstMessage(...messages: string[]): string {
  return messages.find((m) => m !== '') ?? ''
}

interface ListSectionProps {
  title: string
  list: InboxList
  now: Date
  launch: LaunchOptions | null
}

function ListSection({ title, list, now, launch }: ListSectionProps) {
  const items = list.items ?? []

  return (
    <section aria-label={title} className="rounded-lg border border-border bg-surface">
      <header className="flex items-baseline justify-between border-b border-border px-4 py-2">
        <h2 className="text-sm font-semibold">{title}</h2>
        <span className="text-xs text-muted">{countText(list)}</span>
      </header>
      {items.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">Nothing here.</p>
      ) : (
        <ul>
          {items.map((item) => (
            <ItemRow key={item.id} item={item} now={now} launch={launch} />
          ))}
        </ul>
      )}
    </section>
  )
}

// InboxView shows the three inbox lists across the full width, with a refresh control and
// the time of the last refresh. The account and the API budget live in the toolbar.
export function InboxView({ inbox, launch }: InboxViewProps) {
  const { snapshot, now, loadError, refreshError, refreshing, refresh } = inbox
  const error = firstMessage(refreshError, snapshot?.lastError ?? '', loadError)

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
              Last refreshed <time dateTime={snapshot.lastRefreshedAt}>{agoText(now, snapshot.lastRefreshedAt)}</time>
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
      {snapshot === null
        ? loadError === '' && <p className="text-sm text-muted">Loading the inbox…</p>
        : sections.map(({ key, title }) => (
            <ListSection key={key} title={title} list={snapshot[key]} now={now} launch={launch} />
          ))}
    </div>
  )
}
