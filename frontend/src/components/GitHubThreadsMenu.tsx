import { useId, useState, type ReactNode } from 'react'

import { brand } from '../generated/brand'
import {
  askAgainText,
  badgeText,
  buttonLabel,
  groupURL,
  groupWhere,
  inboxURL,
  reasonsText,
  refusal,
  summaryText,
  threadGroups,
  type ThreadGroup,
} from '../lib/githubThreads'
import { formatCount } from '../lib/format'
import { formatAgo } from '../lib/time'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import { useNowAs } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import { useSettingsSave } from '../lib/useSettingsSave'
import { openExternal, setGitHubNotificationSwitch, type GitHubThreadsStatus, type Settings } from '../platform'
import { quietButtonClass } from './controls'
import {
  BellIcon,
  CheckIcon,
  CommentIcon,
  ExternalIcon,
  GitHubMarkIcon,
  IssueIcon,
  PullRequestIcon,
  RefreshIcon,
  ShieldAlertIcon,
  SlidersIcon,
  TagIcon,
  XIcon,
} from './icons'
import { PopoverPanel } from './PopoverPanel'
import { RelativeTime } from './RelativeTime'
import { Spinner } from './Spinner'

export interface GitHubThreadsMenuProps {
  // threads is the user's GitHub notifications as the app mirrors them (useGitHubThreads).
  threads: GitHubThreadsState
  // settings are the stored settings, or null until they load: whether the app reads the
  // notifications, which hides the button at once when it doesn't, and whether it clears
  // them on its own.
  settings: Settings | null
  // onSettings opens Settings → GitHub notifications.
  onSettings: () => void
  // onSettingsSaved receives every setting as stored after the menu turned automatic
  // clearing on.
  onSettingsSaved: (settings: Settings) => void
}

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

interface ThreadLineProps {
  group: ThreadGroup
  // finishing reports the line's Done on its way to GitHub, and reading its opening's mark.
  finishing: boolean
  reading: boolean
  // onOpen opens the line on GitHub and marks it read; onDone marks it done.
  onOpen: () => void
  onDone: () => void
}

// ThreadLine is one line of the unread list: why GitHub notified the user, when, the
// title, and where. A click opens it on GitHub, as GitHub's own inbox does, which marks it
// read; its Done, which takes the time's place while the pointer or the focus is on the
// line, marks it done. A line on its way dims, and its Done turns while it goes.
function ThreadLine({ group, finishing, reading, onOpen, onDone }: ThreadLineProps) {
  const where = groupWhere(group)
  const reasons = reasonsText(group.reasons)
  const busy = finishing || reading

  return (
    <li
      aria-busy={busy || undefined}
      className={`group relative transition-opacity duration-200 ${busy ? 'opacity-60' : ''}`}
    >
      <button
        type="button"
        disabled={busy}
        onClick={onOpen}
        title={group.repo === '' ? 'Open on GitHub' : `Open on GitHub · ${group.repo}`}
        className="flex w-full items-start gap-3 px-4 py-2.5 text-left transition-colors hover:bg-raised focus-visible:bg-raised focus-visible:outline-none disabled:cursor-default"
      >
        <span className="mt-0.5 shrink-0 text-muted">
          <GroupIcon group={group} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline justify-between gap-3">
            <span className="truncate text-xs font-medium text-muted">
              {reasons}
              {group.count > 1 && `${reasons === '' ? '' : ' · '}${formatCount(group.count)} unread`}
            </span>
            <RelativeTime
              iso={group.updatedAt}
              show={formatAgo}
              className={`shrink-0 text-[0.6875rem] text-muted tabular-nums ${finishing ? 'invisible' : 'group-focus-within:invisible group-hover:invisible'}`}
            />
          </span>
          <span className="mt-0.5 block truncate text-sm font-medium">{group.title}</span>
          {where !== '' && <span className="block truncate text-xs text-muted">{where}</span>}
        </span>
      </button>
      <button
        type="button"
        disabled={busy}
        onClick={onDone}
        aria-label={finishing ? `Marking done: ${group.title}` : `Done: ${group.title}`}
        title="Done: clears it from your GitHub inbox"
        className={`absolute top-1.5 right-2 flex size-7 items-center justify-center rounded-md text-muted transition hover:bg-surface hover:text-text focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none disabled:cursor-default ${finishing ? 'opacity-100' : 'opacity-0 group-focus-within:opacity-100 group-hover:opacity-100'}`}
      >
        {finishing ? <Spinner className="size-4" /> : <CheckIcon className="size-4" />}
      </button>
    </li>
  )
}

interface RefusedProps {
  status: GitHubThreadsStatus
}

// Refused says GitHub refuses the token the notifications, and when the app asks again; the
// header's refresh button asks now.
function Refused({ status }: RefusedProps) {
  const askAgain = useNowAs((now) => askAgainText(now, status))

  return (
    <div className="border-b border-border px-4 py-2.5">
      <p className="text-xs font-medium text-warning">{refusal(status)}</p>
      {askAgain !== '' && <p className="mt-0.5 text-xs text-muted">{askAgain}</p>}
    </div>
  )
}

interface TurnOnClearingProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // onTurnedOn checks GitHub once clearing is on.
  onTurnedOn: () => void
}

// TurnOnClearing offers to clear notifications on their own, and turns that on through the
// window's settings queue (useSettingsSave), so what it stores never replaces a newer change
// made in Settings; then it checks GitHub.
function TurnOnClearing({ settings: stored, onSaved, onTurnedOn }: TurnOnClearingProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const [pending, setPending] = useState(false)

  const turnOn = async () => {
    setPending(true)

    const result = await save({ githubNotifications: { ...settings.githubNotifications, auto: true } }, () =>
      setGitHubNotificationSwitch('auto', true),
    )

    setPending(false)

    if (result.ok) {
      onTurnedOn()
    }
  }

  return (
    <div className="border-b border-border px-4 py-2.5">
      <p className="text-xs text-muted">
        {brand.name} can clear a notification on GitHub once the work behind it is done, such as a pull request merged
        or your review submitted.
      </p>
      {error !== '' && (
        <p role="alert" className="mt-1 text-xs text-danger">
          Could not turn on automatic clearing: {error}
        </p>
      )}
      <button
        type="button"
        disabled={pending}
        onClick={() => {
          void turnOn()
        }}
        className={`mt-1.5 ${quietButtonClass}`}
      >
        {pending ? 'Turning on…' : 'Turn on automatic clearing'}
      </button>
    </div>
  )
}

// GitHubThreadsMenu is the toolbar's GitHub notifications button, beside the bell: the
// mark, with the count of the unread notifications in the user's GitHub inbox up to 99+,
// none at none, or a warning dot while GitHub refuses the token them. It isn't there while
// the app doesn't read them, as the settings say before the mirror does, and its panel
// closes as it goes. A click opens the counts and when GitHub was last checked, with a
// button that checks it now; why they can't be read; the offer to clear them on their own;
// and the newest unread, grouped by item. A line opens on GitHub and is marked read there,
// as GitHub's own inbox does, and its Done marks it done. Opening the panel reads the app's
// own mirror again, from the database: it sends GitHub nothing and marks nothing read.
export function GitHubThreadsMenu({ threads, settings, onSettings, onSettingsSaved }: GitHubThreadsMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, close, triggerRef } = popover
  const { snapshot, reload, checkNow, checking, checkError } = threads
  const { markDone, markRead, finishing, reading, markError, dismissMarkError } = threads
  const summary = useNowAs((now) => (snapshot === null ? '' : summaryText(now, snapshot.status, checking)))
  const hidden = settings?.githubNotifications.read === false || snapshot === null || !snapshot.status.reading

  // A panel open as the button goes closes, so it never opens again by itself when the
  // button comes back.
  if (hidden && open) {
    close()
  }

  if (hidden) {
    return null
  }

  const { status } = snapshot
  const refused = status.access === 'refused'
  const groups = threadGroups(snapshot.unread)
  const label = buttonLabel(status)

  const onToggle = () => {
    if (!open) {
      reload()
    }

    toggle()
  }

  const check = () => {
    void checkNow()
  }

  // openLine opens a line on GitHub, at a link built from its parts, and marks its
  // notifications read there, as GitHub's own inbox does.
  const openLine = (group: ThreadGroup) => {
    openExternal(groupURL(group))
    void markRead(group.ids)
  }

  const holds = (set: ReadonlySet<number>, group: ThreadGroup) => group.ids.some((threadID) => set.has(threadID))

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
        <GitHubMarkIcon className="size-[1.125rem]" />
        {refused ? (
          <span
            data-testid="github-threads-refused"
            aria-hidden="true"
            className="absolute top-1 right-1 size-2 rounded-full bg-warning ring-2 ring-surface"
          />
        ) : (
          status.unread > 0 && (
            <span
              data-testid="github-threads-count"
              aria-hidden="true"
              className="absolute top-0.5 right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-[0.625rem] leading-4 font-semibold text-surface tabular-nums"
            >
              {badgeText(status.unread)}
            </span>
          )
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
        <div className="flex items-start gap-3 border-b border-border py-2.5 pr-2 pl-4">
          <div className="min-w-0 flex-1">
            <h2 id={`${id}-title`} className="text-sm font-semibold">
              GitHub notifications
            </h2>
            <p className="mt-0.5 truncate text-xs text-muted tabular-nums">{summary}</p>
          </div>
          <button
            type="button"
            disabled={checking}
            onClick={check}
            aria-label={checking ? 'Checking GitHub' : 'Check GitHub now'}
            title={checking ? 'Checking GitHub…' : 'Check GitHub now'}
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text disabled:cursor-default disabled:hover:bg-transparent"
          >
            <RefreshIcon className={`size-4 ${checking ? 'text-accent motion-safe:animate-spin' : ''}`} />
          </button>
        </div>
        {refused ? (
          <Refused status={status} />
        ) : (
          status.lastError !== '' && (
            <p className="border-b border-border px-4 py-2.5 text-xs text-warning">{status.lastError}</p>
          )
        )}
        {checkError !== '' && (
          <p role="alert" className="border-b border-border px-4 py-2 text-xs text-danger">
            Could not check GitHub: {checkError}
          </p>
        )}
        {markError !== '' && (
          <div
            role="alert"
            className="flex items-start gap-2 border-b border-border py-2 pr-2 pl-4 text-xs text-danger"
          >
            <p className="min-w-0 flex-1">{markError}</p>
            <button
              type="button"
              onClick={dismissMarkError}
              aria-label="Dismiss"
              className="flex size-5 shrink-0 items-center justify-center rounded text-muted hover:text-text"
            >
              <XIcon className="size-3.5" />
            </button>
          </div>
        )}
        {settings !== null && !settings.githubNotifications.auto && !refused && (
          <TurnOnClearing settings={settings} onSaved={onSettingsSaved} onTurnedOn={check} />
        )}
        {groups.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm text-muted">Nothing unread</p>
        ) : (
          <ul aria-label="Unread on GitHub" className="min-h-0 flex-1 divide-y divide-border overflow-y-auto">
            {groups.map((group) => (
              <ThreadLine
                key={group.key}
                group={group}
                finishing={holds(finishing, group)}
                reading={holds(reading, group)}
                onOpen={() => {
                  openLine(group)
                }}
                onDone={() => {
                  void markDone(group.ids)
                }}
              />
            ))}
          </ul>
        )}
        <div className="flex items-center gap-1 border-t border-border px-2 py-1.5">
          <button
            type="button"
            onClick={() => {
              close()
              openExternal(inboxURL)
            }}
            className={quietButtonClass}
          >
            <ExternalIcon className="size-3.5" />
            Open on GitHub
          </button>
          <button
            type="button"
            onClick={() => {
              close()
              onSettings()
            }}
            className={`ml-auto ${quietButtonClass}`}
          >
            <SlidersIcon className="size-3.5" />
            Settings
          </button>
        </div>
      </PopoverPanel>
    </div>
  )
}
