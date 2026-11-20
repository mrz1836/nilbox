import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { DescribeAgentChoice, DescribePluginState, Profile } from '../../platform'
import { agents, describeAgentChoice, describePluginState, profile } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { DescribeItSettings, installedText, pluginLabel } from './DescribeItSettings'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// claudeProfiles are Claude Code's default profile and a work one whose own model is Opus
// at high effort, and Codex's, which Describe it never runs.
function claudeProfiles(): Profile[] {
  return [
    profile(),
    profile({ name: 'work', folder: '~/.claude-work', model: 'opus', effort: 'high' }),
    profile({ agent: 'codex', name: 'codex-only', folder: '~/.codex' }),
  ]
}

function renderGroup(defaultProfile = 'default') {
  render(<DescribeItSettings defaultProfile={defaultProfile} agents={agents()} profiles={claudeProfiles()} />)
}

// options lists a select's options as they read.
function options(name: string): string[] {
  return within(screen.getByRole('combobox', { name }))
    .getAllByRole('option')
    .map((o) => o.textContent)
}

// deferred is a call that settles when the test says.
function deferred<T>() {
  let settle: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    settle = resolve
  })

  return { promise, settle }
}

// line returns the queries scoped to the plugin's line for a profile.
function line(name: string) {
  const list = screen.getByRole('list', { name: pluginLabel })
  const found = within(list)
    .getAllByRole('listitem')
    .find((li) => li.querySelector('p')?.textContent === name)

  if (found === undefined) {
    throw new Error(`no line for ${name}`)
  }

  return within(found)
}

describe('the agent Describe it starts with', () => {
  it('offers Claude Code’s profiles, models, and efforts, each Inherit with what it comes to', async () => {
    renderGroup()

    const group = within(screen.getByRole('group', { name: 'Describe it’s agent' }))

    await waitFor(() => {
      expect(group.getByRole('combobox', { name: 'Profile' })).toBeEnabled()
    })
    expect(group.getByRole('combobox', { name: 'Profile' })).toHaveValue('')
    expect(options('Profile')).toEqual(['Inherit · default', 'default', 'work'])
    expect(options('Model')).toEqual(['Inherit · Agent default', 'Fable', 'Opus', 'Sonnet', 'Haiku'])
    expect(options('Effort')).toEqual(['Inherit · Agent default', 'Low', 'Medium', 'High', 'Extra high', 'Max'])
    expect(screen.getByRole('combobox', { name: 'Model' })).toHaveAccessibleDescription(
      /The Claude Code profile, model, and effort a new conversation starts with/,
    )
  })

  it('says what Inherit comes to for the launch defaults’ profile, and for the profile chosen', async () => {
    const user = userEvent.setup()
    renderGroup('work')

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Model' })).toBeEnabled()
    })
    expect(options('Profile')[0]).toBe('Inherit · work')
    expect(options('Model')[0]).toBe('Inherit · Opus')
    expect(options('Effort')[0]).toBe('Inherit · High')

    await user.selectOptions(screen.getByRole('combobox', { name: 'Profile' }), 'default')

    expect(options('Model')[0]).toBe('Inherit · Agent default')
    expect(options('Effort')[0]).toBe('Inherit · Agent default')
  })

  it('stores each choice as it is made, and shows the choice as stored', async () => {
    const user = userEvent.setup()
    platform.setDescribeItAgent.mockImplementation((choice: DescribeAgentChoice) =>
      Promise.resolve({ ...choice, effort: choice.effort === 'max' ? 'high' : choice.effort }),
    )
    renderGroup()

    const model = screen.getByRole('combobox', { name: 'Model' })

    await waitFor(() => {
      expect(model).toBeEnabled()
    })
    await user.selectOptions(model, 'opus')

    expect(platform.setDescribeItAgent).toHaveBeenCalledExactlyOnceWith({ profile: '', model: 'opus', effort: '' })
    expect(model).toHaveValue('opus')

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Effort' })).toBeEnabled()
    })
    await user.selectOptions(screen.getByRole('combobox', { name: 'Effort' }), 'max')

    expect(platform.setDescribeItAgent).toHaveBeenLastCalledWith({ profile: '', model: 'opus', effort: 'max' })
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Effort' })).toHaveValue('high')
    })
  })

  it('holds the choices while one is stored', async () => {
    const user = userEvent.setup()
    const saving = deferred<DescribeAgentChoice>()
    platform.setDescribeItAgent.mockReturnValue(saving.promise)
    renderGroup()

    const profileField = screen.getByRole('combobox', { name: 'Profile' })

    await waitFor(() => {
      expect(profileField).toBeEnabled()
    })
    await user.selectOptions(profileField, 'work')

    expect(profileField).toHaveValue('work')

    for (const field of screen.getAllByRole('combobox')) {
      expect(field).toBeDisabled()
    }

    saving.settle(describeAgentChoice({ profile: 'work' }))

    await waitFor(() => {
      expect(profileField).toBeEnabled()
    })
    expect(profileField).toHaveValue('work')
  })

  it('puts a refused choice back, saying why', async () => {
    const user = userEvent.setup()
    platform.setDescribeItAgent.mockRejectedValue(new Error('Claude Code has no profile “work” here.'))
    renderGroup()

    const profileField = screen.getByRole('combobox', { name: 'Profile' })

    await waitFor(() => {
      expect(profileField).toBeEnabled()
    })
    await user.selectOptions(profileField, 'work')

    expect(await screen.findByRole('alert')).toHaveTextContent('Claude Code has no profile “work” here.')
    expect(profileField).toHaveValue('')
    expect(profileField).toHaveAccessibleDescription(/Claude Code has no profile “work” here\.$/)
  })

  it('still shows a stored choice the lists no longer hold', async () => {
    platform.describeItAgent.mockResolvedValue(describeAgentChoice({ profile: 'gone', model: 'claude-next' }))
    renderGroup()

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Profile' })).toHaveValue('gone')
    })
    expect(options('Profile')).toEqual(['Inherit · default', 'default', 'work', 'gone'])
    expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue('claude-next')
    expect(options('Model')).toContain('claude-next')
    // The profile it names is gone, so Inherit comes to the agent's own defaults.
    expect(options('Model')[0]).toBe('Inherit · Agent default')
  })

  it('holds the choices, saying why, when the agent can’t be read', async () => {
    platform.describeItAgent.mockRejectedValue(new Error('Describe it isn’t ready in this build yet.'))
    renderGroup()

    expect(await screen.findByRole('alert')).toHaveTextContent('Describe it isn’t ready in this build yet.')

    for (const field of screen.getAllByRole('combobox')) {
      expect(field).toBeDisabled()
      expect(field).toHaveValue('')
    }
  })
})

describe('the plugin for the user’s own Claude Code', () => {
  it('says what the plugin does, and how much less such a session sees', () => {
    renderGroup()

    expect(screen.getByText(`Use ${brand.name} in your own Claude Code`)).toBeInTheDocument()
    expect(
      screen.getByText(/a session in your own Claude Code can propose routines, prompts, and settings/),
    ).toHaveTextContent(
      /none of your inbox’s names, and of your setup only your routines’ names and sentences, your prompts’ nicknames, and your settings\. It asks before each of its tool calls, since it may run in a repository you don’t trust\./,
    )
    expect(screen.getByText('Reading Claude Code’s plugins…')).toBeInTheDocument()
  })

  it('installs the plugin in a profile and removes it again, showing every profile as it then stands', async () => {
    const user = userEvent.setup()
    platform.describeItPlugins.mockResolvedValue([
      describePluginState(),
      describePluginState({ profile: 'work', default: false }),
    ])
    platform.installDescribeItPlugin.mockResolvedValue([
      describePluginState({ installed: true, version: '1.0.0' }),
      describePluginState({ profile: 'work', default: false }),
    ])
    renderGroup()

    await screen.findByRole('list', { name: pluginLabel })

    expect(line('default').getByText('Not installed')).toBeInTheDocument()
    expect(line('work').getByText('Not installed')).toBeInTheDocument()

    const install = line('default').getByRole('button', { name: 'Install' })

    expect(install).toHaveAccessibleDescription('default Not installed')
    await user.click(install)

    expect(platform.installDescribeItPlugin).toHaveBeenCalledExactlyOnceWith('default')
    expect(await line('default').findByText('Installed · 1.0.0')).toBeInTheDocument()

    await user.click(line('default').getByRole('button', { name: 'Remove' }))

    expect(platform.removeDescribeItPlugin).toHaveBeenCalledExactlyOnceWith('default')
    expect(await line('default').findByText('Not installed')).toBeInTheDocument()
    expect(line('default').getByRole('button', { name: 'Install' })).toBeEnabled()
  })

  it('runs one change at a time, saying which is under way', async () => {
    const user = userEvent.setup()
    const installing = deferred<DescribePluginState[]>()
    platform.describeItPlugins.mockResolvedValue([
      describePluginState(),
      describePluginState({ profile: 'work', default: false, installed: true, version: '1.0.0' }),
    ])
    platform.installDescribeItPlugin.mockReturnValue(installing.promise)
    renderGroup()

    await screen.findByRole('list', { name: pluginLabel })
    await user.click(line('default').getByRole('button', { name: 'Install' }))

    expect(line('default').getByRole('button', { name: 'Installing…' })).toBeDisabled()
    expect(line('work').getByRole('button', { name: 'Remove' })).toBeDisabled()

    installing.settle([
      describePluginState({ installed: true, version: '1.0.0' }),
      describePluginState({ profile: 'work', default: false, installed: true, version: '1.0.0' }),
    ])

    expect(await line('default').findByRole('button', { name: 'Remove' })).toBeEnabled()
    expect(line('work').getByRole('button', { name: 'Remove' })).toBeEnabled()
  })

  it('says why an install was refused, beside its profile, and shows the profile as it was', async () => {
    const user = userEvent.setup()
    platform.installDescribeItPlugin.mockRejectedValue(
      new Error('A build from source can’t install the plugin; install the app first.'),
    )
    renderGroup()

    await screen.findByRole('list', { name: pluginLabel })
    await user.click(line('default').getByRole('button', { name: 'Install' }))

    expect(await line('default').findByRole('alert')).toHaveTextContent(
      'A build from source can’t install the plugin; install the app first.',
    )
    expect(line('default').getByText('Not installed')).toBeInTheDocument()
    expect(line('default').getByRole('button', { name: 'Install' })).toBeEnabled()
  })

  it('holds back an install its profile can’t take, saying why, and still removes an installed one', async () => {
    platform.describeItPlugins.mockResolvedValue([
      describePluginState({ problem: 'Claude Code isn’t signed in for this profile.' }),
      describePluginState({
        profile: 'work',
        default: false,
        installed: true,
        version: '',
        problem: 'Claude Code’s list of its plugins couldn’t be read.',
      }),
    ])
    renderGroup()

    await screen.findByRole('list', { name: pluginLabel })

    expect(line('default').getByText('Claude Code isn’t signed in for this profile.')).toBeInTheDocument()
    expect(line('default').getByRole('button', { name: 'Install' })).toBeDisabled()
    expect(line('work').getByText('Installed')).toBeInTheDocument()
    expect(line('work').getByRole('button', { name: 'Remove' })).toBeEnabled()
  })

  it('says why the plugins couldn’t be read, and when there is no profile to install it in', async () => {
    platform.describeItPlugins.mockRejectedValueOnce(new Error('Describe it isn’t ready in this build yet.'))
    const view = render(<DescribeItSettings defaultProfile="default" agents={agents()} profiles={[]} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Describe it isn’t ready in this build yet.')
    view.unmount()

    // Nor any agent this build knows, so each choice offers Inherit alone.
    platform.describeItPlugins.mockResolvedValue([])
    render(<DescribeItSettings defaultProfile="default" agents={[]} profiles={[]} />)

    expect(await screen.findByText('No Claude Code profile was found.')).toBeInTheDocument()
    expect(options('Profile')).toEqual(['Inherit · default'])
    expect(options('Model')).toEqual(['Inherit · Agent default'])
    expect(options('Effort')).toEqual(['Inherit · Agent default'])
  })
})

describe('installedText', () => {
  it('says whether the plugin is installed, with its version when it is known', () => {
    expect(installedText({ installed: false, version: '' })).toBe('Not installed')
    expect(installedText({ installed: true, version: '' })).toBe('Installed')
    expect(installedText({ installed: true, version: '1.2.0' })).toBe('Installed · 1.2.0')
  })
})
