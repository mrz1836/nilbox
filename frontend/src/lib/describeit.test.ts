import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { AutopilotRunEvent, DescribeCard, KitSettingKey } from '../platform'
import {
  describeCard,
  kitChecked,
  kitCheckedPrompt,
  kitCheckedRoutine,
  kitCheckedRules,
  kitCheckedSettings,
  kitNewPrompt,
  item,
  kitOutcome,
  kitRoutine,
  prompt,
  routine,
  routineSpec,
  runEvent,
  settings,
} from '../test/fixtures'
import { platform } from '../test/platform'
import {
  agentWords,
  appliedWords,
  applyAll,
  applyRules,
  applySettings,
  appTool,
  cardParts,
  cardPlaces,
  changeDraft,
  changedId,
  chipsFor,
  claudeModeHint,
  costWords,
  fallbackChips,
  headline,
  isEdit,
  lastSeq,
  listsOnly,
  newRoutineDraft,
  outcomeText,
  partKey,
  partLabel,
  partProblems,
  promptInput,
  promptKindWords,
  recorded,
  revisedCards,
  settlePart,
  settleWords,
  storedMatchWords,
  toolWords,
  turnEntries,
  withChips,
  type ApplyAllKit,
  type CardPart,
  type RoutinePart,
} from './describeit'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// tool is the app's own tool named name, as an in-app turn names it.
function tool(name: string): string {
  return `mcp__${brand.slug}__${name}`
}

// events numbers a turn's events in order: each its own line of the log.
function events(...list: Partial<AutopilotRunEvent>[]): AutopilotRunEvent[] {
  return list.map((e, i) => runEvent({ seq: i + 1, offset: i * 100, part: 0, ...e }))
}

// fullCard is a card with every kind of part: a prompt, a new routine, a change to routine
// 3, a settings change, and a rules change.
function fullCard(overrides: Partial<DescribeCard> = {}): DescribeCard {
  return describeCard({
    proposal: {
      summary: 'Everything at once.',
      prompts: [kitNewPrompt()],
      routines: [
        {
          name: 'Fix checks',
          description: 'Fixes them.',
          mode: 'auto',
          template: 'fix-failing-checks',
          spec: routineSpec(),
        },
        { id: 3, base: '2026-06-15T08:00:00Z', name: 'Snooze passing bot PRs', spec: routineSpec() },
      ],
      settings: { agentsAtOnce: 1 },
      rules: { trust: ['renovate[bot]'], mute: ['acme-org/legacy'] },
    },
    checked: kitChecked({
      prompts: [kitCheckedPrompt()],
      routines: [
        kitCheckedRoutine({ index: 0, name: 'Fix checks' }),
        kitCheckedRoutine({ index: 1, name: 'Snooze passing bot PRs', before: kitRoutine() }),
      ],
      settings: kitCheckedSettings(),
      rules: kitCheckedRules(),
    }),
    ...overrides,
  })
}

// part is the part of card named key ("routine:1").
function part(card: DescribeCard, key: string): CardPart {
  const found = cardParts(card).find((p) => partKey(p) === key)

  if (found === undefined) {
    throw new Error(`no part ${key}`)
  }

  return found
}

// routinePart is the routine part of card at index.
function routinePart(card: DescribeCard, index: number): RoutinePart {
  const found = part(card, `routine:${String(index)}`)

  if (found.part !== 'routine') {
    throw new Error('not a routine')
  }

  return found
}

describe('appTool', () => {
  it('reads the app’s own tools in a turn and in the user’s own Claude Code, and nothing else', () => {
    expect(appTool(tool('autopilot_check'))).toBe('autopilot_check')
    expect(appTool(`mcp__plugin_${brand.slug}_autopilot__autopilot_propose`)).toBe('autopilot_propose')
    expect(appTool(`mcp__plugin_${brand.slug}___autopilot_propose`)).toBe('')
    expect(appTool(`mcp__plugin_${brand.slug}_autopilot`)).toBe('')
    expect(appTool('mcp__other__autopilot_check')).toBe('')
    expect(appTool('Bash')).toBe('')
  })
})

describe('headline', () => {
  it('is a result’s first sentence, without its full stop', () => {
    expect(headline('Checked 2 routines: 1 problem.\n- Fix checks: no step')).toBe('Checked 2 routines: 1 problem')
    expect(headline('Checked 1 routine: no problems. It reads: When checks fail, pin it.')).toBe(
      'Checked 1 routine: no problems',
    )
    expect(headline('  Proposed 2 routines and a setting  ')).toBe('Proposed 2 routines and a setting')
    expect(headline('Is it right? Yes.')).toBe('Is it right?')
    expect(headline('')).toBe('')
  })
})

describe('toolWords', () => {
  const use = (name: string, text = tool(name)) => runEvent({ kind: 'tool', tool: tool(name), text })
  const result = (text: string) => runEvent({ kind: 'toolResult', text })

  it('says what each of the app’s tools did', () => {
    expect(toolWords(use('autopilot_guide'), undefined)).toBe('Read how Autopilot works')
    expect(toolWords(use('autopilot_my_setup'), undefined)).toBe('Read your setup')
    expect(toolWords(use('autopilot_inbox_names'), undefined)).toBe('Read the names in your inbox')
    expect(toolWords(use('autopilot_proposal_status'), undefined)).toBe('Read what you did with its proposals')
    expect(toolWords(use('autopilot_future'), undefined)).toBe('Used autopilot_future')
    expect(toolWords(runEvent({ kind: 'tool', tool: 'WebFetch', text: 'WebFetch' }), undefined)).toBe('Used WebFetch')
  })

  it('says a check’s and a proposal’s own first words once their results are in', () => {
    expect(toolWords(use('autopilot_check'), undefined)).toBe('Checking the proposal')
    expect(toolWords(use('autopilot_check'), result('Checked 2 routines: 1 problem.\nThe first …'))).toBe(
      'Checked 2 routines: 1 problem',
    )
    expect(toolWords(use('autopilot_check'), result(''))).toBe('Checked the proposal')
    expect(toolWords(use('autopilot_propose'), undefined)).toBe('Proposing it')
    expect(toolWords(use('autopilot_propose'), result('Proposed 2 routines and a setting. It shows …'))).toBe(
      'Proposed 2 routines and a setting',
    )
    expect(toolWords(use('autopilot_propose'), result(''))).toBe('Proposed it')
  })

  it('names the topic a reference read was for, from its input or its result’s heading', () => {
    expect(toolWords(use('autopilot_reference', 'triggers'), undefined)).toBe('Read about triggers')
    expect(toolWords(use('autopilot_reference', '{"topic":"conditions"}'), undefined)).toBe('Read about conditions')
    expect(toolWords(use('autopilot_reference'), result('# Triggers\nOne per routine.'))).toBe('Read about triggers')
    expect(toolWords(use('autopilot_reference'), result('# GitHub actions\nWhat it may send.'))).toBe(
      'Read about GitHub actions',
    )
    // The headings the app's reference opens with, read as a sentence carries on.
    expect(toolWords(use('autopilot_reference'), result('# The GitHub step\n…'))).toBe('Read about the GitHub step')
    expect(toolWords(use('autopilot_reference'), result("# A routine's definition\n…"))).toBe(
      "Read about a routine's definition",
    )
    expect(toolWords(use('autopilot_reference'), result('# A routine on a schedule\n…'))).toBe(
      'Read about a routine on a schedule',
    )
    expect(toolWords(use('autopilot_reference'), result("# Autopilot's settings\n…"))).toBe(
      "Read about Autopilot's settings",
    )
    expect(toolWords(use('autopilot_reference'), result(`# ${brand.name} and you\n…`))).toBe(
      `Read about ${brand.name} and you`,
    )
    expect(toolWords(use('autopilot_reference'), result('One per routine.'))).toBe('Read Autopilot’s reference')
    expect(toolWords(use('autopilot_reference', ''), undefined)).toBe('Read Autopilot’s reference')
  })
})

describe('turnEntries', () => {
  it('answers each tool with the result after it, marks what failed, and finds the proposals that went through', () => {
    const list = turnEntries(
      events(
        { kind: 'started', text: 'claude-haiku-5-5 · Claude Code 2.1.328' },
        { kind: 'tool', tool: tool('autopilot_guide') },
        { kind: 'tool', tool: tool('autopilot_my_setup') },
        { kind: 'toolResult', text: '# How Autopilot works' },
        { kind: 'toolResult', text: 'The setup couldn’t be read. Try again.', error: true },
        { kind: 'thinking', text: '' },
        { kind: 'text', text: 'Let me check it.' },
        { kind: 'tool', tool: tool('autopilot_propose') },
        { kind: 'toolResult', text: 'The proposal has 1 problem: no step.', error: true },
        { kind: 'tool', tool: tool('autopilot_propose') },
        { kind: 'toolResult', text: 'Proposed 1 routine. It shows in the Autopilot tab.' },
        { kind: 'retry', text: 'Retrying (1 of 10): overloaded' },
        { kind: 'limit', text: "Out of usage: its plan's five-hour limit is reached", error: true },
        { kind: 'denied', tool: 'Bash', text: 'not allowed here' },
        { kind: 'denied', tool: '', text: '' },
        { kind: 'toolResult', text: 'an answer to nothing' },
        { kind: 'task', text: 'a task' },
        { kind: 'result', text: 'done' },
      ),
      false,
      '',
    )

    expect(
      list.map((e) => (e.kind === 'activity' ? [e.text, e.state, e.detail, e.proposed] : [e.kind, e.text])),
    ).toEqual([
      ['Read how Autopilot works', 'ok', '', false],
      ['Read your setup', 'failed', 'The setup couldn’t be read', false],
      ['say', 'Let me check it.'],
      ['The proposal has 1 problem: no step', 'failed', '', false],
      ['Proposed 1 routine', 'ok', '', true],
      ['note', 'Retrying (1 of 10): overloaded'],
      ['note', "Out of usage: its plan's five-hour limit is reached"],
      ['note', 'Claude Code didn’t let it use Bash: not allowed here'],
      ['note', 'Claude Code didn’t let it use a tool'],
    ])
    expect(list.map((e) => e.key)).toContain('100:0')
  })

  it('keeps a tool without an answer pending while the turn goes, and unmarked once it ended', () => {
    const asked = events({ kind: 'tool', tool: tool('autopilot_check') })

    expect(turnEntries(asked, true, '')).toMatchObject([
      { kind: 'activity', state: 'pending', text: 'Checking the proposal' },
    ])
    expect(turnEntries(asked, false, '')).toMatchObject([{ kind: 'activity', state: '' }])
  })

  it('leaves out what Claude said last when it is the reply, whole or cut short', () => {
    const said = events({ kind: 'text', text: 'First.' }, { kind: 'text', text: 'Here is a routine.' })

    expect(turnEntries(said, false, 'Here is a routine.').map((e) => (e.kind === 'say' ? e.text : ''))).toEqual([
      'First.',
    ])
    expect(turnEntries(said, false, 'Here is a routine. It pins them.')).toHaveLength(1)
    expect(turnEntries(said, false, 'Something else.')).toHaveLength(2)
    expect(turnEntries(events({ kind: 'tool', tool: tool('autopilot_guide') }), false, 'Done.')).toHaveLength(1)
  })
})

describe('lastSeq and cardPlaces', () => {
  it('finds the newest event, and places each card after the proposal that made it', () => {
    expect(lastSeq([])).toBe(0)
    expect(lastSeq(events({}, {}, {}))).toBe(3)

    const list = turnEntries(
      events(
        { kind: 'tool', tool: tool('autopilot_propose') },
        { kind: 'toolResult', text: 'Proposed 1 routine.' },
        { kind: 'tool', tool: tool('autopilot_propose') },
        { kind: 'toolResult', text: 'Proposed a setting.' },
      ),
      false,
      '',
    )

    expect(cardPlaces(list, ['a'])).toEqual({ at: new Map([['0:0', 'a']]), rest: [] })
    expect(cardPlaces(list, ['a', 'b', 'c'])).toEqual({
      at: new Map([
        ['0:0', 'a'],
        ['200:0', 'b'],
      ]),
      rest: ['c'],
    })
    expect(cardPlaces([], ['a', 'b'])).toEqual({ at: new Map(), rest: ['a', 'b'] })
  })
})

describe('a card’s parts', () => {
  it('lists them in the order they apply, each numbered as the proposal numbers it', () => {
    const card = fullCard()

    expect(cardParts(card).map(partKey)).toEqual(['prompt:0', 'routine:0', 'routine:1', 'settings:0', 'rules:0'])
    expect(routinePart(card, 0).change?.name).toBe('Fix checks')
    expect(cardParts(describeCard({ proposal: { summary: 'none' } }))).toMatchObject([
      { part: 'routine', change: undefined },
    ])
  })

  it('tells a change from a new routine, and names each part', () => {
    const card = fullCard()
    const added = routinePart(card, 0)
    const changed = routinePart(card, 1)

    expect(isEdit(added)).toBe(false)
    expect(isEdit(changed)).toBe(true)
    expect(changedId(changed)).toBe(3)
    expect(changedId(added)).toBe(0)
    expect(changedId({ ...changed, change: undefined })).toBe(3)
    expect(isEdit({ ...added, checked: { ...added.checked, gone: true } })).toBe(true)
    expect(isEdit({ ...added, change: { name: 'x', id: 9, spec: routineSpec() } })).toBe(true)
    expect(cardParts(card).map(partLabel)).toEqual([
      'the prompt “Fix it”',
      'the routine “Fix checks”',
      'the changes to “Snooze passing bot PRs”',
      'the settings change',
      'the rules change',
    ])
    expect(partLabel({ ...changed, checked: { ...changed.checked, before: null, gone: true } })).toBe(
      'the changes to “Snooze passing bot PRs”',
    )
    expect(
      partLabel({ part: 'prompt', index: 0, checked: kitCheckedPrompt({ prompt: kitNewPrompt({ nickname: '' }) }) }),
    ).toBe('the prompt “Fix the failing checks”')
  })

  it('words what each outcome records for Claude’s next turn, and what its row says', () => {
    const card = fullCard()

    expect(settleWords(routinePart(card, 0), 'added')).toBe('Added “Fix checks”, switched off')
    expect(settleWords(routinePart(card, 0), 'builder')).toBe('Opened “Fix checks” in the builder')
    expect(settleWords(routinePart(card, 0), 'discarded')).toBe('Discarded the routine “Fix checks”')
    expect(settleWords(routinePart(card, 1), 'saved')).toBe('Saved the changes to “Snooze passing bot PRs”')
    expect(settleWords(routinePart(card, 1), 'discarded')).toBe('Discarded the changes to “Snooze passing bot PRs”')
    expect(settleWords(part(card, 'prompt:0'), 'added')).toBe('Added the prompt “Fix it”')
    expect(settleWords(part(card, 'settings:0'), 'applied')).toBe('Applied the settings change')
    expect(settleWords(part(card, 'rules:0'), 'applied')).toBe('Applied the rules change')
    expect(settleWords(part(card, 'rules:0'), 'discarded')).toBe('Discarded the rules change')

    expect(outcomeText('routine', 'added')).toBe('Added, switched off')
    expect(outcomeText('prompt', 'added')).toBe('Added to your prompts')
    expect(outcomeText('routine', 'saved')).toBe('Changes saved')
    expect(outcomeText('settings', 'applied')).toBe('Applied')
    expect(outcomeText('routine', 'builder')).toBe('Opened in the builder')
    expect(outcomeText('rules', 'discarded')).toBe('Discarded')
  })

  it('reads what the card recorded of a part', () => {
    const applied = {
      proposal: '20260615T120010Z-9f2c4100ab31',
      part: 'settings' as const,
      index: 0,
      kind: 'applied' as const,
      words: 'Applied the settings change',
      at: '2026-06-15T12:01:00Z',
    }
    const card = fullCard({ outcomes: [kitOutcome(), applied] })

    expect(recorded(card, { part: 'routine', index: 0 })).toEqual({
      kind: 'added',
      words: 'Added “Snooze passing bot PRs”, switched off',
      routineId: 11,
    })
    expect(recorded(card, { part: 'settings', index: 0 })).toMatchObject({ kind: 'applied', routineId: 0 })
    expect(recorded(card, { part: 'rules', index: 0 })).toBeUndefined()
  })

  it('says what keeps a part from being applied', () => {
    const card = fullCard()
    const changed = routinePart(card, 1)

    expect(partProblems(changed)).toEqual([])
    expect(
      partProblems({
        ...changed,
        checked: { ...changed.checked, gone: true, problems: [{ part: 'steps', field: '', message: 'Add a step.' }] },
      }),
    ).toEqual(['The routine it changes is gone.', 'Add a step.'])
    expect(
      partProblems({ part: 'settings', index: 0, checked: kitCheckedSettings({ problems: ['Too many.'] }) }),
    ).toEqual(['Too many.'])
  })
})

describe('what a card applies', () => {
  it('adds a prompt with the library’s defaults for everything but its words', () => {
    expect(promptInput(kitNewPrompt())).toEqual({
      name: 'Fix the failing checks',
      nickname: 'Fix it',
      appliesTo: 'pr',
      body: 'Fix the failing checks of {{.Repo}} #{{.Number}}.',
      purpose: 'agent',
      replyAction: '',
      models: {},
      effort: '',
      planMode: null,
      worktree: null,
      extraFlags: {},
    })
    expect(promptInput(kitNewPrompt({ purpose: 'reply', replyAction: 'comment' })).replyAction).toBe('comment')
  })

  it('adds a new routine in the mode chosen, and saves a change in the routine’s own', () => {
    const card = fullCard()

    expect(newRoutineDraft(routinePart(card, 0), 'dryRun')).toEqual({
      id: 0,
      name: 'Fix checks',
      description: 'Fixes them.',
      mode: 'dryRun',
      spec: routineSpec(),
      templateKey: 'fix-failing-checks',
    })
    expect(newRoutineDraft({ ...routinePart(card, 0), change: undefined }, 'ask')).toMatchObject({
      description: '',
      templateKey: '',
    })
    expect(changeDraft(routine({ id: 3, mode: 'auto', description: 'Its own.' }), routinePart(card, 1))).toEqual({
      id: 3,
      name: 'Snooze passing bot PRs',
      description: 'Its own.',
      mode: 'auto',
      spec: routineSpec(),
    })

    const described = routinePart(card, 1)

    expect(
      changeDraft(routine({ description: 'Its own.' }), {
        ...described,
        change: { ...routinePart(card, 0).change, name: 'x', spec: routineSpec(), description: 'New words.' },
      }).description,
    ).toBe('New words.')
  })

  it('tells a routine that watches the inbox’s lists alone', () => {
    expect(listsOnly(routineSpec())).toBe(true)
    expect(listsOnly({ ...routineSpec(), watch: { sources: [] } })).toBe(false)
    expect(listsOnly({ ...routineSpec(), watch: { sources: [{ lists: [] }] } })).toBe(false)
    expect(
      listsOnly({
        ...routineSpec(),
        watch: { sources: [{ lists: ['reviewRequested'] }, { search: { kind: 'pr', where: ['@me'] } }] },
      }),
    ).toBe(false)
  })

  it('says what a routine matches in the inbox now', () => {
    const m = { count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: false }

    expect(storedMatchWords(m)).toBe('Matches nothing in your inbox now')
    expect(storedMatchWords({ ...m, unknown: 2 })).toBe(
      '2 items in your inbox may match now, once what it checks is read',
    )
    expect(storedMatchWords({ ...m, count: 3, pullRequests: 2, issues: 1 })).toBe(
      'Matches 2 pull requests and 1 issue in your inbox now',
    )
    expect(storedMatchWords({ ...m, count: 1, pullRequests: 1, unknown: 1, incomplete: true })).toBe(
      'Matches at least 1 pull request in your inbox now; 1 more can’t be told until what it checks is read',
    )
    expect(storedMatchWords({ ...m, count: 2 })).toBe('Matches 2 items in your inbox now')
  })

  it('hints at the mode Claude suggests, as a template’s suggestion does', () => {
    expect(claudeModeHint('auto', 'ask')).toBe(
      'Claude suggests Autopilot. It starts in Ask first, as new routines do: change it here if you like.',
    )
    expect(claudeModeHint('ask', 'ask')).toBe('')
    expect(claudeModeHint(undefined, 'ask')).toBe('')
  })

  it('applies a settings change one setter at a time, the history before its logs, and stops at a failure', async () => {
    const after = {
      ...settings().autopilot,
      agentsAtOnce: 1,
      historyDays: 30,
      logDays: 7,
      dailyCap: { amount: 5, unit: 'usd' as const },
    }
    const change = kitCheckedSettings({
      changes: [
        { key: 'logDays', label: 'Keep logs', before: '30 days', after: '7 days' },
        { key: 'historyDays', label: 'Keep history', before: '90 days', after: '30 days' },
        { key: 'agentsAtOnce', label: 'Agents at once', before: '2', after: '1' },
        { key: 'dailyCap', label: 'Daily cap', before: 'none', after: '$5' },
      ],
      after,
    })
    const order: string[] = []

    platform.setAutopilotAgentsAtOnce.mockImplementation((n) => {
      order.push(`agents ${String(n)}`)

      return Promise.resolve(settings())
    })
    platform.setAutopilotDailyCap.mockImplementation((cap) => {
      order.push(`cap ${String(cap.amount)}`)

      return Promise.resolve(settings())
    })
    platform.setAutopilotHistoryDays.mockImplementation((days) => {
      order.push(`history ${String(days)}`)

      return Promise.resolve(settings())
    })
    platform.setAutopilotLogDays.mockImplementation((days) => {
      order.push(`logs ${String(days)}`)

      return Promise.resolve(settings())
    })

    expect(await applySettings(change)).toEqual({ ok: true, data: undefined })
    expect(order).toEqual(['agents 1', 'cap 5', 'history 30', 'logs 7'])

    platform.setAutopilotDailyCap.mockRejectedValueOnce(new Error('Keep the cap to $1,000 a day.'))
    order.length = 0
    expect(await applySettings(change)).toEqual({ ok: false, error: 'Keep the cap to $1,000 a day.' })
    expect(order).toEqual(['agents 1'])
  })

  it('applies every kind of setting through its own setter', async () => {
    const after = settings().autopilot
    const keys: KitSettingKey[] = [
      'defaultMode',
      'runsPerDay',
      'actionsPerDay',
      'keepAwake',
      'holdOnOutage',
      'catchUpHours',
    ]

    await applySettings(
      kitCheckedSettings({ changes: keys.map((key) => ({ key, label: key, before: '', after: '' })), after }),
    )

    expect(platform.setAutopilotDefaultMode).toHaveBeenCalledWith('ask')
    expect(platform.setAutopilotRunsPerDay).toHaveBeenCalledWith(20)
    expect(platform.setAutopilotActionsPerDay).toHaveBeenCalledWith(50)
    expect(platform.setAutopilotKeepAwake).toHaveBeenCalledWith(false)
    expect(platform.setAutopilotHoldOnOutage).toHaveBeenCalledWith(true)
    expect(platform.setAutopilotCatchUpHours).toHaveBeenCalledWith(24)
    expect(platform.setAutopilotAgentsAtOnce).not.toHaveBeenCalled()
  })

  it('applies a rules change: the trusted bots, each target’s rule, and the ignored workflows', async () => {
    const change = kitCheckedRules({
      changes: [
        { kind: 'trust', target: 'renovate[bot]', words: 'Trust renovate[bot]' },
        { kind: 'pin', target: 'acme-org/api', words: 'Pin acme-org/api' },
        { kind: 'mute', target: 'acme-org/legacy', words: 'Mute acme-org/legacy' },
        { kind: 'clear', target: 'acme-org', words: 'Clear acme-org' },
        { kind: 'ignoreWorkflows', target: 'Deploy', words: 'Ignore the held runs of Deploy' },
      ],
      after: {
        trustedBots: ['renovate[bot]'],
        pinned: ['acme-org/api'],
        muted: ['acme-org/legacy'],
        ignoredWorkflows: ['Deploy'],
      },
    })

    expect(await applyRules(change)).toEqual({ ok: true, data: undefined })
    expect(platform.setTrustedBots).toHaveBeenCalledWith(['renovate[bot]'])
    expect(platform.setRepoRule.mock.calls).toEqual([
      ['acme-org/api', 'pinned'],
      ['acme-org/legacy', 'muted'],
      ['acme-org', ''],
    ])
    expect(platform.setIgnoredWorkflows).toHaveBeenCalledWith(['Deploy'])

    platform.setRepoRule.mockRejectedValueOnce(new Error('That isn’t a repository or an org.'))
    expect(await applyRules(change)).toEqual({ ok: false, error: 'That isn’t a repository or an org.' })

    expect(
      await applyRules(
        kitCheckedRules({ changes: [{ kind: 'unignoreWorkflows', target: 'Deploy', words: 'Count Deploy again' }] }),
      ),
    ).toEqual({
      ok: true,
      data: undefined,
    })
    expect(
      await applyRules(kitCheckedRules({ changes: [{ kind: 'untrust', target: 'x[bot]', words: 'Untrust x[bot]' }] })),
    ).toEqual({
      ok: true,
      data: undefined,
    })
  })

  it('records what the user did with a part', async () => {
    const card = fullCard()

    expect(
      await settlePart(card, routinePart(card, 0), {
        kind: 'added',
        words: 'Added “Fix checks”, switched off',
        routineId: 11,
      }),
    ).toEqual({
      ok: true,
      data: undefined,
    })
    expect(platform.settleDescribeItCard).toHaveBeenCalledWith({
      ref: card.ref,
      part: 'routine',
      index: 0,
      kind: 'added',
      words: 'Added “Fix checks”, switched off',
      routineId: 11,
    })
  })
})

describe('applyAll', () => {
  // kitOf is Apply all's kit, nothing settled, a new routine in Ask first, and a change
  // saved as routine 3.
  function kitOf(overrides: Partial<ApplyAllKit> = {}): ApplyAllKit {
    return {
      settled: () => false,
      modeOf: () => 'ask',
      saveChange: () => Promise.resolve({ ok: true, data: routine({ id: 3 }) }),
      onSettled: vi.fn(),
      ...overrides,
    }
  }

  it('applies the parts in order, prompts, routines, settings, rules, and records each as it goes', async () => {
    const order: string[] = []

    platform.createPrompt.mockImplementation((input) => {
      order.push(`prompt ${input.nickname}`)

      return Promise.resolve(prompt({ id: 9, nickname: input.nickname }))
    })
    platform.saveRoutine.mockImplementation((d) => {
      order.push(`routine ${d.name}`)

      return Promise.resolve(routine({ id: 12, name: d.name }))
    })
    platform.setAutopilotAgentsAtOnce.mockImplementation(() => {
      order.push('settings')

      return Promise.resolve(settings())
    })
    platform.setTrustedBots.mockImplementation(() => {
      order.push('rules')

      return Promise.resolve(settings())
    })
    platform.settleDescribeItCard.mockImplementation((req) => {
      order.push(`settle ${req.part}:${String(req.index)} ${req.kind}`)

      return Promise.resolve()
    })

    const saveChange = vi.fn(() => {
      order.push('change')

      return Promise.resolve({ ok: true as const, data: routine({ id: 3 }) })
    })
    const kit = kitOf({ saveChange })

    expect(await applyAll(fullCard(), kit)).toEqual({ ok: true })
    expect(order).toEqual([
      'prompt Fix it',
      'settle prompt:0 added',
      'routine Fix checks',
      'settle routine:0 added',
      'change',
      'settle routine:1 saved',
      'settings',
      'settle settings:0 applied',
      'rules',
      'settle rules:0 applied',
    ])
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({
      id: 0,
      mode: 'ask',
      templateKey: 'fix-failing-checks',
    })
    expect(vi.mocked(kit.onSettled).mock.calls.map(([p, s]) => [partKey(p), s])).toEqual([
      ['prompt:0', { kind: 'added', words: 'Added the prompt “Fix it”', routineId: 0 }],
      ['routine:0', { kind: 'added', words: 'Added “Fix checks”, switched off', routineId: 12 }],
      ['routine:1', { kind: 'saved', words: 'Saved the changes to “Snooze passing bot PRs”', routineId: 3 }],
      ['settings:0', { kind: 'applied', words: 'Applied the settings change', routineId: 0 }],
      ['rules:0', { kind: 'applied', words: 'Applied the rules change', routineId: 0 }],
    ])
  })

  it('leaves the parts already dealt with alone', async () => {
    const kit = kitOf({ settled: (p) => p.part !== 'settings' })

    expect(await applyAll(fullCard(), kit)).toEqual({ ok: true })
    expect(platform.createPrompt).not.toHaveBeenCalled()
    expect(platform.saveRoutine).not.toHaveBeenCalled()
    expect(platform.setAutopilotAgentsAtOnce).toHaveBeenCalledOnce()
    expect(platform.setTrustedBots).not.toHaveBeenCalled()
  })

  it('stops at the first part that fails, saying which and why, and keeps what it applied', async () => {
    platform.saveRoutine.mockRejectedValueOnce(new Error('Another routine has that name.'))

    const stopped = await applyAll(fullCard(), kitOf())

    expect(stopped).toMatchObject({ ok: false, error: 'Another routine has that name.' })
    expect(stopped.ok ? '' : appliedWords(stopped)).toBe(
      'Stopped at the routine “Fix checks”: Another routine has that name.',
    )
    expect(platform.createPrompt).toHaveBeenCalledOnce()
    expect(platform.settleDescribeItCard).toHaveBeenCalledOnce()
    expect(platform.setAutopilotAgentsAtOnce).not.toHaveBeenCalled()
  })

  it('stops at a part with problems, and at a question closed unanswered', async () => {
    const card = fullCard()
    const blocked = {
      ...card,
      checked: { ...card.checked, prompts: [kitCheckedPrompt({ problems: ['Another prompt has that nickname.'] })] },
    }
    const atProblem = await applyAll(blocked, kitOf())

    expect(atProblem).toMatchObject({ ok: false, error: 'Another prompt has that nickname.' })
    expect(platform.createPrompt).not.toHaveBeenCalled()

    const unanswered = await applyAll(card, kitOf({ saveChange: () => Promise.resolve({ ok: false, error: '' }) }))

    expect(unanswered.ok ? '' : appliedWords(unanswered)).toBe(
      'Stopped at the changes to “Snooze passing bot PRs”: the question about what it matches now was closed unanswered.',
    )
  })

  it('stops at each kind of part that fails', async () => {
    platform.createPrompt.mockRejectedValueOnce(new Error('Another prompt has that nickname.'))
    expect(await applyAll(fullCard(), kitOf())).toMatchObject({ ok: false, part: { part: 'prompt' } })

    platform.setAutopilotAgentsAtOnce.mockRejectedValueOnce(new Error('Keep it to 8.'))
    expect(
      await applyAll(fullCard(), kitOf({ settled: (p) => p.part === 'prompt' || p.part === 'routine' })),
    ).toMatchObject({
      ok: false,
      part: { part: 'settings' },
      error: 'Keep it to 8.',
    })

    platform.setTrustedBots.mockRejectedValueOnce(new Error('Not a bot.'))
    expect(await applyAll(fullCard(), kitOf({ settled: (p) => p.part !== 'rules' }))).toMatchObject({
      ok: false,
      part: { part: 'rules' },
      error: 'Not a bot.',
    })
  })

  it('stops when what it did can’t be recorded, saying it was applied', async () => {
    platform.settleDescribeItCard.mockRejectedValueOnce(new Error('The card is gone.'))

    const kit = kitOf()

    expect(await applyAll(fullCard(), kit)).toMatchObject({
      ok: false,
      part: { part: 'prompt' },
      error: 'It was applied, but Claude won’t hear of it: The card is gone.',
    })
    expect(kit.onSettled).toHaveBeenCalledOnce()
  })
})

describe('revisedCards', () => {
  it('finds the cards a later one proposes again', () => {
    const card = (id: string, proposal: DescribeCard['proposal']) =>
      describeCard({ ref: { source: 'c', proposal: id }, proposal })
    const cards = [
      card('a', { summary: 'a', routines: [{ name: 'Fix checks', spec: routineSpec() }] }),
      card('b', { summary: 'b', prompts: [kitNewPrompt()] }),
      card('c', { summary: 'c', settings: { agentsAtOnce: 1 } }),
      card('d', { summary: 'd', routines: [{ id: 3, name: 'Snooze', spec: routineSpec() }] }),
      card('e', { summary: 'e', rules: { trust: ['x[bot]'] } }),
      card('f', { summary: 'f', routines: [{ name: 'fix CHECKS', spec: routineSpec() }], settings: { runsPerDay: 2 } }),
      card('g', {
        summary: 'g',
        routines: [{ id: 3, name: 'Snooze it', spec: routineSpec() }],
        prompts: [kitNewPrompt({ nickname: 'FIX IT' })],
      }),
      card('h', { summary: 'h', rules: { mute: ['acme-org'] } }),
    ]

    expect([...revisedCards(cards)].sort()).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(revisedCards([])).toEqual(new Set())
  })
})

describe('the header’s and a prompt’s words', () => {
  it('says what a prompt is for, what a conversation cost, and the agent it runs with', () => {
    expect(promptKindWords(kitNewPrompt())).toBe('Pull requests · a coding agent runs it')
    expect(promptKindWords(kitNewPrompt({ appliesTo: 'issue', purpose: 'reply', replyAction: 'comment' }))).toBe(
      'Issues · a reply that comments',
    )
    expect(promptKindWords(kitNewPrompt({ appliesTo: 'any', purpose: 'reply', replyAction: 'approve' }))).toBe(
      'Pull requests and issues · a reply that approves',
    )
    expect(promptKindWords(kitNewPrompt({ purpose: 'reply', replyAction: 'request_changes' }))).toBe(
      'Pull requests · a reply that requests changes',
    )
    expect(promptKindWords(kitNewPrompt({ purpose: 'reply' }))).toBe('Pull requests · a review reply')

    expect(costWords(null, true)).toBe('')
    expect(costWords(0.041, true)).toBe('about $0.04')
    expect(costWords(1.5, false)).toBe('$1.50')

    expect(agentWords({ profile: 'work', model: 'opus', effort: 'high' })).toBe('Claude · work · opus · high')
    expect(agentWords({ profile: '', model: '', effort: '' })).toBe('Claude')
  })
})

describe('chips', () => {
  it('names each attachment as this Mac stores it, and falls back to its kind and ID', async () => {
    platform.routineNames.mockResolvedValue([{ id: 3, name: 'Snooze passing bot PRs', deleted: false }])
    platform.itemsByID.mockResolvedValue([item({ id: 42, repo: 'acme-org/example-repo', number: 42 })])

    expect(
      await chipsFor([
        { kind: 'routine', id: 3 },
        { kind: 'item', id: 42 },
        { kind: 'routine', id: 9 },
        { kind: 'item', id: 7 },
      ]),
    ).toEqual([
      { ref: { kind: 'routine', id: 3 }, label: 'Snooze passing bot PRs' },
      { ref: { kind: 'item', id: 42 }, label: 'acme-org/example-repo#42' },
      { ref: { kind: 'routine', id: 9 }, label: 'Routine 9' },
      { ref: { kind: 'item', id: 7 }, label: 'Item 7' },
    ])
    expect(platform.itemsByID).toHaveBeenCalledWith([42, 7])

    expect(await chipsFor([{ kind: 'item', id: 42 }])).toHaveLength(1)
    expect(platform.routineNames).toHaveBeenCalledOnce()
    expect(await chipsFor([{ kind: 'routine', id: 3 }])).toHaveLength(1)
    expect(platform.itemsByID).toHaveBeenCalledTimes(2)
  })

  it('adds chips each once, and names those it couldn’t read', () => {
    const held = fallbackChips([{ kind: 'routine', id: 3 }])

    expect(held).toEqual([{ ref: { kind: 'routine', id: 3 }, label: 'Routine 3' }])
    expect(
      withChips(held, [
        { ref: { kind: 'routine', id: 3 }, label: 'again' },
        { ref: { kind: 'item', id: 4 }, label: 'Item 4' },
        { ref: { kind: 'item', id: 4 }, label: 'twice' },
      ]),
    ).toEqual([
      { ref: { kind: 'routine', id: 3 }, label: 'Routine 3' },
      { ref: { kind: 'item', id: 4 }, label: 'Item 4' },
    ])
  })
})
