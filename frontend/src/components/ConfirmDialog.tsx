import { useEffect, useId, useRef, type ReactNode } from 'react'

import { useModalDialog } from '../lib/useModalDialog'
import { buttonClass, dangerButtonClass, modalClass, primaryButtonClass } from './controls'
import { Spinner } from './Spinner'

export interface ConfirmDialogProps {
  title: string
  // children is the dialog's message: what confirming does.
  children: ReactNode
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  // onDismiss is what Escape does, for a question whose cancel button is an answer of its
  // own, as "Only what changes from now on" is; it defaults to onCancel.
  onDismiss?: (() => void) | undefined
  // tone colors the confirm button: danger for destructive actions (the default),
  // primary for safe ones.
  tone?: 'danger' | 'primary'
  // cancelLabel names the cancel button; null leaves only the confirm button, for a
  // message that only needs acknowledging.
  cancelLabel?: string | null
  // confirmDisabled turns the confirm button off, such as while nothing is chosen.
  confirmDisabled?: boolean
  // busy marks work the confirm button started as under way: both buttons are off, the
  // confirm button turns beside its label, which says what is happening, and Escape
  // cancels nothing until it ends. Focus a button lost as it went off comes back to the
  // safe choice when the work ends, as it was when the dialog opened.
  busy?: boolean
  // size gives the dialog room for a list: wide for prompts to choose among, large for a
  // long list grouped by repository.
  size?: 'normal' | 'wide' | 'large'
}

// sizeClass is each size's largest width.
const sizeClass: Readonly<Record<NonNullable<ConfirmDialogProps['size']>, string>> = {
  normal: 'max-w-md',
  wide: 'max-w-xl',
  large: 'max-w-3xl',
}

// ConfirmDialog asks the user to confirm an action, or to acknowledge a message. It is a
// native modal <dialog> (see useModalDialog): the rest of the page is inert and dimmed
// behind it while it is open. It starts with focus on Cancel (the safe choice) when there
// is one, keeps Tab inside itself, cancels on Escape, and returns focus to where it was
// when it closes. Render it only while it is open.
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  onConfirm,
  onCancel,
  onDismiss = onCancel,
  tone = 'danger',
  cancelLabel = 'Cancel',
  confirmDisabled = false,
  busy = false,
  size = 'normal',
}: ConfirmDialogProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const confirmRef = useRef<HTMLButtonElement>(null)
  const cancelWhenIdle = () => {
    if (!busy) {
      onDismiss()
    }
  }
  const cancel = useModalDialog(dialogRef, cancelWhenIdle, () => cancelRef.current ?? confirmRef.current)

  // wasBusy is busy as the last render had it. A button turned off loses focus, so once the
  // work ends with focus nowhere in the dialog, or on one of its buttons, the safe choice
  // takes it, as when the dialog opened: the user stays in the dialog, at its failure or a
  // new question, never on a button that now does something else. Focus the user moved to
  // another control in it meanwhile stays there.
  const wasBusy = useRef(busy)

  useEffect(() => {
    const ended = wasBusy.current && !busy
    const dialog = dialogRef.current
    const active = document.activeElement

    wasBusy.current = busy

    if (!ended || dialog === null) {
      return
    }

    if (!dialog.contains(active) || active === confirmRef.current || active === cancelRef.current) {
      ;(cancelRef.current ?? confirmRef.current)?.focus()
    }
  }, [busy])

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-body`}
      onCancel={cancel}
      className={`${modalClass} ${sizeClass[size]}`}
    >
      <h2 id={`${id}-title`} className="text-base font-semibold">
        {title}
      </h2>
      <div id={`${id}-body`} className="mt-2 text-sm text-muted">
        {children}
      </div>
      <div className="mt-4 flex justify-end gap-2">
        {cancelLabel !== null && (
          <button ref={cancelRef} type="button" disabled={busy} onClick={onCancel} className={buttonClass}>
            {cancelLabel}
          </button>
        )}
        <button
          ref={confirmRef}
          type="button"
          disabled={confirmDisabled || busy}
          aria-busy={busy || undefined}
          onClick={onConfirm}
          className={tone === 'danger' ? dangerButtonClass : primaryButtonClass}
        >
          {busy && <Spinner className="size-3.5" />}
          {confirmLabel}
        </button>
      </div>
    </dialog>
  )
}
