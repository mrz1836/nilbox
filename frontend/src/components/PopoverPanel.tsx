import type { HTMLAttributes, ReactNode, RefObject } from 'react'

import { badgeText } from '../lib/format'
import type { Side } from '../lib/usePopover'

// placements put a panel below or above its button, lined up with the button's right or
// left edge, growing out of that corner.
const placements = {
  below: { right: 'top-full mt-1.5 right-0 origin-top-right', left: 'top-full mt-1.5 left-0 origin-top-left' },
  above: {
    right: 'bottom-full mb-1.5 right-0 origin-bottom-right',
    left: 'bottom-full mb-1.5 left-0 origin-bottom-left',
  },
} as const

// elevations are the shadows a panel floats on: a menu's, or a card chip's, a little
// darker. Each is written out whole, so Tailwind finds it.
const elevations = { menu: 'shadow-menu', card: 'shadow-card' } as const

// panelClass is the card a hand-drawn panel shares with PopoverPanel, such as a submenu
// or a field's list of suggestions: its edge, its fill, and a menu's elevation.
export const panelClass = 'rounded-lg border border-border bg-surface shadow-menu'

export interface PopoverPanelProps extends HTMLAttributes<HTMLDivElement> {
  // state is the panel's state from usePopover: whether it is open, its ref, the side it
  // opened on, the height it keeps within, if any, and how far it moves sideways to stay in
  // view (0, or none, for not at all).
  state: {
    open: boolean
    panelRef: RefObject<HTMLDivElement | null>
    side: Side
    maxHeight: number | undefined
    shift?: number
  }
  // align lines the panel's right or left edge up with the button's.
  align: 'left' | 'right'
  // elevation is how the panel floats: "menu" (the default) for a menu or a toolbar's
  // panel, "card" for the panel a card's chip opens, on a darker shadow-card.
  elevation?: 'menu' | 'card'
}

// PopoverPanel is the floating card a popover button opens, rendered only while it is
// open. The button and the panel share a positioned parent; className adds the panel's
// size, layer, and layout. The panel takes focus itself (tabIndex -1), so a click on a
// radio, checkbox, or button in it, which the desktop's WebKit focuses no control for,
// leaves focus in the panel rather than on whatever holds it.
export function PopoverPanel({
  state,
  align,
  elevation = 'menu',
  className = '',
  style,
  children,
  ...rest
}: PopoverPanelProps) {
  const { open, panelRef, side, maxHeight, shift = 0 } = state

  if (!open) {
    return null
  }

  // The panel moves sideways off the edge it lines up with, in that edge's own terms, so
  // the pop-in's transform stays its own.
  const placed = {
    ...style,
    ...(maxHeight === undefined ? {} : { maxHeight }),
    ...(shift === 0 ? {} : align === 'right' ? { right: -shift } : { left: shift }),
  }

  return (
    <div
      ref={panelRef}
      tabIndex={-1}
      {...rest}
      style={placed}
      className={`absolute rounded-lg border border-border bg-surface ${elevations[elevation]} outline-none motion-safe:animate-pop-in ${placements[side][align]} ${className}`}
    >
      {children}
    </div>
  )
}

export interface ToolbarPopoverButtonProps {
  // popover is the button's state from usePopover: whether its panel is open, the ref the
  // button carries, and toggle, which a click calls.
  popover: {
    open: boolean
    triggerRef: RefObject<HTMLButtonElement | null>
    toggle: () => void
  }
  // id is the panel's, which the button controls while it is open.
  id: string
  // label names the button, for assistive tech and as its tooltip.
  label: string
  // onClick takes the click in place of toggle, for a button that does more as it opens
  // or closes its panel, and toggles it itself.
  onClick?: (() => void) | undefined
  // className is the button's whole style: its size, its colors, and their states.
  className: string
  // children are what the button shows: its icon, and a CountDot or other mark over it.
  children: ReactNode
}

// ToolbarPopoverButton is a toolbar button that opens a dialog in a PopoverPanel: named by
// label, which is its tooltip too, and saying whether the panel is open, and which it is
// while it is.
export function ToolbarPopoverButton({ popover, id, label, onClick, className, children }: ToolbarPopoverButtonProps) {
  const { open, triggerRef, toggle } = popover

  return (
    <button
      ref={triggerRef}
      type="button"
      aria-label={label}
      title={label}
      aria-haspopup="dialog"
      aria-expanded={open}
      aria-controls={open ? id : undefined}
      onClick={onClick ?? toggle}
      className={className}
    >
      {children}
    </button>
  )
}

// CountDot is a toolbar button's count, as badgeText words it, in a dot over the button's
// corner, hidden from assistive tech, which hears the count in the button's label. It
// shows nothing at none. testId names it for the tests.
export function CountDot({ count, testId }: { count: number; testId: string }) {
  if (count <= 0) {
    return null
  }

  return (
    <span
      data-testid={testId}
      aria-hidden="true"
      className="absolute top-0.5 right-0.5 min-w-4 rounded-full bg-accent px-1 text-center text-badge leading-4 font-semibold text-surface tabular-nums"
    >
      {badgeText(count)}
    </span>
  )
}
