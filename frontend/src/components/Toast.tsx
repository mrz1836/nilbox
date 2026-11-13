import { useEffect, useEffectEvent, useState } from 'react'

import { AlertIcon, CircleCheckIcon, XIcon } from './icons'

// How long a toast can be set to show, in seconds, unless the pointer rests on it, and how
// long it shows until the settings load: long enough to read a message and reach its
// button. Settings → Notifications sets it; internal/settings keeps the same three, and a
// test holds the two in step.
export const minToastSeconds = 5
export const maxToastSeconds = 60
export const defaultToastSeconds = 12

// toastButtonClass is a toast's text button, such as Undo.
const toastButtonClass =
  'max-w-full truncate rounded-md px-2 py-1 text-sm font-medium text-accent transition-colors hover:bg-surface'

// ToastMessage is one short confirmation, such as "Dismissed", with an optional Undo.
export interface ToastMessage {
  // id tells one toast from the next, so each gets its full time on screen.
  id: number
  text: string
  // detail is quieter text after the message, such as the item's title.
  detail?: string
  // tone colors the message: an error is red, and a change done in full, or in part,
  // leads with a green check or an amber warning.
  tone?: 'normal' | 'success' | 'warning' | 'error'
  // undo, when set, puts an Undo button on the toast.
  undo?: () => void
  // action, when set, puts a button with its label on the toast, such as View.
  action?: ToastAction
}

// ToastAction is a toast's button that leads somewhere, such as View.
export interface ToastAction {
  label: string
  onClick: () => void
}

export interface ToastProps {
  // toast is the message showing, or null for none.
  toast: ToastMessage | null
  // seconds is how long a toast shows before it closes itself, as set in Settings.
  seconds: number
  onClose: () => void
}

// Toast shows a short confirmation at the bottom of the window, its message wrapping over
// its buttons, with a button to close it. It closes itself after seconds, waits while the
// pointer rests on it or focus is in it (so Undo can be reached with the keyboard), and
// is announced politely to screen readers: the live region stays mounted, so each new
// message is read.
export function Toast({ toast, seconds, onClose }: ToastProps) {
  // hoveredId and focusedId are the toast the pointer rests on and the one focus is in.
  // Keyed by toast, a pause never outlives the toast it was for, even when that one is
  // replaced under the pointer.
  const [hoveredId, setHoveredId] = useState<number | null>(null)
  const [focusedId, setFocusedId] = useState<number | null>(null)
  const paused = toast !== null && (hoveredId === toast.id || focusedId === toast.id)

  // The timer closes with the latest onClose, so a new one never restarts it.
  const close = useEffectEvent(() => {
    onClose()
  })

  useEffect(() => {
    if (toast === null || paused) {
      return undefined
    }

    const timer = setTimeout(() => {
      close()
    }, seconds * 1000)

    return () => {
      clearTimeout(timer)
    }
  }, [toast, paused, seconds])

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
          className="pointer-events-auto flex max-w-2xl min-w-0 items-start gap-3 rounded-lg border border-border bg-raised py-2.5 pr-2 pl-4 text-sm shadow-xl shadow-black/30 motion-safe:animate-pop-in"
        >
          {toast.tone === 'success' && <CircleCheckIcon className="mt-0.5 size-4 shrink-0 text-success" />}
          {toast.tone === 'warning' && <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />}
          <div className="min-w-0 flex-1">
            {/* The message wraps, whole, rather than being cut short beside a button; the
                buttons sit on their own line under it. */}
            <p className="leading-snug break-words">
              <span className={`font-medium ${error ? 'text-danger' : 'text-text'}`}>{toast.text}</span>
              {toast.detail !== undefined && toast.detail !== '' && (
                <span className="text-muted"> · {toast.detail}</span>
              )}
            </p>
            {(toast.undo !== undefined || toast.action !== undefined) && (
              <div className="mt-1 -ml-2 flex min-w-0 flex-wrap items-center gap-1">
                {toast.undo !== undefined && (
                  <button type="button" onClick={toast.undo} className={toastButtonClass}>
                    Undo
                  </button>
                )}
                {toast.action !== undefined && (
                  <button
                    type="button"
                    title={toast.action.label}
                    onClick={toast.action.onClick}
                    className={toastButtonClass}
                  >
                    {toast.action.label}
                  </button>
                )}
              </div>
            )}
          </div>
          <button
            type="button"
            aria-label="Close"
            title="Close"
            onClick={onClose}
            className="-my-0.5 flex size-8 shrink-0 items-center justify-center rounded-md text-text/70 transition-colors hover:bg-surface hover:text-text"
          >
            <XIcon className="size-4" />
          </button>
        </div>
      )}
    </div>
  )
}
