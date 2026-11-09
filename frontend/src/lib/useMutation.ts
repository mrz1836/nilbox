import { useRef, useState } from 'react'

import { attempt, start, type Result } from './call'

// Mutation is a write to the backend as the window shows it.
export interface Mutation<A extends unknown[], T> {
  // run makes the write and resolves with how it ended; it never rejects.
  run: (...args: A) => Promise<Result<T>>
  // pending reports a write under way.
  pending: boolean
  // error is the message for the latest write's failure, worded for the user, or ''.
  error: string
  // reset clears the error.
  reset: () => void
}

// useMutation makes writes with mutate. A write is never cancelled, since it may already
// have been stored; when writes overlap, the state follows the latest one. A write that
// finishes while its page is hidden (an Activity, kept with its state while another view
// shows) is still recorded, so the page is not left pending when it shows again; one that
// finishes after its page has gone sets nothing, which React ignores, and run still
// resolves, so a caller can report it.
export function useMutation<A extends unknown[], T>(mutate: (...args: A) => PromiseLike<T>): Mutation<A, T> {
  const [state, setState] = useState({ pending: false, error: '' })
  // latest numbers the writes, so only the latest one's outcome is shown.
  const latest = useRef(0)

  const run = async (...args: A): Promise<Result<T>> => {
    latest.current += 1
    const n = latest.current

    setState({ pending: true, error: '' })

    const result = await attempt(start(() => mutate(...args)))

    if (n === latest.current) {
      setState({ pending: false, error: result.ok ? '' : result.error })
    }

    return result
  }

  return {
    run,
    pending: state.pending,
    error: state.error,
    reset: () => {
      setState((was) => ({ ...was, error: '' }))
    },
  }
}
