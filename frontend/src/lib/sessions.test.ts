import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { agentSession, item, sessionsSnapshot } from '../test/fixtures'
import {
  cardSession,
  isLive,
  itemSessions,
  needsYou,
  noSessions,
  sessionsOf,
  sinceWhen,
  stateDot,
  stateLabel,
} from './sessions'

describe('sessions', () => {
  it('keys each item’s sessions, newest first, ignoring case', () => {
    const newer = agentSession({ id: 'b', repo: 'Acme-Org/Example-Repo' })
    const older = agentSession({ id: 'a' })
    const other = agentSession({ id: 'c', number: 8 })

    const byItem = itemSessions(sessionsSnapshot([newer, older, other]))

    expect(sessionsOf(byItem, item())?.map((s) => s.id)).toEqual(['b', 'a'])
    expect(sessionsOf(byItem, item({ number: 8 }))?.map((s) => s.id)).toEqual(['c'])
    expect(sessionsOf(byItem, item({ number: 9 }))).toBeUndefined()
  })

  it('holds nothing while the sessions are not followed, or not loaded', () => {
    expect(itemSessions(sessionsSnapshot([agentSession()], false))).toBe(noSessions)
    expect(itemSessions(null)).toBe(noSessions)
    expect(itemSessions(sessionsSnapshot())).toBe(noSessions)
  })

  it('shows the newest running session on the card, else one that ended in the last day', () => {
    const running = agentSession({ id: 'running', state: 'working' })
    const endedRecently = agentSession({
      id: 'ended',
      state: 'ended',
      endedAt: '2026-06-15T09:00:00Z',
      canFocus: false,
      canResume: true,
    })
    const endedLongAgo = agentSession({ id: 'old', state: 'ended', endedAt: '2026-06-13T09:00:00Z' })

    expect(cardSession([endedRecently, running], FIXED_NOW)?.id).toBe('running')
    expect(cardSession([endedRecently, endedLongAgo], FIXED_NOW)?.id).toBe('ended')
    expect(cardSession([endedLongAgo], FIXED_NOW)).toBeNull()
    expect(cardSession(undefined, FIXED_NOW)).toBeNull()
    expect(cardSession([], FIXED_NOW)).toBeNull()
  })

  it('words and colors every state', () => {
    expect(stateLabel('needs_approval')).toBe('needs approval')
    expect(stateLabel('needs_input')).toBe('needs input')
    expect(stateLabel('unknown')).toBe('status unavailable')
    expect(stateDot.working).toBe('bg-info')
    expect(stateDot.needs_input).toBe('bg-warning')
    expect(isLive('idle')).toBe(true)
    expect(isLive('ended')).toBe(false)
    expect(needsYou('needs_approval')).toBe(true)
    expect(needsYou('working')).toBe(false)
  })

  it('counts a session’s state from when it came to it, or when it ended', () => {
    expect(sinceWhen(agentSession())).toBe('2026-06-15T11:59:00Z')
    expect(sinceWhen(agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:30Z' }))).toBe('2026-06-15T11:59:30Z')
  })
})
