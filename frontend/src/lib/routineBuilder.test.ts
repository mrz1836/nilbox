import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import type { AutopilotProblem, AutopilotSpec, AutopilotStep } from '../platform'
import { autopilotCatalog, routine, routineTest } from '../test/fixtures'
import {
  actsOn,
  addable,
  authorLabel,
  authorValue,
  canonical,
  costLine,
  dryRunWhy,
  fieldsFor,
  lengthParts,
  moved,
  newRoutineName,
  newStep,
  omit,
  onlyIfChoices,
  opsFor,
  outcomeLabel,
  placeLabel,
  placeValue,
  problemsAt,
  problemsExactly,
  routineDraft,
  sameDraft,
  scratchDraft,
  scratchSpec,
  stepKindOf,
  settingRange,
  testedKeys,
  testedLabel,
  testHeadline,
  toolsOf,
  unshown,
  watchChanged,
  watchKinds,
  withKind,
  involvingFor,
  newTrigger,
} from './routineBuilder'

const catalog = autopilotCatalog()

// withSteps is the scratch spec running steps.
function withSteps(...steps: AutopilotStep[]): AutopilotSpec {
  return { ...scratchSpec(), steps }
}

// agentStep is an agent step in the background with tools, or in the terminal.
function agentStep(tools = '', where: 'background' | 'terminal' = 'background'): AutopilotStep {
  return {
    agent: {
      agent: 'inherit',
      prompt: { itemDefault: true },
      where,
      ...(tools === '' ? {} : { fence: { tools: tools as 'readOnly' } }),
    },
  }
}

describe('a draft', () => {
  it('compares by what it stores, whatever order or empty values it was edited into', () => {
    expect(canonical({ b: 1, a: [], c: '', d: null, e: undefined, f: { z: [1], y: '' } })).toBe('{"b":1,"f":{"z":[1]}}')
    expect(canonical(undefined)).toBe('null')
    expect(canonical([{ pin: {} }])).not.toBe(canonical([{ dismiss: {} }]))

    const d = routineDraft(routine())

    expect(sameDraft(d, { ...d, name: ` ${d.name} ` })).toBe(true)
    expect(sameDraft(d, { ...d, description: 'Why' })).toBe(false)
    expect(sameDraft(d, { ...d, mode: 'auto' })).toBe(false)
    expect(sameDraft(d, { ...d, spec: { ...d.spec, limits: { triesPerItem: 3 } } })).toBe(false)
    expect(sameDraft({ ...d, description: undefined as never }, { ...d, description: '' })).toBe(true)
  })

  it('is a saved routine as the builder edits it, naming no mode, its spec its own copy', () => {
    const r = routine()
    const d = routineDraft(r)

    expect(d).toEqual({ id: 3, name: r.name, description: '', spec: r.spec })
    expect(d.spec).not.toBe(r.spec)
    expect('mode' in d).toBe(false)
  })

  it('starts from scratch on your pull requests, in the mode new routines start in, under a free name', () => {
    expect(scratchDraft('New routine', 'dryRun')).toMatchObject({ id: 0, name: 'New routine', mode: 'dryRun' })
    expect(scratchSpec().watch.sources).toEqual([{ search: { kind: 'pr', where: ['@me'] } }])
    expect(newRoutineName([])).toBe('New routine')
    expect(newRoutineName([{ name: 'new ROUTINE' }, { name: 'New routine 2' }])).toBe('New routine 3')
  })

  it('reports a change to what it watches, fires on, or checks, and nothing else', () => {
    const before = scratchSpec()

    expect(watchChanged(before, withSteps({ pin: {} }))).toBe(false)
    expect(watchChanged(before, { ...before, limits: { runsPerDay: 3 } })).toBe(false)
    expect(watchChanged(before, { ...before, watch: { sources: [{ lists: ['authored'] }] } })).toBe(true)
    expect(watchChanged(before, { ...before, trigger: { event: 'appears' } })).toBe(true)
    expect(watchChanged(before, { ...before, conditions: { match: 'any', of: [] } })).toBe(true)
    // Once per decides which occurrence each current match is for.
    expect(watchChanged(before, { ...before, limits: { oncePer: 'day' } })).toBe(true)
    expect(watchChanged({ ...before, limits: { oncePer: 'day' } }, { ...before, limits: { oncePer: 'day' } })).toBe(
      false,
    )
  })

  it('leaves a key out', () => {
    expect(omit({ a: 1, b: 2 }, 'a')).toEqual({ b: 2 })
  })
})

describe('the steps', () => {
  it('names each step’s kind', () => {
    expect(stepKindOf({ snooze: {} })).toBe('snooze')
    expect(stepKindOf(agentStep())).toBe('agent')
    expect(stepKindOf({ onlyIf: 'pushed' })).toBeUndefined()
  })

  it('reads an agent step’s tools and place, with their defaults', () => {
    expect(toolsOf({ prompt: {} })).toBe('editTestPush')
    expect(toolsOf({ prompt: {}, fence: { tools: 'readOnly' } })).toBe('readOnly')
  })

  it('offers only what fits at the end, as the catalog orders it', () => {
    const kinds = (spec: AutopilotSpec) => addable(catalog, spec).map((c) => c.kind)

    const first = kinds(withSteps())

    expect(first).toContain('agent')
    expect(first).toContain('pin')
    expect(first).not.toContain('waitForChecks')
    expect(first).not.toContain('merge')
    expect(first).not.toContain('postReview')
    expect(first).not.toContain('github')

    const afterAgent = kinds(withSteps(agentStep()))

    expect(afterAgent).not.toContain('agent')
    expect(afterAgent).toContain('waitForChecks')
    expect(afterAgent).not.toContain('postReview')

    expect(kinds(withSteps(agentStep('readOnly')))).toContain('postReview')
    expect(kinds(withSteps(agentStep('readOnly')))).not.toContain('waitForChecks')
    expect(kinds(withSteps(agentStep(), { waitForChecks: {}, onlyIf: 'pushed' }))).toContain('merge')
    expect(kinds(withSteps(agentStep(), { waitForChecks: {} }))).not.toContain('merge')
    expect(kinds(withSteps(agentStep('', 'terminal')))).toEqual(['notify', 'tellMe'])

    const issues: AutopilotSpec = {
      ...withSteps(),
      watch: { sources: [{ search: { kind: 'issue', where: ['@me'] } }] },
    }

    expect(kinds({ ...issues, trigger: { event: 'appears' } })).not.toContain('waitForChecks')
    expect(kinds(withSteps(...Array.from({ length: 10 }, () => ({ pin: {} }))))).toEqual([])

    const offered = addable(catalog, withSteps()).find((c) => c.kind === 'agent')

    expect(offered).toMatchObject({ label: 'Run a coding agent', offered: true })
  })

  it('offers a GitHub step once the catalog lists an action to send', () => {
    const withActions = {
      ...catalog,
      githubActions: [
        {
          kind: 'rebase' as const,
          label: 'Ask Dependabot to rebase',
          help: '',
          dispatcher: 'github' as const,
          kinds: ['pr' as const],
          offered: true,
        },
      ],
    }

    expect(addable(withActions, withSteps()).map((c) => c.kind)).toContain('github')
  })

  it('adds each kind of step at its defaults', () => {
    expect(newStep('snooze', [])).toEqual({ snooze: {} })
    expect(newStep('agent', [])).toEqual({ agent: { agent: 'inherit', prompt: { itemDefault: true } } })
    expect(newStep('agent', [], catalog.places)).toEqual({
      agent: { agent: 'inherit', prompt: { itemDefault: true }, where: 'terminal' },
    })
    expect(newStep('waitForChecks', [])).toEqual({ waitForChecks: {}, onlyIf: 'pushed' })
    expect(newStep('merge', [])).toEqual({ merge: { how: 'autoMergeIfItWasOn' }, onlyIf: 'passed' })
    expect(newStep('postReview', [])).toEqual({ postReview: { as: 'comment' } })
    expect(newStep('tellMe', [])).toEqual({ tellMe: { when: 'reached' } })
    expect(newStep('tellMe', [agentStep()])).toEqual({ tellMe: { when: 'verdict' } })
    expect(newStep('notify', [])).toEqual({ notify: {} })
    expect(newStep('github', [])).toEqual({ github: [] })
    expect(newStep('pin', [])).toEqual({ pin: {} })
  })

  it('offers each step only the outcomes a step before it can end with', () => {
    const ids = (steps: AutopilotStep[], i: number) => onlyIfChoices(catalog, steps, i).map((o) => o.id)

    expect(ids([{ pin: {} }], 0)).toEqual([])
    expect(ids([{ pin: {} }, { notify: {} }], 1)).toEqual([])
    expect(ids([agentStep(), { notify: {} }], 1)).toEqual(['pushed', 'notPushed'])
    expect(ids([agentStep('readOnly'), { notify: {} }], 1)).toEqual(['approve', 'requestChanges'])
    expect(ids([agentStep(), { waitForChecks: {} }], 1)).toEqual(['pushed'])
    expect(ids([agentStep(), { waitForChecks: {}, onlyIf: 'pushed' }, { merge: { how: 'whenMergeable' } }], 2)).toEqual(
      ['passed'],
    )
    expect(ids([agentStep(), { waitForChecks: {}, onlyIf: 'pushed' }, { tellMe: {} }], 2)).toEqual([
      'passed',
      'stillFails',
      'pushed',
      'notPushed',
    ])
    expect(ids([agentStep()], 3)).toEqual(['pushed', 'notPushed'])
  })

  it('moves one, and leaves the list as it was for one that isn’t there', () => {
    expect(moved(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c'])
    expect(moved(['a', 'b'], 5, 0)).toEqual(['a', 'b'])
  })
})

describe('the kinds of item', () => {
  it('are those the watch holds, and of them those the trigger fires for', () => {
    expect(watchKinds(catalog, { ...scratchSpec(), watch: { sources: [] } })).toEqual(['pr', 'issue'])
    expect(watchKinds(catalog, { ...scratchSpec(), watch: { sources: [{ search: { where: ['@me'] } }] } })).toEqual([
      'pr',
      'issue',
    ])
    expect(watchKinds(catalog, { ...scratchSpec(), watch: { sources: [{ lists: ['reviewRequested'] }] } })).toEqual([
      'pr',
    ])
    expect(watchKinds(catalog, { ...scratchSpec(), watch: { sources: [{ lists: ['authored'] }] } })).toEqual([
      'pr',
      'issue',
    ])

    const both: AutopilotSpec = { ...scratchSpec(), watch: { sources: [{ lists: ['assigned'] }] } }

    expect(actsOn(catalog, both)).toEqual(['pr'])
    expect(actsOn(catalog, { ...both, trigger: { event: 'appears' } })).toEqual(['pr', 'issue'])
    expect(actsOn(catalog, { ...both, trigger: { event: 'unknown' as 'appears' } })).toEqual(['pr', 'issue'])
  })

  it('decide the fields a condition can read, and each field its operators', () => {
    expect(fieldsFor(catalog, ['issue']).some((f) => f.id === 'checks.failedNames')).toBe(false)
    expect(fieldsFor(catalog, ['pr']).some((f) => f.id === 'checks.failedNames')).toBe(true)

    const field = (id: string) => {
      const f = catalog.fields.find((x) => x.id === id)

      if (f === undefined) {
        throw new Error(`no field ${id}`)
      }

      return f
    }

    expect(opsFor(catalog, field('item.lists')).map((o) => o.id)).toEqual([
      'hasAny',
      'hasAll',
      'hasNone',
      'isEmpty',
      'notEmpty',
    ])
    expect(opsFor(catalog, field('checks.failedNames')).map((o) => o.id)).toContain('anyContains')
    expect(opsFor(catalog, field('pr.lines')).map((o) => o.id)).toEqual(['eq', 'ne', 'lt', 'lte', 'gt', 'gte'])
  })
})

describe('the problems', () => {
  const problems = [
    { part: 'watch' as const, field: 'sources[0].search.where', message: 'name a scope' },
    { part: 'conditions' as const, field: 'of[1].values[0]', message: 'bad' },
    { part: 'conditions' as const, field: 'of[10]', message: 'far' },
    { part: 'conditions' as const, field: 'of[1]', message: 'here' },
    { part: 'version' as const, field: '', message: 'this build reads version 1' },
  ]

  it('belong where they are, and no deeper path takes another’s', () => {
    expect(problemsAt(problems, 'conditions', 'of[1]').map((p) => p.message)).toEqual(['bad', 'here'])
    expect(problemsAt(problems, 'conditions').length).toBe(3)
    expect(problemsAt(problems, 'watch', 'sources[0]').length).toBe(1)
    expect(problemsExactly(problems, 'conditions', 'of[1]').map((p) => p.message)).toEqual(['here'])
  })

  it('show at the top only where no block draws them', () => {
    const spec: AutopilotSpec = {
      ...scratchSpec(),
      conditions: {
        match: 'all',
        of: [{ field: 'item.title', op: 'contains', value: 'x' }, { group: { match: 'any', of: [] } }],
      },
    }
    const at = (part: AutopilotProblem['part'], field: string): AutopilotProblem => ({ part, field, message: field })

    expect(
      unshown(
        [
          at('version', ''),
          at('trigger', 'schedule.at'),
          at('limits', 'activeHours.days[0]'),
          at('watch', 'sources'),
          at('watch', 'sources[0]'),
          at('watch', 'sources[0].search.where'),
          at('watch', 'sources[3]'),
          at('conditions', 'of'),
          at('conditions', 'match'),
          at('conditions', 'of[0].value'),
          at('conditions', 'of[1].group.of'),
          at('conditions', 'of[4]'),
          at('steps', ''),
          at('steps', '[0].note'),
          at('steps', '[2]'),
        ],
        spec,
      ).map((p) => `${p.part}.${p.field}`),
    ).toEqual(['version.', 'watch.sources[3]', 'conditions.of[4]', 'steps.[2]'])
  })
})

describe('what a test holds for', () => {
  it('keys its matches by what decides them, its notes by the searches, and its warnings by the steps too', () => {
    const spec = scratchSpec()
    const was = testedKeys(3, spec)

    expect(testedKeys(3, { ...spec, steps: [{ pin: {} }] })).toEqual({ ...was, steps: canonical([{ pin: {} }]) })
    expect(testedKeys(3, { ...spec, limits: { runsPerDay: 2 } })).toEqual(was)

    const oncePer = testedKeys(3, { ...spec, limits: { oncePer: 'day' } })

    expect(oncePer.key).not.toBe(was.key)
    expect(oncePer.watch).toBe(was.watch)

    const watched = testedKeys(3, { ...spec, watch: { sources: [{ lists: ['authored'] }] } })

    expect(watched.key).not.toBe(was.key)
    expect(watched.watch).not.toBe(was.watch)
    expect(testedKeys(4, spec).key).not.toBe(was.key)
  })
})

describe('a trigger and a search as they start', () => {
  it('stores the schedule On a schedule shows, and nothing for another trigger', () => {
    expect(newTrigger(catalog, 'schedule')).toEqual({ event: 'schedule', schedule: { every: 'day', at: '09:00' } })
    expect(newTrigger(catalog, 'checks_failed')).toEqual({ event: 'checks_failed' })
  })

  it('drops what only a pull request has once a search looks for issues', () => {
    const search = {
      kind: 'pr' as const,
      where: ['@me'],
      base: 'main',
      headPrefix: 'dependabot/',
      state: ['failing' as const],
      involving: ['reviewRequested' as const, 'mentioned' as const],
      labels: ['security'],
    }

    expect(withKind(search, 'issue')).toEqual({
      kind: 'issue',
      where: ['@me'],
      involving: ['mentioned'],
      labels: ['security'],
    })
    expect(withKind({ ...search, involving: ['reviewRequested'] }, 'issue')).not.toHaveProperty('involving')
    expect(withKind(search, '')).toEqual(omit(search, 'kind'))
    expect(withKind({ ...search, kind: 'issue' }, 'pr')).toEqual(search)
    expect(involvingFor('issue')).toEqual(['assigned', 'mentioned'])
    expect(involvingFor('pr')).toEqual(['assigned', 'reviewRequested', 'mentioned'])
  })

  it('reads a setting’s bounds and default from the catalog', () => {
    expect(settingRange(catalog.stepSettings, 'snooze.days')).toEqual({ min: 1, max: 365, default: 3, unit: 'days' })
    expect(settingRange(catalog.agentSettings, 'budgetTokens')).toMatchObject({ min: 10_000, default: 200_000 })
    expect(settingRange(catalog.limits, 'nothing').default).toBe(0)
  })
})

describe('the words', () => {
  it('say why steps run in Dry run alone', () => {
    expect(dryRunWhy(catalog, ['agent'])).toBe(
      `Agent steps run in Dry run until this version of ${brand.name} can run agents.`,
    )
    expect(dryRunWhy(catalog, ['merge'])).toBe(
      `“Merge it” runs in Dry run until this version of ${brand.name} can merge.`,
    )
    expect(dryRunWhy(catalog, ['unknown' as 'pin'])).toBe(
      `“unknown” runs in Dry run until this version of ${brand.name} can run it.`,
    )
  })

  it('say what the searches cost a refresh', () => {
    expect(costLine({ requests: 1, pointsPerRefresh: 1, pointsPerHour: 12, percent: 0.24 })).toBe(
      'About 1 point a refresh (0.2% of the budget)',
    )
    expect(costLine({ requests: 2, pointsPerRefresh: 2, pointsPerHour: 24, percent: 0.04 })).toBe(
      'About 2 points a refresh (<0.1% of the budget)',
    )
    expect(costLine({ requests: 9, pointsPerRefresh: 90, pointsPerHour: 1080, percent: 21.6 })).toContain('(22% of')
    expect(costLine({ requests: 0, pointsPerRefresh: 0, pointsPerHour: 0, percent: 0 })).toContain('Nothing more')
    // A test of earlier searches says so.
    expect(costLine({ requests: 1, pointsPerRefresh: 1, pointsPerHour: 12, percent: 0.24 }, true)).toBe(
      'About 1 point a refresh (0.2% of the budget) · as last tested, before the searches changed',
    )
  })

  it('say what a test found, and what it spent', () => {
    expect(testHeadline(routineTest(), ['pr'])).toBe('1 of 2 open pull requests matches now')
    expect(testHeadline(routineTest({ matched: 0, unknown: 1, watched: 1 }), ['issue'])).toBe(
      '0 of 1 open issue match now; 1 can’t be told yet',
    )
    expect(testHeadline(routineTest({ matched: 2, watched: 2 }), ['pr', 'issue'])).toBe('2 of 2 open items match now')
    expect(testHeadline(routineTest({ watched: 0 }), ['pr'])).toBe('No open pull requests to try it on now.')
    expect(testHeadline(routineTest({ matched: 0, watched: 1 }), ['pr'])).toBe('0 of 1 open pull request match now')
    expect(testHeadline(routineTest({ watched: 0 }), ['issue'])).toBe('No open issues to try it on now.')
    expect(testHeadline(routineTest({ watched: 0 }), [])).toBe('No open items to try it on now.')

    expect(testedLabel(undefined)).toBe('Test')
    expect(testedLabel(routineTest())).toBe('Test (3 points)')
    expect(testedLabel(routineTest({ points: 0, cached: true }))).toBe('Test (cached)')
    expect(testedLabel(routineTest({ points: 0 }))).toBe('Test')
  })

  it('name authors and places as tokens, and read them as typed', () => {
    expect(authorLabel('@me')).toBe('You')
    expect(authorLabel('app/dependabot')).toBe('Dependabot')
    expect(authorLabel('app/acme-bot')).toBe('acme-bot')
    expect(authorLabel('octo-reviewer')).toBe('octo-reviewer')

    expect(authorValue('me', [])).toBe('@me')
    expect(authorValue('@me', [])).toBe('@me')
    expect(authorValue('App/Renovate', [])).toBe('app/renovate')
    expect(authorValue('Dependabot', ['dependabot'])).toBe('app/dependabot')
    expect(authorValue('acme-bot[bot]', [])).toBe('app/acme-bot')
    expect(authorValue('@octo-reviewer', [])).toBe('octo-reviewer')

    expect(placeLabel('@me')).toBe('Your account')
    expect(placeLabel('org:acme-org')).toBe('acme-org')
    expect(placeLabel('acme-org')).toBe('acme-org')

    expect(placeValue('Your account', '')).toBe('@me')
    expect(placeValue('octo-user', 'octo-user')).toBe('@me')
    expect(placeValue('User:octo-other', 'octo-user')).toBe('user:octo-other')
    expect(placeValue('acme-org/api', '')).toBe('repo:acme-org/api')
    expect(placeValue(' acme-labs ', '')).toBe('org:acme-labs')
  })

  it('say an Only if, and read a length', () => {
    expect(outcomeLabel(catalog, 'pushed')).toBe('only if it pushed')
    expect(outcomeLabel(catalog, 'gone' as 'pushed')).toBe('only if gone')
    expect(lengthParts('30m')).toEqual({ n: '30', unit: 'm' })
    expect(lengthParts('2h')).toEqual({ n: '2', unit: 'h' })
    expect(lengthParts('14d')).toEqual({ n: '14', unit: 'd' })
  })
})
