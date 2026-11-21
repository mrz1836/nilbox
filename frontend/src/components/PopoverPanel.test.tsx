import { createRef } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Side } from '../lib/usePopover'
import { CountDot, panelClass, PopoverPanel, ToolbarPopoverButton } from './PopoverPanel'

// state is a usePopover state for a panel open on side, keeping within maxHeight.
function state(open: boolean, side: Side = 'below', maxHeight?: number) {
  return { open, panelRef: createRef<HTMLDivElement>(), side, maxHeight }
}

describe('PopoverPanel', () => {
  it('renders nothing while closed', () => {
    const { container } = render(
      <PopoverPanel state={state(false)} align="right">
        Inside
      </PopoverPanel>,
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('draws the floating card, focusable itself, with what the caller adds', () => {
    const open = state(true)
    render(
      <PopoverPanel
        state={open}
        align="right"
        id="budget"
        role="dialog"
        aria-label="API budget"
        className="shadow-black/20 z-40 w-80"
        style={{ width: '20rem' }}
      >
        Inside
      </PopoverPanel>,
    )

    const panel = screen.getByRole('dialog', { name: 'API budget' })
    expect(panel).toBe(open.panelRef.current)
    expect(panel).toHaveAttribute('id', 'budget')
    expect(panel).toHaveAttribute('tabindex', '-1')
    expect(panel).toHaveTextContent('Inside')
    expect(panel.style.width).toBe('20rem')
    expect(panel.style.maxHeight).toBe('')
    expect(panel).toHaveClass(
      'absolute',
      'rounded-lg',
      'border',
      'border-border',
      'bg-surface',
      'outline-none',
      'motion-safe:animate-pop-in',
      'z-40',
      'w-80',
      'shadow-black/20',
    )
  })

  it.each([
    ['below', 'right', ['top-full', 'mt-1.5', 'right-0', 'origin-top-right']],
    ['below', 'left', ['top-full', 'mt-1.5', 'left-0', 'origin-top-left']],
    ['above', 'right', ['bottom-full', 'mb-1.5', 'right-0', 'origin-bottom-right']],
    ['above', 'left', ['bottom-full', 'mb-1.5', 'left-0', 'origin-bottom-left']],
  ] as const)('opens %s its button, lined up on the %s', (side, align, classes) => {
    render(
      <PopoverPanel state={state(true, side)} align={align} role="dialog" aria-label="Panel">
        Inside
      </PopoverPanel>,
    )

    expect(screen.getByRole('dialog', { name: 'Panel' })).toHaveClass(...classes)
  })

  it('floats on a menu’s shadow, or a card chip’s', () => {
    const { rerender } = render(
      <PopoverPanel state={state(true)} align="right" role="dialog" aria-label="Panel">
        Inside
      </PopoverPanel>,
    )

    const panel = screen.getByRole('dialog', { name: 'Panel' })
    expect(panel).toHaveClass('shadow-menu')
    expect(panel).not.toHaveClass('shadow-card')
    expect(panel).not.toHaveAttribute('elevation')

    rerender(
      <PopoverPanel state={state(true)} align="right" role="dialog" aria-label="Panel" elevation="card">
        Inside
      </PopoverPanel>,
    )

    expect(panel).toHaveClass('shadow-card')
    expect(panel).not.toHaveClass('shadow-menu')
  })

  it('keeps within the height its state leaves it, beside the caller’s style', () => {
    render(
      <PopoverPanel
        state={state(true, 'above', 240)}
        align="left"
        role="dialog"
        aria-label="Panel"
        style={{ width: '20rem' }}
      >
        Inside
      </PopoverPanel>,
    )

    const panel = screen.getByRole('dialog', { name: 'Panel' })
    expect(panel.style.width).toBe('20rem')
    expect(panel.style.maxHeight).toBe('240px')
  })
})

describe('panelClass', () => {
  it('is the card a hand-drawn panel shares with PopoverPanel', () => {
    expect(panelClass).toBe('rounded-lg border border-border bg-surface shadow-menu')

    render(
      <PopoverPanel state={state(true)} align="left" role="dialog" aria-label="Panel">
        Inside
      </PopoverPanel>,
    )

    expect(screen.getByRole('dialog', { name: 'Panel' })).toHaveClass(...panelClass.split(' '))
  })
})

describe('ToolbarPopoverButton', () => {
  it('names itself, says whether its panel is open, and toggles it', async () => {
    const toggle = vi.fn()
    const { rerender } = render(
      <ToolbarPopoverButton
        popover={{ open: false, triggerRef: createRef(), toggle }}
        id="threads"
        label="Notifications"
        className="size-8"
      >
        <svg aria-hidden="true" />
      </ToolbarPopoverButton>,
    )

    const button = screen.getByRole('button', { name: 'Notifications' })
    expect(button).toHaveAttribute('title', 'Notifications')
    expect(button).toHaveAttribute('aria-haspopup', 'dialog')
    expect(button).toHaveAttribute('aria-expanded', 'false')
    expect(button).not.toHaveAttribute('aria-controls')
    expect(button).toHaveClass('size-8')

    await userEvent.click(button)
    expect(toggle).toHaveBeenCalledOnce()

    rerender(
      <ToolbarPopoverButton
        popover={{ open: true, triggerRef: createRef(), toggle }}
        id="threads"
        label="Notifications"
        className="size-8"
      >
        <svg aria-hidden="true" />
      </ToolbarPopoverButton>,
    )

    expect(button).toHaveAttribute('aria-expanded', 'true')
    expect(button).toHaveAttribute('aria-controls', 'threads')
  })

  it('takes the click in place of toggle when given onClick', async () => {
    const toggle = vi.fn()
    const onClick = vi.fn()
    render(
      <ToolbarPopoverButton
        popover={{ open: false, triggerRef: createRef(), toggle }}
        id="threads"
        label="Notifications"
        onClick={onClick}
        className="size-8"
      >
        <svg aria-hidden="true" />
      </ToolbarPopoverButton>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))

    expect(onClick).toHaveBeenCalledOnce()
    expect(toggle).not.toHaveBeenCalled()
  })
})

describe('CountDot', () => {
  it('shows nothing at none', () => {
    const { container } = render(<CountDot count={0} testId="dot" />)

    expect(container).toBeEmptyDOMElement()
  })

  it('shows the count as badgeText words it, hidden from assistive tech', () => {
    const { rerender } = render(<CountDot count={7} testId="dot" />)

    const dot = screen.getByTestId('dot')
    expect(dot).toHaveTextContent('7')
    expect(dot).toHaveAttribute('aria-hidden', 'true')
    expect(dot).toHaveClass('text-badge', 'leading-4', 'font-semibold', 'text-surface')

    rerender(<CountDot count={120} testId="dot" />)

    expect(dot).toHaveTextContent('99+')
  })
})
