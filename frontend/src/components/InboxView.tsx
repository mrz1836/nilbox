import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { isInboxNil } from '../lib/nil'
import { formatAgo, formatWhen } from '../lib/time'
import { findItem, inboxCount, isDismissed } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import type { InboxItem, ListKey } from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { InboxNil } from './InboxNil'
import type { LaunchOptions } from './ItemRow'
import { ListSection, type Reveal } from './ListSection'
import { Toast, type ToastMessage } from './Toast'
import type { TriageActions } from './TriageControls'

export type { LaunchOptions } from './ItemRow'
export type { Reveal } from './ListSection'

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
      {error !== '' && <ErrorBanner>{error}</ErrorBanner>}
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
