import { useState } from 'react'

import { brand } from '../../generated/brand'
import {
  filterThreads,
  githubTypeOrder,
  githubTypes,
  groupURL,
  inboxURL,
  summaryText,
  threadGroups,
  type GitHubShow,
  type GitHubTypeID,
  type ThreadGroup,
} from '../../lib/githubThreads'
import { filterGitHubThreads } from '../../lib/notificationFilter'
import type { ListedItem } from '../../lib/triage'
import type { GitHubThreadsState } from '../../lib/useGitHubThreads'
import { useNowAs } from '../../lib/useNow'
import { useSettingsSave } from '../../lib/useSettingsSave'
import { openExternal, setGitHubNotificationSwitch, type Settings } from '../../platform'
import { buttonClass, quietButtonClass, Segmented } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { ExternalIcon, RefreshIcon, SlidersIcon, XIcon } from '../icons'
import { Spinner } from '../Spinner'
import { ClearedList } from './ClearedList'
import { DayGroups } from './DayGroups'
import { Refused, TurnOnClearing } from './GitHubNotes'
import { NoMatches, PageNote, Toolbar } from './parts'
import { ThreadRow } from './ThreadRow'

// showOptions and typeOptions are the tab's Show and Type, as the segmented controls offer
// them.
const showOptions: readonly { value: GitHubShow; label: string }[] = [
  { value: 'unread', label: 'Unread' },
  { value: 'all', label: 'All' },
  { value: 'cleared', label: 'Cleared' },
]

const typeOptions = githubTypeOrder.map((id) => ({ value: id, label: githubTypes[id].label }))

// noteClass is where a note about reading sits on the page: a band under the header.
const noteClass = 'border-b border-border px-4 py-2.5'

interface TurnOnReadingProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // onTurnedOn checks GitHub once reading is on.
  onTurnedOn: () => void
}

// TurnOnReading says the app doesn't read the user's GitHub notifications, and turns that
// on through the window's settings queue (useSettingsSave), then checks GitHub.
function TurnOnReading({ settings: stored, onSaved, onTurnedOn }: TurnOnReadingProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const [pending, setPending] = useState(false)

  const turnOn = async () => {
    setPending(true)

    const result = await save({ githubNotifications: { ...settings.githubNotifications, read: true } }, () =>
      setGitHubNotificationSwitch('read', true),
    )

    setPending(false)

    if (result.ok) {
      onTurnedOn()
    }
  }

  return (
    <PageNote title={`${brand.name} isn’t reading your GitHub notifications`}>
      <p>Turn it on to see them here, with the toolbar’s count and automatic clearing.</p>
      {error !== '' && (
        <p role="alert" className="mt-1 text-danger">
          Could not turn it on: {error}
        </p>
      )}
      <button
        type="button"
        disabled={pending}
        onClick={() => {
          void turnOn()
        }}
        className={`mt-3 ${buttonClass}`}
      >
        {pending ? 'Turning on…' : 'Turn on'}
      </button>
    </PageNote>
  )
}

export interface GitHubNotificationsTabProps {
  threads: GitHubThreadsState
  // settings are the stored settings, or null until they load: whether the app reads the
  // notifications and clears them on its own.
  settings: Settings | null
  // show and type are the tab's Show and Type, which the window holds, so the toolbar's
  // field counts what the list shows; onShow and onType hear each choice.
  show: GitHubShow
  type: GitHubTypeID
  onShow: (show: GitHubShow) => void
  onType: (type: GitHubTypeID) => void
  // query is the toolbar's field, and onClearQuery empties it.
  query: string
  onClearQuery: () => void
  // listed is the inbox's items by itemKey, or null until the inbox loads; onShowItem
  // shows one of them in the inbox.
  listed: ReadonlyMap<string, ListedItem> | null
  onShowItem: (itemId: number) => void
  // onSettings opens Settings → GitHub notifications, and onSettingsSaved receives the
  // settings as stored after the tab turned reading or clearing on.
  onSettings: () => void
  onSettingsSaved: (settings: Settings) => void
}

// GitHubNotificationsTab is the Notifications page's tab of the user's GitHub
// notifications, as the app mirrors them: every one, read and unread, grouped by item and
// by day, narrowed by Show, Type, and the toolbar's field, with the counts, when GitHub was
// last checked, and Check now. A line opens on GitHub and is marked read there; while the
// pointer or the focus is on it, it offers Show in inbox (while the inbox lists its item),
// Mark read, and Done. Cleared shows what clearing is about to do, with Undo, and what it
// did. Every mark goes to GitHub at once, one line at a time: there is no bulk mark.
export function GitHubNotificationsTab({
  threads,
  settings,
  show,
  type,
  onShow,
  onType,
  query,
  onClearQuery,
  listed,
  onShowItem,
  onSettings,
  onSettingsSaved,
}: GitHubNotificationsTabProps) {
  const { snapshot, checkNow, checking, checkError, list, listError, keep } = threads
  const { markDone, markRead, finishing, reading, markError, dismissMarkError } = threads
  const summary = useNowAs((now) => (snapshot === null ? '' : summaryText(now, snapshot.status, checking)))
  const off = settings?.githubNotifications.read === false || snapshot?.status.reading === false

  const check = () => {
    void checkNow()
  }

  if (off) {
    return settings === null ? (
      <PageNote title={`${brand.name} isn’t reading your GitHub notifications`} />
    ) : (
      <TurnOnReading settings={settings} onSaved={onSettingsSaved} onTurnedOn={check} />
    )
  }

  const status = snapshot?.status ?? null
  const refused = status?.access === 'refused'
  const kept = filterThreads(list?.threads ?? [], show, type)
  const matched = filterGitHubThreads(kept, query)
  const groups = matched?.groups ?? threadGroups(kept)
  const holds = (set: ReadonlySet<number>, group: ThreadGroup) => group.ids.some((id) => set.has(id))

  // openLine opens a line on GitHub, at a link built from its parts, and marks its unread
  // notifications read there, as GitHub's own inbox does.
  const openLine = (group: ThreadGroup) => {
    openExternal(groupURL(group))

    if (group.unreadIds.length > 0) {
      void markRead(group.unreadIds)
    }
  }

  // empty says why the list is empty: not read yet, a filter that matches nothing, nothing
  // unread, or nothing of the type.
  const empty = () => {
    if (status !== null && status.lastPassAt === '') {
      return (
        <PageNote title="Your GitHub notifications haven’t been read yet">
          <button type="button" disabled={checking} onClick={check} className={`mt-2 ${buttonClass}`}>
            <RefreshIcon className="size-3.5" />
            {checking ? 'Checking…' : 'Check now'}
          </button>
        </PageNote>
      )
    }

    if (matched !== null) {
      return <NoMatches onClear={onClearQuery} />
    }

    if (show === 'unread' && type === 'all') {
      return <PageNote title="You’re all caught up">Show All to see what’s read in your GitHub inbox.</PageNote>
    }

    return <PageNote title={githubTypes[type].none} />
  }

  const lines = () => {
    if (list === null) {
      return listError === '' ? (
        <p role="status" className="flex items-center gap-2 px-3 py-3 text-sm text-muted">
          <Spinner />
          Listing your GitHub notifications…
        </p>
      ) : null
    }

    if (groups.length === 0) {
      return empty()
    }

    return (
      <DayGroups
        items={groups}
        when={(group) => group.updatedAt}
        render={(group) => {
          const inInbox = listed?.get(group.key)

          return (
            <ThreadRow
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
              onRead={() => {
                void markRead(group.unreadIds)
              }}
              onShowInInbox={
                inInbox === undefined
                  ? undefined
                  : () => {
                      onShowItem(inInbox.item.id)
                    }
              }
            />
          )
        }}
      />
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-border py-2 pr-2 pl-4">
        <p className="min-w-0 flex-1 truncate text-xs text-muted tabular-nums">{summary}</p>
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
        <button
          type="button"
          onClick={() => {
            openExternal(inboxURL)
          }}
          className={quietButtonClass}
        >
          <ExternalIcon className="size-3.5" />
          Open on GitHub
        </button>
        <button type="button" onClick={onSettings} className={quietButtonClass}>
          <SlidersIcon className="size-3.5" />
          Settings
        </button>
      </div>
      {status !== null &&
        (refused ? (
          <Refused status={status} className={noteClass} />
        ) : (
          status.lastError !== '' && <p className={`${noteClass} text-xs text-warning`}>{status.lastError}</p>
        ))}
      {status?.truncated === true && (
        <p className={`${noteClass} text-xs text-muted`}>
          GitHub holds more than 1,000 notifications for you. {brand.name} reads the newest 1,000.
        </p>
      )}
      {checkError !== '' && (
        <p role="alert" className={`${noteClass} text-xs text-danger`}>
          Could not check GitHub: {checkError}
        </p>
      )}
      {markError !== '' && (
        <div role="alert" className="flex items-start gap-2 border-b border-border py-2 pr-2 pl-4 text-xs text-danger">
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
      <Toolbar>
        <Segmented label="Show" size="sm" value={show} options={showOptions} onChange={onShow} />
        {show !== 'cleared' && (
          <Segmented label="Type" size="sm" value={type} options={typeOptions} onChange={onType} />
        )}
      </Toolbar>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {listError !== '' && show !== 'cleared' && (
          <ErrorBanner className="mx-2 mt-3">Could not list your GitHub notifications: {listError}</ErrorBanner>
        )}
        {show === 'cleared' ? (
          <>
            {settings !== null && !settings.githubNotifications.auto && !refused && (
              <TurnOnClearing
                settings={settings}
                onSaved={onSettingsSaved}
                onTurnedOn={check}
                className="mx-2 mt-3 rounded-lg border border-border bg-surface px-4 py-3"
              />
            )}
            <ClearedList queued={snapshot?.queued ?? []} recent={snapshot?.recent ?? []} keep={keep} />
          </>
        ) : (
          lines()
        )}
      </div>
    </div>
  )
}
