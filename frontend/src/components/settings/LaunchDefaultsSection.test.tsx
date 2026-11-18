import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { DefaultPrompts, LaunchDefaults } from '../../platform'
import { agents, autopilotOff, defaultPrompts, launchDefaults, profile, prompts, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { LaunchDefaultsSection } from './LaunchDefaultsSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// nth returns the i-th element of a query's results, failing when there is none.
function nth(list: HTMLElement[], i: number): HTMLElement {
  const el = list[i]

  if (el === undefined) {
    throw new Error(`no element ${String(i)}`)
  }

  return el
}

function renderSection(
  stored: LaunchDefaults[] = [],
  onChanged = vi.fn(() => Promise.resolve()),
  app: DefaultPrompts = defaultPrompts(),
) {
  render(
    <LaunchDefaultsSection
      settings={settings({ worktreeDefault: true })}
      agents={agents()}
      profiles={[
        profile(),
        profile({ name: 'work' }),
        profile({ agent: 'codex' }),
        profile({ agent: 'codex', name: 'alt' }),
      ]}
      prompts={prompts()}
      defaultPrompts={app}
      launchDefaults={stored}
      onChanged={onChanged}
    />,
  )

  return { onChanged }
}

beforeEach(() => {
  platform.listLaunchTargets.mockResolvedValue([
    { owner: 'acme-org', name: '', items: 3, cloned: true },
    { owner: 'acme-org', name: 'api', items: 2, cloned: false },
    { owner: 'acme-org', name: 'example-repo', items: 0, cloned: true },
  ])
})

describe('LaunchDefaultsSection', () => {
  it('says launches use the app’s settings when nothing is saved', () => {
    renderSection()

    expect(screen.getByText(/No launch defaults yet/)).toBeInTheDocument()
  })

  it('adds an org’s defaults, showing what each setting inherits', async () => {
    const { onChanged } = renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))

    expect(form.getByRole('option', { name: 'Inherit · Claude (app)' })).toBeInTheDocument()
    expect(form.getAllByRole('option', { name: 'Inherit · default (app)' })).toHaveLength(2)
    expect(form.getByRole('option', { name: 'Inherit · Review (app)' })).toBeInTheDocument()
    expect(form.getByRole('option', { name: 'Inherit · Work on it (app)' })).toBeInTheDocument()
    expect(form.getByText('Inherits on (app)')).toBeInTheDocument()
    expect(form.getByRole('button', { name: 'Save defaults' })).toBeDisabled()

    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Claude profile' }), 'work')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Codex profile' }), 'alt')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt for pull requests' }), '2')
    await userEvent.click(
      within(form.getByRole('radiogroup', { name: 'Start in a worktree' })).getByRole('radio', { name: 'Off' }),
    )
    await userEvent.type(form.getByRole('textbox', { name: 'Claude extra flags' }), '--advisor fable')
    await userEvent.click(within(form.getByRole('radiogroup', { name: 'Agent' })).getByRole('radio', { name: 'Codex' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Codex model' }), 'gpt-6-sol')

    await waitFor(() => {
      expect(platform.checkLaunchDefaults).toHaveBeenLastCalledWith(
        expect.objectContaining({ owner: 'acme-org', extraFlags: { claude: '--advisor fable' } }),
      )
    })
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith({
      owner: 'acme-org',
      name: '',
      agent: '',
      profiles: { claude: 'work', codex: 'alt' },
      prPromptId: 2,
      issuePromptId: null,
      checksPromptId: null,
      conflictsPromptId: null,
      changesPromptId: null,
      rereviewPromptId: null,
      models: { codex: 'gpt-6-sol' },
      effort: '',
      planMode: null,
      worktree: false,
      extraFlags: { claude: '--advisor fable' },
      autopilotOutside: '',
    })
    expect(onChanged).toHaveBeenCalledOnce()
    expect(await screen.findByText('Saved the defaults for acme-org.')).toBeInTheDocument()
  })

  it('offers the inbox’s and the clones’ orgs and repositories', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const targets = 'datalist[id$="-targets"]'

    await waitFor(() => {
      expect(document.querySelectorAll(`${targets} option`)).toHaveLength(3)
    })
    expect(document.querySelector(`${targets} option[value="acme-org"]`)).toHaveTextContent('All repositories')
    expect(document.querySelector(`${targets} option[value="acme-org/api"]`)).toHaveTextContent('2 in the inbox')
    expect(document.querySelector(`${targets} option[value="acme-org/example-repo"]`)).toHaveTextContent('Local clone')
  })

  it('offers only what has no defaults yet', async () => {
    renderSection([launchDefaults({ owner: 'Acme-Org' }), launchDefaults({ name: 'Example-Repo' })])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const targets = 'datalist[id$="-targets"]'

    await waitFor(() => {
      expect(document.querySelectorAll(`${targets} option`)).toHaveLength(1)
    })
    expect(document.querySelector(`${targets} option[value="acme-org/api"]`)).toHaveTextContent('2 in the inbox')
  })

  it('shows a repository what it inherits from its org', async () => {
    renderSection([launchDefaults({ prPromptId: 2, worktree: false, models: { claude: 'opus' } })])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org/api')

    expect(form.getByRole('option', { name: 'Inherit · work (org)' })).toBeInTheDocument()
    expect(form.getByRole('option', { name: 'Inherit · Quick look (org)' })).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Claude model' })).toHaveAttribute('placeholder', 'Inherit · Opus (org)')
    expect(form.getByText('Inherits off (org)')).toBeInTheDocument()
  })

  it('shows what each situation’s prompt inherits: the org’s, else the app’s, else the pull request’s', async () => {
    const { onChanged } = renderSection(
      [launchDefaults({ conflictsPromptId: 2 })],
      undefined,
      defaultPrompts({ checksFailing: 3, conflicts: 3 }),
    )

    // The org's own list names its prompt for the situation it sets.
    expect(screen.getByText('Quick look when it has conflicts')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))

    const org = within(screen.getByRole('form', { name: 'Launch defaults for acme-org' }))
    const inherits = (field: string) =>
      within(org.getByRole('combobox', { name: field })).getAllByRole('option')[0]?.textContent

    expect(org.getByRole('combobox', { name: 'Prompt when it has conflicts' })).toHaveValue('2')
    expect(inherits('Prompt when it has conflicts')).toBe('Inherit · Fix checks (app)')
    expect(inherits('Prompt when checks fail')).toBe('Inherit · Fix checks (app)')
    expect(inherits('Prompt when changes are requested')).toBe('Inherit · same as pull requests')

    await userEvent.click(org.getByRole('button', { name: 'Cancel' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org/api')

    const repoInherits = (field: string) =>
      within(form.getByRole('combobox', { name: field })).getAllByRole('option')[0]?.textContent

    expect(repoInherits('Prompt when checks fail')).toBe('Inherit · Fix checks (app)')
    expect(repoInherits('Prompt when it has conflicts')).toBe('Inherit · Quick look (org)')
    expect(repoInherits('Prompt when changes are requested')).toBe('Inherit · same as pull requests')
    expect(repoInherits('Prompt when you’re asked to review again')).toBe('Inherit · same as pull requests')
    // Each offers the prompts for pull requests.
    expect(
      within(form.getByRole('combobox', { name: 'Prompt when you’re asked to review again' }))
        .getAllByRole('option')
        .slice(1)
        .map((o) => o.textContent),
    ).toEqual(['Fix checks', 'Quick look', 'Review', 'Explain'])

    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt when you’re asked to review again' }), '2')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt when checks fail' }), '1')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt when checks fail' }), '0')
    await waitFor(() => {
      expect(platform.checkLaunchDefaults).toHaveBeenLastCalledWith(
        expect.objectContaining({ name: 'api', rereviewPromptId: 2 }),
      )
    })
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        owner: 'acme-org',
        name: 'api',
        prPromptId: null,
        checksPromptId: null,
        conflictsPromptId: null,
        changesPromptId: null,
        rereviewPromptId: 2,
      }),
    )
    expect(onChanged).toHaveBeenCalledOnce()
  })

  it('chooses the agent, and puts an agent’s profile back to inherit', async () => {
    renderSection([launchDefaults({ agent: 'codex', issuePromptId: 4 })])

    const rows = within(screen.getByRole('list', { name: 'Launch defaults' }))
    expect(rows.getByText('starts Codex')).toBeInTheDocument()
    expect(rows.getByText('Claude profile work')).toBeInTheDocument()
    expect(rows.getByText('Work on it for issues')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org/api')

    expect(form.getByRole('option', { name: 'Inherit · Codex (org)' })).toBeInTheDocument()

    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Agent' }), 'claude')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Claude profile' }), 'default')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Codex profile' }), 'alt')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Claude profile' }), '')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt for issues' }), '5')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt for issues' }), '0')
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: 'acme-org',
        name: 'api',
        agent: 'claude',
        profiles: { codex: 'alt' },
        issuePromptId: null,
      }),
    )
  })

  it('shows the check’s problems and refuses a target that already has defaults', async () => {
    platform.checkLaunchDefaults.mockResolvedValue([
      { field: 'extraFlags', message: '--model is set by the Model setting.' },
    ])
    renderSection([launchDefaults()])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')

    expect(form.getByText('It already has defaults; edit them in the list.')).toBeInTheDocument()

    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), '/api')
    await userEvent.type(form.getByRole('textbox', { name: 'Claude extra flags' }), '--model opus')

    expect(await form.findByText('--model is set by the Model setting.')).toBeInTheDocument()
    expect(form.getByRole('button', { name: 'Save defaults' })).toBeDisabled()

    await userEvent.click(form.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('form')).not.toBeInTheDocument()
  })

  it('describes each select with its problem', async () => {
    platform.checkLaunchDefaults.mockResolvedValue([
      { field: 'profiles.claude', message: 'No profile named gone.' },
      { field: 'prPromptId', message: 'That prompt is for issues.' },
      { field: 'issuePromptId', message: 'That prompt was deleted.' },
      { field: 'checksPromptId', message: 'That prompt is a reply.' },
      { field: 'effort', message: 'Effort must be one of the levels.' },
    ])
    renderSection([])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')

    expect(await form.findByText('No profile named gone.')).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Claude profile' })).toHaveAccessibleDescription('No profile named gone.')
    expect(form.getByRole('combobox', { name: 'Prompt for pull requests' })).toHaveAccessibleDescription(
      'That prompt is for issues.',
    )
    expect(form.getByRole('combobox', { name: 'Prompt for issues' })).toHaveAccessibleDescription(
      'That prompt was deleted.',
    )
    expect(form.getByRole('combobox', { name: 'Prompt when checks fail' })).toHaveAccessibleDescription(
      'That prompt is a reply.',
    )
    expect(form.getByRole('combobox', { name: 'Prompt when it has conflicts' })).not.toHaveAccessibleDescription()
    expect(form.getByRole('combobox', { name: 'Effort' })).toHaveAccessibleDescription(
      'Effort must be one of the levels.',
    )
  })

  it('lets Autopilot onto outside contributors’ items only where it is turned on, saying what Inherit means', async () => {
    renderSection([launchDefaults({ autopilotOutside: 'on' })])

    const rows = within(screen.getByRole('list', { name: 'Launch defaults' }))
    expect(rows.getByText('Autopilot on outside contributors’ items, in plan mode')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    const outside = form.getByRole('radiogroup', { name: 'Autopilot on outside contributors’ items' })
    expect(within(outside).getByRole('radio', { name: 'Inherit' })).toBeChecked()
    expect(outside).toHaveAccessibleDescription(/always in plan mode/)

    // A repository inherits its org's choice.
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org/api')
    expect(form.getByText('Inherits on (org)')).toBeInTheDocument()

    await userEvent.click(within(outside).getByRole('radio', { name: 'Off' }))
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({ owner: 'acme-org', name: 'api', autopilotOutside: 'off' }),
    )
  })

  it('shows no Autopilot choice while Autopilot (beta) is off, and keeps the stored one on a save', async () => {
    render(
      <LaunchDefaultsSection
        settings={autopilotOff({ worktreeDefault: true })}
        agents={agents()}
        profiles={[profile()]}
        prompts={prompts()}
        defaultPrompts={defaultPrompts()}
        launchDefaults={[launchDefaults({ autopilotOutside: 'on' })]}
        onChanged={vi.fn(() => Promise.resolve())}
      />,
    )

    expect(screen.queryByText(/outside contributors/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(
      screen.queryByRole('radiogroup', { name: 'Autopilot on outside contributors’ items' }),
    ).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Save defaults' }))
    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(expect.objectContaining({ autopilotOutside: 'on' }))
  })

  it('says an org’s Inherit is off, since nothing app-wide lets Autopilot onto outside contributors’ items', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')

    expect(form.getByText('Inherits off')).toBeInTheDocument()

    await userEvent.click(
      within(form.getByRole('radiogroup', { name: 'Autopilot on outside contributors’ items' })).getByRole('radio', {
        name: 'On (plan mode only)',
      }),
    )
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({ owner: 'acme-org', name: '', autopilotOutside: 'on' }),
    )
  })

  it('edits and removes stored defaults', async () => {
    const { onChanged } = renderSection([
      launchDefaults({ effort: 'max' }),
      launchDefaults({ name: 'api', profiles: {} }),
    ])

    const rows = within(screen.getByRole('list', { name: 'Launch defaults' }))
    await userEvent.click(nth(rows.getAllByRole('button', { name: 'Edit' }), 0))

    const form = within(screen.getByRole('form', { name: 'Launch defaults for acme-org' }))
    expect(form.getByRole('combobox', { name: 'Effort' })).toHaveValue('max')

    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Effort' }), 'low')
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({ owner: 'acme-org', name: '', effort: 'low' }),
    )
    expect(await screen.findByText('Saved the defaults for acme-org.')).toBeInTheDocument()

    await userEvent.click(
      nth(within(screen.getByRole('list', { name: 'Launch defaults' })).getAllByRole('button', { name: 'Remove' }), 1),
    )

    const dialog = within(screen.getByRole('dialog', { name: 'Remove the defaults for acme-org/api?' }))
    expect(dialog.getByText(/go back to its org's defaults/)).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Remove defaults' }))

    expect(platform.removeLaunchDefaults).toHaveBeenCalledWith('acme-org', 'api')
    await waitFor(() => {
      expect(onChanged).toHaveBeenCalledTimes(2)
    })
    expect(await screen.findByText('Removed the defaults for acme-org/api.')).toBeInTheDocument()
  })

  it('says why defaults could not be removed', async () => {
    platform.removeLaunchDefaults.mockRejectedValue(new Error('database is locked'))
    const { onChanged } = renderSection([launchDefaults()])

    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove defaults' }))

    expect(await screen.findByRole('status')).toHaveTextContent('database is locked')
    expect(onChanged).not.toHaveBeenCalled()
  })

  it('shows a save that failed', async () => {
    platform.saveLaunchDefaults.mockRejectedValue(new Error('database is locked'))
    renderSection([launchDefaults()])

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save defaults' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
  })
})
