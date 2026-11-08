import { useEffect, useEffectEvent, useRef, useState, type DependencyList } from 'react'

import { cancel, start } from './call'
import { errorMessage } from './format'

// Query is a read from the backend as the window shows it.
export interface Query<T> {
  // data is the latest answer, kept while a newer request is under way, and after a
  // failure; it is undefined until the first answer.
  data: T | undefined
  // error is the message for the latest request's failure, worded for the user, or ''
  // once a request succeeds.
  error: string
  // pending reports a request under way, or waiting out its delay.
  pending: boolean
  // waiting reports a request still waiting out its delay, not yet asked.
  waiting: boolean
  // refetch asks again now, cancelling a request under way. It resolves once a request
  // made since has been answered, however that went.
  refetch: () => Promise<void>
  // set replaces the data, as a write the window made leaves it, without asking.
  set: (update: (data: T | undefined) => T | undefined) => void
}

export interface QueryOptions {
  // enabled is false while there is nothing to ask; the data stays as it was.
  enabled?: boolean
  // delay waits this many milliseconds after the inputs last changed before asking.
  delay?: number
  // log writes a failure to the console under this message, for a read whose failure
  // the window does not show.
  log?: string
}

// Request is the request the query is on: the inputs it was made for and its number.
interface Request {
  deps: DependencyList
  id: number
}

// Answer is the outcome of the latest request to settle.
interface Answer<T> {
  id: number
  data: T | undefined
  error: string
}

// Waiter is a refetch waiting for an answer to a request numbered after since.
interface Waiter {
  since: number
  done: () => void
}

// sameDeps compares two input lists the way React compares a hook's dependencies.
function sameDeps(a: DependencyList, b: DependencyList): boolean {
  return a.length === b.length && a.every((v, i) => Object.is(v, b[i]))
}

// release lets go of every waiter that an answer to request id satisfies.
function release(waiting: Set<Waiter>, id: number): void {
  for (const waiter of waiting) {
    if (waiter.since < id) {
      waiting.delete(waiter)
      waiter.done()
    }
  }
}

// useQuery reads from the backend with load, and again whenever deps change: the inputs
// the read depends on, compared like an effect's dependencies, so each must be a value
// that stays the same while it means the same, never an object built while rendering.
// load itself is not compared: the read always uses the latest one.
//
// A request that a newer one replaces, or that is under way when the component goes, is
// cancelled, which stops the bound call behind it, and its answer is never shown: the
// window only ever shows the answer to the request it last made.
export function useQuery<T>(load: () => PromiseLike<T>, deps: DependencyList, options: QueryOptions = {}): Query<T> {
  const { enabled = true, delay = 0, log = '' } = options
  const key = [enabled, delay, ...deps]
  const [request, setRequest] = useState<Request>({ deps: key, id: 0 })
  const [answer, setAnswer] = useState<Answer<T>>({ id: -1, data: undefined, error: '' })
  // asked is the latest request asked once its delay was out.
  const [asked, setAsked] = useState(-1)
  const waiting = useRef(new Set<Waiter>())
  const read = useEffectEvent(load)

  // Changed inputs make a new request. Setting state while rendering, as React suggests
  // for state that follows a changed prop, starts it without an extra render.
  if (!sameDeps(request.deps, key)) {
    setRequest({ deps: key, id: request.id + 1 })
  }

  const { id } = request

  useEffect(() => {
    const waiters = waiting.current

    if (!enabled) {
      release(waiters, id + 1)

      return undefined
    }

    let live = true
    let call: PromiseLike<T> | undefined

    const ask = () => {
      call = start(read)
      call.then(
        (data) => {
          if (live) {
            setAnswer({ id, data, error: '' })
            release(waiters, id)
          }
        },
        (err: unknown) => {
          if (!live) {
            return
          }

          if (log !== '') {
            console.error(log, err)
          }

          setAnswer((was) => ({ id, data: was.data, error: errorMessage(err) }))
          release(waiters, id)
        },
      )
    }

    const timer =
      delay > 0
        ? setTimeout(() => {
            setAsked(id)
            ask()
          }, delay)
        : undefined

    if (timer === undefined) {
      ask()
    }

    return () => {
      live = false
      clearTimeout(timer)

      if (call !== undefined) {
        cancel(call)
      }
    }
  }, [id, enabled, delay, log])

  // Nothing is left waiting once the component has gone.
  useEffect(() => {
    const waiters = waiting.current

    return () => {
      release(waiters, Infinity)
    }
  }, [])

  return {
    data: answer.data,
    error: answer.error,
    pending: enabled && answer.id !== id,
    waiting: enabled && delay > 0 && answer.id !== id && asked !== id,
    refetch: () =>
      new Promise<void>((done) => {
        waiting.current.add({ since: id, done })
        setRequest((was) => ({ deps: was.deps, id: was.id + 1 }))
      }),
    set: (update) => {
      setAnswer((was) => ({ ...was, data: update(was.data) }))
    },
  }
}

// useDebouncedQuery is useQuery for inputs that change as the user types: it asks once
// they have rested for delay milliseconds, and a change cancels the wait and any request
// under way.
export function useDebouncedQuery<T>(
  load: () => PromiseLike<T>,
  deps: DependencyList,
  delay: number,
  options: Omit<QueryOptions, 'delay'> = {},
): Query<T> {
  return useQuery(load, deps, { ...options, delay })
}
