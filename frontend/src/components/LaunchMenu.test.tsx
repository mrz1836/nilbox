import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { noChoices } from '../lib/launchChoices'
import { agents, item, profile, prompts, resolution } from '../test/fixtures'
import { LaunchMenu, type LaunchMenuProps } from './LaunchMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function props(overrides: Partial<LaunchMenuProps> = {}): LaunchMenuProps {
  return {
    item: item(),
    resolved: resolution(),
    choices: noChoices,
    onChoices: vi.fn(),
    agents: agents(),
    profiles: [profile()],
    prompts: prompts(),
    agentName: 'Claude',
    onLaunch: vi.fn(),
    busy: false,
    onSave: vi.fn(),
    saving: false,
    status: null,
    resolveError: '',
    onManagePrompts: vi.fn(),
    onCancel: vi.fn(),
    ...overrides,
  }
}

describe('LaunchMenu', () => {
  it('launches, or cancels without launching', async () => {
    const onLaunch = vi.fn()
    const onCancel = vi.fn()
    render(<LaunchMenu {...props({ onLaunch, onCancel })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onLaunch).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    expect(onLaunch).toHaveBeenCalledOnce()
  })

  it('holds Open back while a launch or a clone runs', async () => {
    const onLaunch = vi.fn()
    render(<LaunchMenu {...props({ onLaunch, busy: true })} />)

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open).toBeDisabled()

    await userEvent.click(open)
    expect(onLaunch).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled()
  })
})
