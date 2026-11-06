import { useCallback, useEffect, useRef, useState } from 'react'

import {
  dismissItem,
  getInboxSnapshot,
  onInboxUpdated,
  refreshInbox,
  restoreItem,
  snoozeItem,
  type InboxSnapshot,
} from '../platform'
import { errorMessage } from './format'

// tickMs is how often now moves on, so relative times, the budget levels, and snoozes
// stay current.
const tickMs = 60_000

function systemClock(): Date {
  return new Date()
}

// InboxState is the stored inbox and what the window is doing with it.
export interface InboxState {
  // snapshot is null until the first load finishes.
  snapshot: InboxSnapshot | null
  // now is the time relative ages, budget levels, and snoozes are measured against.
  now: Date
  loadError: string
  refreshError: string
  refreshing: boolean
  // refresh fetches the inbox from GitHub now.
  refresh: () => void
  // reload reads the stored inbox again, without asking GitHub, such as after a setting
  // that changes how items are shown.
  reload: () => void
  // dismiss hides an item until it is restored, snooze hides it for a number of days,
  // and restore brings back either. Each resolves with the new inbox once it is shown,
  // and rejects with the reason it failed.
  dismiss: (id: number) => Promise<InboxSnapshot>
  snooze: (id: number, days: number) => Promise<InboxSnapshot>
  restore: (id: number) => Promise<InboxSnapshot>
}

// useInbox loads the stored inbox, loads it again whenever the shell reports a change
// (the background refresh), and refreshes from GitHub when asked. The whole window shares
// one InboxState: the inbox lists, the account menu, and the budget gauge. clock returns
// the current time; keep it stable across renders.
//
// Loads, refreshes, and triage can overlap, and their answers can arrive out of order.
// Each request is numbered as it starts, and an answer is shown only when no later
// request's answer already is, so an older inbox never replaces a newer one. Every change
// to the stored inbox is followed by an update from the shell, whose load is the latest.
export function useInbox(clock: () => Date = systemClock): InboxState {
  const [snapshot, setSnapshot] = useState<InboxSnapshot | null>(null)
  const [loadError, setLoadError] = useState('')
  const [refreshError, setRefreshError] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const [now, setNow] = useState(clock)
  // started numbers the requests; shown is the number of the one whose answer is shown.
  const started = useRef(0)
  const shown = useRef(0)

  // request starts a call for the inbox, shows its answer unless a later request's
  // answer is already shown, and resolves with the answer either way.
  const request = useCallback(async (call: () => Promise<InboxSnapshot>): Promise<InboxSnapshot> => {
    started.current += 1
    const number = started.current
    const next = await call()

    if (number > shown.current) {
      shown.current = number
      setSnapshot(next)
    }

    return next
  }, [])

  useEffect(() => {
    let active = true

    const load = () => {
      request(getInboxSnapshot)
        .then(() => {
          if (active) {
            setLoadError('')
            setRefreshError('')
            setNow(clock())
          }
        })
        .catch((err: unknown) => {
          if (active) {
            setLoadError(errorMessage(err))
          }
        })
    }

    load()

    const stop = onInboxUpdated(load)
    const tick = setInterval(() => {
      setNow(clock())
    }, tickMs)

    return () => {
      active = false
      stop()
      clearInterval(tick)
    }
  }, [clock, request])

  const refresh = useCallback(() => {
    setRefreshing(true)

    request(refreshInbox)
      .then(() => {
        setRefreshError('')
      })
      .catch((err: unknown) => {
        setRefreshError(errorMessage(err))
      })
      .finally(() => {
        setRefreshing(false)
        setNow(clock())
      })
  }, [clock, request])

  const reload = useCallback(() => {
    request(getInboxSnapshot)
      .then(() => {
        setLoadError('')
      })
      .catch((err: unknown) => {
        setLoadError(errorMessage(err))
      })
  }, [request])

  // triage runs a triage call and shows its inbox, measured against a fresh clock.
  const triage = useCallback(
    async (call: () => Promise<InboxSnapshot>) => {
      const next = await request(call)
      setNow(clock())

      return next
    },
    [clock, request],
  )

  const dismiss = useCallback((id: number) => triage(() => dismissItem(id)), [triage])
  const snooze = useCallback((id: number, days: number) => triage(() => snoozeItem(id, days)), [triage])
  const restore = useCallback((id: number) => triage(() => restoreItem(id)), [triage])

  return { snapshot, now, loadError, refreshError, refreshing, refresh, reload, dismiss, snooze, restore }
}
