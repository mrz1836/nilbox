import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { item, launchResult, profile } from '../test/fixtures'
import { LaunchControls, type LaunchControlsProps } from './LaunchControls'

const platform = vi.hoisted(() => ({ launch: vi.fn() }))

vi.mock('../platform', () => platform)

const overrideLabel = 'Use profile permissions for this item'
const profiles = [profile(), profile({ name: 'work', configDir: '~/.claude_work' })]

function props(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return { item: item(), profiles, defaultProfile: 'default', worktreeDefault: true, ...overrides }
}

beforeEach(() => {
  platform.launch.mockReset()
  platform.launch.mockResolvedValue(launchResult())
})

describe('LaunchControls', () => {
  it('preselects the default profile', () => {
    render(<LaunchControls {...props({ defaultProfile: 'work' })} />)

    expect(screen.getByRole('combobox', { name: 'Profile' })).toHaveValue('work')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['default', 'work'])
  })

  it('falls back to the first profile when the default is missing', () => {
    render(<LaunchControls {...props({ defaultProfile: 'gone' })} />)

    expect(screen.getByRole('combobox', { name: 'Profile' })).toHaveValue('default')
  })

  it('launches with the picked profile for this launch only', async () => {
    render(<LaunchControls {...props()} />)

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Profile' }), 'work')
    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({
      itemId: 1,
      profile: 'work',
      worktree: true,
      fullAccessOverride: false,
    })
    expect(await screen.findByRole('status')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Profile' })).toHaveValue('default')
  })

  it('launches with no profile when none were discovered', async () => {
    render(<LaunchControls {...props({ profiles: [] })} />)

    expect(screen.getByRole('combobox', { name: 'Profile' })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({ itemId: 1, profile: '', worktree: true, fullAccessOverride: false })
  })

  it('override only on untrusted rows', () => {
    const { unmount } = render(<LaunchControls {...props({ item: item({ trusted: true }) })} />)

    expect(screen.queryByText('outside contributor')).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: overrideLabel })).not.toBeInTheDocument()

    unmount()
    render(<LaunchControls {...props({ item: item({ trusted: false, authorAssociation: 'NONE' }) })} />)

    expect(screen.getByText('outside contributor')).toBeInTheDocument()
    const override = screen.getByRole('checkbox', { name: overrideLabel })
    expect(override).not.toBeChecked()
    expect(override).toHaveAccessibleDescription(
      "Lifts the outside-contributor restriction; keeps the profile's plan and permission settings.",
    )
  })

  it('an untrusted launch without the override stays restricted', async () => {
    render(<LaunchControls {...props({ item: item({ trusted: false }) })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({
      itemId: 1,
      profile: 'default',
      worktree: true,
      fullAccessOverride: false,
    })
  })

  it('override keeps profile plan and permission settings', async () => {
    const planner = profile({ name: 'planner', permissionMode: 'acceptEdits', planMode: true })
    platform.launch.mockResolvedValue(launchResult({ profile: 'planner', permission: 'plan mode' }))

    render(
      <LaunchControls
        {...props({ item: item({ trusted: false }), profiles: [profile(), planner], defaultProfile: 'planner' })}
      />,
    )

    await userEvent.click(screen.getByRole('checkbox', { name: overrideLabel }))
    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    // The request names the profile and lifts only the outside-contributor restriction: it
    // carries no permission or plan-mode values of its own, so the profile's settings
    // govern, and the backend's summary of them is shown as is.
    expect(platform.launch).toHaveBeenCalledOnce()
    expect(platform.launch.mock.calls[0]?.[0]).toStrictEqual({
      itemId: 1,
      profile: 'planner',
      worktree: true,
      fullAccessOverride: true,
    })
    expect(await screen.findByRole('status')).toHaveTextContent('plan mode')
    expect(screen.getByRole('checkbox', { name: overrideLabel })).not.toBeChecked()
  })

  it('worktree checkbox preset from the pref and untick applies to this launch only', async () => {
    const { unmount } = render(<LaunchControls {...props({ worktreeDefault: false })} />)
    expect(screen.getByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()
    unmount()

    render(
      <>
        <div data-testid="first">
          <LaunchControls {...props({ item: item({ id: 1 }) })} />
        </div>
        <div data-testid="second">
          <LaunchControls {...props({ item: item({ id: 2, number: 8 }) })} />
        </div>
      </>,
    )
    const first = within(screen.getByTestId('first'))
    const second = within(screen.getByTestId('second'))

    expect(first.getByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()
    expect(second.getByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()

    await userEvent.click(first.getByRole('checkbox', { name: 'Start in a worktree' }))
    expect(second.getByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()

    await userEvent.click(first.getByRole('button', { name: 'Open in Claude' }))
    expect(platform.launch).toHaveBeenLastCalledWith({
      itemId: 1,
      profile: 'default',
      worktree: false,
      fullAccessOverride: false,
    })

    // The untick covered that launch only: the row is back on the default, and the next
    // launch starts in a worktree again.
    await first.findByRole('status')
    expect(first.getByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()

    await userEvent.click(first.getByRole('button', { name: 'Open in Claude' }))
    expect(platform.launch).toHaveBeenLastCalledWith({
      itemId: 1,
      profile: 'default',
      worktree: true,
      fullAccessOverride: false,
    })

    await userEvent.click(second.getByRole('button', { name: 'Open in Claude' }))
    expect(platform.launch).toHaveBeenLastCalledWith({
      itemId: 2,
      profile: 'default',
      worktree: true,
      fullAccessOverride: false,
    })
  })

  it('shows where the session started', async () => {
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Opened in iterm2: ~/projects/example-repo (worktree pr-7) · default · full access',
    )
  })

  it('shows a launch without a worktree in the clone', async () => {
    platform.launch.mockResolvedValue(launchResult({ worktree: '' }))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Opened in iterm2: ~/projects/example-repo · default')
  })

  it('shows the backend error and keeps the choices for a retry', async () => {
    platform.launch.mockRejectedValue(new Error('no local clone of acme-org/example-repo under ~/projects'))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('checkbox', { name: 'Start in a worktree' }))
    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'no local clone of acme-org/example-repo under ~/projects',
    )
    expect(screen.getByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()
  })

  it('disables the button while opening', async () => {
    let finish: (value: unknown) => void = () => undefined
    platform.launch.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(screen.getByRole('button', { name: 'Opening…' })).toBeDisabled()

    finish(launchResult())
    expect(await screen.findByRole('button', { name: 'Open in Claude' })).toBeEnabled()
  })
})
