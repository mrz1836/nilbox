import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { Account, LaunchDefaults, Profile, Settings } from '../platform'
import { account, launchDefaults, profile, prompts, settings, signedOut } from '../test/fixtures'
import type { Theme } from '../lib/theme'
import { SettingsView, type SettingsSection } from './SettingsView'

const platform = vi.hoisted(() => ({
  getAppInfo: vi.fn(),
  openInBrowser: vi.fn(),
  setCloneRoot: vi.fn(),
  setDefaultProfile: vi.fn(),
  setTerminal: vi.fn(),
  setWorktreeDefault: vi.fn(),
  setWorktreeAutoCleanup: vi.fn(),
  sweepWorktrees: vi.fn(),
  setSnoozeDays: vi.fn(),
  setSnoozeWake: vi.fn(),
  setWindowSize: vi.fn(),
  setDefaultPrompt: vi.fn(),
  getSettings: vi.fn(),
  setTrustedBots: vi.fn(),
  resetTrustedBots: vi.fn(),
  listLaunchTargets: vi.fn(),
  checkLaunchDefaults: vi.fn(),
  saveLaunchDefaults: vi.fn(),
  removeLaunchDefaults: vi.fn(),
  setSensitiveScope: vi.fn(),
  setSensitiveCategory: vi.fn(),
  setSensitivePatterns: vi.fn(),
  matchSensitivePath: vi.fn(),
  refreshInbox: vi.fn(),
}))

vi.mock('../platform', () => platform)

beforeEach(() => {
  for (const mock of Object.values(platform)) {
    mock.mockReset().mockResolvedValue(undefined)
  }

  platform.getAppInfo.mockResolvedValue({
    name: brand.name,
    tagline: brand.tagline,
    version: 'v1.4.0-2-gabc1234-dirty',
    release: '1.4.0',
    build: '42',
    system: 'macOS 15.1 (24B83)',
    arch: 'arm64',
    bugReportUrl: 'https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml',
  })
  platform.listLaunchTargets.mockResolvedValue([])
  platform.checkLaunchDefaults.mockResolvedValue([])
})

interface Options {
  section?: SettingsSection
  current?: Settings | null
  profiles?: Profile[]
  who?: Account | null
  defaults?: LaunchDefaults[]
  theme?: Theme
  onThemeChange?: (theme: Theme) => Promise<void>
}

function renderSettings({
  section,
  current = settings(),
  profiles = [profile(), profile({ name: 'work' })],
  who = account(),
  defaults = [],
  theme = 'system',
  onThemeChange = vi.fn(() => Promise.resolve()),
}: Options = {}) {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  const onLaunchChanged = vi.fn(() => Promise.resolve())

  render(
    <SettingsView
      initialSection={section}
      settings={current}
      profiles={profiles}
      prompts={prompts()}
      defaultPrompts={{ pr: 1, issue: 4 }}
      launchDefaults={defaults}
      account={who}
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

    expect(platform.openInBrowser).toHaveBeenCalledWith('https://github.com/octo-user')
  })

  it('logs a profile that could not be opened', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))
    renderSettings({ who: account({ name: '' }) })

    expect(screen.getByText('octo-user')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'View on GitHub' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalled()
    })
    logged.mockRestore()
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

    await userEvent.click(within(reviews).getByRole('radio', { name: 'Never' }))
    expect(platform.setSnoozeWake).toHaveBeenLastCalledWith({ commits: 1, reviews: 'never', comments: 0 })

    await userEvent.click(within(commits).getByRole('radio', { name: 'Never' }))
    expect(platform.setSnoozeWake).toHaveBeenLastCalledWith({ commits: 0, reviews: 'decisions', comments: 0 })
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
  })

  it('shows a snooze length the backend refused', async () => {
    platform.setSnoozeDays.mockRejectedValue(new Error('invalid snooze length'))
    const { onSaved } = renderSettings()
    await openSection('Snoozing')

    await userEvent.click(screen.getByRole('radio', { name: '2w' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid snooze length')
    expect(onSaved).not.toHaveBeenCalled()
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
    expect(within(sizes).getByRole('radio', { name: 'Standard' })).toBeChecked()
    expect(within(sizes).getByText('Standard')).toHaveAttribute('title', '1280 × 800 (default)')
    expect(within(sizes).getByText('Large')).toHaveAttribute('title', '1440 × 900')
    expect(screen.getByText(/Opens at 1280 × 800\./)).toBeInTheDocument()

    await userEvent.click(within(sizes).getByRole('radio', { name: 'Large' }))

    expect(platform.setWindowSize).toHaveBeenCalledWith({ width: 1440, height: 900 })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ windowSize: { width: 1440, height: 900 } }))
    })
  })

  it('shows a window size the backend refused', async () => {
    platform.setWindowSize.mockRejectedValue(new Error('invalid window size'))
    const { onSaved } = renderSettings()
    await openSection('Appearance')

    await userEvent.click(screen.getByRole('radio', { name: 'Extra large' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid window size')
    expect(onSaved).not.toHaveBeenCalled()
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

  it('waits for the settings before showing the window size, but not to show the theme', async () => {
    renderSettings({ current: null })
    await openSection('Appearance')

    expect(screen.getByText('Loading the window size…')).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup', { name: 'Window size' })).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'System' })).toBeChecked()
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
  })

  it('shows a theme that could not be stored', async () => {
    const onThemeChange = vi.fn(() => Promise.reject(new Error('unknown theme "dim"')))
    renderSettings({ onThemeChange })
    await openSection('Appearance')

    await userEvent.click(screen.getByRole('radio', { name: 'Dark' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown theme "dim"')
  })

  it('persists the terminal choice', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const terminal = screen.getByRole('combobox', { name: 'Terminal' })
    expect(terminal).toHaveValue('iterm2')
    expect(terminal).toHaveAccessibleDescription('The app new sessions open in.')

    await userEvent.selectOptions(terminal, 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(onSaved).toHaveBeenCalledWith(settings({ terminal: 'terminal' }))
  })

  it('shows a terminal the backend refused', async () => {
    platform.setTerminal.mockRejectedValue(new Error('unknown terminal'))
    const { onSaved } = renderSettings()
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown terminal')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('persists the default profile', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Default profile' }), 'work')

    expect(platform.setDefaultProfile).toHaveBeenCalledWith('work')
    expect(onSaved).toHaveBeenCalledWith(settings({ defaultProfile: 'work' }))
  })

  it('cannot pick a default profile when none were found', async () => {
    renderSettings({ profiles: [] })
    await openSection('Launching')

    const select = screen.getByRole('combobox', { name: 'Default profile' })
    expect(select).toBeDisabled()
    expect(select).toHaveAccessibleDescription('No agent profiles were found in your home directory.')
  })

  it('persists the worktree default with a switch', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

    const toggle = screen.getByRole('switch', { name: 'Start in a worktree' })
    expect(toggle).toBeChecked()

    await userEvent.click(toggle)

    expect(platform.setWorktreeDefault).toHaveBeenCalledWith(false)
    expect(onSaved).toHaveBeenCalledWith(settings({ worktreeDefault: false }))
  })

  it('shows a worktree default the backend refused', async () => {
    platform.setWorktreeDefault.mockRejectedValue(new Error('database is locked'))
    renderSettings()
    await openSection('Launching')

    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
  })

  it('turns the automatic worktree cleanup off and on, sweeping at once when turned on', async () => {
    const { onSaved } = renderSettings()
    await openSection('Launching')

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
    await openSection('Launching')

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
    expect(onSaved).toHaveBeenCalledWith(settings({ cloneRoot: '~/code' }))
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
  })

  it('waits for the settings to load', async () => {
    renderSettings({ current: null })

    await openSection('Launching')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()

    await openSection('Repositories')
    expect(screen.getByText('Loading the settings…')).toBeInTheDocument()
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

  it('opens a bug report with the details filled in', async () => {
    renderSettings()
    await openSection('About')

    await userEvent.click(await screen.findByRole('button', { name: 'Report a bug' }))

    expect(platform.openInBrowser).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml',
    )
  })

  it('logs a bug report that could not be opened', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))
    renderSettings()
    await openSection('About')

    await userEvent.click(await screen.findByRole('button', { name: 'Report a bug' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalled()
    })
    logged.mockRestore()
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
        profile({ name: 'bare', folder: '~/.claude_bare', addDirs: null, extraArgs: null, env: null }),
      ],
    })
    await openSection('Profiles')

    expect(screen.getByRole('button', { name: 'Profiles' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('heading', { name: 'Profiles' })).toBeInTheDocument()

    const traits = (name: string) =>
      within(screen.getByRole('list', { name: `${name} settings` }))
        .getAllByRole('listitem')
        .map((chip) => chip.textContent)

    const rows = within(screen.getByRole('list', { name: 'Profiles' }))
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

  it('explains where profiles come from and what counts as one', async () => {
    renderSettings()
    await openSection('Profiles')

    const note = screen.getByText(/looks for profiles in your home folder each time it starts/)
    expect(note).toHaveTextContent('each ~/.claude_<name> or ~/.claude-<name> folder the agent has run with')
    expect(note).toHaveTextContent('.claude.json, settings.json, or projects/')
  })

  it('says when no profiles were found', async () => {
    renderSettings({ profiles: [] })
    await openSection('Profiles')

    expect(screen.getByText('No agent profiles were found in your home folder.')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Profiles' })).not.toBeInTheDocument()
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

    const used = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    used.preventDefault()
    fireEvent(document.body, used)
    expect(onClose).not.toHaveBeenCalled()

    // An input method composing text owns the key too.
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
  })

  it('lists the org and repository launch defaults under Repositories', async () => {
    renderSettings({
      defaults: [
        launchDefaults({ prPromptId: 2, effort: 'max' }),
        launchDefaults({ name: 'api', profile: '', worktree: false }),
      ],
    })

    await openSection('Repositories')

    const list = within(screen.getByRole('list', { name: 'Launch defaults' }))

    expect(list.getByText('acme-org')).toBeInTheDocument()
    expect(list.getByText('All repositories')).toBeInTheDocument()
    expect(list.getByText('profile work')).toBeInTheDocument()
    expect(list.getByText('Quick look for PRs')).toBeInTheDocument()
    expect(list.getByText('max effort')).toBeInTheDocument()
    expect(list.getByText('acme-org/api')).toBeInTheDocument()
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

  it('opens at the Sensitive files section when asked', () => {
    renderSettings({ section: 'files' })

    expect(screen.getByRole('button', { name: 'Sensitive files' })).toHaveAttribute('aria-current', 'page')
  })
})
