import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { LaunchDefaults } from '../../platform'
import { launchDefaults, profile, prompts, settings } from '../../test/fixtures'
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

function renderSection(stored: LaunchDefaults[] = [], onChanged = vi.fn(() => Promise.resolve())) {
  render(
    <LaunchDefaultsSection
      settings={settings({ defaultProfile: 'default', worktreeDefault: true })}
      profiles={[profile(), profile({ name: 'work' })]}
      prompts={prompts()}
      defaultPrompts={{ pr: 1, issue: 4 }}
      launchDefaults={stored}
      onChanged={onChanged}
    />,
  )

  return { onChanged }
}

beforeEach(() => {
  platform.listLaunchTargets.mockResolvedValue([
    { owner: 'acme-org', name: '', items: 3 },
    { owner: 'acme-org', name: 'api', items: 2 },
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

    expect(form.getByRole('option', { name: 'Inherit · default (app)' })).toBeInTheDocument()
    expect(form.getByRole('option', { name: 'Inherit · Review (app)' })).toBeInTheDocument()
    expect(form.getByRole('option', { name: 'Inherit · Work on it (app)' })).toBeInTheDocument()
    expect(form.getByText('Inherits on (app)')).toBeInTheDocument()
    expect(form.getByRole('button', { name: 'Save defaults' })).toBeDisabled()

    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Profile' }), 'work')
    await userEvent.selectOptions(form.getByRole('combobox', { name: 'Prompt for pull requests' }), '2')
    await userEvent.click(
      within(form.getByRole('radiogroup', { name: 'Start in a worktree' })).getByRole('radio', { name: 'Off' }),
    )
    await userEvent.type(form.getByRole('textbox', { name: 'Extra flags' }), '--advisor fable')

    await waitFor(() => {
      expect(platform.checkLaunchDefaults).toHaveBeenLastCalledWith(
        expect.objectContaining({ owner: 'acme-org', extraFlags: '--advisor fable' }),
      )
    })
    await userEvent.click(form.getByRole('button', { name: 'Save defaults' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith({
      owner: 'acme-org',
      name: '',
      agent: '',
      profile: 'work',
      prPromptId: 2,
      issuePromptId: null,
      model: '',
      effort: '',
      planMode: null,
      worktree: false,
      extraFlags: '--advisor fable',
    })
    expect(onChanged).toHaveBeenCalledOnce()
    expect(await screen.findByText('Saved the defaults for acme-org.')).toBeInTheDocument()
  })

  it('shows a repository what it inherits from its org', async () => {
    renderSection([launchDefaults({ prPromptId: 2, worktree: false, model: 'opus' })])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org/api')

    expect(form.getByRole('option', { name: 'Inherit · work (org)' })).toBeInTheDocument()
    expect(form.getByRole('option', { name: 'Inherit · Quick look (org)' })).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Model' })).toHaveAttribute('placeholder', 'Inherit · Opus (org)')
    expect(form.getByText('Inherits off (org)')).toBeInTheDocument()
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
    await userEvent.type(form.getByRole('textbox', { name: 'Extra flags' }), '--model opus')

    expect(await form.findByText('--model is set by the Model setting.')).toBeInTheDocument()
    expect(form.getByRole('button', { name: 'Save defaults' })).toBeDisabled()

    await userEvent.click(form.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('form')).not.toBeInTheDocument()
  })

  it('describes each select with its problem', async () => {
    platform.checkLaunchDefaults.mockResolvedValue([
      { field: 'profile', message: 'No profile named gone.' },
      { field: 'prPromptId', message: 'That prompt is for issues.' },
      { field: 'issuePromptId', message: 'That prompt was deleted.' },
      { field: 'effort', message: 'Effort must be one of the levels.' },
    ])
    renderSection([])

    await userEvent.click(screen.getByRole('button', { name: 'Add defaults' }))

    const form = within(screen.getByRole('form', { name: 'New launch defaults' }))
    await userEvent.type(form.getByRole('combobox', { name: 'Defaults for' }), 'acme-org')

    expect(await form.findByText('No profile named gone.')).toBeInTheDocument()
    expect(form.getByRole('combobox', { name: 'Profile' })).toHaveAccessibleDescription('No profile named gone.')
    expect(form.getByRole('combobox', { name: 'Prompt for pull requests' })).toHaveAccessibleDescription(
      'That prompt is for issues.',
    )
    expect(form.getByRole('combobox', { name: 'Prompt for issues' })).toHaveAccessibleDescription(
      'That prompt was deleted.',
    )
    expect(form.getByRole('combobox', { name: 'Effort' })).toHaveAccessibleDescription(
      'Effort must be one of the levels.',
    )
  })

  it('edits and removes stored defaults', async () => {
    const { onChanged } = renderSection([
      launchDefaults({ effort: 'max' }),
      launchDefaults({ name: 'api', profile: '' }),
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
