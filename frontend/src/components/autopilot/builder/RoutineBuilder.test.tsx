import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotDraft, AutopilotSpec, AutopilotStepKind } from '../../../platform'
import {
  agents,
  autopilotCatalog,
  described,
  profile,
  prompts,
  routineSuggestions,
  routineTest,
  testItem,
} from '../../../test/fixtures'
import { platform } from '../../../test/platform'
import { RoutineBuilder } from './RoutineBuilder'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// flagship is the flagship routine as the builder holds it: Dependabot's pull requests in
// your account and acme-org, when checks fail, if a failed check's name contains security
// or codeql, run Fix checks, wait for the checks if it pushed, then tell me.
function flagship(): AutopilotSpec {
  return {
    version: 1,
    watch: { sources: [{ search: { kind: 'pr', authors: ['app/dependabot'], where: ['@me', 'org:acme-org'] } }] },
    trigger: { event: 'checks_failed' },
    conditions: {
      match: 'all',
      of: [{ field: 'checks.failedNames', op: 'anyContains', values: ['security', 'codeql'] }],
    },
    steps: [
      { agent: { agent: 'inherit', prompt: { starter: 'fix-checks' } } },
      { waitForChecks: {}, onlyIf: 'pushed' },
      { tellMe: { when: 'verdict' } },
    ],
    limits: { oncePer: 'headCommit', triesPerItem: 2 },
  }
}

const onSave = vi.fn()
const onDiscard = vi.fn()
const onEditPrompts = vi.fn()
const changed = vi.fn<(d: AutopilotDraft) => void>()

interface HarnessProps {
  spec?: AutopilotSpec
  isNew?: boolean
  heldKinds?: AutopilotStepKind[]
  saving?: boolean
  saveError?: string
  name?: string
}

// Harness holds the draft as the tab does, the builder's delays at nothing.
function Harness({
  spec = flagship(),
  isNew = false,
  heldKinds = [],
  saving = false,
  saveError = '',
  name = 'Fix security',
}: HarnessProps) {
  const [start] = useState<AutopilotDraft>(() => ({ id: isNew ? 0 : 9, name, description: '', spec }))
  const [draft, setDraft] = useState<AutopilotDraft>(isNew ? { ...start, mode: 'ask' } : start)

  return (
    <RoutineBuilder
      draft={draft}
      baseline={isNew ? { ...start, mode: 'ask' } : start}
      isNew={isNew}
      sentence={isNew ? '' : 'When checks fail, as it was saved.'}
      mode="ask"
      heldKinds={heldKinds}
      catalog={autopilotCatalog()}
      suggestions={routineSuggestions()}
      viewer="octo-user"
      agentKit={{
        prompts: prompts(),
        agents: agents(),
        profiles: [
          profile(),
          profile({ name: 'work', model: 'opus', effort: 'high' }),
          profile({ agent: 'codex', name: 'default' }),
        ],
        defaultAgent: 'claude',
        defaultProfiles: { claude: 'default' },
        onEditPrompts,
      }}
      saving={saving}
      saveError={saveError}
      onChange={(d) => {
        changed(d)
        setDraft(d)
      }}
      onSave={onSave}
      onDiscard={onDiscard}
      delays={{ describe: 0, test: 0 }}
    />
  )
}

// spec is the draft's spec as last edited.
function spec(): AutopilotSpec {
  const last = changed.mock.lastCall?.[0]

  if (last === undefined) {
    throw new Error('the draft has not changed')
  }

  return last.spec
}

// block returns the queries scoped to one of the builder's blocks.
function block(name: 'Watch' | 'When' | 'If' | 'Then' | 'Limits') {
  return within(screen.getByRole('region', { name }))
}

function renderBuilder(props: HarnessProps = {}) {
  return render(<Harness {...props} />)
}

describe('the routine builder', () => {
  it('renders a routine from the catalog, top to bottom in the order it runs, with its sentence and its cost', async () => {
    platform.describeRoutine.mockResolvedValue(
      described({
        sentence: 'When checks fail on pull requests by Dependabot, run Fix checks.',
        dryRunOnly: ['agent'],
      }),
    )
    renderBuilder()

    expect(
      await screen.findByText('When checks fail on pull requests by Dependabot, run Fix checks.'),
    ).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Watch',
      'When',
      'If',
      'Then',
      'Limits',
      'Test',
    ])

    const watch = block('Watch')

    expect(watch.getByRole('combobox', { name: 'Look for' })).toHaveValue('pr')
    expect(watch.getByRole('button', { name: 'Remove Dependabot' })).toBeInTheDocument()
    expect(watch.getByRole('button', { name: 'Remove Your account' })).toBeInTheDocument()
    expect(watch.getByRole('button', { name: 'Remove acme-org' })).toBeInTheDocument()
    expect(await watch.findByText('About 1 point a refresh (0.2% of the budget)')).toBeInTheDocument()

    expect(block('When').getByRole('combobox', { name: 'Trigger' })).toHaveValue('checks_failed')
    expect(block('When').getByRole('checkbox', { name: 'Wait until every check has finished' })).toBeChecked()
    expect(block('If').getByRole('combobox', { name: 'Condition 1: field' })).toHaveValue('checks.failedNames')
    expect(block('If').getByRole('button', { name: 'Remove security' })).toBeInTheDocument()

    const then = block('Then')

    expect(then.getByText('Run a coding agent')).toBeInTheDocument()
    expect(then.getAllByText('· Dry run for now').length).toBe(2)
    expect(then.getByRole('combobox', { name: 'Step 2 runs' })).toHaveValue('pushed')
    expect(then.getByRole('combobox', { name: 'Prompt' })).toHaveValue('starter:fix-checks')
    expect(block('Limits').getByRole('combobox', { name: 'Once per' })).toHaveValue('headCommit')
    expect(block('Limits').getByRole('spinbutton', { name: 'Tries per item' })).toHaveValue(2)

    expect(await screen.findByText('1 of 2 open pull requests matches now')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Test (3 points)' })).toBeEnabled()
    expect(platform.testRoutine).toHaveBeenCalledWith(expect.objectContaining({ id: 9, spec: flagship() }))
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('shows each problem where it is, keeps the last sentence, tries nothing, and holds Save', async () => {
    const user = userEvent.setup()
    platform.describeRoutine.mockResolvedValue(
      described({
        sentence: '',
        problems: [
          { part: 'watch', field: 'sources[0].search.where', message: 'name a user, an org, or a repository' },
          { part: 'conditions', field: 'of[0].values', message: 'give at least one value' },
          { part: 'steps', field: '[0].agent.prompt', message: 'that starter prompt isn’t in your library any more' },
          { part: 'steps', field: '[1].onlyIf', message: 'only if it pushed needs an agent step before it' },
          { part: 'trigger', field: 'settleMinutes', message: 'from 0 to 60' },
          { part: 'limits', field: 'triesPerItem', message: 'from 1 to 10' },
          { part: 'version', field: '', message: 'this build reads version 1' },
        ],
        warnings: [{ part: 'steps', field: '[2]', message: 'mind the inbox lists' }],
      }),
    )
    renderBuilder()

    expect(await block('Watch').findByText('Name a user, an org, or a repository.')).toBeInTheDocument()
    expect(block('Watch').getByRole('combobox', { name: 'Where' })).toHaveAttribute('aria-invalid', 'true')
    expect(block('If').getByText('Give at least one value.')).toBeInTheDocument()
    expect(block('Then').getByText('That starter prompt isn’t in your library any more.')).toBeInTheDocument()
    expect(block('Then').getByText('Only if it pushed needs an agent step before it.')).toBeInTheDocument()
    expect(block('Then').getByText('Mind the inbox lists.')).toBeInTheDocument()
    expect(block('When').getByText('From 0 to 60.')).toBeInTheDocument()
    expect(block('Limits').getByText('From 1 to 10.')).toBeInTheDocument()
    expect(screen.getByText('This build reads version 1.')).toBeInTheDocument()
    expect(screen.getByText('When checks fail, as it was saved.')).toBeInTheDocument()
    expect(screen.getByText('Fix the problems above to try it on your items.')).toBeInTheDocument()
    expect(platform.testRoutine).not.toHaveBeenCalled()

    await user.type(screen.getByRole('textbox', { name: 'Name' }), '!')
    expect(await screen.findByText('Fix 7 problems to save.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.clear(screen.getByRole('textbox', { name: 'Name' }))
    expect(screen.getByText('Give the routine a name to save it.')).toBeInTheDocument()
  })

  it('edits the watch in place: its kind, who opened its items, where, and the rest under More', async () => {
    const user = userEvent.setup()
    renderBuilder()

    const watch = block('Watch')

    await user.selectOptions(watch.getByRole('combobox', { name: 'Look for' }), 'issue')
    expect(spec().watch.sources[0]?.search?.kind).toBe('issue')
    await user.selectOptions(watch.getByRole('combobox', { name: 'Look for' }), '')
    expect(spec().watch.sources[0]?.search).not.toHaveProperty('kind')
    await user.selectOptions(watch.getByRole('combobox', { name: 'Look for' }), 'pr')

    // Tokens by keyboard: a suggestion picked with the arrows, and a name typed with a comma.
    await user.click(watch.getByRole('combobox', { name: 'Opened by' }))
    await user.keyboard('renov{ArrowDown}{Enter}')
    expect(spec().watch.sources[0]?.search?.authors).toEqual(['app/dependabot', 'app/renovate'])
    await user.keyboard('{Backspace}')
    expect(spec().watch.sources[0]?.search?.authors).toEqual(['app/dependabot'])

    await user.type(watch.getByRole('combobox', { name: 'Where' }), 'acme-labs,acme-org/api{Enter}')
    expect(spec().watch.sources[0]?.search?.where).toEqual([
      '@me',
      'org:acme-org',
      'org:acme-labs',
      'repo:acme-org/api',
    ])
    await user.click(watch.getByRole('button', { name: 'Remove acme-labs' }))

    await user.click(watch.getByRole('button', { name: 'More' }))
    await user.click(watch.getByRole('checkbox', { name: 'Asking for your review' }))
    expect(spec().watch.sources[0]?.search?.involving).toEqual(['reviewRequested'])
    await user.click(watch.getByRole('checkbox', { name: 'Asking for your review' }))
    expect(spec().watch.sources[0]?.search?.involving).toEqual([])

    await user.type(watch.getByRole('combobox', { name: 'Labeled any of' }), 'depend{ArrowDown}{Enter}')
    await user.type(watch.getByRole('combobox', { name: 'Not labeled' }), 'wontfix{Enter}')
    await user.type(watch.getByRole('textbox', { name: 'Into branch' }), 'main')
    await user.type(watch.getByRole('textbox', { name: 'From a branch starting' }), 'dependabot/')
    await user.click(watch.getByRole('checkbox', { name: 'Not a draft' }))
    await user.click(watch.getByRole('checkbox', { name: 'Checks failing' }))
    await user.click(watch.getByRole('checkbox', { name: 'Not a draft' }))
    await user.type(watch.getByRole('textbox', { name: 'More qualifiers' }), 'language:go')

    expect(spec().watch.sources[0]?.search).toEqual({
      kind: 'pr',
      authors: ['app/dependabot'],
      where: ['@me', 'org:acme-org', 'repo:acme-org/api'],
      involving: [],
      labels: ['dependencies'],
      notLabels: ['wontfix'],
      base: 'main',
      headPrefix: 'dependabot/',
      state: ['failing'],
      more: 'language:go',
    })

    await user.click(watch.getByRole('button', { name: 'More' }))
    expect(watch.queryByRole('textbox', { name: 'More qualifiers' })).not.toBeInTheDocument()
  })

  it('watches inbox lists and several searches, and takes a source away', async () => {
    const user = userEvent.setup()
    renderBuilder()

    const watch = block('Watch')

    expect(watch.queryByRole('button', { name: 'Remove source 1' })).not.toBeInTheDocument()

    await user.click(watch.getByRole('button', { name: 'Watch inbox lists' }))
    expect(spec().watch.sources[1]).toEqual({ lists: ['reviewRequested'] })
    expect(watch.queryByRole('button', { name: 'Watch inbox lists' })).not.toBeInTheDocument()

    await user.click(watch.getByRole('checkbox', { name: 'Authored' }))
    await user.click(watch.getByRole('checkbox', { name: 'Review requested' }))
    expect(spec().watch.sources[1]).toEqual({ lists: ['authored'] })

    await user.click(watch.getByRole('button', { name: 'Add a search' }))
    expect(spec().watch.sources[2]).toEqual({ search: { kind: 'pr', where: ['@me'] } })

    // A kind with a search's More open shows no branch for issues.
    await user.selectOptions(watch.getAllByRole('combobox', { name: 'Look for' })[1] as HTMLElement, 'issue')
    await user.click(watch.getAllByRole('button', { name: 'More' })[1] as HTMLElement)
    expect(watch.queryAllByRole('textbox', { name: 'Into branch' })).toHaveLength(0)
    expect(watch.getByRole('textbox', { name: 'More qualifiers' })).toBeInTheDocument()

    await user.click(watch.getByRole('button', { name: 'Remove source 2' }))
    expect(spec().watch.sources.map((s) => Object.keys(s)[0])).toEqual(['search', 'search'])

    for (let i = 0; i < 3; i++) {
      await user.click(watch.getByRole('button', { name: 'Add a search' }))
    }

    expect(watch.queryByRole('button', { name: 'Add a search' })).not.toBeInTheDocument()
  })

  it('shows what a test found of the searches: an error, and its warnings', async () => {
    platform.testRoutine.mockResolvedValue(
      routineTest({
        searches: [
          {
            query: 'a',
            count: 340,
            returned: 100,
            truncated: true,
            error: '',
            warnings: ['This search matches 340 items; only the newest 100 are checked.'],
          },
          { query: 'b', count: 0, returned: 0, truncated: false, error: 'GitHub refused it', warnings: [] },
        ],
        estimate: { requests: 0, pointsPerRefresh: 0, pointsPerHour: 0, percent: 0 },
      }),
    )
    renderBuilder()

    const watch = block('Watch')

    expect(
      await watch.findByText('Search 1: This search matches 340 items; only the newest 100 are checked.'),
    ).toBeInTheDocument()
    expect(watch.getByText('Search 2: GitHub refused it.')).toBeInTheDocument()
    expect(watch.getByText('Nothing more a refresh: the inbox reads its lists already.')).toBeInTheDocument()
  })

  it('changes the trigger and its options, each at its default until set', async () => {
    const user = userEvent.setup()
    renderBuilder()

    const when = block('When')
    const trigger = when.getByRole('combobox', { name: 'Trigger' })

    expect(
      when.getByText('Fires when the item has a failed or errored check on its head commit, once per head commit.'),
    ).toBeInTheDocument()

    await user.click(when.getByRole('checkbox', { name: 'Wait until every check has finished' }))
    expect(spec().trigger).toEqual({ event: 'checks_failed', waitForChecks: false })
    await user.click(when.getByRole('checkbox', { name: 'Wait until every check has finished' }))
    expect(spec().trigger).toEqual({ event: 'checks_failed' })

    await user.type(when.getByRole('spinbutton', { name: 'Settle for' }), '5')
    expect(spec().trigger.settleMinutes).toBe(5)
    await user.click(when.getByRole('button', { name: 'Reset settle for' }))
    expect(spec().trigger).not.toHaveProperty('settleMinutes')

    await user.selectOptions(trigger, 'labeled')
    await user.type(when.getByRole('combobox', { name: 'Label' }), 'security')
    expect(spec().trigger).toEqual({ event: 'labeled', label: 'security' })

    await user.selectOptions(trigger, 'new_comments')
    await user.type(when.getByRole('spinbutton', { name: 'Comments' }), '3')
    expect(spec().trigger).toEqual({ event: 'new_comments', count: 3 })
    await user.clear(when.getByRole('spinbutton', { name: 'Comments' }))
    expect(spec().trigger).toEqual({ event: 'new_comments' })

    await user.selectOptions(trigger, 'schedule')
    await user.selectOptions(when.getByRole('combobox', { name: 'How often' }), 'week')
    expect(spec().trigger.schedule).toEqual({ every: 'week', at: '09:00', on: 'mon' })
    await user.selectOptions(when.getByRole('combobox', { name: 'On' }), 'fri')
    expect(spec().trigger.schedule?.on).toBe('fri')
    await user.selectOptions(when.getByRole('combobox', { name: 'How often' }), 'weekday')
    expect(spec().trigger.schedule).toEqual({ every: 'weekday', at: '09:00' })

    const time = when.getByLabelText('at')

    await user.clear(time)
    await user.type(time, '07:30')
    expect(spec().trigger.schedule?.at).toBe('07:30')
  })

  it('builds the conditions: a field, how it compares, its values, all or any, and one level of groups', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: { ...flagship(), conditions: { match: 'all', of: [] } } })

    const ifs = block('If')

    expect(ifs.getByText('No conditions: every item that fires counts.')).toBeInTheDocument()

    await user.click(ifs.getByRole('button', { name: 'Add a condition' }))
    expect(spec().conditions.of[0]).toEqual({ field: 'item.kind', op: 'oneOf', values: [] })

    // A field of choices takes only its choices, by their labels.
    await user.type(ifs.getByRole('combobox', { name: 'Condition 1: values' }), 'pull{Enter}nothing{Enter}')
    expect(spec().conditions.of[0]?.values).toEqual(['pr'])

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: field' }), 'item.title')
    expect(spec().conditions.of[0]).toEqual({ field: 'item.title', op: 'is', value: '' })
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: how it compares' }), 'matchesRegex')
    expect(ifs.getByText('A Go (RE2) regular expression, ignoring case.')).toBeInTheDocument()
    await user.type(ifs.getByRole('textbox', { name: 'Condition 1: value' }), '^Bump')
    expect(spec().conditions.of[0]).toEqual({ field: 'item.title', op: 'matchesRegex', value: '^Bump' })

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: field' }), 'item.age')
    await user.type(ifs.getByRole('spinbutton', { name: 'Condition 1: how long' }), '14')
    expect(spec().conditions.of[0]?.value).toBe('14d')
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: unit' }), 'h')
    expect(spec().conditions.of[0]?.value).toBe('14h')
    await user.clear(ifs.getByRole('spinbutton', { name: 'Condition 1: how long' }))
    expect(spec().conditions.of[0]?.value).toBe('')
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: unit' }), 'm')
    expect(spec().conditions.of[0]?.value).toBe('1m')

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: field' }), 'pr.lines')
    await user.type(ifs.getByRole('spinbutton', { name: 'Condition 1: value' }), '400')
    expect(spec().conditions.of[0]).toEqual({ field: 'pr.lines', op: 'eq', value: '400' })

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: field' }), 'checks.failedNames')
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: how it compares' }), 'anyContains')
    await user.type(ifs.getByRole('combobox', { name: 'Condition 1: values' }), 'codeql{ArrowDown}{Enter}')
    expect(spec().conditions.of[0]).toEqual({
      field: 'checks.failedNames',
      op: 'anyContains',
      values: ['CodeQL / Analyze'],
    })
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: how it compares' }), 'isEmpty')
    expect(spec().conditions.of[0]).toEqual({ field: 'checks.failedNames', op: 'isEmpty' })

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Condition 1: field' }), 'item.author')
    expect(ifs.getByRole('combobox', { name: 'Condition 1: value' })).toHaveAttribute('list')

    await user.selectOptions(ifs.getByRole('combobox', { name: 'Match' }), 'any')
    expect(spec().conditions.match).toBe('any')
    await user.selectOptions(ifs.getByRole('combobox', { name: 'Match' }), 'all')

    await user.click(ifs.getByRole('button', { name: 'Add a group' }))
    expect(spec().conditions.of[1]).toEqual({
      group: { match: 'any', of: [{ field: 'item.kind', op: 'oneOf', values: [] }] },
    })

    const group = within(
      ifs.getByRole('combobox', { name: 'Match in the group' }).closest('div.rounded-md') as HTMLElement,
    )

    await user.selectOptions(group.getByRole('combobox', { name: 'Match in the group' }), 'all')
    await user.click(group.getByRole('button', { name: 'Add a condition' }))
    expect(spec().conditions.of[1]?.group?.of).toHaveLength(2)
    expect(group.queryByRole('button', { name: 'Add a group' })).not.toBeInTheDocument()
    await user.click(group.getByRole('button', { name: 'Remove condition 2' }))
    expect(spec().conditions.of[1]?.group).toEqual({
      match: 'all',
      of: [{ field: 'item.kind', op: 'oneOf', values: [] }],
    })
    await user.click(group.getByRole('button', { name: 'Remove the group' }))
    expect(spec().conditions.of).toHaveLength(1)

    await user.click(ifs.getByRole('button', { name: 'Remove condition 1' }))
    expect(spec().conditions.of).toEqual([])
  })

  it('shows a field it doesn’t list as itself, so its problem shows where it is', () => {
    renderBuilder({
      spec: {
        ...flagship(),
        conditions: { match: 'all', of: [{ field: 'item.gone' as 'item.kind', op: 'is', value: 'x' }] },
      },
    })

    expect(block('If').getByRole('combobox', { name: 'Condition 1: field' })).toHaveValue('item.gone')
  })

  it('adds steps that fit where they sit, orders them, runs each only if, and takes one away', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: { ...flagship(), steps: [{ notify: {} }] } })

    const then = block('Then')

    await user.click(then.getByRole('button', { name: 'Add a step' }))
    expect(screen.getByRole('menuitem', { name: /Run a coding agent/ })).toHaveTextContent('Dry run for now')
    expect(screen.queryByRole('menuitem', { name: /Wait for the checks/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: /Run a coding agent/ }))
    expect(spec().steps[1]).toEqual({ agent: { agent: 'inherit', prompt: { itemDefault: true } } })

    await user.click(then.getByRole('button', { name: 'Add a step' }))
    expect(screen.queryByRole('menuitem', { name: /Run a coding agent/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: /Wait for the checks/ }))
    expect(spec().steps[2]).toEqual({ waitForChecks: {}, onlyIf: 'pushed' })

    await user.click(then.getByRole('button', { name: 'Add a step' }))
    await user.click(screen.getByRole('menuitem', { name: /Merge it/ }))
    expect(spec().steps[3]).toEqual({ merge: { how: 'autoMergeIfItWasOn' }, onlyIf: 'passed' })
    expect(then.getByRole('combobox', { name: 'Step 4 runs' })).toHaveValue('passed')

    await user.click(then.getByRole('button', { name: 'Add a step' }))
    await user.click(screen.getByRole('menuitem', { name: /Tell me/ }))
    expect(spec().steps[4]).toEqual({ tellMe: { when: 'verdict' } })

    await user.selectOptions(then.getByRole('combobox', { name: 'Step 5 runs' }), 'stillFails')
    expect(spec().steps[4]?.onlyIf).toBe('stillFails')
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 5 runs' }), '')
    expect(spec().steps[4]).not.toHaveProperty('onlyIf')

    expect(then.getByRole('button', { name: 'Move step 1 up' })).toBeDisabled()
    expect(then.getByRole('button', { name: 'Move step 5 down' })).toBeDisabled()
    await user.click(then.getByRole('button', { name: 'Move step 1 down' }))
    expect(Object.keys(spec().steps[0] ?? {})).toEqual(['agent'])
    await user.click(then.getByRole('button', { name: 'Move step 2 up' }))
    expect(Object.keys(spec().steps[0] ?? {})).toEqual(['notify'])

    await user.click(then.getByRole('button', { name: 'Remove step 5' }))
    expect(spec().steps).toHaveLength(4)
  })

  it('names an Only if that no longer fits, so its problem shows where it is', () => {
    renderBuilder({ spec: { ...flagship(), steps: [{ notify: {} }, { tellMe: {}, onlyIf: 'passed' }] } })

    expect(block('Then').getByRole('combobox', { name: 'Step 2 runs' })).toHaveValue('passed')
  })

  it('edits each step’s own settings on its line', async () => {
    const user = userEvent.setup()
    renderBuilder({
      spec: {
        ...flagship(),
        steps: [
          { notify: {} },
          { snooze: {} },
          { agent: { agent: 'inherit', prompt: { itemDefault: true }, fence: { tools: 'readOnly' } } },
          { postReview: { as: 'comment' } },
          { tellMe: {} },
          { pin: {} },
        ],
      },
    })

    const then = block('Then')

    await user.type(then.getByRole('textbox', { name: 'Step 1: a note of your own' }), 'Look')
    expect(spec().steps[0]).toEqual({ notify: { note: 'Look' } })

    await user.type(then.getByRole('spinbutton', { name: 'Step 2: days to snooze' }), '7')
    expect(spec().steps[1]).toEqual({ snooze: { days: 7 } })
    await user.click(then.getByRole('button', { name: 'Reset step 2: days to snooze' }))
    expect(spec().steps[1]).toEqual({ snooze: {} })

    await user.selectOptions(then.getByRole('combobox', { name: 'Step 4: post it as' }), 'verdict')
    await user.click(then.getByRole('checkbox', { name: 'Allow approving' }))
    expect(spec().steps[3]).toEqual({ postReview: { as: 'verdict', allowApprove: true } })
    await user.click(then.getByRole('checkbox', { name: 'Allow approving' }))
    expect(spec().steps[3]).toEqual({ postReview: { as: 'verdict' } })
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 4: post it as' }), 'comment')
    expect(spec().steps[3]).toEqual({ postReview: { as: 'comment' } })

    await user.selectOptions(then.getByRole('combobox', { name: 'Step 5: what it tells' }), 'verdict')
    expect(spec().steps[4]).toEqual({ tellMe: { when: 'verdict' } })
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 5: what it tells' }), 'reached')
    expect(spec().steps[4]).toEqual({ tellMe: { when: 'reached' } })

    expect(then.getByText('Pins it in the inbox, marked with the routine that did it.')).toBeInTheDocument()
  })

  it('edits the wait and the merge', async () => {
    const user = userEvent.setup()
    renderBuilder({
      spec: {
        ...flagship(),
        steps: [
          { agent: { agent: 'inherit', prompt: { itemDefault: true } } },
          { waitForChecks: {}, onlyIf: 'pushed' },
          { merge: { how: 'autoMergeIfItWasOn' }, onlyIf: 'passed' },
          { github: [] },
        ],
      },
    })

    const then = block('Then')

    await user.type(then.getByRole('spinbutton', { name: 'Step 2: minutes to settle' }), '4')
    await user.type(then.getByRole('spinbutton', { name: 'Step 2: hours before giving up' }), '3')
    expect(spec().steps[1]).toEqual({ waitForChecks: { settleMinutes: 4, giveUpHours: 3 }, onlyIf: 'pushed' })
    await user.click(then.getByRole('button', { name: 'Reset step 2: minutes to settle' }))
    await user.click(then.getByRole('button', { name: 'Reset step 2: hours before giving up' }))
    expect(spec().steps[1]).toEqual({ waitForChecks: {}, onlyIf: 'pushed' })

    await user.selectOptions(then.getByRole('combobox', { name: 'Step 3: how it merges' }), 'whenMergeable')
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 3: merge method' }), 'squash')
    expect(spec().steps[2]).toEqual({ merge: { how: 'whenMergeable', method: 'squash' }, onlyIf: 'passed' })
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 3: how it merges' }), 'autoMergeIfItWasOn')
    await user.selectOptions(then.getByRole('combobox', { name: 'Step 3: merge method' }), '')
    expect(spec().steps[2]).toEqual({ merge: { how: 'autoMergeIfItWasOn' }, onlyIf: 'passed' })

    expect(then.getByText('GitHub actions')).toBeInTheDocument()
  })

  it('says of a step this version doesn’t know that it doesn’t', () => {
    renderBuilder({ spec: { ...flagship(), steps: [{ onlyIf: 'pushed' }] } })

    expect(block('Then').getByText('A step this version doesn’t know')).toBeInTheDocument()
  })
})

describe('the agent step', () => {
  // agentOnly is a routine whose one step is the agent's.
  function agentOnly(agent: NonNullable<AutopilotSpec['steps'][number]['agent']>): AutopilotSpec {
    return { ...flagship(), steps: [{ agent }] }
  }

  it('picks a library prompt for the item’s kind, and opens the Prompts page', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true } }) })

    const then = block('Then')
    const prompt = then.getByRole('combobox', { name: 'Prompt' })

    expect(
      within(prompt)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['The item’s own default prompt', 'Fix checks', 'Quick look', 'Review', 'Explain'])

    await user.selectOptions(prompt, 'starter:fix-checks')
    expect(spec().steps[0]?.agent?.prompt).toEqual({ starter: 'fix-checks' })
    await user.selectOptions(prompt, 'nickname:Explain')
    expect(spec().steps[0]?.agent?.prompt).toEqual({ nickname: 'Explain' })
    await user.selectOptions(prompt, 'item')
    expect(spec().steps[0]?.agent?.prompt).toEqual({ itemDefault: true })

    await user.click(then.getByRole('button', { name: 'Edit in Prompts' }))
    expect(onEditPrompts).toHaveBeenCalledOnce()
  })

  it('keeps a prompt the library no longer offers, by its name', () => {
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { nickname: 'Gone' } }) })

    expect(block('Then').getByRole('combobox', { name: 'Prompt' })).toHaveDisplayValue('Gone')
  })

  it('runs in the background or in the terminal, with the fence only in the background', async () => {
    const user = userEvent.setup()
    renderBuilder({
      spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true }, fence: { timeoutMinutes: 45 } }),
    })

    const then = block('Then')

    await user.click(then.getByRole('radio', { name: 'In my terminal' }))
    expect(spec().steps[0]?.agent).toEqual({ agent: 'inherit', prompt: { itemDefault: true }, where: 'terminal' })
    expect(then.getByText(/A terminal run is exactly a click’s launch/)).toBeInTheDocument()
    expect(then.queryByRole('spinbutton', { name: 'Time limit' })).not.toBeInTheDocument()

    await user.click(then.getByRole('button', { name: 'Change' }))
    await user.click(within(then.getByRole('radiogroup', { name: 'Plan mode' })).getByRole('radio', { name: 'On' }))
    await user.click(
      within(then.getByRole('radiogroup', { name: 'Start in a worktree' })).getByRole('radio', { name: 'Off' }),
    )
    expect(spec().steps[0]?.agent).toMatchObject({ planMode: true, worktree: false })
    await user.click(
      within(then.getByRole('radiogroup', { name: 'Plan mode' })).getByRole('radio', { name: 'Inherit' }),
    )
    expect(spec().steps[0]?.agent).not.toHaveProperty('planMode')

    await user.click(then.getByRole('radio', { name: 'In the background' }))
    expect(spec().steps[0]?.agent).toEqual({ agent: 'inherit', prompt: { itemDefault: true }, where: 'background' })
    expect(then.getByText('Set by Tools')).toBeInTheDocument()
  })

  it('shows its launch on one line, as a click would, and changes it under Change ▸', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true } }) })

    const then = block('Then')

    expect(then.getByText('As a click would:')).toBeInTheDocument()
    expect(then.getByText(/Claude · default · its default model · default effort/)).toBeInTheDocument()

    await user.click(then.getByRole('button', { name: 'Change' }))
    expect(then.getByText(/Inherit takes each repository’s and org’s own launch defaults first/)).toBeInTheDocument()

    await user.selectOptions(then.getByRole('combobox', { name: 'Profile' }), 'work')
    expect(spec().steps[0]?.agent?.profile).toBe('work')
    expect(then.getByText(/Claude · work · Opus · high/)).toBeInTheDocument()
    expect(then.queryByText('As a click would:')).not.toBeInTheDocument()

    await user.type(then.getByRole('combobox', { name: 'Claude model' }), 'sonnet')
    await user.selectOptions(then.getByRole('combobox', { name: 'Effort' }), 'low')
    expect(spec().steps[0]?.agent).toMatchObject({ model: 'sonnet', effort: 'low' })

    await user.selectOptions(then.getByRole('combobox', { name: 'Agent' }), 'codex')
    expect(spec().steps[0]?.agent).toMatchObject({ agent: 'codex', profile: '', model: '' })
    expect(then.getByRole('combobox', { name: 'Codex model' })).toBeInTheDocument()

    await user.selectOptions(then.getByRole('combobox', { name: 'Agent' }), 'inherit')
    expect(spec().steps[0]?.agent?.agent).toBe('inherit')

    await user.click(then.getByRole('button', { name: 'Change' }))
    expect(then.queryByRole('combobox', { name: 'Agent' })).not.toBeInTheDocument()
  })

  it('keeps a profile this Mac doesn’t have, by its name', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'claude', profile: 'gone', prompt: { itemDefault: true } }) })

    await user.click(block('Then').getByRole('button', { name: 'Change' }))
    expect(block('Then').getByRole('combobox', { name: 'Profile' })).toHaveDisplayValue('gone')
  })

  it('falls back to other profiles, in order, up to three', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true } }) })

    const then = block('Then')

    await user.click(then.getByRole('button', { name: 'Change' }))
    expect(then.getByText('None: a profile out of usage waits.')).toBeInTheDocument()

    await user.click(then.getByRole('button', { name: 'Add a fallback' }))
    await user.type(then.getByRole('combobox', { name: 'Fallback 1: profile' }), 'work')
    await user.selectOptions(then.getByRole('combobox', { name: 'Fallback 1: agent' }), 'codex')
    await user.type(then.getByRole('combobox', { name: 'Fallback 1: profile' }), 'default')
    expect(spec().steps[0]?.agent?.fallbacks).toEqual([{ agent: 'codex', profile: 'default' }])

    await user.click(then.getByRole('button', { name: 'Add a fallback' }))
    await user.click(then.getByRole('button', { name: 'Add a fallback' }))
    expect(then.queryByRole('button', { name: 'Add a fallback' })).not.toBeInTheDocument()

    for (const n of [3, 2, 1]) {
      await user.click(then.getByRole('button', { name: `Remove fallback ${String(n)}` }))
    }

    expect(spec().steps[0]?.agent).not.toHaveProperty('fallbacks')
  })

  it('sets the launch for one org or repository at a time', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true } }) })

    const then = block('Then')

    await user.click(then.getByRole('button', { name: 'Per org' }))
    await user.click(then.getByRole('button', { name: 'Add an org or repository' }))
    await user.type(then.getByRole('combobox', { name: 'Row 1: org or repository' }), 'acme-org')
    await user.selectOptions(then.getByRole('combobox', { name: 'Row 1: agent' }), 'claude')
    await user.type(then.getByRole('textbox', { name: 'Row 1: profile' }), 'work')
    await user.type(then.getByRole('textbox', { name: 'Row 1: model' }), 'opus')
    await user.selectOptions(then.getByRole('combobox', { name: 'Row 1: effort' }), 'high')
    expect(spec().steps[0]?.agent?.launchFor).toEqual([
      { target: 'acme-org', agent: 'claude', profile: 'work', model: 'opus', effort: 'high' },
    ])

    await user.click(then.getByRole('button', { name: 'Remove row 1' }))
    expect(spec().steps[0]?.agent).not.toHaveProperty('launchFor')
  })

  it('fences a background run: its budget in dollars or tokens, its time, and its tools', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true } }) })

    const then = block('Then')

    expect(then.getByText('default $3.00')).toBeInTheDocument()
    await user.type(then.getByRole('spinbutton', { name: 'Budget per run' }), '5')
    expect(spec().steps[0]?.agent?.fence).toEqual({ budget: { usd: 5 } })
    await user.selectOptions(then.getByRole('combobox', { name: 'Budget in' }), 'tokens')
    expect(spec().steps[0]?.agent?.fence).toEqual({ budget: { tokens: 200000 } })
    await user.clear(then.getByRole('spinbutton', { name: 'Budget per run' }))
    expect(spec().steps[0]?.agent).not.toHaveProperty('fence')
    await user.selectOptions(then.getByRole('combobox', { name: 'Budget in' }), 'tokens')
    await user.type(then.getByRole('spinbutton', { name: 'Budget per run' }), '0')
    expect(spec().steps[0]?.agent?.fence).toEqual({ budget: { tokens: 2000000 } })
    await user.selectOptions(then.getByRole('combobox', { name: 'Budget in' }), 'usd')
    await user.click(then.getByRole('button', { name: 'Reset the budget' }))
    expect(spec().steps[0]?.agent).not.toHaveProperty('fence')

    await user.type(then.getByRole('spinbutton', { name: 'Time limit' }), '45')
    expect(spec().steps[0]?.agent?.fence).toEqual({ timeoutMinutes: 45 })
    await user.click(then.getByRole('button', { name: 'Reset time limit' }))

    await user.selectOptions(then.getByRole('combobox', { name: 'Tools' }), 'readOnly')
    expect(spec().steps[0]?.agent?.fence).toEqual({ tools: 'readOnly' })
    await user.click(then.getByRole('button', { name: 'Reset the tools' }))
    expect(spec().steps[0]?.agent).not.toHaveProperty('fence')
  })

  it('shows what is wrong inside it, and its warnings', async () => {
    platform.describeRoutine.mockResolvedValue(
      described({
        problems: [{ part: 'steps', field: '[0].agent.model', message: 'opus-x isn’t one of its models' }],
        warnings: [{ part: 'steps', field: '[0].agent.where', message: 'Ask first suits a terminal run better' }],
      }),
    )
    const user = userEvent.setup()
    renderBuilder({ spec: agentOnly({ agent: 'inherit', prompt: { itemDefault: true }, model: 'opus-x' }) })

    const then = block('Then')

    expect(await then.findByText('Opus-x isn’t one of its models.')).toBeInTheDocument()
    expect(then.getByText('Ask first suits a terminal run better.')).toBeInTheDocument()

    await user.click(then.getByRole('button', { name: 'Change' }))
    expect(then.getByRole('combobox', { name: 'Claude model' })).toHaveAttribute('aria-invalid', 'true')
  })
})

describe('the limits', () => {
  it('take their defaults until set, and Reset puts one back', async () => {
    const user = userEvent.setup()
    renderBuilder({ spec: { ...flagship(), limits: {} } })

    const limits = block('Limits')

    expect(limits.getByRole('combobox', { name: 'Once per' })).toHaveDisplayValue('head commit (the trigger’s)')
    await user.selectOptions(limits.getByRole('combobox', { name: 'Once per' }), 'day')
    expect(spec().limits).toEqual({ oncePer: 'day' })
    await user.click(limits.getByRole('button', { name: 'Reset once per' }))
    expect(spec().limits).toEqual({})
    await user.selectOptions(limits.getByRole('combobox', { name: 'Once per' }), 'item')
    await user.selectOptions(limits.getByRole('combobox', { name: 'Once per' }), '')
    expect(spec().limits).toEqual({})

    await user.type(limits.getByRole('spinbutton', { name: 'Tries per item' }), '3')
    await user.type(limits.getByRole('spinbutton', { name: 'Runs a day' }), '5')
    await user.type(limits.getByRole('spinbutton', { name: 'Minutes between tries' }), '0')
    expect(spec().limits).toEqual({ triesPerItem: 3, runsPerDay: 5, waitMinutes: 0 })
    await user.click(limits.getByRole('button', { name: 'Reset tries per item' }))
    expect(spec().limits).toEqual({ runsPerDay: 5, waitMinutes: 0 })
  })
})

describe('the Test panel', () => {
  it('lists every item it watches, matches first, each with where it stands and why', async () => {
    const user = userEvent.setup()
    platform.testRoutine.mockResolvedValue(
      routineTest({
        items: [
          testItem({ record: 'It already ran on this.' }),
          testItem({ id: 3, number: 8, title: 'Bump vite', match: 'yes' }),
          testItem({
            id: 4,
            number: 9,
            title: 'Bump zod',
            match: 'unknown',
            needs: ['checks', 'other' as 'checks'],
            trace: [],
          }),
          testItem({ id: 5, number: 10, title: 'Bump react', match: 'unknown', needs: [] }),
          testItem({ id: 6, number: 11, title: 'Bump left', leftAlone: true }),
          testItem({
            id: 7,
            number: 46,
            title: 'Bump next',
            match: 'no',
            trace: [
              {
                path: 'conditions.of[0]',
                condition: 'a failed check’s name contains security',
                result: 'no',
                actual: '',
              },
            ],
          }),
        ],
        matched: 3,
        unknown: 2,
        watched: 6,
      }),
    )
    renderBuilder()

    const rows = within(await screen.findByRole('list', { name: 'Items it watches' }))

    expect(screen.getByText('3 of 6 open pull requests match now; 2 can’t be told yet')).toBeInTheDocument()
    expect(rows.getByText('It already ran on this.')).toBeInTheDocument()
    expect(rows.getByText('would run')).toBeInTheDocument()
    expect(rows.getByText('waiting for data: the check names, other')).toBeInTheDocument()
    expect(rows.getByText('waiting for data')).toBeInTheDocument()
    expect(rows.getByText('left alone: you took back what it did')).toBeInTheDocument()
    expect(rows.getByText('doesn’t match')).toBeInTheDocument()

    await user.click(rows.getByRole('button', { name: 'Why, for acme-org/example-repo #7' }))
    const why = within(rows.getByRole('list', { name: 'Why, for acme-org/example-repo #7' }))

    expect(why.getByText('checks pass')).toBeInTheDocument()
    expect(why.getByText('· every check passed, settled')).toBeInTheDocument()
    expect(why.getAllByText('Matches:')).toHaveLength(2)

    await user.click(rows.getByRole('button', { name: 'Why, for acme-org/example-repo #9' }))
    expect(rows.getByText('Nothing was checked.')).toBeInTheDocument()

    await user.click(rows.getByRole('button', { name: 'Why, for acme-org/example-repo #46' }))
    expect(rows.getByText('a failed check’s name contains security')).toBeInTheDocument()
    expect(rows.getAllByText('Doesn’t match:').length).toBeGreaterThan(0)
    expect(rows.getAllByText('Not known yet:').length).toBeGreaterThan(0)

    await user.click(rows.getByRole('button', { name: 'Why, for acme-org/example-repo #46' }))
    expect(rows.queryByText('a failed check’s name contains security')).not.toBeInTheDocument()
  })

  it('tries the draft again on Test, and says what it spent, a cached search, a low budget, and a failure', async () => {
    const user = userEvent.setup()
    platform.testRoutine
      .mockResolvedValueOnce(routineTest())
      .mockResolvedValueOnce(routineTest({ points: 0, cached: true, lowBudget: true, items: [] }))
    renderBuilder()

    await user.click(await screen.findByRole('button', { name: 'Test (3 points)' }))
    expect(await screen.findByRole('button', { name: 'Test (cached)' })).toHaveAttribute(
      'title',
      expect.stringMatching(/within the minute/),
    )
    expect(screen.getByText(/Your GitHub API budget is low/)).toBeInTheDocument()
    expect(platform.testRoutine).toHaveBeenCalledTimes(2)

    platform.testRoutine.mockRejectedValue(new Error('GitHub couldn’t be asked about the searches.'))
    await user.click(screen.getByRole('button', { name: 'Test (cached)' }))
    expect(await screen.findByText('GitHub couldn’t be asked about the searches.')).toBeInTheDocument()
  })

  it('says what a Dry run would do, and that it is trying', async () => {
    let answer: (r: ReturnType<typeof routineTest>) => void = () => undefined
    platform.testRoutine.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    platform.describeRoutine.mockResolvedValue(described({ dryRunOnly: ['agent'] }))
    renderBuilder({ heldKinds: ['agent'] })

    expect(await screen.findByText('Trying it on your items…')).toBeInTheDocument()
    answer(routineTest())
    expect(await screen.findByText('would run, in Dry run')).toBeInTheDocument()
  })

  it('has not tried a draft it can’t yet read', () => {
    platform.describeRoutine.mockReturnValue(new Promise(() => undefined))
    renderBuilder()

    expect(screen.getByText('Fix the problems above to try it on your items.')).toBeInTheDocument()
  })
})

describe('saving', () => {
  it('saves with ⌘S once the draft has changes and nothing wrong, and discards them', async () => {
    const user = userEvent.setup()
    renderBuilder()

    await screen.findByText('1 of 2 open pull requests matches now')
    await user.keyboard('{Meta>}s{/Meta}')
    expect(onSave).not.toHaveBeenCalled()

    await user.type(screen.getByRole('textbox', { name: 'Name' }), ' now')
    expect(await screen.findByText('Unsaved changes')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
    await user.keyboard('{Meta>}s{/Meta}')
    expect(onSave).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledTimes(2)

    await user.click(screen.getByRole('button', { name: 'Discard' }))
    expect(onDiscard).toHaveBeenCalledOnce()

    // Another key does nothing.
    await user.keyboard('{Meta>}k{/Meta}')
    expect(onSave).toHaveBeenCalledTimes(2)
  })

  it('says it is saving, and why a save failed', () => {
    const { rerender } = render(<Harness saving />)

    expect(screen.getByText('Saving…')).toBeInTheDocument()

    rerender(<Harness saveError="Another routine has that name." />)
    expect(screen.getByRole('alert')).toHaveTextContent('Another routine has that name.')
  })

  it('says when the draft couldn’t be read', async () => {
    platform.describeRoutine.mockRejectedValue(new Error('the database is locked'))
    renderBuilder()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the routine: the database is locked')
  })

  it('makes a new routine in the mode chosen, and says why one with an agent step waits in Dry run', async () => {
    const user = userEvent.setup()
    platform.describeRoutine.mockResolvedValue(described({ sentence: '' }))
    renderBuilder({ isNew: true, name: 'New routine' })

    expect(screen.getByText('The sentence it reads as shows here once it has every part it needs.')).toBeInTheDocument()

    const mode = screen.getByRole('radiogroup', { name: 'Mode' })

    expect(within(mode).getByRole('radio', { name: 'Ask first' })).toBeChecked()
    expect(screen.getByText(/It starts off: switch it on once it’s saved\./)).toBeInTheDocument()
    await user.click(within(mode).getByRole('radio', { name: 'Dry run' }))
    expect(changed.mock.lastCall?.[0].mode).toBe('dryRun')

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Create routine' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Create routine' }))
    expect(onSave).toHaveBeenCalledOnce()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onDiscard).toHaveBeenCalledOnce()
  })

  it('holds a new routine with an agent step in Dry run, and says so in words', async () => {
    platform.describeRoutine.mockResolvedValue(described({ dryRunOnly: ['agent'] }))
    renderBuilder({ isNew: true })

    expect(await screen.findByText('Agent steps run in Dry run until this Mac can run agents.')).toBeInTheDocument()
    expect(
      within(screen.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Dry run' }),
    ).toBeChecked()
    expect(
      within(screen.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Autopilot' }),
    ).toBeDisabled()
  })

  it('says a saved routine that gains an agent step will run in Dry run once saved', async () => {
    platform.describeRoutine.mockResolvedValue(described({ dryRunOnly: ['agent'] }))
    renderBuilder()

    expect(await screen.findByText(/Once saved, it runs in Dry run\. Agent steps run in Dry run/)).toBeInTheDocument()
  })
})
