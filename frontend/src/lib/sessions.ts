// The agent sessions the inbox's cards follow: each item's sessions, from the sessions
// service's latest list (useSessions), which session a card shows, and how each state
// reads. The backend names the states (internal/sessions); this file only words them.

import type { AgentSession, InboxItem, SessionState, SessionsSnapshot } from '../platform'
import { itemKey } from './itemWorktrees'

// noSessions is the empty map, shared so it stays the same object.
export const noSessions: ReadonlyMap<string, readonly AgentSession[]> = new Map()

// endedShownMs is how long after it ended a session still shows on its item's card.
export const endedShownMs = 24 * 60 * 60 * 1000

// itemSessions maps each item, by itemKey, to its sessions, newest first, as the list
// holds them. It is empty while the app does not follow sessions.
export function itemSessions(snapshot: SessionsSnapshot | null): ReadonlyMap<string, readonly AgentSession[]> {
  if (snapshot === null || !snapshot.following || snapshot.sessions.length === 0) {
    return noSessions
  }

  const out = new Map<string, AgentSession[]>()

  for (const s of snapshot.sessions) {
    const key = itemKey(s.repo, s.number)
    const list = out.get(key)

    if (list === undefined) {
      out.set(key, [s])
    } else {
      list.push(s)
    }
  }

  return out
}

// sessionsOf returns the item's sessions, newest first, if it has any.
export function sessionsOf(
  sessions: ReadonlyMap<string, readonly AgentSession[]>,
  item: InboxItem,
): readonly AgentSession[] | undefined {
  return sessions.get(itemKey(item.repo, item.number))
}

// isLive reports a session that has not ended.
export function isLive(state: SessionState): boolean {
  return state !== 'ended'
}

// needsYou reports a session waiting for the user.
export function needsYou(state: SessionState): boolean {
  return state === 'needs_approval' || state === 'needs_input'
}

// runningSession is the newest of an item's sessions that has not ended, or null.
export function runningSession(sessions: readonly AgentSession[] | undefined): AgentSession | null {
  return sessions?.find((s) => isLive(s.state)) ?? null
}

// cardSession picks the session an item's card shows: the newest still running, else the
// newest that ended in the last day, else none.
export function cardSession(sessions: readonly AgentSession[] | undefined, now: Date): AgentSession | null {
  if (sessions === undefined) {
    return null
  }

  const live = sessions.find((s) => isLive(s.state))
  if (live !== undefined) {
    return live
  }

  const ended = sessions.find((s) => s.endedAt !== '')
  if (ended === undefined || now.getTime() - Date.parse(ended.endedAt) > endedShownMs) {
    return null
  }

  return ended
}

// stateLabels are how each state reads after the agent's name: "Claude · needs approval".
const stateLabels: Readonly<Record<SessionState, string>> = {
  starting: 'starting',
  working: 'working',
  needs_approval: 'needs approval',
  needs_input: 'needs input',
  idle: 'idle',
  ended: 'ended',
  failed: 'failed',
  unknown: 'status unavailable',
}

// stateLabel words a state.
export function stateLabel(state: SessionState): string {
  return stateLabels[state]
}

// stateDot is each state's dot: blue while working, amber while it waits for the user,
// green once its turn is done, red when it failed, grey otherwise.
export const stateDot: Readonly<Record<SessionState, string>> = {
  starting: 'bg-muted',
  working: 'bg-info',
  needs_approval: 'bg-warning',
  needs_input: 'bg-warning',
  idle: 'bg-success',
  ended: 'bg-muted',
  failed: 'bg-danger',
  unknown: 'bg-muted',
}

// stateText is each state's text color on the card's tag.
export const stateText: Readonly<Record<SessionState, string>> = {
  starting: 'text-muted',
  working: 'text-info',
  needs_approval: 'text-warning',
  needs_input: 'text-warning',
  idle: 'text-success',
  ended: 'text-muted',
  failed: 'text-danger',
  unknown: 'text-muted',
}

// stateTint is the faint backing of the card's tag in a state the user should see at a
// glance: a session at work, waiting on them, or failed, so it stands apart from the
// card's other details. The rest, starting included, have none.
export const stateTint: Readonly<Record<SessionState, string>> = {
  starting: '',
  working: 'bg-info/12 ring-1 ring-info/25',
  needs_approval: 'bg-warning/12 ring-1 ring-warning/30',
  needs_input: 'bg-warning/12 ring-1 ring-warning/30',
  idle: '',
  ended: '',
  failed: 'bg-danger/10',
  unknown: '',
}

// pings reports a state whose dot has a ring swelling out of it: a session at work.
export function pings(state: SessionState): boolean {
  return state === 'working'
}

// pulses reports a state whose dot pulses: a session starting up.
export function pulses(state: SessionState): boolean {
  return state === 'starting'
}

// sinceWhen is the time a session's state is counted from: when it ended, or came to the
// state it is in.
export function sinceWhen(s: AgentSession): string {
  return s.endedAt === '' ? s.stateAt : s.endedAt
}
