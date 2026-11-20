import { describe, expect, it } from 'vitest'

import type { Achievement, AchievementKind } from '../platform'
import { nilMoment } from '../test/fixtures'
import { achievementTitle, momentQuip, momentQuips, nilChip } from './milestones'

// at is a time where the user is, in June 2026: the 15th is a Monday, the 19th a Friday,
// and the 20th a Saturday.
function at(day: number, hour: number): Date {
  return new Date(2026, 5, day, hour, 0)
}

describe('achievementTitle', () => {
  it.each([
    [{ kind: 'streak', n: 7 }, '7-day streak'],
    [{ kind: 'streak', n: 60 }, '60-day streak'],
    [{ kind: 'count', n: 100 }, '100 nils'],
    [{ kind: 'first', n: 1 }, 'First nil'],
    [{ kind: 'sweep', n: 10 }, 'Clean sweep'],
    [{ kind: 'early', n: 1 }, 'Early bird'],
    [{ kind: 'friday', n: 1 }, 'Friday finish'],
    [{ kind: 'weekend', n: 1 }, 'Weekend warrior'],
    [{ kind: 'owl', n: 1 }, 'Night owl'],
  ] satisfies [Achievement, string][])('calls %j %j', (a, title) => {
    expect(achievementTitle(a)).toBe(title)
  })

  it('names every kind', () => {
    const kinds: AchievementKind[] = ['streak', 'count', 'first', 'sweep', 'early', 'friday', 'weekend', 'owl']

    for (const kind of kinds) {
      expect(achievementTitle({ kind, n: 3 })).not.toBe('')
    }
  })
})

describe('nilChip', () => {
  it('shows nothing without a moment, or for the first day found at nil', () => {
    expect(nilChip(null)).toBeNull()
    expect(nilChip(nilMoment())).toBeNull()
  })

  it('shows the most significant achievement unlocked, and how many came with it', () => {
    const moment = nilMoment({
      earned: true,
      count: 1,
      streak: 7,
      unlocked: [
        { kind: 'streak', n: 7 },
        { kind: 'first', n: 1 },
        { kind: 'owl', n: 1 },
      ],
    })

    expect(nilChip(moment)).toEqual({
      kind: 'unlocked',
      title: '7-day streak',
      more: 2,
      titles: ['7-day streak', 'First nil', 'Night owl'],
    })
    expect(nilChip(nilMoment({ earned: true, unlocked: [{ kind: 'first', n: 1 }] }))).toEqual({
      kind: 'unlocked',
      title: 'First nil',
      more: 0,
      titles: ['First nil'],
    })
  })

  it('notes an ordinary earned nil by its number, or its streak from the second day', () => {
    expect(nilChip(nilMoment({ earned: true, count: 37 }))).toEqual({ kind: 'note', text: 'Nil #37' })
    expect(nilChip(nilMoment({ earned: true, count: 37, streak: 4 }))).toEqual({ kind: 'note', text: 'Day 4 at nil' })
  })

  it('notes a nil found empty by its streak, from the second day', () => {
    expect(nilChip(nilMoment({ streak: 2 }))).toEqual({ kind: 'note', text: 'Day 2 at nil' })
  })
})

describe('momentQuips', () => {
  it('fits nothing to an ordinary weekday afternoon', () => {
    expect(momentQuips(nilMoment({ earned: true }), at(15, 14))).toEqual([])
  })

  it('fits the moment, the most significant first', () => {
    const swept = nilMoment({ earned: true, cleared: 14, streak: 5, best: 5 })

    expect(momentQuips(swept, at(19, 8))).toEqual([
      '14 down, 0 to go.',
      '5 days running. A new personal best.',
      'Nil on a Friday. The weekend starts now.',
      'Nil before standup. Your update writes itself.',
    ])
  })

  it('tells a streak from a new best', () => {
    expect(momentQuips(nilMoment({ streak: 3, best: 9 }), at(15, 14))).toEqual(['Day 3 at nil. Don’t break the chain.'])
    expect(momentQuips(nilMoment({ streak: 2, best: 2 }), at(15, 14))).toEqual(['Day 2 at nil. Don’t break the chain.'])
  })

  it('knows a weekend and the dark, and keeps standup to weekdays', () => {
    expect(momentQuips(nilMoment(), at(20, 8))).toEqual(['Nil on a weekend. Nobody asked, and you did it anyway.'])
    expect(momentQuips(nilMoment(), at(16, 23))).toEqual(['Nil after dark. The bugs can sleep tonight.'])
    expect(momentQuips(nilMoment(), at(16, 3))).toEqual(['Nil after dark. The bugs can sleep tonight.'])
  })
})

describe('momentQuip', () => {
  it('speaks only to a nil the user earned', () => {
    expect(momentQuip(nilMoment({ cleared: 30 }), at(19, 9))).toBeNull()
    expect(momentQuip(nilMoment({ earned: true }), at(15, 14))).toBeNull()
  })

  it('speaks to the most significant when the nil unlocked something', () => {
    const moment = nilMoment({ earned: true, cleared: 12, unlocked: [{ kind: 'sweep', n: 10 }] })

    expect(momentQuip(moment, at(19, 9), () => 0.9999)).toBe('12 down, 0 to go.')
  })

  it('picks one of those that fit otherwise', () => {
    const moment = nilMoment({ earned: true, streak: 2, best: 6 })

    expect(momentQuip(moment, at(19, 9), () => 0)).toBe('Day 2 at nil. Don’t break the chain.')
    expect(momentQuip(moment, at(19, 9), () => 0.9999)).toBe('Nil before standup. Your update writes itself.')
  })
})
