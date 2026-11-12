import { useEffectEvent, useLayoutEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

import { useModalDialog } from '../lib/useModalDialog'
import type { InboxItem } from '../platform'
import { quietButtonClass } from './controls'
import { IssueIcon, PullRequestIcon, XIcon } from './icons'

export interface LaunchDialogProps {
  // id is the dialog's ID, which the button that opened it names in aria-controls.
  id: string
  // title is the dialog's heading, the launch it makes: "Open in Codex".
  title: string
  // item is the pull request or issue the launch is for, named under the title.
  item: InboxItem
  onClose: () => void
  // children are the launch options, which fill the dialog under its heading.
  children: ReactNode
}

// initialFocus is the control focus starts on: the one marked for it (the Prompt select),
// else the first chosen option, else the first control, else the dialog's body, when it
// shows no options.
function initialFocus(body: HTMLElement | null): HTMLElement | null {
  return (
    body?.querySelector<HTMLElement>('select[data-initial-focus], [data-initial-focus] input:checked') ??
    body?.querySelector<HTMLElement>('input:checked') ??
    body?.querySelector<HTMLElement>('input') ??
    body
  )
}

// LaunchDialog holds one item's launch options in the middle of the window, so a row at
// the top or bottom of a list never cuts them off. It is a native modal <dialog> (see
// useModalDialog), its accessible name "Launch options" as the button that opens it says,
// and one opened over the review sheet hears the keys alone: Escape closes it and leaves
// the sheet open. Focus starts on the chosen prompt, the choice most often changed, and goes
// back to the button that opened it as it closes: on Escape, the close button, or a click on
// the backdrop. The heading stays at the top while the options under it scroll.
//
// It is drawn in the page's body rather than inside the row that opens it, so the row
// doesn't count the pointer or focus in the dialog as its own and light up behind the
// backdrop. A page that hides with it open (the window's views are Activities, kept while
// another shows) closes it, so it never shows again unasked, nor opens before the review
// sheet it was opened over. Render it only while it is open.
//
// The dialog has no height of its own, only a cap, so it is as tall as what it holds. No
// column inside it may take flex-1 (a basis of 0%): the desktop's WebKit then counts the
// column as empty, and the whole dialog collapses to its border over the dimmed window.
// Each column keeps its content's height and shrinks under the cap (min-h-0), the options
// scrolling between the heading and the buttons.
export function LaunchDialog({ id, title, item, onClose, children }: LaunchDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const cancel = useModalDialog(dialogRef, onClose, () => initialFocus(bodyRef.current), { backdrop: true })
  // live says the dialog's effects are running: they stop as its page hides, or it closes.
  const live = useRef(false)

  const pageHidden = useEffectEvent(() => {
    onClose()
  })

  // A page that hides keeps the dialog in the document, where a closed one is gone; and
  // StrictMode runs the effects again at once after this cleanup, where a hidden page
  // waits. So the close waits a microtask and happens only for a hidden page.
  useLayoutEffect(() => {
    const dialog = dialogRef.current

    live.current = true

    return () => {
      live.current = false

      queueMicrotask(() => {
        if (!live.current && dialog?.isConnected === true) {
          pageHidden()
        }
      })
    }
  }, [])

  const name = `${item.repo}#${String(item.number)}`

  return createPortal(
    <dialog
      ref={dialogRef}
      id={id}
      aria-modal="true"
      aria-label="Launch options"
      aria-describedby={`${id}-item`}
      onCancel={cancel}
      // The backdrop's color is spelled out, as ConfirmDialog's is, for older Safari.
      className="m-auto max-h-[calc(100%-3rem)] w-[calc(100%-2rem)] max-w-[34rem] overflow-hidden rounded-xl border border-border bg-surface p-0 text-text shadow-xl backdrop:bg-[rgb(0_0_0/0.4)] open:flex open:flex-col motion-safe:animate-pop-in"
    >
      {/* The body takes focus itself, so a click on a radio, checkbox, or button, which the
          desktop's WebKit focuses no control for, leaves focus in the dialog. */}
      <div ref={bodyRef} tabIndex={-1} className="flex min-h-0 flex-col outline-none">
        <header className="flex shrink-0 items-start gap-3 border-b border-border py-3 pr-2.5 pl-3.5">
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-base font-semibold">{title}</h2>
            <p
              id={`${id}-item`}
              title={`${name} · ${item.title}`}
              className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted"
            >
              {item.kind === 'pr' ? (
                <PullRequestIcon className="size-3.5 shrink-0" />
              ) : (
                <IssueIcon className="size-3.5 shrink-0" />
              )}
              <span className="truncate">
                <span className="font-medium text-text">{name}</span>
                {` · ${item.title}`}
              </span>
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            title="Close (Esc)"
            onClick={onClose}
            className={`${quietButtonClass} p-1`}
          >
            <XIcon className="size-4" />
          </button>
        </header>
        {children}
      </div>
    </dialog>,
    document.body,
  )
}
