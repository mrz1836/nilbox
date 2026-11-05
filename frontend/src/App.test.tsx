import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import { brand } from './generated/brand'
import { item, list, profile, rate, settings, snapshot, unknownRate, worktreesSnapshot } from './test/fixtures'

const platform = vi.hoisted(() => ({
  getAppInfo: vi.fn(),
  getSettings: vi.fn(),
  listProfiles: vi.fn(),
  setTerminal: vi.fn(),
  setCloneRoot: vi.fn(),
  setDefaultProfile: vi.fn(),
  setWorktreeDefault: vi.fn(),
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
  listWorktrees: vi.fn(),
}))

vi.mock('./platform', () => platform)

beforeEach(() => {
  vi.clearAllMocks()
  platform.getSettings.mockResolvedValue(settings({ defaultProfile: 'work', worktreeDefault: false }))
  platform.listProfiles.mockResolvedValue([profile(), profile({ name: 'work' })])
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
  platform.onInboxUpdated.mockReturnValue(() => undefined)
  platform.setTerminal.mockResolvedValue(undefined)
  platform.setDefaultProfile.mockResolvedValue(undefined)
  platform.setWorktreeDefault.mockResolvedValue(undefined)
  platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
  platform.getAppInfo.mockResolvedValue({ name: brand.name, tagline: brand.tagline, version: 'dev' })
})

// openSettingsFromMenu opens the settings page the way a person would: the account menu.
async function openSettingsFromMenu() {
  await userEvent.click(await screen.findByRole('button', { name: 'Account: octo-user' }))
  await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))
}

describe('App', () => {
  it('shows the product name in the toolbar and the tagline under About', async () => {
    render(<App />)

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'About' }))

    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })

  it('shows the account and the API budget from the inbox in the toolbar', async () => {
    // The app runs on the real clock, so the reading must reset in the future.
    const resetAt = new Date(Date.now() + 30 * 60_000).toISOString()
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ rate: { graphql: rate({ resetAt }), core: rate({ remaining: 400, resetAt }), search: unknownRate } }),
    )
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Account: octo-user' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^API budget: Critical, 8% left \(REST\)/ })).toHaveClass('text-danger')
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
    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('opens the settings page from the account menu and keeps a saved change', async () => {
    render(<App />)
    await screen.findByRole('button', { name: 'Open in Claude' })

    await openSettingsFromMenu()

    expect(screen.getByRole('region', { name: 'Account' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveValue('terminal')

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveValue('terminal')
  })

  it('applies a new launch default to the inbox rows', async () => {
    render(<App />)
    expect(await screen.findByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))
    await userEvent.keyboard('{Escape}')

    expect(await screen.findByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()
  })

  it('opens the settings with ⌘, and returns to the view it came from', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await userEvent.keyboard('{Meta>},{/Meta}')

    expect(screen.getByRole('navigation', { name: 'Settings sections' })).toBeInTheDocument()

    // Pressing it again stays put rather than forgetting where to go back to.
    await userEvent.keyboard('{Meta>},{/Meta}')
    await userEvent.keyboard('{Escape}')

    expect(screen.getByRole('region', { name: 'Worktrees' })).toBeInTheDocument()

    // A comma without ⌘ is just a comma.
    await userEvent.keyboard(',')
    expect(screen.queryByRole('navigation', { name: 'Settings sections' })).not.toBeInTheDocument()
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
