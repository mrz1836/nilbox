import { useEffect, useEffectEvent, useLayoutEffect, useRef, type RefObject } from 'react'

import { focusBack } from './focusBack'

// focusableSelector finds a dialog's controls for keeping keyboard focus inside it.
const focusableSelector =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex]:not([tabindex="-1"])'

// nextFocus is the control Tab (or Shift+Tab) should move to so focus wraps around the
// dialog instead of leaving it, or null when the browser's own move stays inside. A
// control in an inert part of the dialog, such as the card guide's sample card, takes no
// focus, so it is passed over.
export function nextFocus(dialog: HTMLElement, backwards: boolean): HTMLElement | null {
  const controls = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector)).filter(
    (el) => el.closest('dialog') === dialog && el.closest('[inert]') === null,
  )
  const first = controls[0]
  const last = controls.at(-1)

  if (first === undefined || last === undefined) {
    return null
  }

  const current = document.activeElement

  if (!dialog.contains(current)) {
    return backwards ? last : first
  }

  if (backwards && current === first) {
    return last
  }

  if (!backwards && current === last) {
    return first
  }

  return null
}

// opened lists the modals useModalDialog has open, in the order they opened: the order the
// browser stacks them in, the last on top, wherever each sits in the page.
const opened: HTMLDialogElement[] = []

// isTopmost reports whether dialog is the open modal the user is in: the one the browser
// shows on top, the last useModalDialog opened, as a confirmation opened over the review
// sheet is, or the quit's question opened while another dialog shows, though the page
// renders that one later. A dialog inside another is over it, whichever opened first, as a
// picker in a sheet is. A dialog opened any other way counts by its place in the page,
// beneath those.
export function isTopmost(dialog: HTMLDialogElement): boolean {
  const open = opened.filter((d) => d.open && d.isConnected)
  let top = open.at(-1)

  if (top === undefined) {
    return Array.from(document.querySelectorAll('dialog[open]')).at(-1) === dialog
  }

  for (let inner = innerOf(top, open); inner !== undefined; inner = innerOf(top, open)) {
    top = inner
  }

  return top === dialog
}

// anyModalOpen reports a modal open over the page, a confirmation or a sheet, whose page
// a shortcut such as ⌘S must then leave alone.
export function anyModalOpen(): boolean {
  return document.querySelector('dialog[open]') !== null
}

// innerOf is the last of open to have opened inside outer, or undefined for none.
function innerOf(outer: HTMLDialogElement, open: readonly HTMLDialogElement[]): HTMLDialogElement | undefined {
  return open.findLast((d) => d !== outer && outer.contains(d))
}

// forget takes dialog out of the opened modals.
function forget(dialog: HTMLDialogElement): void {
  const at = opened.indexOf(dialog)

  if (at !== -1) {
    opened.splice(at, 1)
  }
}

// useCloseWhenHidden closes, through onClose, a modal <dialog> whose page hides while it is
// open (the window's views are Activities, kept while another shows), so it never shows
// again unasked, nor opens again before a dialog it was opened over, which would then
// cover it. A page that hides keeps the dialog in the document, where a closed one is
// gone; and StrictMode runs the effects again at once after this cleanup, where a hidden
// page waits. So the close waits a microtask and happens only for a hidden page.
export function useCloseWhenHidden(ref: RefObject<HTMLDialogElement | null>, onClose: () => void): void {
  // live says the dialog's effects are running: they stop as its page hides, or it closes.
  const live = useRef(false)

  const pageHidden = useEffectEvent(() => {
    onClose()
  })

  useLayoutEffect(() => {
    const dialog = ref.current

    live.current = true

    return () => {
      live.current = false

      queueMicrotask(() => {
        if (!live.current && dialog?.isConnected === true) {
          pageHidden()
        }
      })
    }
  }, [ref])
}

export interface ModalDialogOptions {
  // backdrop cancels the dialog on a click on its backdrop, as Escape does. A press that
  // begins inside the dialog and ends on the backdrop, such as a drag selecting text,
  // doesn't. The dialog's content must fill it (no padding of its own), so that a click
  // on the dialog element itself is one on the backdrop.
  backdrop?: boolean
}

// ModalCancelEvent is the part of a dialog's cancel event the handler uses.
export interface ModalCancelEvent {
  preventDefault: () => void
  stopPropagation: () => void
}

// useModalDialog runs the native modal <dialog> ref holds for as long as the component is
// mounted: it opens it as a modal (the rest of the page inert and dimmed behind it) and
// moves focus to what initialFocus returns, keeps Tab inside it, cancels on Escape (and,
// with the backdrop option, on a click on the backdrop), and gives focus back to where it
// was when it first opened as it closes, ringed only after a key (focusBack). Only the
// topmost open dialog hears the keys, the last opened (isTopmost), so a confirmation
// opened over another dialog handles its own Escape and Tab. It returns the dialog's
// cancel handler, for a close request the system sends.
export function useModalDialog(
  ref: RefObject<HTMLDialogElement | null>,
  onCancel: () => void,
  initialFocus?: () => HTMLElement | null,
  { backdrop = false }: ModalDialogOptions = {},
): (e: ModalCancelEvent) => void {
  // opener is where focus was as the dialog first opened. A page hidden and shown again
  // (an Activity) opens the dialog again, while focus is somewhere that page no longer
  // holds, so only the first opening is kept.
  const opener = useRef<Element | null | undefined>(undefined)

  // The focus target is read once, as the dialog opens.
  const focusFirst = useEffectEvent(() => {
    initialFocus?.()?.focus()
  })

  // Open as a modal on mount and move focus in; close and give focus back to the opener on
  // unmount. The opener is read before showModal, which moves focus itself.
  useLayoutEffect(() => {
    const dialog = ref.current

    if (opener.current === undefined) {
      opener.current = document.activeElement
    }

    if (dialog !== null) {
      forget(dialog)
      dialog.showModal()
      opened.push(dialog)
    }

    focusFirst()

    return () => {
      if (dialog !== null) {
        forget(dialog)
        dialog.close()
      }

      const back = opener.current

      if (back instanceof HTMLElement && back.isConnected) {
        focusBack(back)
      }
    }
  }, [ref])

  // A click on the backdrop cancels, with the latest onCancel.
  const dismiss = useEffectEvent(() => {
    onCancel()
  })

  useEffect(() => {
    const dialog = ref.current

    if (!backdrop || dialog === null) {
      return undefined
    }

    // pressed records that a press began on the backdrop, so a click that ends there
    // cancels, but a drag out of the dialog does not.
    let pressed = false

    const onMouseDown = (e: MouseEvent) => {
      pressed = e.target === dialog
    }

    const onClick = (e: MouseEvent) => {
      if (pressed && e.target === dialog) {
        dismiss()
      }

      pressed = false
    }

    dialog.addEventListener('mousedown', onMouseDown)
    dialog.addEventListener('click', onClick)

    return () => {
      dialog.removeEventListener('mousedown', onMouseDown)
      dialog.removeEventListener('click', onClick)
    }
  }, [ref, backdrop])

  // Escape cancels, with the latest onCancel, unless a menu open in the dialog already took
  // it (usePopover), and Tab keeps focus inside the dialog.
  const handleKey = useEffectEvent((e: KeyboardEvent) => {
    const dialog = ref.current

    if (dialog === null || !isTopmost(dialog)) {
      return
    }

    if (e.key === 'Escape') {
      if (!e.defaultPrevented) {
        e.preventDefault()
        onCancel()
      }

      return
    }

    if (e.key === 'Tab') {
      const target = nextFocus(dialog, e.shiftKey)

      if (target !== null) {
        e.preventDefault()
        target.focus()
      }
    }
  })

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      handleKey(e)
    }

    // The capture phase runs this before any page-level Escape handler, which then sees
    // the key as handled (defaultPrevented) and leaves the page open. Handling the key
    // also keeps the browser from closing the dialog itself, which only the owner of the
    // open state may do.
    document.addEventListener('keydown', onKeyDown, true)

    return () => {
      document.removeEventListener('keydown', onKeyDown, true)
    }
  }, [])

  // Any other close request, such as one the system sends, cancels the same way. React
  // passes a cancel event on to the handlers of the dialogs around this one, as if it
  // bubbled, so it stops here: cancelling a dialog opened over another leaves that one open.
  return (e) => {
    e.preventDefault()
    e.stopPropagation()
    onCancel()
  }
}
