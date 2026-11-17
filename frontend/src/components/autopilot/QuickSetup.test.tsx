import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi, type Mock } from 'vitest'

import type { AutopilotDraft, AutopilotRoutine, AutopilotTemplate } from '../../platform'
import {
  autopilotCatalog,
  described,
  galleryTemplate,
  readyCheck,
  routine,
  routineReady,
  routineSuggestions,
  routineTest,
  snoozeTemplate,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { QuickSetup } from './QuickSetup'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// flaky is Re-run flaky checks once as the gallery serves it: its checks' names to fill in,
// and a where, in a build that runs it.
function flaky(): AutopilotTemplate {
  return galleryTemplate({
    key: 're-run-flaky-checks-once',
    name: 'Re-run flaky checks once',
    icon: '🔁',
    needs: [],
    needsWords: '',
    blanks: [
      {
        kind: 'checks',
        label: 'Flaky checks',
        help: 'The checks that fail now and then.',
        source: 0,
        condition: 0,
        required: true,
      },
      { kind: 'where', label: 'Where', help: 'Your account, and orgs.', source: 0, condition: 0, required: false },
    ],
    spec: {
      version: 1,
      watch: { sources: [{ search: { kind: 'pr', authors: ['app/dependabot'], where: ['@me'] } }] },
      trigger: { event: 'checks_failed' },
      conditions: { match: 'all', of: [{ field: 'checks.failedNames', op: 'hasAny', values: [] }] },
      steps: [{ agent: { prompt: { starter: 'rerun-failed-jobs' }, where: 'background' } }],
      limits: { oncePer: 'headCommit' },
    },
    prompts: [{ starter: 'rerun-failed-jobs', name: 'Re-run failed jobs', inLibrary: true }],
  })
}

interface Shown {
  onSaved: Mock<(routine: AutopilotRoutine) => void>
  onCustomize: Mock<(draft: AutopilotDraft) => void>
  onCancel: Mock<() => void>
  onPromptsChanged: Mock<() => void>
}

// show renders the quick setup of t, beside a routine of t's name, and returns its calls.
function show(t: AutopilotTemplate): Shown {
  const calls: Shown = {
    onSaved: vi.fn<(routine: AutopilotRoutine) => void>(),
    onCustomize: vi.fn<(draft: AutopilotDraft) => void>(),
    onCancel: vi.fn<() => void>(),
    onPromptsChanged: vi.fn<() => void>(),
  }

  render(
    <QuickSetup
      template={t}
      routines={[{ name: t.name }]}
      defaultMode="ask"
      catalog={autopilotCatalog()}
      suggestions={routineSuggestions({ checks: ['e2e / browser'], owners: ['acme-org'] })}
      viewer="octo-user"
      describeDelay={0}
      {...calls}
    />,
  )

  return calls
}

describe('QuickSetup', () => {
  it('asks only for the template’s blanks, and saves the routine filled in, under a free name, in the default mode', async () => {
    const user = userEvent.setup()
    platform.saveRoutine.mockImplementation((d) =>
      Promise.resolve(routine({ id: 12, name: d.name, mode: d.mode ?? 'ask' })),
    )
    const calls = show(flaky())

    const region = within(screen.getByRole('region', { name: /Re-run flaky checks once/ }))

    expect(region.getAllByRole('combobox')).toHaveLength(2)
    expect(region.getByRole('combobox', { name: 'Flaky checks' })).toBeInTheDocument()
    expect(region.getByRole('combobox', { name: 'Where' })).toBeInTheDocument()
    expect(region.getByText('It’s saved as “Re-run flaky checks once 2”, switched off.')).toBeInTheDocument()
    expect(
      within(region.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Ask first' }),
    ).toBeChecked()

    await user.type(region.getByRole('combobox', { name: 'Flaky checks' }), 'e2e / browser{Enter}')
    await user.type(region.getByRole('combobox', { name: 'Where' }), 'acme-org{Enter}')
    await user.click(within(region.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Autopilot' }))

    const save = region.getByRole('button', { name: 'Save' })

    await waitFor(() => {
      expect(save).toBeEnabled()
    })
    await user.click(save)

    await waitFor(() => {
      expect(calls.onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({
      name: 'Re-run flaky checks once 2',
      mode: 'auto',
      templateKey: 're-run-flaky-checks-once',
      spec: {
        conditions: { of: [{ values: ['e2e / browser'] }] },
        watch: { sources: [{ search: { where: ['@me', 'org:acme-org'] } }] },
      },
    })
  })

  it('shows a blank’s problem where it is, and the rest above Test, keeping Save off', async () => {
    platform.describeRoutine.mockResolvedValue(
      described({
        sentence: '',
        problems: [
          { part: 'conditions', field: 'of[0].values', message: 'add at least one check name' },
          { part: 'limits', field: 'runsPerDay', message: 'keep it to 200 a day' },
        ],
      }),
    )
    show(flaky())

    expect(await screen.findByText('add at least one check name')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'What keeps it from being saved' })).toHaveTextContent(
      'keep it to 200 a day',
    )
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('says plainly what this build can’t run of it yet, keeping Save off, and Customize opens it all the same', async () => {
    const user = userEvent.setup()
    const calls = show(galleryTemplate())

    expect(screen.getByRole('alert')).toHaveTextContent('It needs agents in the background')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getByText(/It starts off: switch it on once it’s saved/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Customize' }))
    expect(calls.onCustomize).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Fix Dependabot security failures 2', templateKey: 'fix-dependabot-security' }),
    )

    await user.click(screen.getByRole('button', { name: 'Back to templates' }))
    expect(calls.onCancel).toHaveBeenCalledOnce()
  })

  it('offers a starter the library lost back, and says why adding it failed', async () => {
    const user = userEvent.setup()
    const lost = galleryTemplate({
      needs: [],
      needsWords: '',
      prompts: [{ starter: 'fix-dependency-security', name: 'Fix dep security', inLibrary: false }],
    })
    const calls = show(lost)

    expect(screen.getByText('It runs “Fix dep security”, which isn’t in your prompts any more.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Add it again' }))
    expect(platform.addStarterPrompt).toHaveBeenCalledWith('fix-dependency-security')
    await waitFor(() => {
      expect(calls.onPromptsChanged).toHaveBeenCalledOnce()
    })

    platform.addStarterPrompt.mockRejectedValueOnce(new Error('The prompt couldn’t be added.'))
    await user.click(screen.getByRole('button', { name: 'Add it again' }))
    expect(await screen.findByText('The prompt couldn’t be added.')).toBeInTheDocument()
  })

  it('says what this Mac lacks for it, with each fix', async () => {
    platform.draftReadiness.mockResolvedValue(
      routineReady({
        ready: false,
        problems: [
          readyCheck({ id: 'agent:claude', label: 'Claude Code', found: 'isn’t installed', fix: '', command: false }),
          readyCheck({
            id: 'profile:claude/default',
            label: 'Profile default',
            found: 'signed out',
            fix: 'claude auth login',
            command: true,
          }),
          readyCheck({
            id: 'banners',
            label: 'Banners',
            found: 'off',
            fix: 'System Settings → Notifications',
            command: false,
          }),
        ],
      }),
    )
    show(snoozeTemplate())

    const lacks = await screen.findByRole('list', { name: 'What this Mac lacks for it' })

    expect(
      within(lacks)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual([
      'Claude Code: isn’t installed',
      'Profile default: signed out · run claude auth login',
      'Banners: off · System Settings → Notifications',
    ])
  })

  it('tests it on the items now, and says why a save failed', async () => {
    const user = userEvent.setup()
    platform.saveRoutine.mockRejectedValue(new Error('Another routine has that name.'))
    show(snoozeTemplate())

    const test = within(screen.getByRole('region', { name: 'Test' }))
    const run = test.getByRole('button', { name: /Test/ })

    await waitFor(() => {
      expect(run).toBeEnabled()
    })
    await user.click(run)

    expect(platform.testRoutine).toHaveBeenCalledOnce()
    expect(await test.findByRole('list', { name: 'Items it watches' })).toBeInTheDocument()
    expect(routineTest().items.length).toBeGreaterThan(0)

    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Another routine has that name.')).toBeInTheDocument()
  })
})
