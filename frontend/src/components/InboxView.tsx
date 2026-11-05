import { useEffect, useState } from 'react'

import { formatAge } from '../lib/age'
import { errorMessage } from '../lib/format'
import {
  getInboxSnapshot,
  onInboxUpdated,
  openInBrowser,
  refreshInbox,
  type InboxItem,
  type InboxList,
  type InboxSnapshot,
  type Profile,
} from '../platform'
import { LaunchControls } from './LaunchControls'
import { RateGauge } from './RateGauge'

// LaunchOptions are what each row's launch controls need from the settings and profiles.
export interface LaunchOptions {
  profiles: Profile[]
  defaultProfile: string
  worktreeDefault: boolean
}

export interface InboxViewProps {
  // launch is null until the settings and profiles have loaded; rows show no launch
  // controls until then.
  launch: LaunchOptions | null
  // clock returns the current time for the relative ages. Keep it stable across renders.
  clock?: () => Date
}

type ListKey = 'reviewRequested' | 'authored' | 'assigned'

const sections: readonly { key: ListKey; title: string }[] = [
  { key: 'reviewRequested', title: 'Review requested' },
  { key: 'authored', title: 'Authored' },
  { key: 'assigned', title: 'Assigned' },
]

// tickMs is how often the relative ages are brought up to date.
const tickMs = 60_000

function systemClock(): Date {
  return new Date()
}

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

function openItem(url: string) {
  openInBrowser(url).catch((err: unknown) => {
    console.error('could not open the item in the browser', err)
  })
}

interface ItemRowProps {
  item: InboxItem
  now: Date
  launch: LaunchOptions | null
}

function ItemRow({ item, now, launch }: ItemRowProps) {
  return (
    <li className="flex flex-col gap-1.5 border-b border-border px-3 py-2.5 last:border-b-0">
      <div className="flex items-baseline gap-2">
        <span className="shrink-0 rounded bg-raised px-1.5 py-0.5 text-xs text-muted">
          {item.kind === 'pr' ? 'PR' : 'Issue'}
        </span>
        <button
          type="button"
          onClick={() => {
            openItem(item.url)
          }}
          className="text-left font-medium text-text hover:text-accent hover:underline"
        >
          {item.title}
        </button>
        {item.isDraft && <span className="shrink-0 text-xs text-muted">draft</span>}
      </div>
      <p className="text-xs text-muted">
        {item.repo} #{item.number} · {item.author} ·{' '}
        <time dateTime={item.updatedAt}>{formatAge(now, item.updatedAt)}</time>
      </p>
      {launch !== null && (
        <LaunchControls
          item={item}
          profiles={launch.profiles}
          defaultProfile={launch.defaultProfile}
          worktreeDefault={launch.worktreeDefault}
        />
      )}
    </li>
  )
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
      <header className="flex items-baseline justify-between border-b border-border px-3 py-2">
        <h2 className="text-sm font-semibold">{title}</h2>
        <span className="text-xs text-muted">{countText(list)}</span>
      </header>
      {items.length === 0 ? (
        <p className="px-3 py-3 text-sm text-muted">Nothing here.</p>
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

// InboxView shows the three inbox lists and the API budget. It loads the stored inbox on
// mount and again whenever the shell reports the inbox changed (the background refresh),
// and refreshes from GitHub when asked.
export function InboxView({ launch, clock = systemClock }: InboxViewProps) {
  const [snapshot, setSnapshot] = useState<InboxSnapshot | null>(null)
  const [loadError, setLoadError] = useState('')
  const [refreshError, setRefreshError] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const [now, setNow] = useState(clock)

  useEffect(() => {
    let active = true

    const load = () => {
      getInboxSnapshot()
        .then((next) => {
          if (active) {
            setSnapshot(next)
            setLoadError('')
            setRefreshError('')
            setNow(clock())
          }
        })
        .catch((err: unknown) => {
          if (active) {
            setLoadError(errorMessage(err))
          }
        })
    }

    load()

    const stop = onInboxUpdated(load)
    const tick = setInterval(() => {
      setNow(clock())
    }, tickMs)

    return () => {
      active = false
      stop()
      clearInterval(tick)
    }
  }, [clock])

  const refresh = () => {
    setRefreshing(true)

    refreshInbox()
      .then((next) => {
        setSnapshot(next)
        setRefreshError('')
      })
      .catch((err: unknown) => {
        setRefreshError(errorMessage(err))
      })
      .finally(() => {
        setRefreshing(false)
        setNow(clock())
      })
  }

  const error = firstMessage(refreshError, snapshot?.lastError ?? '', loadError)

  return (
    <div className="flex flex-col gap-4 p-4 lg:flex-row lg:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-4">
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
          {snapshot !== null && snapshot.viewer !== '' && (
            <span className="ml-auto text-muted">Signed in as {snapshot.viewer}</span>
          )}
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
      {snapshot !== null && (
        <aside className="w-full lg:w-72 lg:shrink-0">
          <RateGauge gauge={snapshot.rate} now={now} />
        </aside>
      )}
    </div>
  )
}
