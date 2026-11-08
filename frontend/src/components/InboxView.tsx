import { useEffect, useRef, useState } from 'react'

import { isInboxNil } from '../lib/nil'
import { formatAgo, formatWhen } from '../lib/time'
import { findItem, isDismissed, type TriagedInbox } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxOrderState } from '../lib/useInboxOrder'
import { useClock } from '../lib/useNow'
import type { InboxItem, ItemResolution, ListKey } from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { InboxNil } from './InboxNil'
import type { LaunchOptions } from './ItemRow'
import { ListSection, type Reveal } from './ListSection'
import { ProjectsTip } from './ProjectsTip'
import { RelativeTime } from './RelativeTime'
import { Toast, type ToastMessage } from './Toast'
import type { TriageActions } from './TriageControls'

export type { LaunchOptions } from './ItemRow'
export type { Reveal } from './ListSection'

export interface InboxViewProps {
  // inbox is the window's shared inbox state (useInbox).
  inbox: InboxState
  // triaged is the inbox sorted into its tabs (useTriage), or null until it has loaded.
  triaged: TriagedInbox | null
  // launch is null until the settings and profiles have loaded; rows show no launch
  // controls until then.
  launch: LaunchOptions | null
  // resolutions holds each item's resolved launch settings by item ID, as they load.
  resolutions: ReadonlyMap<number, ItemResolution>
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
  // showing reports whether the page is on screen; App keeps it mounted, hidden, while
  // another page shows.
  showing?: boolean
  // offerProjects reports whether the inbox offers to show issue projects (see
  // offersProjects), and onDismissProjects stores that the user put the offer away.
  offerProjects?: boolean
  onDismissProjects?: () => void
  // inboxOrder is how the lists are ordered (useInboxOrder). The lists wait for it to
  // load.
  inboxOrder: InboxOrderState
}

const sections: readonly { key: ListKey; title: string }[] = [
  { key: 'reviewRequested', title: 'Review requested' },
  { key: 'authored', title: 'Authored' },
  { key: 'assigned', title: 'Assigned' },
]

// firstMessage returns the first non-empty message.
function firstMessage(...messages: string[]): string {
  return messages.find((m) => m !== '') ?? ''
}

// InboxView shows the three inbox lists across the full width, with a refresh control and
// the time of the last refresh. Each list has tabs for what was snoozed or dismissed from
// it and collapses from its header, and each triage action is confirmed by a toast with an
// Undo. When every list is empty, the inbox throws a party above them (InboxNil). The
// account and the API budget live in the toolbar.
export function InboxView({
  inbox,
  triaged,
  launch,
  resolutions,
  snoozeDays,
  onSnoozeSettings,
  onFilesSettings,
  collapsed,
  onCollapse,
  reveal = null,
  showing = true,
  offerProjects = false,
  onDismissProjects,
  inboxOrder,
}: InboxViewProps) {
  const { snapshot, loadError, refreshError, refreshing, refresh, dismiss, snooze, restore, pin, unpin, refreshItem } =
    inbox
  const { order, setStatusSort, setStatusOrder } = inboxOrder
  const clock = useClock()
  const error = firstMessage(refreshError, snapshot?.lastError ?? '', loadError)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const count = triaged?.count ?? 0
  const nil =
    snapshot !== null && triaged !== null && collapsed !== null && error === '' && isInboxNil(snapshot, triaged)
  // busy records that the inbox has held something while the page was showing, so emptying
  // it from here throws the full party, while coming to the page at nil only sprinkles.
  // The page stays mounted while another shows, so hiding it forgets busy, and each visit
  // numbers the party anew: an inbox emptied meanwhile is found at nil, and sprinkles.
  const [busy, setBusy] = useState(false)
  const [visit, setVisit] = useState({ showing, n: 0 })
  const toastCount = useRef(0)
  // offeredProjects records that the projects tip showed during this run, so a refresh
  // that then finds access says the projects are on; projectsClosed, that the user
  // closed it.
  const [offeredProjects, setOfferedProjects] = useState(false)
  const [projectsClosed, setProjectsClosed] = useState(false)

  if (offerProjects && !offeredProjects) {
    setOfferedProjects(true)
  }

  const projectsOn = offeredProjects && snapshot?.projectAccess === 'on'
  const projectsTip = !projectsClosed && (offerProjects || projectsOn)

  if (visit.showing !== showing) {
    setVisit({ showing, n: showing ? visit.n + 1 : visit.n })
    setBusy(false)
  } else if (showing && !busy && count > 0) {
    setBusy(true)
  }

  // revealIn is the first list that holds the item to show, the one that shows it.
  const revealIn =
    reveal === null || snapshot === null
      ? null
      : (sections.find(({ key }) => snapshot[key].items.some((item) => item.id === reveal.id))?.key ?? null)
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

  const showToast = (message: Omit<ToastMessage, 'id'>) => {
    toastCount.current += 1
    setToast({ ...message, id: toastCount.current })
  }

  const closeToast = () => {
    setToast(null)
  }

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

  // triage is what every row's triage buttons do. It reads no time while rendering (the
  // toast after a snooze reads the clock when it shows), so the rows keep the same
  // actions from one tick to the next.
  const triage: TriageActions = {
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
        const text = until === '' ? 'Snoozed' : `Snoozed until ${formatWhen(clock.getSnapshot(), new Date(until))}`

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
              Last refreshed <RelativeTime iso={snapshot.lastRefreshedAt} show={formatAgo} />
            </>
          )}
        </span>
      </div>
      {error !== '' && <ErrorBanner>{error}</ErrorBanner>}
      {snapshot !== null && snapshot.scopeWarning !== '' && (
        <p className="rounded-md border border-warning px-3 py-2 text-sm text-warning">{snapshot.scopeWarning}</p>
      )}
      {projectsTip && (
        <ProjectsTip
          on={projectsOn}
          refreshing={refreshing}
          onCheck={refresh}
          onClose={() => {
            setProjectsClosed(true)

            if (!projectsOn) {
              onDismissProjects?.()
            }
          }}
        />
      )}
      {nil && <InboxNil key={visit.n} stats={triaged} earned={busy} />}
      {triaged === null || collapsed === null || order === null
        ? loadError === '' && <p className="text-sm text-muted">Loading the inbox…</p>
        : sections.map(({ key, title }) => (
            <ListSection
              key={key}
              title={title}
              triaged={triaged.lists[key]}
              launch={launch}
              resolutions={resolutions}
              triage={triage}
              collapsed={collapsed.has(key)}
              onCollapse={(on) => {
                onCollapse(key, on)
              }}
              onFilesSettings={onFilesSettings}
              reveal={key === revealIn ? reveal : null}
              order={{
                byStatus: order.statusSorted.includes(key),
                stored: order.statusOrder,
                onSortChange: (on) => {
                  setStatusSort(key, on)
                },
                onOrderChange: setStatusOrder,
              }}
            />
          ))}
      <Toast toast={toast} onClose={closeToast} />
    </div>
  )
}
