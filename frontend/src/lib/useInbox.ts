import { useCallback, useEffect, useState } from 'react'

import { getInboxSnapshot, onInboxUpdated, refreshInbox, type InboxSnapshot } from '../platform'
import { errorMessage } from './format'

// tickMs is how often now moves on, so relative times and the budget levels stay current.
const tickMs = 60_000

function systemClock(): Date {
  return new Date()
}

// InboxState is the stored inbox and what the window is doing with it.
export interface InboxState {
  // snapshot is null until the first load finishes.
  snapshot: InboxSnapshot | null
  // now is the time relative ages and budget levels are measured against.
  now: Date
  loadError: string
  refreshError: string
  refreshing: boolean
  // refresh fetches the inbox from GitHub now.
  refresh: () => void
  // reload reads the stored inbox again, without asking GitHub, such as after a setting
  // that changes how items are shown.
  reload: () => void
}

// useInbox loads the stored inbox, loads it again whenever the shell reports a change
// (the background refresh), and refreshes from GitHub when asked. The whole window shares
// one InboxState: the inbox lists, the account menu, and the budget gauge. clock returns
// the current time; keep it stable across renders.
export function useInbox(clock: () => Date = systemClock): InboxState {
  const [snapshot, setSnapshot] = useState<InboxSnapshot | null>(null)
  const [loadError, setLoadError] = useState('')
  const [refreshError, setRefreshError] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const [now, setNow] = useState(clock)

  useEffect(() => {
    let active = true

    const load = () => {
      getInboxSnapshot()
        .then((next) => {
          if (active) {
            setSnapshot(next)
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
  }, [clock])

  const refresh = useCallback(() => {
    setRefreshing(true)

    refreshInbox()
      .then((next) => {
        setSnapshot(next)
        setRefreshError('')
      })
      .catch((err: unknown) => {
        setRefreshError(errorMessage(err))
      })
      .finally(() => {
        setRefreshing(false)
        setNow(clock())
      })
  }, [clock])

  const reload = useCallback(() => {
    getInboxSnapshot()
      .then((next) => {
        setSnapshot(next)
        setLoadError('')
      })
      .catch((err: unknown) => {
        setLoadError(errorMessage(err))
      })
  }, [])

  return { snapshot, now, loadError, refreshError, refreshing, refresh, reload }
}
