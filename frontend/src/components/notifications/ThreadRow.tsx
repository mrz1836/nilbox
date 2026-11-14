import type { ReactNode } from 'react'

import { formatCount } from '../../lib/format'
import { groupWhere, reasonsText, stateWords, type ThreadGroup } from '../../lib/githubThreads'
import { formatAgo, formatFull } from '../../lib/time'
import {
  BellIcon,
  CheckIcon,
  CommentIcon,
  EyeIcon,
  InboxIcon,
  IssueIcon,
  PullRequestIcon,
  ShieldAlertIcon,
  TagIcon,
} from '../icons'
import { RelativeTime } from '../RelativeTime'
import { Spinner } from '../Spinner'
import { rowActionClass } from './EntryRow'

type IconComponent = (props: { className?: string }) => ReactNode

// subjectIcons are the icons of the subjects a notification about no item can have; any
// other shows the bell.
const subjectIcons: Readonly<Record<string, IconComponent>> = {
  Release: TagIcon,
  Discussion: CommentIcon,
  RepositoryVulnerabilityAlert: ShieldAlertIcon,
  RepositoryDependabotAlertsThread: ShieldAlertIcon,
}

// GroupIcon is a line's icon: its item's kind, or what it is about.
function GroupIcon({ group }: { group: ThreadGroup }) {
  const className = 'size-4'

  if (group.item !== null) {
    return group.item.kind === 'pr' ? <PullRequestIcon className={className} /> : <IssueIcon className={className} />
  }

  const Subject =
    (Object.hasOwn(subjectIcons, group.subjectType) ? subjectIcons[group.subjectType] : undefined) ?? BellIcon

  return <Subject className={className} />
}

// stateTones color the chip of an item's state the way GitHub does: merged in the accent,
// closed muted.
const stateTones: Readonly<Record<string, string>> = {
  MERGED: 'border-accent/45 text-text',
  CLOSED: 'border-border text-muted',
}

export interface ThreadRowProps {
  group: ThreadGroup
  // finishing reports the line's Done on its way to GitHub, and reading its Mark read's, or
  // its opening's.
  finishing: boolean
  reading: boolean
  // onOpen opens the line on GitHub and marks it read; onDone marks it done. On the page,
  // onRead marks an unread line read, and onShowInInbox, given only while the inbox lists
  // its item, shows the item there.
  onOpen: () => void
  onDone: () => void
  onRead?: () => void
  onShowInInbox?: (() => void) | undefined
  // compact is the toolbar panel's line, which lists only the unread; the page's line
  // tells read from unread, chips its item's state, and skips rendering while off screen.
  compact?: boolean
}

// ThreadRow is one line of a list of GitHub notifications: why GitHub notified the user,
// when, the title, and where. A click opens it on GitHub, as GitHub's own inbox does, which
// marks it read; its actions, which take the time's place while the pointer or the focus
// is on the line, mark it done, and on the page also mark it read or show its item in the
// inbox. A line on its way dims, and the action it waits on turns while it goes.
export function ThreadRow({
  group,
  finishing,
  reading,
  onOpen,
  onDone,
  onRead,
  onShowInInbox,
  compact = false,
}: ThreadRowProps) {
  const where = groupWhere(group)
  const reasons = reasonsText(group.reasons)
  const busy = finishing || reading
  const unread = group.unread > 0
  const state = compact ? '' : stateWords(group.state)
  const weight = compact ? 'font-medium' : unread ? 'font-semibold' : 'font-normal'

  return (
    <li
      aria-busy={busy || undefined}
      className={`group relative transition-opacity duration-200 ${compact ? '' : 'notification-row'} ${busy ? 'opacity-60' : ''}`}
    >
      <button
        type="button"
        disabled={busy}
        onClick={onOpen}
        title={group.repo === '' ? 'Open on GitHub' : `Open on GitHub · ${group.repo}`}
        className={`flex w-full items-start gap-3 py-2.5 text-left transition-colors hover:bg-raised focus-visible:bg-raised focus-visible:outline-none disabled:cursor-default ${compact ? 'px-4' : 'rounded-md px-3'}`}
      >
        <span className="relative mt-0.5 shrink-0 text-muted">
          <GroupIcon group={group} />
          {!compact && unread && (
            <span
              data-testid="thread-unread"
              aria-hidden="true"
              className="absolute -top-0.5 -right-0.5 size-1.5 rounded-full bg-accent ring-2 ring-surface"
            />
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline justify-between gap-3">
            <span className="truncate text-xs font-medium text-muted">
              {!compact && unread && <span className="sr-only">Unread: </span>}
              {reasons}
              {group.unread > 1 && `${reasons === '' ? '' : ' · '}${formatCount(group.unread)} unread`}
            </span>
            <span title={formatFull(group.updatedAt)} className="shrink-0">
              <RelativeTime
                iso={group.updatedAt}
                show={formatAgo}
                className={`text-[0.6875rem] text-muted tabular-nums ${busy ? 'invisible' : 'group-focus-within:invisible group-hover:invisible'}`}
              />
            </span>
          </span>
          <span className="mt-0.5 flex min-w-0 items-center gap-2">
            <span className={`truncate text-sm ${weight}`}>{group.title}</span>
            {state !== '' && (
              <span
                className={`shrink-0 rounded-full border px-1.5 text-[0.6875rem] leading-4 font-medium ${stateTones[group.state] ?? ''}`}
              >
                {state}
              </span>
            )}
          </span>
          {where !== '' && <span className="block truncate text-xs text-muted">{where}</span>}
        </span>
      </button>
      <span
        className={`absolute top-1.5 right-2 flex items-center gap-0.5 transition-opacity ${busy ? 'opacity-100' : 'opacity-0 group-focus-within:opacity-100 group-hover:opacity-100'}`}
      >
        {onShowInInbox !== undefined && (
          <button
            type="button"
            disabled={busy}
            onClick={onShowInInbox}
            aria-label={`Show in inbox: ${group.title}`}
            title="Show in inbox"
            className={rowActionClass}
          >
            <InboxIcon className="size-4" />
          </button>
        )}
        {onRead !== undefined && unread && (
          <button
            type="button"
            disabled={busy}
            onClick={onRead}
            aria-label={reading ? `Marking read: ${group.title}` : `Mark read: ${group.title}`}
            title="Mark read on GitHub"
            className={rowActionClass}
          >
            {reading ? <Spinner className="size-4" /> : <EyeIcon className="size-4" />}
          </button>
        )}
        <button
          type="button"
          disabled={busy}
          onClick={onDone}
          aria-label={finishing ? `Marking done: ${group.title}` : `Done: ${group.title}`}
          title="Done: clears it from your GitHub inbox"
          className={rowActionClass}
        >
          {finishing ? <Spinner className="size-4" /> : <CheckIcon className="size-4" />}
        </button>
      </span>
    </li>
  )
}
