import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { agentSession, details, item, sessionsSnapshot } from '../test/fixtures'
import {
  cardSession,
  compactCount,
  githubEventTone,
  githubEventWords,
  isLive,
  itemSessions,
  needsYou,
  noSessions,
  sessionsOf,
  sinceWhen,
  stateDot,
  stateLabel,
  stateText,
  stateTint,
  superseded,
  usageWords,
} from './sessions'

describe('sessions', () => {
  it('words what came of a session’s item on GitHub, never who did it', () => {
    const at = '2026-06-15T11:58:00Z'
    const commit = 'abc1234def5678abc1234def5678abc1234def56'
    const words = (githubEvent: '' | 'approved' | 'changes_requested' | 'merged' | 'closed', more = {}) =>
      githubEventWords(agentSession({ githubEvent, githubEventCommit: commit, githubEventAt: at, ...more }))

    expect(words('approved')).toBe('Approved at abc1234')
    expect(words('changes_requested')).toBe('Changes requested at abc1234')
    expect(words('approved', { githubEventCommit: '' })).toBe('Approved')
    expect(words('merged')).toBe('Merged')
    expect(words('closed')).toBe('Closed')
    expect(words('')).toBe('')
    // No time is nothing learned.
    expect(words('approved', { githubEventAt: '' })).toBe('')
    expect(githubEventWords(agentSession({ githubEvent: 'example-event' as 'merged', githubEventAt: at }))).toBe('')

    expect(githubEventTone(agentSession({ githubEvent: 'approved' }))).toBe('text-success')
    expect(githubEventTone(agentSession({ githubEvent: 'merged' }))).toBe('text-success')
    expect(githubEventTone(agentSession({ githubEvent: 'changes_requested' }))).toBe('text-warning')
    expect(githubEventTone(agentSession({ githubEvent: 'closed' }))).toBe('text-muted')
  })

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

  it('drops an ended session from the card once its pull request moves past it', () => {
    const reviewed = 'abc1234def5678abc1234def5678abc1234def56'
    const pushed = 'fed4321cba8765fed4321cba8765fed4321cba87'
    const ended = agentSession({
      id: 'ended',
      state: 'ended',
      endedAt: '2026-06-15T09:00:00Z',
      githubEvent: 'changes_requested',
      githubEventCommit: reviewed,
      githubEventAt: '2026-06-15T08:50:00Z',
    })
    const quiet = agentSession({ id: 'quiet', state: 'ended', endedAt: '2026-06-15T09:00:00Z' })
    const running = agentSession({ id: 'running', state: 'working', githubEventCommit: reviewed })
    const at = (headOid: string, reviewRequestedAt = '') => item({ reviewRequestedAt, details: details({ headOid }) })

    // Its review is on the head, asked for before it ran: what it found is the latest.
    expect(cardSession([ended], FIXED_NOW, at(reviewed, '2026-06-15T08:00:00Z'))?.id).toBe('ended')
    expect(cardSession([ended], FIXED_NOW, at(reviewed.toUpperCase()))?.id).toBe('ended')
    // The author pushed since its review, asked again or not.
    expect(cardSession([ended], FIXED_NOW, at(pushed))).toBeNull()
    expect(cardSession([ended], FIXED_NOW, at(pushed, '2026-06-15T11:00:00Z'))).toBeNull()
    expect(superseded(ended, at(pushed))).toBe(true)
    // A session that left no review is past once a request arrives after it ended.
    expect(cardSession([quiet], FIXED_NOW, at(pushed, '2026-06-15T11:00:00Z'))).toBeNull()
    expect(cardSession([quiet], FIXED_NOW, at(pushed, '2026-06-15T08:00:00Z'))?.id).toBe('quiet')
    expect(cardSession([quiet], FIXED_NOW, at(pushed))?.id).toBe('quiet')
    // Nothing to compare before the details are read, or without the item.
    expect(cardSession([ended], FIXED_NOW, item())?.id).toBe('ended')
    expect(cardSession([ended], FIXED_NOW)?.id).toBe('ended')
    // A running session stays, whatever moved.
    expect(cardSession([running, ended], FIXED_NOW, at(pushed, '2026-06-15T11:00:00Z'))?.id).toBe('running')
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

  it('gives every state its dot, its text color, and its tag’s tint', () => {
    expect(stateDot).toEqual({
      starting: 'bg-muted',
      working: 'bg-info',
      needs_approval: 'bg-warning',
      needs_input: 'bg-warning',
      idle: 'bg-success',
      ended: 'bg-muted',
      failed: 'bg-danger',
      unknown: 'bg-muted',
    })
    expect(stateText).toEqual({
      starting: 'text-muted',
      working: 'text-info',
      needs_approval: 'text-warning',
      needs_input: 'text-warning',
      idle: 'text-success',
      ended: 'text-muted',
      failed: 'text-danger',
      unknown: 'text-muted',
    })
    expect(stateTint).toEqual({
      starting: '',
      working: 'bg-info/10 ring-1 ring-info/22',
      needs_approval: 'bg-warning/10 ring-1 ring-warning/22',
      needs_input: 'bg-warning/10 ring-1 ring-warning/22',
      idle: '',
      ended: '',
      failed: 'bg-danger/10',
      unknown: '',
    })
  })

  it('counts a session’s state from when it came to it, or when it ended', () => {
    expect(sinceWhen(agentSession())).toBe('2026-06-15T11:59:00Z')
    expect(sinceWhen(agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:30Z' }))).toBe('2026-06-15T11:59:30Z')
  })

  it('words a session’s tokens short, every count in full in its title, and nothing it can’t count', () => {
    const none = {
      known: false,
      fromRun: false,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
      total: 0,
      partial: false,
    }

    expect(usageWords(undefined)).toBeNull()
    expect(usageWords(none)).toBeNull()
    expect(usageWords({ ...none, known: true })).toBeNull()

    const one = usageWords({ ...none, known: true, output: 1, total: 1 })
    expect(one?.text).toBe('1 token')
    expect(one?.title).toBe(
      '1 token: 0 in, 1 out, 0 read from the cache, 0 written to it, as the agent’s own files on this Mac report them.',
    )
    expect(usageWords({ ...none, known: true, input: 12_400, total: 12_400 })?.text).toBe('12.4K tokens')

    const run = usageWords({
      ...none,
      known: true,
      fromRun: true,
      input: 1200,
      output: 30,
      cacheRead: 9000,
      total: 10_230,
    })
    expect(run?.text).toBe('10.2K tokens · 9K cached')
    expect(run?.title).toBe(
      '10,230 tokens: 1,200 in, cache writes included, 30 out, 9,000 read from the cache, as its run reported them.',
    )
    expect(compactCount(840)).toBe('840')
    expect(compactCount(2_500_000)).toBe('2.5M')
  })
})
