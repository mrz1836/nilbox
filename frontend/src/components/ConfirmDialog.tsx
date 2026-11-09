import { useId, useRef, type ReactNode } from 'react'

import { useModalDialog } from '../lib/useModalDialog'
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
  tone = 'danger',
  cancelLabel = 'Cancel',
  confirmDisabled = false,
  size = 'normal',
}: ConfirmDialogProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const confirmRef = useRef<HTMLButtonElement>(null)
  const cancel = useModalDialog(dialogRef, onCancel, () => cancelRef.current ?? confirmRef.current)

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-body`}
      onCancel={cancel}
      // The backdrop's color is spelled out: before Safari 17.4, ::backdrop sees none of the
      // page's custom properties, so a theme color would leave it clear.
      className={`m-auto w-[calc(100%-2rem)] ${sizeClass[size]} rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-[rgb(0_0_0/0.4)]`}
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
