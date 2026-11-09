import { useEffect, useEffectEvent } from 'react'

import { listSessions, on, type AgentSession, type SessionsSnapshot } from '../platform'
import { itemSessions } from './sessions'
import { useQuery } from './useQuery'

// SessionsState is the agent sessions as the inbox's cards follow them.
export interface SessionsState {
  // snapshot is the sessions service's latest list, or null until it has loaded.
  snapshot: SessionsSnapshot | null
  // byItem holds each item's sessions, newest first, by itemKey; empty while the app
  // does not follow sessions.
  byItem: ReadonlyMap<string, readonly AgentSession[]>
  // error is why the sessions could not be loaded, or ''.
  error: string
}

// useSessions loads the agent sessions as the inbox mounts, and again whenever the shell
// says one was launched or changed. Unlike the worktrees, they need no other view opened
// first: listing them reads the local ledger alone.
export function useSessions(): SessionsState {
  const sessions = useQuery(() => listSessions(), [], { log: 'could not load the agent sessions' })
  const reloadOnUpdate = useEffectEvent(() => {
    void sessions.refetch()
  })

  useEffect(
    () =>
      on('sessions:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const snapshot = sessions.data ?? null

  return { snapshot, byItem: itemSessions(snapshot), error: sessions.error }
}
