import { useEffect, useEffectEvent, useLayoutEffect, type RefObject } from 'react'

// focusableSelector finds a dialog's controls for keeping keyboard focus inside it.
const focusableSelector =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex]:not([tabindex="-1"])'

// nextFocus is the control Tab (or Shift+Tab) should move to so focus wraps around the
// dialog instead of leaving it, or null when the browser's own move stays inside.
export function nextFocus(dialog: HTMLElement, backwards: boolean): HTMLElement | null {
  const controls = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector)).filter(
    (el) => el.closest('dialog') === dialog,
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

// isTopmost reports whether dialog is the open modal the user is in: the last open
// dialog in the page, as one opened from inside another (a confirmation over the review
// sheet) comes after it.
export function isTopmost(dialog: HTMLDialogElement): boolean {
  return Array.from(document.querySelectorAll('dialog[open]')).at(-1) === dialog
}

// useModalDialog runs the native modal <dialog> ref holds for as long as the component is
// mounted: it opens it as a modal (the rest of the page inert and dimmed behind it) and
// moves focus to what initialFocus returns, keeps Tab inside it, cancels on Escape, and
// gives focus back to where it was when it closes. Only the topmost open dialog hears the
// keys, so a confirmation opened over another dialog handles its own Escape and Tab. It
// returns the dialog's cancel handler, for a close request the system sends.
export function useModalDialog(
  ref: RefObject<HTMLDialogElement | null>,
  onCancel: () => void,
  initialFocus?: () => HTMLElement | null,
): (e: { preventDefault: () => void }) => void {
  // The focus target is read once, as the dialog opens.
  const focusFirst = useEffectEvent(() => {
    initialFocus?.()?.focus()
  })

  // Open as a modal on mount and move focus in; close and give focus back to the opener on
  // unmount. The opener is read before showModal, which moves focus itself.
  useLayoutEffect(() => {
    const opener = document.activeElement
    const dialog = ref.current

    dialog?.showModal()
    focusFirst()

    return () => {
      dialog?.close()

      if (opener instanceof HTMLElement && opener.isConnected) {
        opener.focus()
      }
    }
  }, [ref])

  // Escape cancels, with the latest onCancel, and Tab keeps focus inside the dialog.
  const handleKey = useEffectEvent((e: KeyboardEvent) => {
    const dialog = ref.current

    if (dialog === null || !isTopmost(dialog)) {
      return
    }

    if (e.key === 'Escape') {
      e.preventDefault()
      onCancel()

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

  // Any other close request, such as one the system sends, cancels the same way.
  return (e) => {
    e.preventDefault()
    onCancel()
  }
}
