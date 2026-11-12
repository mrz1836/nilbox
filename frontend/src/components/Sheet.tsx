import { useRef, type ReactNode } from 'react'

import { useModalDialog } from '../lib/useModalDialog'

export interface SheetProps {
  // labelledBy is the ID of the element that names the sheet, such as its title.
  labelledBy: string
  onClose: () => void
  children: ReactNode
}

// Sheet is a large modal over the window, most of its size, for work that needs room, such
// as reviewing a pull request. Like ConfirmDialog it is a native modal <dialog> (see
// useModalDialog): the page behind it is inert, Escape closes it, Tab stays inside, and
// focus goes back where it was when it closes. Focus starts on the sheet itself, so its
// own keys work at once. Render it only while it is open.
export function Sheet({ labelledBy, onClose, children }: SheetProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const cancel = useModalDialog(dialogRef, onClose, () => bodyRef.current)

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={labelledBy}
      onCancel={cancel}
      // The backdrop's color is spelled out, as ConfirmDialog's is, for older Safari. The
      // sheet cuts off what runs past it with overflow: clip, never hidden, which would
      // leave the sheet itself scrollable: a file picked in its list (scrollIntoView) could
      // then scroll the header away. Only its own panes scroll, as in App's window column.
      className="m-auto h-[calc(100%-3rem)] max-h-none w-[calc(100%-3rem)] max-w-none overflow-clip rounded-xl border border-border bg-surface p-0 text-text shadow-xl backdrop:bg-[rgb(0_0_0/0.4)] open:flex open:flex-col"
    >
      <div ref={bodyRef} tabIndex={-1} className="relative flex min-h-0 flex-1 flex-col outline-none">
        {children}
      </div>
    </dialog>
  )
}
