import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import { brand } from './generated/brand'
import {
  checkResult,
  item,
  itemResolution,
  list,
  profile,
  prompts,
  rate,
  resolution,
  settings,
  snapshot,
  sweepReport,
  unknownRate,
  variables,
  worktreesSnapshot,
} from './test/fixtures'

const platform = vi.hoisted(() => ({
  getAppInfo: vi.fn(),
  getSettings: vi.fn(),
  listProfiles: vi.fn(),
  setTerminal: vi.fn(),
  setCloneRoot: vi.fn(),
  setDefaultProfile: vi.fn(),
  setWorktreeDefault: vi.fn(),
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
  listWorktrees: vi.fn(),
  listPrompts: vi.fn(),
  getDefaultPrompts: vi.fn(),
  listPromptVariables: vi.fn(),
  listLaunchDefaults: vi.fn(),
  resolveLaunches: vi.fn(),
  resolveLaunch: vi.fn(),
  previewLaunch: vi.fn(),
  checkPrompt: vi.fn(),
  setDefaultPrompt: vi.fn(),
  listLaunchTargets: vi.fn(),
  checkLaunchDefaults: vi.fn(),
  setTrustedBots: vi.fn(),
  resetTrustedBots: vi.fn(),
  setSnoozeDays: vi.fn(),
  snoozeItem: vi.fn(),
  lastWorktreeSweep: vi.fn(),
  onWorktreesUpdated: vi.fn(),
}))

vi.mock('./platform', () => platform)

beforeEach(() => {
  vi.clearAllMocks()
  platform.getSettings.mockResolvedValue(settings({ defaultProfile: 'work', worktreeDefault: false }))
  platform.listProfiles.mockResolvedValue([profile(), profile({ name: 'work' })])
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
  platform.onInboxUpdated.mockReturnValue(() => undefined)
  platform.setTerminal.mockResolvedValue(undefined)
  platform.setDefaultProfile.mockResolvedValue(undefined)
  platform.setWorktreeDefault.mockResolvedValue(undefined)
  platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
  platform.getAppInfo.mockResolvedValue({
    name: brand.name,
    tagline: brand.tagline,
    version: 'dev',
    release: '0.0.0',
    build: '0',
    system: 'macOS 15.1 (24B83)',
    arch: 'arm64',
    bugReportUrl: 'https://github.com/acme-org/example-repo/issues/new',
  })
  platform.listPrompts.mockResolvedValue(prompts())
  platform.getDefaultPrompts.mockResolvedValue({ pr: 1, issue: 4 })
  platform.listPromptVariables.mockResolvedValue(variables())
  platform.listLaunchDefaults.mockResolvedValue([])
  platform.resolveLaunches.mockResolvedValue([
    itemResolution({
      profile: { value: 'work', source: 'app', inherited: 'work', inheritedSource: 'app' },
      worktree: { value: false, source: 'app', inherited: false, inheritedSource: 'app' },
    }),
  ])
  platform.resolveLaunch.mockResolvedValue(resolution())
  platform.checkPrompt.mockResolvedValue(checkResult())
  platform.listLaunchTargets.mockResolvedValue([])
  platform.checkLaunchDefaults.mockResolvedValue([])
  platform.setTrustedBots.mockResolvedValue(undefined)
  platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 0 }))
  platform.onWorktreesUpdated.mockReturnValue(() => undefined)
})

// rowOptions opens the inbox row's launch options and returns queries scoped to them.
async function rowOptions() {
  await userEvent.click(await screen.findByRole('button', { name: /^Launch options/ }))

  return within(screen.getByRole('dialog', { name: 'Launch options' }))
}

// openSettingsFromMenu opens the settings page the way a person would: the account menu.
async function openSettingsFromMenu() {
  await userEvent.click(await screen.findByRole('button', { name: 'Account: octo-user' }))
  await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))
}

describe('App', () => {
  it('shows the product name in the toolbar and the tagline under About', async () => {
    render(<App />)

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'About' }))

    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })

  it('shows the account and the API budget from the inbox in the toolbar', async () => {
    // The app runs on the real clock, so the reading must reset in the future.
    const resetAt = new Date(Date.now() + 30 * 60_000).toISOString()
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ rate: { graphql: rate({ resetAt }), core: rate({ remaining: 400, resetAt }), search: unknownRate } }),
    )
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Account: octo-user' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^API budget: Critical, 8% left \(REST\)/ })).toHaveClass('text-danger')
  })

  it('opens on the inbox with launch controls from the settings', async () => {
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Launch options: Review · work' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(platform.resolveLaunches).toHaveBeenCalledWith([1])

    const options = await rowOptions()
    expect(options.getByRole('radio', { name: 'work' })).toBeChecked()
    expect(options.getByRole('checkbox', { name: /Start in a worktree/ })).not.toBeChecked()
  })

  it('switches between the inbox and the worktrees view', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(screen.getByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('opens the settings page from the account menu and keeps a saved change', async () => {
    render(<App />)
    await screen.findByRole('button', { name: 'Open in Claude' })

    await openSettingsFromMenu()

    expect(screen.getByRole('region', { name: 'Account' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Terminal' }), 'terminal')

    expect(platform.setTerminal).toHaveBeenCalledWith('terminal')
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveValue('terminal')

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    expect(screen.getByRole('combobox', { name: 'Terminal' })).toHaveValue('terminal')
  })

  it('snoozes for the length chosen in Settings, reached from the snooze menu', async () => {
    platform.setSnoozeDays.mockResolvedValue(undefined)
    platform.snoozeItem.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<App />)

    expect(await screen.findByRole('button', { name: 'Snooze for 3 days' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Snooze for…' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Snooze settings…' }))

    expect(screen.getByRole('region', { name: 'Snoozing' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: '2w' }))
    expect(platform.setSnoozeDays).toHaveBeenCalledWith(14)

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Snooze for 2 weeks' }))

    expect(platform.snoozeItem).toHaveBeenCalledWith(1, 14)

    // The account menu still opens the settings at the account.
    await openSettingsFromMenu()
    expect(screen.getByRole('region', { name: 'Account' })).toBeInTheDocument()
  })

  it('re-resolves the inbox rows after a settings change', async () => {
    render(<App />)
    await screen.findByRole('button', { name: 'Launch options: Review · work' })
    expect(platform.resolveLaunches).toHaveBeenCalledOnce()

    platform.resolveLaunches.mockResolvedValue([itemResolution()])

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Launching' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Start in a worktree' }))
    await userEvent.keyboard('{Escape}')

    expect(await screen.findByRole('button', { name: 'Launch options: Review · default' })).toBeInTheDocument()
    expect(platform.resolveLaunches).toHaveBeenCalledTimes(2)
  })

  it('reads the inbox again when the trusted bots change, so the cards relabel', async () => {
    const bot = item({ author: 'dependabot', trust: 'outside', trusted: false, authorIsBot: true })
    platform.getSettings.mockResolvedValue(settings({ trustedBots: [] }))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([bot]) }))
    render(<App />)

    expect(await screen.findByText('untrusted bot')).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Trust' }))

    platform.getSettings.mockResolvedValue(settings({ trustedBots: ['dependabot'] }))
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...bot, trust: 'bot', trusted: true }]) }),
    )

    await userEvent.type(screen.getByRole('textbox', { name: 'Bot to trust' }), 'dependabot')
    await userEvent.click(screen.getByRole('button', { name: 'Trust bot' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    expect(await screen.findByText('bot')).toBeInTheDocument()
    expect(screen.queryByText('untrusted bot')).not.toBeInTheDocument()
  })

  it('opens the Prompts page and keeps it while switching tabs', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Prompts' }))

    const library = await screen.findByRole('navigation', { name: 'Prompts' })
    expect(within(library).getByRole('button', { name: /^Fix checks/ })).toHaveAttribute('aria-current', 'true')

    const title = screen.getByRole('textbox', { name: 'Title' })
    await userEvent.type(title, ' again')

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    expect(screen.queryByRole('navigation', { name: 'Prompts' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Prompts' }))
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks again')
  })

  it('opens the Prompts page from a row’s launch menu', async () => {
    render(<App />)

    await userEvent.click((await rowOptions()).getByRole('button', { name: 'Manage' }))

    expect(await screen.findByRole('navigation', { name: 'Prompts' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prompts' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('opens the settings with ⌘, and returns to the view it came from', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await userEvent.keyboard('{Meta>},{/Meta}')

    expect(screen.getByRole('navigation', { name: 'Settings sections' })).toBeInTheDocument()

    // Pressing it again stays put rather than forgetting where to go back to.
    await userEvent.keyboard('{Meta>},{/Meta}')
    await userEvent.keyboard('{Escape}')

    expect(screen.getByRole('region', { name: 'Worktrees' })).toBeInTheDocument()

    // A comma without ⌘ is just a comma.
    await userEvent.keyboard(',')
    expect(screen.queryByRole('navigation', { name: 'Settings sections' })).not.toBeInTheDocument()
  })

  it('counts the worktrees worth a look on the Worktrees tab and toasts an automatic cleanup', async () => {
    let swept: () => void = () => undefined
    const stop = vi.fn()
    platform.onWorktreesUpdated.mockImplementation((listener: () => void) => {
      swept = listener

      return stop
    })
    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 1, leftover: 2 }))

    const { unmount } = render(<App />)

    const tab = screen.getByRole('button', { name: 'Worktrees' })
    expect(await within(tab).findByText('2')).toBeInTheDocument()
    expect(tab).toHaveAttribute('title', '2 worktrees are done on GitHub but still on disk')
    expect(screen.queryByText(/Cleaned up/)).not.toBeInTheDocument()

    // A sweep removed two worktrees, one of them for an item.
    platform.lastWorktreeSweep.mockResolvedValue(
      sweepReport({
        seq: 2,
        leftover: 1,
        cleaned: [
          {
            repoPath: '/home/user/projects/example-repo',
            path: '/home/user/projects/example-repo/.claude/worktrees/pr-7',
            displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
            removed: true,
            deletedBranch: 'worktree-pr-7',
            item: 'acme-org/example-repo#7',
            reason: '',
          },
          {
            repoPath: '/home/user/projects/example-repo',
            path: '/home/user/projects/example-repo/.claude/worktrees/pr-8',
            displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
            removed: true,
            deletedBranch: '',
            item: '',
            reason: '',
          },
        ],
      }),
    )
    await act(async () => {
      swept()
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up 2 finished worktrees')).toBeInTheDocument()
    expect(
      screen.getByText(/acme-org\/example-repo#7, ~\/projects\/example-repo\/\.claude\/worktrees\/pr-8/),
    ).toBeInTheDocument()
    expect(within(tab).getByText('1')).toBeInTheDocument()
    expect(tab).toHaveAttribute('title', '1 worktree is done on GitHub but still on disk')

    // An event for a sweep already read changes nothing; a later one with one cleaned
    // worktree says so.
    await act(async () => {
      swept()
      await Promise.resolve()
    })

    platform.lastWorktreeSweep.mockResolvedValue(
      sweepReport({
        seq: 3,
        cleaned: [
          {
            repoPath: '/r',
            path: '/r/.claude/worktrees/pr-9',
            displayPath: '~/r/.claude/worktrees/pr-9',
            removed: true,
            deletedBranch: '',
            item: 'acme-org/r#9',
            reason: '',
          },
        ],
      }),
    )
    await act(async () => {
      swept()
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    expect(within(tab).queryByText('1')).not.toBeInTheDocument()
    expect(tab).not.toHaveAttribute('title')

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByText('Cleaned up a finished worktree')).not.toBeInTheDocument()

    unmount()
    expect(stop).toHaveBeenCalledOnce()
  })

  it('counts from the Worktrees view and lists again after a sweep', async () => {
    let swept: () => void = () => undefined
    platform.onWorktreesUpdated.mockImplementation((listener: () => void) => {
      swept = listener

      return () => undefined
    })
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ finishedCount: 2, reviewCount: 1 }))

    render(<App />)
    const tab = screen.getByRole('button', { name: 'Worktrees' })
    await userEvent.click(tab)

    expect(await within(tab).findByText('3')).toBeInTheDocument()
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 4, leftover: 0 }))
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await act(async () => {
      swept()
      await Promise.resolve()
    })

    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(within(tab).queryByText(/\d/)).not.toBeInTheDocument()
    })
  })

  it('logs a sweep that could not be read', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.lastWorktreeSweep.mockRejectedValue(new Error('backend gone'))

    render(<App />)

    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the latest worktree sweep', expect.any(Error))
    })
    error.mockRestore()
  })

  it('shows settings that could not be loaded', async () => {
    platform.getSettings.mockRejectedValue(new Error('read the preferences: database is locked'))
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not load the launch settings: read the preferences: database is locked',
    )
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })
})
