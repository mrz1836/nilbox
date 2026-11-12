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
    onUndo: null,
    resolveError: '',
    onManagePrompts: vi.fn(),
    onCancel: vi.fn(),
    ...overrides,
  }
}

describe('LaunchMenu', () => {
  it('waits for a choice before it saves defaults, then saves in one click with an undo', async () => {
    const onSave = vi.fn()
    const { rerender } = render(<LaunchMenu {...props({ onSave })} />)

    expect(screen.getByText('Change a choice above to save it as the default for')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'example-repo' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'all of acme-org' })).toBeDisabled()

    rerender(<LaunchMenu {...props({ onSave, choices: { ...noChoices, profile: 'work' } })} />)

    expect(screen.getByText('Save these choices as the default for')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'all of acme-org' }))
    expect(onSave).toHaveBeenCalledWith('org')

    const onUndo = vi.fn()
    rerender(
      <LaunchMenu
        {...props({
          onSave,
          onUndo,
          status: { tone: 'success', text: 'Saved as the default for every acme-org repository.' },
        })}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('Saved as the default for every acme-org repository.')
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(onUndo).toHaveBeenCalledOnce()

    rerender(<LaunchMenu {...props({ onUndo, saving: true, status: { tone: 'success', text: 'Saved.' } })} />)
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  })

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
