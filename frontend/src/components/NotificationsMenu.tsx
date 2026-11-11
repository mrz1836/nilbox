import { useId, useState, type ReactNode } from 'react'

import { formatAgo } from '../lib/time'
import { formatCount } from '../lib/format'
import { entryRef, entryTitle, kindTitle } from '../lib/notifications'
import { useMutation } from '../lib/useMutation'
import type { NotificationsState } from '../lib/useNotifications'
import { usePopover } from '../lib/usePopover'
import type { AppNotification, NotificationKind } from '../platform'
import { quietButtonClass } from './controls'
import { AssignIcon, BellIcon, ClockIcon, EyeIcon, SlidersIcon, TagIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'
import { RelativeTime } from './RelativeTime'

export interface NotificationsMenuProps {
  notifications: NotificationsState
  // onOpen shows an entry's item: in the inbox while it is there, on GitHub after; an
  // entry about no item, such as a release, opens its page.
  onOpen: (entry: AppNotification) => void
  // onSettings opens the notification settings.
  onSettings: () => void
}

// kindIcons are each kind's icon in the history. A kind this build does not know, as
// another build may have stored, shows the bell.
const kindIcons: Readonly<Partial<Record<NotificationKind, (props: { className?: string }) => ReactNode>>> = {
  review_requested: EyeIcon,
  assigned: AssignIcon,
  woke: ClockIcon,
  release_done: TagIcon,
}

// bellLabel names the bell with how many entries are unread.
function bellLabel(unread: number): string {
  return unread === 0 ? 'Notifications' : `Notifications, ${formatCount(unread)} unread`
}

interface EntryProps {
  entry: AppNotification
  // fresh marks an entry that was unread when the history opened.
  fresh: boolean
  onOpen: () => void
}

// Entry is one notification in the history: what happened, to which item, and when.
function Entry({ entry, fresh, onOpen }: EntryProps) {
  const Icon = kindIcons[entry.kind] ?? BellIcon
  const where = entry.detail === '' ? entryRef(entry) : `${entryRef(entry)} · ${entry.detail}`

  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        title={entryTitle(entry)}
        className="flex w-full items-start gap-3 px-4 py-2.5 text-left transition-colors hover:bg-raised focus-visible:bg-raised focus-visible:outline-none"
      >
        <span className="relative mt-0.5 shrink-0 text-muted">
          <Icon className="size-4" />
          {fresh && (
            <span
              data-testid="notification-unread"
              aria-hidden="true"
              className="absolute -top-0.5 -right-0.5 size-1.5 rounded-full bg-accent ring-2 ring-surface"
            />
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline justify-between gap-3">
            <span className="truncate text-xs font-medium text-muted">
              {fresh && <span className="sr-only">Unread: </span>}
              {kindTitle(entry.kind)}
            </span>
            <RelativeTime
              iso={entry.createdAt}
              show={formatAgo}
              className="shrink-0 text-[0.6875rem] text-muted tabular-nums"
            />
          </span>
          <span className="mt-0.5 block truncate text-sm font-medium">{entry.title}</span>
          <span className="block truncate text-xs text-muted">{where}</span>
        </span>
      </button>
    </li>
  )
}

// NotificationsMenu is the toolbar's bell: how many notifications are unread, and a click
// opens the history, newest first. Opening it marks everything read, while the entries
// that were unread stay marked until it closes. An entry opens its item.
export function NotificationsMenu({ notifications, onOpen, onSettings }: NotificationsMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, close, triggerRef } = popover
  const { log, error, markRead, markError, clear } = notifications
  const items = log.items
  const [fresh, setFresh] = useState<ReadonlySet<number>>(new Set())
  const clearing = useMutation(clear)

  const onToggle = () => {
    if (!open) {
      setFresh(new Set(items.filter((n) => !n.read).map((n) => n.id)))
      clearing.reset()

      if (log.unread > 0) {
        markRead()
      }
    }

    toggle()
  }

  const label = bellLabel(log.unread)
  const failure =
    clearing.error !== ''
      ? `Could not clear: ${clearing.error}`
      : markError !== ''
        ? `Could not mark them read: ${markError}`
        : error !== ''
          ? `Could not load the notifications: ${error}`
          : ''

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
        onClick={onToggle}
        className="relative flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text"
      >
        <BellIcon className="size-[1.125rem]" />
        {log.unread > 0 && (
          <span
            data-testid="notifications-unread"
            aria-hidden="true"
            className="absolute top-0.5 right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-[0.625rem] leading-4 font-semibold text-surface tabular-nums"
          >
            {log.unread > 99 ? '99+' : formatCount(log.unread)}
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
          {items.length > 0 && (
            <button
              type="button"
              onClick={() => {
                void clearing.run()
              }}
              className={`ml-auto ${quietButtonClass}`}
            >
              Clear
            </button>
          )}
        </div>
        {failure !== '' && (
          <p role="alert" className="border-b border-border px-4 py-2 text-xs text-danger">
            {failure}
          </p>
        )}
        {items.length === 0 ? (
          <div className="px-4 py-6 text-center">
            <p className="text-sm font-medium">No notifications yet</p>
            <p className="mt-1 text-xs text-muted">
              New review requests, assignments, and snoozes that end early show up here.
            </p>
          </div>
        ) : (
          <ul aria-label="Notification history" className="min-h-0 flex-1 divide-y divide-border overflow-y-auto">
            {items.map((entry) => (
              <Entry
                key={entry.id}
                entry={entry}
                fresh={fresh.has(entry.id) || !entry.read}
                onOpen={() => {
                  close()
                  onOpen(entry)
                }}
              />
            ))}
          </ul>
        )}
        <div className="border-t border-border px-2 py-1.5">
          <button
            type="button"
            onClick={() => {
              close()
              onSettings()
            }}
            className={quietButtonClass}
          >
            <SlidersIcon className="size-3.5" />
            Notification settings
          </button>
        </div>
      </PopoverPanel>
    </div>
  )
}
