import { counted, formatCount } from '../lib/format'
import type { Account, RateGauge } from '../platform'
import { AccountMenu } from './AccountMenu'
import { BudgetMenu } from './BudgetMenu'
import { Logo } from './Logo'

// View is the page the window shows. Settings is opened from the account menu (or ⌘,)
// rather than from a tab.
export type View = 'inbox' | 'worktrees' | 'prompts' | 'settings'

const tabs: readonly { id: View; label: string }[] = [
  { id: 'inbox', label: 'Inbox' },
  { id: 'worktrees', label: 'Worktrees' },
  { id: 'prompts', label: 'Prompts' },
]

export interface AppHeaderProps {
  view: View
  onViewChange: (view: View) => void
  // account and gauge are null until the inbox has loaded.
  account: Account | null
  gauge: RateGauge | null
  now: Date
  // inboxCount is how many items are in the inbox; the Inbox tab shows it when it is not 0.
  inboxCount?: number
  // worktreeCount is how many linked worktrees are on disk, and worktreeAttention how many
  // of them are done on GitHub. The Worktrees tab shows the count when it is not 0, marked
  // when some are done.
  worktreeCount?: number
  worktreeAttention?: number
  onOpenSettings: () => void
}

// TabCount is what a view tab counts: how many, what that means, and whether some of
// them need a look.
interface TabCount {
  count: number
  title: string
  attention: boolean
}

// worktreesTitle explains the Worktrees tab's count.
function worktreesTitle(count: number, attention: number): string {
  const worktrees = counted(count, 'linked worktree', 'linked worktrees')

  return attention > 0 ? `${worktrees}, ${formatCount(attention)} done on GitHub but still on disk` : worktrees
}

// tabCount is the count a tab shows; Prompts and Settings show none.
function tabCount(id: View, props: AppHeaderProps): TabCount {
  const { inboxCount = 0, worktreeCount = 0, worktreeAttention = 0 } = props

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
    default:
      return { count: 0, title: '', attention: false }
  }
}

// AppHeader is the window's single toolbar, sitting beside the macOS window controls:
// the logo, the view tabs (Inbox and Worktrees with their counts, Prompts), and on the
// right the API budget gauge and the account menu. The bar itself drags the window
// (--wails-draggable), double-clicking it zooms, and the controls opt out so they take
// clicks. The left inset clears the traffic lights.
export function AppHeader(props: AppHeaderProps) {
  const { view, onViewChange, account, gauge, now, onOpenSettings } = props

  return (
    <header className="relative z-30 flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-20 select-none [--wails-draggable:drag]">
      <h1 className="flex">
        <Logo />
      </h1>
      <nav aria-label="Views" className="flex items-center gap-0.5 [--wails-draggable:no-drag]">
        {tabs.map(({ id, label }) => {
          const { count, title, attention } = tabCount(id, props)

          return (
            <button
              key={id}
              type="button"
              aria-pressed={view === id}
              aria-label={label}
              title={count > 0 ? title : undefined}
              onClick={() => {
                onViewChange(id)
              }}
              className="group flex items-center gap-1.5 rounded-md px-2.5 py-1 text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:bg-raised aria-pressed:text-text"
            >
              {label}
              {/* The pill sits one step above the tab: raised on the bar, border on a
                  hovered or active tab. */}
              {count > 0 && (
                <span
                  data-testid={`${id}-count`}
                  className="flex items-center gap-1 rounded-full bg-raised px-1.5 text-[11px] leading-[18px] font-medium tabular-nums transition-colors group-hover:bg-border group-aria-pressed:bg-border"
                >
                  {attention && (
                    <span
                      data-testid="worktree-attention"
                      aria-hidden="true"
                      className="size-1.5 rounded-full bg-warning"
                    />
                  )}
                  {formatCount(count)}
                </span>
              )}
            </button>
          )
        })}
      </nav>
      <div className="ml-auto flex items-center gap-1 [--wails-draggable:no-drag]">
        <BudgetMenu gauge={gauge} now={now} />
        <AccountMenu account={account} onOpenSettings={onOpenSettings} />
      </div>
    </header>
  )
}
