import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import { brand } from './generated/brand'
import type { Settings, TerminalID } from './platform'
import { sendEvent } from './test/events'
import {
  appInfo,
  checkResult,
  fileCheck,
  item,
  notification,
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
  worktreeCleanup,
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
  on: vi.fn(),
  openExternal: vi.fn(),
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
  recentCleanups: vi.fn(),
  getCollapsedSections: vi.fn(),
  setSectionCollapsed: vi.fn(),
  getTheme: vi.fn(),
  setTheme: vi.fn(),
  checkItemFiles: vi.fn(),
  setSensitiveScope: vi.fn(),
  setSensitiveCategory: vi.fn(),
  setSensitivePatterns: vi.fn(),
  matchSensitivePath: vi.fn(),
  listNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  clearNotifications: vi.fn(),
  notificationBannerStatus: vi.fn(),
  sendTestNotification: vi.fn(),
  setMenuBar: vi.fn(),
  setNotificationBanner: vi.fn(),
}))

vi.mock('./platform', () => platform)

// stored is every setting as the backend stores it after a change, as each setter returns
// them.
function stored(change: Partial<Settings> = {}): Promise<Settings> {
  return Promise.resolve(settings({ defaultProfile: 'work', worktreeDefault: false, ...change }))
}

beforeEach(() => {
  platform.getSettings.mockImplementation(() => stored())
  platform.listProfiles.mockResolvedValue([profile(), profile({ name: 'work' })])
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
  platform.on.mockReturnValue(() => undefined)
  platform.setTerminal.mockImplementation((terminal: TerminalID) => stored({ terminal }))
  platform.setDefaultProfile.mockImplementation((defaultProfile: string) => stored({ defaultProfile }))
  platform.setWorktreeDefault.mockImplementation((worktreeDefault: boolean) => stored({ worktreeDefault }))
  platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
  platform.getCollapsedSections.mockResolvedValue([])
  platform.setSectionCollapsed.mockResolvedValue(settings())
  platform.getTheme.mockResolvedValue('system')
  platform.setTheme.mockResolvedValue(settings())
  platform.getAppInfo.mockResolvedValue(appInfo())
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
  platform.setTrustedBots.mockResolvedValue(settings())
  platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 0 }))
  platform.recentCleanups.mockResolvedValue([])
  platform.listNotifications.mockResolvedValue({ items: [], unread: 0 })
  platform.markNotificationsRead.mockResolvedValue(undefined)
  platform.clearNotifications.mockResolvedValue(undefined)
  platform.notificationBannerStatus.mockResolvedValue('on')
  platform.sendTestNotification.mockResolvedValue(undefined)
  platform.setMenuBar.mockImplementation((menuBar: boolean) => stored({ menuBar }))
  platform.setNotificationBanner.mockResolvedValue(settings())
})

// rowOptions opens the inbox row's launch options and returns queries scoped to them.
async function rowOptions() {
  await userEvent.click(await screen.findByRole('button', { name: /^Launch options/ }))

  return within(screen.getByRole('dialog', { name: 'Launch options' }))
}

// openSettingsFromMenu opens the settings page the way a person would: the account menu.
// The page's code loads on its first opening, so this waits for the page.
async function openSettingsFromMenu() {
  await userEvent.click(await screen.findByRole('button', { name: 'Account: octo-user' }))
  await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))
  await screen.findByRole('navigation', { name: 'Settings sections' })
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

    expect(await screen.findByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('keeps the inbox lists collapsed the way they were left, across views', async () => {
    platform.getCollapsedSections.mockResolvedValue(['assigned'])
    render(<App />)

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))
    const assigned = within(screen.getByRole('region', { name: 'Assigned' }))
    expect(assigned.getByRole('button', { name: 'Expand Assigned' })).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(review.getByRole('button', { name: 'Collapse Review requested' }))
    expect(platform.setSectionCollapsed).toHaveBeenCalledWith('reviewRequested', true)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    const back = within(await screen.findByRole('region', { name: 'Review requested' }))
    expect(back.getByRole('button', { name: 'Expand Review requested' })).toBeInTheDocument()
    expect(
      within(screen.getByRole('region', { name: 'Assigned' })).getByRole('button', { name: 'Expand Assigned' }),
    ).toBeInTheDocument()
    expect(platform.getCollapsedSections).toHaveBeenCalledOnce()
  })

  it('keeps the inbox as it was left, a list’s tab included, while another view shows', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([
          item(),
          item({ id: 2, number: 8, title: 'Bump the parser', dismissedAt: '2026-06-14T09:00:00Z' }),
        ]),
      }),
    )
    render(<App />)

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))
    await userEvent.click(review.getByRole('tab', { name: 'Dismissed 1' }))

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    expect(await screen.findByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    const back = within(screen.getByRole('region', { name: 'Review requested' }))
    expect(back.getByRole('tab', { name: 'Dismissed 1' })).toHaveAttribute('aria-selected', 'true')
    expect(back.getByRole('button', { name: 'Bump the parser' })).toBeInTheDocument()
    expect(platform.getInboxSnapshot).toHaveBeenCalledOnce()
  })

  it('finds an inbox emptied while another view showed at nil, without the party for emptying it', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([item()]), lastRefreshedAt: '2026-06-15T11:55:00Z' }),
    )
    render(<App />)
    await screen.findByRole('button', { name: 'Add the widget cache' })

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'Worktrees' })

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:58:00Z' }))
    act(() => {
      sendEvent(platform.on, 'inbox:updated')
    })
    await vi.waitFor(() => {
      expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(2)
    })

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByTestId('nil-badge')).toBeInTheDocument()
    expect(screen.queryByText('Achievement unlocked')).not.toBeInTheDocument()
  })

  it('lists the worktrees again each time their view opens', async () => {
    render(<App />)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'example-repo' })
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
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

  it('shows the stored theme and switches it from Settings → Appearance, without reading the inbox again', async () => {
    platform.getTheme.mockResolvedValue('dark')
    render(<App />)
    await screen.findByRole('button', { name: 'Open in Claude' })

    await vi.waitFor(() => {
      expect(document.documentElement.dataset.theme).toBe('dark')
    })

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Appearance' }))
    expect(screen.getByRole('radio', { name: 'Dark' })).toBeChecked()

    const inboxReads = platform.getInboxSnapshot.mock.calls.length
    const resolves = platform.resolveLaunches.mock.calls.length

    await userEvent.click(screen.getByRole('radio', { name: 'Light' }))

    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(screen.getByRole('radio', { name: 'Light' })).toBeChecked()
    expect(platform.setTheme).toHaveBeenCalledExactlyOnceWith('light')
    expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(inboxReads)
    expect(platform.resolveLaunches).toHaveBeenCalledTimes(resolves)
  })

  it('opens Settings → Sensitive files from a card’s sensitive files, and comes back', async () => {
    const outside = item({ trusted: false, trust: 'outside', files: fileCheck() })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([outside]) }))
    render(<App />)

    await userEvent.click(await screen.findByRole('button', { name: /^Changes sensitive files/ }))
    await userEvent.click(screen.getByRole('button', { name: 'What counts as sensitive' }))

    expect(screen.getByRole('region', { name: 'Sensitive files' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sensitive files' })).toHaveAttribute('aria-current', 'page')

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('snoozes for the length chosen in Settings, reached from the snooze menu', async () => {
    platform.setSnoozeDays.mockImplementation((snoozeDays: number) => stored({ snoozeDays }))
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

  it('keeps a saved setting that neither launches nor the inbox read, reading nothing again', async () => {
    platform.setSnoozeDays.mockImplementation((snoozeDays: number) => stored({ snoozeDays }))
    render(<App />)
    await screen.findByRole('button', { name: 'Launch options: Review · work' })

    const inboxReads = platform.getInboxSnapshot.mock.calls.length
    const settingsReads = platform.getSettings.mock.calls.length

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Snoozing' }))
    await userEvent.click(screen.getByRole('radio', { name: '1w' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    expect(await screen.findByRole('button', { name: 'Snooze for 1 week' })).toBeInTheDocument()
    expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(inboxReads)
    expect(platform.getSettings).toHaveBeenCalledTimes(settingsReads)
    expect(platform.resolveLaunches).toHaveBeenCalledOnce()
  })

  it('reads the inbox again when the trusted bots change, so the cards relabel', async () => {
    const bot = item({ author: 'dependabot', trust: 'outside', trusted: false, authorIsBot: true })
    platform.getSettings.mockResolvedValue(settings({ trustedBots: [] }))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([bot]) }))
    render(<App />)

    expect(await screen.findByText('untrusted bot')).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Trust' }))

    platform.setTrustedBots.mockResolvedValue(settings({ trustedBots: ['dependabot'] }))
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

  it('counts the worktrees on the Worktrees tab, marks those worth a look, and toasts an automatic cleanup', async () => {
    const stop = vi.fn()
    platform.on.mockImplementation((event: string) => (event === 'worktrees:updated' ? stop : () => undefined))
    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 1, leftover: 2, worktreeCount: 5 }))

    const { unmount } = render(<App />)

    const tab = screen.getByRole('button', { name: 'Worktrees' })
    await vi.waitFor(() => {
      expect(tab).toHaveTextContent('Worktrees5')
    })
    expect(tab).toHaveAccessibleDescription('5 linked worktrees, 2 done on GitHub but still on disk')
    expect(within(tab).getByTestId('worktree-attention')).toBeInTheDocument()
    expect(screen.queryByText(/Cleaned up/)).not.toBeInTheDocument()

    // A sweep removed two worktrees, one of them for an item.
    platform.lastWorktreeSweep.mockResolvedValue(
      sweepReport({
        seq: 2,
        leftover: 1,
        worktreeCount: 3,
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
      sendEvent(platform.on, 'worktrees:updated')
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up 2 finished worktrees')).toBeInTheDocument()
    expect(
      screen.getByText(/acme-org\/example-repo#7, ~\/projects\/example-repo\/\.claude\/worktrees\/pr-8/),
    ).toBeInTheDocument()
    expect(tab).toHaveTextContent('Worktrees3')
    expect(tab).toHaveAccessibleDescription('3 linked worktrees, 1 done on GitHub but still on disk')

    // An event for a sweep already read changes nothing; a later one with one cleaned
    // worktree says so.
    await act(async () => {
      sendEvent(platform.on, 'worktrees:updated')
      await Promise.resolve()
    })

    platform.lastWorktreeSweep.mockResolvedValue(
      sweepReport({
        seq: 3,
        worktreeCount: 2,
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
      sendEvent(platform.on, 'worktrees:updated')
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    expect(tab).toHaveTextContent('Worktrees2')
    expect(tab).toHaveAccessibleDescription('2 linked worktrees')
    expect(within(tab).queryByTestId('worktree-attention')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByText('Cleaned up a finished worktree')).not.toBeInTheDocument()

    unmount()
    expect(stop).toHaveBeenCalledOnce()
  })

  it('counts from the Worktrees view and lists again after a sweep', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ worktreeCount: 6, finishedCount: 2, reviewCount: 1 }))

    render(<App />)
    const tab = screen.getByRole('button', { name: 'Worktrees' })
    await userEvent.click(tab)

    await vi.waitFor(() => {
      expect(tab).toHaveTextContent('Worktrees6')
    })
    expect(tab).toHaveAccessibleDescription('6 linked worktrees, 3 done on GitHub but still on disk')
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 4, leftover: 0, worktreeCount: 0 }))
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ worktreeCount: 0, repos: [] }))
    await act(async () => {
      sendEvent(platform.on, 'worktrees:updated')
      await Promise.resolve()
    })

    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(within(tab).queryByText(/\d/)).not.toBeInTheDocument()
    })
    expect(tab).not.toHaveAttribute('title')
  })

  it('counts the items in the inbox on the Inbox tab, each once, as the inbox changes', async () => {
    const mine = item({ id: 2, number: 8, title: 'Tidy the cache keys' })
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([item()]), authored: list([mine]), assigned: list([mine]) }),
    )

    render(<App />)
    const tab = screen.getByRole('button', { name: 'Inbox' })

    await vi.waitFor(() => {
      expect(tab).toHaveTextContent('Inbox2')
    })
    expect(tab).toHaveAccessibleDescription('2 items in the inbox')

    const dismissed = { ...mine, dismissedAt: '2026-06-15T12:00:00Z' }
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([item()]), authored: list([dismissed]), assigned: list([dismissed]) }),
    )
    await act(async () => {
      sendEvent(platform.on, 'inbox:updated')
      await Promise.resolve()
    })

    await vi.waitFor(() => {
      expect(tab).toHaveTextContent('Inbox1')
    })
    expect(tab).toHaveAccessibleDescription('1 item in the inbox')
  })

  it("opens the recently cleaned-up list from a cleanup toast's View", async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    const cleaned = {
      repoPath: '/home/user/projects/example-repo',
      path: '/home/user/projects/example-repo/.claude/worktrees/pr-7',
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
      removed: true,
      deletedBranch: 'worktree-pr-7',
      item: 'acme-org/example-repo#7',
      reason: '',
    }
    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 1, cleaned: [cleaned] }))
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])

    render(<App />)

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'View' }))

    expect(screen.queryByText('Cleaned up a finished worktree')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'true')
    expect(await screen.findByRole('region', { name: 'Recently cleaned up' })).toBeInTheDocument()
    await vi.waitFor(() => {
      expect(scroll).toHaveBeenCalledOnce()
    })

    // Back through the tabs, the view opens at the top again.
    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    expect(await screen.findByRole('region', { name: 'Recently cleaned up' })).toBeInTheDocument()
    expect(scroll).toHaveBeenCalledOnce()
  })

  it('offers no View on a cleanup toast while the Worktrees view is open', async () => {
    render(<App />)
    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'example-repo' })

    platform.lastWorktreeSweep.mockResolvedValue(
      sweepReport({
        seq: 2,
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
      sendEvent(platform.on, 'worktrees:updated')
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'View' })).not.toBeInTheDocument()
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

  it('says when the data could not be read and was set aside, until dismissed', async () => {
    platform.getAppInfo.mockResolvedValue(
      appInfo({ dataReset: { path: '~/data/app.db.broken-20260102T030405Z', reason: 'corrupt' } }),
    )
    render(<App />)

    expect(
      await screen.findByText(
        (_, el) =>
          el?.tagName === 'P' &&
          el.textContent ===
            `${brand.name} couldn’t read its data, so it set it aside at ~/data/app.db.broken-20260102T030405Z and started fresh.`,
      ),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss notice' }))

    expect(screen.queryByText(/couldn’t read its data/)).not.toBeInTheDocument()
  })

  it('shows settings that could not be loaded', async () => {
    platform.getSettings.mockRejectedValue(new Error('read the preferences: database is locked'))
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not load the launch settings: read the preferences: database is locked',
    )
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })

  describe('showing an item', () => {
    // reveal sends a request to show an item, as the shell does for a click in the menu
    // bar or on a notification.
    function reveal(itemId: number, url = '') {
      act(() => {
        sendEvent(platform.on, 'inbox:reveal', { itemId, url })
      })
    }

    // row returns the inbox row of the item titled title in the Review requested list.
    async function row(title: string) {
      const review = within(await screen.findByRole('region', { name: 'Review requested' }))

      return review.getByRole('button', { name: title }).closest('li')
    }

    it('opens the inbox at an item the shell asks for, from any view', async () => {
      render(<App />)
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
      await screen.findByRole('region', { name: 'example-repo' })

      reveal(1)

      expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
      expect(platform.openExternal).not.toHaveBeenCalled()
    })

    it('opens an item no longer in the inbox on GitHub, and ignores one it cannot find', async () => {
      render(<App />)
      await screen.findByRole('region', { name: 'Review requested' })

      reveal(99, 'https://github.com/acme-org/example-repo/pull/99')
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/99')

      reveal(98)
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/99')
    })

    it('does not show the item again when the inbox is opened later', async () => {
      render(<App />)
      await screen.findByRole('region', { name: 'Review requested' })

      reveal(1)
      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')

      await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
      await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

      expect(await row('Add the widget cache')).not.toHaveAttribute('data-highlighted')
    })

    it('opens an entry of the notification history', async () => {
      platform.listNotifications.mockResolvedValue({ items: [notification({ itemId: 1 })], unread: 1 })
      render(<App />)
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(await screen.findByRole('button', { name: 'Notifications, 1 unread' }))
      expect(platform.markNotificationsRead).toHaveBeenCalledOnce()

      const history = within(screen.getByRole('dialog', { name: 'Notifications' }))
      await userEvent.click(history.getByRole('button', { name: /Add the widget cache/ }))

      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
    })

    it('opens the notification settings from the bell', async () => {
      render(<App />)
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
      await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))

      expect(await screen.findByRole('region', { name: 'Notifications' })).toBeInTheDocument()
      expect(screen.getByRole('switch', { name: 'Show in the menu bar' })).toBeChecked()
    })
  })
})
