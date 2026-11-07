import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as platform from './index'

const mocks = vi.hoisted(() => ({
  appInfo: { Info: vi.fn(), ReportFirstPaint: vi.fn() },
  inbox: {
    Snapshot: vi.fn(),
    Refresh: vi.fn(),
    RefreshItem: vi.fn(),
    Dismiss: vi.fn(),
    Snooze: vi.fn(),
    Restore: vi.fn(),
    Pin: vi.fn(),
    Unpin: vi.fn(),
    CheckFiles: vi.fn(),
  },
  launcher: {
    Launch: vi.fn(),
    Resolve: vi.fn(),
    ResolveItems: vi.fn(),
    Preview: vi.fn(),
    CloneTarget: vi.fn(),
    Clone: vi.fn(),
  },
  profiles: { List: vi.fn() },
  prompts: {
    List: vi.fn(),
    Create: vi.fn(),
    Update: vi.fn(),
    Delete: vi.fn(),
    Duplicate: vi.fn(),
    Reset: vi.fn(),
    Check: vi.fn(),
    Variables: vi.fn(),
    Defaults: vi.fn(),
    SetDefault: vi.fn(),
  },
  launchDefaults: { List: vi.fn(), Check: vi.fn(), Save: vi.fn(), Remove: vi.fn(), Targets: vi.fn() },
  settings: {
    Get: vi.fn(),
    SetTerminal: vi.fn(),
    SetCloneRoot: vi.fn(),
    SetDefaultProfile: vi.fn(),
    SetWorktreeDefault: vi.fn(),
    SetWorktreeAutoCleanup: vi.fn(),
    SetTrustedBots: vi.fn(),
    ResetTrustedBots: vi.fn(),
    SetSnoozeDays: vi.fn(),
    SetSnoozeWake: vi.fn(),
    SetWindowSize: vi.fn(),
    CollapsedSections: vi.fn(),
    SetSectionCollapsed: vi.fn(),
    Theme: vi.fn(),
    SetTheme: vi.fn(),
    SetSensitiveScope: vi.fn(),
    SetSensitiveCategory: vi.fn(),
    SetSensitivePatterns: vi.fn(),
    MatchSensitivePath: vi.fn(),
  },
  worktrees: {
    List: vi.fn(),
    Remove: vi.fn(),
    ForceRemove: vi.fn(),
    RemoveAllClean: vi.fn(),
    Prune: vi.fn(),
    Unlock: vi.fn(),
    CleanUp: vi.fn(),
    CleanUpFinished: vi.fn(),
    Sweep: vi.fn(),
    LastSweep: vi.fn(),
    RecentCleanups: vi.fn(),
    ClearCleanups: vi.fn(),
  },
  events: { On: vi.fn() },
  browser: { OpenURL: vi.fn() },
  window: { SetSize: vi.fn() },
}))

vi.mock('@wailsio/runtime', () => ({ Events: mocks.events, Browser: mocks.browser, Window: mocks.window }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/appinfo', () => ({ Service: mocks.appInfo }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/inbox', () => ({ Service: mocks.inbox }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/launcher', () => ({ Service: mocks.launcher }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/profiles', () => ({ Service: mocks.profiles }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/prompts', () => ({ Service: mocks.prompts }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults', () => ({ Service: mocks.launchDefaults }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/settings', () => ({ Service: mocks.settings }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/worktrees', () => ({ Service: mocks.worktrees }))

const emptyList: platform.InboxList = { items: [], shown: 0, total: 0 }
const unknownRate: platform.Rate = { known: false, limit: 0, remaining: 0, used: 0, cost: 0, resetAt: '', takenAt: '' }

const snapshot: platform.InboxSnapshot = {
  account: { login: 'octo-user', name: '', profileUrl: 'https://github.com/octo-user', avatar: '' },
  reviewRequested: emptyList,
  authored: emptyList,
  assigned: emptyList,
  rate: { graphql: unknownRate, core: unknownRate, search: unknownRate },
  lastRefreshedAt: '',
  lastError: '',
  scopeWarning: '',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('app', () => {
  it('reads the app info', async () => {
    const info: platform.Info = {
      name: 'App',
      tagline: 'Tagline',
      version: 'dev',
      release: '0.0.0',
      build: '0',
      system: 'macOS 15.1 (24B83)',
      arch: 'arm64',
      bugReportUrl: 'https://github.com/acme-org/example-repo/issues/new',
    }
    mocks.appInfo.Info.mockResolvedValue(info)

    await expect(platform.getAppInfo()).resolves.toEqual(info)
  })

  it('reports the first paint', async () => {
    mocks.appInfo.ReportFirstPaint.mockResolvedValue(undefined)

    await platform.reportFirstPaint()

    expect(mocks.appInfo.ReportFirstPaint).toHaveBeenCalledOnce()
  })

  it('opens a URL in the browser', async () => {
    mocks.browser.OpenURL.mockResolvedValue(undefined)

    await platform.openInBrowser('https://github.com/acme-org/example-repo/pull/7')

    expect(mocks.browser.OpenURL).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })
})

describe('inbox', () => {
  it('reads and refreshes the snapshot', async () => {
    mocks.inbox.Snapshot.mockResolvedValue(snapshot)
    mocks.inbox.Refresh.mockResolvedValue(snapshot)

    await expect(platform.getInboxSnapshot()).resolves.toBe(snapshot)
    await expect(platform.refreshInbox()).resolves.toBe(snapshot)
  })

  it('refreshes one item', async () => {
    mocks.inbox.RefreshItem.mockResolvedValue(snapshot)

    await expect(platform.refreshItem(4)).resolves.toBe(snapshot)

    expect(mocks.inbox.RefreshItem).toHaveBeenCalledWith(4)
  })

  it("checks a pull request's files now", async () => {
    const check: platform.FileCheck = {
      status: 'clear',
      head: 'abc',
      checked: 2,
      total: 2,
      complete: true,
      groups: [],
      checkedAt: '2026-06-15T12:00:00Z',
      error: '',
    }
    mocks.inbox.CheckFiles.mockResolvedValueOnce(check).mockResolvedValueOnce(null)

    await expect(platform.checkItemFiles(4)).resolves.toBe(check)
    await expect(platform.checkItemFiles(5)).rejects.toThrow('not checked')
    expect(mocks.inbox.CheckFiles).toHaveBeenCalledWith(4)
  })

  it('dismisses, snoozes, and restores an item', async () => {
    mocks.inbox.Dismiss.mockResolvedValue(snapshot)
    mocks.inbox.Snooze.mockResolvedValue(snapshot)
    mocks.inbox.Restore.mockResolvedValue(snapshot)

    await expect(platform.dismissItem(4)).resolves.toBe(snapshot)
    await expect(platform.snoozeItem(4, 3)).resolves.toBe(snapshot)
    await expect(platform.restoreItem(4)).resolves.toBe(snapshot)

    expect(mocks.inbox.Dismiss).toHaveBeenCalledWith(4)
    expect(mocks.inbox.Snooze).toHaveBeenCalledWith(4, 3)
    expect(mocks.inbox.Restore).toHaveBeenCalledWith(4)
  })

  it('pins and unpins an item', async () => {
    mocks.inbox.Pin.mockResolvedValue(snapshot)
    mocks.inbox.Unpin.mockResolvedValue(snapshot)

    await expect(platform.pinItem(4)).resolves.toBe(snapshot)
    await expect(platform.unpinItem(4)).resolves.toBe(snapshot)

    expect(mocks.inbox.Pin).toHaveBeenCalledWith(4)
    expect(mocks.inbox.Unpin).toHaveBeenCalledWith(4)
  })

  it('subscribes to inbox updates and returns the unsubscribe function', () => {
    const unsubscribe = vi.fn()
    mocks.events.On.mockReturnValue(unsubscribe)

    const listener = vi.fn()
    const stop = platform.onInboxUpdated(listener)

    expect(mocks.events.On).toHaveBeenCalledWith(platform.inboxUpdatedEvent, expect.any(Function))

    const handler = mocks.events.On.mock.calls[0]?.[1] as () => void
    handler()
    expect(listener).toHaveBeenCalledOnce()

    stop()
    expect(unsubscribe).toHaveBeenCalledOnce()
  })
})

describe('launch', () => {
  it('lists profiles, treating no rows as an empty list', async () => {
    const profile: platform.Profile = {
      name: 'default',
      configDir: '',
      model: '',
      effort: '',
      permissionMode: 'full',
      planMode: false,
      addDirs: [],
      extraArgs: [],
      env: {},
      folder: '~/.claude',
      missing: false,
    }

    mocks.profiles.List.mockResolvedValueOnce([profile]).mockResolvedValueOnce(null)

    await expect(platform.listProfiles()).resolves.toEqual([profile])
    await expect(platform.listProfiles()).resolves.toEqual([])
  })

  const request: platform.LaunchRequest = {
    itemId: 3,
    profile: '',
    promptId: 0,
    model: null,
    effort: null,
    planMode: null,
    worktree: null,
    fullAccessOverride: false,
    confirmed: '',
  }

  it('launches with the request', async () => {
    const result: platform.LaunchResult = {
      scriptPath: '~/launch/pr-7.command',
      clonePath: '~/projects/example-repo',
      worktree: 'pr-7',
      prompt: 'Review',
      profile: 'default',
      permission: 'full access',
      terminal: 'iterm2',
      hold: null,
    }
    mocks.launcher.Launch.mockResolvedValue(result)

    await expect(platform.launch(request)).resolves.toBe(result)
    expect(mocks.launcher.Launch).toHaveBeenCalledWith(request)
  })

  it('resolves, previews, and resolves many items, treating no rows as an empty list', async () => {
    const resolution = { itemId: 3 }
    const preview = { prompt: 'Review it' }
    mocks.launcher.Resolve.mockResolvedValue(resolution)
    mocks.launcher.Preview.mockResolvedValue(preview)
    mocks.launcher.ResolveItems.mockResolvedValueOnce([resolution]).mockResolvedValueOnce(null)

    await expect(platform.resolveLaunch(request)).resolves.toBe(resolution)
    await expect(platform.previewLaunch(request)).resolves.toBe(preview)
    await expect(platform.resolveLaunches([3])).resolves.toEqual([resolution])
    await expect(platform.resolveLaunches([])).resolves.toEqual([])
    expect(mocks.launcher.ResolveItems).toHaveBeenCalledWith([3])
  })

  it('finds where a missing clone goes, and clones it', async () => {
    const target: platform.CloneTarget = {
      repo: 'acme-org/example-repo',
      cloned: false,
      dir: '~/projects/example-repo',
      taken: false,
    }
    const cloned: platform.CloneTarget = { ...target, cloned: true }
    mocks.launcher.CloneTarget.mockResolvedValue(target)
    mocks.launcher.Clone.mockResolvedValue(cloned)

    await expect(platform.cloneTarget(3)).resolves.toBe(target)
    await expect(platform.cloneRepo(3)).resolves.toBe(cloned)
    expect(mocks.launcher.CloneTarget).toHaveBeenCalledWith(3)
    expect(mocks.launcher.Clone).toHaveBeenCalledWith(3)
  })
})

describe('prompts', () => {
  it('passes every call through, treating no rows as empty lists', async () => {
    const input = { name: 'x' } as unknown as platform.PromptInput
    const prompt = { id: 4 }
    const check = { preview: '' }

    for (const fn of [mocks.prompts.Create, mocks.prompts.Update, mocks.prompts.Duplicate, mocks.prompts.Reset]) {
      fn.mockResolvedValue(prompt)
    }

    mocks.prompts.List.mockResolvedValueOnce([prompt]).mockResolvedValueOnce(null)
    mocks.prompts.Variables.mockResolvedValueOnce([{ name: 'Title' }]).mockResolvedValueOnce(null)
    mocks.prompts.Check.mockResolvedValue(check)
    mocks.prompts.Defaults.mockResolvedValue({ pr: 1, issue: 2 })
    mocks.prompts.Delete.mockResolvedValue(undefined)
    mocks.prompts.SetDefault.mockResolvedValue(undefined)

    await expect(platform.listPrompts()).resolves.toEqual([prompt])
    await expect(platform.listPrompts()).resolves.toEqual([])
    await expect(platform.listPromptVariables()).resolves.toEqual([{ name: 'Title' }])
    await expect(platform.listPromptVariables()).resolves.toEqual([])
    await expect(platform.createPrompt(input)).resolves.toBe(prompt)
    await expect(platform.updatePrompt(4, input)).resolves.toBe(prompt)
    await expect(platform.duplicatePrompt(4)).resolves.toBe(prompt)
    await expect(platform.resetPrompt(4)).resolves.toBe(prompt)
    await expect(platform.getDefaultPrompts()).resolves.toEqual({ pr: 1, issue: 2 })

    const checkRequest = { id: 4, input, itemId: 0, inWorktree: false, atPrHead: false }
    await expect(platform.checkPrompt(checkRequest)).resolves.toBe(check)

    await platform.deletePrompt(4)
    await platform.setDefaultPrompt('pr', 4)

    expect(mocks.prompts.Update).toHaveBeenCalledWith(4, input)
    expect(mocks.prompts.Delete).toHaveBeenCalledWith(4)
    expect(mocks.prompts.SetDefault).toHaveBeenCalledWith('pr', 4)
    expect(mocks.prompts.Check).toHaveBeenCalledWith(checkRequest)
  })
})

describe('launch defaults', () => {
  it('passes every call through, treating no rows as empty lists', async () => {
    const input = { owner: 'acme-org' } as unknown as platform.LaunchDefaultsInput
    const saved = { owner: 'acme-org' }
    mocks.launchDefaults.List.mockResolvedValueOnce([saved]).mockResolvedValueOnce(null)
    mocks.launchDefaults.Check.mockResolvedValueOnce([{ field: 'owner', message: 'x' }]).mockResolvedValueOnce(null)
    mocks.launchDefaults.Targets.mockResolvedValueOnce([{ owner: 'acme-org' }]).mockResolvedValueOnce(null)
    mocks.launchDefaults.Save.mockResolvedValue(saved)
    mocks.launchDefaults.Remove.mockResolvedValue(undefined)

    await expect(platform.listLaunchDefaults()).resolves.toEqual([saved])
    await expect(platform.listLaunchDefaults()).resolves.toEqual([])
    await expect(platform.checkLaunchDefaults(input)).resolves.toEqual([{ field: 'owner', message: 'x' }])
    await expect(platform.checkLaunchDefaults(input)).resolves.toEqual([])
    await expect(platform.listLaunchTargets()).resolves.toEqual([{ owner: 'acme-org' }])
    await expect(platform.listLaunchTargets()).resolves.toEqual([])
    await expect(platform.saveLaunchDefaults(input)).resolves.toBe(saved)

    await platform.removeLaunchDefaults('acme-org', 'api')
    expect(mocks.launchDefaults.Remove).toHaveBeenCalledWith('acme-org', 'api')
  })
})

describe('settings', () => {
  it('reads and writes settings', async () => {
    const settings: platform.Settings = {
      defaultProfile: 'default',
      worktreeDefault: true,
      worktreeAutoCleanup: true,
      terminal: 'iterm2',
      cloneRoot: '~/projects',
      trustedBots: ['dependabot'],
      defaultTrustedBots: ['dependabot'],
      snoozeDays: 3,
      snoozeWake: { commits: 1, reviews: 'decisions', comments: 0 },
      windowSize: { width: 1280, height: 800 },
      defaultWindowSize: { width: 1280, height: 800 },
      sensitiveFiles: { scope: 'collaborators', off: [], patterns: [] },
      sensitiveCategories: [],
    }
    mocks.settings.Get.mockResolvedValue(settings)
    mocks.settings.SetTerminal.mockResolvedValue(undefined)
    mocks.settings.SetCloneRoot.mockResolvedValue(undefined)
    mocks.settings.SetDefaultProfile.mockResolvedValue(undefined)
    mocks.settings.SetWorktreeDefault.mockResolvedValue(undefined)
    mocks.settings.SetWorktreeAutoCleanup.mockResolvedValue(undefined)

    await expect(platform.getSettings()).resolves.toBe(settings)

    await platform.setTerminal('terminal')
    await platform.setCloneRoot('~/code')
    await platform.setDefaultProfile('work')
    await platform.setWorktreeDefault(false)
    await platform.setWorktreeAutoCleanup(false)

    expect(mocks.settings.SetTerminal).toHaveBeenCalledWith('terminal')
    expect(mocks.settings.SetCloneRoot).toHaveBeenCalledWith('~/code')
    expect(mocks.settings.SetDefaultProfile).toHaveBeenCalledWith('work')
    expect(mocks.settings.SetWorktreeDefault).toHaveBeenCalledWith(false)
    expect(mocks.settings.SetWorktreeAutoCleanup).toHaveBeenCalledWith(false)

    mocks.settings.SetTrustedBots.mockResolvedValue(undefined)
    mocks.settings.ResetTrustedBots.mockResolvedValue(undefined)
    await platform.setTrustedBots(['renovate'])
    await platform.resetTrustedBots()

    expect(mocks.settings.SetTrustedBots).toHaveBeenCalledWith(['renovate'])
    expect(mocks.settings.ResetTrustedBots).toHaveBeenCalledOnce()

    mocks.settings.SetSnoozeDays.mockResolvedValue(undefined)
    await platform.setSnoozeDays(7)

    expect(mocks.settings.SetSnoozeDays).toHaveBeenCalledWith(7)

    const rules: platform.SnoozeRules = { commits: 3, reviews: 'any', comments: 5 }
    mocks.settings.SetSnoozeWake.mockResolvedValue(undefined)
    await platform.setSnoozeWake(rules)

    expect(mocks.settings.SetSnoozeWake).toHaveBeenCalledWith(rules)
  })

  it('stores the sensitive file settings and tries a path', async () => {
    mocks.settings.SetSensitiveScope.mockResolvedValue(undefined)
    mocks.settings.SetSensitiveCategory.mockResolvedValue(undefined)
    mocks.settings.SetSensitivePatterns.mockResolvedValue(undefined)

    const match: platform.SensitivePathMatch = {
      sensitive: true,
      category: 'workflows',
      label: 'Workflows and CI',
      level: 'danger',
      pattern: '.github/workflows/',
    }
    mocks.settings.MatchSensitivePath.mockResolvedValue(match)

    await platform.setSensitiveScope('outside')
    await platform.setSensitiveCategory('dependencies', false)
    await platform.setSensitivePatterns(['infra/'])

    expect(mocks.settings.SetSensitiveScope).toHaveBeenCalledWith('outside')
    expect(mocks.settings.SetSensitiveCategory).toHaveBeenCalledWith('dependencies', false)
    expect(mocks.settings.SetSensitivePatterns).toHaveBeenCalledWith(['infra/'])
    await expect(platform.matchSensitivePath('.github/workflows/ci.yml')).resolves.toBe(match)
  })

  it('reads and stores which inbox sections are collapsed', async () => {
    mocks.settings.CollapsedSections.mockResolvedValue(['authored'])
    await expect(platform.getCollapsedSections()).resolves.toEqual(['authored'])

    mocks.settings.CollapsedSections.mockResolvedValue(null)
    await expect(platform.getCollapsedSections()).resolves.toEqual([])

    mocks.settings.SetSectionCollapsed.mockResolvedValue(undefined)
    await platform.setSectionCollapsed('assigned', true)

    expect(mocks.settings.SetSectionCollapsed).toHaveBeenCalledWith('assigned', true)
  })

  it('reads and stores the theme', async () => {
    mocks.settings.Theme.mockResolvedValue('dark')
    await expect(platform.getTheme()).resolves.toBe('dark')

    mocks.settings.SetTheme.mockResolvedValue(undefined)
    await platform.setTheme('light')

    expect(mocks.settings.SetTheme).toHaveBeenCalledWith('light')
  })

  it('stores the window size, then resizes the open window', async () => {
    const calls: string[] = []
    mocks.settings.SetWindowSize.mockImplementation(() => {
      calls.push('store')
      return Promise.resolve()
    })
    mocks.window.SetSize.mockImplementation(() => {
      calls.push('resize')
      return Promise.resolve()
    })

    await platform.setWindowSize({ width: 1440, height: 900 })

    expect(mocks.settings.SetWindowSize).toHaveBeenCalledWith({ width: 1440, height: 900 })
    expect(mocks.window.SetSize).toHaveBeenCalledWith(1440, 900)
    expect(calls).toEqual(['store', 'resize'])
  })

  it('leaves the window alone when the size is refused', async () => {
    mocks.window.SetSize.mockClear()
    mocks.settings.SetWindowSize.mockRejectedValue(new Error('invalid window size'))

    await expect(platform.setWindowSize({ width: 100, height: 100 })).rejects.toThrow('invalid window size')
    expect(mocks.window.SetSize).not.toHaveBeenCalled()
  })
})

describe('worktrees', () => {
  it('lists, removes, force-removes, and prunes', async () => {
    const listing: platform.WorktreesSnapshot = {
      root: '~/projects',
      repos: [],
      cleanCount: 0,
      finishedCount: 0,
      reviewCount: 0,
      worktreeCount: 0,
      linkError: '',
      listedAt: '',
    }
    mocks.worktrees.List.mockResolvedValue(listing)
    mocks.worktrees.Remove.mockResolvedValue(undefined)
    mocks.worktrees.ForceRemove.mockResolvedValue(undefined)
    mocks.worktrees.Prune.mockResolvedValue(undefined)

    await expect(platform.listWorktrees()).resolves.toBe(listing)

    await platform.removeWorktree('/repo', '/repo/.claude/worktrees/pr-7')
    await platform.forceRemoveWorktree('/repo', '/repo/.claude/worktrees/pr-8')
    await platform.pruneWorktrees('/repo')

    expect(mocks.worktrees.Remove).toHaveBeenCalledWith('/repo', '/repo/.claude/worktrees/pr-7')
    expect(mocks.worktrees.ForceRemove).toHaveBeenCalledWith('/repo', '/repo/.claude/worktrees/pr-8')
    expect(mocks.worktrees.Prune).toHaveBeenCalledWith('/repo')
  })

  it('removes all clean worktrees, treating no results as an empty list', async () => {
    const removed: platform.RemoveResult = {
      repoPath: '/repo',
      path: '/repo/.claude/worktrees/pr-7',
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
      removed: true,
      deletedBranch: '',
      item: '',
      reason: '',
    }
    mocks.worktrees.RemoveAllClean.mockResolvedValueOnce([removed]).mockResolvedValueOnce(null)

    await expect(platform.removeAllCleanWorktrees()).resolves.toEqual([removed])
    await expect(platform.removeAllCleanWorktrees()).resolves.toEqual([])
  })

  it('unlocks, cleans up, and sweeps', async () => {
    const cleaned: platform.RemoveResult = {
      repoPath: '/repo',
      path: '/repo/.claude/worktrees/pr-7',
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
      removed: true,
      deletedBranch: 'worktree-pr-7',
      item: 'acme-org/example-repo#7',
      reason: '',
    }
    const report: platform.SweepReport = {
      seq: 3,
      at: '2026-11-06T18:00:00Z',
      enabled: true,
      cleaned: [cleaned],
      failed: [],
      leftover: 1,
      worktreeCount: 2,
      error: '',
    }
    mocks.worktrees.Unlock.mockResolvedValue(undefined)
    mocks.worktrees.CleanUp.mockResolvedValue(cleaned)
    mocks.worktrees.CleanUpFinished.mockResolvedValueOnce([cleaned]).mockResolvedValueOnce(null)
    mocks.worktrees.Sweep.mockResolvedValue(report)
    mocks.worktrees.LastSweep.mockResolvedValue(report)

    await platform.unlockWorktree('/repo', cleaned.path)
    expect(mocks.worktrees.Unlock).toHaveBeenCalledWith('/repo', cleaned.path)

    await expect(platform.cleanUpWorktree('/repo', cleaned.path)).resolves.toBe(cleaned)
    expect(mocks.worktrees.CleanUp).toHaveBeenCalledWith('/repo', cleaned.path)

    await expect(platform.cleanUpFinishedWorktrees()).resolves.toEqual([cleaned])
    await expect(platform.cleanUpFinishedWorktrees()).resolves.toEqual([])

    await expect(platform.sweepWorktrees()).resolves.toBe(report)
    await expect(platform.lastWorktreeSweep()).resolves.toBe(report)
  })

  it('lists and clears the recent cleanups, treating none as an empty list', async () => {
    const cleanup: platform.WorktreeCleanup = {
      id: 1,
      cleanedAt: '2026-11-06T18:00:00Z',
      automatic: true,
      repo: 'example-repo',
      worktree: 'pr-7',
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
      deletedBranch: 'worktree-pr-7',
      head: 'fc1c3975011e66491785ace891c893e4fc716587',
      item: null,
    }
    mocks.worktrees.RecentCleanups.mockResolvedValueOnce([cleanup]).mockResolvedValueOnce(null)
    mocks.worktrees.ClearCleanups.mockResolvedValue(undefined)

    await expect(platform.recentCleanups()).resolves.toEqual([cleanup])
    await expect(platform.recentCleanups()).resolves.toEqual([])

    await platform.clearCleanups()
    expect(mocks.worktrees.ClearCleanups).toHaveBeenCalledOnce()
  })

  it('subscribes to worktree sweeps and returns the unsubscribe function', () => {
    const unsubscribe = vi.fn()
    mocks.events.On.mockReturnValue(unsubscribe)

    const listener = vi.fn()
    const stop = platform.onWorktreesUpdated(listener)

    expect(mocks.events.On).toHaveBeenCalledWith(platform.worktreesUpdatedEvent, expect.any(Function))

    const handler = mocks.events.On.mock.calls.at(-1)?.[1] as () => void
    handler()
    expect(listener).toHaveBeenCalledOnce()

    stop()
    expect(unsubscribe).toHaveBeenCalledOnce()
  })
})
