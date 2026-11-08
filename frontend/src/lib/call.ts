// Calls to the backend as the hooks handle them: a failure as a value the window can show,
// cancellation for a call no one waits on any more, and writes kept in the order they
// were made.

import { errorMessage } from './format'

// Result is how a call ended: its value, or the message to show for its failure.
export type Result<T> = { ok: true; data: T } | { ok: false; error: string }

// attempt waits for a call and reports how it ended, so a caller branches on the result
// instead of catching. The message is errorMessage's, worded for the user.
export async function attempt<T>(call: PromiseLike<T>): Promise<Result<T>> {
  try {
    return { ok: true, data: await call }
  } catch (err: unknown) {
    return { ok: false, error: errorMessage(err) }
  }
}

// start makes a call with make, turning a synchronous throw into a rejected call, so the
// caller meets every failure the same way.
export function start<T>(make: () => PromiseLike<T>): PromiseLike<T> {
  try {
    return make()
  } catch (err: unknown) {
    return Promise.resolve().then(() => {
      throw err
    })
  }
}

// cancel cancels a call that can be cancelled, as the bound calls can, which stops the
// work behind it; other promises run on, and the caller ignores their answer. A cancelled
// call rejects, so it needs a rejection handler, as every promise the hooks wait on has.
export function cancel(call: PromiseLike<unknown>): void {
  if ('cancel' in call && typeof call.cancel === 'function') {
    void (call.cancel as () => unknown)()
  }
}

// all waits for every call, like Promise.all, and cancels every one that can be cancelled
// when it is cancelled itself.
export function all<const T extends readonly PromiseLike<unknown>[]>(
  calls: T,
): Promise<{ -readonly [K in keyof T]: Awaited<T[K]> }> & { cancel: () => void } {
  return Object.assign(Promise.all(calls), {
    cancel: () => {
      for (const call of calls) {
        cancel(call)
      }
    },
  })
}

// Queue runs tasks one at a time.
export type Queue = <T>(task: () => PromiseLike<T>) => Promise<T>

// serial returns a Queue: each task starts once the one before it has settled, however it
// ended, so writes reach the backend, and their answers come back, in the order they were
// made.
export function serial(): Queue {
  let tail: Promise<unknown> = Promise.resolve()

  return <T>(task: () => PromiseLike<T>): Promise<T> => {
    const next = tail.then(task)

    tail = next.then(
      () => undefined,
      () => undefined,
    )

    return next
  }
}
