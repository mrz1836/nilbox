import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import {
  activityGroup,
  autopilotEffect,
  autopilotEngine,
  autopilotGlance,
  autopilotMatch,
  dryRunMatch,
  itemRef,
  leftAlone,
  routine,
} from '../test/fixtures'
import {
  activityLines,
  autopilotBadge,
  chipOf,
  effectState,
  effectVerb,
  elsewhereText,
  itemText,
  lineOf,
  matchingWords,
  mergeDays,
  modeLabel,
  routineActivity,
  routineStatus,
  sameSecond,
  troubled,
} from './autopilot'

const noon = new Date('2026-06-15T12:00:00Z')

describe('autopilotBadge', () => {
  it('counts nothing while nothing works, waits, or is broken', () => {
    expect(autopilotBadge(autopilotGlance(), [routine()], autopilotEngine())).toEqual({
      count: 0,
      attention: false,
      failed: false,
      title: '',
    })
    expect(autopilotBadge(null, [], null).count).toBe(0)
  })

  it('counts the agents working, with a dot for what needs you and one for a routine that can’t run', () => {
    const working = autopilotGlance().working
    const glance = autopilotGlance({
      needsYou: 1,
      working: [
        ...working,
        {
          runId: 1,
          matchId: 2,
          routineId: 3,
          routine: 'Fix it',
          item: itemRef(),
          agent: 'claude',
          where: 'background',
          startedAt: '2026-06-15T11:50:00Z',
          createdAt: '2026-06-15T11:50:00Z',
        },
        {
          runId: 4,
          matchId: 5,
          routineId: 3,
          routine: 'Fix it',
          item: itemRef({ id: 2 }),
          agent: 'claude',
          where: 'background',
          startedAt: '2026-06-15T11:55:00Z',
          createdAt: '2026-06-15T11:55:00Z',
        },
      ],
    })
    const broken = routine({ id: 4, broken: `Made by a newer ${brand.name}: update to edit it`, brokenReason: 'newer' })

    expect(autopilotBadge(glance, [routine(), broken], autopilotEngine())).toEqual({
      count: 2,
      attention: true,
      failed: true,
      title: '2 agents working · 1 needs you · 1 routine can’t run',
    })
  })

  it('counts a routine whose search fails as one that can’t run, and none of another account’s', () => {
    const engine = autopilotEngine({
      problems: [
        { key: 'p', routineId: 3, kind: 'search_failed', detail: 'GitHub refused the search.', since: '', told: true },
      ],
    })

    expect(troubled([routine()], engine)).toBe(1)
    expect(troubled([routine({ otherAccount: true, broken: 'x' })], null)).toBe(0)
  })
})

describe('routineStatus', () => {
  const quiet = { today: 0, wouldRun: 0, waiting: 0, last: '' }

  it('says whose another account’s routine is, and why a broken one won’t run, before anything else', () => {
    expect(routineStatus(routine({ otherAccount: true, account: 'octo-other' }), quiet, 0, '').text).toBe(
      'Belongs to @octo-other',
    )
    expect(
      routineStatus(routine({ broken: `Made by a newer ${brand.name}: update to edit it` }), quiet, 0, ''),
    ).toEqual({
      text: `Made by a newer ${brand.name}: update to edit it`,
      tone: 'danger',
    })
    expect(routineStatus(routine(), quiet, 0, 'GitHub refused the search.').tone).toBe('danger')
    expect(routineStatus(routine({ lastError: 'It failed.' }), quiet, 0, '').text).toBe('It failed.')
  })

  it('says its mode, and what it is doing or did today', () => {
    expect(routineStatus(routine({ enabled: false }), quiet, 0, '').text).toBe('Autopilot · off')
    expect(routineStatus(routine(), quiet, 2, '').text).toBe('Autopilot · 2 working')
    expect(routineStatus(routine({ mode: 'ask' }), { ...quiet, waiting: 1 }, 0, '').text).toBe('Ask first · 1 waiting')
    expect(routineStatus(routine({ mode: 'dryRun' }), { ...quiet, wouldRun: 3 }, 0, '').text).toBe(
      'Dry run · 3 would run today',
    )
    expect(routineStatus(routine(), { ...quiet, today: 6 }, 0, '').text).toBe('Autopilot · 6 today')
    expect(routineStatus(routine(), { ...quiet, last: '2026-06-14T09:00:00Z' }, 0, '')).toEqual({
      text: 'Autopilot · ran',
      tone: 'muted',
      since: '2026-06-14T09:00:00Z',
    })
    expect(routineStatus(routine(), undefined, 0, '').text).toBe('Autopilot · nothing matched yet')
  })

  it('counts each routine’s matches today from the pages loaded', () => {
    const days = activityPage().days
    const sums = routineActivity(days, noon)

    expect(sums.get(3)).toEqual({ today: 1, wouldRun: 0, waiting: 0, last: '2026-06-15T11:30:00Z' })
    expect(sums.get(4)).toEqual({ today: 0, wouldRun: 1, waiting: 0, last: '2026-06-15T11:40:00Z' })
  })
})

// activityPage is a page's days: today, routine 3's snooze, grouped, and routine 4's Dry run
// record.
function activityPage() {
  return {
    days: [{ date: '2026-06-15', matches: [dryRunMatch(), autopilotMatch()], groups: [activityGroup()] }],
  }
}

describe('chipOf', () => {
  it('says what a local step did, and whether it was taken back', () => {
    expect(chipOf(autopilotMatch())).toEqual({ label: 'Snoozed', tone: 'accent' })
    expect(chipOf(autopilotMatch({ effects: [autopilotEffect({ action: 'pin' })] })).label).toBe('Pinned')
    expect(chipOf(autopilotMatch({ effects: [autopilotEffect({ undoneAt: '2026-06-15T11:45:00Z' })] })).label).toBe(
      'Undone',
    )
    expect(chipOf(autopilotMatch({ effects: [autopilotEffect({ reversedAt: '2026-06-15T11:45:00Z' })] })).label).toBe(
      'Taken back',
    )
  })

  it('says what a Dry run would have done, a notice, and where an open match is', () => {
    expect(chipOf(dryRunMatch())).toEqual({ label: 'Would run', tone: 'muted' })
    expect(
      chipOf(
        autopilotMatch({
          effects: [],
          steps: [
            {
              kind: 'notify',
              label: 'Notify me',
              status: 'done',
              outcome: 'done',
              note: '',
              runId: 0,
              startedAt: null,
              endedAt: null,
            },
          ],
        }),
      ).label,
    ).toBe('Told you')
    expect(chipOf(autopilotMatch({ state: 'queued', blockedBy: 'paused', reason: '', effects: [] })).label).toBe(
      'Queued',
    )
    expect(chipOf(autopilotMatch({ state: 'skipped', reason: 'tries', effects: [] }))).toEqual({
      label: 'Gave up',
      tone: 'danger',
    })
  })
})

describe('matchingWords', () => {
  it('asks the question about what matches now', () => {
    expect(matchingWords({ count: 3, pullRequests: 3, issues: 0, unknown: 0 })).toBe('3 pull requests match right now.')
    expect(matchingWords({ count: 1, pullRequests: 0, issues: 1, unknown: 0 })).toBe('1 issue matches right now.')
    expect(matchingWords({ count: 3, pullRequests: 2, issues: 1, unknown: 1 })).toBe(
      '2 pull requests and 1 issue match right now.',
    )
  })
})

describe('activityLines', () => {
  const days = activityPage().days

  it('lists a grouped line in place of the matches it stands for, and the rest by themselves, newest first', () => {
    const lines = activityLines(days, [leftAlone()], { routine: 0, result: 'all' })

    expect(lines.map((l) => l.kind)).toEqual(['leftAlone', 'match', 'group'])
    expect(lineOf(lines, 12)?.kind).toBe('group')
    expect(lineOf(lines, 14)?.kind).toBe('match')
    expect(lineOf(lines, 99)).toBeUndefined()
  })

  it('narrows the lines by routine and by result', () => {
    expect(activityLines(days, [leftAlone()], { routine: 4, result: 'all' }).map((l) => l.kind)).toEqual(['match'])
    expect(activityLines(days, [leftAlone()], { routine: 0, result: 'actions' }).map((l) => l.kind)).toEqual(['group'])
    expect(activityLines(days, [leftAlone()], { routine: 0, result: 'dryRuns' }).map((l) => l.kind)).toEqual(['match'])
    expect(activityLines(days, [leftAlone()], { routine: 0, result: 'attention' }).map((l) => l.kind)).toEqual([
      'leftAlone',
    ])
  })

  it('leaves out a baseline, a match that closed before it acted, and one still open', () => {
    const quiet = [
      autopilotMatch({ id: 20, state: 'baseline', reason: 'from_now', effects: [] }),
      autopilotMatch({ id: 21, state: 'closed', reason: 'no_longer_matches', effects: [] }),
      autopilotMatch({ id: 22, state: 'queued', blockedBy: 'paused', reason: '', effects: [] }),
    ]

    expect(
      activityLines([{ date: '2026-06-15', matches: quiet, groups: [] }], [], { routine: 0, result: 'all' }),
    ).toEqual([])
  })
})

describe('mergeDays', () => {
  it('joins a day and a grouped line two pages split', () => {
    const later = activityGroup({ undo: [61], matches: [16], items: [itemRef({ id: 6, number: 9 })], count: 1 })
    const merged = mergeDays(activityPage().days, [
      { date: '2026-06-15', matches: [autopilotMatch({ id: 16 })], groups: [later] },
      { date: '2026-06-14', matches: [], groups: [] },
    ])

    expect(merged.map((d) => d.date)).toEqual(['2026-06-15', '2026-06-14'])
    expect(merged[0]?.matches.map((m) => m.id)).toEqual([14, 12, 16])
    expect(merged[0]?.groups).toHaveLength(1)
    expect(merged[0]?.groups[0]).toMatchObject({ words: 'Snoozed 2', count: 2, undo: [51, 61], matches: [12, 16] })
  })
})

describe('the results', () => {
  const notice = autopilotMatch({
    id: 40,
    effects: [],
    steps: [
      {
        kind: 'tellMe',
        label: 'Tell me',
        status: 'done',
        outcome: '',
        note: '',
        runId: 0,
        startedAt: null,
        endedAt: null,
      },
    ],
  })
  const failed = autopilotMatch({ id: 41, effects: [], steps: [], reason: 'failed' })
  const ran = autopilotMatch({ id: 42, effects: [], steps: [] })
  const days = [{ date: '2026-06-15', matches: [notice, failed, ran], groups: [] }]

  it('files a notice, a step that failed, and a plain run under their results', () => {
    const ids = (result: 'notices' | 'attention' | 'actions') =>
      activityLines(days, [], { routine: 0, result }).map((l) => (l.kind === 'match' ? l.match.id : 0))

    expect(ids('notices')).toEqual([40])
    expect(ids('attention')).toEqual([41])
    expect(ids('actions')).toEqual([42])
    expect(chipOf(failed)).toEqual({ label: 'A step failed', tone: 'danger' })
    expect(chipOf(ran)).toEqual({ label: 'Ran', tone: 'accent' })
  })

  it('says where a step’s change stands', () => {
    expect(effectState(autopilotEffect())).toBe('Still there')
    expect(effectState(autopilotEffect({ canUndo: false }))).toBe('Changed since')
    expect(effectState(autopilotEffect({ reversedAt: '2026-06-15T11:45:00Z' }))).toBe('You took it back')
    expect(effectVerb('markDone')).toBe('MarkDone')
  })

  it('keeps a grouped line a later page brings that the first didn’t hold', () => {
    const other = activityGroup({ key: '2026-06-15|4|pin', routineId: 4, action: 'pin', words: 'Pinned 1' })
    const merged = mergeDays(
      [{ date: '2026-06-15', matches: [], groups: [activityGroup()] }],
      [{ date: '2026-06-15', matches: [], groups: [other] }],
    )

    expect(merged[0]?.groups.map((g) => g.words)).toEqual(['Snoozed 1', 'Pinned 1'])
  })
})

describe('the small words', () => {
  it('names a mode, an item, and another copy', () => {
    expect(modeLabel('ask')).toBe('Ask first')
    expect(itemText(itemRef())).toBe('example-repo #7')
    expect(elsewhereText).toContain(brand.name)
  })

  it('compares a target’s time to a match’s to the second', () => {
    expect(sameSecond('2026-06-15T11:30:00Z', '2026-06-15T11:30:00.123456789Z')).toBe(true)
    expect(sameSecond('2026-06-15T11:30:00Z', '2026-06-15T11:30:01Z')).toBe(false)
    expect(sameSecond('', '2026-06-15T11:30:00Z')).toBe(false)
  })
})
