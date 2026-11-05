// Fake data for component tests: fake orgs, users, and repositories only.

import type {
  InboxItem,
  InboxList,
  InboxSnapshot,
  LaunchResult,
  Profile,
  Rate,
  Settings,
  Worktree,
  WorktreeRepo,
  WorktreesSnapshot,
} from '../platform'

export function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: 1,
    kind: 'pr',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    repo: 'acme-org/example-repo',
    author: 'octo-user',
    updatedAt: '2026-06-15T09:00:00Z',
    trusted: true,
    isCrossRepository: false,
    authorAssociation: 'OWNER',
    branch: 'widget-cache',
    isDraft: false,
    ...overrides,
  }
}

export function profile(overrides: Partial<Profile> = {}): Profile {
  return {
    name: 'default',
    configDir: '',
    model: '',
    effort: '',
    permissionMode: 'full',
    planMode: false,
    addDirs: [],
    extraArgs: [],
    env: {},
    ...overrides,
  }
}

export function settings(overrides: Partial<Settings> = {}): Settings {
  return { defaultProfile: 'default', worktreeDefault: true, terminal: 'iterm2', cloneRoot: '~/projects', ...overrides }
}

export function launchResult(overrides: Partial<LaunchResult> = {}): LaunchResult {
  return {
    scriptPath: '~/launch/pr-7.command',
    clonePath: '~/projects/example-repo',
    worktree: 'pr-7',
    profile: 'default',
    permission: 'full access',
    terminal: 'iterm2',
    ...overrides,
  }
}

export const unknownRate: Rate = { known: false, limit: 0, remaining: 0, used: 0, cost: 0, resetAt: '', takenAt: '' }

export function list(items: InboxItem[], total = items.length): InboxList {
  return { items, shown: items.length, total }
}

export function snapshot(overrides: Partial<InboxSnapshot> = {}): InboxSnapshot {
  return {
    viewer: 'octo-user',
    reviewRequested: list([]),
    authored: list([]),
    assigned: list([]),
    rate: { graphql: unknownRate, core: unknownRate, search: unknownRate },
    lastRefreshedAt: '',
    lastError: '',
    scopeWarning: '',
    ...overrides,
  }
}

export function worktree(overrides: Partial<Worktree> = {}): Worktree {
  return {
    path: '/home/user/projects/example-repo/.claude/worktrees/pr-7',
    displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
    branch: 'worktree-pr-7',
    detached: false,
    main: false,
    locked: false,
    lockReason: '',
    staleLock: false,
    prunable: false,
    prunableReason: '',
    dirty: false,
    dirtyCount: 0,
    lastCommitUnix: 0,
    claudeCreated: true,
    canRemove: true,
    canForceRemove: false,
    error: '',
    ...overrides,
  }
}

// mainWorktree is a repository's main worktree, which is never removable.
export function mainWorktree(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    path: '/home/user/projects/example-repo',
    displayPath: '~/projects/example-repo',
    branch: 'main',
    main: true,
    claudeCreated: false,
    canRemove: false,
    ...overrides,
  })
}

export function worktreeRepo(overrides: Partial<WorktreeRepo> = {}): WorktreeRepo {
  return {
    path: '/home/user/projects/example-repo',
    displayPath: '~/projects/example-repo',
    name: 'example-repo',
    hasStale: false,
    error: '',
    worktrees: [mainWorktree(), worktree()],
    ...overrides,
  }
}

export function worktreesSnapshot(overrides: Partial<WorktreesSnapshot> = {}): WorktreesSnapshot {
  return { root: '~/projects', repos: [worktreeRepo()], cleanCount: 1, listedAt: '2026-06-15T12:00:00Z', ...overrides }
}
