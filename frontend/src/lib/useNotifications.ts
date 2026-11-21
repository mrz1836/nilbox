import { startTransition, useOptimistic, useState } from 'react'

import {
  clearNotifications,
  listNotifications,
  markAllNotificationsRead,
  markNotificationsRead,
  markNotificationsUnread,
  type NotificationLog,
} from '../platform'
import { attempt, start } from './call'
import { unreadOf } from './notifications'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// NotificationsState is the notification history as the window shows it.
export interface NotificationsState {
  log: NotificationLog
  // error is why the history could not be loaded, or ''.
  error: string
  // markRead marks the entries with ids read, as opening one, clicking its banner, or its
  // Done does; markUnread marks them unread again, which puts them back on the bell; and
  // markAllRead marks every entry read (Mark all done). Opening the history marks nothing.
  // markItemRead marks the unread entries about an item read, as opening its sheet does,
  // and does nothing when there are none. Each shows at once; when it cannot be stored,
  // the entries go back as they were and markError says why.
  markRead: (ids: readonly number[]) => void
  markUnread: (ids: readonly number[]) => void
  markAllRead: () => void
  markItemRead: (itemId: number) => void
  markError: string
  // clear forgets the history, and rejects with the reason it could not.
  clear: () => Promise<void>
}

// Mark is a change to which entries are read.
type Mark = { type: 'read'; ids: readonly number[] } | { type: 'unread'; ids: readonly number[] } | { type: 'all' }

const emptyLog: NotificationLog = { items: [], unread: 0 }

// marked is a log with mark made.
function marked(log: NotificationLog, mark: Mark): NotificationLog {
  const ids = mark.type === 'all' ? null : new Set(mark.ids)
  const read = mark.type !== 'unread'
  const items = log.items.map((n) => (n.read === read || (ids !== null && !ids.has(n.id)) ? n : { ...n, read }))

  return { items, unread: items.filter((n) => !n.read).length }
}

// stores are the backend's calls that store each kind of mark.
function store(mark: Mark): PromiseLike<void> {
  switch (mark.type) {
    case 'read':
      return markNotificationsRead([...mark.ids])
    case 'unread':
      return markNotificationsUnread([...mark.ids])
    case 'all':
      return markAllNotificationsRead()
  }
}

// useNotifications loads the notification history and loads it again whenever the shell
// reports a change: a refresh that found something new, or the history read or cleared.
// A load that a newer one replaces is cancelled, so only the latest load's answer shows.
export function useNotifications(): NotificationsState {
  const history = useQuery(() => listNotifications(), [])

  useOn('notifications:updated', () => {
    void history.refetch()
  })

  const [log, show] = useOptimistic<NotificationLog, Mark>(history.data ?? emptyLog, marked)
  const [markError, setMarkError] = useState('')

  // mark shows mark at once and stores it. Stored, the log keeps it until the shell's
  // update confirms it; refused, the optimistic change ends with the transition, so the
  // entries go back as they were.
  const mark = (change: Mark) => {
    startTransition(async () => {
      show(change)
      setMarkError('')

      const result = await attempt(start(() => store(change)))

      startTransition(() => {
        if (result.ok) {
          history.set((was) => marked(was ?? emptyLog, change))
        } else {
          setMarkError(result.error)
        }
      })
    })
  }

  const markIds = (type: 'read' | 'unread', ids: readonly number[]) => {
    if (ids.length > 0) {
      mark({ type, ids })
    }
  }

  const clear = () =>
    clearNotifications().then(() => {
      history.set(() => emptyLog)
    })

  return {
    log,
    error: history.error,
    markRead: (ids) => {
      markIds('read', ids)
    },
    markUnread: (ids) => {
      markIds('unread', ids)
    },
    markAllRead: () => {
      if (log.unread > 0) {
        mark({ type: 'all' })
      }
    },
    markItemRead: (itemId) => {
      markIds('read', unreadOf(log.items, itemId))
    },
    markError,
    clear,
  }
}
