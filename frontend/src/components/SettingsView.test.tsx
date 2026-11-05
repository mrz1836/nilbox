import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { Account, Profile, Settings } from '../platform'
import { account, profile, settings, signedOut } from '../test/fixtures'
import { SettingsView } from './SettingsView'

const platform = vi.hoisted(() => ({
  getAppInfo: vi.fn(),
  openInBrowser: vi.fn(),
  setCloneRoot: vi.fn(),
  setDefaultProfile: vi.fn(),
  setTerminal: vi.fn(),
  setWorktreeDefault: vi.fn(),
}))

vi.mock('../platform', () => platform)

beforeEach(() => {
  for (const mock of Object.values(platform)) {
    mock.mockReset().mockResolvedValue(undefined)
  }

  platform.getAppInfo.mockResolvedValue({ name: brand.name, tagline: brand.tagline, version: '1.4.0' })
})

interface Options {
  current?: Settings | null
  profiles?: Profile[]
  who?: Account | null
}

function renderSettings({
  current = settings(),
  profiles = [profile(), profile({ name: 'work' })],
  who = account(),
}: Options = {}) {
  const onSaved = vi.fn()
  const onClose = vi.fn()

  render(<SettingsView settings={current} profiles={profiles} account={who} onSaved={onSaved} onClose={onClose} />)

  return { onSaved, onClose }
}

// openSection picks a section from the sidebar.
async function openSection(name: string) {
  await userEvent.click(screen.getByRole('button', { name }))
}

describe('SettingsView', () => {
  it('opens on the account, with a sidebar of sections', () => {
    renderSettings()

    const nav = screen.getByRole('navigation', { name: 'Settings sections' })
    expect(nav).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('button', { name: 'Launching' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('region', { name: 'Account' })).toBeInTheDocument()
    expect(screen.getByText('Octo User')).toBeInTheDocument()
    expect(screen.getByText('@octo-user')).toBeInTheDocument()
    expect(screen.getByText(/gh auth switch/)).toBeInTheDocument()
  })

  it('opens the GitHub profile in the browser', async () => {
    renderSettings()

    await userEvent.click(screen.getByRole('button', { name: 'View on GitHub' }))

    expect(platform.openInBrowser).toHaveBeenCalledWith('https://github.com/octo-user')
  })

  it('logs a profile that could not be opened', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))
    renderSettings({ who: account({ name: '' }) })

    expect(screen.getByText('octo-user')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'View on GitHub' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalled()
    })
    logged.mockRestore()
  })

  it('says when no one is signed in yet', () => {
    renderSettings({ who: signedOut })

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'View on GitHub' })).not.toBeInTheDocument()
  })

  it('persists the terminal choice', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const terminal = screen.getByRole('combobox', { name: 'Terminal' })
    expect(terminal).toHaveValue('iterm2')
    expect(terminal).toHaveAccessibleDescription('The app new sessions open in.')

    await userEvent.selectOptions(terminal, 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(onSaved).toHaveBeenCalledWith(settings({ terminal: 'terminal' }))
  })

  it('shows a terminal the backend refused', async () => {
    platform.setTerminal.mockRejectedValue(new Error('unknown terminal'))
    const { onSaved } = renderSettings()
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown terminal')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('persists the default profile', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Default profile' }), 'work')

    expect(platform.setDefaultProfile).toHaveBeenCalledWith('work')
    expect(onSaved).toHaveBeenCalledWith(settings({ defaultProfile: 'work' }))
  })

  it('cannot pick a default profile when none were found', async () => {
    renderSettings({ profiles: [] })
    await openSection('Launching')

    const select = screen.getByRole('combobox', { name: 'Default profile' })
    expect(select).toBeDisabled()
    expect(select).toHaveAccessibleDescription('No agent profiles were found in your home directory.')
  })

  it('persists the worktree default with a switch', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const toggle = screen.getByRole('switch', { name: 'Start in a worktree' })
    expect(toggle).toBeChecked()

    await userEvent.click(toggle)

    expect(platform.setWorktreeDefault).toHaveBeenCalledWith(false)
    expect(onSaved).toHaveBeenCalledWith(settings({ worktreeDefault: false }))
  })

  it('shows a worktree default the backend refused', async () => {
    platform.setWorktreeDefault.mockRejectedValue(new Error('database is locked'))
    renderSettings()
    await openSection('Launching')

    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
  })

  it('persists the clone root', async () => {
    const { onSaved } = renderSettings()
    await openSection('Repositories')

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    expect(field).toHaveValue('~/projects')

    await userEvent.clear(field)
    await userEvent.type(field, ' ~/code ')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.setCloneRoot).toHaveBeenCalledWith('~/code')
    expect(onSaved).toHaveBeenCalledWith(settings({ cloneRoot: '~/code' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')

    // Editing again clears the confirmation.
    await userEvent.type(field, 'x')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows a clone root the backend refused', async () => {
    platform.setCloneRoot.mockRejectedValue(new Error('clone root ~/missing is not a directory'))
    const { onSaved } = renderSettings()
    await openSection('Repositories')

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    await userEvent.clear(field)
    await userEvent.type(field, '~/missing{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('clone root ~/missing is not a directory')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('waits for the settings to load', async () => {
    renderSettings({ current: null })

    await openSection('Launching')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()

    await openSection('Repositories')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()
  })

  it('shows the product and its version', async () => {
    renderSettings()
    await openSection('About')

    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
    expect(await screen.findByText('1.4.0')).toBeInTheDocument()
  })

  it('says when the version could not be read', async () => {
    platform.getAppInfo.mockRejectedValue(new Error('no build info'))
    renderSettings()
    await openSection('About')

    expect(await screen.findByText('unknown (no build info)')).toBeInTheDocument()
  })

  it('closes from the back button and on Escape', async () => {
    const { onClose } = renderSettings()

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(onClose).toHaveBeenCalledOnce()

    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('closes on Escape from a switch or a menu, which do not use the key', async () => {
    const { onClose } = renderSettings()
    await openSection('Launching')

    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()

    screen.getByRole('combobox', { name: 'Terminal' }).focus()
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('leaves Escape to a field being edited, or to whatever already used it', async () => {
    const { onClose } = renderSettings()
    await openSection('Repositories')

    await userEvent.click(screen.getByRole('textbox', { name: 'Clone root' }))
    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()

    const used = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    used.preventDefault()
    fireEvent(document.body, used)
    expect(onClose).not.toHaveBeenCalled()

    // An input method composing text owns the key too.
    fireEvent.keyDown(document.body, { key: 'Escape', isComposing: true })
    expect(onClose).not.toHaveBeenCalled()
  })
})
