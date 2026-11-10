import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { Result } from '../lib/call'
import type { Theme } from '../lib/theme'
import type {
  Account,
  AgentInfo,
  LaunchDefaults,
  NotificationKind,
  Profile,
  ProjectAccess,
  Settings,
} from '../platform'
import {
  account,
  agents,
  appInfo,
  defaultPrompts,
  launchDefaults,
  profile,
  prompts,
  settings,
  signedOut,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { SettingsView, type SettingsSection, type SettingsViewProps } from './SettingsView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// A release build, which About describes.
beforeEach(() => {
  platform.getAppInfo.mockResolvedValue(
    appInfo({
      version: 'v1.4.0-2-gabc1234-dirty',
      release: '1.4.0',
      build: '42',
    }),
  )
})

// StoredSettings keeps the settings each save returns and shows the page with them, as App
// does, so a second change starts from the first.
function StoredSettings(props: SettingsViewProps) {
  const { settings: initial, onSaved } = props
  const [current, setCurrent] = useState(initial)

  return (
    <SettingsView
      {...props}
      settings={current}
      onSaved={(next) => {
        onSaved(next)
        setCurrent(next)
      }}
    />
  )
}

interface Options {
  section?: SettingsSection
  agents?: AgentInfo[]
  current?: Settings | null
  profiles?: Profile[]
  who?: Account | null
  access?: ProjectAccess
  defaults?: LaunchDefaults[]
  theme?: Theme
  onThemeChange?: (theme: Theme) => Promise<Result<void>>
}

function renderSettings({
  section,
  agents: coding = agents(),
  current = settings(),
  profiles = [profile(), profile({ name: 'work' })],
  who = account(),
  access = 'on',
  defaults = [],
  theme = 'system',
  onThemeChange = vi.fn(() => Promise.resolve<Result<void>>({ ok: true, data: undefined })),
}: Options = {}) {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  const onLaunchChanged = vi.fn(() => Promise.resolve())

  render(
    <StoredSettings
      {...(section === undefined ? {} : { initialSection: section })}
      settings={current}
      agents={coding}
      profiles={profiles}
      prompts={prompts()}
      defaultPrompts={defaultPrompts()}
      launchDefaults={defaults}
      account={who}
      projectAccess={access}
      theme={theme}
      onThemeChange={onThemeChange}
      onSaved={onSaved}
      onLaunchChanged={onLaunchChanged}
      onClose={onClose}
    />,
  )

  return { onSaved, onClose, onLaunchChanged, onThemeChange }
}

// openSection picks a section from the sidebar.
async function openSection(name: string) {
  await userEvent.click(screen.getByRole('button', { name }))
}

describe('SettingsView', () => {
  it('turns to the section asked for at each opening, and keeps its place in between', async () => {
    const props: SettingsViewProps = {
      settings: settings(),
      agents: agents(),
      profiles: [profile()],
      prompts: prompts(),
      defaultPrompts: defaultPrompts(),
      launchDefaults: [],
      account: account(),
      theme: 'system',
      onThemeChange: () => Promise.resolve({ ok: true, data: undefined }),
      onSaved: vi.fn(),
      onLaunchChanged: () => Promise.resolve(),
      onClose: vi.fn(),
    }
    const { rerender } = render(<SettingsView {...props} initialSection="snoozing" opened={1} />)

    expect(screen.getByRole('region', { name: 'Snoozing' })).toBeInTheDocument()

    await openSection('Trust')
    rerender(<SettingsView {...props} initialSection="snoozing" opened={1} />)
    expect(screen.getByRole('region', { name: 'Trust' })).toBeInTheDocument()

    rerender(<SettingsView {...props} initialSection="snoozing" opened={2} />)
    expect(screen.getByRole('region', { name: 'Snoozing' })).toBeInTheDocument()
  })

  it('opens on the account, with a sidebar of sections', () => {
    renderSettings()

    const nav = screen.getByRole('navigation', { name: 'Settings sections' })
    expect(nav).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('button', { name: 'Launching' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('region', { name: 'Account' })).toBeInTheDocument()
    expect(screen.getByText('Octo User')).toBeInTheDocument()
    expect(screen.getByText('@octo-user')).toBeInTheDocument()
    expect(screen.getByText(/gh auth switch/)).toBeInTheDocument()
  })

  it('opens the GitHub profile in the browser', async () => {
    renderSettings()

    await userEvent.click(screen.getByRole('button', { name: 'View on GitHub' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/octo-user')
  })

  it('shows the login for an account with no name', () => {
    renderSettings({ who: account({ name: '' }) })

    expect(screen.getByText('octo-user')).toBeInTheDocument()
  })

  it('says issue cards show their projects when the token reads them', () => {
    renderSettings()

    const projects = screen.getByRole('region', { name: 'Projects on issues' })
    expect(within(projects).getByText('On')).toBeInTheDocument()
    expect(within(projects).getByText(/Issue cards show each GitHub project an issue is in/)).toBeInTheDocument()
    expect(within(projects).queryByRole('button', { name: 'Copy the command' })).not.toBeInTheDocument()
  })

  it('gives the command that lets the token read projects', async () => {
    renderSettings({ access: 'needs_scope' })

    const projects = screen.getByRole('region', { name: 'Projects on issues' })
    expect(within(projects).getByText('Not set up')).toBeInTheDocument()
    expect(within(projects).getByText('gh auth refresh -s read:project')).toBeInTheDocument()

    await userEvent.click(within(projects).getByRole('button', { name: 'Copy the command' }))
    expect(platform.copyText).toHaveBeenCalledWith('gh auth refresh -s read:project')
  })

  it('says the first refresh checks for projects', () => {
    renderSettings({ access: '' })

    const projects = screen.getByRole('region', { name: 'Projects on issues' })
    expect(within(projects).getByText(/The first refresh checks/)).toBeInTheDocument()
    expect(within(projects).queryByText('On')).not.toBeInTheDocument()
  })

  it('says when no one is signed in yet', () => {
    renderSettings({ who: signedOut })

    expect(screen.getByText('Not signed in yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'View on GitHub' })).not.toBeInTheDocument()
  })

  it('opens at the section asked for', () => {
    renderSettings({ section: 'snoozing' })

    expect(screen.getByRole('button', { name: 'Snoozing' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('region', { name: 'Snoozing' })).toBeInTheDocument()
  })

  it('saves the snooze length as soon as it is chosen', async () => {
    const { onSaved } = renderSettings()
    await openSection('Snoozing')

    const lengths = screen.getByRole('radiogroup', { name: 'Snooze for' })
    expect(lengths).not.toHaveAttribute('aria-describedby')
    expect(within(lengths).getByRole('radio', { name: '3d' })).toBeChecked()
    expect(within(lengths).getAllByRole('radio')).toHaveLength(6)
    expect(screen.getByText(/never comes back on its own/)).toBeInTheDocument()

    await userEvent.click(within(lengths).getByRole('radio', { name: '1w' }))

    expect(platform.setSnoozeDays).toHaveBeenCalledWith(7)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ snoozeDays: 7 }))
    })
  })

  it('saves which activity wakes a snooze early, one rule at a time', async () => {
    const { onSaved } = renderSettings()
    await openSection('Snoozing')

    const commits = screen.getByRole('radiogroup', { name: 'Wake on new commits' })
    const reviews = screen.getByRole('radiogroup', { name: 'Wake on reviews' })
    const comments = screen.getByRole('radiogroup', { name: 'Wake on new comments' })

    expect(within(commits).getByRole('radio', { name: 'Any' })).toBeChecked()
    expect(within(reviews).getByRole('radio', { name: 'Decisions' })).toBeChecked()
    expect(within(comments).getByRole('radio', { name: 'Never' })).toBeChecked()
    expect(
      within(comments)
        .getAllByRole('radio')
        .map((r) => r.closest('label')?.textContent),
    ).toEqual(['Never', 'Any', '3+', '5+', '10+'])
    expect(within(commits).getByRole('radio', { name: '3+' }).closest('label')).toHaveAttribute(
      'title',
      '3 or more new commits',
    )

    await userEvent.click(within(comments).getByRole('radio', { name: '5+' }))

    expect(platform.setSnoozeWake).toHaveBeenCalledWith({ commits: 1, reviews: 'decisions', comments: 5 })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ snoozeWake: { commits: 1, reviews: 'decisions', comments: 5 } }))
    })

    // Each rule changes from the settings the last change stored.
    await userEvent.click(within(reviews).getByRole('radio', { name: 'Never' }))
    expect(platform.setSnoozeWake).toHaveBeenLastCalledWith({ commits: 1, reviews: 'never', comments: 5 })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledTimes(2)
    })

    await userEvent.click(within(commits).getByRole('radio', { name: 'Never' }))
    expect(platform.setSnoozeWake).toHaveBeenLastCalledWith({ commits: 0, reviews: 'never', comments: 5 })
  })

  it('shows wake rules the backend refused', async () => {
    platform.setSnoozeWake.mockRejectedValue(new Error('invalid snooze wake rules'))
    const { onSaved } = renderSettings()
    await openSection('Snoozing')

    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Wake on reviews' })).getByRole('radio', { name: 'Any' }),
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid snooze wake rules')
    expect(onSaved).not.toHaveBeenCalled()
    // Each choice is read with why the last one was refused.
    expect(screen.getByRole('radiogroup', { name: 'Wake on reviews' })).toHaveAccessibleDescription(
      'invalid snooze wake rules',
    )
    expect(screen.getByRole('radiogroup', { name: 'Snooze for' })).toHaveAccessibleDescription(
      'invalid snooze wake rules',
    )
  })

  it('shows a snooze length the backend refused', async () => {
    platform.setSnoozeDays.mockRejectedValue(new Error('invalid snooze length'))
    const { onSaved } = renderSettings()
    await openSection('Snoozing')

    await userEvent.click(screen.getByRole('radio', { name: '2w' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid snooze length')
    expect(onSaved).not.toHaveBeenCalled()
    expect(screen.getByRole('radiogroup', { name: 'Snooze for' })).toHaveAccessibleDescription('invalid snooze length')
  })

  it('waits for the settings before showing the snooze length', async () => {
    renderSettings({ current: null })
    await openSection('Snoozing')

    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()
  })

  it('saves and applies the window size as soon as it is chosen', async () => {
    const { onSaved } = renderSettings()
    await openSection('Appearance')

    const sizes = screen.getByRole('radiogroup', { name: 'Window size' })
    expect(within(sizes).getAllByRole('radio')).toHaveLength(4)
    expect(within(sizes).getByRole('radio', { name: 'Large' })).toBeChecked()
    expect(within(sizes).getByText('Large')).toHaveAttribute('title', '1440 × 900 (default)')
    expect(within(sizes).getByText('Standard')).toHaveAttribute('title', '1280 × 800')
    expect(screen.getByText(/Opens at 1440 × 900\./)).toBeInTheDocument()

    await userEvent.click(within(sizes).getByRole('radio', { name: 'Extra large' }))

    expect(platform.setWindowSize).toHaveBeenCalledWith({ width: 1680, height: 1050 })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ windowSize: { width: 1680, height: 1050 } }))
    })
  })

  it('shows a window size the backend refused', async () => {
    platform.setWindowSize.mockRejectedValue(new Error('invalid window size'))
    const { onSaved } = renderSettings()
    await openSection('Appearance')

    await userEvent.click(screen.getByRole('radio', { name: 'Extra large' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid window size')
    expect(onSaved).not.toHaveBeenCalled()
    expect(screen.getByRole('radiogroup', { name: 'Window size' })).toHaveAccessibleDescription('invalid window size')
  })

  it('selects no preset for a size that matches none', async () => {
    renderSettings({ current: settings({ windowSize: { width: 1366, height: 768 } }) })
    await openSection('Appearance')

    const sizes = screen.getByRole('radiogroup', { name: 'Window size' })
    for (const radio of within(sizes).getAllByRole('radio')) {
      expect(radio).not.toBeChecked()
    }
    expect(screen.getByText(/Opens at 1366 × 768\./)).toBeInTheDocument()
  })

  it('waits for the settings before showing the text and window sizes, but not to show the theme', async () => {
    renderSettings({ current: null })
    await openSection('Appearance')

    expect(screen.getByText('Loading the text and window sizes…')).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup', { name: 'Text size' })).not.toBeInTheDocument()
    expect(screen.queryByRole('radiogroup', { name: 'Window size' })).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'System' })).toBeChecked()
  })

  it('offers the text sizes, smallest first, with the stored one checked', async () => {
    renderSettings({ current: settings({ textSize: 'large' }) })
    await openSection('Appearance')

    const sizes = screen.getByRole('radiogroup', { name: 'Text size' })
    expect(
      within(sizes)
        .getAllByRole('radio')
        .map((r) => r.getAttribute('value')),
    ).toEqual(['default', 'large', 'larger'])
    expect(within(sizes).getByRole('radio', { name: '115%' })).toBeChecked()
    expect(within(sizes).getByText('100%')).toHaveAttribute('title', 'The default size')
    expect(within(sizes).getByText('115%')).toHaveAttribute('title', '15% larger than the default')
    expect(within(sizes).getByText('130%')).toHaveAttribute('title', '30% larger than the default')
  })

  it('saves the text size as soon as it is chosen', async () => {
    const { onSaved } = renderSettings()
    await openSection('Appearance')

    const sizes = screen.getByRole('radiogroup', { name: 'Text size' })
    expect(within(sizes).getByRole('radio', { name: '100%' })).toBeChecked()

    await userEvent.click(within(sizes).getByRole('radio', { name: '130%' }))

    expect(within(sizes).getByRole('radio', { name: '130%' })).toBeChecked()
    expect(platform.setTextSize).toHaveBeenCalledWith('larger')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ textSize: 'larger' }))
    })
  })

  it('shows a text size the backend refused, and goes back to the stored one', async () => {
    platform.setTextSize.mockRejectedValue(new Error('unknown text size'))
    const { onSaved } = renderSettings()
    await openSection('Appearance')

    await userEvent.click(screen.getByRole('radio', { name: '115%' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown text size')
    expect(onSaved).not.toHaveBeenCalled()
    const sizes = screen.getByRole('radiogroup', { name: 'Text size' })
    expect(sizes).toHaveAccessibleDescription('unknown text size')
    expect(within(sizes).getByRole('radio', { name: '100%' })).toBeChecked()
  })

  it('offers the themes, with the chosen one checked', async () => {
    renderSettings({ theme: 'dark' })
    await openSection('Appearance')

    const themes = screen.getByRole('radiogroup', { name: 'Theme' })
    expect(
      within(themes)
        .getAllByRole('radio')
        .map((r) => r.getAttribute('value')),
    ).toEqual(['system', 'light', 'dark'])
    expect(within(themes).getByRole('radio', { name: 'Dark' })).toBeChecked()
    expect(within(themes).getByText('System')).toHaveAttribute(
      'title',
      'Light or dark as your Mac is, switching when it does',
    )
    expect(within(themes).getByText('Light')).toHaveAttribute('title', 'Always light')
  })

  it('switches the theme as soon as it is chosen, turning in only the picked icon', async () => {
    const { onThemeChange } = renderSettings({ theme: 'system' })
    await openSection('Appearance')

    const themes = screen.getByRole('radiogroup', { name: 'Theme' })
    const icon = (name: string) => within(themes).getByText(name).querySelector('svg')

    expect(icon('System')?.getAttribute('class')).not.toMatch(/animate-/)
    expect(icon('Light')?.getAttribute('class')).not.toMatch(/animate-/)

    await userEvent.click(within(themes).getByRole('radio', { name: 'Light' }))

    expect(onThemeChange).toHaveBeenCalledExactlyOnceWith('light')
    expect(icon('Light')).toHaveClass('motion-safe:animate-sun-in')
    expect(icon('Dark')?.getAttribute('class')).not.toMatch(/animate-/)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(themes).not.toHaveAttribute('aria-describedby')
  })

  it('shows a theme that could not be stored', async () => {
    const onThemeChange = vi.fn(() => Promise.resolve<Result<void>>({ ok: false, error: 'unknown theme "dim"' }))
    renderSettings({ onThemeChange })
    await openSection('Appearance')

    await userEvent.click(screen.getByRole('radio', { name: 'Dark' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown theme "dim"')
    expect(screen.getByRole('radiogroup', { name: 'Theme' })).toHaveAccessibleDescription('unknown theme "dim"')
  })

  it('persists the terminal choice', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const terminal = screen.getByRole('combobox', { name: 'Terminal' })
    expect(terminal).toHaveValue('iterm2')
    expect(terminal).toHaveAccessibleDescription('The app new sessions open in.')

    await userEvent.selectOptions(terminal, 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ terminal: 'terminal' }))
    })
  })

  it('shows a terminal the backend refused', async () => {
    platform.setTerminal.mockRejectedValue(new Error('unknown terminal'))
    const { onSaved } = renderSettings()
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown terminal')
    expect(onSaved).not.toHaveBeenCalled()
    // The menu is read with its help, then why the change was refused.
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveAccessibleDescription(
      'The app new sessions open in. unknown terminal',
    )
  })

  it('persists the default agent', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const select = screen.getByRole('combobox', { name: 'Default agent' })
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Claude', 'Codex'])

    await userEvent.selectOptions(select, 'codex')

    expect(platform.setDefaultAgent).toHaveBeenCalledWith('codex')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ defaultAgent: 'codex' }))
    })
  })

  // bothAgents are a Claude and a Codex profile each, beside each agent's default.
  const bothAgents = [
    profile(),
    profile({ name: 'work' }),
    profile({ agent: 'codex', folder: '~/.codex' }),
    profile({ agent: 'codex', name: 'side', folder: '~/.codex_side' }),
  ]

  it('offers each agent only its own profiles as its default', async () => {
    renderSettings({ profiles: bothAgents })
    await openSection('Launching')

    const options = (name: string) =>
      within(screen.getByRole('combobox', { name }))
        .getAllByRole('option')
        .map((o) => o.textContent)

    expect(options('Claude profile')).toEqual(['default', 'work'])
    expect(options('Codex profile')).toEqual(['default', 'side'])
  })

  it.each([
    { agent: 'claude', label: 'Claude profile', name: 'work', saved: { claude: 'work', codex: 'default' } },
    { agent: 'codex', label: 'Codex profile', name: 'side', saved: { claude: 'default', codex: 'side' } },
  ] as const)('persists $agent’s default profile', async ({ agent, label, name, saved }) => {
    const { onSaved } = renderSettings({ profiles: bothAgents })
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: label }), name)

    expect(platform.setDefaultProfile).toHaveBeenCalledWith(agent, name)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ defaultProfiles: saved }))
    })
  })

  it('shows a default profile the backend refused', async () => {
    platform.setDefaultProfile.mockRejectedValue(new Error('no profile named work'))
    renderSettings()
    await openSection('Launching')

    const select = screen.getByRole('combobox', { name: 'Claude profile' })
    await userEvent.selectOptions(select, 'work')

    expect(await screen.findByRole('alert')).toHaveTextContent('no profile named work')
    expect(select).toHaveAccessibleDescription(
      'The Claude profile a launch uses, unless an org or repository chooses another. no profile named work',
    )
  })

  it('cannot pick a default profile for an agent with none', async () => {
    renderSettings({ current: settings({ defaultProfiles: { claude: 'default' } }) })
    await openSection('Launching')

    const select = screen.getByRole('combobox', { name: 'Codex profile' })
    expect(select).toBeDisabled()
    expect(select).toHaveAccessibleDescription('No Codex profiles were found: run codex once so it makes ~/.codex.')
  })

  it('persists the worktree default with a switch', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const toggle = screen.getByRole('switch', { name: 'Start in a worktree' })
    expect(toggle).toBeChecked()

    await userEvent.click(toggle)

    expect(platform.setWorktreeDefault).toHaveBeenCalledWith(false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ worktreeDefault: false }))
    })
    expect(toggle).not.toBeChecked()
  })

  it('leaves Codex its own effort and speed unless switched on, saying what it costs', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const effort = screen.getByRole('switch', { name: 'Set Codex’s effort at launch' })
    const fast = screen.getByRole('switch', { name: 'Start Codex in Fast mode' })
    expect(effort).not.toBeChecked()
    expect(fast).not.toBeChecked()
    expect(effort).toHaveAccessibleDescription(/can’t show when it waits for you/)
    expect(fast).toHaveAccessibleDescription(/\/fast/)

    await userEvent.click(effort)
    expect(platform.setCodexEffort).toHaveBeenCalledWith(true)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ codexEffort: true }))
    })

    await userEvent.click(fast)
    expect(platform.setCodexFast).toHaveBeenCalledWith(true)
  })

  it('turns following the agent sessions off and on', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const follow = screen.getByRole('switch', { name: 'Follow agent sessions' })
    expect(follow).toBeChecked()
    expect(follow).toHaveAccessibleDescription(/nothing asks the agents/)

    await userEvent.click(follow)

    expect(platform.setFollowSessions).toHaveBeenCalledWith(false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ followSessions: false }))
    })
    expect(follow).not.toBeChecked()
  })

  it('shows a worktree default the backend refused, and switches it back', async () => {
    let refuse: (err: Error) => void = () => undefined
    platform.setWorktreeDefault.mockReturnValue(
      new Promise((_, reject) => {
        refuse = reject
      }),
    )
    renderSettings()
    await openSection('Launching')

    const toggle = screen.getByRole('switch', { name: 'Start in a worktree' })
    await userEvent.click(toggle)

    // The switch moves before the backend answers.
    expect(toggle).not.toBeChecked()

    refuse(new Error('database is locked'))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    await waitFor(() => {
      expect(toggle).toBeChecked()
    })
    expect(toggle).toHaveAccessibleDescription(/You can still change it per launch\. .* database is locked$/)
  })

  it('turns the automatic worktree cleanup off and on, sweeping at once when turned on', async () => {
    const { onSaved } = renderSettings()
    await openSection('Worktrees')

    const toggle = screen.getByRole('switch', { name: 'Clean up after merging' })
    expect(toggle).toBeChecked()
    expect(toggle).toHaveAccessibleDescription(/once its pull request merges or its issue closes/)

    await userEvent.click(toggle)

    expect(platform.setWorktreeAutoCleanup).toHaveBeenCalledWith(false)
    await vi.waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ worktreeAutoCleanup: false }))
    })
    expect(platform.sweepWorktrees).not.toHaveBeenCalled()
  })

  it('sweeps when the automatic cleanup is turned on, and shows a refusal', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.sweepWorktrees.mockRejectedValue(new Error('no clone root'))
    const { onSaved } = renderSettings({ current: settings({ worktreeAutoCleanup: false }) })
    await openSection('Worktrees')

    await userEvent.click(screen.getByRole('switch', { name: 'Clean up after merging' }))

    expect(platform.setWorktreeAutoCleanup).toHaveBeenCalledWith(true)
    await vi.waitFor(() => {
      expect(platform.sweepWorktrees).toHaveBeenCalledOnce()
    })
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not sweep the worktrees', expect.any(Error))
    })
    expect(onSaved).toHaveBeenCalledWith(settings({ worktreeAutoCleanup: true }))
    error.mockRestore()

    platform.setWorktreeAutoCleanup.mockRejectedValue(new Error('database is locked'))
    await userEvent.click(screen.getByRole('switch', { name: 'Clean up after merging' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    expect(screen.getByRole('switch', { name: 'Clean up after merging' })).toHaveAccessibleDescription(
      /ignored files in it go with it\. database is locked$/,
    )
  })

  it('persists the clone root', async () => {
    const { onSaved } = renderSettings()
    await openSection('Repositories')

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    expect(field).toHaveValue('~/projects')

    await userEvent.clear(field)
    await userEvent.type(field, ' ~/code ')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.setCloneRoot).toHaveBeenCalledWith('~/code')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cloneRoot: '~/code' }))
    })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')

    // Editing again clears the confirmation.
    await userEvent.type(field, 'x')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows a clone root the backend refused', async () => {
    platform.setCloneRoot.mockRejectedValue(new Error('clone root ~/missing is not a directory'))
    const { onSaved } = renderSettings()
    await openSection('Repositories')

    const field = screen.getByRole('textbox', { name: 'Clone root' })
    await userEvent.clear(field)
    await userEvent.type(field, '~/missing{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('clone root ~/missing is not a directory')
    expect(onSaved).not.toHaveBeenCalled()
    expect(field).toHaveAccessibleDescription(/look one level inside it\. clone root ~\/missing is not a directory$/)
  })

  it('waits for the settings to load', async () => {
    renderSettings({ current: null })

    await openSection('Launching')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()

    await openSection('Repositories')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()

    for (const name of ['Notifications', 'Reviewing', 'Trust', 'Sensitive files']) {
      await openSection(name)
      expect(screen.getByText('Loading the settings…')).toBeInTheDocument()
    }
  })

  it('shows the product and its version', async () => {
    renderSettings()
    await openSection('About')

    const about = screen.getByRole('region', { name: 'About' })
    expect(within(about).getByText(brand.name)).toHaveClass('lowercase')
    expect(about.querySelector('svg[aria-hidden="true"] path')).toHaveAttribute('d', brand.mark.path)
    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
    expect(await screen.findByText('1.4.0 (42)')).toBeInTheDocument()
  })

  it('lists the commit and the system a bug report needs', async () => {
    renderSettings()
    await openSection('About')

    const about = screen.getByRole('region', { name: 'About' })
    expect(await within(about).findByText('v1.4.0-2-gabc1234-dirty')).toBeInTheDocument()
    expect(within(about).getByText('Commit')).toBeInTheDocument()
    expect(within(about).getByText('macOS 15.1 (24B83), arm64')).toBeInTheDocument()
    expect(within(about).getByText('System')).toBeInTheDocument()
  })

  it('opens a bug report with the details filled in, made as it opens', async () => {
    renderSettings()
    await openSection('About')

    expect(platform.bugReportURL).not.toHaveBeenCalled()

    await userEvent.click(await screen.findByRole('button', { name: 'Report a bug' }))

    await waitFor(() => {
      expect(platform.openExternal).toHaveBeenCalledWith(
        'https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml',
      )
    })
    expect(platform.bugReportURL).toHaveBeenCalledOnce()
  })

  it('logs a bug report link that could not be made', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.bugReportURL.mockRejectedValue(new Error('the app is closing'))
    renderSettings()
    await openSection('About')

    await userEvent.click(await screen.findByRole('button', { name: 'Report a bug' }))

    await waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not make the bug report link', expect.any(Error))
    })
    expect(platform.openExternal).not.toHaveBeenCalled()
    error.mockRestore()
  })

  it('lists each coding agent’s installed version', async () => {
    platform.agentVersions.mockResolvedValue([
      { agent: 'claude', name: 'Claude', installed: true, path: '~/.local/bin/claude', version: '2.1.319', error: '' },
      {
        agent: 'codex',
        name: 'Codex',
        installed: true,
        path: '/opt/bin/codex',
        version: '',
        error: 'codex --version failed: boom',
      },
    ])
    renderSettings()
    await openSection('About')

    const agents = within(screen.getByRole('region', { name: 'Coding agents' }))
    expect(await agents.findByText('2.1.319')).toHaveAttribute('title', '~/.local/bin/claude')
    expect(agents.getByText('Claude')).toBeInTheDocument()
    expect(agents.getByText('unknown')).toHaveAttribute('title', 'codex --version failed: boom')
  })

  it('shows the GitHub CLI version with the tools', async () => {
    renderSettings({ section: 'about' })

    const tools = within(screen.getByRole('region', { name: 'Tools' }))
    expect(await tools.findByText('2.83.1')).toHaveAttribute('title', '/opt/homebrew/bin/gh')
    expect(tools.getByText('GitHub CLI')).toBeInTheDocument()
  })

  it('says why the tools could not be checked', async () => {
    platform.toolVersions.mockRejectedValue(new Error('the app is closing'))
    renderSettings({ section: 'about' })

    expect(await screen.findByText("Couldn't check the tools: the app is closing")).toBeInTheDocument()
  })

  it('says it is checking the tools', async () => {
    platform.toolVersions.mockReturnValue(new Promise(() => undefined))
    renderSettings({ section: 'about' })

    expect(await screen.findByText('Checking the tools…')).toBeInTheDocument()
  })

  it('says when a coding agent is not installed', async () => {
    platform.agentVersions.mockResolvedValue([
      { agent: 'claude', name: 'Claude', installed: false, path: '', version: '', error: '' },
    ])
    renderSettings({ section: 'about' })

    const agents = within(screen.getByRole('region', { name: 'Coding agents' }))
    expect(await agents.findByText('not installed')).not.toHaveAttribute('title')
  })

  it('says it is checking the agents', async () => {
    platform.agentVersions.mockReturnValue(new Promise(() => undefined))
    renderSettings({ section: 'about' })

    expect(await screen.findByText('Checking the agents…')).toBeInTheDocument()
  })

  it('says why the agents could not be checked', async () => {
    platform.agentVersions.mockRejectedValue(new Error('the app is closing'))
    renderSettings({ section: 'about' })

    expect(await screen.findByText("Couldn't check the agents: the app is closing")).toBeInTheDocument()
  })

  it('shows a placeholder while the version loads', async () => {
    platform.getAppInfo.mockReturnValue(new Promise(() => undefined))
    renderSettings()
    await openSection('About')

    expect(screen.getByText('…')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Report a bug' })).not.toBeInTheDocument()
  })

  it('says when the version could not be read', async () => {
    platform.getAppInfo.mockRejectedValue(new Error('no build info'))
    renderSettings()
    await openSection('About')

    expect(await screen.findByText('unknown (no build info)')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Report a bug' })).not.toBeInTheDocument()
  })

  it('lists each profile with its folder and what it sets', async () => {
    renderSettings({
      profiles: [
        profile(),
        profile({
          name: 'work',
          configDir: '/home/user/.claude_work',
          folder: '~/.claude_work',
          model: 'opus',
          effort: 'high',
          planMode: true,
          permissionMode: 'acceptEdits',
          addDirs: ['~/shared', '~/docs'],
          extraArgs: ['--verbose', '--debug'],
          env: { EXAMPLE_FLAG: '1', OTHER_FLAG: '2' },
        }),
        profile({
          name: 'odd',
          folder: '/opt/profiles/odd',
          permissionMode: 'customMode',
          addDirs: ['~/shared'],
          extraArgs: ['--verbose'],
          env: { EXAMPLE_FLAG: '1' },
        }),
        profile({ name: 'old', folder: '~/.claude_old', missing: true, permissionMode: '' }),
        profile({ name: 'bare', folder: '~/.claude_bare', addDirs: [], extraArgs: [], env: {} }),
      ],
    })
    await openSection('Profiles')

    expect(screen.getByRole('button', { name: 'Profiles' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('heading', { name: 'Profiles' })).toBeInTheDocument()

    const traits = (name: string) =>
      within(screen.getByRole('list', { name: `${name} settings` }))
        .getAllByRole('listitem')
        .map((chip) => chip.textContent)

    const rows = within(screen.getByRole('list', { name: 'Claude profiles' }))
    expect(rows.getByText('~/.claude')).toHaveAttribute('title', '~/.claude')
    expect(rows.getByText('~/.claude_work')).toBeInTheDocument()

    expect(traits('default')).toEqual(['Full access'])
    expect(traits('work')).toEqual([
      'opus',
      'high effort',
      'Starts in plan mode',
      '2 extra directories',
      '2 extra arguments',
      '2 environment variables',
      'Accepts edits',
    ])
    expect(traits('odd')).toEqual(['1 extra directory', '1 extra argument', '1 environment variable', 'customMode'])
    expect(traits('old')).toEqual(['Folder missing', 'Asks first'])
    expect(traits('bare')).toEqual(['Full access'])
  })

  it('explains where each agent’s profiles come from and what counts as one', async () => {
    renderSettings()
    await openSection('Profiles')

    expect(screen.getByText(/looks for profiles in your home folder each time it starts/)).toBeInTheDocument()

    const claude = within(screen.getByRole('region', { name: 'Claude profiles' }))
    const claudeNote = claude.getByText(/default profile, and each/)
    expect(claudeNote).toHaveTextContent('each ~/.claude_<name> or ~/.claude-<name> folder becomes profile <name>')
    expect(claudeNote).toHaveTextContent('run with CLAUDE_CONFIG_DIR set to it')
    expect(claudeNote).toHaveTextContent('.claude.json, settings.json, or projects/')

    const codexNote = within(screen.getByRole('region', { name: 'Codex profiles' })).getByText(
      /default profile, and each/,
    )
    expect(codexNote).toHaveTextContent('each ~/.codex_<name> or ~/.codex-<name> folder becomes profile <name>')
    expect(codexNote).toHaveTextContent('run with CODEX_HOME set to it')
    expect(codexNote).toHaveTextContent('auth.json, config.toml, or sessions/')
  })

  it('groups profiles by agent, and says how to sign one in', async () => {
    renderSettings({
      profiles: [
        profile(),
        profile({ agent: 'codex', folder: '~/.codex', permissionMode: 'workspace-write' }),
        profile({ agent: 'codex', name: 'side', configDir: '~/.codex_side', folder: '~/.codex_side', signedIn: false }),
      ],
    })
    await openSection('Profiles')

    const codex = within(screen.getByRole('list', { name: 'Codex profiles' }))
    expect(codex.getByText('~/.codex_side')).toBeInTheDocument()
    expect(codex.getByText(/Not signed in: run/)).toHaveTextContent(
      'Not signed in: run CODEX_HOME=~/.codex_side codex login',
    )
    expect(
      within(codex.getByRole('list', { name: 'default settings' }))
        .getAllByRole('listitem')
        .map((chip) => chip.textContent),
    ).toEqual(['Writes in the workspace'])
    expect(
      within(codex.getByRole('list', { name: 'side settings' }))
        .getAllByRole('listitem')
        .map((chip) => chip.textContent),
    ).toEqual(['Not signed in', 'Full access'])

    expect(
      within(screen.getByRole('list', { name: 'Claude profiles' })).queryByText('~/.codex'),
    ).not.toBeInTheDocument()
  })

  it('shows each agent’s installed version over its profiles', async () => {
    renderSettings()
    await openSection('Profiles')

    const claude = within(screen.getByRole('region', { name: 'Claude profiles' }))
    expect(await claude.findByText('2.1.319')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Codex profiles' })).getByText('0.163.0')).toBeInTheDocument()
  })

  it('says how to sign in to an agent’s default profile, and when an agent is not installed', async () => {
    renderSettings({
      agents: agents().map((a) => (a.id === 'codex' ? { ...a, installed: false, path: '' } : a)),
      profiles: [profile(), profile({ agent: 'codex', folder: '~/.codex', signedIn: false })],
    })
    await openSection('Profiles')

    const codex = within(screen.getByRole('region', { name: 'Codex profiles' }))
    expect(codex.getByRole('heading', { name: /Codex/ })).toHaveTextContent('codex isn’t installed')
    expect(codex.getByText(/Not signed in: run/)).toHaveTextContent('Not signed in: run codex login')
    expect(
      within(screen.getByRole('region', { name: 'Claude profiles' })).getByRole('heading', { name: /Claude/ }),
    ).toHaveTextContent('/opt/bin/claude')
  })

  it('marks an agent that is not installed among the default agents', async () => {
    renderSettings({ agents: agents().map((a) => (a.id === 'codex' ? { ...a, installed: false, path: '' } : a)) })
    await openSection('Launching')

    expect(
      within(screen.getByRole('combobox', { name: 'Default agent' }))
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Claude', 'Codex (not installed)'])
  })

  it('says when an agent has no profiles', async () => {
    renderSettings({ profiles: [] })
    await openSection('Profiles')

    expect(screen.getByText(/No Claude profiles were found in your home folder/)).toBeInTheDocument()
    expect(screen.getByText(/No Codex profiles were found in your home folder/)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Claude profiles' })).not.toBeInTheDocument()
  })

  it('closes from the back button and on Escape', async () => {
    const { onClose } = renderSettings()

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(onClose).toHaveBeenCalledOnce()

    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('closes from the Settings title too, part of the back button', async () => {
    const { onClose } = renderSettings()

    await userEvent.click(screen.getByText('Settings'))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('closes on Escape from a switch or a menu, which do not use the key', async () => {
    const { onClose } = renderSettings()
    await openSection('Launching')

    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()

    screen.getByRole('combobox', { name: 'Terminal' }).focus()
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('leaves Escape to a field being edited, or to whatever already used it', async () => {
    const { onClose } = renderSettings()
    await openSection('Repositories')

    await userEvent.click(screen.getByRole('textbox', { name: 'Clone root' }))
    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()

    // Whatever handles Escape before the page, as an open menu does, marks it used.
    await userEvent.click(document.body)
    const useEscape = (e: KeyboardEvent) => {
      e.preventDefault()
    }
    document.addEventListener('keydown', useEscape, true)
    await userEvent.keyboard('{Escape}')
    document.removeEventListener('keydown', useEscape, true)
    expect(onClose).not.toHaveBeenCalled()

    // An input method composing text owns the key too. user-event cannot compose text, so
    // the composing key is sent by hand.
    fireEvent.keyDown(document.body, { key: 'Escape', isComposing: true })
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('SettingsView launch defaults', () => {
  it('sets the app’s default prompt for each kind of item', async () => {
    const { onLaunchChanged } = renderSettings()

    await openSection('Launching')

    const pr = screen.getByRole('combobox', { name: 'Prompt for pull requests' })
    const issue = screen.getByRole('combobox', { name: 'Prompt for issues' })

    expect(pr).toHaveValue('1')
    expect(
      within(pr)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Fix checks', 'Quick look', 'Review', 'Explain'])
    expect(issue).toHaveValue('4')
    expect(
      within(issue)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Work on it', 'Explain'])

    await userEvent.selectOptions(pr, '2')

    expect(platform.setDefaultPrompt).toHaveBeenCalledWith('pr', 2)
    await waitFor(() => {
      expect(onLaunchChanged).toHaveBeenCalledOnce()
    })
  })

  it('shows why a default prompt was refused', async () => {
    platform.setDefaultPrompt.mockRejectedValue(new Error('the prompt doesn’t apply to that kind of item'))
    renderSettings()

    await openSection('Launching')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Prompt for issues' }), '5')

    expect(await screen.findByRole('alert')).toHaveTextContent('doesn’t apply to that kind of item')
    expect(screen.getByRole('combobox', { name: 'Prompt for issues' })).toHaveAccessibleDescription(
      /Edit prompts on the Prompts tab\. the prompt doesn’t apply to that kind of item$/,
    )
    expect(screen.getByRole('combobox', { name: 'Prompt for pull requests' })).toHaveAccessibleDescription(
      /Edit prompts on the Prompts tab\.$/,
    )
  })

  it('lists the org and repository launch defaults under Repositories', async () => {
    renderSettings({
      defaults: [
        launchDefaults({ prPromptId: 2, effort: 'max' }),
        launchDefaults({ name: 'api', agent: 'codex', profiles: {}, models: { codex: 'gpt-6-sol' }, worktree: false }),
      ],
    })

    await openSection('Repositories')

    const list = within(screen.getByRole('list', { name: 'Launch defaults' }))

    expect(list.getByText('acme-org')).toBeInTheDocument()
    expect(list.getByText('All repositories')).toBeInTheDocument()
    expect(list.getByText('Claude profile work')).toBeInTheDocument()
    expect(list.getByText('Quick look for PRs')).toBeInTheDocument()
    expect(list.getByText('max effort')).toBeInTheDocument()
    expect(list.getByText('acme-org/api')).toBeInTheDocument()
    expect(list.getByText('starts Codex')).toBeInTheDocument()
    expect(list.getByText('Codex: GPT-6-Sol')).toBeInTheDocument()
    expect(list.getByText('no worktree')).toBeInTheDocument()
  })
})

describe('SettingsView escape and trust', () => {
  it('keeps the page open on Escape inside a dialog or an editor', async () => {
    const { onClose } = renderSettings({ defaults: [launchDefaults()] })

    await openSection('Repositories')
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    screen.getByRole('combobox', { name: 'Effort' }).focus()
    await userEvent.keyboard('{Escape}')

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('form')).toBeInTheDocument()
  })

  it('has a Trust section for the trusted bots', async () => {
    renderSettings()

    await openSection('Trust')

    expect(screen.getByRole('region', { name: 'Trust' })).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Trusted bots' })).getAllByRole('listitem')).toHaveLength(3)
  })

  it('has a Sensitive files section after Trust', async () => {
    renderSettings()

    const nav = within(screen.getByRole('navigation', { name: 'Settings sections' }))
    const names = nav.getAllByRole('button').map((b) => b.textContent)
    expect(names.indexOf('Sensitive files')).toBe(names.indexOf('Trust') + 1)

    await openSection('Sensitive files')

    expect(screen.getByRole('region', { name: 'Sensitive files' })).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: 'Check pull requests from' })).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Categories' })).toBeInTheDocument()
  })

  it('has a Reviewing section after Snoozing, for the merge method', async () => {
    renderSettings()

    const nav = within(screen.getByRole('navigation', { name: 'Settings sections' }))
    const names = nav.getAllByRole('button').map((b) => b.textContent)
    expect(names.indexOf('Reviewing')).toBe(names.indexOf('Snoozing') + 1)

    await openSection('Reviewing')

    expect(screen.getByRole('region', { name: 'Reviewing' })).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: 'Merge method' })).toBeInTheDocument()
  })

  it('has an Integrations section between Sensitive files and About, for the tools it runs', async () => {
    renderSettings()

    const nav = within(screen.getByRole('navigation', { name: 'Settings sections' }))
    const names = nav.getAllByRole('button').map((b) => b.textContent)
    expect(names.indexOf('Integrations')).toBe(names.indexOf('Sensitive files') + 1)
    expect(names.indexOf('About')).toBe(names.indexOf('Integrations') + 1)

    await openSection('Integrations')

    const region = screen.getByRole('region', { name: 'Integrations' })
    expect(
      within(region).getByText(`Tools ${brand.name} runs for you: mage-x releases, go-pre-commit hooks, and scripts.`),
    ).toBeInTheDocument()
    expect(await within(region).findByRole('switch', { name: 'mage-x' })).toBeChecked()
    expect(within(region).getByRole('switch', { name: 'go-pre-commit' })).toBeChecked()
  })

  it('hands on the settings a tool’s switch stored', async () => {
    const { onSaved } = renderSettings({ section: 'integrations' })

    await userEvent.click(await screen.findByRole('switch', { name: 'mage-x' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'off')
    expect(onSaved.mock.lastCall?.[0]).toMatchObject({ integrations: { switches: { 'mage-x': 'off' } } })
  })

  it('opens at the Sensitive files section when asked', () => {
    renderSettings({ section: 'files' })

    expect(screen.getByRole('button', { name: 'Sensitive files' })).toHaveAttribute('aria-current', 'page')
  })
})

describe('SettingsView notifications', () => {
  // banner returns a kind's banner switch.
  function banner(name: string) {
    return screen.getByRole('switch', { name })
  }

  it('comes after Appearance, and is where the bell’s settings link goes', () => {
    renderSettings({ section: 'notifications' })

    const nav = within(screen.getByRole('navigation', { name: 'Settings sections' }))
    const names = nav.getAllByRole('button').map((b) => b.textContent)
    expect(names.indexOf('Notifications')).toBe(names.indexOf('Appearance') + 1)
    expect(screen.getByRole('button', { name: 'Notifications' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument()
  })

  it('turns the menu bar icon off and on', async () => {
    const { onSaved } = renderSettings({ section: 'notifications' })

    const menuBar = screen.getByRole('switch', { name: 'Show in the menu bar' })
    expect(menuBar).toBeChecked()
    expect(menuBar).toHaveAccessibleDescription(/closing the window keeps/)

    await userEvent.click(menuBar)

    expect(platform.setMenuBar).toHaveBeenCalledWith(false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({ menuBar: false }))
    })
  })

  it('switches each kind’s banners, saving the kinds that are off', async () => {
    let stored = settings({ bannersOff: ['woke'] })
    platform.setNotificationBanner.mockImplementation((kind: NotificationKind, on: boolean) => {
      stored = {
        ...stored,
        bannersOff: on ? stored.bannersOff.filter((k) => k !== kind) : [...stored.bannersOff, kind],
      }

      return Promise.resolve(stored)
    })
    const { onSaved } = renderSettings({ section: 'notifications', current: stored })

    expect(banner('Review requests')).toBeChecked()
    expect(banner('Assignments')).toBeChecked()
    expect(banner('Snoozes ending early')).not.toBeChecked()

    await userEvent.click(banner('Review requests'))
    expect(platform.setNotificationBanner).toHaveBeenCalledWith('review_requested', false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({ bannersOff: ['woke', 'review_requested'] }))
    })

    await userEvent.click(banner('Snoozes ending early'))
    expect(platform.setNotificationBanner).toHaveBeenCalledWith('woke', true)
    await waitFor(() => {
      expect(onSaved).toHaveBeenLastCalledWith(expect.objectContaining({ bannersOff: ['review_requested'] }))
    })
    expect(banner('Review requests')).not.toBeChecked()
    expect(banner('Snoozes ending early')).toBeChecked()
  })

  it('starts the agent sessions’ banners off, apart from the inbox’s', async () => {
    let stored = settings()
    platform.setNotificationBanner.mockImplementation((kind: NotificationKind, on: boolean) => {
      stored = {
        ...stored,
        bannersOff: on ? stored.bannersOff.filter((k) => k !== kind) : [...stored.bannersOff, kind],
      }

      return Promise.resolve(stored)
    })
    renderSettings({ section: 'notifications', current: stored })

    const sessions = screen.getByRole('switch', { name: 'Sessions waiting for you' })
    expect(sessions).not.toBeChecked()
    expect(banner('Sessions finishing a turn')).not.toBeChecked()
    expect(banner('Review requests')).toBeChecked()
    expect(screen.getByText(/can already notify through your terminal, so these start off/)).toBeInTheDocument()

    await userEvent.click(sessions)
    expect(platform.setNotificationBanner).toHaveBeenCalledWith('session_needs_you', true)
    await waitFor(() => {
      expect(sessions).toBeChecked()
    })
  })

  it('shows a setting that could not be saved', async () => {
    platform.setMenuBar.mockRejectedValue(new Error('database is locked'))
    const { onSaved } = renderSettings({ section: 'notifications' })

    await userEvent.click(screen.getByRole('switch', { name: 'Show in the menu bar' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Show in the menu bar' })).toBeChecked()
    })
    expect(onSaved).not.toHaveBeenCalled()
    // Every switch is read with why the last change was refused.
    expect(screen.getByRole('switch', { name: 'Show in the menu bar' })).toHaveAccessibleDescription(
      /quit from its menu or with ⌘Q\. database is locked$/,
    )
    expect(banner('Review requests')).toHaveAccessibleDescription(/ database is locked$/)
  })

  it('sends a test banner, and says how it went', async () => {
    renderSettings({ section: 'notifications' })

    const test = screen.getByRole('button', { name: 'Send a test' })
    await userEvent.click(test)

    expect(platform.sendTestNotification).toHaveBeenCalledOnce()
    expect(await screen.findByText(/^Sent\./)).toBeInTheDocument()
    expect(test).toHaveAccessibleDescription(/^Sent\. If no banner showed/)

    platform.sendTestNotification.mockRejectedValue(new Error('notifications are off for this app in System Settings'))
    await userEvent.click(test)

    expect(await screen.findByRole('alert')).toHaveTextContent('notifications are off')
    expect(test).toHaveAccessibleDescription(
      'Show a sample banner, asking for permission first if you have not been asked. notifications are off for this app in System Settings',
    )
    // Asking may have changed whether they are allowed, so the state is read again.
    expect(platform.notificationBannerStatus).toHaveBeenCalledTimes(3)
  })

  it('explains banners the user has not allowed', async () => {
    platform.notificationBannerStatus.mockResolvedValue('off')
    renderSettings({ section: 'notifications' })

    expect(await screen.findByText(/turned off for this app in System Settings/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send a test' })).toBeEnabled()
  })

  it('explains a build that cannot show banners, and offers no test', async () => {
    platform.notificationBannerStatus.mockResolvedValue('unavailable')
    renderSettings({ section: 'notifications' })

    expect(await screen.findByText(/need the packaged app/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send a test' })).toBeDisabled()
  })

  it('logs a banner state that could not be read', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.notificationBannerStatus.mockRejectedValue(new Error('timed out'))

    renderSettings({ section: 'notifications' })

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not read whether notifications are allowed', expect.any(Error))
    })
    logged.mockRestore()
  })
})

describe('SettingsView search', () => {
  // searchProps are the page's props with a query, for a test that changes the query.
  function searchProps(query: string, section: SettingsSection = 'account'): SettingsViewProps {
    return {
      initialSection: section,
      settings: settings(),
      agents: agents(),
      profiles: [profile(), profile({ agent: 'codex', name: 'default', folder: '~/.codex' })],
      prompts: prompts(),
      defaultPrompts: defaultPrompts(),
      launchDefaults: [],
      account: account(),
      projectAccess: 'on',
      theme: 'system',
      onThemeChange: () => Promise.resolve({ ok: true, data: undefined }),
      onSaved: vi.fn(),
      onLaunchChanged: () => Promise.resolve(),
      onClose: vi.fn(),
      query,
    }
  }

  // sidebar names the sections the sidebar lists, without their counts.
  function sidebar() {
    const nav = within(screen.getByRole('navigation', { name: 'Settings sections' }))

    return nav
      .getAllByRole('button')
      .filter((b) => b.getAttribute('aria-label') !== 'Close settings')
      .map((b) => b.firstChild?.textContent)
  }

  // matched lists the labels of the rows the search marks as matching.
  function matched(): (string | null)[] {
    return [...document.querySelectorAll('[data-match]')].map((el) => el.getAttribute('data-setting'))
  }

  it('lists only the sections with a matching setting, each with how many, and turns to the first', () => {
    render(<SettingsView {...searchProps('wake')} />)

    expect(sidebar()).toEqual(['Snoozing'])
    expect(screen.getByRole('button', { name: 'Snoozing (4 matches)' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('region', { name: 'Snoozing' })).toBeInTheDocument()
    expect(matched()).toEqual(['Wake early', 'New commits', 'Reviews', 'New comments'])
  })

  it('says a single match in the singular', () => {
    render(<SettingsView {...searchProps('dark mode')} />)

    expect(screen.getByRole('button', { name: 'Appearance (1 match)' })).toBeInTheDocument()
    expect(matched()).toEqual(['Theme'])
  })

  it('stays on the open section while it has a match', () => {
    render(<SettingsView {...searchProps('worktree', 'worktrees')} />)

    expect(sidebar()).toEqual(['Launching', 'Worktrees'])
    expect(screen.getByRole('button', { name: 'Launching (1 match)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Worktrees (6 matches)' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
  })

  it('scrolls the first match into view as the search changes', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    const { rerender } = render(<SettingsView {...searchProps('', 'launching')} />)

    expect(scroll).not.toHaveBeenCalled()

    rerender(<SettingsView {...searchProps('codex', 'launching')} />)

    expect(scroll).toHaveBeenCalledOnce()
    expect(scroll.mock.contexts[0]).toHaveAttribute('data-setting', 'Default agent')
    expect(matched()).toEqual([
      'Default agent',
      'Codex profile',
      'Set Codex’s effort at launch',
      'Start Codex in Fast mode',
    ])
  })

  it('says when no setting matches, and keeps the section open', () => {
    render(<SettingsView {...searchProps('zebra crossing', 'trust')} />)

    expect(screen.getByText('No setting matches “zebra crossing”.')).toBeInTheDocument()
    expect(sidebar()).toEqual([])
    expect(screen.getByRole('region', { name: 'Trust' })).toBeInTheDocument()
    expect(matched()).toEqual([])
  })

  it('lists every section again once the search is cleared, where it was left', () => {
    const { rerender } = render(<SettingsView {...searchProps('banners')} />)

    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument()

    rerender(<SettingsView {...searchProps('')} />)

    expect(sidebar()).toHaveLength(13)
    expect(sidebar()).toContain('Account')
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument()
    expect(matched()).toEqual([])
    expect(screen.queryByText(/No setting matches/)).not.toBeInTheDocument()
  })

  it('colors a matching heading', () => {
    render(<SettingsView {...searchProps('alerts')} />)

    expect(screen.getByRole('heading', { name: 'Banners' })).toHaveClass('text-accent')
    expect(screen.getByRole('heading', { name: 'Agent sessions' })).not.toHaveClass('text-accent')
  })

  it('marks the account’s cards', () => {
    render(<SettingsView {...searchProps('github')} />)

    expect(matched()).toEqual(['GitHub account', 'Projects on issues'])
  })

  it('marks one agent’s profiles', async () => {
    render(<SettingsView {...searchProps('codex_home')} />)

    expect(await screen.findByRole('region', { name: 'Codex profiles' })).toHaveAttribute('data-match')
    expect(screen.getByRole('region', { name: 'Claude profiles' })).not.toHaveAttribute('data-match')
    expect(screen.getByRole('heading', { name: /^Codex/ })).toHaveClass('text-accent')
  })

  it('marks the version in About', async () => {
    render(<SettingsView {...searchProps('report a bug')} />)

    expect(await screen.findByText('1.4.0 (42)')).toBeInTheDocument()
    expect(matched()).toEqual(['Version'])
  })

  it('marks the scope of the sensitive-file check, a choice of its own', () => {
    render(<SettingsView {...searchProps('forks')} />)

    expect(matched()).toEqual(['Check pull requests from'])
    expect(screen.getByRole('radiogroup', { name: 'Check pull requests from' })).toBeInTheDocument()
  })
})
