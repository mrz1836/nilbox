import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'

// Popover is the state of a button that opens a floating panel.
export interface Popover<T extends HTMLElement, P extends HTMLElement> {
  open: boolean
  // triggerRef and panelRef go on the button and the panel; clicks and focus inside
  // either keep the panel open.
  triggerRef: RefObject<T | null>
  panelRef: RefObject<P | null>
  toggle: () => void
  show: () => void
  // close hides the panel, moving focus back to the button when restoreFocus is set.
  close: (restoreFocus?: boolean) => void
}

// usePopover opens and closes a floating panel the way a desktop menu behaves: a click or
// focus anywhere outside the button and the panel closes it, and Escape closes it and
// puts focus back on the button. Escape is taken in the capture phase and marked handled
// (defaultPrevented), so a page-level Escape handler can tell it was used.
export function usePopover<T extends HTMLElement, P extends HTMLElement>(): Popover<T, P> {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<T>(null)
  const panelRef = useRef<P>(null)

  const close = useCallback((restoreFocus = false) => {
    setOpen(false)

    if (restoreFocus) {
      triggerRef.current?.focus()
    }
  }, [])

  const show = useCallback(() => {
    setOpen(true)
  }, [])

  const toggle = useCallback(() => {
    setOpen((was) => !was)
  }, [])

  useEffect(() => {
    if (!open) {
      return undefined
    }

    const inside = (target: EventTarget | null) =>
      target instanceof Node && (triggerRef.current?.contains(target) || panelRef.current?.contains(target)) === true

    const onPointer = (e: MouseEvent) => {
      if (!inside(e.target)) {
        close()
      }
    }

    const onFocus = (e: FocusEvent) => {
      if (!inside(e.target)) {
        close()
      }
    }

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        close(true)
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
  }, [open, close])

  return { open, triggerRef, panelRef, toggle, show, close }
}
