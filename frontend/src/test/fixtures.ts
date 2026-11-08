// Fake data for component tests: fake orgs, users, and repositories only.

import { brand } from '../generated/brand'
import type {
  Account,
  AppNotification,
  BundlePrompt,
  CloneTarget,
  FileCheck,
  ImportEntry,
  Info,
  InboxItem,
  ItemDetails,
  InboxList,
  InboxSnapshot,
  ItemResolution,
  LaunchDefaults,
  LaunchHold,
  LaunchPreview,
  LaunchResult,
  LinkedItem,
  Profile,
  Project,
  Prompt,
  PromptBackup,
  PromptCheckResult,
  PromptVariable,
  Rate,
  RemoveResult,
  Resolution,
  SensitiveCategory,
  SensitiveGroup,
  Settings,
  SweepReport,
  Worktree,
  WorktreeCleanup,
  WorktreeRepo,
  WorktreesSnapshot,
} from '../platform'

// appInfo is a development build's identity on macOS, with no data reset.
export function appInfo(overrides: Partial<Info> = {}): Info {
  return {
    name: brand.name,
    tagline: brand.tagline,
    version: 'dev',
    release: '0.0.0',
    build: '0',
    system: 'macOS 15.1 (24B83)',
    arch: 'arm64',
    bugReportUrl: 'https://github.com/acme-org/example-repo/issues/new',
    ...overrides,
  }
}

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
    files: null,
    dismissedAt: '',
    snoozedUntil: '',
    wokeBy: null,
    pinnedAt: '',
    ...overrides,
  }
}

// notification is an unread review request for pull request 7 of example-repo.
export function notification(overrides: Partial<AppNotification> = {}): AppNotification {
  return {
    id: 1,
    kind: 'review_requested',
    createdAt: '2026-06-15T11:55:00Z',
    read: false,
    itemId: 1,
    itemKind: 'pr',
    repo: 'acme-org/example-repo',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    author: 'octo-other',
    detail: '',
    ...overrides,
  }
}

// project is an issue's place in the roadmap project, with its status there.
export function project(overrides: Partial<Project> = {}): Project {
  return {
    title: 'Roadmap',
    url: 'https://github.com/orgs/acme-org/projects/1',
    status: 'In progress',
    statusColor: '#bf8700',
    fields: [],
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
    projects: [],
    subIssues: { total: 0, completed: 0 },
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
    sensitiveFiles: { scope: 'collaborators', off: [], patterns: [] },
    sensitiveCategories: sensitiveCategories(),
    menuBar: true,
    bannersOff: [],
    theme: 'system',
    collapsedSections: [],
    projectsTipDismissed: false,
    inboxOrder: { statusSorted: [], statusOrder: [] },
    ...overrides,
  }
}

// sensitiveCategories are three built-in categories of sensitive files, with a few of
// their patterns each: the locked agent category first.
export function sensitiveCategories(): SensitiveCategory[] {
  return [
    {
      id: 'agent',
      label: 'Agent configuration',
      short: 'Agent config',
      description: 'Settings, hooks, MCP servers, and instructions that coding agents load.',
      level: 'danger',
      locked: true,
      patterns: ['.claude/', '.mcp.json', 'CLAUDE.md'],
    },
    {
      id: 'workflows',
      label: 'Workflows and CI',
      short: 'Workflows',
      description: 'Run on CI with the repository’s token and secrets.',
      level: 'danger',
      locked: false,
      patterns: ['.github/workflows/', '.github/actions/'],
    },
    {
      id: 'dependencies',
      label: 'Dependencies',
      short: 'Dependencies',
      description: 'Bring in third-party code, or change where it comes from.',
      level: 'warning',
      locked: false,
      patterns: ['go.mod', 'go.sum'],
    },
  ]
}

// agentGroup is a pull request's agent files: new agent settings.
export function agentGroup(overrides: Partial<SensitiveGroup> = {}): SensitiveGroup {
  return {
    category: 'agent',
    label: 'Agent configuration',
    short: 'Agent config',
    description: 'Settings, hooks, MCP servers, and instructions that coding agents load.',
    level: 'danger',
    files: [
      {
        path: '.claude/settings.json',
        previousPath: '',
        change: 'added',
        pattern: '.claude/',
        url: 'https://github.com/acme-org/example-repo/pull/7/files#diff-aaaa',
      },
    ],
    ...overrides,
  }
}

// workflowsGroup is a pull request's workflow files: one moved out of the workflows folder.
export function workflowsGroup(overrides: Partial<SensitiveGroup> = {}): SensitiveGroup {
  return {
    category: 'workflows',
    label: 'Workflows and CI',
    short: 'Workflows',
    description: 'Run on CI with the repository’s token and secrets.',
    level: 'danger',
    files: [
      {
        path: 'docs/old-codeql.yml',
        previousPath: '.github/workflows/codeql.yml',
        change: 'renamed',
        pattern: '.github/workflows/',
        url: 'https://github.com/acme-org/example-repo/pull/7/files#diff-bbbb',
      },
    ],
    ...overrides,
  }
}

// fileCheck is a finished check of a pull request's files that flagged agent settings and
// a workflow.
export function fileCheck(overrides: Partial<FileCheck> = {}): FileCheck {
  return {
    status: 'flagged',
    head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    checked: 6,
    total: 6,
    complete: true,
    groups: [agentGroup(), workflowsGroup()],
    checkedAt: '2026-06-15T11:58:00Z',
    error: '',
    ...overrides,
  }
}

// launchHold is a launch held back because the pull request changes agent settings.
export function launchHold(overrides: Partial<LaunchHold> = {}): LaunchHold {
  return {
    key: 'agentFiles:a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    reason: 'agentFiles',
    head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    files: agentGroup().files,
    checked: 6,
    total: 6,
    error: '',
    atHead: true,
    dir: '~/projects/example-repo',
    worktree: 'pr-7',
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
    hold: null,
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
    createdAt: '2026-06-01T09:00:00Z',
    updatedAt: '2026-06-15T09:00:00Z',
    ...overrides,
  }
}

// bundlePrompt is a prompt as a prompt file holds it: one the user wrote.
export function bundlePrompt(overrides: Partial<BundlePrompt> = {}): BundlePrompt {
  return {
    name: 'Look for security problems',
    nickname: 'Security',
    appliesTo: 'any',
    body: 'Look for security problems in {{.URL}}\n',
    model: '',
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: [],
    starterKey: '',
    createdAt: '2026-11-07T09:00:00Z',
    updatedAt: '2026-11-07T09:30:00Z',
    ...overrides,
  }
}

// importEntry is a prompt a file holds that importing would add.
export function importEntry(overrides: Partial<ImportEntry> = {}): ImportEntry {
  return {
    prompt: bundlePrompt(),
    outcome: 'add',
    nickname: 'Security',
    replaces: '',
    notes: [],
    ...overrides,
  }
}

// promptBackup is the database the app set aside when an update could not read it.
export function promptBackup(overrides: Partial<PromptBackup> = {}): PromptBackup {
  return {
    path: '/data/app.db.broken-20261108T165152Z',
    kind: 'set-aside',
    at: '2026-11-08T16:51:52Z',
    prompts: 4,
    new: 3,
    error: '',
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

// cloneTarget is example-repo's clone under the clone root, already cloned.
export function cloneTarget(overrides: Partial<CloneTarget> = {}): CloneTarget {
  return { repo: 'acme-org/example-repo', cloned: true, dir: '~/projects/example-repo', taken: false, ...overrides }
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
    projectAccess: 'on',
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

// removeResult is example-repo's pr-7 worktree, removed along with its branch.
export function removeResult(overrides: Partial<RemoveResult> = {}): RemoveResult {
  return {
    repoPath: '/home/user/projects/example-repo',
    path: '/home/user/projects/example-repo/.claude/worktrees/pr-7',
    displayPath: '~/projects/example-repo/.claude/worktrees/pr-7',
    removed: true,
    deletedBranch: 'worktree-pr-7',
    item: 'acme-org/example-repo#7',
    reason: '',
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
