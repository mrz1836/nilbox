import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { settings } from '../test/fixtures'
import { SettingsPanel } from './SettingsPanel'

const platform = vi.hoisted(() => ({ setTerminal: vi.fn(), setCloneRoot: vi.fn() }))

vi.mock('../platform', () => platform)

beforeEach(() => {
  platform.setTerminal.mockReset().mockResolvedValue(undefined)
  platform.setCloneRoot.mockReset().mockResolvedValue(undefined)
})

describe('SettingsPanel', () => {
  it('persists the terminal choice', async () => {
    const onSaved = vi.fn()
    render(<SettingsPanel id="settings" settings={settings()} onSaved={onSaved} />)

    const terminal = screen.getByRole('combobox', { name: 'Terminal' })
    expect(terminal).toHaveValue('iterm2')

    await userEvent.selectOptions(terminal, 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(onSaved).toHaveBeenCalledWith(settings({ terminal: 'terminal' }))
  })

  it('shows a terminal the backend refused', async () => {
    platform.setTerminal.mockRejectedValue(new Error('unknown terminal'))
    const onSaved = vi.fn()
    render(<SettingsPanel id="settings" settings={settings()} onSaved={onSaved} />)

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown terminal')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('persists the clone root', async () => {
    const onSaved = vi.fn()
    render(<SettingsPanel id="settings" settings={settings()} onSaved={onSaved} />)

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    expect(field).toHaveValue('~/projects')

    await userEvent.clear(field)
    await userEvent.type(field, ' ~/code ')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.setCloneRoot).toHaveBeenCalledWith('~/code')
    expect(onSaved).toHaveBeenCalledWith(settings({ cloneRoot: '~/code' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
  })

  it('shows a clone root the backend refused', async () => {
    platform.setCloneRoot.mockRejectedValue(new Error('clone root ~/missing is not a directory'))
    const onSaved = vi.fn()
    render(<SettingsPanel id="settings" settings={settings()} onSaved={onSaved} />)

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    await userEvent.clear(field)
    await userEvent.type(field, '~/missing{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('clone root ~/missing is not a directory')
    expect(onSaved).not.toHaveBeenCalled()
  })
})
