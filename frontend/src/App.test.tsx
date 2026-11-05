import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import { brand } from './generated/brand'
import { item, list, profile, settings, snapshot } from './test/fixtures'

const platform = vi.hoisted(() => ({
  getSettings: vi.fn(),
  listProfiles: vi.fn(),
  setTerminal: vi.fn(),
  setCloneRoot: vi.fn(),
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
}))

vi.mock('./platform', () => platform)

beforeEach(() => {
  vi.clearAllMocks()
  platform.getSettings.mockResolvedValue(settings({ defaultProfile: 'work', worktreeDefault: false }))
  platform.listProfiles.mockResolvedValue([profile(), profile({ name: 'work' })])
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
  platform.onInboxUpdated.mockReturnValue(() => undefined)
  platform.setTerminal.mockResolvedValue(undefined)
})

describe('App', () => {
  it('shows the product name and tagline', () => {
    render(<App />)

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()
    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })

  it('opens on the inbox with launch controls from the settings', async () => {
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Profile' })).toHaveValue('work')
    expect(screen.getByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()
  })

  it('switches between the inbox and the worktrees view', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(screen.getByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('opens the settings panel and keeps a saved change', async () => {
    render(<App />)
    await screen.findByRole('button', { name: 'Open in Claude' })

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveValue('terminal')

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))

    expect(screen.queryByRole('combobox', { name: 'Terminal' })).not.toBeInTheDocument()
  })

  it('shows settings that could not be loaded', async () => {
    platform.getSettings.mockRejectedValue(new Error('read the preferences: database is locked'))
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not load the launch settings: read the preferences: database is locked',
    )
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })
})
