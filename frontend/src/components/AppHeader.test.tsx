import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { AppHeader, type AppHeaderProps } from './AppHeader'

function renderHeader(overrides: Partial<AppHeaderProps> = {}) {
  const props: AppHeaderProps = {
    view: 'inbox',
    onViewChange: vi.fn(),
    settingsOpen: false,
    onToggleSettings: vi.fn(),
    settingsPanelId: 'settings-panel',
    ...overrides,
  }

  render(<AppHeader {...props} />)

  return props
}

describe('AppHeader', () => {
  it('header shows brand name and tagline', () => {
    renderHeader()

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()
    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })

  it('marks the active view and switches views on click', async () => {
    const props = renderHeader({ view: 'inbox' })

    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(props.onViewChange).toHaveBeenCalledWith('worktrees')
  })

  it('toggles the settings panel', async () => {
    const props = renderHeader({ settingsOpen: true })
    const toggle = screen.getByRole('button', { name: 'Settings' })

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(toggle).toHaveAttribute('aria-controls', 'settings-panel')

    await userEvent.click(toggle)

    expect(props.onToggleSettings).toHaveBeenCalledOnce()
  })
})
