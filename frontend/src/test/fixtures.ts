// Fake data for component tests: fake orgs, users, and repositories only.

import { brand } from '../generated/brand'
import catalogJSON from './autopilotCatalog.json'
import type {
  AssigneeCandidate,
  AssigneeCandidates,
  AutopilotCatalog,
  AutopilotDescribed,
  DescribeAgentChoice,
  DescribeCard,
  DescribeConversation,
  DescribePluginState,
  DescribeStatus,
  DescribeTurn,
  KitChecked,
  KitCheckedPrompt,
  KitCheckedRoutine,
  KitCheckedRules,
  KitCheckedSettings,
  KitNewPrompt,
  KitOutcome,
  KitRoutine,
  DescribeAttachment,
  AutopilotExplanation,
  AutopilotHold,
  AutopilotImportPreview,
  AutopilotItemDraft,
  AutopilotTemplate,
  AutopilotMyScript,
  AutopilotReadiness,
  AutopilotReadyCheck,
  AutopilotRoutineReady,
  AutopilotRunPreview,
  AutopilotStats,
  AutopilotSuggestions,
  AutopilotTestItem,
  AutopilotTestResult,
  Account,
  AfterMerge,
  Agent,
  AgentID,
  AgentSession,
  AgentInfo,
  AgentVersion,
  ToolVersion,
  AppNotification,
  AutopilotActivityPage,
  AutopilotEffect,
  AutopilotEngine,
  AutopilotFigures,
  AutopilotGlance,
  AutopilotGroup,
  AutopilotItemRef,
  AutopilotLeftAlone,
  AutopilotMatch,
  AutopilotUpcoming,
  AutopilotRoutine,
  AutopilotRun,
  AutopilotRunEnding,
  AutopilotRunEvent,
  AutopilotRunEvents,
  AutopilotRunView,
  AutopilotTarget,
  BudgetGauge,
  BudgetReading,
  BudgetWrites,
  BranchStatus,
  BundlePrompt,
  DefaultPrompts,
  Review,
  ReviewCommit,
  Reviewers,
  ReviewFile,
  ReviewOffer,
  ReviewReplyList,
  ReviewResult,
  ReviewUnrun,
  YourReview,
  CloneTarget,
  LinkedClone,
  LocationPreview,
  LocationState,
  FileCheck,
  GitHubSignIn,
  GitHubBulkDone,
  GitHubThread,
  GitHubThreadList,
  GitHubThreadsSnapshot,
  GitHubThreadsStatus,
  HookActionResult,
  HookChecksResult,
  HookInstallAllResult,
  HookRepo,
  HooksList,
  ImportEntry,
  ImportPreview,
  DefaultOffer,
  BackupImported,
  BackupPlan,
  BackupPreview,
  BackupSection,
  BackupSectionPreview,
  InboxItem,
  Issue,
  IssueLinked,
  IssueOffer,
  IssueRepoLabels,
  IssueReply,
  IssueResult,
  TimelineEntry,
  TimelineLineComment,
  TimelinePage,
  InboxList,
  InboxSnapshot,
  Info,
  InstallPlan,
  Restored,
  SetupSignals,
  TerminalChoice,
  InstallResult,
  IntegrationsConfig,
  IntegrationTool,
  IntegrationTools,
  ItemDetails,
  IssueInfo,
  ClosingPull,
  ItemResolution,
  LaunchDefaults,
  LaunchHold,
  LaunchPreview,
  LaunchResult,
  LinkedItem,
  ManifestsClone,
  ManifestsModule,
  ManifestsPage,
  MergedBranch,
  Profile,
  Project,
  Prompt,
  PromptBackup,
  PromptCheckResult,
  PromptVariable,
  QueuedClear,
  Rate,
  ReleaseCandidate,
  ReleaseCandidates,
  ReleaseCheck,
  ReleaseFollow,
  ReleaseOffer,
  ReleaseResult,
  RemoveResult,
  RepoLaunchPreview,
  RepoResolution,
  RepoScripts,
  Resolution,
  SituationChoice,
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
  SettledClear,
  Settings,
  SweepReport,
  ToolCopy,
  UserScript,
  Worktree,
  WorktreeBranch,
  WorktreeCleanup,
  WorktreeCommit,
  WorktreeCopies,
  WorktreeCopyComparison,
  WorktreeDetails,
  WorktreeFile,
  WorktreeRepo,
  WorktreesSnapshot,
  ServiceProvider,
  ServiceStatus,
  StatusConfig,
  StatusPart,
  StatusProvider,
  RunGoing,
  MoveResult,
  NilMoment,
  UpdateLocation,
  UpdateLocationKind,
  UpdateStatus,
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
    sandbox: false,
    ...overrides,
  }
}

// restored is what a start brought back of the user's own: prompts, launch defaults,
// scripts, and no Autopilot routine, from a database set aside.
export function restored(overrides: Partial<Restored> = {}): Restored {
  return {
    prompts: 12,
    launchDefaults: 3,
    scripts: 2,
    routines: 0,
    from: '~/Library/Application Support/Example/example.db.reset-20261115T090000Z',
    ...overrides,
  }
}

// githubSignIn is the GitHub CLI's sign-in as the setup's GitHub step reads it: ready, as
// octo-user, with the scopes the inbox needs and projects.
export function githubSignIn(overrides: Partial<GitHubSignIn> = {}): GitHubSignIn {
  return {
    state: 'ready',
    login: 'octo-user',
    name: 'Octo User',
    scopes: ['repo', 'read:org', 'read:project'],
    missing: [],
    unverified: false,
    projects: 'granted',
    cliVersion: '2.83.1',
    fix: '',
    message: '',
    ...overrides,
  }
}

// terminalChoices are the terminals launches can open in, both installed.
export function terminalChoices(overrides: { iterm?: boolean } = {}): TerminalChoice[] {
  return [
    { id: 'iterm2', name: 'iTerm2', installed: overrides.iterm ?? true },
    { id: 'terminal', name: 'Terminal', installed: true },
  ]
}

// setupSignals counts the clones that hold each sign the Tools step looks for: none.
export function setupSignals(overrides: Partial<SetupSignals> = {}): SetupSignals {
  return { clones: 0, goModules: 0, mageX: 0, goPreCommit: 0, fortress: 0, ...overrides }
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
    { tool: 'git', name: 'Git', installed: true, path: '/usr/bin/git', version: '2.50.1', error: '' },
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
    unread: false,
    trust: 'member',
    trusted: true,
    authorIsBot: false,
    authorAvatarUrl: '',
    isCrossRepository: false,
    authorAssociation: 'OWNER',
    branch: 'widget-cache',
    isDraft: false,
    details: null,
    files: null,
    dismissedAt: '',
    snoozedUntil: '',
    wokeBy: null,
    nudgedAt: '',
    pinnedAt: '',
    setAsideBy: 0,
    pinnedBy: 0,
    repoRule: '',
    authored: false,
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
    heading: '',
    autopilot: autopilotTarget(),
    ...overrides,
  }
}

// autopilotTarget is what an Autopilot notification opens: nothing, unless overrides say.
export function autopilotTarget(overrides: Partial<AutopilotTarget> = {}): AutopilotTarget {
  return { routineId: 0, matchId: 0, runId: 0, createdAt: '', activity: false, ...overrides }
}

// autopilotAlert is a routine's Notify me about pull request 7 of example-repo, item 1,
// headed by the routine's name and opening its match.
export function autopilotAlert(overrides: Partial<AppNotification> = {}): AppNotification {
  return notification({
    kind: 'autopilot_alert',
    heading: 'Tell me about security failures',
    title: 'Bump golang.org/x/net',
    author: '',
    detail: '1 check failed on a1b2c3d; a failed check’s name contains security (Security / gitleaks).',
    autopilot: autopilotTarget({ routineId: 3, matchId: 12, createdAt: '2026-06-15T11:54:00Z' }),
    ...overrides,
  })
}

// autopilotGlance is Autopilot at a glance, on: no routine, nothing waiting, no agent
// working.
export function autopilotGlance(overrides: Partial<AutopilotGlance> = {}): AutopilotGlance {
  return { on: true, routines: 0, enabled: 0, paused: false, needsYou: 0, working: [], ...overrides }
}

// routineSpec is a routine's spec: Checks pass on the pull requests in Review requested,
// snoozed for three days.
export function routineSpec(): AutopilotRoutine['spec'] {
  return {
    version: 1,
    watch: { sources: [{ lists: ['reviewRequested'] }] },
    trigger: { event: 'checks_passed' },
    conditions: { match: 'all', of: [] },
    steps: [{ snooze: { days: 3 } }],
    limits: {},
  }
}

// pastRulesSpec is a routine's spec that merges your own pull requests past the rules: when
// checks pass on the pull requests you opened, merge it when GitHub allows, or past the
// rules when an approval from someone else is all it lacks, sensitive files included.
export function pastRulesSpec(): AutopilotRoutine['spec'] {
  return {
    version: 1,
    watch: { sources: [{ lists: ['authored'] }] },
    trigger: { event: 'checks_passed' },
    conditions: { match: 'all', of: [] },
    steps: [{ merge: { how: 'whenMergeable', allowBypass: true, allowSensitive: true } }],
    limits: {},
  }
}

// autopilotCatalog is the catalog the bound Catalog returns, as a Go test writes it from
// the catalog itself (autopilotCatalog.json), so the builder's tests render what the app
// does: the agent step, among others, listed but not offered yet.
export function autopilotCatalog(): AutopilotCatalog {
  return structuredClone(catalogJSON) as unknown as AutopilotCatalog
}

// routineSuggestions are what the stored items suggest the builder's pickers.
export function routineSuggestions(overrides: Partial<AutopilotSuggestions> = {}): AutopilotSuggestions {
  return {
    authors: ['octo-reviewer'],
    bots: ['dependabot', 'renovate'],
    owners: ['acme-org', 'octo-user'],
    labels: ['dependencies', 'security'],
    checks: ['🔒 Security Scans', 'CodeQL / Analyze', 'test (go 1.26)'],
    ...overrides,
  }
}

// described is a draft that reads, with no problem and no warning.
export function described(overrides: Partial<AutopilotDescribed> = {}): AutopilotDescribed {
  return {
    sentence:
      'When checks pass on pull requests in Review requested, snooze it for 3 days. Once per head commit, at most 2 tries per pull request.',
    problems: [],
    warnings: [],
    dryRunOnly: [],
    ...overrides,
  }
}

// testItem is pull request 7 of example-repo, matching, with the trace of its decision.
export function testItem(overrides: Partial<AutopilotTestItem> = {}): AutopilotTestItem {
  return {
    id: 1,
    kind: 'pr',
    repo: 'acme-org/example-repo',
    number: 7,
    title: 'Bump x/net from 0.30 to 0.33',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    match: 'yes',
    fireKey: 'checks_passed:aaaaaaa',
    needs: [],
    trace: [
      { path: 'trigger', condition: 'checks pass', result: 'yes', actual: 'every check passed, settled' },
      {
        path: 'conditions.of[0]',
        condition: 'it is by Dependabot',
        result: 'yes',
        actual: 'dependabot',
      },
    ],
    leftAlone: false,
    record: '',
    listed: true,
    runsNow: false,
    ...overrides,
  }
}

// myScript is one of the user's own Repo scripts, Lint, which doesn't ask before each run.
export function myScript(overrides: Partial<AutopilotMyScript> = {}): AutopilotMyScript {
  return {
    uuid: fixtureUUID(700 + (overrides.id ?? 7)),
    id: 7,
    name: 'Lint',
    command: 'magex lint',
    asks: false,
    look: 'v1',
    shows: 'every clone',
    ...overrides,
  }
}

// nilMoment is what a time at nil was worth: by default, the first day at nil, unlocking
// nothing and throwing no confetti.
export function nilMoment(overrides: Partial<NilMoment> = {}): NilMoment {
  return { earned: false, cleared: 0, streak: 1, best: 1, count: 0, unlocked: [], confetti: false, ...overrides }
}

// routineStats is a routine's week with nothing run.
export function routineStats(overrides: Partial<AutopilotStats> = {}): AutopilotStats {
  return {
    since: '2026-06-08T12:00:00Z',
    runs: 0,
    scripts: 0,
    fixes: 0,
    merges: 0,
    reviews: 0,
    actions: 0,
    costUsd: null,
    costed: 0,
    estimated: 0,
    tokensIn: 0,
    tokensOut: 0,
    cacheRead: 0,
    counted: 0,
    ...overrides,
  }
}

// routineTest is a draft tried on its items now: one search, one match of two, three
// points spent.
export function routineTest(overrides: Partial<AutopilotTestResult> = {}): AutopilotTestResult {
  return {
    searches: [
      {
        query: 'is:open archived:false sort:updated-desc is:pr author:app/dependabot org:acme-org',
        count: 2,
        returned: 2,
        truncated: false,
        error: '',
        warnings: [],
      },
    ],
    items: [
      testItem(),
      testItem({
        id: 2,
        number: 46,
        title: 'Bump zod from 3 to 4',
        url: 'https://github.com/acme-org/example-repo/pull/46',
        match: 'no',
        fireKey: '',
        trace: [{ path: 'trigger', condition: 'checks pass', result: 'no', actual: 'test (go 1.26) failed' }],
      }),
    ],
    matched: 1,
    unknown: 0,
    watched: 2,
    points: 3,
    cached: false,
    lowBudget: false,
    estimate: { requests: 1, pointsPerRefresh: 1, pointsPerHour: 12, percent: 0.24 },
    warnings: [],
    ...overrides,
  }
}

// readyCheck is the GitHub sign-in, checked at noon and fine.
export function readyCheck(overrides: Partial<AutopilotReadyCheck> = {}): AutopilotReadyCheck {
  return {
    need: { kind: 'github', agent: '', profile: '', tool: '', place: '', fallback: false },
    id: 'github',
    label: 'GitHub',
    level: 'ok',
    found: 'signed in as octo-user',
    fix: '',
    command: false,
    checkedAt: '2026-06-15T12:00:00Z',
    ...overrides,
  }
}

// claudeMissing is Claude Code not found on this Mac, with the command that installs it.
export function claudeMissing(overrides: Partial<AutopilotReadyCheck> = {}): AutopilotReadyCheck {
  return readyCheck({
    need: { kind: 'agent', agent: 'claude', profile: '', tool: '', place: '', fallback: false },
    id: 'agent:claude',
    label: 'Claude',
    level: 'problem',
    found: 'isn’t installed',
    fix: 'npm install -g @anthropic-ai/claude-code',
    command: true,
    ...overrides,
  })
}

// routineReady is routine 3, ready on this Mac.
export function routineReady(overrides: Partial<AutopilotRoutineReady> = {}): AutopilotRoutineReady {
  return { routineId: 3, name: 'Snooze passing bot PRs', ready: true, problems: [], warnings: [], ...overrides }
}

// readiness is this Mac checked at noon: GitHub signed in, and every routine ready.
export function readiness(overrides: Partial<AutopilotReadiness> = {}): AutopilotReadiness {
  return {
    checkedAt: '2026-06-15T12:00:00Z',
    checking: false,
    checks: [readyCheck()],
    routines: [routineReady()],
    notReady: 0,
    ...overrides,
  }
}

// autopilotHold is a routine's start held for pull request 7's agent files at its head.
export function autopilotHold(overrides: Partial<AutopilotHold> = {}): AutopilotHold {
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
    dirty: 0,
    ...overrides,
  }
}

// runPreview is a routine's agent step planned on pull request 7: Claude Code in a new
// worktree, opening in iTerm2.
export function runPreview(overrides: Partial<AutopilotRunPreview> = {}): AutopilotRunPreview {
  return {
    agent: 'claude',
    profile: 'default',
    model: 'opus',
    effort: 'high',
    promptName: 'Fix checks',
    sources: { agent: 'routine', profile: 'app', model: 'routine', effort: 'prompt', prompt: 'routine' },
    dir: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    worktree: {
      name: 'pr-7',
      path: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
      branch: 'worktree-pr-7',
      action: 'create',
      base: 'the pull request’s head',
      dirty: 0,
    },
    command: "claude <prompt> --model opus '--name=Fix checks · example-repo#7'",
    env: ['unset ANTHROPIC_API_KEY'],
    permission: 'full access',
    prompt: 'Fix the failing checks of pull request #7 in acme-org/example-repo.\n',
    terminal: 'iterm2',
    warnings: [],
    ...overrides,
  }
}

// explanation is why routine 3 didn't act on item 1: its checks haven't passed.
export function explanation(overrides: Partial<AutopilotExplanation> = {}): AutopilotExplanation {
  return {
    routineId: 3,
    itemId: 1,
    match: 'no',
    fireKey: '',
    trace: [
      { path: 'trigger', condition: 'checks pass', result: 'no', actual: 'test (go 1.26) failed' },
      { path: 'conditions.of[0]', condition: 'it is a bot’s', result: 'yes', actual: 'dependabot' },
    ],
    needs: [],
    inWatch: true,
    leftAlone: false,
    ledger: ['It hasn’t acted on this item yet.'],
    record: null,
    routine: [],
    watch: [],
    records: [],
    ...overrides,
  }
}

// fixtureUUID is a canonical UUID of its own for the fixture record n, so two prompts, two
// scripts, or two routines a test makes never share one.
export function fixtureUUID(n: number): string {
  return `0b1c2d3e-4f50-4a61-8b72-${n.toString(16).padStart(12, '0')}`
}

// routine is Snooze passing bot PRs, routine 3, on in Autopilot since the morning, of the
// account signed in.
export function routine(overrides: Partial<AutopilotRoutine> = {}): AutopilotRoutine {
  return {
    id: 3,
    uuid: fixtureUUID(300 + (overrides.id ?? 3)),
    name: 'Snooze passing bot PRs',
    description: '',
    enabled: true,
    enabledAt: '2026-06-15T08:00:00Z',
    mode: 'auto',
    spec: routineSpec(),
    sentence:
      'When checks pass on pull requests in Review requested, snooze it for 3 days. Once per head commit, at most 2 tries per pull request.',
    templateKey: 'snooze-passing-bot-prs',
    account: 'octo-user',
    otherAccount: false,
    brokenReason: '',
    broken: '',
    problems: [],
    dryRunOnly: [],
    lastError: '',
    pastRules: '',
    figures: routineFigures(),
    createdAt: '2026-06-14T09:00:00Z',
    updatedAt: '2026-06-15T08:00:00Z',
    ...overrides,
  }
}

// routineFigures are what a routine's ledger says of it: nothing matched yet, unless
// overrides say.
export function routineFigures(overrides: Partial<AutopilotFigures> = {}): AutopilotFigures {
  return { today: 0, wouldRunToday: 0, waiting: 0, lastAt: null, lastDryRun: false, graduate: false, ...overrides }
}

// itemRef is the ledger's copy of pull request 7 of example-repo, item 1.
export function itemRef(overrides: Partial<AutopilotItemRef> = {}): AutopilotItemRef {
  return {
    id: 1,
    kind: 'pr',
    repo: 'acme-org/example-repo',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    ...overrides,
  }
}

// autopilotEffect is routine 3's snooze of item 1 at 11:30, which Undo can still take back.
export function autopilotEffect(overrides: Partial<AutopilotEffect> = {}): AutopilotEffect {
  return {
    id: 51,
    matchId: 12,
    step: 0,
    action: 'snooze',
    appliedAt: '2026-06-15T11:30:00Z',
    undoneAt: null,
    reversedAt: null,
    canUndo: true,
    ...overrides,
  }
}

// autopilotUpcoming is routine 7, Merge syncs, which will run by itself on item 1 when its
// checks pass: it comments one of the user's replies, then merges it.
export function autopilotUpcoming(overrides: Partial<AutopilotUpcoming> = {}): AutopilotUpcoming {
  return {
    itemId: 1,
    routineId: 7,
    routine: 'Merge syncs',
    mode: 'auto',
    asksFirst: false,
    trigger: 'checks_passed',
    when: 'when its checks pass',
    at: null,
    steps: [
      githubStep({ kind: 'github', label: 'GitHub actions', actions: ['reply'] }),
      githubStep({ kind: 'merge', label: 'Merge it', actions: [] }),
    ],
    ...overrides,
  }
}

// autopilotMatch is routine 3's match 12 on item 1 at 11:30: it ran, and snoozed it.
export function autopilotMatch(overrides: Partial<AutopilotMatch> = {}): AutopilotMatch {
  return {
    id: 12,
    routineId: 3,
    routine: 'Snooze passing bot PRs',
    item: itemRef(),
    fireKey: 'checks_passed:commit:a1b2c3d',
    dryRun: false,
    manual: false,
    state: 'done',
    blockedBy: '',
    reason: 'ran',
    words: 'It ran.',
    steps: [
      {
        kind: 'snooze',
        label: 'Snooze',
        status: 'done',
        outcome: 'done',
        note: 'Snoozed it for 3 days.',
        runId: 0,
        actions: [],
        sent: 0,
        verdict: '',
        review: null,
        postAs: '',
        reviewAs: '',
        allowApprove: false,
        merge: '',
        autoMerge: false,
        startedAt: '2026-06-15T11:30:00Z',
        endedAt: '2026-06-15T11:30:00Z',
      },
    ],
    step: 1,
    trace: [],
    approved: false,
    attempt: 0,
    hold: null,
    runs: [],
    effects: [autopilotEffect()],
    createdAt: '2026-06-15T11:30:00Z',
    updatedAt: '2026-06-15T11:30:00Z',
    decidedAt: '2026-06-15T11:30:00Z',
    closedAt: '2026-06-15T11:30:00Z',
    ...overrides,
  }
}

// githubRun is a GitHub action routine 3's match 12 sent item 1 at 11:30: Dependabot asked
// to rebase, at the head it read.
export function githubRun(overrides: Partial<AutopilotMatch['runs'][number]> = {}): AutopilotMatch['runs'][number] {
  return {
    id: 70,
    matchId: 12,
    step: 0,
    runner: 'github',
    agent: '',
    action: 'rebase',
    status: 'ended',
    outcome: 'done',
    verdict: '',
    headBefore: 'a1b2c3d',
    commit: '',
    headAfter: '',
    budget: 0,
    unit: '',
    cost: null,
    costEstimated: false,
    tokensIn: 0,
    tokensOut: 0,
    cacheRead: 0,
    turns: 0,
    summary: 'Asked Dependabot to rebase acme-org/example-repo#7',
    error: '',
    manual: false,
    scriptRunId: 0,
    sessionId: '',
    preset: '',
    profile: '',
    command: '',
    logCleared: false,
    model: '',
    effort: '',
    worktree: '',
    queuedAt: '2026-06-15T11:30:00Z',
    startedAt: '2026-06-15T11:30:00Z',
    finishedAt: '2026-06-15T11:30:00Z',
    ...overrides,
  }
}

// agentRun is the background run 81 routine 3's match 12 started on item 1 at 11:30, an
// Edit and test agent working in pr-7 on Claude's default profile.
export function agentRun(overrides: Partial<AutopilotRun> = {}): AutopilotRun {
  return githubRun({
    id: 81,
    runner: 'background',
    agent: 'claude',
    action: '',
    status: 'running',
    outcome: '',
    headBefore: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
    budget: 3,
    unit: 'usd',
    summary: '',
    scriptRunId: 44,
    sessionId: '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f71',
    preset: 'editTest',
    profile: 'default',
    model: 'opus',
    effort: 'high',
    worktree: 'pr-7',
    finishedAt: null,
    ...overrides,
  })
}

// agentMatch is routine 3's match 12 on item 1, its agent step running as run 81, then a
// wait for the checks.
export function agentMatch(overrides: Partial<AutopilotMatch> = {}): AutopilotMatch {
  return autopilotMatch({
    routine: 'Fix failing checks',
    state: 'running',
    reason: '',
    words: 'Running.',
    closedAt: null,
    step: 0,
    effects: [],
    steps: [
      { ...githubStep({ kind: 'agent', label: 'Agent', status: 'running', runId: 81, actions: [] }) },
      { ...githubStep({ kind: 'waitForChecks', label: 'Wait for the checks', actions: [] }) },
    ],
    runs: [agentRun()],
    ...overrides,
  })
}

// agentRunView is run 81 as the run view shows it: working, and one Take over can resume.
export function agentRunView(overrides: Partial<AutopilotRunView> = {}): AutopilotRunView {
  return { run: agentRun(), match: agentMatch(), live: true, canTakeOver: true, ...overrides }
}

// runEvent is one event of a run's timeline: what the agent said, at 06:15:02.
export function runEvent(overrides: Partial<AutopilotRunEvent> = {}): AutopilotRunEvent {
  return {
    seq: 1,
    offset: 0,
    part: 0,
    at: '2026-06-15T06:15:02Z',
    kind: 'text',
    tool: '',
    category: 'other',
    text: 'Reading the failing checks.',
    cut: false,
    error: false,
    ...overrides,
  }
}

// runEventsPage is a page of a run's timeline: none, a run still going, with nothing
// before it.
export function runEventsPage(overrides: Partial<AutopilotRunEvents> = {}): AutopilotRunEvents {
  return {
    events: [],
    first: 0,
    earlier: false,
    before: 0,
    done: false,
    progress: { step: '', todos: [], tokensIn: 0, tokensOut: 0, cacheRead: 0, events: 0, unknown: 0 },
    ending: null,
    ...overrides,
  }
}

// runEnding is how a run came out: finished, with its report.
export function runEnding(overrides: Partial<AutopilotRunEnding> = {}): AutopilotRunEnding {
  return {
    summary: 'Finished: fixed.',
    message: 'Moved esbuild to 0.25.1; the checks pass here.',
    reported: true,
    outcome: 'fixed',
    report: 'Moved esbuild to 0.25.1, which fixes GHSA-xxxx.',
    pushed: true,
    pushedCommit: '4f2a9c1000000000000000000000000000000000',
    needsPerson: false,
    reason: '',
    noReport: '',
    turns: 12,
    durationMs: 372000,
    models: ['claude-opus-5-5'],
    commit: '4f2a9c1000000000000000000000000000000000',
    changed: ['package.json', 'pnpm-lock.yaml'],
    todos: [],
    refused: 0,
    ...overrides,
  }
}

// githubStep is a GitHub step of a match, as Activity shows it: its actions, none sent yet.
export function githubStep(overrides: Partial<AutopilotMatch['steps'][number]> = {}): AutopilotMatch['steps'][number] {
  return {
    kind: 'github',
    label: 'GitHub actions',
    status: '',
    outcome: '',
    note: '',
    runId: 0,
    startedAt: null,
    endedAt: null,
    actions: ['rebase'],
    sent: 0,
    verdict: '',
    review: null,
    postAs: '',
    reviewAs: '',
    allowApprove: false,
    merge: '',
    autoMerge: false,
    ...overrides,
  }
}

// githubMatch is routine 6's match 30 on item 1, at its GitHub step, waiting as wait says.
export function githubMatch(overrides: Partial<AutopilotMatch> = {}): AutopilotMatch {
  return autopilotMatch({
    id: 30,
    routineId: 6,
    routine: 'Ask Dependabot to rebase what conflicts',
    fireKey: 'merge_conflicts:commit:a1b2c3d',
    state: 'waiting',
    blockedBy: 'ask',
    reason: '',
    words: 'Waiting for your OK.',
    steps: [githubStep()],
    step: 0,
    effects: [],
    closedAt: null,
    ...overrides,
  })
}

// dryRunMatch is routine 4's Dry run record on item 2 at 11:40: it would have run.
export function dryRunMatch(overrides: Partial<AutopilotMatch> = {}): AutopilotMatch {
  return autopilotMatch({
    id: 14,
    routineId: 4,
    routine: 'Investigate new bugs',
    item: itemRef({
      id: 2,
      kind: 'issue',
      number: 9,
      title: 'The widget forgets its size',
      url: 'https://github.com/acme-org/example-repo/issues/9',
    }),
    fireKey: 'appears:item',
    dryRun: true,
    reason: 'would_run',
    words: 'Dry run: it would have run.',
    steps: [],
    effects: [],
    createdAt: '2026-06-15T11:40:00Z',
    updatedAt: '2026-06-15T11:40:00Z',
    decidedAt: '2026-06-15T11:40:00Z',
    closedAt: '2026-06-15T11:40:00Z',
    ...overrides,
  })
}

// activityGroup is today's line for routine 3's snoozes: match 12's, which Undo can take
// back.
export function activityGroup(overrides: Partial<AutopilotGroup> = {}): AutopilotGroup {
  return {
    key: '2026-06-15|3|snooze',
    routineId: 3,
    routine: 'Snooze passing bot PRs',
    action: 'snooze',
    words: 'Snoozed 1',
    count: 1,
    undone: 0,
    reversed: 0,
    undo: [51],
    matches: [12],
    items: [itemRef()],
    latest: '2026-06-15T11:30:00Z',
    ...overrides,
  }
}

// activityPage is Activity's first and only page: today, a snooze and a Dry run's record,
// and nothing left alone.
export function activityPage(overrides: Partial<AutopilotActivityPage> = {}): AutopilotActivityPage {
  return {
    days: [{ date: '2026-06-15', matches: [dryRunMatch(), autopilotMatch()], groups: [activityGroup()] }],
    more: false,
    next: 0,
    leftAlone: [],
    ...overrides,
  }
}

// leftAlone is item 1, which routine 3 leaves alone since the user brought back what it
// snoozed.
export function leftAlone(overrides: Partial<AutopilotLeftAlone> = {}): AutopilotLeftAlone {
  return {
    routineId: 3,
    routine: 'Snooze passing bot PRs',
    item: itemRef(),
    why: 'You brought back what it snoozed.',
    since: '2026-06-15T11:45:00Z',
    ...overrides,
  }
}

// autopilotEngine is the engine as this copy runs it: its last cycle at 11:59, holding the
// lock, with nothing paused and no problem.
export function autopilotEngine(overrides: Partial<AutopilotEngine> = {}): AutopilotEngine {
  return {
    running: true,
    holds: true,
    elsewhere: false,
    primed: true,
    pausedSince: null,
    lastCycle: {
      at: '2026-06-15T11:59:00Z',
      tookMs: 34,
      waiting: '',
      routines: 2,
      items: 14,
      matches: 7,
      starts: 1,
      closed: 0,
      reads: 0,
      catchUp: false,
    },
    searchesPaused: '',
    signedOut: false,
    problems: [],
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

// issueInfo is how an issue is planned: in no way, unless overridden.
export function issueInfo(overrides: Partial<IssueInfo> = {}): IssueInfo {
  return {
    type: '',
    typeColor: '',
    fields: [],
    milestone: null,
    parent: null,
    blockedBy: 0,
    blocking: 0,
    ...overrides,
  }
}

// closingPull is an open pull request from someone trusted that closes an issue once it
// merges: #8 in the item fixture's repository, unless overridden.
export function closingPull(overrides: Partial<ClosingPull> = {}): ClosingPull {
  return {
    repo: 'acme-org/example-repo',
    number: 8,
    title: 'Fix the flaky retry test',
    url: 'https://github.com/acme-org/example-repo/pull/8',
    merged: false,
    isDraft: false,
    trusted: true,
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
    issue: issueInfo(),
    closingPulls: [],
    comments: 4,
    additions: 120,
    deletions: 30,
    changedFiles: 6,
    commits: 3,
    mergeable: 'MERGEABLE',
    checks: { state: 'SUCCESS', passed: 12, failed: 0, pending: 0, skipped: 3, awaiting: 0, awaitingOnGitHub: 0 },
    reviews: { decision: 'APPROVED', reviewers: 2, reviewed: 1, approvals: 1, changesRequested: 0 },
    autoMerge: '',
    mergeBot: '',
    fetchedAt: '2026-06-15T11:59:00Z',
    pushedSinceReview: false,
    reviewRequestedAgain: false,
    viewerVerdict: '',
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
    versionArgs: ['--version'],
    signInArgs: ['auth', 'status', '--json'],
    keyEnv: ['ANTHROPIC_API_KEY'],
    loginArgs: ['auth', 'login'],
    statusPart: 'claude.code',
    background: {
      background: true,
      budget: true,
      cost: true,
      usage: true,
      usageLive: true,
      structured: true,
      resume: true,
      readsTrust: true,
      minVersion: '2.1.319',
      presets: (['readOnly', 'githubTasks', 'editTest', 'editTestPush', 'profile'] as const).map((preset) => ({
        preset,
        kept: true,
        why: '',
      })),
    },
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
    versionArgs: ['--version'],
    signInArgs: ['login', 'status'],
    keyEnv: ['OPENAI_API_KEY', 'CODEX_API_KEY'],
    loginArgs: ['login'],
    statusPart: 'openai.codex-cli',
    background: {
      background: true,
      budget: false,
      cost: false,
      usage: true,
      usageLive: false,
      structured: true,
      resume: true,
      readsTrust: false,
      minVersion: '0.163.0',
      presets: (['readOnly', 'githubTasks', 'editTest', 'editTestPush', 'profile'] as const).map((preset) => ({
        preset,
        kept: preset !== 'githubTasks',
        why: preset === 'githubTasks' ? codexGitHubTasksWhy : '',
      })),
    },
    ...overrides,
  }
}

// codexGitHubTasksWhy is why Codex keeps no GitHub tasks only, as its spec says it.
export const codexGitHubTasksWhy =
  "Codex can't keep GitHub tasks only: its sandbox cuts gh off from GitHub and its sign-in, and outside a " +
  "sandbox nothing but the fence's hook would hold the run in."

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

// autopilotOff is the settings with Autopilot (beta) off, as a fresh install has them.
export function autopilotOff(overrides: Partial<Settings> = {}): Settings {
  const base = settings(overrides)

  return { ...base, autopilot: { ...base.autopilot, enabled: false } }
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
    claudeApiKey: false,
    codexApiKey: false,
    cloneRoot: '~/projects',
    cloneLocations: [],
    trustedBots: ['dependabot', 'github-actions', 'renovate'],
    defaultTrustedBots: ['dependabot', 'github-actions', 'renovate'],
    snoozeDays: 3,
    snoozeWake: { commits: 1, reviews: 'decisions', comments: 0, ready: true },
    windowSize: { width: 1440, height: 900 },
    defaultWindowSize: { width: 1440, height: 900 },
    sensitiveFiles: { scope: 'collaborators', off: [], patterns: [] },
    branches: { own: true, pullRequest: true, merged: true, protected: ['main', 'master'] },
    defaultProtectedBranches: ['main', 'master'],
    repoRules: { pinned: [], muted: [] },
    sensitiveCategories: sensitiveCategories(),
    menuBar: true,
    bannersOff: ['session_needs_you', 'session_finished'],
    theme: 'system',
    textSize: 'default',
    celebrations: 'milestones',
    toastSeconds: 12,
    collapsedSections: [],
    projectsTipDismissed: false,
    reviewTab: 'files',
    notificationsTab: 'app',
    conversationOrder: 'oldest',
    inboxOrder: { sorts: {}, statusOrder: [], kinds: {} },
    hiddenLabels: ['size/*'],
    defaultHiddenLabels: ['size/*'],
    cardAvatars: true,
    cardDensity: 'comfortable',
    cardUnread: true,
    ignoredWorkflows: [],
    mergeBotsOn: false,
    mergeBots: [],
    mergeMethod: 'repo',
    bypassOwn: false,
    nudgeAfterHours: 24,
    nudgeSnooze: false,
    integrations: integrationsConfig(),
    githubNotifications: {
      read: true,
      auto: false,
      action: 'done',
      triggersOff: [],
      watching: true,
      botActivity: true,
    },
    refresh: { inbox: 300, checks: 45, releaseChecks: 45, worktrees: 300, notifications: 300, serviceStatus: 1800 },
    refreshChoices: {
      inbox: [120, 300, 600, 900, 1800, 3600],
      checks: [0, 30, 45, 60, 120],
      releaseChecks: [0, 30, 45, 60, 120],
      worktrees: [300, 600, 900, 1800, 3600],
      notifications: [60, 120, 300, 600, 900, 1800, 3600],
      serviceStatus: [0, 300, 600, 900, 1800, 3600],
    },
    watchPushes: true,
    serviceStatus: statusWatch(),
    statusProviders: statusProviders(),
    updateMode: 'download',
    // Autopilot (beta) is on, so the tests see every part of the window it adds; a fresh
    // database has it off, which the tests of that state set (autopilotOff).
    autopilot: {
      enabled: true,
      paused: false,
      defaultMode: 'ask',
      agentsAtOnce: 2,
      runsPerDay: 20,
      actionsPerDay: 500,
      actionsPerHour: 120,
      dailyCap: { amount: 0, unit: 'usd' },
      keepAwake: false,
      holdOnOutage: true,
      catchUpHours: 24,
      historyDays: 90,
      logDays: 30,
      mergePastRules: false,
    },
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
    sections: {},
    showRuns: false,
    releaseSort: 'newest',
    depsSort: 'oldest',
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
    outlivesQuit: false,
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
function sensitiveCategories(): SensitiveCategory[] {
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
    dirty: 0,
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
        background: false,
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
    uuid: fixtureUUID(overrides.id ?? 1),
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
    usage: {
      defaults: ['pr'],
      slots: [],
      orgs: [],
      repos: [],
      slotPlaces: [],
      kinds: ['pr'],
      replies: [],
      routines: [],
    },
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

// importPreview is what a prompt file holds: one prompt importing would add, and no
// default to offer.
export function importPreview(overrides: Partial<ImportPreview> = {}): ImportPreview {
  return { entries: [importEntry()], defaults: [], ...overrides }
}

// defaultOffer offers to make the file's first prompt the default approval reply.
export function defaultOffer(overrides: Partial<DefaultOffer> = {}): DefaultOffer {
  return { slot: 'approve', nickname: 'Security', current: 'LGTM', entry: 0, ...overrides }
}

// backupPlan is what importing a category would do: nothing, until a test says.
export function backupPlan(overrides: Partial<BackupPlan> = {}): BackupPlan {
  return { count: { add: 0, update: 0, same: 0, remove: 0 }, items: [], notes: [], ...overrides }
}

// backupSectionPreview is a category a backup holds, with nothing to change under either
// mode.
export function backupSectionPreview(
  section: BackupSection,
  overrides: Partial<BackupSectionPreview> = {},
): BackupSectionPreview {
  return { section, present: true, merge: backupPlan(), replace: backupPlan(), error: '', ...overrides }
}

// backupPreview is a backup holding every category: the settings change the theme, a
// prompt is added under Merge and another deleted under Replace, and the rest change
// nothing.
export function backupPreview(overrides: Partial<BackupPreview> = {}): BackupPreview {
  const theme = { label: 'Theme', change: 'update' as const, detail: 'dark (now system)' }
  const added = { label: 'Security', change: 'add' as const, detail: '' }
  const removed = { label: 'Old review', change: 'remove' as const, detail: '' }

  return {
    path: '/backups/setup.json',
    hash: 'c0ffee',
    exportedAt: '2026-11-13T17:00:00Z',
    app: 'v1.2.3',
    sections: [
      backupSectionPreview('settings', {
        merge: backupPlan({ count: { add: 0, update: 1, same: 20, remove: 0 }, items: [theme] }),
        replace: backupPlan({ count: { add: 0, update: 1, same: 20, remove: 0 }, items: [theme] }),
      }),
      backupSectionPreview('repositories'),
      backupSectionPreview('prompts', {
        merge: backupPlan({ count: { add: 1, update: 0, same: 8, remove: 0 }, items: [added] }),
        replace: backupPlan({ count: { add: 1, update: 0, same: 8, remove: 1 }, items: [added, removed] }),
      }),
      backupSectionPreview('launchDefaults'),
      backupSectionPreview('scripts'),
      backupSectionPreview('autopilot'),
    ],
    ...overrides,
  }
}

// backupImported is what importing sections did: each changed one item, and the setup as
// it was is saved in the app's backups folder.
export function backupImported(sections: readonly BackupSection[]): BackupImported {
  return {
    savedTo: `~/Library/Application Support/${brand.name}/backups/before import.json`,
    sections: sections.map((section) => ({ section, count: { add: 1, update: 0, same: 0, remove: 0 }, notes: [] })),
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
    checksFailing: 0,
    conflicts: 0,
    changesRequested: 0,
    rereview: 0,
    replies: { approve: 0, approveBots: 0, requestChanges: 0, comment: 0, nudge: 0 },
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
      usage: { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] },
    }),
    prompt({
      id: 2,
      name: 'Take a quick look for blockers',
      nickname: 'Quick look',
      models: { claude: 'sonnet' },
      effort: 'low',
      worktree: false,
      starterKey: 'quick-look',
      usage: {
        defaults: [],
        slots: [],
        orgs: ['acme-org'],
        repos: ['acme-org/api'],
        slotPlaces: [],
        kinds: ['pr'],
        replies: [],
        routines: [],
      },
    }),
    prompt(),
    prompt({
      id: 4,
      name: 'Work on an issue',
      nickname: 'Work on it',
      appliesTo: 'issue',
      body: 'Work on issue #{{.Number}}\n',
      starterKey: 'work-on-issue',
      usage: {
        defaults: ['issue'],
        slots: [],
        orgs: [],
        repos: [],
        slotPlaces: [],
        kinds: ['issue'],
        replies: [],
        routines: [],
      },
    }),
    prompt({
      id: 5,
      name: 'Explain the change',
      nickname: 'Explain',
      appliesTo: 'any',
      body: 'Explain {{.URL}}\n',
      starterKey: '',
      usage: { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] },
    }),
  ]
}

// variables are a few of the variables a prompt can name, each marked prOnly when only a
// pull request fills it in, and itemOnly when a repository's launch, which has no item,
// leaves it empty.
export function variables(): PromptVariable[] {
  const item = { group: 'Item', prOnly: false, itemOnly: true }

  return [
    { ...item, name: 'Kind', type: 'text', description: '"pull request" or "issue"' },
    { ...item, name: 'Number', type: 'number', description: 'The pull request or issue number' },
    { ...item, name: 'Title', type: 'text', description: 'The title, cut to 200 characters' },
    { ...item, name: 'URL', type: 'text', description: 'The web address on GitHub' },
    {
      name: 'Repo',
      group: 'Repository',
      type: 'text',
      description: 'The repository, as owner/name',
      prOnly: false,
      itemOnly: false,
    },
    {
      name: 'Branch',
      group: 'Pull request',
      type: 'text',
      description: 'The head branch',
      prOnly: true,
      itemOnly: true,
    },
    {
      name: 'InWorktree',
      group: 'Session',
      type: 'yes/no',
      description: 'The session starts in its own worktree',
      prOnly: false,
      itemOnly: false,
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
    situations: [],
    ...overrides,
  }
}

// situationChoices are a pull request's prompts for the situations it can be in, as its
// resolution names them: each the pull request's own (Review, the app's default), as with
// no situation prompt set, unless prompts names another for it.
export function situationChoices(
  prompts: Partial<Record<SituationChoice['situation'], Partial<Resolution['prompt']>>> = {},
): SituationChoice[] {
  return (['checks_failing', 'conflicts', 'changes_requested', 'rereview'] as const).map((situation) => ({
    situation,
    prompt: { ...resolution().prompt, ...prompts[situation] },
  }))
}

// repoResolution is a repository's launch settings as the Deps page's Upgrade resolves
// them for acme-org/api-server: no item, trusted, the Upgrade dependencies starter picked,
// and its own worktree always, with nothing to say of the repository first.
export function repoResolution(overrides: Partial<RepoResolution> = {}): RepoResolution {
  return {
    ...resolution(),
    itemId: 0,
    kind: '',
    repo: 'acme-org/api-server',
    prompt: {
      id: 21,
      nickname: 'Upgrade deps',
      name: 'Upgrade dependencies',
      source: 'launch',
      inheritedId: 0,
      inheritedSource: '',
    },
    situations: [],
    notice: '',
    ...overrides,
  }
}

// repoLaunchPreview is the preview of the Deps page's Upgrade for acme-org/api-server: a
// new worktree, deps-20260615, made from origin/master.
export function repoLaunchPreview(overrides: Partial<RepoLaunchPreview> = {}): RepoLaunchPreview {
  const dir = `~/projects/.${brand.slug}/worktrees/api-server/deps-20260615`

  return {
    ...launchPreview(),
    resolution: repoResolution(),
    dir,
    worktree: {
      name: 'deps-20260615',
      path: dir,
      branch: 'worktree-deps-20260615',
      action: 'create',
      base: 'origin/master',
      dirty: -1,
    },
    command: "claude <prompt> --model opus '--name=Upgrade deps · api-server'",
    prompt: 'Upgrade the Go and JavaScript dependencies of acme-org/api-server …\n',
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
    checksPromptId: null,
    conflictsPromptId: null,
    changesPromptId: null,
    rereviewPromptId: null,
    models: {},
    effort: '',
    planMode: null,
    worktree: null,
    extraArgs: {},
    extraFlags: {},
    autopilotOutside: '',
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
  return {
    repo: 'acme-org/example-repo',
    cloned: true,
    dir: '~/projects/example-repo',
    taken: false,
    elsewhere: '',
    where: '',
    ...overrides,
  }
}

// locationPreview is what adding ~/work, a folder with three clones directly inside it,
// would add.
export function locationPreview(overrides: Partial<LocationPreview> = {}): LocationPreview {
  return {
    path: '~/work',
    kind: 'folder',
    clones: 3,
    repo: '',
    covered: '',
    coveredBy: '',
    inside: [],
    moreInside: false,
    partial: false,
    parent: { path: '', clones: 0 },
    replaces: [],
    ...overrides,
  }
}

// locationState is the clone root, ~/projects, read now: a folder of six clones.
export function locationState(overrides: Partial<LocationState> = {}): LocationState {
  return {
    path: '~/projects',
    kind: 'folder',
    primary: true,
    state: 'ok',
    clones: 6,
    repo: '',
    coveredBy: '',
    ...overrides,
  }
}

// linkedClone is example-repo's clone found on the desktop, alone in its folder.
export function linkedClone(overrides: Partial<LinkedClone> = {}): LinkedClone {
  return {
    dir: '~/Desktop/example-repo',
    location: locationPreview({
      path: '~/Desktop/example-repo',
      kind: 'clone',
      clones: 0,
      repo: 'acme-org/example-repo',
    }),
    ...overrides,
  }
}

const unknownRate: Rate = { known: false, limit: 0, remaining: 0, used: 0, cost: 0, resetAt: '', takenAt: '' }

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

// noPurposes is a budget's spend split by what each request was for, with nothing spent.
function noPurposes(): BudgetReading['purposes'] {
  return { inbox: 0, watches: 0, follower: 0, autopilot: 0, other: 0 }
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
  purposes: noPurposes(),
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
    purposes: noPurposes(),
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
    // The chore lane: no notifications cleared lately, paced as the app paces them.
    chores: writes({ perHour: 300, perMinute: 50 }),
    // The bulk lane: none marked done in bulk lately.
    bulk: writes({ perHour: 1200, perMinute: 40 }),
    ...overrides,
  }
}

// writes is the app's edits on GitHub: none lately, paced as the app paces them, with a
// 5,000-point REST budget's reserve.
export function writes(overrides: Partial<BudgetWrites> = {}): BudgetWrites {
  return { lastHour: 0, lastMinute: 0, perHour: 200, perMinute: 30, reserve: 50, heldUntil: '', ...overrides }
}

// statusProviders is the catalog of status pages the service status check reads, as Go
// sends it in the settings: GitHub's, Claude's, and OpenAI's parts, with the defaults
// ticked.
function statusProviders(): StatusProvider[] {
  const part = (id: string, name: string, def = false, group = ''): StatusPart => ({ id, name, group, default: def })

  return [
    {
      id: 'github',
      name: 'GitHub',
      pageUrl: 'https://www.githubstatus.com/',
      parts: [
        part('github.git', 'Git Operations', true),
        part('github.api', 'API Requests', true),
        part('github.issues', 'Issues', true),
        part('github.pulls', 'Pull Requests', true),
        part('github.actions', 'Actions', true),
        part('github.webhooks', 'Webhooks'),
        part('github.copilot', 'Copilot'),
      ],
    },
    {
      id: 'claude',
      name: 'Claude',
      pageUrl: 'https://status.claude.com/',
      parts: [
        part('claude.api', 'Claude API', true),
        part('claude.code', 'Claude Code', true),
        part('claude.ai', 'claude.ai'),
        part('claude.gov', 'Claude for Government'),
      ],
    },
    {
      id: 'openai',
      name: 'OpenAI',
      pageUrl: 'https://status.openai.com/',
      parts: [
        part('openai.codex-api', 'Codex API', true, 'Codex'),
        part('openai.codex-cli', 'Codex CLI', true, 'Codex'),
        part('openai.codex-web', 'Codex Web', false, 'Codex'),
        part('openai.responses', 'Responses', true, 'APIs'),
        part('openai.login', 'Login', false, 'ChatGPT'),
      ],
    },
  ]
}

// statusWatch is what a fresh database watches: every provider on, each with its default
// parts ticked.
export function statusWatch(): StatusConfig {
  return {
    watch: statusProviders().map((p) => ({
      provider: p.id,
      on: true,
      parts: p.parts.filter((part) => part.default).map((part) => part.id),
    })),
  }
}

// serviceProvider is a provider watched, read a minute before the tests' noon, with
// everything it watches operational.
export function serviceProvider(overrides: Partial<ServiceProvider> = {}): ServiceProvider {
  return {
    id: 'github',
    name: 'GitHub',
    url: 'https://www.githubstatus.com/',
    state: 'operational',
    affected: [],
    incidents: [],
    others: 0,
    unlisted: [],
    checkedAt: '2026-06-15T11:59:00Z',
    error: '',
    ...overrides,
  }
}

// serviceStatus is every provider watched, read a minute before the tests' noon, with
// everything they watch operational; providers replaces the ones given by id.
export function serviceStatus(
  overrides: Partial<Omit<ServiceStatus, 'providers'>> = {},
  ...providers: ServiceProvider[]
): ServiceStatus {
  const base = [
    serviceProvider(),
    serviceProvider({ id: 'claude', name: 'Claude', url: 'https://status.claude.com/' }),
    serviceProvider({ id: 'openai', name: 'OpenAI', url: 'https://status.openai.com/' }),
  ]

  return {
    state: 'operational',
    checkedAt: '2026-06-15T11:59:00Z',
    providers: base.map((p) => providers.find((q) => q.id === p.id) ?? p),
    ...overrides,
  }
}

// claudeOutage is Claude in a partial outage of the Claude API.
export function claudeOutage(overrides: Partial<ServiceProvider> = {}): ServiceProvider {
  return serviceProvider({
    id: 'claude',
    name: 'Claude',
    url: 'https://status.claude.com/',
    state: 'major',
    affected: [{ id: 'claude.api', name: 'Claude API', state: 'major' }],
    incidents: [
      {
        name: 'Elevated errors on Claude API',
        stage: 'identified',
        state: 'major',
        url: 'https://status.claude.com/incidents/x1',
        parts: ['Claude API'],
        unscoped: false,
        maintenance: false,
      },
    ],
    others: 1,
    ...overrides,
  })
}

// serviceIncident is the service status while Claude has a partial outage of the Claude
// API and the rest is operational.
export function serviceIncident(provider: ServiceProvider = claudeOutage()): ServiceStatus {
  return serviceStatus({ state: provider.state }, provider)
}

// threadsStatus is reading the user's GitHub notifications going well: the last pass a
// minute before the tests' noon, with an empty GitHub inbox.
export function threadsStatus(overrides: Partial<GitHubThreadsStatus> = {}): GitHubThreadsStatus {
  return {
    reading: true,
    account: 'octo-user',
    access: 'ok',
    lastPassAt: '2026-06-15T11:59:00Z',
    lastFullWalkAt: '2026-06-15T11:00:00Z',
    lastError: '',
    askAgainAt: '',
    count: 0,
    unread: 0,
    truncated: false,
    ...overrides,
  }
}

// githubThreads is what the app mirrors of the user's GitHub notifications: by default
// read a minute ago, with none in the GitHub inbox, nothing queued to clear, none held back
// for review, none cleared lately, and no bulk mark. Every test builds one here, with the
// fields it cares about, so a field the backend adds needs a default in this one place.
export function githubThreads(
  overrides: Partial<Omit<GitHubThreadsSnapshot, 'status'>> & { status?: Partial<GitHubThreadsStatus> } = {},
): GitHubThreadsSnapshot {
  const { status, ...rest } = overrides

  return {
    unread: [],
    queued: [],
    backlog: 0,
    recent: [],
    bulkDone: githubBulkDone(),
    ...rest,
    status: threadsStatus(status),
  }
}

// githubBulkDone is the bulk mark of the user's GitHub notifications (Mark all done): by
// default none to show. bulkRunning is one started at noon less a minute, of every one in
// example-repo, 412 of about 1,180 marked so far.
export function githubBulkDone(overrides: Partial<GitHubBulkDone> = {}): GitHubBulkDone {
  return {
    running: false,
    age: 'any',
    before: '',
    all: false,
    repos: [],
    expected: 0,
    done: 0,
    failed: 0,
    left: 0,
    more: false,
    nextAt: '',
    pause: 'none',
    error: '',
    startedAt: '',
    endedAt: '',
    stopped: false,
    perMinute: 40,
    perHour: 1200,
    ...overrides,
  }
}

// bulkRunning is a bulk mark going well, as githubBulkDone describes it.
export function bulkRunning(overrides: Partial<GitHubBulkDone> = {}): GitHubBulkDone {
  return githubBulkDone({
    running: true,
    before: '2026-06-15T11:59:00Z',
    repos: ['acme-org/example-repo'],
    expected: 1180,
    done: 412,
    left: 768,
    startedAt: '2026-06-15T11:59:00Z',
    ...overrides,
  })
}

// queuedClear is the notification about pull request #7 of example-repo, queued to be
// marked done after the user dismissed it, held for the Undo until 15 seconds past noon.
export function queuedClear(overrides: Partial<QueuedClear> = {}): QueuedClear {
  return {
    id: 1,
    threadId: 1001,
    owner: 'acme-org',
    name: 'example-repo',
    kind: 'pr',
    number: 7,
    title: 'Add the widget cache',
    sendAfter: '2026-06-15T12:00:15Z',
    trigger: 'dismissed',
    action: 'done',
    ...overrides,
  }
}

// settledClear is that notification marked done on GitHub, at noon, after the user merged
// the pull request.
export function settledClear(overrides: Partial<SettledClear> = {}): SettledClear {
  return {
    id: 1,
    threadId: 1001,
    owner: 'acme-org',
    name: 'example-repo',
    kind: 'pr',
    number: 7,
    title: 'Add the widget cache',
    trigger: 'merged',
    action: 'done',
    state: 'sent',
    at: '2026-06-15T12:00:00Z',
    error: '',
    reason: '',
    ...overrides,
  }
}

// githubThread is an unread notification asking for the user's review of pull request #7
// of example-repo, updated five minutes before the tests' noon.
export function githubThread(overrides: Partial<GitHubThread> = {}): GitHubThread {
  return {
    id: 1001,
    owner: 'acme-org',
    name: 'example-repo',
    kind: 'pr',
    number: 7,
    subjectType: 'PullRequest',
    title: 'Add the widget cache',
    reason: 'review_requested',
    updatedAt: '2026-06-15T11:55:00Z',
    unread: true,
    state: 'OPEN',
    ...overrides,
  }
}

// githubThreadList is every notification the app mirrors, for the Notifications page: by
// default none, read a minute ago. The status takes the same overrides as githubThreads'.
export function githubThreadList(
  overrides: { threads?: GitHubThread[]; status?: Partial<GitHubThreadsStatus> } = {},
): GitHubThreadList {
  return { threads: overrides.threads ?? [], status: threadsStatus(overrides.status) }
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
    yours: list([]),
    rate: { graphql: unknownRate, core: unknownRate, search: unknownRate },
    lastRefreshedAt: '',
    lastError: '',
    scopeWarning: '',
    projectAccess: 'on',
    refreshing: false,
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
    missingCount: 0,
    files: [],
    lastCommitUnix: 0,
    head: 'fc1c3975011e66491785ace891c893e4fc716587',
    unpushed: 0,
    onGitHub: true,
    item: null,
    // A worktree with an item is linked to it somehow: by its name, unless said.
    linkedBy: overrides.item === undefined || overrides.item === null ? '' : 'name',
    copies: worktreeCopies(),
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

// worktreeCopies is what comparing a worktree's commits not on GitHub with the pushed
// branches found: by default nothing, as for a worktree with every commit on GitHub.
export function worktreeCopies(overrides: Partial<WorktreeCopies> = {}): WorktreeCopies {
  return {
    ref: '',
    branch: '',
    count: 0,
    identical: 0,
    edited: 0,
    none: 0,
    recorded: false,
    pr: 0,
    gone: false,
    note: '',
    ...overrides,
  }
}

// copiedPR is pull request #42, open, whose branch, feature-x, a session pushed with the
// commits of a sub-agent's worktree copied onto it.
export function copiedPR(overrides: Partial<LinkedItem> = {}): LinkedItem {
  return linkedItem({
    number: 42,
    title: 'Describe the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/42',
    state: 'open',
    closedAt: '',
    headOid: '4d2e8f1a0b3c5d7e9f1a2b3c4d5e6f7a8b9c0d1e',
    headRef: 'feature-x',
    launched: false,
    ...overrides,
  })
}

// copiedWorktree is a sub-agent's worktree Claude Code made in example-repo, on its own
// branch, whose 10 commits are on no remote-tracking branch by commit ID, but each has an
// identical copy on origin/feature-x, so they're on GitHub: it is linked by those copies to
// pull request #42, still open, whose branch keeps its own branch, which may not go yet.
export function copiedWorktree(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    path: '/home/user/projects/example-repo/.claude/worktrees/agent-a1b2c3',
    displayPath: '~/projects/example-repo/.claude/worktrees/agent-a1b2c3',
    branch: 'worktree-agent-a1b2c3',
    head: '9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d',
    legacy: false,
    unpushed: 10,
    onGitHub: true,
    item: copiedPR(),
    linkedBy: 'copy',
    copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, pr: 42 }),
    stage: 'active',
    branches: [
      ownBranch({
        name: 'worktree-agent-a1b2c3',
        head: '9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d',
        deletable: false,
        reason: "its commits are on GitHub only as copies, in pull request #42, which hasn't merged with them",
      }),
    ],
    ...overrides,
  })
}

// recordedWorktree is that worktree once pull request #42 merged and its branch was
// deleted: no remote-tracking branch has the copies now, but the app's record of them
// vouches that the pull request has every one, so it is finished, its own branch free to go.
export function recordedWorktree(overrides: Partial<Worktree> = {}): Worktree {
  return copiedWorktree({
    item: copiedPR({ state: 'merged', closedAt: '2026-06-13T12:00:00Z' }),
    copies: worktreeCopies({
      ref: 'origin/feature-x',
      branch: 'feature-x',
      count: 10,
      identical: 10,
      recorded: true,
      pr: 42,
      gone: true,
    }),
    stage: 'finished',
    canCleanUp: true,
    branches: [ownBranch({ name: 'worktree-agent-a1b2c3', head: '9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d' })],
    ...overrides,
  })
}

// partlyCopiedWorktree is another sub-agent's worktree whose one commit not on GitHub has
// only an edited copy on origin/feature-x, pull request #42's branch: the same author,
// time, and subject, but another change, so it isn't on GitHub, and nothing links it.
export function partlyCopiedWorktree(overrides: Partial<Worktree> = {}): Worktree {
  return copiedWorktree({
    path: '/home/user/projects/example-repo/.claude/worktrees/agent-d4e5f6',
    displayPath: '~/projects/example-repo/.claude/worktrees/agent-d4e5f6',
    branch: 'worktree-agent-d4e5f6',
    head: '1f2e3d4c5b6a7980f1e2d3c4b5a69788f9e0d1c2',
    unpushed: 1,
    onGitHub: false,
    item: null,
    linkedBy: '',
    copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 1, edited: 1, pr: 42 }),
    stage: '',
    branches: [
      ownBranch({
        name: 'worktree-agent-d4e5f6',
        head: '1f2e3d4c5b6a7980f1e2d3c4b5a69788f9e0d1c2',
        deletable: false,
        reason: "1 commit on it isn't on GitHub",
      }),
    ],
    ...overrides,
  })
}

// worktreeFileCodes are the status letters worktreeFiles gives its files, in turn.
const worktreeFileCodes = [' M', '??', 'A ', ' D']

// worktreeFiles is n changed files, sorted by path as Go sends them: src/file-001.ts on,
// modified, untracked, added, and deleted in turn.
export function worktreeFiles(n: number): WorktreeFile[] {
  return Array.from({ length: n }, (_, i) => ({
    code: worktreeFileCodes[i % worktreeFileCodes.length] ?? ' M',
    path: `src/file-${String(i + 1).padStart(3, '0')}.ts`,
  }))
}

// worktreeCommit is a commit on a worktree that isn't on GitHub, made two hours before the
// tests' clock.
export function worktreeCommit(overrides: Partial<WorktreeCommit> = {}): WorktreeCommit {
  return {
    oid: '0b1c3975011e66491785ace891c893e4fc716588',
    unix: 1781517600,
    subject: 'Keep the widget cache warm',
    copy: '',
    copyOid: '',
    ...overrides,
  }
}

// copyComparison is how a drawer's comparison of a worktree's commits not on GitHub with
// the pushed branches went: by default it ran, and each of 10 has an identical copy on
// origin/feature-x, as copiedWorktree's do.
export function copyComparison(overrides: Partial<WorktreeCopyComparison> = {}): WorktreeCopyComparison {
  return {
    compared: true,
    ref: 'origin/feature-x',
    branch: 'feature-x',
    identical: 10,
    edited: 0,
    note: '',
    ...overrides,
  }
}

// worktreeDetails is what a clean worktree's drawer reads, all of it on GitHub and level
// with origin/main, which the app made a day before the tests' clock.
export function worktreeDetails(overrides: Partial<WorktreeDetails> = {}): WorktreeDetails {
  return {
    files: [],
    fileCount: 0,
    missingCount: 0,
    commits: [],
    unpushed: 0,
    copies: { compared: false, ref: '', branch: '', identical: 0, edited: 0, note: '' },
    base: 'origin/main',
    behind: 0,
    createdAt: '2026-06-14T12:00:00Z',
    errors: [],
    ...overrides,
  }
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
    unpushed: 0,
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

// managed is the path the window shows for a worktree the app made, name, under the clone
// root's managed root, for clone: "~/projects/.<slug>/worktrees/example-repo/pr-7".
export function managed(name: string, clone = 'example-repo'): string {
  return `~/projects/.${brand.slug}/worktrees/${clone}/${name}`
}

// agentName names an agent as the worktrees page's lines and menus take it: Codex, and
// Claude for any other.
export function agentName(id: AgentID): string {
  return id === 'codex' ? 'Codex' : 'Claude'
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
    roots: ['~/projects'],
    unread: [],
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
    worktreeAction: 'create',
    permission: 'full access',
    notice: '',
    state: 'needs_approval',
    detail: 'Permission prompt',
    stateAt: '2026-06-15T11:59:00Z',
    lastMessage: 'I need to run the test suite. May I?',
    launchedAt: '2026-06-15T11:55:00Z',
    endedAt: '',
    canFocus: true,
    canResume: false,
    runId: 0,
    githubEvent: '',
    githubEventCommit: '',
    githubEventAt: '',
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

// rerequestOffer asks GitHub for the user's review again, which sends no text and runs on
// the click, as the backend marks it.
export function rerequestOffer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return offer({ action: 'rerequest_review', label: 'Re-request my review', reviews: false, ...overrides })
}

// updateBranchOffer merges the base branch into the pull request's branch, which sends no
// text and runs on the click, as the backend marks it.
export function updateBranchOffer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return offer({ action: 'update_branch', label: 'Update branch', reviews: false, ...overrides })
}

// approveRunsOffer approves the workflow runs GitHub holds for approval, which sends no
// text and runs on the click, as the backend marks it.
export function approveRunsOffer(overrides: Partial<ReviewOffer> = {}): ReviewOffer {
  return offer({ action: 'approve_runs', label: 'Approve workflows to run', reviews: false, ...overrides })
}

// heldRun is a fork's workflow run named name, with the ID id, which GitHub holds for a
// maintainer's approval.
export function heldRun(name: string, id: number): ReviewUnrun {
  return {
    name,
    approval: true,
    onGitHub: false,
    runId: id,
    url: `https://github.com/acme-org/example-repo/actions/runs/${String(id)}`,
    ignored: false,
  }
}

// heldOnGitHub is a workflow run named name, with the ID id, waiting for an approval GitHub
// takes only on its site, as a run a review started in the base repository does. It doesn't
// count toward where the pull request stands (ignored), as the backend marks it while GitHub
// doesn't report the merge blocked; blockedOnGitHub is the same run while GitHub does.
export function heldOnGitHub(name: string, id: number): ReviewUnrun {
  return { ...heldRun(name, id), onGitHub: true, ignored: true }
}

export function blockedOnGitHub(name: string, id: number): ReviewUnrun {
  return { ...heldOnGitHub(name, id), ignored: false }
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
    openedAt: '2026-06-14T12:00:00Z',
    body: 'Adds a cache in front of the widget store.',
    bodyTrimmed: false,
    editedAt: '',
    reactions: [],
    bump: null,
    change: { kind: 'code', label: 'Code', areas: ['cache'] },
    files: [reviewFile()],
    totalFiles: 1,
    filesComplete: true,
    checks: [{ name: 'test', workflow: 'CI', state: 'SUCCESS', outcome: 'passed', url: '', required: true }],
    counts: {
      state: 'SUCCESS',
      passed: 1,
      failed: 0,
      pending: 0,
      skipped: 0,
      at: '2026-06-15T11:59:00Z',
      newer: false,
    },
    moreChecks: 0,
    moved: false,
    unrun: [],
    reviews: [],
    timeline: { items: [], total: 0, before: '' },
    lastSeenAt: '',
    commitList: [],
    earlierCommits: 0,
    yourReview: null,
    behind: false,
    behindBy: 0,
    closes: [],
    moreCloses: 0,
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
    ownAwaitsApproval: false,
    mergeMethods: ['MERGE', 'SQUASH'],
    defaultMethod: 'SQUASH',
    autoMerge: '',
    head: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    fetchedAt: '2026-06-15T11:59:00Z',
    stale: false,
    canEditReviewers: false,
    assignees: [],
    moreAssignees: 0,
    canEditAssignees: false,
    ...overrides,
  }
}

// assigneeCandidates is whom the assignee picker offers for pull request #7 or issue #12,
// in acme-org/example-repo, as the app learned them before a read of who can be assigned
// there: the user, the author octo-other, octo-commenter, who took part, octo-member, who
// works on the repository, and octo-org, who works on acme-org/web.
export function assigneeCandidates(overrides: Partial<AssigneeCandidates> = {}): AssigneeCandidates {
  const person = (c: Partial<AssigneeCandidate> & Pick<AssigneeCandidate, 'login' | 'source'>): AssigneeCandidate => ({
    name: '',
    mine: false,
    reviewed: 0,
    authored: 0,
    committed: 0,
    assignedAt: '',
    lastAt: '',
    where: '',
    ...c,
  })

  return {
    people: [
      person({ login: 'octo-user', name: 'Octo User', mine: true, source: 'you' }),
      person({ login: 'octo-other', source: 'author' }),
      person({ login: 'octo-commenter', source: 'participant' }),
      person({
        login: 'octo-member',
        name: 'Octo Member',
        source: 'works_here',
        reviewed: 12,
        lastAt: '2026-06-12T12:00:00Z',
      }),
      person({ login: 'octo-org', source: 'org', authored: 2, lastAt: '2026-06-14T12:00:00Z', where: 'acme-org/web' }),
    ],
    total: 0,
    complete: false,
    readAt: '',
    stale: false,
    ...overrides,
  }
}

// reviewerCandidates is who the reviewer picker offers for pull request #7: GitHub suggests
// octo-suggested, who changed its files, then octo-member and octo-helper work on
// acme-org/example-repo, which has the web and api teams, learned from 50 pull requests.
export function reviewerCandidates(overrides: Partial<Reviewers> = {}): Reviewers {
  return {
    people: [
      {
        login: 'octo-suggested',
        name: 'Octo Suggested',
        suggestion: 'changed_files',
        reviewed: 2,
        authored: 0,
        committed: 3,
        lastAt: '2026-06-14T12:00:00Z',
      },
      {
        login: 'octo-member',
        name: 'Octo Member',
        suggestion: '',
        reviewed: 12,
        authored: 4,
        committed: 0,
        lastAt: '2026-06-12T12:00:00Z',
      },
      {
        login: 'octo-helper',
        name: '',
        suggestion: '',
        reviewed: 0,
        authored: 3,
        committed: 0,
        lastAt: '2026-05-01T12:00:00Z',
      },
    ],
    teams: [
      { slug: 'web', name: 'Web' },
      { slug: 'api', name: 'API' },
    ],
    pulls: 50,
    readAt: '2026-06-13T12:00:00Z',
    stale: false,
    ...overrides,
  }
}

// reviewCommit is one of the widget cache's commits, by its author.
export function reviewCommit(overrides: Partial<ReviewCommit> = {}): ReviewCommit {
  const oid = overrides.oid ?? 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'

  return {
    oid,
    short: oid.slice(0, 7),
    headline: 'Add the widget cache',
    author: 'octo-other',
    committedAt: '2026-06-15T10:00:00Z',
    url: `https://github.com/acme-org/example-repo/pull/7/commits/${oid}`,
    ...overrides,
  }
}

// yourReview is the user's request for changes on the widget cache's first commit, with one
// commit pushed since.
export function yourReview(overrides: Partial<YourReview> = {}): YourReview {
  return {
    state: 'CHANGES_REQUESTED',
    commit: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    at: '2026-06-15T10:30:00Z',
    url: 'https://github.com/acme-org/example-repo/pull/7#pullrequestreview-1',
    since: 'pushed',
    count: 1,
    ...overrides,
  }
}

// lineComment is a review's comment on line 12 of the widget cache, on the head's diff.
export function lineComment(overrides: Partial<TimelineLineComment> = {}): TimelineLineComment {
  return {
    id: 'PRRC_1',
    url: 'https://github.com/acme-org/example-repo/pull/7#discussion_r1',
    at: '2026-06-15T10:40:00Z',
    path: 'cache/widget.go',
    line: 12,
    startLine: 0,
    outdated: false,
    body: 'Should this expire?',
    trimmed: false,
    hunk: [' func get() {', '+\treturn cache[key]'],
    ...overrides,
  }
}

// reviewEntry is octo-reviewer's review of pull request 7 in its conversation: changes
// requested, with a word and a line comment.
export function reviewEntry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return issueEvent({
    kind: 'reviewed',
    id: 'PRR_1',
    at: '2026-06-15T10:45:00Z',
    actor: 'octo-reviewer',
    body: 'Two things before this merges.',
    url: 'https://github.com/acme-org/example-repo/pull/7#pullrequestreview-1',
    association: 'MEMBER',
    label: null,
    reviewState: 'CHANGES_REQUESTED',
    lineComments: [lineComment()],
    ...overrides,
  })
}

// reviewTimeline is pull request 7's conversation: the user's comment, octo-reviewer's
// review with its line comment, and octo-other's reply, the whole of it.
export function reviewTimeline(overrides: Partial<TimelinePage> = {}): TimelinePage {
  return {
    items: [
      issueComment({
        id: 'IC_1',
        at: '2026-06-15T10:00:00Z',
        actor: 'octo-user',
        mine: true,
        body: 'Is this ready for a look?',
        url: 'https://github.com/acme-org/example-repo/pull/7#issuecomment-1',
        association: 'OWNER',
      }),
      reviewEntry(),
      issueComment({
        id: 'IC_2',
        body: 'Expiry comes in the next pull request.',
        url: 'https://github.com/acme-org/example-repo/pull/7#issuecomment-2',
      }),
    ],
    total: 3,
    before: '',
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

// issueOffer is an offer to comment on issue 12, enabled.
export function issueOffer(overrides: Partial<IssueOffer> = {}): IssueOffer {
  return {
    action: 'comment',
    label: 'Comment',
    enabled: true,
    reason: '',
    confirm: false,
    commentsFirst: false,
    commentBlocked: '',
    ...overrides,
  }
}

// closeIssueOffer is an offer to close issue 12 as completed, which asks first and posts
// the text before it.
export function closeIssueOffer(overrides: Partial<IssueOffer> = {}): IssueOffer {
  return issueOffer({
    action: 'close_completed',
    label: 'Close as completed',
    confirm: true,
    commentsFirst: true,
    ...overrides,
  })
}

// issueRef is issue 3 of example-repo, open, as the sheet names a linked item.
export function issueRef(overrides: Partial<IssueLinked> = {}): IssueLinked {
  return {
    kind: 'issue',
    repo: 'acme-org/example-repo',
    number: 3,
    title: 'Parser bug',
    url: 'https://github.com/acme-org/example-repo/issues/3',
    state: 'OPEN',
    stateReason: '',
    draft: false,
    ...overrides,
  }
}

// pullRef is pull request 14 of example-repo, open, as the sheet names a linked item.
export function pullRef(overrides: Partial<IssueLinked> = {}): IssueLinked {
  return issueRef({
    kind: 'pr',
    number: 14,
    title: 'Keep the trailing newline',
    url: 'https://github.com/acme-org/example-repo/pull/14',
    ...overrides,
  })
}

// issueEvent is a timeline entry of issue 12: octo-other labeled it bug, two days ago.
export function issueEvent(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    kind: 'labeled',
    id: 'LE_1',
    at: '2026-06-13T12:00:00Z',
    actor: 'octo-other',
    actorIsBot: false,
    mine: false,
    body: '',
    trimmed: false,
    url: '',
    association: '',
    editedAt: '',
    minimized: false,
    minimizedReason: '',
    ref: null,
    willClose: false,
    commit: '',
    duplicateOf: null,
    stateReason: '',
    label: { name: 'bug', color: '#d73a4a' },
    assignee: '',
    from: '',
    to: '',
    reviewState: '',
    outdated: false,
    lineComments: [],
    moreLineComments: 0,
    reviewer: '',
    branch: '',
    ...overrides,
  }
}

// issueComment is a comment on issue 12: octo-other's, an hour before the clock's now.
export function issueComment(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return issueEvent({
    kind: 'comment',
    id: 'IC_2',
    at: '2026-06-15T11:00:00Z',
    body: 'Can repro on 1.4.',
    url: 'https://github.com/acme-org/example-repo/issues/12#issuecomment-2',
    association: 'MEMBER',
    label: null,
    ...overrides,
  })
}

// issueTimeline is issue 12's newest page: the user's comment, a mention by pull request
// 14, and octo-other's comment, of 40 items in all.
export function issueTimeline(overrides: Partial<TimelinePage> = {}): TimelinePage {
  return {
    items: [
      issueComment({
        id: 'IC_1',
        at: '2026-06-15T10:00:00Z',
        actor: 'octo-user',
        mine: true,
        body: 'Looking into it.',
        url: 'https://github.com/acme-org/example-repo/issues/12#issuecomment-1',
        association: 'OWNER',
      }),
      issueEvent({ kind: 'mentioned', id: 'CRE_1', at: '2026-06-15T10:30:00Z', label: null, ref: pullRef() }),
      issueComment(),
    ],
    total: 40,
    before: 'Y3Vyc29y',
    ...overrides,
  }
}

// issue is issue 12 of example-repo as the sheet read it: octo-other's, assigned to the
// user, open, with a description, a conversation, and the offers to comment and close.
export function issue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: 1,
    repo: 'acme-org/example-repo',
    number: 12,
    title: 'Parser drops the trailing newline',
    url: 'https://github.com/acme-org/example-repo/issues/12',
    state: 'OPEN',
    stateReason: '',
    closedAt: '',
    author: 'octo-other',
    isBot: false,
    mine: false,
    openedAt: '2026-06-12T12:00:00Z',
    updatedAt: '2026-06-15T11:00:00Z',
    assigned: true,
    locked: false,
    lockReason: '',
    canEdit: true,
    body: 'It drops the newline at the end of the file.',
    bodyTrimmed: false,
    editedAt: '',
    reactions: [{ content: 'THUMBS_UP', count: 4 }],
    type: { name: 'Bug', color: '#d73a4a', description: 'Something isn’t working' },
    milestone: null,
    labels: [{ name: 'bug', color: '#d73a4a' }],
    projects: [],
    assignees: [{ login: 'octo-user', mine: true }],
    moreAssignees: 0,
    participants: [
      { login: 'octo-other', mine: false },
      { login: 'octo-user', mine: true },
    ],
    moreParticipants: 0,
    parent: null,
    duplicateOf: null,
    subIssues: [],
    subIssuesTotal: 0,
    subIssuesDone: 0,
    fixes: [],
    moreFixes: 0,
    mentioned: [pullRef()],
    timeline: issueTimeline(),
    lastSeenAt: '',
    seen: { comments: 2, latest: 'IC_2', latestAt: '2026-06-15T11:00:00Z' },
    standing: { state: 'assigned', sentence: 'Assigned to you', at: '2026-06-12T12:00:00Z', opened: true },
    offers: [
      issueOffer(),
      closeIssueOffer(),
      closeIssueOffer({ action: 'close_not_planned', label: 'Close as not planned' }),
    ],
    primary: 'comment',
    fetchedAt: '2026-06-15T11:59:00Z',
    stale: false,
    ...overrides,
  }
}

// issueResult is what a comment on issue 12 came to.
export function issueResult(overrides: Partial<IssueResult> = {}): IssueResult {
  return {
    outcome: 'commented',
    message: 'Commented on acme-org/example-repo#12',
    reason: '',
    steps: ['Commented'],
    commented: true,
    leftInbox: false,
    ...overrides,
  }
}

// repoLabels are example-repo's labels, as the label picker reads them.
export function repoLabels(overrides: Partial<IssueRepoLabels> = {}): IssueRepoLabels {
  return {
    labels: [
      { name: 'bug', color: '#d73a4a', description: 'Something isn’t working' },
      { name: 'p1', color: '#b60205', description: '' },
      { name: 'triage', color: '', description: 'Needs a first look' },
    ],
    total: 3,
    ...overrides,
  }
}

// issueReplies are the comment replies the issue sheet offers: one for issues, one for
// both kinds.
export function issueReplies(): IssueReply[] {
  return [
    { id: 31, name: 'Looking into it', nickname: 'Looking' },
    { id: 32, name: 'Thanks', nickname: 'Thanks' },
  ]
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
    nudge: 0,
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
    usage: {
      defaults: [],
      slots: [],
      orgs: [],
      repos: [],
      slotPlaces: [],
      kinds: [],
      replies: ['approve'],
      routines: [],
    },
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
      usage: {
        defaults: [],
        slots: [],
        orgs: [],
        repos: [],
        slotPlaces: [],
        kinds: [],
        replies: ['approve_bots'],
        routines: [],
      },
    }),
    replyPrompt({
      id: 23,
      name: 'Ask for changes',
      nickname: 'Needs changes',
      body: 'Thanks, @{{.Author}}. A few things to change first:\n\n- \n',
      replyAction: 'request_changes',
      starterKey: 'needs-changes',
      usage: { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] },
    }),
    replyPrompt({
      id: 24,
      name: 'Ask a question',
      nickname: 'Question',
      body: 'A question before I approve, @{{.Author}}:\n\n',
      replyAction: 'comment',
      starterKey: '',
      usage: { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] },
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
    stuck: 0,
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
    follow: null,
    ...overrides,
  }
}

// releaseFollow is following acme-org/sdk v1.4.3's workflow, three polls in, from
// FIXED_NOW for 45 minutes.
export function releaseFollow(overrides: Partial<ReleaseFollow> = {}): ReleaseFollow {
  return {
    state: 'following',
    since: '2026-06-15T12:00:00Z',
    until: '2026-06-15T12:45:00Z',
    ended: '',
    minutes: 0,
    polls: 3,
    runsDone: false,
    workflow: '',
    url: '',
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

// manifestsModule is acme-org/api-server's go.mod at its top: a Go module whose
// dependencies last changed fourteen months before FIXED_NOW, with its go.sum.
export function manifestsModule(overrides: Partial<ManifestsModule> = {}): ManifestsModule {
  return {
    kind: 'go',
    path: 'go.mod',
    lockfile: 'go.sum',
    changedAt: '2025-04-15T12:00:00Z',
    subject: 'chore(deps): bump the go modules (#41)',
    goModule: 'github.com/acme-org/api-server',
    goVersion: '1.22',
    toolchain: '',
    direct: 4,
    indirect: 9,
    name: '',
    packageManager: '',
    workspaces: [],
    deps: 0,
    devDeps: 0,
    peerDeps: 0,
    optionalDeps: 0,
    problem: '',
    uncounted: '',
    ...overrides,
  }
}

// manifestsClone is acme-org/api-server's clone, read at origin/master as it last fetched
// it three days before FIXED_NOW: its go.mod and its web console's package.json, set up for
// magex and pnpm.
export function manifestsClone(overrides: Partial<ManifestsClone> = {}): ManifestsClone {
  return {
    repo: 'acme-org/api-server',
    dir: '~/projects/api-server',
    pinned: false,
    modules: [
      manifestsModule(),
      manifestsModule({
        kind: 'js',
        path: 'web/package.json',
        lockfile: 'pnpm-lock.yaml',
        changedAt: '2026-02-15T12:00:00Z',
        subject: 'chore(web): refresh the lockfile (#52)',
        goModule: '',
        goVersion: '',
        direct: 0,
        indirect: 0,
        name: 'web-console',
        packageManager: 'pnpm',
        deps: 48,
        devDeps: 12,
      }),
    ],
    counted: 2,
    oldest: '2025-04-15T12:00:00Z',
    checkedAt: '',
    ageFrom: '2025-04-15T12:00:00Z',
    tools: ['magex', 'pnpm'],
    workspaces: [],
    tip: 'origin/master',
    commit: '4f2a9c1e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a39',
    atHead: false,
    atHeadWhy: '',
    defaultBranch: 'master',
    fetchedAt: '2026-06-12T12:00:00Z',
    shallow: false,
    truncated: false,
    error: '',
    ...overrides,
  }
}

// manifestsPage is the Deps page as the clones were read a minute before FIXED_NOW: one
// stale clone, acme-org/api-server, with one muted clone and one not on GitHub left out.
export function manifestsPage(overrides: Partial<ManifestsPage> = {}): ManifestsPage {
  return {
    clones: [manifestsClone()],
    muted: 1,
    withoutGitHub: 1,
    withoutModules: 0,
    modules: 2,
    checkedAt: '2026-06-15T11:59:00Z',
    error: '',
    ...overrides,
  }
}

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
    deps: false,
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
    uuid: fixtureUUID(500 + (overrides.id ?? 1)),
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

// updateStatus is a release build of 0.2.0 in /Applications, up to date: its last check,
// twelve minutes before the tests' noon, found nothing newer, and nothing failed.
export function updateStatus(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    flavor: 'release',
    enabled: true,
    off: '',
    current: '0.2.0',
    mode: 'download',
    state: 'idle',
    checking: false,
    progress: 0,
    latest: '',
    releaseUrl: 'https://github.com/acme-org/example-repo/releases/tag/v0.2.0',
    lastChecked: '2026-06-15T11:48:00Z',
    lastError: '',
    bannerHidden: false,
    justUpdated: '',
    installFailed: { version: '', reason: '', log: false, retryable: false },
    location: { ok: true, kind: 'applications', target: '/Applications/Example.app', movable: false },
    ...overrides,
  }
}

// updateLocation is where a release build runs that can't update itself, which a move
// would put in /Applications: in Downloads unless kind says elsewhere, movable unless it is
// in an Applications folder already.
export function updateLocation(kind: UpdateLocationKind = 'elsewhere'): UpdateLocation {
  return {
    ok: false,
    kind,
    target: '/Applications/Example.app',
    movable: kind !== 'applications' && kind !== 'userApplications',
  }
}

// moveResult is how moving the app into Applications ended: moved, by default.
export function moveResult(overrides: Partial<MoveResult> = {}): MoveResult {
  return { outcome: 'moved', target: '/Applications/Example.app', ...overrides }
}

// updateReady is 0.3.0 downloaded and checked, waiting for a restart to install it.
export function updateReady(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return updateStatus({
    state: 'ready',
    latest: '0.3.0',
    releaseUrl: 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0',
    ...overrides,
  })
}

// runGoing is a release running in the app, which restarting to update would stop.
export function runGoing(overrides: Partial<RunGoing> = {}): RunGoing {
  return {
    id: 7,
    kind: 'release',
    title: 'Release v1.2.0',
    repo: 'acme-org/sdk',
    dir: '~/projects/sdk',
    stopRefused: '',
    outlives: false,
    ...overrides,
  }
}

// galleryTemplate is the flagship template, Fix Dependabot security failures, as the gallery
// shows it in a build that can't run its background agent yet.
export function galleryTemplate(overrides: Partial<AutopilotTemplate> = {}): AutopilotTemplate {
  return {
    key: 'fix-dependabot-security',
    name: 'Fix Dependabot security failures',
    icon: '🛡️',
    summary: 'When a Dependabot update fails a security scan, an agent fixes the cause in the background.',
    suggests: 'ask',
    blanks: [
      {
        kind: 'where',
        label: 'Where',
        help: 'Your account, and the orgs, users, or repositories to watch too.',
        source: 0,
        condition: 0,
        required: false,
      },
    ],
    spec: {
      version: 1,
      watch: { sources: [{ search: { kind: 'pr', authors: ['app/dependabot'], where: ['@me'] } }] },
      trigger: { event: 'checks_failed', waitForChecks: true },
      conditions: {
        match: 'all',
        of: [{ field: 'checks.failedNames', op: 'anyContains', values: ['security', 'codeql'] }],
      },
      steps: [{ agent: { prompt: { starter: 'fix-dependency-security' }, where: 'background' } }, { tellMe: {} }],
      limits: { oncePer: 'headCommit' },
    },
    sentence:
      'When checks fail on pull requests by Dependabot in your account, run "Fix dep security" in the background.',
    needs: ['agents in the background'],
    needsWords: `It needs agents in the background, which this version of ${brand.name} can’t run yet.`,
    prompts: [{ starter: 'fix-dependency-security', name: 'Fix dep security', inLibrary: true }],
    suggested: '',
    ...overrides,
  }
}

// snoozeTemplate is Snooze passing bot PRs, which asks nothing and runs in every build.
export function snoozeTemplate(overrides: Partial<AutopilotTemplate> = {}): AutopilotTemplate {
  return galleryTemplate({
    key: 'snooze-passing-bot-prs',
    name: 'Snooze passing bot PRs',
    icon: '⏾',
    summary: 'A trusted bot’s patch or minor update whose checks pass leaves your lists for three days.',
    suggests: 'auto',
    blanks: [],
    spec: routineSpec(),
    sentence: 'When checks pass on pull requests in your Review requested list, snooze it for 3 days.',
    needs: [],
    needsWords: '',
    prompts: [],
    ...overrides,
  })
}

// itemDraft is a routine filled in from pull request 7, Dependabot's, whose checks failed.
export function itemDraft(overrides: Partial<AutopilotItemDraft> = {}): AutopilotItemDraft {
  return {
    draft: {
      id: 0,
      name: 'Dependabot’s pull requests in acme-org',
      mode: 'ask',
      spec: {
        version: 1,
        watch: { sources: [{ search: { kind: 'pr', authors: ['app/dependabot'], where: ['org:acme-org'] } }] },
        trigger: { event: 'checks_failed' },
        conditions: { match: 'all', of: [{ field: 'checks.failedNames', op: 'anyContains', values: ['CodeQL'] }] },
        steps: [{ agent: { prompt: { itemDefault: true }, where: 'terminal' } }],
        limits: {},
      },
    },
    suggestions: [{ field: 'dep.updateType', op: 'oneOf', values: ['minor'] }],
    item: {
      id: 7,
      kind: 'pr',
      repo: 'acme-org/example-repo',
      number: 7,
      title: 'Bump vite from 7.1.0 to 7.2.1',
      url: 'https://github.com/acme-org/example-repo/pull/7',
    },
    match: 'yes',
    trace: [{ path: 'trigger', condition: 'checks fail', result: 'yes', actual: 'CodeQL failed' }],
    ...overrides,
  }
}

// routineImportPreview is a bundle of one routine that names a profile this Mac lacks, and
// brings one prompt.
export function routineImportPreview(overrides: Partial<AutopilotImportPreview> = {}): AutopilotImportPreview {
  return {
    sha256: 'a1b2c3',
    routines: [
      {
        name: 'Triage bugs',
        arrivesAs: 'Triage bugs',
        sentence: 'When an issue in your Assigned list is labeled bug, run "Triage" in your terminal.',
        broken: '',
      },
    ],
    prompts: [{ nickname: 'Triage', arrives: 'new' }],
    missing: [
      {
        kind: 'profile',
        agent: 'claude',
        name: 'work',
        starter: '',
        reply: false,
        routines: ['Triage bugs'],
        choices: ['default', 'personal'],
        inherits: false,
      },
    ],
    ...overrides,
  }
}

// describeStatus is Describe it ready to run a turn, with the launch defaults' Claude
// profile and the agent inherited.
export function describeStatus(overrides: Partial<DescribeStatus> = {}): DescribeStatus {
  return {
    ready: true,
    problem: '',
    fix: '',
    version: '2.1.328',
    agent: describeAgentChoice(),
    profile: 'default',
    model: 'sonnet',
    effort: '',
    running: false,
    pending: 0,
    ...overrides,
  }
}

// describeAgentChoice is the agent a conversation runs with: each part inherited.
export function describeAgentChoice(overrides: Partial<DescribeAgentChoice> = {}): DescribeAgentChoice {
  return { profile: '', model: '', effort: '', ...overrides }
}

// describeConversation is no conversation yet, unless overrides say.
export function describeConversation(overrides: Partial<DescribeConversation> = {}): DescribeConversation {
  return {
    id: '',
    agent: describeAgentChoice(),
    profile: 'default',
    model: 'sonnet',
    effort: '',
    createdAt: '2026-06-15T12:00:00Z',
    turns: [],
    running: false,
    cost: null,
    estimated: false,
    tokensIn: 0,
    tokensOut: 0,
    ...overrides,
  }
}

// describeTurn is a first turn that finished, without a card unless overrides say.
export function describeTurn(overrides: Partial<DescribeTurn> = {}): DescribeTurn {
  return {
    n: 1,
    text: 'When the checks fail on my pull requests, pin them.',
    attach: [],
    feedback: '',
    state: 'done',
    error: '',
    startedAt: '2026-06-15T12:00:00Z',
    endedAt: '2026-06-15T12:00:20Z',
    reply: 'Here is a routine that pins them. It starts switched off, in Ask first.',
    cost: 0.04,
    estimated: true,
    cards: [],
    ...overrides,
  }
}

// describeCard is a proposal of one new routine from the conversation's first turn, which
// checks clean.
export function describeCard(overrides: Partial<DescribeCard> = {}): DescribeCard {
  return {
    ref: { source: '00000000-0000-4000-8000-000000000001', proposal: '20260615T120010Z-9f2c4100ab31' },
    at: '2026-06-15T12:00:10Z',
    where: '',
    turn: 1,
    summary: 'A routine that pins your pull requests when their checks fail.',
    proposal: {
      summary: 'A routine that pins your pull requests when their checks fail.',
      routines: [{ name: 'Snooze passing bot PRs', mode: 'ask', spec: routineSpec() }],
    },
    checked: kitChecked(),
    outcomes: [],
    settled: false,
    ...overrides,
  }
}

// kitChecked is a proposal of one new routine, checked: no problem, no risk.
export function kitChecked(overrides: Partial<KitChecked> = {}): KitChecked {
  return { prompts: [], routines: [kitCheckedRoutine()], settings: null, rules: null, ...overrides }
}

// kitCheckedRoutine is a new routine, checked as the builder checks a draft.
export function kitCheckedRoutine(overrides: Partial<KitCheckedRoutine> = {}): KitCheckedRoutine {
  return {
    index: 0,
    name: 'Snooze passing bot PRs',
    nameTaken: false,
    spec: routineSpec(),
    sentence:
      'When checks pass on pull requests in Review requested, snooze it for 3 days. Once per head commit, at most 2 tries per pull request.',
    problems: [],
    warnings: [],
    dryRunOnly: [],
    before: null,
    gone: false,
    stale: false,
    risks: [],
    pastRules: false,
    ...overrides,
  }
}

// describePluginState is the plugin not installed in the launch defaults' profile.
export function describePluginState(overrides: Partial<DescribePluginState> = {}): DescribePluginState {
  return { profile: 'default', default: true, installed: false, version: '', problem: '', ...overrides }
}

// kitNewPrompt is a new agent prompt for pull requests, Fix it.
export function kitNewPrompt(overrides: Partial<KitNewPrompt> = {}): KitNewPrompt {
  return {
    name: 'Fix the failing checks',
    nickname: 'Fix it',
    purpose: 'agent',
    appliesTo: 'pr',
    body: 'Fix the failing checks of {{.Repo}} #{{.Number}}.',
    ...overrides,
  }
}

// kitCheckedPrompt is a new prompt, checked: no problem, no risk.
export function kitCheckedPrompt(overrides: Partial<KitCheckedPrompt> = {}): KitCheckedPrompt {
  return { index: 0, problems: [], risks: [], prompt: kitNewPrompt(), ...overrides }
}

// kitRoutine is routine 3 as Claude read it from the setup: Snooze passing bot PRs, on, in
// Autopilot.
export function kitRoutine(overrides: Partial<KitRoutine> = {}): KitRoutine {
  return {
    id: 3,
    name: 'Snooze passing bot PRs',
    description: 'Keeps the bots’ passing updates out of the way.',
    enabled: true,
    mode: 'auto',
    sentence:
      'When checks pass on pull requests in Review requested, snooze it for 3 days. Once per head commit, at most 2 tries per pull request.',
    broken: '',
    spec: routineSpec(),
    updatedAt: '2026-06-15T08:00:00Z',
    ...overrides,
  }
}

// kitCheckedSettings is a change of Agents at once from 2 to 1, checked.
export function kitCheckedSettings(overrides: Partial<KitCheckedSettings> = {}): KitCheckedSettings {
  return {
    changes: [{ key: 'agentsAtOnce', label: 'Agents at once', before: '2', after: '1' }],
    problems: [],
    risks: [],
    after: { ...settings().autopilot, agentsAtOnce: 1 },
    ...overrides,
  }
}

// kitCheckedRules is trusting renovate[bot] and muting acme-org/legacy, checked.
export function kitCheckedRules(overrides: Partial<KitCheckedRules> = {}): KitCheckedRules {
  return {
    changes: [
      { kind: 'trust', target: 'renovate[bot]', words: 'Trust renovate[bot]' },
      { kind: 'mute', target: 'acme-org/legacy', words: 'Mute acme-org/legacy' },
    ],
    problems: [],
    risks: [{ level: 'warning', text: 'Routines that act on trusted bots act on renovate[bot]’s pull requests too.' }],
    after: {
      trustedBots: ['dependabot', 'github-actions', 'renovate', 'renovate[bot]'],
      pinned: [],
      muted: ['acme-org/legacy'],
      ignoredWorkflows: [],
    },
    ...overrides,
  }
}

// kitOutcome is a new routine added from the conversation's first card, switched off.
export function kitOutcome(overrides: Partial<KitOutcome> = {}): KitOutcome {
  return {
    proposal: '20260615T120010Z-9f2c4100ab31',
    part: 'routine',
    index: 0,
    kind: 'added',
    words: 'Added “Snooze passing bot PRs”, switched off',
    routineId: 11,
    at: '2026-06-15T12:01:00Z',
    ...overrides,
  }
}

// describeAttachment is routine 3 attached to a message, as Change with Claude… attaches it.
export function describeAttachment(overrides: Partial<DescribeAttachment> = {}): DescribeAttachment {
  return {
    kind: 'routine',
    id: 3,
    label: 'Snooze passing bot PRs',
    words: 'The routine “Snooze passing bot PRs”.',
    ...overrides,
  }
}
