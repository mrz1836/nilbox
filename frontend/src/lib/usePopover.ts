import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState, type RefObject } from 'react'

import { remToPx } from './rem'

// Placement is where a panel opens against its button: always below it ('below'); below
// unless it does not fit there and there is more room above ('flip'); or flipped the same
// way and kept within the area it scrolls in, scrolling inside itself when it runs longer
// ('fit'), for a panel inside a scrolling list that would otherwise be cut off.
export type Placement = 'below' | 'flip' | 'fit'

// Side is the side of its button a panel opened on.
export type Side = 'above' | 'below'

// PopoverFocus is where focus goes as a panel opens: onto the panel itself, so Escape and
// Tab work from there, or onto the control a function picks inside it. Without one, focus
// stays on the button.
export type PopoverFocus<P extends HTMLElement> = 'panel' | ((panel: P) => HTMLElement | null | undefined)

export interface PopoverOptions<P extends HTMLElement> {
  placement?: Placement
  focus?: PopoverFocus<P>
}

// Popover is the state of a button that opens a floating panel.
export interface Popover<T extends HTMLElement, P extends HTMLElement> {
  open: boolean
  // triggerRef and panelRef go on the button and the panel; clicks and focus inside
  // either keep the panel open.
  triggerRef: RefObject<T | null>
  panelRef: RefObject<P | null>
  // side is the side of the button the panel opened on.
  side: Side
  // maxHeight is the most the panel may grow to, with the 'fit' placement, or undefined.
  maxHeight: number | undefined
  toggle: () => void
  show: () => void
  // close hides the panel, moving focus back to the button when restoreFocus is set.
  close: (restoreFocus?: boolean) => void
}

// panelGap is the room kept between a panel and the edge of the area it opens in, and
// minPanelHeight the least height a fitted panel keeps however little room there is. Both
// are in rem, so they follow the text size as the panels themselves do.
const panelGap = 0.75
const minPanelHeight = 10

interface Bounds {
  top: number
  bottom: number
}

// scrollBounds returns the top and bottom of the nearest ancestor that scrolls, which clips
// a panel inside it, or of the window.
function scrollBounds(el: HTMLElement): Bounds {
  for (let node = el.parentElement; node !== null; node = node.parentElement) {
    const overflow = getComputedStyle(node).overflowY

    if (overflow === 'auto' || overflow === 'scroll') {
      const rect = node.getBoundingClientRect()

      return { top: rect.top, bottom: rect.bottom }
    }
  }

  return { top: 0, bottom: window.innerHeight }
}

// place works out the side a panel opens on, and with 'fit' the height it keeps within: it
// opens above when it does not fit below and there is more room above.
function place(trigger: HTMLElement, panel: HTMLElement, placement: Placement): { side: Side; maxHeight?: number } {
  const bounds = placement === 'fit' ? scrollBounds(trigger) : { top: 0, bottom: window.innerHeight }
  const rect = trigger.getBoundingClientRect()
  const gap = remToPx(panelGap, trigger.ownerDocument)
  const below = bounds.bottom - rect.bottom - gap
  const above = rect.top - bounds.top - gap
  // The panel's full height, whether it scrolls inside itself or not.
  const height = Math.max(panel.offsetHeight, panel.scrollHeight)
  const up = height > below && above > below

  if (placement !== 'fit') {
    return { side: up ? 'above' : 'below' }
  }

  return {
    side: up ? 'above' : 'below',
    maxHeight: Math.max(remToPx(minPanelHeight, trigger.ownerDocument), up ? above : below),
  }
}

// usePopover opens and closes a floating panel the way a desktop menu behaves: a click or
// focus anywhere outside the button and the panel closes it (focus landing on something
// that holds the panel is not outside), and Escape closes it and
// puts focus back on the button. Escape is taken in the capture phase and marked handled
// (defaultPrevented), so a page-level Escape handler can tell it was used. As the panel
// opens, it is placed (see Placement) and focus moves as asked (see PopoverFocus).
export function usePopover<T extends HTMLElement, P extends HTMLElement>({
  placement = 'below',
  focus,
}: PopoverOptions<P> = {}): Popover<T, P> {
  const [open, setOpen] = useState(false)
  const [side, setSide] = useState<Side>('below')
  const [maxHeight, setMaxHeight] = useState<number | undefined>(undefined)
  const triggerRef = useRef<T>(null)
  const panelRef = useRef<P>(null)

  const close = (restoreFocus = false) => {
    setOpen(false)

    if (restoreFocus) {
      triggerRef.current?.focus()
    }
  }

  const show = () => {
    setOpen(true)
  }

  const toggle = () => {
    setOpen((was) => !was)
  }

  // Place the panel before it paints, so it never shows on the wrong side first.
  useLayoutEffect(() => {
    const trigger = triggerRef.current
    const panel = panelRef.current

    if (!open || placement === 'below' || trigger === null || panel === null) {
      return
    }

    const placed = place(trigger, panel, placement)

    setSide(placed.side)
    setMaxHeight(placed.maxHeight)
  }, [open, placement])

  // Focus moves as the latest options ask, without opening the panel again when they change.
  const moveFocus = useEffectEvent(() => {
    const panel = panelRef.current

    if (panel === null || focus === undefined) {
      return
    }

    if (focus === 'panel') {
      panel.focus()
    } else {
      focus(panel)?.focus()
    }
  })

  useEffect(() => {
    if (open) {
      moveFocus()
    }
  }, [open])

  // The listeners close with the latest close, so they are added once per opening.
  const dismiss = useEffectEvent((restoreFocus: boolean) => {
    close(restoreFocus)
  })

  useEffect(() => {
    if (!open) {
      return undefined
    }

    const inside = (target: EventTarget | null) =>
      target instanceof Node && (triggerRef.current?.contains(target) || panelRef.current?.contains(target)) === true

    // holdsPanel reports focus landing on something the panel is inside, which is no
    // focus elsewhere: the desktop's WebKit gives a radio, checkbox, or button no focus on
    // a click, and focuses the nearest focusable thing holding it instead, such as the
    // review sheet the panel opened in.
    const holdsPanel = (target: EventTarget | null) =>
      target instanceof Node && panelRef.current !== null && target.contains(panelRef.current)

    const onPointer = (e: MouseEvent) => {
      if (!inside(e.target)) {
        dismiss(false)
      }
    }

    const onFocus = (e: FocusEvent) => {
      if (!inside(e.target) && !holdsPanel(e.target)) {
        dismiss(false)
      }
    }

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        dismiss(true)
      }
    }

    document.addEventListener('mousedown', onPointer)
    document.addEventListener('focusin', onFocus)
    document.addEventListener('keydown', onKeyDown, true)

    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('focusin', onFocus)
      document.removeEventListener('keydown', onKeyDown, true)
    }
  }, [open])

  return { open, triggerRef, panelRef, side, maxHeight, toggle, show, close }
}
