import { beforeEach, describe, expect, it, vi } from 'vitest'

import * as platform from './index'

const mocks = vi.hoisted(() => ({
  appInfo: { Info: vi.fn(), ReportFirstPaint: vi.fn() },
  inbox: { Snapshot: vi.fn(), Refresh: vi.fn() },
  launcher: { Launch: vi.fn() },
  profiles: { List: vi.fn() },
  settings: {
    Get: vi.fn(),
    SetTerminal: vi.fn(),
    SetCloneRoot: vi.fn(),
    SetDefaultProfile: vi.fn(),
    SetWorktreeDefault: vi.fn(),
  },
  worktrees: { List: vi.fn(), Remove: vi.fn(), ForceRemove: vi.fn(), RemoveAllClean: vi.fn(), Prune: vi.fn() },
  events: { On: vi.fn() },
  browser: { OpenURL: vi.fn() },
}))

vi.mock('@wailsio/runtime', () => ({ Events: mocks.events, Browser: mocks.browser }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/appinfo', () => ({ Service: mocks.appInfo }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/inbox', () => ({ Service: mocks.inbox }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/launcher', () => ({ Service: mocks.launcher }))
vi.mock('../../bindings/github.com/mrz1836/nilbox/internal/profiles', () => ({ Service: mocks.profiles }))
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
    const info: platform.Info = { name: 'App', tagline: 'Tagline', version: 'dev' }
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
    }

    mocks.profiles.List.mockResolvedValueOnce([profile]).mockResolvedValueOnce(null)

    await expect(platform.listProfiles()).resolves.toEqual([profile])
    await expect(platform.listProfiles()).resolves.toEqual([])
  })

  it('launches with the request', async () => {
    const request: platform.LaunchRequest = { itemId: 3, profile: '', worktree: null, fullAccessOverride: false }
    const result: platform.LaunchResult = {
      scriptPath: '~/launch/pr-7.command',
      clonePath: '~/projects/example-repo',
      worktree: 'pr-7',
      profile: 'default',
      permission: 'full access',
      terminal: 'iterm2',
    }
    mocks.launcher.Launch.mockResolvedValue(result)

    await expect(platform.launch(request)).resolves.toBe(result)
    expect(mocks.launcher.Launch).toHaveBeenCalledWith(request)
  })
})

describe('settings', () => {
  it('reads and writes settings', async () => {
    const settings: platform.Settings = {
      defaultProfile: 'default',
      worktreeDefault: true,
      terminal: 'iterm2',
      cloneRoot: '~/projects',
    }
    mocks.settings.Get.mockResolvedValue(settings)
    mocks.settings.SetTerminal.mockResolvedValue(undefined)
    mocks.settings.SetCloneRoot.mockResolvedValue(undefined)
    mocks.settings.SetDefaultProfile.mockResolvedValue(undefined)
    mocks.settings.SetWorktreeDefault.mockResolvedValue(undefined)

    await expect(platform.getSettings()).resolves.toBe(settings)

    await platform.setTerminal('terminal')
    await platform.setCloneRoot('~/code')
    await platform.setDefaultProfile('work')
    await platform.setWorktreeDefault(false)

    expect(mocks.settings.SetTerminal).toHaveBeenCalledWith('terminal')
    expect(mocks.settings.SetCloneRoot).toHaveBeenCalledWith('~/code')
    expect(mocks.settings.SetDefaultProfile).toHaveBeenCalledWith('work')
    expect(mocks.settings.SetWorktreeDefault).toHaveBeenCalledWith(false)
  })
})

describe('worktrees', () => {
  it('lists, removes, force-removes, and prunes', async () => {
    const listing: platform.WorktreesSnapshot = { root: '~/projects', repos: [], cleanCount: 0, listedAt: '' }
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
      reason: '',
    }
    mocks.worktrees.RemoveAllClean.mockResolvedValueOnce([removed]).mockResolvedValueOnce(null)

    await expect(platform.removeAllCleanWorktrees()).resolves.toEqual([removed])
    await expect(platform.removeAllCleanWorktrees()).resolves.toEqual([])
  })
})
