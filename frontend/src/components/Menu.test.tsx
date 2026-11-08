import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { MenuButton, type MenuItem } from './Menu'

function items(onSelect = vi.fn()): MenuItem[] {
  return [
    { label: 'Duplicate', onSelect },
    { label: 'Locked', onSelect, disabled: true, title: 'Save first.' },
    { label: 'Delete…', onSelect, danger: true },
  ]
}

describe('MenuButton', () => {
  it('builds its items only while open, when given a function', async () => {
    const build = vi.fn(() => items())
    render(
      <MenuButton label="More actions" items={build}>
        ⋯
      </MenuButton>,
    )

    expect(build).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))

    expect(build).toHaveBeenCalled()
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()
  })

  it('opens a menu that focuses its first enabled item', async () => {
    render(
      <MenuButton label="More actions" items={items()}>
        ⋯
      </MenuButton>,
    )

    const button = screen.getByRole('button', { name: 'More actions' })
    expect(button).toHaveAttribute('aria-haspopup', 'menu')

    await userEvent.click(button)

    expect(button).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('menu', { name: 'More actions' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()
    expect(screen.getByRole('menuitem', { name: 'Locked' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveClass('text-danger')
  })

  it('opens from its button with the arrow keys, at the first or the last enabled item', async () => {
    render(
      <MenuButton label="More actions" items={items()}>
        ⋯
      </MenuButton>,
    )
    const button = screen.getByRole('button', { name: 'More actions' })
    button.focus()

    // Other keys leave it closed.
    await userEvent.keyboard('x')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()

    await userEvent.keyboard('{Escape}')
    expect(button).toHaveFocus()

    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveFocus()
  })

  it('moves to the first or last item from its button while open, and opens at the first next time', async () => {
    render(
      <MenuButton label="More actions" items={items()}>
        ⋯
      </MenuButton>,
    )
    const button = screen.getByRole('button', { name: 'More actions' })

    await userEvent.click(button)
    button.focus()

    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveFocus()

    button.focus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()

    button.focus()
    await userEvent.keyboard('{ArrowUp}{Escape}')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    await userEvent.click(button)
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()
  })

  it('moves with the arrows past disabled items, and runs the chosen one', async () => {
    const onSelect = vi.fn()
    render(
      <MenuButton label="More actions" items={items(onSelect)} align="left">
        ⋯
      </MenuButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveFocus()

    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()

    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveFocus()

    await userEvent.keyboard('{Home}')
    expect(screen.getByRole('menuitem', { name: 'Duplicate' })).toHaveFocus()

    await userEvent.keyboard('{End}{Enter}')

    expect(onSelect).toHaveBeenCalledOnce()
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More actions' })).toHaveFocus()
  })

  it('closes on Escape', async () => {
    render(
      <MenuButton label="More actions" items={items()}>
        ⋯
      </MenuButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('shows a heading, hints, and a divider', async () => {
    const choices: MenuItem[] = [
      { label: '1 day', hint: 'Tomorrow', onSelect: vi.fn() },
      { label: 'Change the default…', divided: true, onSelect: vi.fn() },
    ]

    render(
      <MenuButton label="Snooze for…" heading="Snooze until" items={choices}>
        ▾
      </MenuButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Snooze for…' }))

    expect(screen.getByText('Snooze until')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('menuitem', { name: /^1 day,\s?Tomorrow$/ })).toBeInTheDocument()
    expect(screen.getByRole('separator')).toBeInTheDocument()
    expect(screen.getAllByRole('menuitem')).toHaveLength(2)
  })

  it('opens upward when there is no room below the button', async () => {
    const rect = vi
      .spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockReturnValue(DOMRect.fromRect({ x: 0, y: window.innerHeight - 40, width: 24, height: 24 }))
    const height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(200)

    render(
      <MenuButton label="More actions" items={items()} align="left">
        ⋯
      </MenuButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))

    const panel = screen.getByRole('menu').parentElement
    expect(panel).toHaveClass('bottom-full', 'left-0', 'origin-bottom-left')
    expect(panel).not.toHaveClass('top-full')

    rect.mockRestore()
    height.mockRestore()
  })
})
