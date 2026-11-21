import type { ReactNode } from 'react'

import { entryHeading, entryTitle, entryWhere } from '../../lib/notifications'
import { formatAgo, formatFull } from '../../lib/time'
import type { AppNotification, NotificationKind } from '../../platform'
import {
  AssignIcon,
  AutopilotIcon,
  BellIcon,
  CheckIcon,
  CircleCheckIcon,
  ClockIcon,
  DotIcon,
  EyeIcon,
  PulseIcon,
  TagIcon,
  UpdateIcon,
  XIcon,
} from '../icons'
import { RelativeTime } from '../RelativeTime'

// kindIcons are each kind's icon in the history, Autopilot's all its bolt. A kind this
// build does not know, as another build may have stored, shows the bell.
export const kindIcons: Readonly<Partial<Record<NotificationKind, (props: { className?: string }) => ReactNode>>> = {
  review_requested: EyeIcon,
  assigned: AssignIcon,
  woke: ClockIcon,
  release_done: TagIcon,
  release_published: CircleCheckIcon,
  release_failed: XIcon,
  run_failed: XIcon,
  service_status: PulseIcon,
  update_ready: UpdateIcon,
  autopilot_alert: AutopilotIcon,
  autopilot_needs_you: AutopilotIcon,
  autopilot_finished: AutopilotIcon,
  autopilot_checks: AutopilotIcon,
  autopilot_problem: AutopilotIcon,
}

// kindTones color the icon of a kind that tells of something gone wrong, as the Runs page
// marks a failed run, or of one that waits for the user; every other kind's icon is muted.
export const kindTones: Readonly<Partial<Record<NotificationKind, string>>> = {
  release_failed: 'text-danger',
  run_failed: 'text-danger',
  autopilot_needs_you: 'text-warning',
  autopilot_problem: 'text-danger',
}

// rowActionClass is a row's action button, which takes the time's place while the pointer
// or the focus is on the row.
export const rowActionClass =
  'flex size-7 items-center justify-center rounded-md text-muted transition hover:bg-surface hover:text-text focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none disabled:cursor-default'

// rowActionsClass shows a row's actions only while the pointer or the focus is on it.
const rowActionsClass =
  'absolute top-1.5 right-2 flex items-center gap-0.5 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100'

export interface EntryRowProps {
  entry: AppNotification
  // onOpen opens the entry and marks it read; onDone marks it read, which takes it off the
  // bell; onUnread, on the page, marks a read entry unread, which puts it back there.
  onOpen: () => void
  onDone: () => void
  onUnread?: () => void
  // compact is the bell's row, which lists only the unread; the page's row tells read from
  // unread.
  compact?: boolean
}

// EntryRow is one notification of the app's own: what happened, to which item, and when.
// A click opens it, which marks it read. While the pointer or the focus is on it, its time
// gives way to Done (✓, which marks it read and keeps it in the history), or, on the page,
// for a read entry, Mark unread. An unread entry carries a dot, and on the page a bolder
// title.
export function EntryRow({ entry, onOpen, onDone, onUnread, compact = false }: EntryRowProps) {
  const Icon = kindIcons[entry.kind] ?? BellIcon
  const tone = kindTones[entry.kind] ?? 'text-muted'
  const where = entryWhere(entry)
  const unread = !entry.read
  const weight = compact ? 'font-medium' : unread ? 'font-semibold' : 'font-normal'

  return (
    <li className="group relative">
      <button
        type="button"
        onClick={onOpen}
        title={entryTitle(entry)}
        className={`flex w-full items-start gap-3 py-2.5 text-left transition-colors hover:bg-raised focus-visible:bg-raised focus-visible:outline-none ${compact ? 'px-4' : 'rounded-md px-3'}`}
      >
        <span className={`relative mt-0.5 shrink-0 ${tone}`}>
          <Icon className="size-4" />
          {unread && (
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
              {unread && <span className="sr-only">Unread: </span>}
              {entryHeading(entry)}
            </span>
            <span title={formatFull(entry.createdAt)} className="shrink-0">
              <RelativeTime
                iso={entry.createdAt}
                show={formatAgo}
                className="text-caption text-muted tabular-nums group-focus-within:invisible group-hover:invisible"
              />
            </span>
          </span>
          <span className={`mt-0.5 block truncate text-sm ${weight}`}>{entry.title}</span>
          {where !== '' && <span className="block truncate text-xs text-muted">{where}</span>}
        </span>
      </button>
      <span className={rowActionsClass}>
        {unread ? (
          <button
            type="button"
            onClick={onDone}
            aria-label={`Done: ${entry.title}`}
            title="Done · it stays in your history"
            className={rowActionClass}
          >
            <CheckIcon className="size-4" />
          </button>
        ) : (
          onUnread !== undefined && (
            <button
              type="button"
              onClick={onUnread}
              aria-label={`Mark unread: ${entry.title}`}
              title="Mark unread · it goes back on the bell"
              className={rowActionClass}
            >
              <DotIcon className="size-4" />
            </button>
          )
        )}
      </span>
    </li>
  )
}
