import { brand } from '../generated/brand'
import type { Account, RateGauge } from '../platform'
import { AccountMenu } from './AccountMenu'
import { BudgetMenu } from './BudgetMenu'

// View is the page the window shows. Settings is opened from the account menu (or ⌘,)
// rather than from a tab.
export type View = 'inbox' | 'worktrees' | 'settings'

const tabs: readonly { id: View; label: string }[] = [
  { id: 'inbox', label: 'Inbox' },
  { id: 'worktrees', label: 'Worktrees' },
]

export interface AppHeaderProps {
  view: View
  onViewChange: (view: View) => void
  // account and gauge are null until the inbox has loaded.
  account: Account | null
  gauge: RateGauge | null
  now: Date
  onOpenSettings: () => void
}

// AppHeader is the window's single toolbar, sitting beside the macOS window controls:
// the product name, the view tabs, and on the right the API budget gauge and the account
// menu. The bar itself drags the window (--wails-draggable), double-clicking it zooms,
// and the controls opt out so they take clicks. The left inset clears the traffic lights.
export function AppHeader({ view, onViewChange, account, gauge, now, onOpenSettings }: AppHeaderProps) {
  return (
    <header className="relative z-30 flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-20 select-none [--wails-draggable:drag]">
      <h1 className="text-[15px] font-semibold tracking-tight">{brand.name}</h1>
      <nav aria-label="Views" className="flex items-center gap-0.5 [--wails-draggable:no-drag]">
        {tabs.map(({ id, label }) => (
          <button
            key={id}
            type="button"
            aria-pressed={view === id}
            onClick={() => {
              onViewChange(id)
            }}
            className="rounded-md px-2.5 py-1 text-sm font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:bg-raised aria-pressed:text-text"
          >
            {label}
          </button>
        ))}
      </nav>
      <div className="ml-auto flex items-center gap-1 [--wails-draggable:no-drag]">
        <BudgetMenu gauge={gauge} now={now} />
        <AccountMenu account={account} onOpenSettings={onOpenSettings} />
      </div>
    </header>
  )
}
