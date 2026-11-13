import { useEffectEvent, useLayoutEffect, useRef, type ReactNode } from 'react'

import { useModalDialog } from '../lib/useModalDialog'

export interface SheetProps {
  // labelledBy is the ID of the element that names the sheet, such as its title.
  labelledBy: string
  onClose: () => void
  // initialFocus selects the control focus starts on as the sheet opens, such as the step
  // arrow a step to an item of another kind was taken with, which opens the other kind's
  // sheet in its place; without it, or with nothing it selects that can take focus, focus
  // starts on the sheet.
  initialFocus?: string | undefined
  // refocus moves focus as the sheet opens whenever it changes, for a sheet that shows
  // another item in place, as a link in it or its step back does: the control that moved
  // it may be gone with what it showed, which would leave focus nowhere.
  refocus?: number | undefined
  // toast is a message shown over the sheet, such as the inbox's toast with its Undo,
  // which the page behind the sheet can't show: the sheet makes it inert.
  toast?: ReactNode
  children: ReactNode
}

// Sheet is a large modal over the window, most of its size, for work that needs room, such
// as reviewing a pull request. Like ConfirmDialog it is a native modal <dialog> (see
// useModalDialog): the page behind it is inert, Escape closes it, Tab stays inside, and
// focus goes back where it was when it closes. Focus starts on the sheet itself, so its
// own keys work at once, unless initialFocus picks a control in it. Render it only while
// it is open.
export function Sheet({ labelledBy, onClose, initialFocus, refocus = 0, toast, children }: SheetProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const firstFocus = () => {
    const body = bodyRef.current
    const picked = initialFocus === undefined ? null : body?.querySelector<HTMLElement>(initialFocus)

    // A control that can't take focus, being disabled, leaves focus on the sheet.
    return picked === null || picked === undefined || picked.matches(':disabled') ? body : picked
  }
  const cancel = useModalDialog(dialogRef, onClose, firstFocus)
  const focusAgain = useEffectEvent(() => {
    firstFocus()?.focus()
  })

  // As it opens this repeats what the dialog just did; after that it runs only when
  // refocus changes.
  useLayoutEffect(() => {
    focusAgain()
  }, [refocus])

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
      {toast}
    </dialog>
  )
}
