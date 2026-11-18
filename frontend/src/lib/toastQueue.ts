// The window shows one toast at a time. Every Toast offers its message to one queue (the
// ToastQueue in components/Toast), keyed by the Toast that shows it, and shows it only on
// its turn. A toast that answers the user's click goes first, newest first, so the click
// is answered at once, and pushes the toast showing back into line rather than drop it,
// so no Undo is lost; a background toast, which nothing the user just did asked for, such
// as a worktree cleanup's, waits its turn behind the rest. A source that replaces its own
// toast replaces it in place, as each always has. The queue is pure, so its rules are
// tested apart from the timers.

// Queued is what the queue needs of a toast: whether it is a background one.
export interface Queued {
  background?: boolean
}

// QueueEntry is one Toast's message, showing or waiting: key names the Toast.
export interface QueueEntry<T extends Queued> {
  key: string
  toast: T
}

// ToastQueueState is the queue.
export interface ToastQueueState<T extends Queued> {
  // entries are the toasts to show, the one showing first: those that answer a click,
  // newest first, then the background ones, oldest first.
  entries: readonly QueueEntry<T>[]
  // done counts the toasts shown and taken off since the queue was last empty, which
  // numbers the one showing ("2 of 3").
  done: number
  // closed is the toast showing that was taken off last, so one handed at once to another
  // Toast, as the inbox's is into a sheet and back out, keeps its place and its number.
  closed: T | null
}

// emptyQueue is a queue with nothing in it.
export function emptyQueue<T extends Queued>(): ToastQueueState<T> {
  return { entries: [], done: 0, closed: null }
}

// offerToast is the queue once the Toast key offers toast, or, with null, takes its
// message off: the one showing taken off counts as done, and the next one shows.
export function offerToast<T extends Queued>(
  state: ToastQueueState<T>,
  key: string,
  toast: T | null,
): ToastQueueState<T> {
  const at = state.entries.findIndex((e) => e.key === key)

  if (toast === null) {
    return at < 0 ? state : takenOff(state, at)
  }

  if (at >= 0 && state.entries[at]?.toast === toast) {
    return state
  }

  // A toast another Toast holds was handed to this one: it keeps its place.
  if (state.entries.some((e) => e.toast === toast)) {
    return {
      ...state,
      entries: state.entries.filter((e) => e.key !== key).map((e) => (e.toast === toast ? { key, toast } : e)),
    }
  }

  const entry = { key, toast }
  const others = state.entries.filter((e) => e.key !== key)

  // The toast showing, taken off a moment ago, is handed to this Toast: it shows on, as it
  // was numbered.
  if (toast === state.closed) {
    return { entries: [entry, ...others], done: Math.max(state.done - 1, 0), closed: null }
  }

  if (toast.background !== true) {
    return { entries: [entry, ...others], done: state.done, closed: null }
  }

  // A background toast replaces its Toast's own in place, or waits at the end.
  const entries = at >= 0 ? state.entries.map((e, i) => (i === at ? entry : e)) : [...others, entry]

  return { entries, done: state.done, closed: null }
}

// takenOff is the queue without its entry at index at.
function takenOff<T extends Queued>(state: ToastQueueState<T>, at: number): ToastQueueState<T> {
  const entries = state.entries.filter((_, i) => i !== at)
  const showing = state.entries[0]

  if (at !== 0 || showing === undefined) {
    return { ...state, entries }
  }

  return { entries, done: entries.length === 0 ? 0 : state.done + 1, closed: showing.toast }
}

// Turn is where a Toast's message stands: whether it shows now, and the number of the one
// showing among those shown and waiting since the queue was last empty.
export interface Turn {
  showing: boolean
  position: number
  total: number
}

// turnOf is where the Toast key's message stands in the queue.
export function turnOf<T extends Queued>(state: ToastQueueState<T>, key: string): Turn {
  return {
    showing: state.entries[0]?.key === key,
    position: state.done + 1,
    total: state.done + state.entries.length,
  }
}
