import { useEffect, useId, useRef, type ReactNode } from 'react'

export interface ConfirmDialogProps {
  title: string
  // children is the dialog's message: what confirming does.
  children: ReactNode
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
}

// focusableSelector finds the dialog's controls for keeping keyboard focus inside it.
const focusableSelector = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled)'

// nextFocus is the control Tab (or Shift+Tab) should move to so focus wraps around the
// dialog instead of leaving it, or null when the browser's own move stays inside.
function nextFocus(dialog: HTMLElement, backwards: boolean): HTMLElement | null {
  const controls = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector))
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

// ConfirmDialog asks the user to confirm a destructive action. It is modal: it starts
// with focus on Cancel (the safe choice), keeps Tab inside itself, cancels on Escape, and
// returns focus to where it was when it closes. Render it only while it is open.
export function ConfirmDialog({ title, children, confirmLabel, onConfirm, onCancel }: ConfirmDialogProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)

  // Move focus into the dialog on open, and back to the opener on close.
  useEffect(() => {
    const opener = document.activeElement

    cancelRef.current?.focus()

    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) {
        opener.focus()
      }
    }
  }, [])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCancel()

        return
      }

      const dialog = dialogRef.current

      if (e.key === 'Tab' && dialog !== null) {
        const target = nextFocus(dialog, e.shiftKey)

        if (target !== null) {
          e.preventDefault()
          target.focus()
        }
      }
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [onCancel])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${id}-title`}
        aria-describedby={`${id}-body`}
        className="w-full max-w-md rounded-lg border border-border bg-surface p-4 text-text shadow-lg"
      >
        <h2 id={`${id}-title`} className="text-base font-semibold">
          {title}
        </h2>
        <div id={`${id}-body`} className="mt-2 text-sm text-muted">
          {children}
        </div>
        <div className="mt-4 flex justify-end gap-2 text-sm">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            className="rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className="rounded-md bg-danger px-3 py-1 font-medium text-background hover:opacity-90"
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
