import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AutopilotDraft, AutopilotSpec } from '../../../platform'
import {
  agents,
  autopilotCatalog,
  fixtureUUID,
  myScript,
  profile,
  prompts,
  routineSuggestions,
  routineTest,
  testItem,
} from '../../../test/fixtures'
import { platform } from '../../../test/platform'
import { RoutineBuilder } from './RoutineBuilder'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// nudge is a routine On a schedule: every weekday at 08:30, over the Review requested
// list, notify me, then pin it.
function nudge(): AutopilotSpec {
  return {
    version: 1,
    watch: { sources: [{ lists: ['reviewRequested'] }] },
    trigger: { event: 'schedule', schedule: { every: 'weekday', at: '08:30' } },
    conditions: { match: 'all', of: [] },
    steps: [{ notify: {} }, { pin: {} }],
    limits: {},
  }
}

const changed = vi.fn<(d: AutopilotDraft) => void>()

// Harness holds the draft as the tab does, a routine saved as id 9 unless isNew.
function Harness({ spec = nudge(), isNew = false }: { spec?: AutopilotSpec; isNew?: boolean }) {
  const [start] = useState<AutopilotDraft>(() => ({ id: isNew ? 0 : 9, name: 'Weekday nudge', description: '', spec }))
  const [draft, setDraft] = useState<AutopilotDraft>(start)

  return (
    <RoutineBuilder
      draft={draft}
      baseline={start}
      isNew={isNew}
      sentence="Every weekday at 08:30, as it was saved."
      mode="auto"
      heldKinds={[]}
      catalog={autopilotCatalog()}
      suggestions={routineSuggestions()}
      viewer="octo-user"
      agentKit={{
        prompts: prompts(),
        agents: agents(),
        profiles: [profile()],
        defaultAgent: 'claude',
        defaultProfiles: { claude: 'default' },
        onEditPrompts: vi.fn(),
      }}
      saving={false}
      saveError=""
      onChange={(d) => {
        changed(d)
        setDraft(d)
      }}
      onSave={vi.fn()}
      onDiscard={vi.fn()}
      describeDelay={0}
    />
  )
}

// spec is the draft as it last changed.
function spec(): AutopilotSpec {
  const last = changed.mock.lastCall?.[0]

  if (last === undefined) {
    throw new Error('the draft has not changed')
  }

  return last.spec
}

function block(name: 'When' | 'Then') {
  return within(screen.getByRole('region', { name }))
}

describe('a schedule', () => {
  it('tells of each match on its own, or once with the list', async () => {
    const user = userEvent.setup()

    render(<Harness />)

    const when = block('When')
    const style = when.getByRole('combobox', { name: 'How it tells you' })

    expect(style).toHaveValue('each')
    expect(when.getByText(/and tells you of each on its own/)).toBeInTheDocument()
    expect(when.getByText(/A time missed while this Mac slept runs once it wakes, the same day/)).toBeInTheDocument()

    await user.selectOptions(style, 'digest')
    expect(spec().trigger.schedule).toEqual({ every: 'weekday', at: '08:30', style: 'digest' })
    expect(when.getByText(/one digest that lists them/)).toBeInTheDocument()

    await user.selectOptions(style, 'each')
    expect(spec().trigger.schedule).toEqual({ every: 'weekday', at: '08:30' })
  })
})

describe('Run one of my scripts', () => {
  it('picks one of the user’s own scripts and says what it runs', async () => {
    const user = userEvent.setup()

    platform.autopilotMyScripts.mockResolvedValue([
      myScript(),
      myScript({ id: 8, name: 'Tidy', command: 'go mod tidy', asks: true, shows: 'Go repositories' }),
    ])

    render(<Harness />)

    const then = block('Then')

    await user.click(then.getByRole('button', { name: 'Add a step' }))
    await user.click(await screen.findByRole('menuitem', { name: /Run one of my scripts/ }))
    expect(spec().steps[2]).toEqual({ script: { id: 0, name: '' } })

    const picker = await then.findByRole('combobox', { name: 'Step 3: the script it runs' })

    await user.selectOptions(picker, '8')
    expect(spec().steps[2]).toEqual({ script: { uuid: fixtureUUID(708), id: 8, name: 'Tidy' } })
    expect(then.getByText('go mod tidy')).toBeInTheDocument()
    expect(then.getByText(/where it shows in Go repositories/)).toBeInTheDocument()
    expect(then.getByText(/asks before each run: it waits for your OK every time/)).toBeInTheDocument()

    await user.selectOptions(picker, '7')
    expect(spec().steps[2]).toEqual({ script: { uuid: fixtureUUID(707), id: 7, name: 'Lint' } })
    expect(then.queryByText(/asks before each run/)).not.toBeInTheDocument()
  })

  it('keeps a script that is gone listed, and says when there are none', async () => {
    platform.autopilotMyScripts.mockResolvedValue([myScript()])

    const gone = nudge()
    gone.steps = [{ notify: {} }, { script: { id: 99, name: 'Deploy' } }]

    const { unmount } = render(<Harness spec={gone} />)

    expect(await screen.findByRole('option', { name: 'Deploy (no longer one of your scripts)' })).toBeInTheDocument()
    unmount()

    platform.autopilotMyScripts.mockResolvedValue([])

    const fresh = nudge()
    fresh.steps = [{ notify: {} }, { script: { id: 0, name: '' } }]

    render(<Harness spec={fresh} />)

    expect(await screen.findByText(/You have no scripts of your own yet/)).toBeInTheDocument()
  })
})

describe('a script saved under another name', () => {
  it('waits to be picked again, so another script never runs in its place', async () => {
    const user = userEvent.setup()

    // Script 7 here is Lint; the routine picked it as Deploy, as an ID brought from
    // another Mac may name.
    platform.autopilotMyScripts.mockResolvedValue([myScript()])

    const other = nudge()
    other.steps = [{ notify: {} }, { script: { id: 7, name: 'Deploy' } }]

    render(<Harness spec={other} />)

    const picker = await screen.findByRole('combobox', { name: 'Step 2: the script it runs' })

    expect(await within(picker).findByRole('option', { name: 'Deploy (now “Lint”: pick it again)' })).toHaveProperty(
      'selected',
      true,
    )
    expect(screen.queryByText('magex lint')).not.toBeInTheDocument()

    await user.selectOptions(picker, '7')
    expect(spec().steps[1]).toEqual({ script: { uuid: fixtureUUID(707), id: 7, name: 'Lint' } })
    expect(screen.getByText('magex lint')).toBeInTheDocument()
  })

  it('is the routine’s under its UUID, whatever it is called now and whatever its ID', async () => {
    // Lint was renamed, and a restore gave it another ID; the step names it by its UUID.
    platform.autopilotMyScripts.mockResolvedValue([myScript()])

    const renamed = nudge()
    renamed.steps = [{ notify: {} }, { script: { uuid: fixtureUUID(707), id: 3, name: 'Old lint' } }]

    render(<Harness spec={renamed} />)

    const picker = await screen.findByRole('combobox', { name: 'Step 2: the script it runs' })

    expect(await within(picker).findByRole('option', { name: 'Lint' })).toHaveProperty('selected', true)
    expect(within(picker).queryByRole('option', { name: /pick it again/ })).not.toBeInTheDocument()
    expect(within(picker).queryByRole('option', { name: /no longer one of your scripts/ })).not.toBeInTheDocument()
    expect(screen.getByText('magex lint')).toBeInTheDocument()
  })
})

describe('Run on the matches now', () => {
  // matching is a test that found two items the routine would run on now, and one it has a
  // record of.
  function matching() {
    return routineTest({
      items: [
        testItem({ id: 1, number: 7, runsNow: true }),
        testItem({ id: 2, number: 8, title: 'Bump zod', runsNow: true }),
        testItem({ id: 3, number: 9, record: 'It already ran on this.' }),
      ],
      matched: 3,
    })
  }

  // tested runs the routine's Test, once it reads with no problem.
  async function tested(user: ReturnType<typeof userEvent.setup>) {
    const button = screen.getByRole('button', { name: /^Test/ })

    await waitFor(() => {
      expect(button).toBeEnabled()
    })
    await user.click(button)
    await screen.findByRole('list', { name: 'Items it watches' })
  }

  beforeEach(() => {
    platform.testRoutine.mockResolvedValue(matching())
  })

  it('asks with the list first, then runs on exactly those', async () => {
    const user = userEvent.setup()

    render(<Harness />)
    await tested(user)

    await user.click(screen.getByRole('button', { name: 'Run on the matches now…' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Run Weekday nudge on 2 items now?' }))
    const listed = dialog.getByRole('list', { name: 'What it runs on' })

    expect(within(listed).getAllByRole('listitem')).toHaveLength(2)
    expect(within(listed).getByText('Bump zod')).toBeInTheDocument()
    expect(platform.runOnMatches).not.toHaveBeenCalled()

    await user.click(dialog.getByRole('button', { name: 'Run on 2' }))

    expect(platform.runOnMatches).toHaveBeenCalledWith(9, [1, 2])
    expect(await screen.findByText('Running on 2 items.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('cancels with nothing run, and keeps a refusal in the question', async () => {
    const user = userEvent.setup()

    platform.runOnMatches.mockRejectedValue(new Error('Switch the routine on to run it.'))
    render(<Harness />)
    await tested(user)

    await user.click(screen.getByRole('button', { name: 'Run on the matches now…' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(platform.runOnMatches).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Run on the matches now…' }))
    await user.click(screen.getByRole('button', { name: 'Run on 2' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Switch the routine on to run it.')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('is offered only for the routine as saved, and only with something to run on', async () => {
    const user = userEvent.setup()

    const { unmount } = render(<Harness isNew />)

    await tested(user)
    expect(screen.queryByRole('button', { name: 'Run on the matches now…' })).not.toBeInTheDocument()
    unmount()

    platform.testRoutine.mockResolvedValue(routineTest())
    render(<Harness />)
    await tested(user)
    expect(screen.queryByRole('button', { name: 'Run on the matches now…' })).not.toBeInTheDocument()
  })
})
