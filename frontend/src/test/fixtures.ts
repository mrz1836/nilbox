// Fake data for component tests: fake orgs, users, and repositories only.

import type {
  Account,
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

// rate is a GraphQL-sized budget reading, taken a minute before the tests' noon and
// resetting 42 minutes after it.
export function rate(overrides: Partial<Rate> = {}): Rate {
  return {
    known: true,
    limit: 5000,
    remaining: 4980,
    used: 20,
    cost: 1,
    resetAt: '2026-06-15T12:42:00Z',
    takenAt: '2026-06-15T11:59:00Z',
    ...overrides,
  }
}

// avatarDataURL is a stand-in for a cached avatar; the image never decodes in jsdom.
export const avatarDataURL = 'data:image/png;base64,bm90LXJlYWxseS1hLXBuZw=='

export function account(overrides: Partial<Account> = {}): Account {
  return {
    login: 'octo-user',
    name: 'Octo User',
    profileUrl: 'https://github.com/octo-user',
    avatar: avatarDataURL,
    ...overrides,
  }
}

// signedOut is the account before the first refresh.
export const signedOut: Account = { login: '', name: '', profileUrl: '', avatar: '' }

export function list(items: InboxItem[], total = items.length): InboxList {
  return { items, shown: items.length, total }
}

export function snapshot(overrides: Partial<InboxSnapshot> = {}): InboxSnapshot {
  return {
    account: account(),
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
