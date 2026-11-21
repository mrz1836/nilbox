import {
  createContext,
  use,
  useEffect,
  useEffectEvent,
  useId,
  useLayoutEffect,
  useReducer,
  useRef,
  useState,
  type ActionDispatch,
  type ReactNode,
} from 'react'

import { emptyQueue, offerToast, turnOf, type ToastQueueState, type Turn } from '../lib/toastQueue'
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
  // background marks a toast nothing the user just did asked for, such as a worktree
  // cleanup's: under a ToastQueue it waits for the toast showing to close, rather than
  // show over it at once as one that answers a click does.
  background?: boolean
}

// ToastAction is a toast's button that leads somewhere, such as View.
export interface ToastAction {
  label: string
  onClick: () => void
}

// timing is when each toast's time began and the length it was timed for, kept beside the
// toast rather than in the Toast showing it, so a toast handed to another Toast, as the
// inbox's is into a sheet and back out, still closes when it would have. A pause, or a
// length changed under it, starts its time again.
const timing = new WeakMap<ToastMessage, { start: number; seconds: number }>()

// ToastOffer is a Toast offering its message to the queue, or taking it off with null.
interface ToastOffer {
  key: string
  toast: ToastMessage | null
}

// QueueContext is the window's toast queue, or null where none is, as in a component's own
// tests, where each Toast shows its message at once.
const QueueContext = createContext<{
  state: ToastQueueState<ToastMessage>
  offer: ActionDispatch<[ToastOffer]>
} | null>(null)

// queued is the queue once a Toast made its offer.
function queued(state: ToastQueueState<ToastMessage>, o: ToastOffer): ToastQueueState<ToastMessage> {
  return offerToast(state, o.key, o.toast)
}

// ToastQueue makes every Toast inside it take its turn (lib/toastQueue): one shows at a
// time, wherever it is drawn, a toast that answers a click first, and the one showing says
// its number while more wait ("1 of 3").
export function ToastQueue({ children }: { children: ReactNode }) {
  const [state, offer] = useReducer(queued, null, () => emptyQueue<ToastMessage>())

  return <QueueContext value={{ state, offer }}>{children}</QueueContext>
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
//
// Under a ToastQueue it offers its message to the queue and shows it only on its turn,
// timing it only while it shows; one pushed back into line by a newer toast gets its full
// time again when its turn comes.
export function Toast({ toast: offered, seconds, onClose }: ToastProps) {
  const queue = use(QueueContext)
  const key = useId()
  const offer = queue?.offer

  // The offer is made before the window paints, so a toast never shows a frame out of turn.
  useLayoutEffect(() => {
    offer?.({ key, toast: offered })
  }, [offer, key, offered])

  useLayoutEffect(
    () => () => {
      offer?.({ key, toast: null })
    },
    [offer, key],
  )

  const turn: Turn | null = queue === null ? null : turnOf(queue.state, key)
  const toast = turn === null || turn.showing ? offered : null

  // shownLast is the toast this Toast last showed, so one pushed back into line is timed
  // afresh when its turn comes again.
  const shownLast = useRef<ToastMessage | null>(null)

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
    const last = shownLast.current

    shownLast.current = toast

    if (toast === null) {
      if (last !== null && last === offered) {
        timing.delete(last)
      }

      return undefined
    }

    if (paused) {
      timing.delete(toast)

      return undefined
    }

    const kept = timing.get(toast)
    const start = kept?.seconds === seconds ? kept.start : Date.now()

    timing.set(toast, { start, seconds })

    const timer = setTimeout(
      () => {
        close()
      },
      Math.max(start + seconds * 1000 - Date.now(), 0),
    )

    return () => {
      clearTimeout(timer)
    }
  }, [toast, offered, paused, seconds])

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
          className="pointer-events-auto flex max-w-2xl min-w-0 items-start gap-3 rounded-lg border border-border bg-raised py-2.5 pr-2 pl-4 text-sm shadow-toast motion-safe:animate-pop-in"
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
          {turn !== null && turn.total > 1 && (
            <span className="mt-0.5 shrink-0 text-xs text-muted tabular-nums">
              {turn.position} of {turn.total}
            </span>
          )}
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
