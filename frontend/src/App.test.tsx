import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'

import { brand } from './generated/brand'
import type { Settings, TerminalID, AgentID, RemoveResult, TextSize } from './platform'
import {
  appInfo,
  budgetGauge,
  fileCheck,
  item,
  notification,
  itemResolution,
  linkedItem,
  list,
  mainWorktree,
  profile,
  reading,
  removeResult,
  scriptRun,
  settings,
  snapshot,
  sweepReport,
  worktree,
  worktreeCleanup,
  worktreeRepo,
  worktreesSnapshot,
  agentSession,
  sessionsSnapshot,
} from './test/fixtures'
import { emit, listening, platform, renderApp } from './test/platform'

vi.mock('./platform', () => import('./test/platform').then((fake) => fake.platform))

// FIXED_ISO is the test clock's time, when the session came to its state.
const FIXED_ISO = '2026-06-15T12:00:00Z'

// stored is every setting as the backend stores it after a change, as each setter returns
// them.
function stored(change: Partial<Settings> = {}): Promise<Settings> {
  return Promise.resolve(
    settings({ defaultProfiles: { claude: 'work', codex: 'default' }, worktreeDefault: false, ...change }),
  )
}

// The platform fake answers every call. These answers are the app tests' own: settings
// that launch with the work profile and outside a worktree, and an inbox of one item.
beforeEach(() => {
  platform.getSettings.mockImplementation(() => stored())
  platform.listProfiles.mockResolvedValue([profile(), profile({ name: 'work' })])
  platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
  platform.setTerminal.mockImplementation((terminal: TerminalID) => stored({ terminal }))
  platform.setDefaultProfile.mockImplementation((agent: AgentID, name: string) =>
    stored({ defaultProfiles: { claude: 'work', codex: 'default', [agent]: name } }),
  )
  platform.setWorktreeDefault.mockImplementation((worktreeDefault: boolean) => stored({ worktreeDefault }))
  platform.setMenuBar.mockImplementation((menuBar: boolean) => stored({ menuBar }))
  platform.resolveLaunches.mockResolvedValue([
    itemResolution({
      profile: { value: 'work', source: 'app', inherited: 'work', inheritedSource: 'app' },
      worktree: { value: false, source: 'app', inherited: false, inheritedSource: 'app' },
    }),
  ])
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
    await renderApp()

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'About' }))

    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })

  it('shows the account and the live API budget in the toolbar', async () => {
    // The readings reset at 12:42, after the clock's noon, so they stand as read.
    platform.getBudget.mockResolvedValue(budgetGauge({ graphql: reading(), core: reading({ remaining: 400 }) }))
    await renderApp()

    expect(await screen.findByRole('button', { name: 'Account: octo-user' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^API budget: Critical, 8% left \(REST\)/ })).toHaveClass('text-danger')
  })

  it('opens on the inbox with launch controls from the settings', async () => {
    await renderApp()

    expect(await screen.findByRole('button', { name: 'Launch options: Review · work' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(platform.resolveLaunches).toHaveBeenCalledWith([1])

    const options = await rowOptions()
    expect(options.getByRole('radio', { name: 'work' })).toBeChecked()
    expect(options.getByRole('checkbox', { name: /Start in a worktree/ })).not.toBeChecked()
  })

  it('follows each item’s agent session on its card, without the worktrees view', async () => {
    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession({ state: 'working', stateAt: FIXED_ISO })]))

    await renderApp()

    const tag = await screen.findByRole('button', { name: /Claude session: working/ })
    expect(tag).toHaveTextContent('Claude · working')
    expect(platform.listWorktrees).not.toHaveBeenCalled()

    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession()]))
    act(() => {
      emit('sessions:updated')
    })

    expect(await screen.findByRole('button', { name: /Claude session: needs approval/ })).toBeInTheDocument()
  })

  it('switches between the inbox and the worktrees view', async () => {
    await renderApp()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(await screen.findByRole('region', { name: 'Worktrees' })).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
  })

  it('keeps the inbox lists collapsed the way they were left, across views', async () => {
    platform.getCollapsedSections.mockResolvedValue(['assigned'])
    await renderApp()

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
    await renderApp()

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
    await renderApp()
    await screen.findByRole('button', { name: 'Add the widget cache' })

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'Worktrees' })

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:58:00Z' }))
    act(() => {
      emit('inbox:updated')
    })
    await vi.waitFor(() => {
      expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(2)
    })

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByTestId('nil-badge')).toBeInTheDocument()
    expect(screen.queryByText('Achievement unlocked')).not.toBeInTheDocument()
  })

  it('lists the worktrees only once their view opens, and again each time it opens', async () => {
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    expect(platform.listWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    await screen.findByRole('region', { name: 'Review requested' })
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'example-repo' })
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('opens the settings page from the account menu and keeps a saved change', async () => {
    await renderApp()
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
    await renderApp()
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

  it('shows the stored text size and switches it from Settings → Appearance, without reading the inbox again', async () => {
    onTestFinished(() => {
      document.documentElement.style.removeProperty('font-size')
      window.history.replaceState(null, '', '/')
    })
    platform.getSettings.mockImplementation(() => stored({ textSize: 'large' }))
    platform.setTextSize.mockImplementation((textSize: TextSize) => stored({ textSize }))
    await renderApp()
    await screen.findByRole('button', { name: 'Open in Claude' })

    await vi.waitFor(() => {
      expect(document.documentElement.style.fontSize).toBe('115%')
    })
    expect(new URLSearchParams(window.location.search).get('textSize')).toBe('large')

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Appearance' }))
    expect(screen.getByRole('radio', { name: '115%' })).toBeChecked()

    const inboxReads = platform.getInboxSnapshot.mock.calls.length
    const resolves = platform.resolveLaunches.mock.calls.length

    await userEvent.click(screen.getByRole('radio', { name: '130%' }))

    await vi.waitFor(() => {
      expect(document.documentElement.style.fontSize).toBe('130%')
    })
    expect(new URLSearchParams(window.location.search).get('textSize')).toBe('larger')
    expect(platform.setTextSize).toHaveBeenCalledExactlyOnceWith('larger')

    await userEvent.click(screen.getByRole('radio', { name: '100%' }))

    await vi.waitFor(() => {
      expect(document.documentElement.style.fontSize).toBe('')
    })
    expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(inboxReads)
    expect(platform.resolveLaunches).toHaveBeenCalledTimes(resolves)
  })

  it('opens Settings → Sensitive files from a card’s sensitive files, and comes back', async () => {
    const outside = item({ trusted: false, trust: 'outside', files: fileCheck() })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([outside]) }))
    await renderApp()

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
    await renderApp()

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
    await renderApp()
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
    await renderApp()
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
    await renderApp()

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
    await renderApp()

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
    await renderApp()

    await userEvent.click((await rowOptions()).getByRole('button', { name: 'Manage' }))

    expect(await screen.findByRole('navigation', { name: 'Prompts' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Prompts' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('opens the settings with ⌘, and returns to the view it came from', async () => {
    await renderApp()

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
    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 1, leftover: 2, worktreeCount: 5 }))

    const { unmount } = await renderApp()

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
            deletedBranches: ['worktree-pr-7'],
            item: 'acme-org/example-repo#7',
            reason: '',
          },
          {
            repoPath: '/home/user/projects/example-repo',
            path: '/home/user/projects/example-repo/.claude/worktrees/pr-8',
            displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
            removed: true,
            deletedBranches: [],
            item: '',
            reason: '',
          },
        ],
      }),
    )
    await act(async () => {
      emit('worktrees:updated')
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
      emit('worktrees:updated')
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
            deletedBranches: [],
            item: 'acme-org/r#9',
            reason: '',
          },
        ],
      }),
    )
    await act(async () => {
      emit('worktrees:updated')
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    expect(tab).toHaveTextContent('Worktrees2')
    expect(tab).toHaveAccessibleDescription('2 linked worktrees')
    expect(within(tab).queryByTestId('worktree-attention')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByText('Cleaned up a finished worktree')).not.toBeInTheDocument()

    expect(listening('worktrees:updated')).toBe(1)
    unmount()
    expect(listening('worktrees:updated')).toBe(0)
  })

  it('opens an inbox item’s worktree from the Worktrees view, and tags the item’s card with it', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree({
                item: linkedItem({ state: 'open' }),
                agent: 'codex',
                lastLaunchedAt: '2026-06-15T09:00:00Z',
                launches: 1,
              }),
              worktree({
                path: '/home/user/projects/example-repo/.claude/worktrees/pr-8',
                displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
                item: linkedItem({ number: 8 }),
              }),
            ],
          }),
        ],
      }),
    )
    await renderApp()
    await screen.findByRole('button', { name: 'Launch options: Review · work' })

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    const repo = within(await screen.findByRole('region', { name: 'example-repo' }))
    const row = (path: string) => within(repo.getByText(path).closest('li') ?? document.body)
    const listed = row('~/projects/example-repo/.claude/worktrees/pr-7')

    expect(listed.getByText(/Codex/)).toHaveTextContent(/Codex \S+ ago$/)
    // Pull request #8 is not in the inbox, so its worktree has no launch control.
    expect(row('~/projects/example-repo/.claude/worktrees/pr-8').queryByRole('button', { name: /^Open/ })).toBeNull()

    // The control sits at the left of the row, so its menu opens from the left edge.
    await userEvent.click(listed.getByRole('button', { name: /^Launch options/ }))
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toHaveClass('left-0')
    await userEvent.keyboard('{Escape}')

    await userEvent.click(listed.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(expect.objectContaining({ itemId: 1, worktree: true }))

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByTitle('~/projects/example-repo/.claude/worktrees/pr-7')).toHaveTextContent(
      /^worktree · Codex \S+ ago$/,
    )
  })

  it('shows a running session’s terminal from its card and its worktree, rather than starting another', async () => {
    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession({ state: 'working', stateAt: FIXED_ISO })]))
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [mainWorktree(), worktree({ item: linkedItem({ state: 'open' }) })],
          }),
        ],
      }),
    )
    await renderApp()

    const card = await screen.findByRole('button', { name: 'Show in terminal' })
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    await userEvent.click(card)
    expect(platform.focusSession).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    const repo = within(await screen.findByRole('region', { name: 'example-repo' }))
    const listed = within(
      repo.getByText('~/projects/example-repo/.claude/worktrees/pr-7').closest('li') ?? document.body,
    )

    await userEvent.click(listed.getByRole('button', { name: 'Show in terminal' }))
    expect(platform.focusSession).toHaveBeenCalledTimes(2)
    expect(platform.launch).not.toHaveBeenCalled()
  })

  it('counts from the Worktrees view and lists again after a sweep', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ worktreeCount: 6, finishedCount: 2, reviewCount: 1 }))

    await renderApp()
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
      emit('worktrees:updated')
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

    await renderApp()
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
      emit('inbox:updated')
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
      deletedBranches: ['worktree-pr-7'],
      item: 'acme-org/example-repo#7',
      reason: '',
    }
    platform.lastWorktreeSweep.mockResolvedValue(sweepReport({ seq: 1, cleaned: [cleaned] }))
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])

    await renderApp()

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

  it('confirms a change made in the Worktrees view with a toast, offering View once another view is open', async () => {
    let finish: (result: RemoveResult) => void = () => undefined
    platform.removeWorktree.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    await renderApp()
    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    const clone = await screen.findByRole('region', { name: 'example-repo' })
    await userEvent.click(within(clone).getByRole('button', { name: 'Remove' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove' }))
    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    act(() => {
      finish(removeResult())
    })

    const toast = await screen.findByRole('group', { name: 'Notification' })
    expect(toast).toHaveTextContent('Removed example-repo/pr-7 · Its branch worktree-pr-7 was deleted.')

    await userEvent.click(within(toast).getByRole('button', { name: 'View' }))
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()

    // A change finished while the view is open needs no View, and closes like any toast.
    platform.removeWorktree.mockResolvedValueOnce(removeResult({ deletedBranches: [] }))
    const remove = within(await screen.findByRole('region', { name: 'example-repo' })).getByRole('button', {
      name: 'Remove',
    })
    await vi.waitFor(() => {
      expect(remove).toBeEnabled()
    })
    await userEvent.click(remove)
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove' }))

    const again = await screen.findByRole('group', { name: 'Notification' })
    expect(again).toHaveTextContent('Removed example-repo/pr-7 · No branch was deleted.')
    expect(within(again).queryByRole('button', { name: 'View' })).not.toBeInTheDocument()

    await userEvent.click(within(again).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()
  })

  it('offers no View on a cleanup toast while the Worktrees view is open', async () => {
    await renderApp()
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
            deletedBranches: [],
            item: 'acme-org/r#9',
            reason: '',
          },
        ],
      }),
    )
    await act(async () => {
      emit('worktrees:updated')
      await Promise.resolve()
    })

    expect(await screen.findByText('Cleaned up a finished worktree')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'View' })).not.toBeInTheDocument()
  })

  it('logs a sweep that could not be read', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.lastWorktreeSweep.mockRejectedValue(new Error('backend gone'))

    await renderApp()

    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the latest worktree sweep', expect.any(Error))
    })
    error.mockRestore()
  })

  it('says when the data could not be read and was set aside, until dismissed', async () => {
    platform.getAppInfo.mockResolvedValue(
      appInfo({ dataReset: { path: '~/data/app.db.broken-20260102T030405Z', reason: 'corrupt' } }),
    )
    await renderApp()

    expect(
      await screen.findByText(
        (_, el) =>
          el?.tagName === 'P' &&
          el.textContent ===
            `${brand.name} couldn’t read its data, so it set it aside at ~/data/app.db.broken-20260102T030405Z and started fresh. ` +
              'To bring back prompts from it, choose Restore from a backup on the Prompts page.',
      ),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss notice' }))

    expect(screen.queryByText(/couldn’t read its data/)).not.toBeInTheDocument()
  })

  it('asks before quitting while a run goes in the app', async () => {
    platform.listScriptRuns.mockResolvedValue([
      scriptRun({ status: 'running', kind: 'release', repo: 'acme-org/sdk', canStop: true }),
    ])
    await renderApp()

    act(() => {
      emit('app:quitRequested')
    })

    const dialog = within(await screen.findByRole('dialog', { name: `Quit ${brand.name}?` }))

    expect(dialog.getByText(/A release is running in acme-org\/sdk\./)).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Stop and quit' }))

    expect(platform.quitAnyway).toHaveBeenCalledOnce()
  })

  it('shows settings that could not be loaded', async () => {
    platform.getSettings.mockRejectedValue(new Error('read the preferences: database is locked'))
    await renderApp()

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
        emit('inbox:reveal', { itemId, url })
      })
    }

    // row returns the inbox row of the item titled title in the Review requested list.
    async function row(title: string) {
      const review = within(await screen.findByRole('region', { name: 'Review requested' }))

      return review.getByRole('button', { name: title }).closest('li')
    }

    it('opens the inbox at an item the shell asks for, from any view', async () => {
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
      await screen.findByRole('region', { name: 'example-repo' })

      reveal(1)

      expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
      expect(platform.openExternal).not.toHaveBeenCalled()
    })

    it('opens an item no longer in the inbox on GitHub, and ignores one it cannot find', async () => {
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      reveal(99, 'https://github.com/acme-org/example-repo/pull/99')
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/99')

      reveal(98)
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/99')
    })

    it('does not show the item again when the inbox is opened later', async () => {
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      reveal(1)
      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')

      await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
      await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

      expect(await row('Add the widget cache')).not.toHaveAttribute('data-highlighted')
    })

    it('opens an entry of the notification history', async () => {
      platform.listNotifications.mockResolvedValue({ items: [notification({ itemId: 1 })], unread: 1 })
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(await screen.findByRole('button', { name: 'Notifications, 1 unread' }))
      expect(platform.markNotificationsRead).toHaveBeenCalledOnce()

      const history = within(screen.getByRole('dialog', { name: 'Notifications' }))
      await userEvent.click(history.getByRole('button', { name: /Add the widget cache/ }))

      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
    })

    it('opens the notification settings from the bell', async () => {
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
      await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))

      expect(await screen.findByRole('region', { name: 'Notifications' })).toBeInTheDocument()
      expect(screen.getByRole('switch', { name: 'Show in the menu bar' })).toBeChecked()
    })
  })
})

describe('App’s projects tip', () => {
  const needsScope = () =>
    snapshot({
      assigned: list([item({ id: 2, kind: 'issue', title: 'Fix the login page' })]),
      projectAccess: 'needs_scope',
    })

  it('offers to show issue projects, and stores that the offer was put away', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    platform.setProjectsTipDismissed.mockResolvedValue(settings({ projectsTipDismissed: true }))
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Don’t show this again' }))

    expect(platform.setProjectsTipDismissed).toHaveBeenCalledWith(true)
    expect(screen.queryByRole('region', { name: /stand in their projects/ })).not.toBeInTheDocument()
  })

  it('offers nothing once the offer was put away', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    platform.getSettings.mockResolvedValue(settings({ projectsTipDismissed: true }))
    await renderApp()

    expect(await screen.findByText('Fix the login page')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: /stand in their projects/ })).not.toBeInTheDocument()
  })

  it('keeps the command in Settings → Account', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    await renderApp()

    await openSettingsFromMenu()

    const projects = screen.getByRole('region', { name: 'Projects on issues' })
    expect(within(projects).getByText('gh auth refresh -s read:project')).toBeInTheDocument()
  })
})

describe('App’s filter field', () => {
  // field is the toolbar's filter, named by what it does on the page.
  const field = (name = 'Filter inbox') => screen.getByRole<HTMLInputElement>('combobox', { name })
  // said is what the field says it matches, in the toolbar the field is in.
  const said = () =>
    within(screen.getByRole('combobox', { name: /^(Filter|Find)/ }).closest('header') as HTMLElement).getByRole(
      'status',
    )

  // inboxShown waits for the inbox's first list.
  async function inboxShown() {
    await screen.findByRole('region', { name: 'Review requested' })
  }

  function reveal(itemId: number) {
    act(() => {
      emit('inbox:reveal', { itemId, url: '' })
    })
  }

  it('focuses on ⌘F or /, with its text selected, but not on ⌃⌘F', async () => {
    await renderApp()
    await inboxShown()

    await userEvent.keyboard('{Control>}{Meta>}f{/Meta}{/Control}')
    expect(field()).not.toHaveFocus()

    await userEvent.keyboard('{Meta>}f{/Meta}')
    expect(field()).toHaveFocus()

    // A slash typed in a field is just a slash.
    await userEvent.keyboard('cache/')
    expect(field()).toHaveValue('cache/')

    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(field()).not.toHaveFocus()

    await userEvent.keyboard('/')
    expect(field()).toHaveFocus()
    expect([field().selectionStart, field().selectionEnd]).toEqual([0, 6])
  })

  it('says what it does on each page, and leaves Prompts to its own search', async () => {
    await renderApp()
    await inboxShown()

    expect(field('Filter inbox')).toHaveAttribute('placeholder', 'Filter inbox')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    expect(field('Filter worktrees')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Prompts' }))
    const library = await screen.findByRole('searchbox', { name: 'Search prompts' })

    expect(screen.queryByRole('combobox', { name: /^(Filter|Find)/ })).not.toBeInTheDocument()

    // ⌘F there finds a prompt.
    await userEvent.keyboard('{Meta>}f{/Meta}')
    expect(library).toHaveFocus()

    await userEvent.keyboard('{Meta>},{/Meta}')
    expect(await screen.findByRole('combobox', { name: 'Find a setting' })).toBeInTheDocument()
  })

  it('filters the inbox, keeping each page’s query while another page shows', async () => {
    await renderApp()
    await inboxShown()

    await userEvent.type(field(), 'zzz')

    expect(await screen.findByText('No items match “zzz”')).toBeInTheDocument()
    expect(said()).toHaveTextContent('No matches')
    // The Inbox tab still counts the whole inbox.
    expect(screen.getByTestId('inbox-count')).toHaveTextContent('1')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    expect(field('Filter worktrees')).toHaveValue('')

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    expect(field()).toHaveValue('zzz')
    expect(screen.getByText('No items match “zzz”')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Clear filter' }))
    expect(field()).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
  })

  it('offers what the inbox can be narrowed by, and adds a card’s repository when clicked', async () => {
    await renderApp()
    await inboxShown()

    await userEvent.click(field())
    expect(screen.getByRole('option', { name: /Pull requests/ })).toHaveTextContent('Pull requestsis:pr1')

    await userEvent.click(screen.getByRole('button', { name: 'Filter by repository acme-org/example-repo' }))
    expect(field()).toHaveValue('repo:acme-org/example-repo')
    expect(said()).toHaveTextContent('1 match')

    await userEvent.click(screen.getByRole('button', { name: 'Filter by repository acme-org/example-repo' }))
    expect(field()).toHaveValue('')
  })

  it('clears the inbox’s filter for an item the shell asks to show, and finds it', async () => {
    await renderApp()
    await inboxShown()

    await userEvent.type(field(), 'zzz')
    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    reveal(1)

    expect(field()).toHaveValue('')

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))

    expect(review.getByRole('button', { name: 'Add the widget cache' }).closest('li')).toHaveAttribute(
      'data-highlighted',
      'true',
    )
  })

  it('filters the worktrees, counting from their listing', async () => {
    await renderApp()
    await inboxShown()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'example-repo' })

    await userEvent.click(field('Filter worktrees'))
    expect(screen.getByRole('listbox', { name: 'Filters' })).toBeInTheDocument()

    await userEvent.keyboard('pr-7')
    expect(said()).toHaveTextContent('1 match')
  })

  it('forgets what was being found in the settings by every way out of them', async () => {
    await renderApp()
    await inboxShown()

    const find = async () => {
      await userEvent.keyboard('{Meta>},{/Meta}')
      await userEvent.type(await screen.findByRole('combobox', { name: 'Find a setting' }), 'theme')
      expect(said()).not.toHaveTextContent(/^$/)
    }
    const empty = async () => {
      await userEvent.keyboard('{Meta>},{/Meta}')
      expect(await screen.findByRole('combobox', { name: 'Find a setting' })).toHaveValue('')
    }

    // The back button.
    await find()
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    await empty()

    // A toolbar tab.
    await userEvent.type(field('Find a setting'), 'theme')
    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await empty()

    // A reveal.
    await userEvent.type(field('Find a setting'), 'theme')
    reveal(1)
    await empty()

    // Escape: it clears the field first, then leaves it, then closes the settings.
    await userEvent.type(field('Find a setting'), 'theme')
    await userEvent.keyboard('{Escape}')
    expect(field('Find a setting')).toHaveValue('')
    await userEvent.type(field('Find a setting'), 'theme')
    await userEvent.click(screen.getByRole('navigation', { name: 'Settings sections' }))
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('navigation', { name: 'Settings sections' })).not.toBeInTheDocument()
    await empty()
  })
})
