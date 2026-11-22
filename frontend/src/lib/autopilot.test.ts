import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import {
  activityGroup,
  autopilotEffect,
  autopilotEngine,
  autopilotGlance,
  autopilotHold,
  autopilotMatch,
  autopilotUpcoming,
  claudeMissing,
  dryRunMatch,
  githubStep,
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
  cadenceOf,
  checkText,
  chipOf,
  chipView,
  describeAttach,
  describeExamples,
  describeSeesText,
  editable,
  effectState,
  effectVerb,
  elsewhereText,
  filters,
  followUpOf,
  inActivity,
  inChunks,
  itemText,
  liveOf,
  matchingWords,
  maxIDs,
  mergeDays,
  modeLabel,
  needsYou,
  notListedText,
  notReadyBanner,
  notReadyNames,
  notReadyText,
  onTitle,
  pastRulesBlocked,
  pastRulesOffStatus,
  pendingText,
  plannedText,
  readyOf,
  reviewPostLabel,
  reasonChips,
  routineStatus,
  rowAutopilot,
  runLabel,
  sameSecond,
  sheetLine,
  shortHead,
  skipLabel,
  skippedBacklogText,
  troubled,
  upcomingChipText,
  upcomingLine,
  upcomingTitle,
  upcomingWhen,
} from './autopilot'

describe('autopilotBadge', () => {
  it('counts nothing while nothing works, waits, or is broken', () => {
    expect(autopilotBadge(autopilotGlance(), [routine()], autopilotEngine())).toEqual({
      count: 0,
      attention: false,
      failed: false,
      title: '',
      live: 'off',
    })
    expect(autopilotBadge(null, [], null).count).toBe(0)
  })

  it('glows while Autopilot runs a routine, saying so, and breathes while an agent works', () => {
    const on = autopilotGlance({ routines: 1, enabled: 1 })

    expect(autopilotBadge(on, [routine()], autopilotEngine())).toMatchObject({ live: 'on', title: onTitle })
    expect(liveOf(on, autopilotEngine())).toBe('on')
    expect(liveOf({ ...on, paused: true }, autopilotEngine())).toBe('off')
    expect(liveOf({ ...on, enabled: 0 }, autopilotEngine())).toBe('off')
    expect(liveOf(on, autopilotEngine({ elsewhere: true }))).toBe('off')
    expect(liveOf(on, autopilotEngine({ signedOut: true }))).toBe('off')
    expect(liveOf(null, null)).toBe('off')
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
      live: 'working',
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

  it('says what Skip them skipped of the backlog its card showed', () => {
    const shown = [1, 2, 3].map((id) =>
      autopilotMatch({ id, routineId: 6, routine: 'Rebase', state: 'waiting', blockedBy: 'backlog' }),
    )
    const b = { routineId: 6, routine: 'Rebase', matches: shown }

    expect(skippedBacklogText(b, 3)).toBe('Skipped them. Rebase waits for the next time each fires.')
    expect(skippedBacklogText(b, 2)).toBe('Skipped 2 items; 1 other had moved on already.')
    expect(skippedBacklogText(b, 1)).toBe('Skipped 1 item; 2 others had moved on already.')
    expect(skippedBacklogText(b, 0)).toBe('There was nothing left to skip: they had all moved on.')
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

  it('says, in red, when its merge past the rules waits for the switch that is off', () => {
    const merging = routine({ pastRules: 'allowed', figures: routineFigures({ today: 1 }) })

    expect(pastRulesBlocked(merging, false)).toBe(true)
    expect(pastRulesBlocked(merging, true)).toBe(false)
    expect(pastRulesBlocked(merging, null)).toBe(false)
    expect(pastRulesBlocked(routine({ pastRules: '' }), false)).toBe(false)
    expect(routineStatus(merging, 0, '', '', true)).toEqual({
      text: `Autopilot · ${pastRulesOffStatus}`,
      tone: 'danger',
    })
    expect(routineStatus({ ...merging, enabled: false }, 0, '', '', true).text).toBe('Autopilot · off')
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
          steps: [githubStep({ kind: 'notify', label: 'Notify me', status: 'done', outcome: 'done', actions: [] })],
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

type MatchStep = AutopilotMatch['steps'][number]

// step is a match's step of kind, done, with extra over it.
function step(kind: MatchStep['kind'], extra: Partial<MatchStep> = {}): MatchStep {
  return {
    kind,
    label: kind,
    status: 'done',
    outcome: '',
    note: '',
    runId: 0,
    actions: [],
    sent: 0,
    verdict: '',
    review: null,
    postAs: '',
    reviewAs: '',
    allowApprove: false,
    merge: '',
    autoMerge: false,
    startedAt: null,
    endedAt: null,
    ...extra,
  }
}

// verified is a match whose agent pushed 4f2a9c1 and whose Wait for the checks reached
// verdict, saying note, with Tell me after it.
function verified(verdict: MatchStep['verdict'], note: string): AutopilotMatch {
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

  it('chips what a step after the run sent, over the verdict', () => {
    const fixed = verified('fixed', 'Fixed: checks pass on 4f2a9c1.')
    const merged = (autoMerge: boolean): AutopilotMatch => ({
      ...fixed,
      steps: [...fixed.steps, step('merge', { autoMerge })],
    })

    expect(chipOf(merged(false))).toEqual({ label: 'Merged', tone: 'accent' })
    expect(chipOf(merged(true)).label).toBe('Auto-merge on')

    const posted = (outcome: string): AutopilotMatch => ({
      ...fixed,
      steps: [step('postReview', { outcome })],
    })

    expect(chipOf(posted('approve')).label).toBe('Approved')
    expect(chipOf(posted('requestChanges')).label).toBe('Changes requested')
    expect(chipOf(posted('')).label).toBe('Reviewed')
  })

  it('says what a step after the run would do, for its card', () => {
    const base = verified('fixed', 'Fixed: checks pass on 4f2a9c1.')
    const merge = {
      ...base,
      step: 3,
      steps: [...base.steps, step('merge', { status: '', merge: 'Merges it.' })],
    }

    expect(followUpOf(merge)).toEqual({ kind: 'merge', does: 'Merges it.', verdict: 'Fixed: checks pass on 4f2a9c1.' })
    expect(followUpOf(base), 'a match at no such step').toBeUndefined()

    const review = {
      verdict: 'approve' as const,
      body: 'LGTM',
      comments: [{ path: 'a.go', line: 1, startLine: 0, side: '', body: 'x' }],
    }
    const post = (reviewAs: 'verdict' | 'comment', allowApprove: boolean, postAs = '') => ({
      ...base,
      step: 1,
      steps: [step('agent', { review }), step('postReview', { status: '', reviewAs, allowApprove, postAs })],
    })

    const f = followUpOf(post('verdict', true))
    expect(f).toEqual({
      kind: 'review',
      verdict: 'approve',
      body: 'LGTM',
      comments: 1,
      allowApprove: true,
      asComment: false,
    })

    const label = (m: AutopilotMatch) => {
      const got = followUpOf(m)

      return got?.kind === 'review' ? reviewPostLabel(got) : ''
    }

    expect(label(post('verdict', true))).toBe('Post approval')
    expect(label(post('verdict', false))).toBe('Post as comment')
    expect(label(post('comment', false))).toBe('Post as comment')
    expect(label(post('verdict', true, 'comment'))).toBe('Post as comment')
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
    expect(inActivity(bare('closed', 'not_offered'))).toBe(false)

    // A match that sent a reply, then found its merge no longer offered, did something.
    const replied = {
      ...bare('closed', 'not_offered'),
      steps: [
        githubStep({ kind: 'github', label: 'Reply', status: 'done', actions: ['reply'] }),
        githubStep({ kind: 'merge', label: 'Merge it', status: 'skipped', note: 'Not sent: it is off.', actions: [] }),
      ],
    }

    expect(inActivity(replied)).toBe(true)
    expect(
      activityLines([{ date: '2026-11-22', matches: [replied], groups: [] }], [], { routine: 0, result: 'attention' }),
    ).toHaveLength(1)
    expect(notListedText(bare('baseline', 'from_now'))).toBe(
      'Activity has no line for that one. Already matched when you turned it on.',
    )
  })
})

describe('rowAutopilot', () => {
  it('says a routine waits on the user or works on the item, as its chip does, and nothing else', () => {
    const at = (state: AutopilotState, dryRun = false) => rowAutopilot({ ...bare(state, ''), dryRun })

    expect(at('waiting')).toBe('needs:Snooze passing bot PRs')
    expect(at('blocked')).toBe('needs:Snooze passing bot PRs')
    expect(at('running')).toBe('working:Snooze passing bot PRs')
    expect(at('verifying')).toBe('working:Snooze passing bot PRs')
    expect(at('queued')).toBe('')
    expect(at('done')).toBe('')
    // A Dry run would only have run.
    expect(at('waiting', true)).toBe('')
    expect(rowAutopilot(undefined)).toBe('')
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

describe('what runs next', () => {
  const now = new Date('2026-06-15T12:00:00Z')
  const next = autopilotUpcoming()

  it('puts what goes on now first, then what runs next, then what was', () => {
    const ran = autopilotMatch()
    const waiting = autopilotMatch({ state: 'waiting', blockedBy: 'ask', reason: '', closedAt: null })

    expect(chipView(waiting, [next])).toEqual({ kind: 'match', match: waiting })
    expect(chipView(ran, [next])).toEqual({ kind: 'upcoming', upcoming: [next], next })
    expect(chipView(undefined, [next])).toEqual({ kind: 'upcoming', upcoming: [next], next })
    expect(chipView(ran, [])).toEqual({ kind: 'match', match: ran })
    expect(chipView(undefined, [])).toBeNull()
  })

  it('says when, in the trigger’s words, or counting down to a moment the clock decides', () => {
    expect(upcomingWhen(next, now)).toBe('when its checks pass')
    expect(upcomingWhen({ when: 'once its checks settle', at: '2026-06-15T12:02:00Z' }, now)).toBe('in 2m')
    expect(upcomingWhen({ when: 'at 13:00', at: '2026-06-15T13:00:00Z' }, now)).toBe('in 1h')
    expect(upcomingWhen({ when: 'once its checks settle', at: '2026-06-15T11:59:00Z' }, now)).toBe('any moment')
    expect(upcomingChipText(next, now)).toBe('Runs when its checks pass')
  })

  it('says who runs next, how, and what each will do', () => {
    expect(upcomingTitle([next], now)).toBe('Merge syncs runs when its checks pass')
    expect(upcomingTitle([{ ...next, mode: 'ask', asksFirst: true }, next, next], now)).toBe(
      'Merge syncs asks you first when its checks pass · 2 more',
    )
    expect(upcomingTitle([], now)).toBe('')
    expect(upcomingLine(next, now)).toBe('Runs by itself when its checks pass')
    expect(upcomingLine({ asksFirst: true, when: 'once its checks settle', at: '2026-06-15T12:02:00Z' }, now)).toBe(
      'Asks you first, once its checks settle · in 2m',
    )
    expect(plannedText(next.steps)).toBe('Comment one of my replies, then Merge it')
    expect(plannedText([])).toBe('')
  })
})

describe('cadenceOf', () => {
  const on = autopilotGlance({ routines: 1, enabled: 1 })

  it('says when Autopilot checks the routines, and when it last did', () => {
    expect(cadenceOf(on, autopilotEngine())).toEqual({
      text: 'Checks your routines every minute, after each inbox refresh, and at once when you change one',
      lastAt: '2026-06-15T11:59:00Z',
    })
  })

  it('says it waits for the first refresh, or that no routine is on', () => {
    expect(cadenceOf(on, autopilotEngine({ lastCycle: null }))?.text).toMatch(/^Starting/)
    expect(cadenceOf({ ...on, enabled: 0 }, autopilotEngine())?.text).toMatch(/^No routine is on/)
  })

  it('says nothing while the banners say what stops everything', () => {
    expect(cadenceOf({ ...on, paused: true }, autopilotEngine())).toBeNull()
    expect(cadenceOf({ ...on, on: false }, autopilotEngine())).toBeNull()
    expect(cadenceOf(on, autopilotEngine({ elsewhere: true }))).toBeNull()
    expect(cadenceOf(on, autopilotEngine({ signedOut: true }))).toBeNull()
    expect(cadenceOf(on, null)).toBeNull()
  })
})

describe('activityLines', () => {
  const days = activityPage().days

  it('lists a grouped line in place of the matches it stands for, and the rest by themselves, newest first', () => {
    const lines = activityLines(days, [leftAlone()], { routine: 0, result: 'all' })

    expect(lines.map((l) => l.kind)).toEqual(['leftAlone', 'match', 'group'])
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
      autopilotMatch({ id: 21, state: 'closed', reason: 'no_longer_matches', effects: [], steps: [] }),
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
    steps: [githubStep({ kind: 'tellMe', label: 'Tell me', status: 'done', actions: [] })],
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

describe('the words that open Describe it', () => {
  it('attaches the routine, then the item, a request names', () => {
    expect(describeAttach({})).toEqual([])
    expect(describeAttach({ routineId: 0, itemId: 0 })).toEqual([])
    expect(describeAttach({ itemId: 7 })).toEqual([{ kind: 'item', id: 7 }])
    expect(describeAttach({ routineId: 3 })).toEqual([{ kind: 'routine', id: 3 }])
    expect(describeAttach({ itemId: 7, routineId: 3 })).toEqual([
      { kind: 'routine', id: 3 },
      { kind: 'item', id: 7 },
    ])
  })

  it('counts the proposals from the user’s own Claude Code that wait', () => {
    expect(pendingText(1)).toBe('1 proposal from your own Claude Code waits')
    expect(pendingText(3)).toBe('3 proposals from your own Claude Code wait')
  })

  it('offers three examples, and says what Claude sees, never an item’s text or code', () => {
    expect(describeExamples).toHaveLength(3)
    expect(describeSeesText).toMatch(/never your items’ text or code\.$/)
  })

  it('lets a routine change here, with Claude too, only while it is the user’s and its spec reads', () => {
    expect(editable(routine())).toBe(true)
    expect(editable(routine({ problems: [{ part: 'steps', field: '[0]', message: 'Its prompt is gone.' }] }))).toBe(
      true,
    )
    expect(editable(routine({ otherAccount: true }))).toBe(false)
    expect(editable(routine({ brokenReason: 'newer', broken: 'Made by a newer build' }))).toBe(false)
    expect(editable(routine({ brokenReason: 'unreadable', broken: 'It can’t be read' }))).toBe(false)
  })
})
