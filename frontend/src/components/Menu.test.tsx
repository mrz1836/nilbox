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
})
