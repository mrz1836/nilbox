import { useState } from 'react'

import { formatCount } from '../../lib/format'
import { filterAppNotifications } from '../../lib/notificationFilter'
import { appTypes, filterEntries, shownTypes, type AppTypeID, type NotificationShow } from '../../lib/notifications'
import { useAutopilotShown } from '../../lib/useAutopilotShown'
import { useMutation } from '../../lib/useMutation'
import type { NotificationsState } from '../../lib/useNotifications'
import type { AppNotification } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass, Segmented } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { CheckIcon, SlidersIcon, TrashIcon } from '../icons'
import { DayGroups } from './DayGroups'
import { EntryRow } from './EntryRow'
import { NoMatches, PageNote, Toolbar } from './parts'

// showOptions and typeOptions are the tab's Show and Type, as the segmented controls offer
// them: Autopilot's type only while a routine exists.
const showOptions: readonly { value: NotificationShow; label: string }[] = [
  { value: 'unread', label: 'Unread' },
  { value: 'all', label: 'All' },
]

function typeOptions(autopilotShown: boolean) {
  return shownTypes(autopilotShown).map((id) => ({ value: id, label: appTypes[id].label }))
}

export interface AppNotificationsTabProps {
  notifications: NotificationsState
  // show and type are the tab's Show and Type, which the window holds, so the toolbar's
  // field counts what the list shows; onShow and onType hear each choice.
  show: NotificationShow
  type: AppTypeID
  onShow: (show: NotificationShow) => void
  onType: (type: AppTypeID) => void
  // query is the toolbar's field, and onClearQuery empties it.
  query: string
  onClearQuery: () => void
  // onOpen opens an entry and marks it read; onSettings opens the notification settings.
  onOpen: (entry: AppNotification) => void
  onSettings: () => void
}

// AppNotificationsTab is the Notifications page's tab of the app's own notifications: the
// whole history, newest first, under a heading for each day, narrowed by Show, Type, and
// the toolbar's field. A row opens its entry and marks it read; while the pointer or the
// focus is on it, an unread row offers Done and a read one Mark unread, which puts it back
// on the bell. Mark all done marks every entry read; Clear history forgets them all, after
// asking.
export function AppNotificationsTab({
  notifications,
  show,
  type,
  onShow,
  onType,
  query,
  onClearQuery,
  onOpen,
  onSettings,
}: AppNotificationsTabProps) {
  const { log, error, markRead, markUnread, markAllRead, markError, clear } = notifications
  const autopilotShown = useAutopilotShown()
  const [asking, setAsking] = useState(false)
  const clearing = useMutation(clear)
  const listed = filterEntries(log.items, show, type)
  const matched = filterAppNotifications(listed, query)
  const shown = matched ?? listed

  const clearHistory = () => {
    setAsking(false)
    void clearing.run()
  }

  // empty says why the list is empty: no history at all, a filter that matches nothing,
  // nothing unread, or nothing of the type.
  const empty = () => {
    if (log.items.length === 0) {
      return (
        <PageNote title="No notifications yet">
          New review requests, assignments, snoozes that end early, releases, and failed runs show up here.
        </PageNote>
      )
    }

    if (matched !== null) {
      return <NoMatches onClear={onClearQuery} />
    }

    if (show === 'unread' && type === 'all') {
      return <PageNote title="You’re all caught up">Show All to see past notifications.</PageNote>
    }

    return <PageNote title={appTypes[type].none} />
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Toolbar>
        <Segmented label="Show" size="sm" value={show} options={showOptions} onChange={onShow} />
        <Segmented label="Type" size="sm" value={type} options={typeOptions(autopilotShown)} onChange={onType} />
        <span className="ml-auto flex items-center gap-1">
          {log.unread > 0 && (
            <button type="button" onClick={markAllRead} className={quietButtonClass}>
              <CheckIcon className="size-3.5" />
              Mark all done
            </button>
          )}
          <button
            type="button"
            disabled={clearing.pending || log.items.length === 0}
            onClick={() => {
              setAsking(true)
            }}
            className={quietButtonClass}
          >
            <TrashIcon className="size-3.5" />
            Clear history
          </button>
        </span>
      </Toolbar>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {error !== '' && <ErrorBanner className="mx-2 mt-3">Could not load the notifications: {error}</ErrorBanner>}
        {markError !== '' && <ErrorBanner className="mx-2 mt-3">Could not mark them: {markError}</ErrorBanner>}
        {clearing.error !== '' && (
          <ErrorBanner className="mx-2 mt-3">Could not clear the history: {clearing.error}</ErrorBanner>
        )}
        {shown.length === 0 ? (
          empty()
        ) : (
          <DayGroups
            items={shown}
            when={(entry) => entry.createdAt}
            render={(entry) => (
              <EntryRow
                key={entry.id}
                entry={entry}
                onOpen={() => {
                  onOpen(entry)
                }}
                onDone={() => {
                  markRead([entry.id])
                }}
                onUnread={() => {
                  markUnread([entry.id])
                }}
              />
            )}
          />
        )}
        <p className="mt-4 flex items-center justify-center gap-2 px-3 text-xs text-muted">
          Keeps your latest {formatCount(1000)} notifications
          <span aria-hidden="true">·</span>
          <button type="button" onClick={onSettings} className={quietButtonClass}>
            <SlidersIcon className="size-3.5" />
            Notification settings
          </button>
        </p>
      </div>
      {asking && (
        <ConfirmDialog
          title="Clear the notification history?"
          confirmLabel="Clear history"
          onConfirm={clearHistory}
          onCancel={() => {
            setAsking(false)
          }}
        >
          This forgets every notification. Banners and settings stay.
        </ConfirmDialog>
      )}
    </div>
  )
}
