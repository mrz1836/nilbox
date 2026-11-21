import { useId, useRef } from 'react'

import { useModalDialog } from '../../lib/useModalDialog'
import { quietButtonClass, sheetDialogClass } from '../controls'
import { XIcon } from '../icons'
import { CardGuide } from './CardGuide'

export interface CardGuideDialogProps {
  onClose: () => void
}

// CardGuideDialog shows the full card guide, "How to read a card", in a native modal
// <dialog> (useModalDialog, as ConfirmDialog is): the rest of the window inert and dimmed
// behind it, focus on Close as it opens, Escape or Close to close it, and focus back where
// it was. The window owns whether it is open: the inbox's "How to read a card" button and
// Settings → Cards open it. Render it only while it is open. It loads apart from the inbox
// (App.tsx), so the inbox's first paint never pays for it.
export function CardGuideDialog({ onClose }: CardGuideDialogProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const cancel = useModalDialog(dialogRef, onClose, () => closeRef.current)

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      onCancel={cancel}
      className={`${sheetDialogClass} max-h-[calc(100%-3rem)] w-[calc(100%-2rem)] max-w-5xl overflow-y-auto motion-safe:animate-pop-in`}
    >
      <header className="flex items-center gap-3 border-b border-border py-3 pr-2.5 pl-5">
        <h2 id={`${id}-title`} className="min-w-0 flex-1 text-base font-semibold">
          How to read a card
        </h2>
        <button
          ref={closeRef}
          type="button"
          aria-label="Close"
          title="Close (Esc)"
          onClick={onClose}
          className={`${quietButtonClass} p-1`}
        >
          <XIcon className="size-4" />
        </button>
      </header>
      <div className="px-5 pt-2 pb-5">
        <CardGuide variant="full" />
      </div>
    </dialog>
  )
}
