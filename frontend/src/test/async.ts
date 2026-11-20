import { act } from '@testing-library/react'

// Deferred is a promise the test settles itself, to see what shows while it is pending:
// resolve answers it with a value, and reject fails it with a reason.
export interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason: unknown) => void
}

// deferred returns a promise and the means to settle it, as Promise.withResolvers does
// (which the ES2023 library the window targets does not have). A promise that is never
// settled stands for a call that never answers.
export function deferred<T = void>(): Deferred<T> {
  let resolve: (value: T) => void = () => undefined
  let reject: (reason: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

// nextTask lets the window run until a timer's turn, inside act: every answer already
// given lands, with the renders and effects they cause. It waits on setTimeout, so under
// fake timers that fake setTimeout it settles only once they move on.
export async function nextTask(): Promise<void> {
  await act(async () => {
    await new Promise<void>((resolve) => {
      setTimeout(resolve, 0)
    })
  })
}
