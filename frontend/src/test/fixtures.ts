// Fake data for component tests: fake orgs, users, and repositories only.

import type {
  Account,
  InboxItem,
  ItemDetails,
  InboxList,
  InboxSnapshot,
  ItemResolution,
  LaunchDefaults,
  LaunchPreview,
  LaunchResult,
  LinkedItem,
  Profile,
  Prompt,
  PromptCheckResult,
  PromptVariable,
  Rate,
  Resolution,
  Settings,
  SweepReport,
  Worktree,
  WorktreeCleanup,
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
    trust: 'member',
    trusted: true,
    authorIsBot: false,
    isCrossRepository: false,
    authorAssociation: 'OWNER',
    branch: 'widget-cache',
    isDraft: false,
    details: null,
    dismissedAt: '',
    snoozedUntil: '',
    wokeBy: null,
    ...overrides,
  }
}

// details are a pull request's details: passing checks, an approval out of two
// reviewers, a few comments, and two labels.
export function details(overrides: Partial<ItemDetails> = {}): ItemDetails {
  return {
    labels: [
      { name: 'enhancement', color: '#a2eeef' },
      { name: 'needs-docs', color: '' },
    ],
    comments: 4,
    additions: 120,
    deletions: 30,
    changedFiles: 6,
    commits: 3,
    mergeable: 'MERGEABLE',
    checks: { state: 'SUCCESS', passed: 12, failed: 0, pending: 0, skipped: 3 },
    reviews: { decision: 'APPROVED', reviewers: 2, reviewed: 1, approvals: 1, changesRequested: 0 },
    fetchedAt: '2026-06-15T11:59:00Z',
    ...overrides,
  }
}

export function profile(overrides: Partial<Profile> = {}): Profile {
  return {
    name: 'default',
    configDir: '',
    folder: '~/.claude',
    missing: false,
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
  return {
    defaultProfile: 'default',
    worktreeDefault: true,
    worktreeAutoCleanup: true,
    terminal: 'iterm2',
    cloneRoot: '~/projects',
    trustedBots: ['dependabot', 'github-actions', 'renovate'],
    defaultTrustedBots: ['dependabot', 'github-actions', 'renovate'],
    snoozeDays: 3,
    snoozeWake: { commits: 1, reviews: 'decisions', comments: 0 },
    windowSize: { width: 1280, height: 800 },
    defaultWindowSize: { width: 1280, height: 800 },
    ...overrides,
  }
}

export function launchResult(overrides: Partial<LaunchResult> = {}): LaunchResult {
  return {
    scriptPath: '~/launch/pr-7.command',
    clonePath: '~/projects/example-repo',
    worktree: 'pr-7',
    prompt: 'Review',
    profile: 'default',
    permission: 'full access',
    terminal: 'iterm2',
    ...overrides,
  }
}

// prompt is the review starter as the library lists it: the app's default for pull
// requests.
export function prompt(overrides: Partial<Prompt> = {}): Prompt {
  return {
    id: 1,
    name: 'Review a pull request',
    nickname: 'Review',
    appliesTo: 'pr',
    body: 'Review pull request #{{.Number}} in {{.Repo}}: {{.Title}}\n{{.URL}}\n',
    model: '',
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: [],
    extraFlags: '',
    starterKey: 'review-pr',
    modified: false,
    usage: { defaults: ['pr'], orgs: [], repos: [], kinds: ['pr'] },
    updatedAt: '2026-06-15T09:00:00Z',
    ...overrides,
  }
}

// prompts is a small library: two pull request prompts, an issue prompt, and one for both.
export function prompts(): Prompt[] {
  return [
    prompt({
      id: 3,
      name: 'Fix a pull request’s failing checks',
      nickname: 'Fix checks',
      starterKey: 'fix-checks',
      usage: { defaults: [], orgs: [], repos: [], kinds: [] },
    }),
    prompt({
      id: 2,
      name: 'Take a quick look for blockers',
      nickname: 'Quick look',
      model: 'sonnet',
      effort: 'low',
      worktree: false,
      starterKey: 'quick-look',
      usage: { defaults: [], orgs: ['acme-org'], repos: ['acme-org/api'], kinds: ['pr'] },
    }),
    prompt(),
    prompt({
      id: 4,
      name: 'Work on an issue',
      nickname: 'Work on it',
      appliesTo: 'issue',
      body: 'Work on issue #{{.Number}}\n',
      starterKey: 'work-on-issue',
      usage: { defaults: ['issue'], orgs: [], repos: [], kinds: ['issue'] },
    }),
    prompt({
      id: 5,
      name: 'Explain the change',
      nickname: 'Explain',
      appliesTo: 'any',
      body: 'Explain {{.URL}}\n',
      starterKey: '',
      usage: { defaults: [], orgs: [], repos: [], kinds: [] },
    }),
  ]
}

export function variables(): PromptVariable[] {
  return [
    { name: 'Kind', group: 'Item', type: 'text', description: '"pull request" or "issue"', prOnly: false },
    { name: 'Number', group: 'Item', type: 'number', description: 'The pull request or issue number', prOnly: false },
    { name: 'Title', group: 'Item', type: 'text', description: 'The title, cut to 200 characters', prOnly: false },
    { name: 'URL', group: 'Item', type: 'text', description: 'The web address on GitHub', prOnly: false },
    { name: 'Repo', group: 'Repository', type: 'text', description: 'The repository, as owner/name', prOnly: false },
    { name: 'Branch', group: 'Pull request', type: 'text', description: 'The head branch', prOnly: true },
    {
      name: 'InWorktree',
      group: 'Session',
      type: 'yes/no',
      description: 'The session starts in its own worktree',
      prOnly: false,
    },
  ]
}

// checkResult is a clean check of a prompt filled in for pull request 7.
export function checkResult(overrides: Partial<PromptCheckResult> = {}): PromptCheckResult {
  return {
    fields: [],
    diagnostics: [],
    preview: 'Review pull request #7 in acme-org/example-repo: Add the widget cache\n',
    notice: '',
    previewError: '',
    previewNote: '',
    sample: false,
    values: { Title: 'Add the widget cache', Number: '7', Branch: 'widget-cache', URL: '' },
    characters: 66,
    ...overrides,
  }
}

// resolution is a trusted pull request's resolved launch settings: the review prompt and
// the default profile from the app's settings, nothing else set.
export function resolution(overrides: Partial<Resolution> = {}): Resolution {
  return {
    itemId: 1,
    kind: 'pr',
    owner: 'acme-org',
    repo: 'acme-org/example-repo',
    trusted: true,
    agent: { id: 'claude', name: 'Claude', command: 'claude' },
    agentSource: 'app',
    profile: { value: 'default', source: 'app', inherited: 'default', inheritedSource: 'app' },
    prompt: {
      id: 1,
      nickname: 'Review',
      name: 'Review a pull request',
      source: 'app',
      inheritedId: 1,
      inheritedSource: 'app',
    },
    model: { value: '', source: 'agent', inherited: '', inheritedSource: 'agent' },
    effort: { value: 'high', source: 'profile', inherited: 'high', inheritedSource: 'profile' },
    planMode: { value: false, source: 'profile', inherited: false, inheritedSource: 'profile' },
    worktree: { value: true, source: 'app', inherited: true, inheritedSource: 'app' },
    extraArgs: [],
    warnings: [],
    ...overrides,
  }
}

export function itemResolution(overrides: Partial<ItemResolution> = {}): ItemResolution {
  return { ...resolution(), problem: '', ...overrides }
}

export function launchDefaults(overrides: Partial<LaunchDefaults> = {}): LaunchDefaults {
  return {
    owner: 'acme-org',
    name: '',
    agent: '',
    profile: 'work',
    prPromptId: null,
    issuePromptId: null,
    model: '',
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: [],
    extraFlags: '',
    updatedAt: '2026-06-15T09:00:00Z',
    ...overrides,
  }
}

export function launchPreview(overrides: Partial<LaunchPreview> = {}): LaunchPreview {
  return {
    resolution: resolution(),
    dir: '~/projects/example-repo',
    worktree: 'pr-7',
    command: "claude <prompt> --dangerously-skip-permissions '--name=Review · example-repo#7' --worktree '#7'",
    env: ['unset CLAUDE_CONFIG_DIR'],
    permission: 'full access',
    prompt: 'Review pull request #7 in acme-org/example-repo: Add the widget cache\n',
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
    agentLock: false,
    staleLock: false,
    canUnlock: false,
    prunable: false,
    prunableReason: '',
    dirty: false,
    dirtyCount: 0,
    lastCommitUnix: 0,
    head: 'fc1c3975011e66491785ace891c893e4fc716587',
    unpushed: 0,
    onGitHub: true,
    item: null,
    stage: '',
    ownBranch: true,
    claudeCreated: true,
    canRemove: true,
    canForceRemove: false,
    canCleanUp: false,
    error: '',
    ...overrides,
  }
}

// linkedItem is the pull request a worktree was made for: example-repo#7, merged.
export function linkedItem(overrides: Partial<LinkedItem> = {}): LinkedItem {
  return {
    kind: 'pr',
    owner: 'acme-org',
    name: 'example-repo',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    state: 'merged',
    closedAt: '2026-06-15T10:00:00Z',
    headOid: 'fc1c3975011e66491785ace891c893e4fc716587',
    launched: true,
    ...overrides,
  }
}

// finishedWorktree is a worktree whose pull request merged, ready to clean up.
export function finishedWorktree(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({ item: linkedItem(), stage: 'finished', canCleanUp: true, ...overrides })
}

// worktreeCleanup is example-repo's pr-7, cleaned up automatically after its pull request
// merged.
export function worktreeCleanup(overrides: Partial<WorktreeCleanup> = {}): WorktreeCleanup {
  return {
    id: 1,
    cleanedAt: '2026-06-15T10:00:00Z',
    automatic: true,
    repo: 'example-repo',
    worktree: 'pr-7',
    displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
    deletedBranch: 'worktree-pr-7',
    head: 'fc1c3975011e66491785ace891c893e4fc716587',
    item: linkedItem({ closedAt: '', headOid: '', launched: false }),
    ...overrides,
  }
}

export function sweepReport(overrides: Partial<SweepReport> = {}): SweepReport {
  return {
    seq: 1,
    at: '2026-06-15T12:00:00Z',
    enabled: true,
    cleaned: [],
    failed: [],
    leftover: 0,
    worktreeCount: 0,
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
  return {
    root: '~/projects',
    repos: [worktreeRepo()],
    cleanCount: 1,
    finishedCount: 0,
    reviewCount: 0,
    worktreeCount: 1,
    linkError: '',
    listedAt: '2026-06-15T12:00:00Z',
    ...overrides,
  }
}
