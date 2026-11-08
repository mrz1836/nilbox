import { startTransition, useEffect, useEffectEvent, useOptimistic } from 'react'

import { clearNotifications, listNotifications, markNotificationsRead, on, type NotificationLog } from '../platform'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'

// NotificationsState is the notification history as the window shows it.
export interface NotificationsState {
  log: NotificationLog
  // error is why the history could not be loaded, or ''.
  error: string
  // markRead marks every entry read, as opening the history does. It shows at once; when
  // it cannot be stored, the entries go back to unread and markError says why.
  markRead: () => void
  markError: string
  // clear forgets the history, and rejects with the reason it could not.
  clear: () => Promise<void>
}

const emptyLog: NotificationLog = { items: [], unread: 0 }

// readAll is a log with every entry read.
function readAll(log: NotificationLog): NotificationLog {
  return { items: log.items.map((n) => ({ ...n, read: true })), unread: 0 }
}

// useNotifications loads the notification history and loads it again whenever the shell
// reports a change: a refresh that found something new, or the history read or cleared.
// A load that a newer one replaces is cancelled, so only the latest load's answer shows.
export function useNotifications(): NotificationsState {
  const history = useQuery(() => listNotifications(), [])
  const reloadOnUpdate = useEffectEvent(() => {
    void history.refetch()
  })

  useEffect(
    () =>
      on('notifications:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const [log, showRead] = useOptimistic<NotificationLog, undefined>(history.data ?? emptyLog, readAll)
  const marking = useMutation(() => markNotificationsRead())

  const markRead = () => {
    startTransition(async () => {
      showRead(undefined)

      // Stored, the log stays read until the shell's update confirms it.
      if ((await marking.run()).ok) {
        startTransition(() => {
          history.set((was) => readAll(was ?? emptyLog))
        })
      }
    })
  }

  const clear = () =>
    clearNotifications().then(() => {
      history.set(() => emptyLog)
    })

  return { log, error: history.error, markRead, markError: marking.error, clear }
}
