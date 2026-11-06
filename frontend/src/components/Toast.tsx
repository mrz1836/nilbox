import { useEffect, useState } from 'react'

import { XIcon } from './icons'

// toastMs is how long a toast shows, unless the pointer rests on it.
export const toastMs = 6000

// ToastMessage is one short confirmation, such as "Dismissed", with an optional Undo.
export interface ToastMessage {
  // id tells one toast from the next, so each gets its full time on screen.
  id: number
  text: string
  // detail is quieter text after the message, such as the item's title.
  detail?: string
  // tone colors the message; an error is red.
  tone?: 'normal' | 'error'
  // undo, when set, puts an Undo button on the toast.
  undo?: () => void
}

export interface ToastProps {
  // toast is the message showing, or null for none.
  toast: ToastMessage | null
  onClose: () => void
}

// Toast shows a short confirmation at the bottom of the window. It closes itself after a
// few seconds, waits while the pointer rests on it or focus is in it (so Undo can be
// reached with the keyboard), and is announced politely to screen readers: the live
// region stays mounted, so each new message is read.
export function Toast({ toast, onClose }: ToastProps) {
  // hoveredId and focusedId are the toast the pointer rests on and the one focus is in.
  // Keyed by toast, a pause never outlives the toast it was for, even when that one is
  // replaced under the pointer.
  const [hoveredId, setHoveredId] = useState<number | null>(null)
  const [focusedId, setFocusedId] = useState<number | null>(null)
  const paused = toast !== null && (hoveredId === toast.id || focusedId === toast.id)

  useEffect(() => {
    if (toast === null || paused) {
      return undefined
    }

    const timer = setTimeout(onClose, toastMs)

    return () => {
      clearTimeout(timer)
    }
  }, [toast, paused, onClose])

  const error = toast?.tone === 'error'

  return (
    <div
      role="status"
      aria-live="polite"
      className="pointer-events-none fixed inset-x-0 bottom-5 z-50 flex justify-center px-6"
    >
      {toast !== null && (
        // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- pausing a timed message while it is pointed at or focused is the timing adjustment WCAG 2.2.1 asks for, not a control
        <div
          key={toast.id}
          role="group"
          aria-label="Notification"
          onMouseEnter={() => {
            setHoveredId(toast.id)
          }}
          onMouseLeave={() => {
            setHoveredId(null)
          }}
          onFocus={() => {
            setFocusedId(toast.id)
          }}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget)) {
              setFocusedId(null)
            }
          }}
          className="pointer-events-auto flex max-w-xl min-w-0 items-center gap-3 rounded-lg border border-border bg-raised py-1.5 pr-1.5 pl-3.5 text-sm shadow-xl shadow-black/30 motion-safe:animate-pop-in"
        >
          <p className="min-w-0 truncate">
            <span className={`font-medium ${error ? 'text-danger' : 'text-text'}`}>{toast.text}</span>
            {toast.detail !== undefined && toast.detail !== '' && <span className="text-muted"> · {toast.detail}</span>}
          </p>
          {toast.undo !== undefined && (
            <button
              type="button"
              onClick={toast.undo}
              className="shrink-0 rounded-md px-2 py-1 text-sm font-medium text-accent transition-colors hover:bg-surface"
            >
              Undo
            </button>
          )}
          <button
            type="button"
            aria-label="Close"
            title="Close"
            onClick={onClose}
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-surface hover:text-text"
          >
            <XIcon className="size-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
