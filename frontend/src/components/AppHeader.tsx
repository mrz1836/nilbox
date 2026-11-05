import { brand } from '../generated/brand'

export type View = 'inbox' | 'worktrees'

const views: readonly { id: View; label: string }[] = [
  { id: 'inbox', label: 'Inbox' },
  { id: 'worktrees', label: 'Worktrees' },
]

export interface AppHeaderProps {
  view: View
  onViewChange: (view: View) => void
  settingsOpen: boolean
  onToggleSettings: () => void
  // settingsPanelId is the id of the settings panel the toggle opens.
  settingsPanelId: string
}

// AppHeader shows the product identity and the view switcher. The top strip sits under
// the hidden macOS title bar, where the window controls live and a click starts a window
// drag, so it carries no controls; the buttons sit in the row below it.
export function AppHeader({ view, onViewChange, settingsOpen, onToggleSettings, settingsPanelId }: AppHeaderProps) {
  return (
    <header className="border-b border-border bg-surface">
      <div className="flex h-[52px] items-center gap-3 pr-4 pl-20 select-none">
        <h1 className="text-base font-semibold tracking-tight">{brand.name}</h1>
        <p className="truncate text-sm text-muted">{brand.tagline}</p>
      </div>
      <div className="flex items-center gap-2 px-4 pb-2">
        <nav aria-label="Views" className="flex gap-1">
          {views.map(({ id, label }) => (
            <button
              key={id}
              type="button"
              aria-pressed={view === id}
              onClick={() => {
                onViewChange(id)
              }}
              className="rounded-md px-3 py-1 text-sm font-medium text-muted hover:bg-raised hover:text-text aria-pressed:bg-raised aria-pressed:text-text"
            >
              {label}
            </button>
          ))}
        </nav>
        <button
          type="button"
          aria-expanded={settingsOpen}
          aria-controls={settingsPanelId}
          onClick={onToggleSettings}
          className="ml-auto rounded-md px-3 py-1 text-sm text-muted hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text"
        >
          Settings
        </button>
      </div>
    </header>
  )
}
