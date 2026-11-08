import { useEffect, useEffectEvent, useId, useLayoutEffect, useRef, type ReactNode } from 'react'

import { buttonClass, dangerButtonClass, primaryButtonClass } from './controls'

export interface ConfirmDialogProps {
  title: string
  // children is the dialog's message: what confirming does.
  children: ReactNode
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  // tone colors the confirm button: danger for destructive actions (the default),
  // primary for safe ones.
  tone?: 'danger' | 'primary'
  // cancelLabel names the cancel button; null leaves only the confirm button, for a
  // message that only needs acknowledging.
  cancelLabel?: string | null
  // confirmDisabled turns the confirm button off, such as while nothing is chosen.
  confirmDisabled?: boolean
  // wide gives the dialog room for a list, such as prompts to choose among.
  wide?: boolean
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

// ConfirmDialog asks the user to confirm an action, or to acknowledge a message. It is a
// native modal <dialog>: the rest of the page is inert and dimmed behind it while it is
// open. It starts with focus on Cancel (the safe choice) when there is one, keeps Tab
// inside itself, cancels on Escape, and returns focus to where it was when it closes.
// Render it only while it is open.
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  onConfirm,
  onCancel,
  tone = 'danger',
  cancelLabel = 'Cancel',
  confirmDisabled = false,
  wide = false,
}: ConfirmDialogProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const confirmRef = useRef<HTMLButtonElement>(null)

  // Open as a modal on mount and move focus in; close and give focus back to the opener on
  // unmount. The opener is read before showModal, which moves focus itself.
  useLayoutEffect(() => {
    const opener = document.activeElement
    const dialog = dialogRef.current

    dialog?.showModal()
    ;(cancelRef.current ?? confirmRef.current)?.focus()

    return () => {
      dialog?.close()

      if (opener instanceof HTMLElement && opener.isConnected) {
        opener.focus()
      }
    }
  }, [])

  // Escape cancels, with the latest onCancel, and Tab keeps focus inside the dialog.
  const handleKey = useEffectEvent((e: KeyboardEvent) => {
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

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-body`}
      onCancel={(e) => {
        // Any other close request, such as one the system sends, cancels the same way.
        e.preventDefault()
        onCancel()
      }}
      // The backdrop's color is spelled out: before Safari 17.4, ::backdrop sees none of the
      // page's custom properties, so a theme color would leave it clear.
      className={`m-auto w-[calc(100%-2rem)] ${wide ? 'max-w-xl' : 'max-w-md'} rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-[rgb(0_0_0/0.4)]`}
    >
      <h2 id={`${id}-title`} className="text-base font-semibold">
        {title}
      </h2>
      <div id={`${id}-body`} className="mt-2 text-sm text-muted">
        {children}
      </div>
      <div className="mt-4 flex justify-end gap-2">
        {cancelLabel !== null && (
          <button ref={cancelRef} type="button" onClick={onCancel} className={buttonClass}>
            {cancelLabel}
          </button>
        )}
        <button
          ref={confirmRef}
          type="button"
          disabled={confirmDisabled}
          onClick={onConfirm}
          className={tone === 'danger' ? dangerButtonClass : primaryButtonClass}
        >
          {confirmLabel}
        </button>
      </div>
    </dialog>
  )
}
