import type { RefObject } from 'react'

import { counted, formatCount } from '../lib/format'
import type { ThreadItem } from '../lib/githubThreads'
import { scriptsBadgeTitle, type ScriptsBadge } from '../lib/scriptsTab'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import type { NotificationsState } from '../lib/useNotifications'
import type { Account, AppNotification, BudgetGauge, Settings } from '../platform'
import { AccountMenu } from './AccountMenu'
import { BudgetMenu } from './BudgetMenu'
import { GitHubThreadsMenu } from './GitHubThreadsMenu'
import { Logo } from './Logo'
import { NotificationsMenu } from './NotificationsMenu'
import { SearchField, type SearchFieldProps } from './SearchField'

// View is the page the window shows. Settings is opened from the account menu (or ⌘,)
// rather than from a tab.
export type View = 'inbox' | 'worktrees' | 'prompts' | 'scripts' | 'settings'

const tabs: readonly { id: View; label: string }[] = [
  { id: 'inbox', label: 'Inbox' },
  { id: 'worktrees', label: 'Worktrees' },
  { id: 'prompts', label: 'Prompts' },
  { id: 'scripts', label: 'Scripts' },
]

export interface AppHeaderProps {
  view: View
  onViewChange: (view: View) => void
  // account is null until the inbox has loaded, and gauge until the budgets have.
  account: Account | null
  gauge: BudgetGauge | null
  // onBudgetCheck reads every budget from GitHub now; the budget menu asks while it is open.
  onBudgetCheck?: () => void
  // inboxCount is how many items are in the inbox; the Inbox tab shows it when it is not 0.
  inboxCount?: number
  // worktreeCount is how many linked worktrees are on disk, and worktreeAttention how many
  // of them are done on GitHub. The Worktrees tab shows the count when it is not 0, marked
  // when some are done.
  worktreeCount?: number
  worktreeAttention?: number
  // scripts is the Scripts tab's badge while the tab shows, or null while it doesn't: no
  // integration is on (D2).
  scripts?: ScriptsBadge | null
  onOpenSettings: () => void
  // notifications is the notification history the bell shows; without it there is no bell.
  // onOpenNotification shows an entry's item, and onNotificationSettings opens their
  // settings.
  notifications?: NotificationsState
  onOpenNotification?: (entry: AppNotification) => void
  onNotificationSettings?: () => void
  // githubThreads is the user's GitHub notifications, which the button beside the bell
  // shows; without it there is no button. settings are the stored settings, or null until
  // they load, which say whether the app reads the notifications and clears them on its
  // own. onOpenThread shows the item a notification is about, onGitHubThreadsSettings opens
  // their settings, and onSettingsSaved receives the settings as stored after the button
  // turned automatic clearing on.
  githubThreads?: GitHubThreadsState
  settings?: Settings | null
  onOpenThread?: (item: ThreadItem) => void
  onGitHubThreadsSettings?: () => void
  onSettingsSaved?: (settings: Settings) => void
  // search is the current page's filter field, or undefined on a page with none, and
  // searchRef reaches its text field, for ⌘F to focus it.
  search?: SearchFieldProps | undefined
  searchRef?: RefObject<HTMLInputElement | null>
}

// TabCount is what a view tab counts: how many, what that means, whether some of them
// need a look (an amber dot), and whether something failed (a red dot).
interface TabCount {
  count: number
  title: string
  attention: boolean
  failed?: boolean
}

// worktreesTitle explains the Worktrees tab's count.
function worktreesTitle(count: number, attention: number): string {
  const worktrees = counted(count, 'linked worktree', 'linked worktrees')

  return attention > 0 ? `${worktrees}, ${formatCount(attention)} done on GitHub but still on disk` : worktrees
}

// tabCount is the count a tab shows; Prompts and Settings show none. Scripts counts the
// runs going, with a red dot after a run failed until it is opened, and an amber one
// while a tool that's on can't be used.
function tabCount(id: View, props: AppHeaderProps): TabCount {
  const { inboxCount = 0, worktreeCount = 0, worktreeAttention = 0, scripts = null } = props

  switch (id) {
    case 'inbox':
      return {
        count: inboxCount,
        title: counted(inboxCount, 'item in the inbox', 'items in the inbox'),
        attention: false,
      }
    case 'worktrees':
      return {
        count: worktreeCount,
        title: worktreesTitle(worktreeCount, worktreeAttention),
        attention: worktreeAttention > 0,
      }
    case 'scripts':
      return scripts === null
        ? { count: 0, title: '', attention: false }
        : {
            count: scripts.running,
            title: scriptsBadgeTitle(scripts),
            attention: scripts.blocked,
            failed: scripts.failed,
          }
    default:
      return { count: 0, title: '', attention: false }
  }
}

// AppHeader is the window's single toolbar, sitting beside the macOS window controls:
// the logo, the view tabs (Inbox and Worktrees with their counts, Prompts), and on the
// right the page's filter field, the notification bell, the GitHub notifications button,
// the API budget gauge, and the account menu. The filter gives way first as the window narrows; the logo and the tabs
// keep their size. The bar itself drags the window
// (--wails-draggable), double-clicking it zooms, and the controls opt out so they take
// clicks. The left inset clears the traffic lights. The bar's height and that inset line
// it up with the native window controls, which keep their size whatever the text size, so
// both stay in px while everything inside the bar follows the text size.
export function AppHeader(props: AppHeaderProps) {
  const {
    view,
    onViewChange,
    account,
    gauge,
    onBudgetCheck,
    onOpenSettings,
    notifications,
    onOpenNotification = () => undefined,
    onNotificationSettings = onOpenSettings,
    githubThreads,
    settings = null,
    onOpenThread = () => undefined,
    onGitHubThreadsSettings = onOpenSettings,
    onSettingsSaved = () => undefined,
    search,
    searchRef,
  } = props

  return (
    <header className="relative z-30 flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-[80px] select-none [--wails-draggable:drag]">
      <h1 className="flex shrink-0">
        <Logo />
      </h1>
      <nav aria-label="Views" className="flex shrink-0 items-center gap-0.5 [--wails-draggable:no-drag]">
        {tabs.map(({ id, label }) => {
          if (id === 'scripts' && (props.scripts ?? null) === null) {
            return null
          }

          const { count, title, attention, failed = false } = tabCount(id, props)
          const badged = count > 0 || attention || failed

          return (
            <button
              key={id}
              type="button"
              aria-pressed={view === id}
              aria-label={label}
              title={badged ? title : undefined}
              onClick={() => {
                onViewChange(id)
              }}
              className="group flex items-center gap-1.5 rounded-md px-2.5 py-1 text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:bg-raised aria-pressed:text-text"
            >
              {label}
              {/* The pill sits one step above the tab: raised on the bar, border on a
                  hovered or active tab. */}
              {badged && (
                <span
                  data-testid={`${id}-count`}
                  className="flex items-center gap-1 rounded-full bg-raised px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium tabular-nums transition-colors group-hover:bg-border group-aria-pressed:bg-border"
                >
                  {failed && (
                    <span data-testid={`${id}-failed`} aria-hidden="true" className="size-1.5 rounded-full bg-danger" />
                  )}
                  {attention && (
                    <span
                      data-testid={id === 'worktrees' ? 'worktree-attention' : `${id}-attention`}
                      aria-hidden="true"
                      className="size-1.5 rounded-full bg-warning"
                    />
                  )}
                  {count > 0 && formatCount(count)}
                </span>
              )}
            </button>
          )
        })}
      </nav>
      <div className="ml-auto flex min-w-0 items-center gap-1 [--wails-draggable:no-drag]">
        {/* Each page has a field of its own, so its list and caret start afresh. */}
        {search !== undefined && <SearchField key={view} {...search} inputRef={searchRef} />}
        {notifications !== undefined && (
          <NotificationsMenu
            notifications={notifications}
            onOpen={onOpenNotification}
            onSettings={onNotificationSettings}
          />
        )}
        {githubThreads !== undefined && (
          <GitHubThreadsMenu
            threads={githubThreads}
            settings={settings}
            onOpen={onOpenThread}
            onSettings={onGitHubThreadsSettings}
            onSettingsSaved={onSettingsSaved}
          />
        )}
        <BudgetMenu gauge={gauge} onCheck={onBudgetCheck} />
        <AccountMenu account={account} onOpenSettings={onOpenSettings} />
      </div>
    </header>
  )
}
