import { useId } from 'react'

import { badgeText, formatCount } from '../lib/format'
import { quickViewMax } from '../lib/notifications'
import type { NotificationsState } from '../lib/useNotifications'
import { usePopover } from '../lib/usePopover'
import type { AppNotification } from '../platform'
import { quietButtonClass } from './controls'
import { BellIcon, CheckIcon, HistoryIcon, SlidersIcon } from './icons'
import { EntryRow } from './notifications/EntryRow'
import { PopoverPanel } from './PopoverPanel'

export interface NotificationsMenuProps {
  notifications: NotificationsState
  // onOpen shows an entry's item, and marks the entry read: in the inbox while it is there,
  // on GitHub after; an entry about no item opens its page, as a release does, or its run,
  // as a failed run does.
  onOpen: (entry: AppNotification) => void
  // onSettings opens the notification settings.
  onSettings: () => void
  // onViewAll opens the Notifications page on the app's own tab; without it there is no
  // View all.
  onViewAll?: (() => void) | undefined
}

// bellLabel names the bell with how many entries are unread. It never reads as the
// Notifications tab's name alone, so the two are told apart.
export function bellLabel(unread: number): string {
  return unread === 0 ? 'No unread notifications' : `${formatCount(unread)} unread notifications`
}

// NotificationsMenu is the toolbar's bell, a quick view of the unread notifications: how
// many are unread, and a click opens the newest of them, up to quickViewMax. Opening it
// marks nothing. An entry opens its item and is marked read; its Done (✓) marks it read
// without opening it, which takes it off the bell and keeps it in the history; Mark all
// done does that to them all. View all opens the Notifications page, which keeps the whole
// history.
export function NotificationsMenu({ notifications, onOpen, onSettings, onViewAll }: NotificationsMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, close, triggerRef } = popover
  const { log, error, markRead, markAllRead, markError } = notifications
  const unread = log.items.filter((n) => !n.read)
  const shown = unread.slice(0, quickViewMax)
  const more = unread.length - shown.length

  const label = bellLabel(log.unread)
  const failure =
    markError !== ''
      ? `Could not mark them read: ${markError}`
      : error !== ''
        ? `Could not load the notifications: ${error}`
        : ''

  const viewAll = () => {
    close()
    onViewAll?.()
  }

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className="relative flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text"
      >
        <BellIcon className="size-[1.125rem]" />
        {log.unread > 0 && (
          <span
            data-testid="notifications-unread"
            aria-hidden="true"
            className="absolute top-0.5 right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-[0.625rem] leading-4 font-semibold text-surface tabular-nums"
          >
            {badgeText(log.unread)}
          </span>
        )}
      </button>
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="z-40 flex max-h-[min(32rem,calc(100vh-5rem))] w-96 flex-col shadow-black/20 outline-none"
      >
        <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
          <h2 id={`${id}-title`} className="text-sm font-semibold">
            Notifications
          </h2>
          {unread.length > 0 && (
            <button type="button" onClick={markAllRead} className={`ml-auto ${quietButtonClass}`}>
              <CheckIcon className="size-3.5" />
              Mark all done
            </button>
          )}
        </div>
        {failure !== '' && (
          <p role="alert" className="border-b border-border px-4 py-2 text-xs text-danger">
            {failure}
          </p>
        )}
        {log.items.length === 0 ? (
          <div className="px-4 py-6 text-center">
            <p className="text-sm font-medium">No notifications yet</p>
            <p className="mt-1 text-xs text-muted">
              New review requests, assignments, snoozes that end early, releases, and failed runs show up here.
            </p>
          </div>
        ) : unread.length === 0 ? (
          <div className="px-4 py-6 text-center">
            <p className="text-sm font-medium">You’re all caught up</p>
            <p className="mt-1 text-xs text-muted">Past notifications are on the Notifications page.</p>
          </div>
        ) : (
          <ul aria-label="Unread notifications" className="min-h-0 flex-1 divide-y divide-border overflow-y-auto">
            {shown.map((entry) => (
              <EntryRow
                key={entry.id}
                compact
                entry={entry}
                onOpen={() => {
                  close()
                  onOpen(entry)
                }}
                onDone={() => {
                  markRead([entry.id])
                }}
              />
            ))}
            {more > 0 && (
              <li className="px-4 py-2 text-xs text-muted">
                {formatCount(more)} more unread
                {onViewAll !== undefined && (
                  <>
                    {' · '}
                    <button type="button" onClick={viewAll} className="font-medium text-text hover:underline">
                      View all
                    </button>
                  </>
                )}
              </li>
            )}
          </ul>
        )}
        <div className="flex items-center gap-1 border-t border-border px-2 py-1.5">
          {onViewAll !== undefined && (
            <button type="button" onClick={viewAll} className={quietButtonClass}>
              <HistoryIcon className="size-3.5" />
              View all
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              close()
              onSettings()
            }}
            className={`ml-auto ${quietButtonClass}`}
          >
            <SlidersIcon className="size-3.5" />
            Notification settings
          </button>
        </div>
      </PopoverPanel>
    </div>
  )
}
