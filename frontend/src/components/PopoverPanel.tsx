import type { HTMLAttributes, RefObject } from 'react'

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

export interface PopoverPanelProps extends HTMLAttributes<HTMLDivElement> {
  // state is the panel's state from usePopover: whether it is open, its ref, the side it
  // opened on, and the height it keeps within, if any.
  state: {
    open: boolean
    panelRef: RefObject<HTMLDivElement | null>
    side: Side
    maxHeight: number | undefined
  }
  // align lines the panel's right or left edge up with the button's.
  align: 'left' | 'right'
}

// PopoverPanel is the floating card a popover button opens, rendered only while it is
// open. The button and the panel share a positioned parent; className adds the panel's
// size, layer, shadow, and layout. The panel takes focus itself (tabIndex -1), so a click
// on a radio, checkbox, or button in it, which the desktop's WebKit focuses no control
// for, leaves focus in the panel rather than on whatever holds it.
export function PopoverPanel({ state, align, className = '', style, children, ...rest }: PopoverPanelProps) {
  const { open, panelRef, side, maxHeight } = state

  if (!open) {
    return null
  }

  return (
    <div
      ref={panelRef}
      tabIndex={-1}
      {...rest}
      style={maxHeight === undefined ? style : { ...style, maxHeight }}
      className={`absolute rounded-lg border border-border bg-surface shadow-xl outline-none motion-safe:animate-pop-in ${placements[side][align]} ${className}`}
    >
      {children}
    </div>
  )
}
