import { useId } from 'react'

import { buttonLabel, groupURL, inboxURL, summaryText, threadGroups, type ThreadGroup } from '../lib/githubThreads'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import { useNowAs } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import { openExternal, type Settings } from '../platform'
import { quietButtonClass } from './controls'
import { ExternalIcon, GitHubMarkIcon, HistoryIcon, RefreshIcon, SlidersIcon, XIcon } from './icons'
import { Refused, TurnOnClearing } from './notifications/GitHubNotes'
import { ThreadRow } from './notifications/ThreadRow'
import { CountDot, PopoverPanel, ToolbarPopoverButton } from './PopoverPanel'

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
  // onViewAll opens the Notifications page on its GitHub tab; without it there is no View
  // all.
  onViewAll?: (() => void) | undefined
}

// GitHubThreadsMenu is the toolbar's GitHub notifications button, beside the bell: the
// mark, with the count of the unread notifications in the user's GitHub inbox up to 99+,
// none at none, or a warning dot while GitHub refuses the token them. It isn't there while
// the app doesn't read them, as the settings say before the mirror does, and its panel
// closes as it goes. A click opens the counts and when GitHub was last checked, with a
// button that checks it now; why they can't be read; the offer to clear them on their own;
// and the newest unread, grouped by item. A line opens on GitHub and is marked read there,
// as GitHub's own inbox does, and its Done marks it done. View all opens the Notifications
// page's GitHub tab, which lists every notification. Opening the panel reads the app's own
// mirror again, from the database: it sends GitHub nothing and marks nothing read.
export function GitHubThreadsMenu({
  threads,
  settings,
  onSettings,
  onSettingsSaved,
  onViewAll,
}: GitHubThreadsMenuProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel' })
  const { open, toggle, close } = popover
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
      <ToolbarPopoverButton
        popover={popover}
        id={id}
        label={label}
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
          <CountDot count={status.unread} testId="github-threads-count" />
        )}
      </ToolbarPopoverButton>
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="z-40 flex max-h-[min(32rem,calc(100vh-5rem))] w-96 flex-col outline-none"
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
              <ThreadRow
                key={group.key}
                compact
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
          {onViewAll !== undefined && (
            <button
              type="button"
              onClick={() => {
                close()
                onViewAll()
              }}
              className={quietButtonClass}
            >
              <HistoryIcon className="size-3.5" />
              View all
            </button>
          )}
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
