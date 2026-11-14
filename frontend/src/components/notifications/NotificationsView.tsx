import { useId } from 'react'

import { brand } from '../../generated/brand'
import { formatCount } from '../../lib/format'
import type { NotificationLists } from '../../lib/notifications'
import { itemsByRef } from '../../lib/triage'
import type { GitHubThreadsState } from '../../lib/useGitHubThreads'
import type { NotificationsState } from '../../lib/useNotifications'
import type { AppNotification, InboxSnapshot, NotificationsTab, Settings } from '../../platform'
import { BellIcon, GitHubMarkIcon } from '../icons'
import { TabPanel, Tabs } from '../Tabs'
import { AppNotificationsTab } from './AppNotificationsTab'
import { GitHubNotificationsTab } from './GitHubNotificationsTab'

export interface NotificationsViewProps {
  // tab is the tab shown, and onTab hears the user choose one, for the window to remember.
  tab: NotificationsTab
  onTab: (tab: NotificationsTab) => void
  // lists are each tab's Show and Type, and onLists hears a change to them; the window
  // holds them, so the toolbar's field counts what the list shows.
  lists: NotificationLists
  onLists: (lists: NotificationLists) => void
  // query is the toolbar's field, and onClearQuery empties it.
  query: string
  onClearQuery: () => void
  notifications: NotificationsState
  githubThreads: GitHubThreadsState
  // settings are the stored settings, or null until they load, and onSettingsSaved
  // receives them as stored after the page turned reading or clearing on.
  settings: Settings | null
  onSettingsSaved: (settings: Settings) => void
  // snapshot is the inbox, or null until it loads: which of a GitHub notification's items
  // it lists, to show there (onShowItem).
  snapshot: InboxSnapshot | null
  onShowItem: (itemId: number) => void
  // onOpen opens one of the app's own entries and marks it read.
  onOpen: (entry: AppNotification) => void
  // onNotificationSettings and onGitHubSettings open each tab's settings.
  onNotificationSettings: () => void
  onGitHubSettings: () => void
}

// countText is a tab's count of the unread, or '' for none.
function countText(n: number): string {
  return n > 0 ? formatCount(n) : ''
}

// NotificationsView is the Notifications page: the app's own notifications, the whole
// history the bell is a quick view of, and the user's GitHub notifications, every one the
// app mirrors. Each tab counts its unread, and the page opens on the one last chosen
// unless a quick view's View all asks for the other.
export default function NotificationsView({
  tab,
  onTab,
  lists,
  onLists,
  query,
  onClearQuery,
  notifications,
  githubThreads,
  settings,
  onSettingsSaved,
  snapshot,
  onShowItem,
  onOpen,
  onNotificationSettings,
  onGitHubSettings,
}: NotificationsViewProps) {
  const id = useId()
  const status = githubThreads.snapshot?.status
  const reading = settings?.githubNotifications.read !== false && status?.reading === true
  const tabs = [
    {
      value: 'app' as const,
      label: brand.name,
      icon: <BellIcon className="size-4" />,
      count: countText(notifications.log.unread),
    },
    {
      value: 'github' as const,
      label: 'GitHub',
      icon: <GitHubMarkIcon className="size-4" />,
      count: reading ? countText(status.unread) : '',
    },
  ]

  return (
    <div className="flex h-full min-h-0 flex-col">
      <Tabs
        baseId={id}
        label="Notifications"
        tabs={tabs}
        value={tab}
        onChange={onTab}
        className="flex shrink-0 items-end gap-1 border-b border-border px-4 pt-3"
      />
      <TabPanel baseId={id} value={tab} className="flex min-h-0 flex-1 flex-col">
        {tab === 'app' ? (
          <AppNotificationsTab
            notifications={notifications}
            show={lists.app.show}
            type={lists.app.type}
            onShow={(show) => {
              onLists({ ...lists, app: { ...lists.app, show } })
            }}
            onType={(type) => {
              onLists({ ...lists, app: { ...lists.app, type } })
            }}
            query={query}
            onClearQuery={onClearQuery}
            onOpen={onOpen}
            onSettings={onNotificationSettings}
          />
        ) : (
          <GitHubNotificationsTab
            threads={githubThreads}
            settings={settings}
            show={lists.github.show}
            type={lists.github.type}
            onShow={(show) => {
              onLists({ ...lists, github: { ...lists.github, show } })
            }}
            onType={(type) => {
              onLists({ ...lists, github: { ...lists.github, type } })
            }}
            query={query}
            onClearQuery={onClearQuery}
            listed={snapshot === null ? null : itemsByRef(snapshot)}
            onShowItem={onShowItem}
            onSettings={onGitHubSettings}
            onSettingsSaved={onSettingsSaved}
          />
        )}
      </TabPanel>
    </div>
  )
}
