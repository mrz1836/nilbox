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

type Scope = Pick<typeof screen, 'getByRole'>

// optionsButton is the arrow beside "Open in Claude".
function optionsButton(scope: Scope = screen): HTMLElement {
  return scope.getByRole('button', { name: /^Launch options/ })
}

// openOptions opens the launch options and returns queries scoped to them.
async function openOptions(scope: Scope = screen) {
  await userEvent.click(optionsButton(scope))

  return within(scope.getByRole('dialog', { name: 'Launch options' }))
}

beforeEach(() => {
  platform.launch.mockReset()
  platform.launch.mockResolvedValue(launchResult())
})

describe('LaunchControls', () => {
  it('is one quiet button with the profile beside it', () => {
    render(<LaunchControls {...props({ defaultProfile: 'work' })} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(optionsButton()).toHaveAccessibleName('Launch options: work')
    expect(optionsButton()).toHaveTextContent('work')
    expect(optionsButton()).toHaveAttribute('aria-haspopup', 'dialog')
    expect(optionsButton()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('preselects the default profile in the options, focused', async () => {
    render(<LaunchControls {...props({ defaultProfile: 'work' })} />)

    const options = await openOptions()

    expect(optionsButton()).toHaveAttribute('aria-expanded', 'true')
    expect(options.getAllByRole('radio').map((r) => r.getAttribute('value'))).toEqual(['default', 'work'])
    expect(options.getByRole('radio', { name: 'work' })).toBeChecked()
    expect(options.getByRole('radio', { name: 'work' })).toHaveFocus()
  })

  it('falls back to the first profile when the default is missing', async () => {
    render(<LaunchControls {...props({ defaultProfile: 'gone' })} />)

    expect(optionsButton()).toHaveAccessibleName('Launch options: default')
    expect((await openOptions()).getByRole('radio', { name: 'default' })).toBeChecked()
  })

  it('launches with the picked profile for this launch only', async () => {
    render(<LaunchControls {...props()} />)

    await userEvent.click((await openOptions()).getByRole('radio', { name: 'work' }))
    expect(optionsButton()).toHaveAccessibleName('Launch options: work, changed for this launch')

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({
      itemId: 1,
      profile: 'work',
      worktree: true,
      fullAccessOverride: false,
    })
    expect(await screen.findByRole('status')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(optionsButton()).toHaveAccessibleName('Launch options: default')
  })

  it('closes the options on Escape and on a click outside, keeping the choices', async () => {
    render(
      <>
        <LaunchControls {...props()} />
        <button type="button">Elsewhere</button>
      </>,
    )

    await userEvent.click((await openOptions()).getByRole('radio', { name: 'work' }))
    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(optionsButton()).toHaveFocus()

    await openOptions()
    await userEvent.click(screen.getByRole('button', { name: 'Elsewhere' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(optionsButton()).toHaveAccessibleName('Launch options: work, changed for this launch')
  })

  it('launches with no profile when none were discovered', async () => {
    render(<LaunchControls {...props({ profiles: [] })} />)

    expect(optionsButton()).toHaveAccessibleName('Launch options: default')
    expect((await openOptions()).getByText(/No profiles were found/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({ itemId: 1, profile: '', worktree: true, fullAccessOverride: false })
  })

  it('offers the override only on untrusted rows', async () => {
    const { unmount } = render(<LaunchControls {...props({ item: item({ trusted: true }) })} />)

    expect((await openOptions()).queryByRole('checkbox', { name: overrideLabel })).not.toBeInTheDocument()

    unmount()
    render(<LaunchControls {...props({ item: item({ trusted: false, authorAssociation: 'NONE' }) })} />)

    const override = (await openOptions()).getByRole('checkbox', { name: overrideLabel })
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

    await userEvent.click((await openOptions()).getByRole('checkbox', { name: overrideLabel }))
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
    expect((await openOptions()).getByRole('checkbox', { name: overrideLabel })).not.toBeChecked()
  })

  it('worktree choice preset from the pref, and unticking applies to this launch only', async () => {
    const { unmount } = render(<LaunchControls {...props({ worktreeDefault: false })} />)
    expect((await openOptions()).getByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()
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

    await userEvent.click((await openOptions(first)).getByRole('checkbox', { name: 'Start in a worktree' }))
    expect(optionsButton(first)).toHaveAccessibleName('Launch options: default, changed for this launch')
    expect((await openOptions(second)).getByRole('checkbox', { name: 'Start in a worktree' })).toBeChecked()

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
    expect(optionsButton(first)).toHaveAccessibleName('Launch options: default')

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

    const status = await screen.findByRole('status')
    const text = 'Opened in iterm2: ~/projects/example-repo (worktree pr-7) · default · full access'
    expect(status).toHaveTextContent(text)
    expect(status).toHaveAttribute('title', text)
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

    await userEvent.click((await openOptions()).getByRole('checkbox', { name: 'Start in a worktree' }))
    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'no local clone of acme-org/example-repo under ~/projects',
    )
    expect((await openOptions()).getByRole('checkbox', { name: 'Start in a worktree' })).not.toBeChecked()
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
