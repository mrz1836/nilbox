import { render, screen, within } from '@testing-library/react'
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

  it('shows a note below an item, which describes it', async () => {
    const choices: MenuItem[] = [
      { label: 'Run checks', note: 'Some checks fix files.', onSelect: vi.fn() },
      { label: 'Open the config', onSelect: vi.fn() },
    ]

    render(
      <MenuButton label="More actions" items={choices}>
        ⋯
      </MenuButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))

    expect(screen.getByText('Some checks fix files.')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('menuitem', { name: 'Run checks' })).toHaveAccessibleDescription('Some checks fix files.')
    expect(screen.getByRole('menuitem', { name: 'Open the config' })).not.toHaveAttribute('aria-describedby')
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

  describe('submenus', () => {
    // nested is a menu whose middle item opens a submenu.
    function nested(onSelect = vi.fn()): MenuItem[] {
      return [
        { label: 'Open', onSelect },
        {
          label: 'Move to',
          items: [
            { label: 'Inbox', onSelect },
            { label: 'Archive', onSelect },
          ],
        },
        { label: 'Delete…', onSelect, danger: true, section: 'Danger zone', divided: true },
      ]
    }

    it('opens a submenu with the right arrow, and leaves it with the left', async () => {
      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.keyboard('{ArrowDown}')

      const parent = screen.getByRole('menuitem', { name: 'Move to' })
      expect(parent).toHaveFocus()
      expect(parent).toHaveAttribute('aria-haspopup', 'menu')
      expect(parent).toHaveAttribute('aria-expanded', 'false')

      await userEvent.keyboard('{ArrowRight}')

      const submenu = screen.getByRole('menu', { name: 'Move to' })
      expect(parent).toHaveAttribute('aria-expanded', 'true')
      expect(parent).toHaveAttribute('aria-controls', submenu.id)
      expect(within(submenu).getByRole('menuitem', { name: 'Inbox' })).toHaveFocus()

      // The arrows move within the submenu alone, wrapping at its ends.
      await userEvent.keyboard('{ArrowDown}')
      expect(within(submenu).getByRole('menuitem', { name: 'Archive' })).toHaveFocus()
      await userEvent.keyboard('{ArrowDown}')
      expect(within(submenu).getByRole('menuitem', { name: 'Inbox' })).toHaveFocus()

      await userEvent.keyboard('{ArrowLeft}')
      expect(screen.queryByRole('menu', { name: 'Move to' })).not.toBeInTheDocument()
      expect(parent).toHaveFocus()

      // The arrows move within the top menu again.
      await userEvent.keyboard('{ArrowDown}')
      expect(screen.getByRole('menuitem', { name: 'Delete…' })).toHaveFocus()
    })

    it('runs a submenu’s item and closes every menu', async () => {
      const onSelect = vi.fn()
      render(
        <MenuButton label="More actions" items={nested(onSelect)}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.click(screen.getByRole('menuitem', { name: 'Move to' }))
      expect(screen.getByRole('menuitem', { name: 'Inbox' })).toHaveFocus()

      await userEvent.keyboard('{ArrowDown}{Enter}')

      expect(onSelect).toHaveBeenCalledOnce()
      expect(screen.queryByRole('menu')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'More actions' })).toHaveFocus()
    })

    it('opens a submenu as the pointer rests on its item, and closes it on another', async () => {
      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.hover(screen.getByRole('menuitem', { name: 'Move to' }))

      expect(screen.getByRole('menu', { name: 'Move to' })).toBeInTheDocument()
      expect(screen.getByRole('menuitem', { name: 'Open' })).toHaveFocus()

      await userEvent.hover(screen.getByRole('menuitem', { name: 'Archive' }))
      expect(screen.getByRole('menu', { name: 'Move to' })).toBeInTheDocument()

      await userEvent.hover(screen.getByRole('menuitem', { name: 'Delete…' }))
      expect(screen.queryByRole('menu', { name: 'Move to' })).not.toBeInTheDocument()
    })

    it('closes a submenu as the keyboard moves on to another item', async () => {
      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.hover(screen.getByRole('menuitem', { name: 'Move to' }))
      await userEvent.keyboard('{End}')

      expect(screen.queryByRole('menu', { name: 'Move to' })).not.toBeInTheDocument()
    })

    it('closes every menu on Escape', async () => {
      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.click(screen.getByRole('menuitem', { name: 'Move to' }))
      await userEvent.keyboard('{Escape}')

      expect(screen.queryByRole('menu')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'More actions' })).toHaveFocus()
    })

    it('builds a submenu’s items only as it opens', async () => {
      const build = vi.fn((): MenuItem[] => [{ label: 'Inbox', onSelect: vi.fn() }])
      render(
        <MenuButton label="More actions" items={[{ label: 'Move to', items: build }]}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      expect(build).not.toHaveBeenCalled()

      await userEvent.keyboard('{ArrowRight}')
      expect(build).toHaveBeenCalled()
      expect(screen.getByRole('menuitem', { name: 'Inbox' })).toHaveFocus()
    })

    it('opens no submenu from a disabled item', async () => {
      render(
        <MenuButton
          label="More actions"
          items={[
            { label: 'Open', onSelect: vi.fn() },
            { label: 'Move to', disabled: true, items: [{ label: 'Inbox', onSelect: vi.fn() }] },
          ]}
        >
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.keyboard('{ArrowRight}')
      await userEvent.hover(screen.getByRole('menuitem', { name: 'Move to' }))

      expect(screen.queryByRole('menu', { name: 'Move to' })).not.toBeInTheDocument()
    })

    it('opens to the left when there is no room on the right', async () => {
      const width = vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(300)
      const rect = vi
        .spyOn(HTMLElement.prototype, 'getBoundingClientRect')
        .mockReturnValue(DOMRect.fromRect({ x: 200, y: 10, width: 200, height: 60 }))

      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
      await userEvent.click(screen.getByRole('menuitem', { name: 'Move to' }))

      const submenu = screen.getByRole('menu', { name: 'Move to' })
      expect(submenu).toHaveClass('right-full')
      expect(submenu).not.toHaveClass('left-full')

      rect.mockRestore()
      width.mockRestore()
    })

    it('captions a group of items, hidden from assistive tech', async () => {
      render(
        <MenuButton label="More actions" items={nested()}>
          ⋯
        </MenuButton>,
      )

      await userEvent.click(screen.getByRole('button', { name: 'More actions' }))

      expect(screen.getByText('Danger zone')).toHaveAttribute('aria-hidden', 'true')
      expect(screen.getAllByRole('separator')).toHaveLength(1)
    })
  })
})
