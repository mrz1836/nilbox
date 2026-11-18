import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'

import { brand } from './generated/brand'
import type {
  AutopilotTarget,
  Settings,
  TerminalID,
  AgentID,
  RemoveResult,
  TextSize,
  InboxItem,
  PictureBot,
  CardDensity,
} from './platform'
import {
  afterMerge,
  appInfo,
  goPreCommitTool,
  integrationsConfig,
  integrationTool,
  integrationTools,
  reviewResult,
  budgetGauge,
  fileCheck,
  githubThread,
  githubThreadList,
  githubThreads,
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
  autopilotAlert,
  autopilotMatch,
  autopilotOff,
  autopilotTarget,
  activityPage,
  avatarDataURL,
  routine,
  details,
  heldOnGitHub,
  review,
} from './test/fixtures'
import { emit, listening, mocks, platform, renderApp } from './test/platform'

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

// The views App loads apart from its first paint are imported once before the tests, so no
// test pays a view's cold, instrumented import inside the second a findBy waits: on a busy
// runner, the first test to open Settings otherwise ran out of that second, whichever test
// it was.
beforeAll(async () => {
  await Promise.all([
    import('./components/notifications/NotificationsView'),
    import('./components/PromptsView'),
    import('./components/scripts/ScriptsView'),
    import('./components/SettingsView'),
    import('./components/WorktreesView'),
    import('./components/card/CardGuideDialog'),
  ])
})

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

// chooseCardDensity picks a density in Settings → Cards, as a person would, and closes the
// settings.
async function chooseCardDensity(density: 'Comfortable' | 'Compact') {
  await openSettingsFromMenu()
  await userEvent.click(screen.getByRole('button', { name: 'Cards' }))
  await userEvent.click(
    within(screen.getByRole('radiogroup', { name: 'Density' })).getByRole('radio', { name: density }),
  )
  await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
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

    const pill = await screen.findByRole('button', { name: /Claude session: working/ })
    expect(pill).toHaveTextContent(/^Claude working/)
    expect(platform.listWorktrees).not.toHaveBeenCalled()

    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession()]))
    act(() => {
      emit('sessions:updated')
    })

    const terminal = await screen.findByRole('button', { name: 'Show terminal' })
    expect(terminal.closest('li')).toHaveAttribute('data-rule', 'session_needs_you')
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

  it('keeps the toolbar in the window however far a page scrolls, scrolling only the page area', async () => {
    await renderApp()
    await openSettingsFromMenu()

    // jsdom lays nothing out, so this pins the structure the layout relies on: the toolbar
    // and the page area are the window column's own, the column is the window's height and
    // holds and clips everything positioned inside it, and only the page area scrolls.
    const toolbar = screen.getByRole('banner')
    const column = toolbar.parentElement
    const pages = screen.getByRole('main')
    expect(column).toHaveClass('relative', 'flex', 'h-screen', 'flex-col', 'overflow-clip')
    expect(pages.parentElement).toBe(column)
    expect(pages).toHaveClass('min-h-0', 'flex-1', 'overflow-y-auto')
    expect(pages).toContainElement(screen.getByRole('navigation', { name: 'Settings sections' }))

    // Nothing that holds the toolbar can be scrolled, by a person or by scrollIntoView.
    for (let el = column; el !== null; el = el.parentElement) {
      expect(el.className).not.toMatch(/\boverflow-(?:[xy]-)?(?:auto|scroll|hidden)\b/)
    }
  })

  it('keeps the inbox lists collapsed the way they were left, across views', async () => {
    // Yours holds an item too, so it shows: a list with nothing in it hides.
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([item()]),
        yours: list([item({ id: 2, kind: 'issue', number: 31, title: 'Crash on start' })]),
      }),
    )
    platform.getCollapsedSections.mockResolvedValue(['yours'])
    await renderApp()

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))
    const yours = within(screen.getByRole('region', { name: 'Yours' }))
    expect(yours.getByRole('button', { name: 'Expand Yours' })).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(review.getByRole('button', { name: 'Collapse Review requested' }))
    expect(platform.setSectionCollapsed).toHaveBeenCalledWith('reviewRequested', true)

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    const back = within(await screen.findByRole('region', { name: 'Review requested' }))
    expect(back.getByRole('button', { name: 'Expand Review requested' })).toBeInTheDocument()
    expect(
      within(screen.getByRole('region', { name: 'Yours' })).getByRole('button', { name: 'Expand Yours' }),
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

    await userEvent.click(await screen.findByRole('button', { name: 'Outside · Agent config · Workflows' }))
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

    expect(await screen.findByText('Untrusted bot')).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Trust' }))

    platform.setTrustedBots.mockResolvedValue(settings({ trustedBots: ['dependabot'] }))
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...bot, trust: 'bot', trusted: true }]) }),
    )

    await userEvent.type(screen.getByRole('textbox', { name: 'Bot to trust' }), 'dependabot')
    await userEvent.click(screen.getByRole('button', { name: 'Trust bot' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    await waitFor(() => {
      expect(screen.queryByText('Untrusted bot')).not.toBeInTheDocument()
    })
    expect(document.querySelector('[data-avatar="bot"]')).toBeInTheDocument()
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
                dirty: true,
                dirtyCount: 2,
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

    // The row's launch options open in the dialog every launch control opens, worktree on.
    await userEvent.click(listed.getByRole('button', { name: /^Launch options/ }))
    const options = within(screen.getByRole('dialog', { name: 'Launch options' }))
    expect(options.getByRole('checkbox', { name: /Start in a worktree/ })).toBeChecked()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Launch options' })).not.toBeInTheDocument()

    await userEvent.click(listed.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(expect.objectContaining({ itemId: 1, worktree: true }))

    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByTitle('~/projects/example-repo/.claude/worktrees/pr-7')).toHaveTextContent(/^2 changes$/)
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

  it('counts the items in the inbox on the Inbox tab, as the inbox changes', async () => {
    const mine = item({ id: 2, number: 8, title: 'Tidy the cache keys', authored: true, assigned: true })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]), yours: list([mine]) }))

    await renderApp()
    const tab = screen.getByRole('button', { name: 'Inbox' })

    await vi.waitFor(() => {
      expect(tab).toHaveTextContent('Inbox2')
    })
    expect(tab).toHaveAccessibleDescription('2 items in the inbox')

    const dismissed = { ...mine, dismissedAt: '2026-06-15T12:00:00Z' }
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]), yours: list([dismissed]) }))
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
        emit('inbox:reveal', { itemId, url, runId: 0, runStarted: '', notificationId: 0, autopilot: autopilotTarget() })
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

      await userEvent.click(await screen.findByRole('button', { name: '1 unread notifications' }))
      expect(platform.markNotificationsRead).not.toHaveBeenCalled()
      expect(platform.markAllNotificationsRead).not.toHaveBeenCalled()

      const history = within(screen.getByRole('dialog', { name: 'Notifications' }))
      await userEvent.click(history.getByRole('button', { name: /^(?!Done: ).*Add the widget cache/ }))

      expect(await row('Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
      expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([1])
    })

    it('opens the notification settings from the bell', async () => {
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      await userEvent.click(screen.getByRole('button', { name: 'No unread notifications' }))
      await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))

      expect(await screen.findByRole('region', { name: 'Notifications' })).toBeInTheDocument()
      expect(screen.getByRole('switch', { name: 'Show in the menu bar' })).toBeChecked()
    })
  })
})

describe('App’s card pictures', () => {
  // dependabotURL is a bot's picture on the avatar host, as GitHub gives it.
  const dependabotURL = 'https://avatars.githubusercontent.com/in/29110?v=4'

  // picture is a stand-in data: URL for what.
  function picture(what: string): string {
    return `data:image/png;base64,${btoa(what)}`
  }

  // authored is 59 items opened by 59 people, person-00 and on, and one by dependabot: 60
  // authors in Review requested, item id n + 1 opened by person-n.
  function authored(): InboxItem[] {
    const people = Array.from({ length: 59 }, (_, i) =>
      item({
        id: i + 1,
        number: i + 1,
        title: `Change ${String(i + 1)}`,
        url: `https://github.com/acme-org/example-repo/pull/${String(i + 1)}`,
        author: `person-${String(i).padStart(2, '0')}`,
      }),
    )

    return [
      ...people,
      item({
        id: 60,
        number: 60,
        title: 'Bump the cache',
        url: 'https://github.com/acme-org/example-repo/pull/60',
        author: 'dependabot',
        authorIsBot: true,
        authorAvatarUrl: dependabotURL,
      }),
    ]
  }

  // Held is a request for the cards' pictures, answered when the test says.
  interface Held {
    people: string[]
    bots: PictureBot[]
    answer: (got: Record<string, string>) => void
  }

  // hold makes every request for the cards' pictures wait for the test, and lists them.
  function hold(): Held[] {
    const held: Held[] = []

    platform.cardPictures.mockImplementation(
      (people, bots) =>
        new Promise((answer) => {
          held.push({ people: people ?? [], bots: bots ?? [], answer })
        }),
    )

    return held
  }

  // pictureOn is the picture on item id's card, or null while it shows none.
  function pictureOn(id: number): HTMLImageElement | null {
    return document.querySelector(`[data-item-id="${String(id)}"] img`)
  }

  it('asks for 60 authors’ pictures in three requests at once, and shows each one’s as it lands', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list(authored()) }))
    const held = hold()
    await renderApp()
    await screen.findByText('Bump the cache')

    await waitFor(() => {
      expect(held).toHaveLength(3)
    })
    expect(held.map((r) => r.people.length + r.bots.length)).toEqual([25, 25, 10])
    expect(held[2]?.bots).toEqual([{ login: 'dependabot', url: dependabotURL }])

    // The last request lands first: its pictures show while the others still download.
    await act(async () => {
      held[2]?.answer({ 'person-58': picture('58'), 'dependabot[bot]': picture('dependabot') })
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(pictureOn(60)).toHaveAttribute('src', picture('dependabot'))
    })
    expect(pictureOn(59)).toHaveAttribute('src', picture('58'))
    expect(pictureOn(1)).toBeNull()

    // A person is a circle, a bot a rounded square, each once it decodes.
    fireEvent.load(pictureOn(59) as HTMLImageElement)
    fireEvent.load(pictureOn(60) as HTMLImageElement)
    expect(pictureOn(59)).toHaveClass('rounded-full')
    expect(pictureOn(60)).toHaveClass('rounded-lg')

    await act(async () => {
      held[0]?.answer({ 'person-00': picture('00') })
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(pictureOn(1)).toHaveAttribute('src', picture('00'))
    })
    expect(platform.cardPictures).toHaveBeenCalledTimes(3)
  })

  it('asks for no picture while Author pictures is off, and shows them once it is turned on', async () => {
    platform.getSettings.mockImplementation(() => stored({ cardAvatars: false }))
    platform.setCardAvatars.mockImplementation((cardAvatars: boolean) => stored({ cardAvatars }))
    platform.cardPictures.mockResolvedValue({ 'octo-user': avatarDataURL })
    await renderApp()
    await screen.findByRole('button', { name: 'Launch options: Review · work' })

    expect(platform.cardPictures).not.toHaveBeenCalled()
    expect(pictureOn(1)).toBeNull()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Cards' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Author pictures' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    await waitFor(() => {
      expect(pictureOn(1)).toHaveAttribute('src', avatarDataURL)
    })
    expect(platform.cardPictures).toHaveBeenCalledExactlyOnceWith(['octo-user'], [])

    // Off again, the pictures go at once, and nothing more is asked.
    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Cards' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Author pictures' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    await waitFor(() => {
      expect(pictureOn(1)).toBeNull()
    })
    expect(platform.cardPictures).toHaveBeenCalledOnce()
  })

  // cancellable makes every request for the cards' pictures wait for the test, and lists
  // each one's answer and its cancel.
  function cancellable() {
    const answers: ((got: Record<string, string>) => void)[] = []
    const cancels: ReturnType<typeof vi.fn>[] = []

    platform.cardPictures.mockImplementation(() => {
      const cancel = vi.fn()
      cancels.push(cancel)

      return Object.assign(
        new Promise<Record<string, string>>((answer) => {
          answers.push(answer)
        }),
        { cancel },
      )
    })

    return { answers, cancels }
  }

  // someAuthored are 26 of authored's items, 25 people's and dependabot's: two requests.
  const someAuthored = () => authored().slice(34)

  // slow is the time a test that renders some authored cards and visits Settings may take
  // on a busy runner, under coverage.
  const slow = { timeout: 15_000 }

  it('asks for no picture while the cards are compact, and in batches once they are comfortable', slow, async () => {
    platform.getSettings.mockImplementation(() => stored({ cardDensity: 'compact' }))
    platform.setCardDensity.mockImplementation((cardDensity: CardDensity) => stored({ cardDensity }))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list(someAuthored()) }))
    const { answers } = cancellable()
    await renderApp()
    await screen.findByText('Bump the cache')

    expect(platform.cardPictures).not.toHaveBeenCalled()

    await chooseCardDensity('Comfortable')

    await waitFor(() => {
      expect(platform.cardPictures).toHaveBeenCalledTimes(2)
    })
    await act(async () => {
      for (const answer of answers) {
        answer({ 'person-34': picture('34') })
      }
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(pictureOn(35)).toHaveAttribute('src', picture('34'))
    })
  })

  it('cancels the requests under way as the cards turn compact, and asks nothing more', slow, async () => {
    platform.setCardDensity.mockImplementation((cardDensity: CardDensity) => stored({ cardDensity }))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list(someAuthored()) }))
    const { cancels } = cancellable()
    await renderApp()
    await screen.findByText('Bump the cache')
    await waitFor(() => {
      expect(platform.cardPictures).toHaveBeenCalledTimes(2)
    })

    await chooseCardDensity('Compact')

    await waitFor(() => {
      expect(cancels[1]).toHaveBeenCalledOnce()
    })
    expect(cancels[0]).toHaveBeenCalledOnce()
    expect(platform.cardPictures).toHaveBeenCalledTimes(2)
  })
})

describe('App’s card density', () => {
  it('shows the cards on one line once Settings → Cards says Compact, and on two again after', async () => {
    platform.setCardDensity.mockImplementation((cardDensity: CardDensity) => stored({ cardDensity }))
    await renderApp()
    await screen.findByRole('button', { name: 'Launch options: Review · work' })

    const card = () => document.querySelector('[data-item-id="1"]')
    expect(card()).not.toHaveAttribute('data-density')

    await chooseCardDensity('Compact')

    await waitFor(() => {
      expect(card()).toHaveAttribute('data-density', 'compact')
    })
    expect(platform.setCardDensity).toHaveBeenCalledExactlyOnceWith('compact')

    await chooseCardDensity('Comfortable')

    await waitFor(() => {
      expect(card()).not.toHaveAttribute('data-density')
    })
  })
})

describe('App’s card guide', () => {
  it('opens the card guide from the inbox and from Settings → Cards, giving focus back as it closes', async () => {
    await renderApp()

    const fromInbox = await screen.findByRole('button', { name: 'How to read a card' })
    await userEvent.click(fromInbox)

    const guide = await screen.findByRole('dialog', { name: 'How to read a card' })
    expect(guide.querySelector('[inert] li[data-item-id]')).not.toBeNull()

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog', { name: 'How to read a card' })).not.toBeInTheDocument()
    expect(fromInbox).toHaveFocus()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Cards' }))
    const fromSettings = screen.getByRole('button', { name: 'Show the guide' })
    await userEvent.click(fromSettings)

    await screen.findByRole('dialog', { name: 'How to read a card' })
    await userEvent.click(screen.getByRole('button', { name: 'Close' }))

    expect(screen.queryByRole('dialog', { name: 'How to read a card' })).not.toBeInTheDocument()
    expect(fromSettings).toHaveFocus()
  })
})

describe('App’s unread dots', () => {
  // dot is the unread dot on the inbox's card, if it shows one.
  const dot = () => document.querySelector('[data-item-id="1"] [data-unread]')

  it('hides the dots once Settings → Cards turns them off, and still marks what the user opens', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item({ unread: true })]) }))
    platform.setCardUnread.mockImplementation((cardUnread: boolean) => stored({ cardUnread }))
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })
    expect(dot()).toHaveAttribute('title', 'New since you last opened it')

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Cards' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Unread dots' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    expect(platform.setCardUnread).toHaveBeenCalledExactlyOnceWith(false)
    await waitFor(() => {
      expect(dot()).toBeNull()
    })

    await userEvent.click(screen.getByRole('button', { name: 'Add the widget cache' }))
    await screen.findByRole('dialog', { name: 'Add the widget cache' })
    await waitFor(() => {
      expect(platform.markItemsSeen).toHaveBeenCalledExactlyOnceWith([1])
    })
  })
})

describe('App’s held workflow runs', () => {
  // approvedByYou is the owner's case: a fork's pull request assigned to the user, who
  // approved it, with 23 checks passed and held runs counted as the backend counts them.
  const approvedByYou = (awaiting: number) =>
    item({
      assigned: true,
      author: 'contributor-a',
      reviewRequest: 'none',
      isCrossRepository: true,
      details: details({
        viewerVerdict: 'approved',
        checks: { state: 'SUCCESS', passed: 23, failed: 0, pending: 0, skipped: 0, awaiting, awaitingOnGitHub: 0 },
      }),
    })
  const card = () => document.querySelector('[data-item-id="1"]')
  const sheetOf = () => screen.findByRole('dialog', { name: 'Add the widget cache' })

  it('reads as approved and ready to merge when its only held run is held on GitHub, and the sheet names it quietly', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ yours: list([approvedByYou(0)]) }))
    platform.refreshReview.mockResolvedValue(review({ unrun: [heldOnGitHub('Auto-merge Trigger', 103)] }))
    await renderApp()

    await screen.findByRole('region', { name: 'Yours' })
    await waitFor(() => {
      expect(card()).toHaveAttribute('data-rule', 'ready')
    })
    expect(card()).toHaveTextContent('Approved · ready to merge')
    expect(within(card() as HTMLElement).getByRole('button', { name: 'Merge' })).toBeInTheDocument()
    expect(
      [...(card()?.querySelectorAll('[data-step]') ?? [])].map(
        (s) => `${s.getAttribute('data-step') ?? ''}:${s.getAttribute('data-state') ?? ''}`,
      ),
    ).toEqual(['checks:passed', 'review:approved', 'merge:mergeable'])

    await userEvent.click(screen.getByRole('button', { name: 'Add the widget cache' }))
    const sheet = await sheetOf()

    expect(await within(sheet).findByRole('list', { name: 'Workflow runs that don’t count' })).toHaveTextContent(
      /^Auto-merge Trigger · held on GitHub · Open on GitHub$/,
    )
    expect(within(sheet).getByText('Ready to merge')).toBeInTheDocument()
    expect(within(sheet).queryByRole('region', { name: 'Workflow runs awaiting approval' })).not.toBeInTheDocument()
  })
})

describe('App’s GitHub notifications', () => {
  // unread are two unread notifications: one about the inbox's pull request #7, one about
  // #9, which the inbox doesn't list.
  const unread = () =>
    githubThreads({
      status: { count: 5, unread: 2 },
      unread: [githubThread(), githubThread({ id: 1002, number: 9, title: 'Bump the parser' })],
    })

  // toolbar is the window's toolbar, the first banner: a page that shows may hold another.
  function toolbar(): HTMLElement {
    const [first] = screen.getAllByRole('banner')

    if (first === undefined) {
      throw new Error('no toolbar')
    }

    return first
  }

  // openButton opens the toolbar's GitHub notifications button, once it shows, and returns
  // the queries scoped to its panel.
  async function openButton() {
    await userEvent.click(await within(toolbar()).findByRole('button', { name: /^GitHub notifications/ }))

    return within(screen.getByRole('dialog', { name: 'GitHub notifications' }))
  }

  // row returns the inbox row of the item titled title in the Review requested list.
  async function row(title: string) {
    const review = within(await screen.findByRole('region', { name: 'Review requested' }))

    return review.getByRole('button', { name: title }).closest('li')
  }

  it('opens a notification on GitHub, as GitHub’s inbox does, and marks it read there', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(unread())
    // GitHub marks the first read, and the other stays unread.
    platform.markGitHubThreadsRead.mockResolvedValueOnce(
      githubThreads({
        status: { count: 5, unread: 1 },
        unread: [githubThread({ id: 1002, number: 9, title: 'Bump the parser' })],
      }),
    )
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    const panel = await openButton()

    await userEvent.click(panel.getByRole('button', { name: /^Review requested.*Add the widget cache/ }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')
    await waitFor(() => {
      expect(platform.markGitHubThreadsRead).toHaveBeenCalledExactlyOnceWith([githubThread().id])
    })

    await userEvent.click(panel.getByRole('button', { name: /^Review requested.*Bump the parser/ }))

    expect(platform.openExternal).toHaveBeenLastCalledWith('https://github.com/acme-org/example-repo/pull/9')
    await waitFor(() => {
      expect(platform.markGitHubThreadsRead).toHaveBeenLastCalledWith([1002])
    })
  })

  it('marks a notification done from its Done, and shows the inbox as GitHub left it', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(unread())
    // GitHub takes the Done, and the mirror, read again after it, no longer lists it.
    platform.markGitHubThreadsDone.mockImplementation(() => {
      const left = githubThreads({
        status: { count: 4, unread: 1 },
        unread: [githubThread({ id: 1002, number: 9, title: 'Bump the parser' })],
      })

      platform.githubThreadsSnapshot.mockResolvedValue(left)

      return Promise.resolve(left)
    })
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    const panel = await openButton()

    await userEvent.click(panel.getByRole('button', { name: 'Done: Add the widget cache' }))

    expect(platform.markGitHubThreadsDone).toHaveBeenCalledExactlyOnceWith([githubThread().id])
    await waitFor(() => {
      expect(panel.queryByText('Add the widget cache')).not.toBeInTheDocument()
    })
    expect(within(toolbar()).getByTestId('github-threads-count')).toHaveTextContent('1')
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('reads only the app’s own mirror as the button opens: it sends GitHub nothing and marks nothing read', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(unread())
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })
    await screen.findByRole('button', { name: /^GitHub notifications/ })

    for (const [, fn] of mocks()) {
      fn.mockClear()
    }

    await openButton()
    await act(async () => {
      await Promise.resolve()
    })

    const called = mocks()
      .filter(([, fn]) => fn.mock.calls.length > 0)
      .map(([name]) => name)

    expect(called).toEqual(['githubThreadsSnapshot'])
  })

  it('leaves the cards out of it: only the toolbar’s button shows the unread notifications', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(unread())
    await renderApp()

    const card = await row('Add the widget cache')

    await waitFor(() => {
      expect(within(toolbar()).getByTestId('github-threads-count')).toHaveTextContent('2')
    })
    expect(card).not.toHaveTextContent(/on GitHub/)
  })

  it('turns automatic clearing on from the button, keeps the settings it stored, and offers it no more', async () => {
    platform.setGitHubNotificationSwitch.mockImplementation((name, on) =>
      stored({ githubNotifications: { ...settings().githubNotifications, [name]: on } }),
    )
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    await userEvent.click((await openButton()).getByRole('button', { name: 'Turn on automatic clearing' }))

    await waitFor(() => {
      expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('auto', true)
    // The check goes out as the setting is saved, before the window renders what it stored.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Turn on automatic clearing' })).not.toBeInTheDocument()
    })
  })

  it('opens Settings → GitHub notifications from the button', async () => {
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    await userEvent.click((await openButton()).getByRole('button', { name: 'Settings' }))

    expect(await screen.findByRole('region', { name: 'GitHub notifications' })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Read my GitHub notifications' })).toBeChecked()
  })

  it('hides the button while the app doesn’t read them', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { reading: false } }))
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()
  })

  // toolbarButton is the toolbar's GitHub notifications button, or null while it is hidden.
  function toolbarButton() {
    return within(toolbar()).queryByRole('button', { name: /^GitHub notifications/ })
  }

  // rulesStored answers the GitHub notification setters as the backend stores them, on top
  // of the app tests' own settings.
  function rulesStored() {
    platform.setGitHubNotificationSwitch.mockImplementation((name, on) =>
      stored({ githubNotifications: { ...settings().githubNotifications, [name]: on } }),
    )
  }

  // openGitHubSettings opens Settings → GitHub notifications from the button.
  async function openGitHubSettings() {
    await userEvent.click((await openButton()).getByRole('button', { name: 'Settings' }))
    await screen.findByRole('region', { name: 'GitHub notifications' })
  }

  it('hides the button as soon as reading is turned off, and reads the mirror again', async () => {
    rulesStored()
    // The mirror still says it reads them: nothing tells the window otherwise yet.
    platform.githubThreadsSnapshot.mockResolvedValue(unread())
    await renderApp()
    await waitFor(() => {
      expect(toolbarButton()).toHaveAccessibleName('GitHub notifications, 2 unread, 5 in your inbox')
    })
    await openGitHubSettings()
    const reads = platform.githubThreadsSnapshot.mock.calls.length

    await userEvent.click(screen.getByRole('switch', { name: 'Read my GitHub notifications' }))

    await waitFor(() => {
      expect(toolbarButton()).not.toBeInTheDocument()
    })
    expect(platform.githubThreadsSnapshot.mock.calls.length).toBeGreaterThan(reads)
    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()
    expect(toolbarButton()).not.toBeInTheDocument()
  })

  it('shares one mirror between the button and the settings, so Check again in Settings updates both', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 3 } }))
    platform.checkGitHubThreadsNow.mockResolvedValue(unread())
    await renderApp()
    await openGitHubSettings()

    expect(listening('ghthreads:updated')).toBe(1)
    expect(platform.githubThreadsSnapshot).toHaveBeenCalledTimes(2)

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByText(/^5 in your GitHub inbox · checked/)).toBeInTheDocument()
    expect(toolbarButton()).toHaveAccessibleName('GitHub notifications, 2 unread, 5 in your inbox')
    expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
    // Opening the settings read the mirror once, as the button's opening did; nothing else.
    expect(platform.githubThreadsSnapshot).toHaveBeenCalledTimes(2)
  })

  it('stores the button’s automatic clearing and a change made in Settings one at a time, in order', async () => {
    let answer: (value: Settings) => void = () => undefined
    platform.setGitHubNotificationSwitch.mockImplementation((name, on) => {
      if (name === 'auto') {
        return new Promise<Settings>((resolve) => {
          answer = resolve
        })
      }

      // The backend has stored the button's change first, so it holds it too.
      return stored({ githubNotifications: { ...settings().githubNotifications, auto: true, [name]: on } })
    })
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    const panel = await openButton()
    await userEvent.click(panel.getByRole('button', { name: 'Turn on automatic clearing' }))
    await userEvent.click(panel.getByRole('button', { name: 'Settings' }))
    await screen.findByRole('region', { name: 'GitHub notifications' })
    await userEvent.click(screen.getByRole('switch', { name: 'Include repositories you only watch' }))

    // The change in Settings waits for the button's.
    expect(platform.setGitHubNotificationSwitch.mock.calls).toEqual([['auto', true]])

    await act(async () => {
      answer(await stored({ githubNotifications: { ...settings().githubNotifications, auto: true } }))
    })

    await waitFor(() => {
      expect(platform.setGitHubNotificationSwitch.mock.calls).toEqual([
        ['auto', true],
        ['watching', false],
      ])
    })
    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Include repositories you only watch' })).not.toBeChecked()
    })
    expect(screen.getByRole('switch', { name: 'Clear them automatically' })).toBeChecked()
    expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
  })
})

describe('App’s projects tip', () => {
  const needsScope = () =>
    snapshot({
      yours: list([item({ id: 2, kind: 'issue', title: 'Fix the login page' })]),
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
      emit('inbox:reveal', {
        itemId,
        url: '',
        runId: 0,
        runStarted: '',
        notificationId: 0,
        autopilot: autopilotTarget(),
      })
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

describe('the Scripts tab', () => {
  // off lists both tools switched off, so no integration is on.
  const off = () =>
    integrationTools({
      tools: [integrationTool({ switch: 'off', on: false }), goPreCommitTool({ switch: 'off', on: false })],
    })
  // missing lists both tools as a first start does before detection: on automatically,
  // and found nowhere yet.
  const missing = () =>
    integrationTools({
      tools: [
        integrationTool({ on: false, state: 'missing', checking: true }),
        goPreCommitTool({ on: false, state: 'missing', checking: true }),
      ],
    })
  const views = () => within(screen.getByRole('navigation', { name: 'Views' }))
  const toast = () => screen.findByRole('group', { name: 'Notification' })

  it('shows while a tool is on, loads on its first visit, and leaves the toolbar’s field and ⌘F out', async () => {
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Scripts' }))

    expect(await screen.findByRole('navigation', { name: 'Scripts' })).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Scripts' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('combobox', { name: /^(Filter|Find)/ })).not.toBeInTheDocument()

    await userEvent.keyboard('{Meta>}f{/Meta}')
    expect(screen.queryByRole('combobox', { name: /^(Filter|Find)/ })).not.toBeInTheDocument()

    // Kept while another page shows.
    await userEvent.click(views().getByRole('button', { name: 'Inbox' }))
    expect(screen.queryByRole('navigation', { name: 'Scripts' })).not.toBeInTheDocument()
    await userEvent.click(views().getByRole('button', { name: 'Scripts' }))
    expect(screen.getByRole('navigation', { name: 'Scripts' })).toBeInTheDocument()
  })

  it('stays hidden while every integration is off, and lists no runs for its badge', async () => {
    platform.listIntegrations.mockResolvedValue(off())
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    await act(async () => {
      await Promise.resolve()
    })

    expect(views().queryByRole('button', { name: 'Scripts' })).not.toBeInTheDocument()
    expect(platform.listIntegrations).toHaveBeenCalled()
    expect(platform.listScriptRuns).not.toHaveBeenCalled()
  })

  it('shows while Scripts is turned on with no tool on', async () => {
    platform.listIntegrations.mockResolvedValue(off())
    platform.getSettings.mockResolvedValue(
      settings({
        integrations: integrationsConfig({ switches: { 'mage-x': 'off', 'go-pre-commit': 'off', scripts: 'on' } }),
      }),
    )
    await renderApp()

    expect(await views().findByRole('button', { name: 'Scripts' })).toBeInTheDocument()
  })

  it('badges the runs going', async () => {
    platform.listScriptRuns.mockResolvedValue([scriptRun({ id: 2, status: 'running' }), scriptRun()])
    await renderApp()

    const scripts = await views().findByRole('button', { name: 'Scripts' })

    await waitFor(() => {
      expect(scripts).toHaveTextContent('Scripts1')
    })
    expect(scripts).toHaveAccessibleDescription('1 script running')
  })

  it('announces the tools found once, as the tab first appears, and opens it from the toast', async () => {
    platform.listIntegrations.mockResolvedValue(missing())
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    expect(views().queryByRole('button', { name: 'Scripts' })).not.toBeInTheDocument()

    platform.listIntegrations.mockResolvedValue(integrationTools())
    act(() => {
      emit('tools:updated')
    })

    const shown = within(await toast())

    expect(shown.getByText('Found magex and go-pre-commit. They’re in the new Scripts tab.')).toBeInTheDocument()
    await waitFor(() => {
      expect(platform.setIntegrationsAnnounced).toHaveBeenCalledExactlyOnceWith(true)
    })
    await userEvent.click(shown.getByRole('button', { name: 'Open Scripts' }))

    expect(await screen.findByRole('navigation', { name: 'Scripts' })).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()

    // Gone and back again, it isn't announced twice.
    platform.listIntegrations.mockResolvedValue(off())
    act(() => {
      emit('tools:updated')
    })
    await waitFor(() => {
      expect(views().queryByRole('button', { name: 'Scripts' })).not.toBeInTheDocument()
    })
    platform.listIntegrations.mockResolvedValue(integrationTools())
    act(() => {
      emit('tools:updated')
    })

    expect(await views().findByRole('button', { name: 'Scripts' })).toBeInTheDocument()
    expect(screen.queryByText(/in the new Scripts tab/)).not.toBeInTheDocument()
  })

  it('never announces the tab again once a past start did', async () => {
    platform.getSettings.mockResolvedValue(settings({ integrations: integrationsConfig({ announced: true }) }))
    platform.listIntegrations.mockResolvedValue(missing())
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    platform.listIntegrations.mockResolvedValue(integrationTools())
    act(() => {
      emit('tools:updated')
    })

    expect(await views().findByRole('button', { name: 'Scripts' })).toBeInTheDocument()
    expect(screen.queryByText(/in the new Scripts tab/)).not.toBeInTheDocument()
    expect(platform.setIntegrationsAnnounced).not.toHaveBeenCalled()
  })

  it('remembers the Scripts page and the Repo scripts clone picked, for the next start', async () => {
    platform.getSettings.mockResolvedValue(settings({ integrations: integrationsConfig({ lastPage: 'runs' }) }))
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Scripts' }))
    const rail = within(await screen.findByRole('navigation', { name: 'Scripts' }))

    await waitFor(() => {
      expect(rail.getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    })

    await userEvent.click(rail.getByRole('button', { name: 'Repo scripts' }))
    expect(platform.setScriptsLastPage).toHaveBeenLastCalledWith('scripts')

    const picker = await screen.findByRole('combobox', { name: 'Repository' })
    await waitFor(() => {
      expect(platform.setScriptsLastRepo).toHaveBeenLastCalledWith('~/projects/sdk')
    })

    await userEvent.selectOptions(picker, '~/projects/web')
    expect(platform.setScriptsLastRepo).toHaveBeenLastCalledWith('~/projects/web')
  })

  it('still shows the page picked when it can’t be remembered, and logs why', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.setScriptsLastPage.mockRejectedValue(new Error('database gone'))
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Scripts' }))
    const rail = within(await screen.findByRole('navigation', { name: 'Scripts' }))
    await userEvent.click(rail.getByRole('button', { name: /^Runs/ }))

    expect(rail.getByRole('button', { name: /^Runs/ })).toHaveAttribute('aria-current', 'page')
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not remember the Scripts page shown:', 'database gone')
    })
    error.mockRestore()
  })

  it('closes the announcement from its close button', async () => {
    platform.listIntegrations.mockResolvedValue(missing())
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    platform.listIntegrations.mockResolvedValue(integrationTools({ tools: [integrationTool()] }))
    act(() => {
      emit('tools:updated')
    })

    const shown = within(await toast())

    expect(shown.getByText('Found magex. Release is in the new Scripts tab.')).toBeInTheDocument()
    await userEvent.click(shown.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()
  })

  it('says nothing of a tab already there as the window opens', async () => {
    await renderApp()

    await views().findByRole('button', { name: 'Scripts' })

    expect(screen.queryByText(/in the new Scripts tab/)).not.toBeInTheDocument()
  })

  it('shows the inbox once the tab goes away under it', async () => {
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Scripts' }))
    await screen.findByRole('navigation', { name: 'Scripts' })

    platform.listIntegrations.mockResolvedValue(off())
    act(() => {
      emit('tools:updated')
    })

    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeVisible()
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('opens Settings → Integrations from the tab', async () => {
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Scripts' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Manage in Settings' }))

    expect(await screen.findByRole('heading', { name: 'Integrations', level: 2 })).toBeInTheDocument()
  })

  it('opens the release a merge offers, from the toast that says it merged', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ yours: list([item()]) }))
    platform.actOnReview.mockResolvedValue(
      reviewResult({ outcome: 'merged', message: 'Merged acme-org/example-repo#7' }),
    )
    platform.offerReleaseAfterMerge.mockResolvedValue(afterMerge())
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })

    await userEvent.click(await within(sheet).findByRole('button', { name: 'More review actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Approve' }))
    await userEvent.click(within(sheet).getByRole('button', { name: 'Approve' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Release acme-org/sdk…' }))

    expect(await screen.findByRole('dialog', { name: 'Release acme-org/sdk' })).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Scripts' })).toHaveAttribute('aria-pressed', 'true')
    expect(platform.prepareRelease).toHaveBeenCalledWith(
      expect.objectContaining({ repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: 'master', refresh: true }),
    )
  })

  it('opens a notification about no item, a release, on GitHub', async () => {
    const released = notification({
      kind: 'release_done',
      itemId: 0,
      itemKind: '',
      repo: 'acme-org/sdk',
      number: 0,
      title: 'v1.4.3 is on GitHub',
      url: 'https://github.com/acme-org/sdk/releases/tag/v1.4.3',
      detail: 'Tagged a1b2c3d on master.',
    })

    platform.listNotifications.mockResolvedValue({ items: [released], unread: 1 })
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: '1 unread notifications' }))

    const entry = screen.getByRole('button', { name: /^(?!Done: ).*v1\.4\.3 is on GitHub/ })

    expect(entry).toHaveTextContent('Released')
    expect(entry).toHaveTextContent('sdk · Tagged a1b2c3d on master.')
    expect(entry).not.toHaveTextContent('#0')
    expect(entry).toHaveAttribute('title', 'acme-org/sdk')

    await userEvent.click(entry)
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/sdk/releases/tag/v1.4.3')
  })

  // failedRun is run 7, checks that failed in acme-org/example-repo, and failedEntry its
  // notification, which names the run and when it started.
  const failedRun = () => scriptRun({ id: 7, kind: 'checks', title: 'Checks · all files', status: 'failed' })
  const failedEntry = () =>
    notification({
      kind: 'run_failed',
      itemId: 0,
      itemKind: '',
      number: 0,
      title: 'Checks failed: Checks · all files',
      url: '',
      detail: '4 passed, 1 failed: whitespace.',
      runId: 7,
      runStarted: '2026-11-10T16:00:00Z',
    })

  // openFailedEntry opens the bell and clicks the failed run's entry.
  async function openFailedEntry() {
    await userEvent.click(await screen.findByRole('button', { name: '1 unread notifications' }))

    const entry = screen.getByRole('button', { name: /^(?!Done: ).*Checks failed: Checks · all files/ })

    expect(entry).toHaveTextContent('Run failed')
    expect(entry).toHaveTextContent('example-repo · 4 passed, 1 failed: whitespace.')
    await userEvent.click(entry)
  }

  it('opens a failed run’s notification at the run, in Scripts → Runs', async () => {
    platform.listScriptRuns.mockResolvedValue([failedRun()])
    platform.getScriptRun.mockResolvedValue(failedRun())
    platform.listNotifications.mockResolvedValue({ items: [failedEntry()], unread: 1 })
    await renderApp()

    await openFailedEntry()

    expect(await screen.findByRole('heading', { name: 'Checks · all files', level: 2 })).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Scripts' })).toHaveAttribute('aria-pressed', 'true')
    expect(
      within(screen.getByRole('navigation', { name: 'Scripts' })).getByRole('button', { name: /^Runs/ }),
    ).toHaveAttribute('aria-current', 'page')
    expect(platform.getScriptRun).toHaveBeenCalledWith(7)
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('opens the run a failed run’s banner asks for', async () => {
    platform.listScriptRuns.mockResolvedValue([failedRun()])
    platform.getScriptRun.mockResolvedValue(failedRun())
    await renderApp()

    await views().findByRole('button', { name: 'Scripts' })
    act(() => {
      emit('inbox:reveal', {
        itemId: 0,
        url: '',
        runId: 7,
        runStarted: '2026-11-10T16:00:00Z',
        notificationId: 0,
        autopilot: autopilotTarget(),
      })
    })

    expect(await screen.findByRole('heading', { name: 'Checks · all files', level: 2 })).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Scripts' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('opens no later run that took a failed run’s id, and says the run is gone', async () => {
    // The history was cleared, and a run that started later was given id 7.
    const later = scriptRun({ id: 7, title: 'magex test', startedAt: '2026-11-11T09:00:00Z' })

    platform.listScriptRuns.mockResolvedValue([later])
    platform.getScriptRun.mockResolvedValue(later)
    platform.listNotifications.mockResolvedValue({ items: [failedEntry()], unread: 1 })
    await renderApp()

    await views().findByRole('button', { name: 'Scripts' })
    await openFailedEntry()

    expect(within(await toast()).getByText('That run is no longer in the history.')).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('heading', { name: 'magex test', level: 2 })).not.toBeInTheDocument()
  })

  it('says a failed run’s run is gone once the history no longer holds it', async () => {
    platform.getScriptRun.mockRejectedValue(new Error('the run is no longer recorded: 7'))
    platform.listNotifications.mockResolvedValue({ items: [failedEntry()], unread: 1 })
    await renderApp()

    await views().findByRole('button', { name: 'Scripts' })
    await openFailedEntry()

    expect(within(await toast()).getByText('That run is no longer in the history.')).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('says how to see a failed run while the Scripts tab is gone', async () => {
    platform.listIntegrations.mockResolvedValue(
      integrationTools({
        tools: [integrationTool({ switch: 'off', on: false }), goPreCommitTool({ switch: 'off', on: false })],
      }),
    )
    platform.listNotifications.mockResolvedValue({ items: [failedEntry()], unread: 1 })
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    await openFailedEntry()

    expect(within(await toast()).getByText('Turn on an integration in Settings to see your runs.')).toBeInTheDocument()
    expect(views().queryByRole('button', { name: 'Scripts' })).not.toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(platform.getScriptRun).not.toHaveBeenCalled()

    // The banner's click says so too.
    await userEvent.click(within(await toast()).getByRole('button', { name: 'Close' }))
    act(() => {
      emit('inbox:reveal', {
        itemId: 0,
        url: '',
        runId: 7,
        runStarted: '2026-11-10T16:00:00Z',
        notificationId: 0,
        autopilot: autopilotTarget(),
      })
    })

    expect(within(await toast()).getByText('Turn on an integration in Settings to see your runs.')).toBeInTheDocument()
  })
})

describe('the Notifications page', () => {
  const views = () => within(screen.getByRole('navigation', { name: 'Views' }))
  // review is an unread review request for the inbox's pull request #7, and older a read
  // one about another item.
  const review = () => notification({ id: 3, itemId: 1 })
  const older = () =>
    notification({
      id: 2,
      itemId: 5,
      number: 12,
      title: 'Bump the parser',
      read: true,
      createdAt: '2026-06-14T09:00:00Z',
    })

  it('opens from its tab beside the Inbox, on the tab last chosen, and remembers a choice', async () => {
    platform.getSettings.mockImplementation(() => stored({ notificationsTab: 'github' }))
    platform.listNotifications.mockResolvedValue({ items: [review(), older()], unread: 1 })
    await renderApp()

    const tab = await views().findByRole('button', { name: 'Notifications' })

    await waitFor(() => {
      expect(tab).toHaveTextContent('Notifications1')
    })
    await userEvent.click(tab)

    const tabs = within(await screen.findByRole('tablist', { name: 'Notifications' }))

    expect(tabs.getByRole('tab', { name: 'GitHub' })).toHaveAttribute('aria-selected', 'true')
    expect(platform.githubThreadsList).toHaveBeenCalled()

    await userEvent.click(tabs.getByRole('tab', { name: `${brand.name} 1` }))

    expect(platform.setNotificationsTab).toHaveBeenCalledExactlyOnceWith('app')
    expect(await screen.findByRole('list', { name: 'Yesterday' })).toHaveTextContent('Bump the parser')
  })

  it('lists every GitHub notification only while its GitHub tab shows', async () => {
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    await userEvent.click(views().getByRole('button', { name: 'Notifications' }))
    await screen.findByRole('tablist', { name: 'Notifications' })

    expect(platform.githubThreadsList).not.toHaveBeenCalled()
  })

  it('opens on each quick view’s tab from its View all', async () => {
    platform.listNotifications.mockResolvedValue({ items: [review()], unread: 1 })
    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({ status: { count: 1, unread: 1 }, unread: [githubThread()] }),
    )
    platform.githubThreadsList.mockResolvedValue(githubThreadList({ threads: [githubThread()] }))
    await renderApp()

    await userEvent.click(await screen.findByRole('button', { name: /^GitHub notifications/ }))
    await userEvent.click(screen.getByRole('button', { name: 'View all' }))

    const tabs = within(await screen.findByRole('tablist', { name: 'Notifications' }))

    expect(views().getByRole('button', { name: 'Notifications' })).toHaveAttribute('aria-pressed', 'true')
    expect(tabs.getByRole('tab', { name: 'GitHub 1' })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('list', { name: 'Today' })).toHaveTextContent('Add the widget cache')

    await userEvent.click(screen.getByRole('button', { name: '1 unread notifications' }))
    await userEvent.click(screen.getByRole('button', { name: 'View all' }))

    expect(tabs.getByRole('tab', { name: `${brand.name} 1` })).toHaveAttribute('aria-selected', 'true')
    expect(platform.setNotificationsTab).not.toHaveBeenCalled()
  })

  it('filters with the toolbar’s field, which ⌘F focuses, counting what the list shows', async () => {
    platform.listNotifications.mockResolvedValue({ items: [review(), older()], unread: 1 })
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Notifications' }))
    await screen.findByRole('tablist', { name: 'Notifications' })

    await userEvent.keyboard('{Meta>}f{/Meta}')

    const field = screen.getByRole('combobox', { name: 'Filter notifications' })

    expect(field).toHaveFocus()
    await userEvent.type(field, 'parser')

    const header = within(field.closest('header') as HTMLElement)

    await waitFor(() => {
      expect(header.getByRole('status')).toHaveTextContent('1 match')
    })
    const page = within(screen.getByRole('tabpanel'))

    expect(page.queryByText('Add the widget cache')).not.toBeInTheDocument()
    expect(page.getByText('Bump the parser')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: 'Unread' }))

    expect(await screen.findByText('No notifications match')).toBeInTheDocument()
    expect(header.getByRole('status')).toHaveTextContent('No matches')

    await userEvent.click(screen.getByRole('button', { name: 'Clear the filter' }))
    expect(field).toHaveValue('')
  })

  it('counts and suggests what the GitHub tab lists', async () => {
    platform.getSettings.mockImplementation(() => stored({ notificationsTab: 'github' }))
    platform.githubThreadsList.mockResolvedValue(
      githubThreadList({ threads: [githubThread(), githubThread({ id: 1002, number: 9, title: 'Fix the parser' })] }),
    )
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Notifications' }))
    await screen.findByRole('list', { name: 'Today' })

    const field = screen.getByRole('combobox', { name: 'Filter notifications' })
    await userEvent.type(field, '#9')

    await waitFor(() => {
      expect(within(field.closest('header') as HTMLElement).getByRole('status')).toHaveTextContent('1 match')
    })

    await userEvent.clear(field)
    await userEvent.type(field, 'reason:')

    expect(await screen.findByRole('option', { name: /review_requested/ })).toBeInTheDocument()

    await userEvent.clear(field)
    await userEvent.click(screen.getByRole('radio', { name: 'Cleared' }))
    await userEvent.type(field, 'parser')

    expect(within(field.closest('header') as HTMLElement).queryByRole('status')).not.toHaveTextContent('match')
  })

  it('suggests what the app tab lists', async () => {
    platform.listNotifications.mockResolvedValue({ items: [review(), older()], unread: 1 })
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Notifications' }))
    await screen.findByRole('tablist', { name: 'Notifications' })
    await userEvent.type(screen.getByRole('combobox', { name: 'Filter notifications' }), 'type:')

    expect(await screen.findByRole('option', { name: /review/ })).toBeInTheDocument()
  })

  it('opens an entry from the page, marking it read', async () => {
    platform.listNotifications.mockResolvedValue({ items: [review()], unread: 1 })
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Notifications' }))
    await userEvent.click(await screen.findByRole('button', { name: /^(?!Done: ).*Add the widget cache/ }))

    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([3])
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('shows a GitHub notification’s item in the inbox', async () => {
    platform.getSettings.mockImplementation(() => stored({ notificationsTab: 'github' }))
    platform.githubThreadsList.mockResolvedValue(githubThreadList({ threads: [githubThread()] }))
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    await userEvent.click(views().getByRole('button', { name: 'Notifications' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show in inbox: Add the widget cache' }))

    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  describe('marking read without asking', () => {
    // click is a click on a banner, as the shell sends it.
    function click(change: {
      itemId?: number
      url?: string
      runId?: number
      notificationId: number
      autopilot?: AutopilotTarget
    }) {
      act(() => {
        emit('inbox:reveal', { itemId: 0, url: '', runId: 0, runStarted: '', autopilot: autopilotTarget(), ...change })
      })
    }

    it('marks a clicked banner’s entry read, but only while the entry is the one it stood for', async () => {
      platform.listNotifications.mockResolvedValue({
        items: [
          review(),
          notification({ id: 4, itemId: 5, number: 12, url: 'https://github.com/acme-org/other/pull/12' }),
        ],
        unread: 2,
      })
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })
      await screen.findByRole('button', { name: '2 unread notifications' })

      // A banner left from before the history was cleared, whose id another entry took.
      click({ itemId: 1, url: 'https://github.com/acme-org/example-repo/pull/7', notificationId: 4 })
      expect(platform.markNotificationsRead).not.toHaveBeenCalled()

      // One the log no longer holds.
      click({ itemId: 1, notificationId: 99 })
      expect(platform.markNotificationsRead).not.toHaveBeenCalled()

      click({ itemId: 1, url: 'https://github.com/acme-org/example-repo/pull/7', notificationId: 3 })
      expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([3])

      // Read now, the same banner clicked again marks nothing.
      await waitFor(() => {
        expect(screen.getByRole('button', { name: '1 unread notifications' })).toBeInTheDocument()
      })
      click({ itemId: 1, url: 'https://github.com/acme-org/example-repo/pull/7', notificationId: 3 })
      expect(platform.markNotificationsRead).toHaveBeenCalledOnce()
    })

    it('marks a failed run’s banner read before opening the run', async () => {
      platform.listNotifications.mockResolvedValue({
        items: [notification({ id: 6, kind: 'run_failed', itemId: 0, itemKind: '', runId: 7, url: '' })],
        unread: 1,
      })
      await renderApp()
      await screen.findByRole('button', { name: '1 unread notifications' })

      click({ runId: 7, notificationId: 6 })

      expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([6])
    })

    it('marks an Autopilot banner about an item read, and opens that item', async () => {
      const alert = autopilotAlert({ id: 7 })

      platform.listNotifications.mockResolvedValue({ items: [alert], unread: 1 })
      await renderApp()
      await screen.findByRole('button', { name: '1 unread notifications' })

      act(() => {
        emit('inbox:reveal', {
          itemId: 1,
          url: alert.url,
          runId: 0,
          runStarted: '',
          notificationId: 7,
          autopilot: alert.autopilot,
        })
      })

      expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([7])

      const review = within(await screen.findByRole('region', { name: 'Review requested' }))

      expect(review.getByRole('button', { name: 'Add the widget cache' }).closest('li')).toHaveAttribute(
        'data-highlighted',
        'true',
      )
      expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()
      expect(platform.openExternal).not.toHaveBeenCalled()
    })

    it('marks an Autopilot problem’s banner read, and opens its routine in the Autopilot tab', async () => {
      const problem = notification({
        id: 8,
        kind: 'autopilot_problem',
        itemId: 0,
        itemKind: '',
        number: 0,
        url: '',
        title: 'Snooze passing bot PRs',
        autopilot: autopilotTarget({ routineId: 3, createdAt: '2026-06-14T09:00:00Z' }),
      })

      platform.listRoutines.mockResolvedValue([routine()])
      platform.listNotifications.mockResolvedValue({ items: [problem], unread: 1 })
      await renderApp()
      await screen.findByRole('button', { name: '1 unread notifications' })

      click({ notificationId: 8, autopilot: problem.autopilot })

      expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([8])
      expect(screen.getByRole('button', { name: 'Autopilot' })).toHaveAttribute('aria-pressed', 'true')
      expect(await screen.findByRole('heading', { name: 'Snooze passing bot PRs', level: 2 })).toBeInTheDocument()
      expect(platform.routineWatching).toHaveBeenCalledWith(3)
      expect(platform.openExternal).not.toHaveBeenCalled()
    })

    it('says so when the routine a target names is gone, or another made since', async () => {
      platform.listRoutines.mockResolvedValue([routine()])
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      click({ notificationId: 0, autopilot: autopilotTarget({ routineId: 3, createdAt: '2026-01-01T00:00:00Z' }) })

      expect(
        within(await screen.findByRole('group', { name: 'Notification' })).getByText('That routine is no longer here.'),
      ).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: 'Activity', level: 2 })).toBeInTheDocument()
    })

    it('opens Activity for the menu bar’s row of what needs you, which names no notification', async () => {
      platform.listRoutines.mockResolvedValue([routine()])
      platform.autopilotActivity.mockResolvedValue(activityPage())
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      click({ notificationId: 0, autopilot: autopilotTarget({ activity: true }) })

      expect(screen.getByRole('button', { name: 'Autopilot' })).toHaveAttribute('aria-pressed', 'true')
      expect(await screen.findByRole('heading', { name: 'Activity', level: 2 })).toBeInTheDocument()
      expect(await screen.findByText('Snoozed 1')).toBeInTheDocument()
      expect(platform.markNotificationsRead).not.toHaveBeenCalled()
    })

    it('opens Activity at the match a banner names, once it is read, or says it is gone', async () => {
      platform.listRoutines.mockResolvedValue([routine()])
      platform.autopilotActivity.mockResolvedValue(activityPage())
      await renderApp()
      await screen.findByRole('region', { name: 'Review requested' })

      click({
        notificationId: 0,
        autopilot: autopilotTarget({ routineId: 3, matchId: 12, createdAt: '2026-06-15T11:30:00Z' }),
      })

      const line = (await screen.findByText('Snoozed 1')).closest('li')

      expect(line).toHaveAttribute('data-highlighted', 'true')

      click({
        notificationId: 0,
        autopilot: autopilotTarget({ routineId: 3, matchId: 12, createdAt: '2026-06-01T11:30:00Z' }),
      })

      expect(
        within(await screen.findByRole('group', { name: 'Notification' })).getByText(
          'That is no longer in Autopilot’s history.',
        ),
      ).toBeInTheDocument()
    })

    it('marks an Autopilot entry about an item read when that item’s sheet opens', async () => {
      platform.listNotifications.mockResolvedValue({
        items: [autopilotAlert({ id: 8, itemId: 1 }), autopilotAlert({ id: 2, itemId: 2 })],
        unread: 2,
      })
      await renderApp()
      await screen.findByRole('button', { name: '2 unread notifications' })

      await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))
      await screen.findByRole('dialog', { name: 'Add the widget cache' })

      await waitFor(() => {
        expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([8])
      })
    })

    it('marks an item’s entries read when its sheet opens, once each opening', async () => {
      platform.listNotifications.mockResolvedValue({
        items: [review(), notification({ id: 1, itemId: 1, kind: 'woke' })],
        unread: 2,
      })
      await renderApp()
      await screen.findByRole('button', { name: '2 unread notifications' })

      await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))
      await screen.findByRole('dialog', { name: 'Add the widget cache' })

      await waitFor(() => {
        expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([3, 1])
      })

      // Another view and back: the sheet is still open, and nothing is marked anew.
      await userEvent.click(views().getByRole('button', { name: 'Notifications' }))
      await userEvent.click(views().getByRole('button', { name: 'Inbox' }))

      expect(platform.markNotificationsRead).toHaveBeenCalledOnce()
    })
  })
})

describe('Autopilot (beta)', () => {
  const views = () => within(screen.getByRole('navigation', { name: 'Views' }))

  // pinnedByARoutine lists a card a routine pinned, whose latest match did it.
  const pinnedByARoutine = () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([item({ pinnedAt: '2026-06-15T11:30:00Z', pinnedBy: 3 })]) }),
    )
    platform.listRoutines.mockResolvedValue([routine()])
    platform.latestAutopilotMatches.mockResolvedValue([autopilotMatch()])
  }

  it('shows the Autopilot tab marked Beta, and the routines’ chips and marks on the rows, while it is on', async () => {
    pinnedByARoutine()
    await renderApp()

    const tab = await views().findByRole('button', { name: 'Autopilot' })

    expect(within(tab).getByText('Beta')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /^Autopilot: / })).toBeInTheDocument()
    expect(screen.getByTitle('Pinned by the routine Snooze passing bot PRs')).toBeInTheDocument()

    // An item's menu offers Autopilot ▸.
    const [more] = screen.getAllByRole('button', { name: 'More actions' })
    if (more === undefined) {
      throw new Error('no row menu')
    }
    await userEvent.click(more)
    expect(
      await within(screen.getByRole('menu', { name: 'More actions' })).findByRole('menuitem', { name: 'Autopilot' }),
    ).toBeInTheDocument()
  })

  it('makes a routine from an item’s menu in the Autopilot tab’s builder, and Start from an item shows the inbox', async () => {
    platform.listRoutines.mockResolvedValue([])
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    const [more] = screen.getAllByRole('button', { name: 'More actions' })
    if (more === undefined) {
      throw new Error('no row menu')
    }
    await userEvent.click(more)
    await userEvent.click(
      await within(screen.getByRole('menu', { name: 'More actions' })).findByRole('menuitem', { name: 'Autopilot' }),
    )
    await userEvent.click(
      await within(screen.getByRole('menu', { name: 'Autopilot' })).findByRole('menuitem', {
        name: 'Make a routine from this…',
      }),
    )

    expect(await screen.findByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(platform.routineFromItem).toHaveBeenCalledOnce()
    expect(views().getByRole('button', { name: 'Autopilot' })).toHaveAttribute('aria-pressed', 'true')

    // Leaving the routine filled in from the item asks first, as any unsaved new routine does.
    await userEvent.click(screen.getByRole('button', { name: 'Templates' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Discard changes' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Start from an item' }))
    await userEvent.click(screen.getByRole('button', { name: 'Show the inbox' }))

    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('shows none of Autopilot while it is off: no tab, chip, mark, or type of notification', async () => {
    pinnedByARoutine()
    platform.getSettings.mockResolvedValue(autopilotOff())
    await renderApp()

    await screen.findByRole('region', { name: 'Review requested' })
    await act(async () => {
      await Promise.resolve()
    })

    expect(views().queryByRole('button', { name: 'Autopilot' })).not.toBeInTheDocument()
    expect(screen.queryByText('Beta')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Autopilot: / })).not.toBeInTheDocument()
    expect(screen.queryByTitle(/by the routine/)).not.toBeInTheDocument()
    expect(platform.latestAutopilotMatches).not.toHaveBeenCalled()

    // An item's menu offers no Autopilot ▸ either.
    const [more] = screen.getAllByRole('button', { name: 'More actions' })
    if (more === undefined) {
      throw new Error('no row menu')
    }
    await userEvent.click(more)
    const menu = within(screen.getByRole('menu', { name: 'More actions' }))
    expect(menu.getByRole('menuitem', { name: 'Open on GitHub' })).toBeInTheDocument()
    expect(menu.queryByRole('menuitem', { name: 'Autopilot' })).not.toBeInTheDocument()
    await userEvent.keyboard('{Escape}')

    await userEvent.click(views().getByRole('button', { name: 'Notifications' }))

    const type = await screen.findByRole('radiogroup', { name: 'Type' })

    expect(within(type).getByRole('radio', { name: 'Reviews' })).toBeInTheDocument()
    expect(within(type).queryByRole('radio', { name: 'Autopilot' })).not.toBeInTheDocument()
  })

  it('says where to turn it on when a notification would open the Autopilot tab while it is off', async () => {
    platform.getSettings.mockResolvedValue(autopilotOff())
    await renderApp()
    await screen.findByRole('region', { name: 'Review requested' })

    act(() => {
      emit('inbox:reveal', {
        itemId: 0,
        url: '',
        runId: 0,
        runStarted: '',
        notificationId: 0,
        autopilot: autopilotTarget({ activity: true }),
      })
    })

    const said = await screen.findByRole('group', { name: 'Notification' })

    expect(within(said).getByText('Autopilot is off. Turn it on in Settings → Labs to see this.')).toBeInTheDocument()
    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('goes back to the inbox once Autopilot is turned off from its tab, and brings the tab back once it is on', async () => {
    await renderApp()

    await userEvent.click(await views().findByRole('button', { name: 'Autopilot' }))
    expect(views().getByRole('button', { name: 'Autopilot' })).toHaveAttribute('aria-pressed', 'true')

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Labs' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Autopilot (beta)' }))

    expect(platform.setAutopilotEnabled).toHaveBeenCalledExactlyOnceWith(false)
    await waitFor(() => {
      expect(views().queryByRole('button', { name: 'Autopilot' })).not.toBeInTheDocument()
    })

    await userEvent.click(screen.getByRole('button', { name: 'Close settings' }))

    expect(views().getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(await screen.findByRole('region', { name: 'Review requested' })).toBeInTheDocument()

    await openSettingsFromMenu()
    await userEvent.click(screen.getByRole('button', { name: 'Labs' }))
    await userEvent.click(screen.getByRole('switch', { name: 'Autopilot (beta)' }))

    expect(platform.setAutopilotEnabled).toHaveBeenLastCalledWith(true)
    expect(within(await views().findByRole('button', { name: 'Autopilot' })).getByText('Beta')).toBeInTheDocument()
  })
})
