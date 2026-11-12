// Fake data for component tests: fake orgs, users, and repositories only.

import { brand } from '../generated/brand'
import type {
  Account,
  AfterMerge,
  Agent,
  AgentSession,
  AgentInfo,
  AgentVersion,
  ToolVersion,
  AppNotification,
  BudgetGauge,
  BudgetReading,
  BudgetWrites,
  BranchStatus,
  BundlePrompt,
  DefaultPrompts,
  Review,
  ReviewFile,
  ReviewOffer,
  ReviewReplyList,
  ReviewResult,
  CloneTarget,
  FileCheck,
  HookActionResult,
  HookChecksResult,
  HookInstallAllResult,
  HookRepo,
  HooksList,
  ImportEntry,
  InboxItem,
  InboxList,
  InboxSnapshot,
  Info,
  InstallPlan,
  InstallResult,
  IntegrationsConfig,
  IntegrationTool,
  IntegrationTools,
  ItemDetails,
  ItemResolution,
  LaunchDefaults,
  LaunchHold,
  LaunchPreview,
  LaunchResult,
  LinkedItem,
  MergedBranch,
  Profile,
  Project,
  Prompt,
  PromptBackup,
  PromptCheckResult,
  PromptVariable,
  Rate,
  ReleaseCandidate,
  ReleaseCandidates,
  ReleaseCheck,
  ReleaseOffer,
  ReleaseResult,
  RemoveResult,
  RepoScripts,
  Resolution,
  ScriptClone,
  ScriptEntry,
  ScriptInput,
  ScriptQuestion,
  ScriptRun,
  ScriptRunChunk,
  ScriptSection,
  SensitiveCategory,
  SensitiveGroup,
  SessionsSnapshot,
  Settings,
  SweepReport,
  ToolCopy,
  UserScript,
  Worktree,
  WorktreeBranch,
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
    ...overrides,
  }
}

// agentVersions are both coding agents, installed, as their commands report their
// versions.
export function agentVersions(): AgentVersion[] {
  return [
    { agent: 'claude', name: 'Claude', installed: true, path: '~/.local/bin/claude', version: '2.1.319', error: '' },
    { agent: 'codex', name: 'Codex', installed: true, path: '/opt/bin/codex', version: '0.163.0', error: '' },
  ]
}

// toolVersions are the tools other than the agents, installed, as their commands report
// their versions.
export function toolVersions(): ToolVersion[] {
  return [
    { tool: 'gh', name: 'GitHub CLI', installed: true, path: '/opt/homebrew/bin/gh', version: '2.83.1', error: '' },
  ]
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
    assigned: false,
    reviewRequest: 'none',
    reviewTeams: [],
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
    runId: 0,
    runStarted: '',
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
    autoMerge: '',
    fetchedAt: '2026-06-15T11:59:00Z',
    ...overrides,
  }
}

// claudeAgent is Claude Code as the backend describes it.
export function claudeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'claude',
    name: 'Claude',
    command: 'claude',
    configEnv: 'CLAUDE_CONFIG_DIR',
    configDir: '.claude',
    markers: [
      { name: '.claude.json', dir: false },
      { name: 'settings.json', dir: false },
      { name: 'projects', dir: true },
    ],
    effortLevels: [
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
      { value: 'xhigh', label: 'Extra high' },
      { value: 'max', label: 'Max' },
    ],
    models: [
      { value: 'fable', label: 'Fable' },
      { value: 'opus', label: 'Opus' },
      { value: 'sonnet', label: 'Sonnet' },
      { value: 'haiku', label: 'Haiku' },
    ],
    permissionModes: [
      { value: 'acceptEdits', label: 'Accepts edits' },
      { value: 'plan', label: 'Plans only' },
    ],
    planLabel: 'Plan mode',
    sessionNames: true,
    sessionIds: true,
    fastMode: false,
    configOverrides: false,
    authFile: '',
    authEnv: [],
    modelsCache: '',
    ...overrides,
  }
}

// codexAgent is OpenAI Codex as the backend describes it, with two models from its cache.
export function codexAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'codex',
    name: 'Codex',
    command: 'codex',
    configEnv: 'CODEX_HOME',
    configDir: '.codex',
    markers: [
      { name: 'auth.json', dir: false },
      { name: 'config.toml', dir: false },
      { name: 'sessions', dir: true },
    ],
    effortLevels: [
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
      { value: 'xhigh', label: 'Extra high' },
      { value: 'max', label: 'Max' },
      { value: 'ultra', label: 'Ultra' },
    ],
    models: [
      { value: 'gpt-6-sol', label: 'GPT-6-Sol' },
      { value: 'gpt-6-luna', label: 'GPT-6-Luna' },
    ],
    permissionModes: [
      { value: 'read-only', label: 'Read-only' },
      { value: 'workspace-write', label: 'Writes in the workspace' },
      { value: 'danger-full-access', label: 'Full access, asks first' },
    ],
    planLabel: 'Read-only',
    sessionNames: false,
    sessionIds: false,
    fastMode: true,
    configOverrides: true,
    authFile: 'auth.json',
    authEnv: ['OPENAI_API_KEY', 'CODEX_API_KEY'],
    modelsCache: 'models_cache.json',
    ...overrides,
  }
}

// agents are both coding agents, installed.
export function agents(): AgentInfo[] {
  return [
    { ...claudeAgent(), installed: true, path: '/opt/bin/claude' },
    { ...codexAgent(), installed: true, path: '/opt/bin/codex' },
  ]
}

export function profile(overrides: Partial<Profile> = {}): Profile {
  return {
    agent: 'claude',
    name: 'default',
    configDir: '',
    folder: '~/.claude',
    missing: false,
    signedIn: true,
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
    defaultAgent: 'claude',
    defaultProfiles: { claude: 'default', codex: 'default' },
    worktreeDefault: true,
    worktreeAutoCleanup: true,
    terminal: 'iterm2',
    followSessions: true,
    codexEffort: false,
    codexFast: false,
    cloneRoot: '~/projects',
    trustedBots: ['dependabot', 'github-actions', 'renovate'],
    defaultTrustedBots: ['dependabot', 'github-actions', 'renovate'],
    snoozeDays: 3,
    snoozeWake: { commits: 1, reviews: 'decisions', comments: 0 },
    windowSize: { width: 1440, height: 900 },
    defaultWindowSize: { width: 1440, height: 900 },
    sensitiveFiles: { scope: 'collaborators', off: [], patterns: [] },
    branches: { own: true, pullRequest: true, merged: true, protected: ['main', 'master'] },
    defaultProtectedBranches: ['main', 'master'],
    sensitiveCategories: sensitiveCategories(),
    menuBar: true,
    bannersOff: ['session_needs_you', 'session_finished'],
    theme: 'system',
    textSize: 'default',
    collapsedSections: [],
    projectsTipDismissed: false,
    inboxOrder: { statusSorted: [], statusOrder: [] },
    mergeMethod: 'repo',
    integrations: integrationsConfig(),
    ...overrides,
  }
}

// integrationsConfig is how a fresh database sets the integrations up: every switch
// automatic, no copy of a tool chosen, runs in the app, a merge offering a release, and
// Repo scripts listing every source.
export function integrationsConfig(overrides: Partial<IntegrationsConfig> = {}): IntegrationsConfig {
  return {
    switches: { 'mage-x': 'auto', 'go-pre-commit': 'auto', scripts: 'auto' },
    paths: { 'mage-x': '', 'go-pre-commit': '' },
    runsIn: 'app',
    offerAfterMerge: true,
    scriptSources: { magexTargets: true, repoFiles: true },
    announced: false,
    lastPage: '',
    lastRepo: '',
    ...overrides,
  }
}

// toolCopy is a copy of magex from a GitHub release, in ~/.local/bin, that runs and is on
// the shell's PATH.
export function toolCopy(overrides: Partial<ToolCopy> = {}): ToolCopy {
  return {
    path: '/Users/example/.local/bin/magex',
    displayPath: '~/.local/bin/magex',
    link: false,
    target: '',
    runs: true,
    version: { raw: '1.29.5', commit: '', built: '' },
    error: '',
    source: 'release',
    onPath: true,
    installedAt: '',
    archive: '',
    shadowing: false,
    ...overrides,
  }
}

// installPlan is what installing mage-x would do while the release's copy in ~/.local/bin
// is in use: put the latest release, the same version, in its place.
export function installPlan(overrides: Partial<InstallPlan> = {}): InstallPlan {
  return {
    action: 'reinstall',
    version: '1.29.5',
    archive: 'mage-x_1.29.5_darwin_arm64.tar.gz',
    targetDir: '~/.local/bin',
    target: '~/.local/bin/magex',
    replaces: '1.29.5',
    replacesPath: '~/.local/bin/magex',
    foreign: true,
    blocked: '',
    brewCommand: '',
    updater: '',
    ...overrides,
  }
}

// scriptRun is a magex lint run in acme-org/example-repo that went in the app and
// succeeded.
export function scriptRun(overrides: Partial<ScriptRun> = {}): ScriptRun {
  return {
    id: 1,
    kind: 'script',
    title: 'magex lint',
    source: 'magex',
    repo: 'acme-org/example-repo',
    dir: '~/projects/example-repo',
    argv: ['/Users/example/.local/bin/magex', 'lint'],
    command: 'magex lint',
    runsIn: 'app',
    status: 'succeeded',
    exitCode: 0,
    summary: 'Finished.',
    result: { tool: 'mage-x', intent: null, warnings: [], unrecognized: [], unrecognizedCount: 0, outcome: null },
    logBytes: 12,
    seen: true,
    startedAt: '2026-11-10T16:00:00Z',
    finishedAt: '2026-11-10T16:00:38Z',
    progress: { step: '', working: '', stopRefused: '', stopConfirm: '' },
    canStop: false,
    canRunAgain: true,
    ...overrides,
  }
}

// scriptRunChunk is a run's whole log, ended: one line of output.
export function scriptRunChunk(overrides: Partial<ScriptRunChunk> = {}): ScriptRunChunk {
  return { text: 'lint passed\n', offset: 0, next: 12, size: 12, done: true, restart: false, ...overrides }
}

// installResult is what installing mage-x 1.30.0 said it did, and the tools after it.
export function installResult(overrides: Partial<InstallResult> = {}): InstallResult {
  return { message: 'Installed mage-x 1.30.0 in ~/.local/bin', run: 0, tools: integrationTools(), ...overrides }
}

// integrationTool is mage-x, switched automatically, found, and as new as its latest
// release.
export function integrationTool(overrides: Partial<IntegrationTool> = {}): IntegrationTool {
  return {
    id: 'mage-x',
    name: 'mage-x',
    binary: 'magex',
    purpose: 'Release new versions of your repositories in two clicks.',
    repo: 'acme-org/mage-x',
    releasesUrl: 'https://github.com/acme-org/mage-x/releases',
    minVersion: '1.29.5',
    switch: 'auto',
    on: true,
    state: 'found',
    checking: false,
    copy: toolCopy(),
    others: [],
    chosen: '',
    chosenMissing: false,
    latest: {
      version: '1.29.5',
      url: 'https://github.com/acme-org/mage-x/releases/tag/v1.29.5',
      checkedAt: '2026-06-15T09:00:00Z',
    },
    install: installPlan(),
    progress: { step: '', done: 0, total: 0 },
    selfUpdate: { run: 0, running: false, updated: false, failed: false, message: '' },
    checkedAt: '2026-06-15T11:50:00Z',
    ...overrides,
  }
}

// goPreCommitTool is go-pre-commit, switched automatically, found from Homebrew, and as
// new as its latest release; Homebrew updates it, not the app.
export function goPreCommitTool(overrides: Partial<IntegrationTool> = {}): IntegrationTool {
  return integrationTool({
    id: 'go-pre-commit',
    name: 'go-pre-commit',
    binary: 'go-pre-commit',
    purpose: 'Install, update, and run Git pre-commit hooks in your Go repositories.',
    repo: 'acme-org/go-pre-commit',
    releasesUrl: 'https://github.com/acme-org/go-pre-commit/releases',
    minVersion: '1.12.0',
    copy: toolCopy({
      path: '/opt/homebrew/bin/go-pre-commit',
      displayPath: '/opt/homebrew/bin/go-pre-commit',
      version: { raw: '1.12.3', commit: '', built: '' },
      source: 'homebrew',
    }),
    latest: {
      version: '1.12.3',
      url: 'https://github.com/acme-org/go-pre-commit/releases/tag/v1.12.3',
      checkedAt: '2026-06-15T09:00:00Z',
    },
    install: installPlan({
      version: '1.12.3',
      archive: 'go-pre-commit_1.12.3_darwin_arm64.tar.gz',
      target: '~/.local/bin/go-pre-commit',
      replaces: '',
      replacesPath: '',
      foreign: false,
      blocked: 'Homebrew manages this copy. Update it with brew upgrade go-pre-commit.',
      brewCommand: 'brew upgrade go-pre-commit',
    }),
    ...overrides,
  })
}

// integrationTools is what the integrations service lists once the login shell was read:
// mage-x and go-pre-commit, both found.
export function integrationTools(overrides: Partial<IntegrationTools> = {}): IntegrationTools {
  return { tools: [integrationTool(), goPreCommitTool()], shellChecked: true, shellRead: true, ...overrides }
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
    running: [],
    ...overrides,
  }
}

// runningHold is a launch held back for a Claude session that needs approval in the same
// worktree.
export function runningHold(overrides: Partial<LaunchHold> = {}): LaunchHold {
  return launchHold({
    key: 'running:0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70',
    reason: 'running',
    head: '',
    files: [],
    checked: 0,
    total: 0,
    atHead: false,
    dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    running: [
      {
        sessionId: '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70',
        agent: 'claude',
        doing: 'needs approval',
        since: '2026-06-15T11:58:00Z',
        dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
        worktree: 'pr-7',
        repo: 'acme-org/example-repo',
        number: 7,
        canFocus: true,
      },
    ],
    ...overrides,
  })
}

export function launchResult(overrides: Partial<LaunchResult> = {}): LaunchResult {
  return {
    scriptPath: '~/launch/pr-7.command',
    clonePath: '~/projects/example-repo',
    dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    worktree: 'pr-7',
    worktreeAction: 'create',
    notice: '',
    agent: 'claude',
    prompt: 'Review',
    profile: 'default',
    permission: 'full access',
    terminal: 'iterm2',
    sessionId: '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70',
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
    purpose: 'agent',
    replyAction: '',
    models: {},
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: {},
    extraFlags: {},
    starterKey: 'review-pr',
    modified: false,
    usage: { defaults: ['pr'], orgs: [], repos: [], kinds: ['pr'], replies: [] },
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
    purpose: 'agent',
    action: '',
    models: {},
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: {},
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

// defaultPrompts are the app's default prompts in the small library below, with no
// default replies.
export function defaultPrompts(overrides: Partial<DefaultPrompts> = {}): DefaultPrompts {
  return {
    pr: 1,
    issue: 4,
    replies: { approve: 0, approveBots: 0, requestChanges: 0, comment: 0 },
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
      usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: [] },
    }),
    prompt({
      id: 2,
      name: 'Take a quick look for blockers',
      nickname: 'Quick look',
      models: { claude: 'sonnet' },
      effort: 'low',
      worktree: false,
      starterKey: 'quick-look',
      usage: { defaults: [], orgs: ['acme-org'], repos: ['acme-org/api'], kinds: ['pr'], replies: [] },
    }),
    prompt(),
    prompt({
      id: 4,
      name: 'Work on an issue',
      nickname: 'Work on it',
      appliesTo: 'issue',
      body: 'Work on issue #{{.Number}}\n',
      starterKey: 'work-on-issue',
      usage: { defaults: ['issue'], orgs: [], repos: [], kinds: ['issue'], replies: [] },
    }),
    prompt({
      id: 5,
      name: 'Explain the change',
      nickname: 'Explain',
      appliesTo: 'any',
      body: 'Explain {{.URL}}\n',
      starterKey: '',
      usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: [] },
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
    agent: claudeAgent(),
    agentSource: 'app',
    agentInherited: 'claude',
    agentInheritedSource: 'app',
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
    effortOff: false,
    fast: { value: false, source: 'agent', inherited: false, inheritedSource: 'agent' },
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
    profiles: { claude: 'work' },
    prPromptId: null,
    issuePromptId: null,
    models: {},
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: {},
    extraFlags: {},
    updatedAt: '2026-06-15T09:00:00Z',
    ...overrides,
  }
}

export function launchPreview(overrides: Partial<LaunchPreview> = {}): LaunchPreview {
  return {
    resolution: resolution(),
    dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    worktree: {
      name: 'pr-7',
      path: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
      branch: 'worktree-pr-7',
      action: 'create',
      base: 'the pull request’s head',
      dirty: 0,
    },
    command: "claude <prompt> --dangerously-skip-permissions '--name=Review · example-repo#7'",
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

// unreadBudget is a budget GitHub has not reported yet.
export const unreadBudget: BudgetReading = {
  known: false,
  limit: 0,
  remaining: 0,
  used: 0,
  resetAt: '',
  takenAt: '',
  spent: 0,
  requests: 0,
  complete: false,
}

// reading is a GraphQL-sized budget reading, taken a minute before the tests' noon and
// refilling 42 minutes after it, with nothing the app spent from it.
export function reading(overrides: Partial<BudgetReading> = {}): BudgetReading {
  return {
    known: true,
    limit: 5000,
    remaining: 4980,
    used: 20,
    resetAt: '2026-06-15T12:42:00Z',
    takenAt: '2026-06-15T11:59:00Z',
    spent: 0,
    requests: 0,
    complete: true,
    ...overrides,
  }
}

// budgetGauge is every budget unread, before any refresh, refreshing every five minutes.
export function budgetGauge(overrides: Partial<BudgetGauge> = {}): BudgetGauge {
  return {
    graphql: unreadBudget,
    core: unreadBudget,
    search: unreadBudget,
    refresh: { known: false, at: '', points: 0, items: 0 },
    refreshesPerHour: 12,
    writes: writes(),
    ...overrides,
  }
}

// writes is the app's edits on GitHub: none lately, paced as the app paces them, with a
// 5,000-point REST budget's reserve.
export function writes(overrides: Partial<BudgetWrites> = {}): BudgetWrites {
  return { lastHour: 0, lastMinute: 0, perHour: 200, perMinute: 30, reserve: 50, heldUntil: '', ...overrides }
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
    managed: true,
    legacy: true,
    agent: '',
    lastLaunchedAt: '',
    launches: 0,
    inUse: [],
    canRemove: true,
    canForceRemove: false,
    canCleanUp: false,
    branches: [ownBranch()],
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
    headRef: '',
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
    deletedBranches: ['worktree-pr-7'],
    item: 'acme-org/example-repo#7',
    reason: '',
    ...overrides,
  }
}

// ownBranch is pr-7's own worktree-pr-7 branch, which nothing keeps it from deleting.
export function ownBranch(overrides: Partial<WorktreeBranch> = {}): WorktreeBranch {
  return {
    name: 'worktree-pr-7',
    head: 'fc1c3975011e66491785ace891c893e4fc716587',
    kind: 'own',
    deletable: true,
    reason: '',
    ...overrides,
  }
}

// mergedBranch is example-repo's feature-x, whose pull request #184 merged, at its head.
export function mergedBranch(overrides: Partial<MergedBranch> = {}): MergedBranch {
  return {
    repoPath: '/home/user/projects/example-repo',
    displayRepo: '~/projects/example-repo',
    branch: {
      name: 'feature-x',
      head: 'fc1c3975011e66491785ace891c893e4fc716587',
      kind: 'merged',
      deletable: true,
      reason: '',
    },
    pullRequest: {
      repo: 'acme-org/example-repo',
      number: 184,
      title: 'Add locktimes',
      url: 'https://github.com/acme-org/example-repo/pull/184',
      mergedAt: '2026-06-12T12:00:00Z',
    },
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
    managed: false,
    legacy: false,
    canRemove: false,
    branches: [],
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
    sessionsUnknown: false,
    listedAt: '2026-06-15T12:00:00Z',
    ...overrides,
  }
}

// agentSession is a Claude session on the fixture's pull request #7, launched a few minutes
// before the fixed clock's time and waiting for approval since a minute before it.
export function agentSession(overrides: Partial<AgentSession> = {}): AgentSession {
  return {
    id: '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70',
    agentSessionId: '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70',
    resumedFrom: '',
    agent: 'claude',
    profile: 'default',
    prompt: 'Review',
    terminal: 'iterm2',
    itemId: 1,
    repo: 'acme-org/example-repo',
    kind: 'pr',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    worktree: 'pr-7',
    state: 'needs_approval',
    detail: 'Permission prompt',
    stateAt: '2026-06-15T11:59:00Z',
    lastMessage: 'I need to run the test suite. May I?',
    launchedAt: '2026-06-15T11:55:00Z',
    endedAt: '',
    canFocus: true,
    canResume: false,
    ...overrides,
  }
}

// sessionsSnapshot is what the sessions service lists: followed, with sessions.
export function sessionsSnapshot(sessions: AgentSession[] = [], following = true): SessionsSnapshot {
  return { following, sessions }
}

// reviewFile is a file a reviewed pull request changes: a source file with its diff.
export function reviewFile(overrides: Partial<ReviewFile> = {}): ReviewFile {
  return {
    path: 'cache/cache.go',
    previousPath: '',
    status: 'modified',
    additions: 2,
    deletions: 1,
    patch: '@@ -1,3 +1,4 @@\n package cache\n-var size = 1\n+var size = 2\n+var ttl = 3\n func Get() {}',
    patchState: '',
    category: '',
    level: '',
    ...overrides,
  }
}

// offer is an action the review sheet offers, enabled: Approve, a review, unless overrides
// say otherwise, as the backend marks every offer.
export function offer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return {
    action: 'approve',
    label: 'Approve',
    enabled: true,
    reason: '',
    confirm: false,
    warnings: [],
    reviews: true,
    commentsFirst: false,
    commentBlocked: '',
    ...overrides,
  }
}

// mergeOffer is a merge with no review of its own, which posts the text as a comment
// first, as the backend marks it.
export function mergeOffer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return offer({
    action: 'merge_now',
    label: 'Squash-merge',
    confirm: true,
    reviews: false,
    commentsFirst: true,
    ...overrides,
  })
}

// closeOffer is a close, which asks first and posts the text as a comment first, as the
// backend marks it.
export function closeOffer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return offer({
    action: 'close',
    label: 'Close pull request',
    confirm: true,
    reviews: false,
    commentsFirst: true,
    ...overrides,
  })
}

// review is pull request #7 as the review sheet shows it, read from GitHub: ready to
// merge by squash, waiting on the user's approval, and closable, last in the menu.
export function review(overrides: Partial<Review> = {}): Review {
  return {
    id: 1,
    repo: 'acme-org/example-repo',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    state: 'OPEN',
    author: 'octo-other',
    isBot: false,
    mine: false,
    draft: false,
    headRef: 'widget-cache',
    baseRef: 'main',
    reviewRequested: true,
    labels: ['enhancement'],
    additions: 120,
    deletions: 30,
    commits: 3,
    comments: 4,
    body: 'Adds a cache in front of the widget store.',
    bump: null,
    change: { kind: 'code', label: 'Code', areas: ['cache'] },
    files: [reviewFile()],
    totalFiles: 1,
    filesComplete: true,
    checks: [{ name: 'test', workflow: 'CI', state: 'SUCCESS', outcome: 'passed', url: '', required: true }],
    reviews: [],
    readiness: { state: 'ready', sentence: 'Ready to merge', needs: [] },
    offers: [
      offer({ action: 'approve_merge', label: 'Approve & squash-merge', confirm: true }),
      mergeOffer(),
      offer(),
      offer({ action: 'request_changes', label: 'Request changes' }),
      offer({ action: 'comment', label: 'Comment' }),
      closeOffer(),
    ],
    primary: 'approve_merge',
    mergeMethods: ['MERGE', 'SQUASH'],
    defaultMethod: 'SQUASH',
    autoMerge: '',
    head: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    fetchedAt: '2026-06-15T11:59:00Z',
    stale: false,
    ...overrides,
  }
}

// reviewResult is what an approval came to.
export function reviewResult(overrides: Partial<ReviewResult> = {}): ReviewResult {
  return {
    outcome: 'reviewed',
    message: 'Approved acme-org/example-repo#7',
    reason: '',
    steps: ['Approved'],
    commented: false,
    leftReview: true,
    leftInbox: false,
    method: '',
    ...overrides,
  }
}

// replyList is the approval replies: one for people, one for bots.
export function replyList(overrides: Partial<ReviewReplyList> = {}): ReviewReplyList {
  return {
    replies: [
      { id: 21, name: 'Approve with thanks', nickname: 'LGTM', action: 'approve' },
      { id: 22, name: 'Approve a dependency update', nickname: 'Update OK', action: 'approve' },
    ],
    default: 21,
    defaultBots: 22,
    ...overrides,
  }
}

// replyPrompt is the LGTM starter reply as the prompt library lists it: the default for
// approvals.
export function replyPrompt(overrides: Partial<Prompt> = {}): Prompt {
  return prompt({
    id: 21,
    name: 'Approve with thanks',
    nickname: 'LGTM',
    body: 'Looks good to me. Thanks, @{{.Author}}!\n',
    purpose: 'reply',
    replyAction: 'approve',
    starterKey: 'lgtm',
    usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: ['approve'] },
    ...overrides,
  })
}

// replyPrompts is a small library of review replies, as the library lists them: two
// approvals (the defaults for people and for bots), a request for changes, and a comment
// the user wrote.
export function replyPrompts(): Prompt[] {
  return [
    replyPrompt(),
    replyPrompt({
      id: 22,
      name: 'Approve a dependency update',
      nickname: 'Update OK',
      body: 'Update looks fine.\n',
      starterKey: 'update-ok',
      usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: ['approve_bots'] },
    }),
    replyPrompt({
      id: 23,
      name: 'Ask for changes',
      nickname: 'Needs changes',
      body: 'Thanks, @{{.Author}}. A few things to change first:\n\n- \n',
      replyAction: 'request_changes',
      starterKey: 'needs-changes',
      usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: [] },
    }),
    replyPrompt({
      id: 24,
      name: 'Ask a question',
      nickname: 'Question',
      body: 'A question before I approve, @{{.Author}}:\n\n',
      replyAction: 'comment',
      starterKey: '',
      usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: [] },
    }),
  ]
}

// releaseCandidate is acme-org/sdk with four commits on master since v1.4.2, checks passed
// on the newest, and its clone on a branch of its own.
export function releaseCandidate(overrides: Partial<ReleaseCandidate> = {}): ReleaseCandidate {
  return {
    repo: 'acme-org/sdk',
    dir: '~/projects/sdk',
    group: 'unreleased',
    reason: '',
    version: 'v1.4.2',
    ahead: 4,
    commits: [
      {
        oid: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
        headline: 'Fix retries on 502 (#45)',
        at: '2026-06-15T11:48:00Z',
      },
      {
        oid: 'b2c3d4e5f60718293a4b5c6d7e8f901234567890',
        headline: 'Bump golang.org/x/net (#44)',
        at: '2026-06-13T09:00:00Z',
      },
      {
        oid: 'c3d4e5f60718293a4b5c6d7e8f90123456789012',
        headline: 'Document the client (#43)',
        at: '2026-06-12T09:00:00Z',
      },
      { oid: 'd4e5f60718293a4b5c6d7e8f9012345678901234', headline: 'Tidy the tests (#42)', at: '2026-06-11T09:00:00Z' },
    ],
    defaultBranch: 'master',
    headline: 'Fix retries on 502 (#45)',
    headAt: '2026-06-15T11:48:00Z',
    checks: 'SUCCESS',
    clone: {
      branch: 'feature-x',
      detached: false,
      dirty: 0,
      notPushed: 0,
      behind: false,
      localOnly: [],
      unreadable: false,
    },
    ...overrides,
  }
}

// releaseCandidates is the Release page's list as GitHub was last read five minutes before
// FIXED_NOW: acme-org/sdk unreleased, and one repository in each other group.
export function releaseCandidates(overrides: Partial<ReleaseCandidates> = {}): ReleaseCandidates {
  return {
    repos: [
      releaseCandidate(),
      releaseCandidate({
        repo: 'acme-org/web',
        dir: '~/projects/web',
        group: 'upToDate',
        version: 'v0.9.1',
        ahead: 0,
        commits: [],
        headline: 'Release v0.9.1',
        headAt: '2026-06-12T09:00:00Z',
        clone: null,
      }),
      releaseCandidate({
        repo: 'acme-org/tools',
        dir: '~/projects/tools',
        group: 'noReleases',
        version: '',
        ahead: 0,
        commits: [],
        headline: 'First commit',
        headAt: '2026-06-01T09:00:00Z',
        checks: '',
        clone: null,
      }),
      releaseCandidate({
        repo: 'acme-org/old',
        dir: '~/projects/old',
        group: 'cantRelease',
        reason: 'Archived on GitHub',
        version: 'v2.0.0',
        ahead: 0,
        commits: [],
        headline: 'Archive',
        headAt: '2025-01-01T09:00:00Z',
        checks: '',
        clone: null,
      }),
    ],
    withoutGitHub: 0,
    checkedAt: '2026-06-15T11:55:00Z',
    error: '',
    kept: false,
    refreshing: false,
    ...overrides,
  }
}

// releaseCheck is the sheet's line that the clone has no uncommitted changes.
export function releaseCheck(overrides: Partial<ReleaseCheck> = {}): ReleaseCheck {
  return {
    id: 'dirty',
    state: 'ok',
    text: 'No uncommitted changes',
    fixes: [],
    commands: [],
    link: '',
    runId: 0,
    ...overrides,
  }
}

// releaseOffer is the sheet for a patch of acme-org/sdk from a clone on feature-x, every
// check passed, with a magex that has expect= and json=true, running in the app.
export function releaseOffer(overrides: Partial<ReleaseOffer> = {}): ReleaseOffer {
  return {
    repo: 'acme-org/sdk',
    branch: 'master',
    branches: [{ name: 'master', default: true }],
    branchNote: '',
    current: 'feature-x',
    head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    checks: [
      releaseCheck(),
      releaseCheck({ id: 'where', text: 'You’re on feature-x. magex switches to master, then back.' }),
      releaseCheck({ id: 'reach', text: 'Reaches origin with your git credentials' }),
      releaseCheck({ id: 'checks', text: 'Checks passed on a1b2c3d' }),
    ],
    bump: 'patch',
    version: 'v1.4.3',
    bumps: [
      { bump: 'patch', version: 'v1.4.3', offered: true, note: '', fixes: [] },
      { bump: 'minor', version: 'v1.5.0', offered: true, note: '', fixes: [] },
      { bump: 'major', version: 'v2.0.0', offered: true, note: '', fixes: [] },
    ],
    major: { version: 'v2.0.0', firstStable: false, modulePath: '', moduleWant: '', moduleText: '', confirmed: false },
    ready: true,
    anyway: false,
    button: 'Release v1.4.3',
    sentence: `magex switches to master, pulls, tags the newest commit as v1.4.3, pushes the tag to origin, and switches back to feature-x. It runs here in ${brand.name}.`,
    argv: [
      '/Users/example/.local/bin/magex',
      'version:bump',
      'bump=patch',
      'branch=master',
      'push=true',
      'expect=v1.4.3',
      'json=true',
    ],
    ...overrides,
  }
}

// releaseResult is acme-org/sdk v1.4.3 verified on GitHub, its clone back on feature-x.
export function releaseResult(overrides: Partial<ReleaseResult> = {}): ReleaseResult {
  return {
    kind: 'released',
    version: 'v1.4.3',
    tag: 'v1.4.3',
    moved: false,
    commit: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    newer: false,
    taken: false,
    tagged: true,
    branch: 'feature-x',
    detached: false,
    original: 'feature-x',
    back: true,
    inProgress: '',
    tagUrl: 'https://github.com/acme-org/sdk/releases/tag/v1.4.3',
    workflowUrl: 'https://github.com/acme-org/sdk/actions?query=branch%3Av1.4.3',
    pushWarning: '',
    commands: [],
    failure: null,
    error: '',
    ...overrides,
  }
}

// releaseRun is the release of acme-org/sdk v1.4.3 from master, in the app, verified on
// GitHub.
export function releaseRun(overrides: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({
    id: 7,
    kind: 'release',
    title: 'Release v1.4.3',
    source: 'magex',
    repo: 'acme-org/sdk',
    dir: '~/projects/sdk',
    argv: ['/Users/example/.local/bin/magex', 'version:bump', 'bump=patch', 'branch=master', 'push=true'],
    command: 'magex version:bump bump=patch branch=master push=true',
    summary: 'v1.4.3 is on GitHub.',
    result: {
      tool: 'mage-x',
      intent: { version: 'v1.4.3', bump: 'patch', branch: 'master', original: 'feature-x', head: '' },
      warnings: [],
      unrecognized: [],
      unrecognizedCount: 0,
      outcome: { kind: 'released', tag: 'v1.4.3' },
    },
    canRunAgain: false,
    ...overrides,
  })
}

// afterMerge is a merge in acme-org/sdk that offers its release.
export function afterMerge(overrides: Partial<AfterMerge> = {}): AfterMerge {
  return { offer: true, repo: 'acme-org/sdk', dir: '~/projects/sdk', branch: 'master', ...overrides }
}

// hookRepo is acme-org/web with go-pre-commit's current hook, executable, every check on
// but gitleaks, and its last checks passed two hours before FIXED_NOW.
export function hookRepo(overrides: Partial<HookRepo> = {}): HookRepo {
  return {
    repo: 'acme-org/web',
    name: 'acme-org/web',
    dir: '~/projects/web',
    group: 'ready',
    state: 'ready',
    action: '',
    otherTool: '',
    hooksDir: '~/projects/web/.git/hooks',
    shared: false,
    sharedWith: 0,
    alsoInstalled: [],
    backups: [],
    ciVersion: '',
    checks: [
      { name: 'eof', on: true },
      { name: 'fumpt', on: true },
      { name: 'gitleaks', on: false },
      { name: 'lint', on: true },
      { name: 'mod-tidy', on: true },
      { name: 'whitespace', on: true },
    ],
    busy: '',
    lastChecks: {
      id: 21,
      status: 'succeeded',
      summary: 'All 5 checks passed on 41 files in 38 s.',
      finishedAt: '2026-06-15T10:00:00Z',
    },
    error: '',
    ...overrides,
  }
}

// noChecks is a row's last checks when none has run.
const noChecks = { id: 0, status: 'succeeded', summary: '', finishedAt: '' } as const

// hooksList is the Hooks page as the clones were scanned a minute before FIXED_NOW: one
// clone with no hook, one outdated, one with husky's, one in a hooks folder six clones
// share, acme-org/web ready, one turned off, and two clones without a config.
export function hooksList(overrides: Partial<HooksList> = {}): HooksList {
  return {
    tool: { version: '1.12.3', path: '/opt/homebrew/bin/go-pre-commit', state: 'found', off: false, usable: true },
    root: '~/projects',
    repos: [
      hookRepo({
        repo: 'acme-org/api',
        name: 'acme-org/api',
        dir: '~/projects/api',
        group: 'attention',
        state: 'missing',
        action: 'install',
        hooksDir: '~/projects/api/.git/hooks',
        lastChecks: noChecks,
      }),
      hookRepo({
        repo: 'acme-org/cli',
        name: 'acme-org/cli',
        dir: '~/projects/cli',
        group: 'attention',
        state: 'outdated',
        action: 'update',
        hooksDir: '~/projects/cli/.git/hooks',
        lastChecks: noChecks,
      }),
      hookRepo({
        repo: 'acme-org/ops',
        name: 'acme-org/ops',
        dir: '~/projects/ops',
        group: 'attention',
        state: 'missing',
        action: 'install',
        hooksDir: '~/.githooks',
        shared: true,
        sharedWith: 6,
        lastChecks: noChecks,
      }),
      hookRepo({
        repo: 'acme-org/sdk',
        name: 'acme-org/sdk',
        dir: '~/projects/sdk',
        group: 'attention',
        state: 'other',
        action: 'replace',
        otherTool: 'husky',
        hooksDir: '~/projects/sdk/.git/hooks',
        lastChecks: noChecks,
      }),
      hookRepo(),
      hookRepo({
        repo: '',
        name: 'scratch',
        dir: '~/projects/scratch',
        group: 'off',
        state: 'missing',
        action: 'install',
        hooksDir: '~/projects/scratch/.git/hooks',
        lastChecks: noChecks,
      }),
    ],
    notSetUp: [
      { name: 'notes', dir: '~/projects/notes' },
      { name: 'site', dir: '~/projects/site' },
    ],
    pathWarning: { show: false, dir: '' },
    toolNote: { show: false, open: false },
    checkedAt: '2026-06-15T11:59:00Z',
    error: '',
    ...overrides,
  }
}

// hookActionResult is go-pre-commit's hook installed in acme-org/api.
export function hookActionResult(overrides: Partial<HookActionResult> = {}): HookActionResult {
  return {
    action: 'install',
    outcome: 'installed',
    message: 'Installed the pre-commit hook in acme-org/api.',
    repo: 'acme-org/api',
    name: 'acme-org/api',
    dir: '~/projects/api',
    hooksDir: '~/projects/api/.git/hooks',
    shared: false,
    sharedWith: 0,
    otherTool: '',
    backup: '',
    restored: false,
    removed: [],
    notExecutable: false,
    error: '',
    ...overrides,
  }
}

// hookChecksResult is every check run on every file in acme-org/web: four passed and
// whitespace failed, fixing and staging two files, one of them already changed, with
// gitleaks off.
export function hookChecksResult(overrides: Partial<HookChecksResult> = {}): HookChecksResult {
  const check = { durationMs: 0, error: '', suggestion: '', detail: [], changedFiles: [] }

  return {
    scope: 'all',
    check: '',
    name: 'acme-org/web',
    dir: '~/projects/web',
    message: '4 passed, 1 failed: whitespace.',
    checks: [
      { ...check, name: 'eof', state: 'passed', durationMs: 120 },
      { ...check, name: 'fumpt', state: 'passed', durationMs: 900 },
      { ...check, name: 'lint', state: 'passed', durationMs: 61_000 },
      { ...check, name: 'mod-tidy', state: 'passed', durationMs: 2_000 },
      {
        ...check,
        name: 'whitespace',
        state: 'failed',
        durationMs: 300,
        error: 'whitespace issues found',
        suggestion: 'Run go-pre-commit run whitespace and commit the fixes.',
        detail: ['README.md:3: trailing whitespace'],
        changedFiles: ['README.md', 'docs/setup.md'],
      },
      { ...check, name: 'gitleaks', state: 'off' },
    ],
    passed: 4,
    failed: 1,
    skipped: 0,
    files: 41,
    durationMs: 72_000,
    noFiles: false,
    disabled: false,
    changed: [
      { path: 'README.md', staged: true, wasDirty: false, by: ['whitespace'] },
      { path: 'docs/setup.md', staged: true, wasDirty: true, by: ['whitespace'] },
    ],
    changedKnown: true,
    changedError: '',
    installs: [],
    errors: [],
    ...overrides,
  }
}

// hookInstallAll is Install and update all over acme-org/api and acme-org/cli: one
// installed, one updated.
export function hookInstallAll(overrides: Partial<HookInstallAllResult> = {}): HookInstallAllResult {
  return {
    lines: [
      {
        name: 'acme-org/api',
        dir: '~/projects/api',
        action: 'install',
        runId: 11,
        outcome: 'installed',
        message: 'Installed the pre-commit hook in acme-org/api.',
      },
      {
        name: 'acme-org/cli',
        dir: '~/projects/cli',
        action: 'update',
        runId: 12,
        outcome: 'updated',
        message: 'Updated the pre-commit hook in acme-org/cli.',
      },
    ],
    message: '1 installed, 1 updated.',
    ...overrides,
  }
}

// hooksRun is go-pre-commit's hook installed in acme-org/api, in the app.
export function hooksRun(overrides: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({
    id: 11,
    kind: 'hooks',
    title: 'Install the hook',
    source: 'install',
    repo: 'acme-org/api',
    dir: '~/projects/api',
    argv: ['/opt/homebrew/bin/go-pre-commit', 'install', '--no-color'],
    command: 'go-pre-commit install --no-color',
    summary: 'Installed the pre-commit hook in acme-org/api.',
    result: {
      tool: 'go-pre-commit',
      intent: { action: 'install', confirm: '' },
      warnings: [],
      unrecognized: [],
      unrecognizedCount: 0,
      outcome: { action: 'install', outcome: 'installed' },
    },
    ...overrides,
  })
}

// checksRun is every check run on every file in acme-org/web, in the app, with
// whitespace failing.
export function checksRun(overrides: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({
    id: 21,
    kind: 'checks',
    title: 'Checks · all files',
    source: 'all',
    repo: 'acme-org/web',
    dir: '~/projects/web',
    argv: ['/opt/homebrew/bin/go-pre-commit', 'run', '--no-color', '--all-files'],
    command: 'go-pre-commit run --no-color --all-files',
    status: 'failed',
    exitCode: 1,
    summary: '4 passed, 1 failed: whitespace.',
    result: {
      tool: 'go-pre-commit',
      intent: { scope: 'all', check: '' },
      warnings: [],
      unrecognized: [],
      unrecognizedCount: 0,
      outcome: { scope: 'all' },
    },
    ...overrides,
  })
}

// scriptClone is acme-org/sdk's clone, as Repo scripts' picker lists it.
export function scriptClone(overrides: Partial<ScriptClone> = {}): ScriptClone {
  return { name: 'sdk', dir: '~/projects/sdk', ...overrides }
}

// scriptClones are the clones Repo scripts' picker lists, the one a script ran in last
// first.
export function scriptClones(): ScriptClone[] {
  return [
    scriptClone(),
    scriptClone({ name: 'web', dir: '~/projects/web' }),
    scriptClone({ name: 'notes', dir: '~/projects/notes' }),
  ]
}

// branchStatus is acme-org/sdk's clone on master, up to date with origin's master, as
// origin said a minute before FIXED_NOW.
export function branchStatus(overrides: Partial<BranchStatus> = {}): BranchStatus {
  return {
    dir: '~/projects/sdk',
    branch: 'master',
    upstream: 'master',
    state: 'upToDate',
    local: '1a2b3c4',
    origin: '1a2b3c4',
    note: 'master is up to date with origin/master.',
    checkedAt: '2026-06-15T11:59:00Z',
    ...overrides,
  }
}

// scriptEntry is t3.json's Test script, never trusted, so running it asks first.
export function scriptEntry(overrides: Partial<ScriptEntry> = {}): ScriptEntry {
  return {
    ref: 't3:Test',
    source: 't3',
    name: 'Test',
    command: 'magex test',
    icon: 'test',
    detail: '',
    custom: false,
    args: false,
    runnable: true,
    why: '',
    release: false,
    background: false,
    note: '',
    trust: 'ask',
    trusted: '',
    trustNote: '',
    sha256: 'c0ffee01',
    inputs: [],
    mine: null,
    ...overrides,
  }
}

// scriptVariables name the variables each of the user's scripts runs with, as Go's
// scripts.Variables does, from the product's slug.
export function scriptVariables(): string[] {
  return ['REPO', 'REPO_DIR', 'DEFAULT_BRANCH'].map((v) => `${brand.slug.toUpperCase()}_${v}`)
}

// userScript is the user's own Fix all, for every clone, run where the setting sends runs.
export function userScript(overrides: Partial<UserScript> = {}): UserScript {
  return {
    id: 1,
    name: 'Fix all',
    command: 'magex format:fix && magex lint',
    icon: 'play',
    scope: 'all',
    repos: [],
    runsIn: '',
    confirm: false,
    ...overrides,
  }
}

// yourEntry is the user's script u as Repo scripts lists it: it needs no trust.
export function yourEntry(u: UserScript = userScript(), overrides: Partial<ScriptEntry> = {}): ScriptEntry {
  return scriptEntry({
    ref: `yours:${String(u.id)}`,
    source: 'yours',
    name: u.name,
    command: u.command,
    icon: u.icon,
    trust: 'none',
    sha256: '',
    mine: u,
    ...overrides,
  })
}

// magexTarget is one of magex's own targets, which needs no trust and takes arguments.
export function magexTarget(name: string, detail: string, overrides: Partial<ScriptEntry> = {}): ScriptEntry {
  return scriptEntry({
    ref: `magex:${name}`,
    source: 'magex',
    name,
    command: `magex ${name}`,
    icon: '',
    detail,
    args: true,
    trust: 'none',
    sha256: '',
    ...overrides,
  })
}

// bumpInput is a release task's pickString: the part of the version to bump, each choice
// a label beside the value it fills in.
export function bumpInput(overrides: Partial<ScriptInput> = {}): ScriptInput {
  return {
    id: 'bump',
    kind: 'pickString',
    description: 'Which part of the version to bump',
    default: 'patch',
    options: [
      { label: 'Patch', value: 'patch', shown: 'Patch (patch)' },
      { label: 'Minor', value: 'minor', shown: 'Minor (minor)' },
      { label: 'major', value: 'major', shown: 'major' },
    ],
    ...overrides,
  }
}

// scriptSection is one source of Repo scripts, found, with no scripts.
export function scriptSection(overrides: Partial<ScriptSection> = {}): ScriptSection {
  return {
    source: 't3',
    title: 'From t3.json',
    off: false,
    found: true,
    error: '',
    note: '',
    entries: [],
    common: [],
    ...overrides,
  }
}

// repoScripts is Repo scripts for acme-org/sdk, on its default branch: none of the user's
// own yet; t3.json's scripts (one trusted, one never run, one that bumps a version);
// .vscode/tasks.json's tasks (one changed since it was trusted, one greyed, one asking
// for inputs, one that runs until stopped); and magex, with the repository's own target
// and a few built-ins, one greyed and one that tags a version.
export function repoScripts(overrides: Partial<RepoScripts> = {}): RepoScripts {
  return {
    repo: 'acme-org/sdk',
    name: 'acme-org/sdk',
    dir: '~/projects/sdk',
    branch: 'master',
    fromBranch: '',
    switches: { magexTargets: true, repoFiles: true },
    variables: scriptVariables(),
    off: false,
    why: '',
    sections: [
      scriptSection({ source: 'yours', title: 'Yours', found: false }),
      scriptSection({
        entries: [
          scriptEntry({
            ref: 't3:Fix all',
            name: 'Fix all',
            command: 'magex format:fix && magex lint && magex test:race',
            icon: 'play',
            trust: 'trusted',
            sha256: 'c0ffee00',
          }),
          scriptEntry(),
          scriptEntry({
            ref: 't3:Version: bump patch',
            name: 'Version: bump patch',
            command: 'magex version:bump bump=patch push',
            icon: 'configure',
            release: true,
            sha256: 'c0ffee02',
          }),
        ],
      }),
      scriptSection({
        source: 'tasks',
        title: 'From .vscode/tasks.json',
        entries: [
          scriptEntry({
            ref: 'tasks:magex: lint',
            source: 'tasks',
            name: 'magex: lint',
            command: 'magex lint',
            icon: '',
            detail: 'Run comprehensive linting with golangci-lint',
            trust: 'changed',
            trusted: 'magex lint:fix',
            sha256: 'c0ffee10',
          }),
          scriptEntry({
            ref: 'tasks:go: test file',
            source: 'tasks',
            name: 'go: test file',
            command: 'go test ${file}',
            icon: '',
            runnable: false,
            why: `Uses \${file}, which ${brand.name} can’t fill in.`,
            sha256: 'c0ffee11',
          }),
          scriptEntry({
            ref: 'tasks:Release: bump',
            source: 'tasks',
            name: 'Release: bump',
            command: 'magex version:bump bump=${input:bump}',
            icon: '',
            release: true,
            sha256: 'c0ffee12',
            inputs: [bumpInput()],
          }),
          scriptEntry({
            ref: 'tasks:Bench',
            source: 'tasks',
            name: 'Bench',
            command: 'go test -run=^$ -bench=${input:pattern} -count=${input:count} ./...',
            icon: '',
            sha256: 'c0ffee13',
            note: 'Runs its dependencies one after another, where VS Code runs them side by side.',
            inputs: [
              {
                id: 'pattern',
                kind: 'promptString',
                description: 'Which benchmarks to run',
                default: '.',
                options: [],
              },
              {
                id: 'count',
                kind: 'pickString',
                description: '',
                default: '1',
                options: [
                  { label: 'Once', value: '1', shown: 'Once (1)' },
                  { label: 'Five times', value: '5', shown: 'Five times (5)' },
                ],
              },
            ],
          }),
          scriptEntry({
            ref: 'tasks:docs: watch',
            source: 'tasks',
            name: 'docs: watch',
            command: 'magex docs:serve',
            icon: '',
            background: true,
            trust: 'trusted',
            sha256: 'c0ffee14',
          }),
        ],
      }),
      scriptSection({
        source: 'magex',
        title: 'magex',
        note: 'magex targets run the repository’s own code and its .github/env settings, as any build does.',
        common: ['magex:format:fix', 'magex:lint', 'magex:test'],
        entries: [
          scriptEntry({
            ref: 'magex:deploy:docs',
            source: 'magex',
            name: 'deploy:docs',
            command: 'magex deploy:docs',
            icon: '',
            detail: 'Publish the docs site',
            custom: true,
            trustNote: 'This runs acme-org/sdk’s own magefiles code.',
            sha256: 'c0ffee20',
          }),
          magexTarget('format:fix', 'Fix the code’s formatting'),
          magexTarget('lint', 'Run the linters'),
          magexTarget('test', 'Run the tests'),
          magexTarget('version:bump', 'Bump the version and tag it', { release: true }),
          magexTarget('update:install', 'Install the latest magex', {
            runnable: false,
            why: 'This updates magex itself. Update it in Settings → Integrations, which updates it the way it was installed.',
          }),
        ],
      }),
    ],
    ...overrides,
  }
}

// scriptQuestion is what running t3.json's Test asks first: it was never trusted.
export function scriptQuestion(overrides: Partial<ScriptQuestion> = {}): ScriptQuestion {
  return {
    ref: 't3:Test',
    name: 'Test',
    source: 't3',
    command: 'magex test',
    trust: 'ask',
    trusted: '',
    trustNote: '',
    sha256: 'c0ffee01',
    ...overrides,
  }
}

// questionError is the error a run is refused with until the user answers question, as
// the desktop runtime delivers it: the full text for the console, and the cause the
// window reads, with the backend's words and the question as its detail.
export function questionError(
  question: ScriptQuestion = scriptQuestion(),
  message = 'Confirm “Test” before it runs.',
): Error {
  return new Error(`the command needs the user's trust: ${question.ref}`, { cause: { message, detail: question } })
}
