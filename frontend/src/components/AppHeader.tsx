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
  // worktreeAttention counts the worktrees done on GitHub but still on disk; the
  // Worktrees tab shows it when it is not 0.
  worktreeAttention?: number
  onOpenSettings: () => void
}

// attentionTitle explains the Worktrees tab's count.
function attentionTitle(n: number): string {
  return n === 1
    ? '1 worktree is done on GitHub but still on disk'
    : `${String(n)} worktrees are done on GitHub but still on disk`
}

// AppHeader is the window's single toolbar, sitting beside the macOS window controls:
// the logo, the view tabs (Inbox, Worktrees with the count of worktrees worth a look,
// Prompts), and on the right the API budget gauge and the account menu. The bar itself
// drags the window (--wails-draggable), double-clicking it zooms, and the controls opt
// out so they take clicks. The left inset clears the traffic lights.
export function AppHeader({
  view,
  onViewChange,
  account,
  gauge,
  now,
  worktreeAttention = 0,
  onOpenSettings,
}: AppHeaderProps) {
  return (
    <header className="relative z-30 flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-20 select-none [--wails-draggable:drag]">
      <h1 className="flex">
        <Logo />
      </h1>
      <nav aria-label="Views" className="flex items-center gap-0.5 [--wails-draggable:no-drag]">
        {tabs.map(({ id, label }) => {
          const count = id === 'worktrees' ? worktreeAttention : 0

          return (
            <button
              key={id}
              type="button"
              aria-pressed={view === id}
              aria-label={label}
              title={count > 0 ? attentionTitle(count) : undefined}
              onClick={() => {
                onViewChange(id)
              }}
              className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:bg-raised aria-pressed:text-text"
            >
              {label}
              {count > 0 && (
                <span data-testid="worktree-attention" className="text-xs font-semibold text-warning">
                  {count}
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
