import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import {
  activityGroup,
  autopilotEffect,
  autopilotEngine,
  autopilotGlance,
  autopilotHold,
  autopilotMatch,
  claudeMissing,
  dryRunMatch,
  itemRef,
  leftAlone,
  readiness,
  routine,
  routineFigures,
  routineReady,
} from '../test/fixtures'
import type { AutopilotMatch, AutopilotState } from '../platform'
import {
  activityLines,
  autopilotBadge,
  backlogText,
  checkText,
  chipOf,
  dirtyText,
  effectState,
  effectVerb,
  elsewhereText,
  filters,
  inActivity,
  inChunks,
  itemText,
  lineOf,
  matchingWords,
  maxIDs,
  mergeDays,
  modeLabel,
  needsYou,
  notListedText,
  notReadyBanner,
  notReadyNames,
  notReadyText,
  readyOf,
  reasonChips,
  routineStatus,
  runLabel,
  sameSecond,
  sheetLine,
  shortHead,
  skipLabel,
  troubled,
} from './autopilot'

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

  it('shows the red dot for a routine this Mac isn’t ready for', () => {
    const engine = autopilotEngine({
      problems: [
        {
          key: 'not_ready:3:agent:claude',
          routineId: 3,
          kind: 'not_ready',
          detail: 'Claude isn’t installed.',
          since: '',
          told: true,
        },
      ],
    })

    expect(autopilotBadge(autopilotGlance(), [routine()], engine)).toMatchObject({
      failed: true,
      title: '1 routine can’t run',
    })
  })
})

describe('readiness', () => {
  const missing = routineReady({ ready: false, problems: [claudeMissing()] })
  const checked = readiness({ routines: [missing, routineReady({ routineId: 4, name: 'Fix checks' })], notReady: 1 })

  it('says what this Mac lacks for a routine, by its first problem', () => {
    expect(checkText(claudeMissing())).toBe('Claude · isn’t installed')
    expect(checkText({ label: 'Terminal', found: '' })).toBe('Terminal')
    expect(readyOf(checked, 3)).toBe(missing)
    expect(readyOf(null, 3)).toBeUndefined()
    expect(notReadyText(missing)).toBe('Not ready on this Mac: Claude · isn’t installed')
    expect(notReadyText(routineReady({ ready: false }))).toBe('Not ready on this Mac')
    expect(notReadyText(routineReady())).toBe('')
    expect(notReadyText(undefined)).toBe('')
  })

  it('names the routines that can’t run here, and counts them', () => {
    expect(notReadyNames(checked)).toEqual(['Snooze passing bot PRs'])
    expect(notReadyNames(null)).toEqual([])
    expect(notReadyBanner(2)).toBe('This Mac can’t run 2 of your routines.')
  })

  it('puts what this Mac lacks in a routine’s status line, while it is on', () => {
    const text = notReadyText(missing)

    expect(routineStatus(routine(), 0, '', text)).toEqual({ text, tone: 'danger' })
    expect(routineStatus(routine({ enabled: false }), 0, '', text).text).toBe('Autopilot · off')
    expect(routineStatus(routine({ broken: 'It won’t load.' }), 0, '', text).text).toBe('It won’t load.')
  })
})

describe('needs you', () => {
  it('sorts the open matches by where they stand, one routine’s alone when asked', () => {
    const waiting = autopilotMatch({ id: 1, state: 'waiting', blockedBy: 'ask', reason: '' })
    const blocked = autopilotMatch({ id: 2, routineId: 4, state: 'blocked', blockedBy: 'not_ready', reason: '' })
    const queued = autopilotMatch({ id: 3, state: 'queued', blockedBy: 'slot', reason: '' })

    expect(needsYou([waiting, blocked, queued])).toEqual({
      waiting: [waiting],
      backlogs: [],
      blocked: [blocked],
      queued: [queued],
    })
    expect(needsYou([waiting, blocked, queued], 4)).toEqual({
      waiting: [],
      backlogs: [],
      blocked: [blocked],
      queued: [],
    })
  })

  it('gathers each routine’s backlog, which Run on them answers at once', () => {
    const one = autopilotMatch({ id: 1, routineId: 6, routine: 'Rebase', state: 'waiting', blockedBy: 'backlog' })
    const two = autopilotMatch({ id: 2, routineId: 6, routine: 'Rebase', state: 'waiting', blockedBy: 'backlog' })
    const other = autopilotMatch({ id: 3, routineId: 7, routine: 'Label', state: 'waiting', blockedBy: 'backlog' })
    const ask = autopilotMatch({ id: 4, routineId: 6, state: 'waiting', blockedBy: 'ask' })

    const got = needsYou([one, ask, two, other])

    expect(got.waiting).toEqual([ask])
    expect(got.backlogs).toEqual([
      { routineId: 6, routine: 'Rebase', matches: [one, two] },
      { routineId: 7, routine: 'Label', matches: [other] },
    ])
    expect(needsYou([one, two, other], 7).backlogs).toEqual([{ routineId: 7, routine: 'Label', matches: [other] }])
    expect(backlogText({ routineId: 7, routine: 'Label', matches: [other] })).toBe(
      '1 item matched at once, more than Label takes on by itself.',
    )
  })

  it('words Run by the hold it confirms, and Skip by the occurrence', () => {
    const files = autopilotMatch({ state: 'waiting', blockedBy: 'hold', hold: autopilotHold() })
    const dirty = autopilotMatch({
      state: 'waiting',
      blockedBy: 'dirty_worktree',
      hold: autopilotHold({ reason: 'dirty', worktree: 'pr-88', dirty: 3 }),
    })

    expect(shortHead('a1b2c3d4e5f6')).toBe('a1b2c3d')
    expect(runLabel(files)).toBe('Run on a1b2c3d')
    expect(runLabel(dirty)).toBe('Run in pr-88 anyway')
    expect(runLabel(autopilotMatch({ state: 'waiting', blockedBy: 'ask' }))).toBe('Run')
    expect(dirtyText(autopilotHold({ worktree: 'pr-88', dirty: 3 }))).toBe('pr-88 has 3 uncommitted files')
    expect(dirtyText(autopilotHold({ worktree: '', dir: '~/projects/example-repo', dirty: 1 }))).toBe(
      '~/projects/example-repo has 1 uncommitted file',
    )
    expect(skipLabel(autopilotMatch())).toBe('Skip this commit')
    expect(skipLabel(autopilotMatch({ item: itemRef({ kind: 'issue' }), fireKey: 'labeled:bug' }))).toBe(
      'Skip this time',
    )
  })

  it('says a match that waits for the user, or for this Mac, needs you, and one queued is queued', () => {
    expect(chipOf(autopilotMatch({ state: 'waiting', blockedBy: 'ask', reason: '', effects: [] }))).toEqual({
      label: 'Needs you',
      tone: 'warning',
    })
    expect(chipOf(autopilotMatch({ state: 'blocked', blockedBy: 'not_ready', reason: '', effects: [] }))).toEqual({
      label: 'Needs you',
      tone: 'warning',
    })
    expect(chipOf(autopilotMatch({ state: 'queued', blockedBy: 'slot', reason: '', effects: [] })).label).toBe('Queued')
  })
})

describe('routineStatus', () => {
  it('says whose another account’s routine is, and why a broken one won’t run, before anything else', () => {
    expect(routineStatus(routine({ otherAccount: true, account: 'octo-other' }), 0, '').text).toBe(
      'Belongs to @octo-other',
    )
    expect(routineStatus(routine({ broken: `Made by a newer ${brand.name}: update to edit it` }), 0, '')).toEqual({
      text: `Made by a newer ${brand.name}: update to edit it`,
      tone: 'danger',
    })
    expect(routineStatus(routine(), 0, 'GitHub refused the search.').tone).toBe('danger')
    expect(routineStatus(routine({ lastError: 'It failed.' }), 0, '').text).toBe('It failed.')
  })

  it('says its mode, and what it is doing or did, from the routine’s own figures', () => {
    const at = '2026-06-14T09:00:00Z'

    expect(routineStatus(routine({ enabled: false }), 0, '').text).toBe('Autopilot · off')
    expect(routineStatus(routine(), 2, '').text).toBe('Autopilot · 2 working')
    expect(routineStatus(routine({ mode: 'ask', figures: routineFigures({ waiting: 1 }) }), 0, '').text).toBe(
      'Ask first · 1 waiting',
    )
    expect(routineStatus(routine({ mode: 'dryRun', figures: routineFigures({ wouldRunToday: 3 }) }), 0, '').text).toBe(
      'Dry run · 3 would run today',
    )
    expect(routineStatus(routine({ figures: routineFigures({ today: 6 }) }), 0, '').text).toBe('Autopilot · 6 today')
    expect(routineStatus(routine({ figures: routineFigures({ lastAt: at }) }), 0, '')).toEqual({
      text: 'Autopilot · ran',
      tone: 'muted',
      since: at,
    })
    expect(routineStatus(routine(), 0, '').text).toBe('Autopilot · nothing matched yet')
  })

  it('says a Dry run’s newest match would have run, never that it ran', () => {
    const at = '2026-06-14T09:00:00Z'

    expect(
      routineStatus(routine({ mode: 'dryRun', figures: routineFigures({ lastAt: at, lastDryRun: true }) }), 0, ''),
    ).toEqual({ text: 'Dry run · would have run', tone: 'muted', since: at })
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
              actions: [],
              sent: 0,
              verdict: '',
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

// everyState is every state the ledger has: a record over the generated union, so a state
// added in Go fails to compile here until the test covers it.
const everyState: Readonly<Record<AutopilotState, true>> = {
  waiting: true,
  blocked: true,
  queued: true,
  running: true,
  verifying: true,
  done: true,
  skipped: true,
  dismissed: true,
  closed: true,
  baseline: true,
}

// verified is a match whose agent pushed 4f2a9c1 and whose Wait for the checks reached
// verdict, saying note, with Tell me after it.
function verified(verdict: AutopilotMatch['steps'][number]['verdict'], note: string): AutopilotMatch {
  const step = (kind: AutopilotMatch['steps'][number]['kind'], extra = {}) => ({
    kind,
    label: kind,
    status: 'done',
    outcome: '',
    note: '',
    runId: 0,
    actions: [],
    sent: 0,
    verdict: '' as const,
    startedAt: null,
    endedAt: null,
    ...extra,
  })

  return autopilotMatch({
    routine: 'Heal Dependabot',
    effects: [],
    steps: [step('agent', { outcome: 'pushed' }), step('waitForChecks', { verdict, note }), step('tellMe')],
  })
}

describe('the verdicts', () => {
  it('chips each verdict, over a notice after it', () => {
    expect(chipOf(verified('fixed', 'Fixed: checks pass on 4f2a9c1.'))).toEqual({ label: 'Fixed', tone: 'accent' })
    expect(chipOf(verified('still_failing', 'Still failing: CodeQL.'))).toEqual({
      label: 'Still failing',
      tone: 'danger',
    })
    expect(chipOf(verified('overtaken', 'Pushed 4f2a9c1, but…')).label).toBe('Overtaken')
    expect(chipOf(verified('push_rejected', 'Its push was rejected…')).label).toBe('Push rejected')
    expect(chipOf(verified('no_push', 'No push: nothing to do.')).label).toBe('No push')
    expect(chipOf(verified('unknown', 'Unknown: …')).label).toBe('Checks unknown')
    expect(chipOf(bare('skipped', 'tries'))).toEqual({ label: 'Gave up', tone: 'danger' })
  })

  it('puts the verdict on the sheet’s line, and that the checks are being read', () => {
    expect(sheetLine(verified('fixed', 'Fixed: checks pass on 4f2a9c1.'))).toEqual({
      routine: 'Heal Dependabot',
      words: 'Fixed: checks pass on 4f2a9c1.',
      tone: 'accent',
    })
    expect(
      sheetLine(autopilotMatch({ state: 'verifying', words: 'The checks on its commit haven’t finished.' })),
    ).toEqual({
      routine: 'Snooze passing bot PRs',
      words: 'The checks on its commit haven’t finished.',
      tone: 'accent',
    })
    expect(sheetLine(autopilotMatch()), 'a match that followed no push has no line').toBeUndefined()
    expect(sheetLine({ ...verified('fixed', 'Fixed.'), dryRun: true }), 'nor a Dry run').toBeUndefined()
    expect(sheetLine(undefined)).toBeUndefined()
  })
})

// bare is a match in state, closed for reason, that ran no step and changed nothing.
function bare(state: AutopilotState, reason: AutopilotMatch['reason']): AutopilotMatch {
  return autopilotMatch({ state, reason, blockedBy: '', steps: [], effects: [] })
}

describe('chipOf over every state and reason', () => {
  it('never says a match ran that didn’t', () => {
    const said = (Object.keys(everyState) as AutopilotState[]).flatMap((state) =>
      (Object.keys(reasonChips) as (keyof typeof reasonChips)[]).map((reason) => ({
        at: `${state}/${reason}`,
        label: chipOf(bare(state, reason)).label,
      })),
    )

    expect(said.filter((s) => s.label === '')).toEqual([])
    expect(said.filter((s) => s.label === 'Ran').map((s) => s.at)).toEqual(['done/ran', 'done/would_run'])
  })

  it('words a baseline and each reason a match closed for', () => {
    expect(chipOf(bare('baseline', 'from_now')).label).toBe('Already matched when you turned it on')
    expect(chipOf(bare('baseline', '')).label).toBe('Already matched when you turned it on')
    expect(chipOf(bare('closed', 'no_longer_matches')).label).toBe('Closed: no longer matches')
    expect(chipOf(bare('closed', 'merged')).label).toBe('Closed: it merged')
    expect(chipOf(bare('closed', 'hands_off')).label).toBe('Left alone')
    expect(chipOf(bare('closed', '')).label).toBe('Closed')
    expect(chipOf(bare('dismissed', 'you_skipped')).label).toBe('You skipped it')
    expect(chipOf(bare('skipped', 'outside_contributor')).label).toBe('Skipped: an outside contributor’s')
    expect(chipOf(bare('done', 'ran')).label).toBe('Ran')
    expect(chipOf(bare('done', 'failed')).label).toBe('A step failed')
  })

  it('says a Dry run would run only of what it would have run', () => {
    const dry = (state: AutopilotState, reason: AutopilotMatch['reason']) => ({ ...bare(state, reason), dryRun: true })

    // Turning a Dry run on from now on records what already matched as its baseline.
    expect(chipOf(dry('baseline', 'from_now')).label).toBe('Already matched when you turned it on')
    expect(notListedText(dry('baseline', 'from_now'))).toBe(
      'Activity has no line for that one. Already matched when you turned it on.',
    )
    expect(chipOf(dry('closed', 'no_longer_matches')).label).toBe('Closed: no longer matches')
    expect(chipOf(dry('done', 'would_run')).label).toBe('Would run')
    expect(chipOf(dry('queued', '')).label).toBe('Would run')
  })

  it('lists in Activity only what did something, and says why another has no line', () => {
    expect(inActivity(bare('done', 'ran'))).toBe(true)
    expect(inActivity(bare('skipped', 'tries'))).toBe(true)
    expect(inActivity(bare('baseline', 'from_now'))).toBe(false)
    expect(inActivity(bare('queued', ''))).toBe(false)
    expect(notListedText(bare('baseline', 'from_now'))).toBe(
      'Activity has no line for that one. Already matched when you turned it on.',
    )
  })
})

describe('filters and chunks', () => {
  it('tells a filter that leaves lines out by its value', () => {
    expect(filters({ routine: 0, result: 'all' })).toBe(false)
    expect(filters({ routine: 3, result: 'all' })).toBe(true)
    expect(filters({ routine: 0, result: 'notices' })).toBe(true)
  })

  it('splits ids into runs the backend reads whole', () => {
    const ids = Array.from({ length: maxIDs * 2 + 1 }, (_, i) => i + 1)
    const chunks = inChunks(ids)

    expect(chunks.map((c) => c.length)).toEqual([maxIDs, maxIDs, 1])
    expect(chunks.flat()).toEqual(ids)
    expect(inChunks([])).toEqual([])
  })
})

describe('matchingWords', () => {
  it('asks the question about what matches now', () => {
    expect(matchingWords({ count: 3, pullRequests: 3, issues: 0, unknown: 0, incomplete: false })).toBe(
      '3 pull requests match right now.',
    )
    expect(matchingWords({ count: 1, pullRequests: 0, issues: 1, unknown: 0, incomplete: false })).toBe(
      '1 issue matches right now.',
    )
    expect(matchingWords({ count: 3, pullRequests: 2, issues: 1, unknown: 1, incomplete: false })).toBe(
      '2 pull requests and 1 issue match right now.',
    )
  })

  it('asks too when none can be told yet, or not everything could be read', () => {
    expect(matchingWords({ count: 0, pullRequests: 0, issues: 0, unknown: 2, incomplete: false })).toBe(
      '2 items may match right now.',
    )
    expect(matchingWords({ count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: true })).toBe(
      'Not everything it watches could be read right now.',
    )
    expect(matchingWords({ count: 1, pullRequests: 1, issues: 0, unknown: 0, incomplete: true })).toBe(
      '1 pull request matches right now.',
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
        actions: [],
        sent: 0,
        verdict: '',
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
