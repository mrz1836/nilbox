import { useCallback, useEffect, useRef, useState } from 'react'

import { clearNotifications, listNotifications, markNotificationsRead, on, type NotificationLog } from '../platform'
import { errorMessage } from './format'

// NotificationsState is the notification history as the window shows it.
export interface NotificationsState {
  log: NotificationLog
  error: string
  // markRead marks every entry read, as opening the history does.
  markRead: () => void
  // clear forgets the history, and rejects with the reason it could not.
  clear: () => Promise<void>
}

const emptyLog: NotificationLog = { items: [], unread: 0 }

// useNotifications loads the notification history and loads it again whenever the shell
// reports a change: a refresh that found something new, or the history read or cleared.
// Loads can overlap and answer out of order, so only the latest load's answer is shown.
export function useNotifications(): NotificationsState {
  const [log, setLog] = useState<NotificationLog>(emptyLog)
  const [error, setError] = useState('')
  const latest = useRef(0)

  const load = useCallback(() => {
    latest.current += 1
    const seq = latest.current

    listNotifications()
      .then((next) => {
        if (seq === latest.current) {
          setLog(next)
          setError('')
        }
      })
      .catch((err: unknown) => {
        if (seq === latest.current) {
          setError(errorMessage(err))
        }
      })
  }, [])

  useEffect(() => {
    load()

    return on('notifications:updated', load)
  }, [load])

  const markRead = useCallback(() => {
    // Shown read at once; the shell's update confirms it.
    setLog((was) => ({ items: was.items.map((n) => ({ ...n, read: true })), unread: 0 }))

    markNotificationsRead().catch((err: unknown) => {
      console.error('could not mark the notifications read', err)
      load()
    })
  }, [load])

  const clear = useCallback(
    () =>
      clearNotifications().then(() => {
        setLog(emptyLog)
      }),
    [],
  )

  return { log, error, markRead, clear }
}
