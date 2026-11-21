import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { agents, cloneTarget, item, itemResolution, launchResult, profile, prompts } from '../test/fixtures'
import { platform } from '../test/platform'
import { LaunchControls, type LaunchControlsProps, type LaunchPlace } from './LaunchControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// place is where a Worktrees row puts its verb and the launch's outcome.
const place: LaunchPlace = { boxClass: 'col-[7] justify-self-end', outcomeClass: 'col-[2/-1] row-start-2' }

// props are a Worktrees row's verb for the fixture item, launching in its worktree, placed
// where LaunchControls puts it by default.
function props(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return {
    variant: 'row',
    item: item(),
    resolution: itemResolution(),
    agents: agents(),
    profiles: [profile()],
    prompts: prompts(),
    launchDefaults: [],
    onDefaultsSaved: vi.fn(() => Promise.resolve()),
    onManagePrompts: vi.fn(),
    onSettingsSaved: vi.fn(),
    initial: { worktree: true },
    ...overrides,
  }
}

// renderRow renders the verb in a grid, where a row places it.
function renderRow(overrides: Partial<LaunchControlsProps> = {}) {
  return render(
    <div className="grid">
      <LaunchControls {...props({ ...place, ...overrides })} />
    </div>,
  )
}

describe('LaunchControls, row variant', () => {
  it('is one bordered button in the row’s verb cell, which opens the launch options first', async () => {
    renderRow()

    const open = screen.getByRole('button', { name: 'Open in Claude' })

    expect(open).toBeEnabled()
    expect(open).toHaveClass('h-6', 'rounded-md', 'border', 'border-border', 'text-xs')
    expect(open).toHaveAttribute(
      'title',
      'Open in Claude with “Review” as default. The launch options open first, to check or change them.',
    )
    expect(open).toHaveAttribute('aria-haspopup', 'dialog')
    expect(open).toHaveAttribute('aria-expanded', 'false')
    expect(open.parentElement).toHaveClass('col-[7]', 'justify-self-end')
    expect(screen.queryByRole('button', { name: /^Launch options/ })).not.toBeInTheDocument()

    await userEvent.click(open)

    const dialog = await screen.findByRole('dialog', { name: 'Launch options' })
    expect(open).toHaveAttribute('aria-expanded', 'true')
    expect(open).toHaveAttribute('aria-controls', dialog.id)
    expect(platform.launch).not.toHaveBeenCalled()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledOnce()
    expect(platform.launch.mock.calls[0]?.[0]).toMatchObject({ itemId: 1, worktree: true })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('marks a choice changed for this launch on the button', async () => {
    renderRow()

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open.querySelector('.bg-accent')).toBeNull()

    await userEvent.click(open)
    const menu = within(await screen.findByRole('dialog', { name: 'Launch options' }))
    const select = menu.getByRole('combobox', { name: 'Prompt' })
    await userEvent.selectOptions(select, within(select).getByRole('option', { name: /^Quick look/ }))
    await userEvent.click(menu.getByRole('button', { name: 'Close' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(open.querySelector('.bg-accent')).toHaveAttribute('aria-hidden', 'true')
    expect(open).toHaveAccessibleName('Open in Claude, changed for this launch')
  })

  it('waits for the launch settings, and says why no prompt applies', () => {
    const { rerender } = renderRow({ resolution: null })

    expect(screen.getByRole('button', { name: 'Open' })).toBeDisabled()

    const problem = 'No prompt applies to this kind of item; add one on the Prompts tab (issues).'
    rerender(
      <div className="grid">
        <LaunchControls {...props({ ...place, resolution: itemResolution({ problem }) })} />
      </div>,
    )

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open).toHaveAttribute('title', problem)
    expect(open.querySelector('svg')).toHaveClass('text-warning')
  })

  it('says the launch is running on the button, and its outcome where the row puts it', async () => {
    platform.launch.mockReturnValueOnce(new Promise(() => undefined))
    renderRow()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Launch options' })).getByRole('button', {
        name: 'Open in Claude',
      }),
    )

    expect(screen.getByRole('button', { name: 'Opening…' })).toBeDisabled()
  })

  it('puts what a launch worked around, or why it failed, under the row', async () => {
    platform.launch
      .mockResolvedValueOnce(launchResult({ notice: 'Started without the worktree: it was in use.' }))
      .mockRejectedValueOnce(new Error('the terminal could not be opened'))
    platform.cloneTarget.mockResolvedValue(cloneTarget({ cloned: true }))
    renderRow()

    const launchIt = async () => {
      await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
      await userEvent.click(
        within(await screen.findByRole('dialog', { name: 'Launch options' })).getByRole('button', {
          name: 'Open in Claude',
        }),
      )
    }

    await launchIt()
    expect(await screen.findByRole('status')).toHaveClass('col-[2/-1]', 'row-start-2', 'text-warning')

    await launchIt()
    expect(await screen.findByRole('alert')).toHaveClass('col-[2/-1]', 'row-start-2', 'text-danger')
    expect(screen.getByRole('alert')).toHaveTextContent('the terminal could not be opened')
  })

  it('centers the button in its row, and puts the outcome under the row’s content, by default', async () => {
    platform.launch.mockRejectedValueOnce(new Error('the terminal could not be opened'))
    platform.cloneTarget.mockResolvedValue(cloneTarget({ cloned: true }))
    render(
      <div className="grid">
        <LaunchControls {...props()} />
      </div>,
    )

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open.parentElement).toHaveClass('self-center')

    await userEvent.click(open)
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Launch options' })).getByRole('button', {
        name: 'Open in Claude',
      }),
    )

    expect(await screen.findByRole('alert')).toHaveClass('col-[2/-1]', 'mt-1.5')
  })
})
