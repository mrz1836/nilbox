// A typed fake of the platform module, the window's only door to the desktop shell. A test
// file installs it with one line, which Vitest runs before anything imports the platform:
//
//   vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))
//
// and then reads and drives that same fake through the exports below. Vitest gives each
// test file a module registry of its own, so each file has its own fake; the
// configuration's mockReset puts every function back to its default before each test.

import { render } from '@testing-library/react'
import type { ComponentType, ReactNode } from 'react'
import { vi, type Mock } from 'vitest'

import { withRepoRule } from '../lib/repoRules'
import type * as real from '../platform'
import { clockAt, FIXED_NOW, withClock } from './clock'
import {
  afterMerge,
  agentRun,
  agentRunView,
  assigneeCandidates,
  agentVersions,
  toolVersions,
  agents,
  activityPage,
  appInfo,
  autopilotCatalog,
  autopilotEngine,
  autopilotGlance,
  autopilotMatch,
  backupImported,
  backupPreview,
  branchStatus,
  budgetGauge,
  checkResult,
  cloneTarget,
  defaultPrompts,
  described,
  describeAgentChoice,
  describeConversation,
  describePluginState,
  describeStatus,
  describeTurn,
  explanation,
  fileCheck,
  galleryTemplate,
  githubSignIn,
  githubThreadList,
  githubThreads,
  hookActionResult,
  hookChecksResult,
  hookInstallAll,
  hooksList,
  importPreview,
  installResult,
  integrationsConfig,
  issue,
  issueReplies,
  issueResult,
  repoLabels,
  issueTimeline,
  itemDraft,
  integrationTools,
  itemResolution,
  launchDefaults,
  launchPreview,
  launchResult,
  linkedClone,
  locationPreview,
  locationState,
  profile,
  prompt,
  promptBackup,
  prompts,
  queuedClear,
  readiness,
  releaseCandidates,
  releaseOffer,
  releaseResult,
  removeResult,
  replyList,
  repoScripts,
  resolution,
  review,
  reviewerCandidates,
  reviewResult,
  myScript,
  routine,
  routineImportPreview,
  routineReady,
  routineStats,
  routineSuggestions,
  routineTest,
  runEventsPage,
  runPreview,
  scriptClones,
  scriptRun,
  scriptRunChunk,
  serviceStatus,
  sessionsSnapshot,
  settings,
  setupSignals,
  terminalChoices,
  snapshot,
  snoozeTemplate,
  statusWatch,
  sweepReport,
  threadsPreview,
  updateReady,
  updateStatus,
  moveResult,
  nilMoment,
  userScript,
  variables,
  worktreeDetails,
  worktreesSnapshot,
} from './fixtures'

type Platform = typeof real

// Loose is a platform function as the fake implements it: the real function's arguments,
// answered with a plain promise where the real one returns Wails' CancellablePromise. The
// callers treat any promise alike, and cancel only one that can be cancelled.
type Loose<F> = F extends (...args: infer A) => real.CancellablePromise<infer R> ? (...args: A) => Promise<R> : F

// FakePlatform is the platform with every function a mock of its Loose form, so a test
// that calls one with the wrong arguments, answers for one with the wrong value, or reads
// its calls (mock.calls, mock.lastCall) as the wrong types does not compile. A value the
// platform hands out as it is, such as a generated constant, stays that value.
export type FakePlatform = {
  [K in keyof Platform]: Platform[K] extends (...args: never[]) => unknown ? Mock<Loose<Platform[K]>> : Platform[K]
}

// WindowEvent names an event the shell sends the window, and EventData is what it carries.
type WindowEvent = Parameters<Platform['on']>[0]
type EventData<E extends WindowEvent> = Parameters<Parameters<typeof real.on<E>>[1]>[0]

// Listener is a subscription the fake keeps, whatever its event's data.
type Listener = (data: unknown) => void

// stored answers a settings setter: the fixture's settings with the one change made, as
// every setter returns the settings as stored after its change.
function stored(change: Partial<real.Settings> = {}): Promise<real.Settings> {
  return Promise.resolve(settings(change))
}

// sensitiveFiles is the fixture's sensitive-file choices with change made.
function sensitiveFiles(change: Partial<real.Settings['sensitiveFiles']>): Partial<real.Settings> {
  return { sensitiveFiles: { ...settings().sensitiveFiles, ...change } }
}

// autopilotSettings is the fixture's Autopilot settings with change made.
function autopilotSettings(change: Partial<real.AutopilotConfig>): Partial<real.Settings> {
  return { autopilot: { ...settings().autopilot, ...change } }
}

// threadRules is the fixture's GitHub notification rules with change made.
function threadRules(change: Partial<real.ThreadRules>): Partial<real.Settings> {
  return { githubNotifications: { ...settings().githubNotifications, ...change } }
}

// fromInput is a prompt the editor saves, as the library lists it once stored.
function fromInput(input: Parameters<Platform['createPrompt']>[0]): Partial<real.Prompt> {
  return { ...input, models: input.models ?? {}, extraFlags: input.extraFlags ?? {} }
}

// answers are the fake's defaults: every read answers with the shared fixtures, every
// write succeeds, and on() subscribes through subscribe.
function answers(subscribe: Loose<Platform['on']>): { [K in keyof Platform]: Loose<Platform[K]> } {
  const done = () => Promise.resolve()
  const inbox = () => Promise.resolve(snapshot())

  return {
    getAppInfo: () => Promise.resolve(appInfo()),
    reportFirstPaint: done,
    agentVersions: () => Promise.resolve(agentVersions()),
    toolVersions: () => Promise.resolve(toolVersions()),
    bugReportURL: () => Promise.resolve('https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml'),
    feedbackURL: () => Promise.resolve('https://github.com/acme-org/example-repo/issues/new?template=BLANK_ISSUE'),

    getBudget: () => Promise.resolve(budgetGauge()),
    checkBudget: () => Promise.resolve(budgetGauge()),
    getServiceStatus: () => Promise.resolve(serviceStatus()),
    checkServiceStatus: () => Promise.resolve(serviceStatus()),
    getAutopilotGlance: () => Promise.resolve(autopilotGlance()),
    // No routine, and no history, unless a test asks for them.
    AutopilotMode: { ModeDryRun: 'dryRun', ModeAsk: 'ask', ModeAuto: 'auto', DefaultMode: 'ask' },
    listRoutines: () => Promise.resolve([]),
    routineNames: () => Promise.resolve([]),
    duplicateRoutine: (id) => Promise.resolve(routine({ id: id + 10, name: 'Snooze passing bot PRs copy' })),
    deleteRoutine: done,
    setRoutineEnabled: (id, enabled) =>
      Promise.resolve(routine({ id, enabled, enabledAt: enabled ? '2026-06-15T12:00:00Z' : null })),
    setRoutineMode: (id, mode) => Promise.resolve(routine({ id, mode })),
    routineMatching: () => Promise.resolve({ count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: false }),
    routineMatchStored: () => Promise.resolve({ count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: false }),
    // Describe it is ready, with no conversation, no card from outside, and the plugin not
    // installed, unless a test says so. A message starts a turn that runs.
    DescribeTurnState: { TurnRunning: 'running', TurnDone: 'done', TurnStopped: 'stopped', TurnFailed: 'failed' },
    DescribeAttachKind: { AttachRoutine: 'routine', AttachItem: 'item' },
    describeItStatus: () => Promise.resolve(describeStatus()),
    describeItAgent: () => Promise.resolve(describeAgentChoice()),
    setDescribeItAgent: (choice) => Promise.resolve(choice),
    describeItConversation: () => Promise.resolve(describeConversation()),
    newDescribeItConversation: (choice) =>
      Promise.resolve(describeConversation({ id: '00000000-0000-4000-8000-000000000001', agent: choice })),
    sendToDescribeIt: (message) =>
      Promise.resolve(
        describeConversation({
          id: '00000000-0000-4000-8000-000000000001',
          running: true,
          turns: [describeTurn({ text: message.text, state: 'running', endedAt: '', reply: '', cost: null })],
        }),
      ),
    stopDescribeIt: done,
    describeItEvents: () => Promise.resolve(runEventsPage({ done: true })),
    describeItProposals: () => Promise.resolve([]),
    settleDescribeItCard: done,
    discardDescribeItProposal: done,
    describeItPlugins: () => Promise.resolve([describePluginState()]),
    installDescribeItPlugin: () => Promise.resolve([describePluginState({ installed: true, version: '1.0.0' })]),
    removeDescribeItPlugin: () => Promise.resolve([describePluginState()]),
    routineWatching: () => Promise.resolve([]),
    getAutopilotEngine: () => Promise.resolve(autopilotEngine()),
    autopilotActivity: () => Promise.resolve(activityPage({ days: [] })),
    autopilotActivityFrom: () => Promise.resolve(activityPage({ days: [] })),
    autopilotItemHistory: (itemId) => Promise.resolve({ itemId, matches: [], open: 0 }),
    latestAutopilotMatches: () => Promise.resolve([]),
    undoAutopilot: (ids) => Promise.resolve({ putBack: ids ?? [], left: [], skipped: [], words: 'Put back 1 item.' }),
    tryRoutineAgain: () => Promise.resolve([]),
    approveAutopilotBacklog: () => Promise.resolve([]),
    skipAutopilotBacklog: (_routineId, ids) =>
      Promise.resolve(ids.map((id) => autopilotMatch({ id, state: 'dismissed', reason: 'you_skipped' }))),
    previewAutopilotActions: () => Promise.resolve([]),
    letRoutineActAgain: done,
    clearAutopilotHistory: () => Promise.resolve({ removed: 4, kept: 1 }),
    // The builder renders from the real catalog, and a draft reads with no problem.
    getAutopilotCatalog: () => Promise.resolve(autopilotCatalog()),
    suggestForRoutines: () => Promise.resolve(routineSuggestions()),
    describeRoutine: () => Promise.resolve(described()),
    testRoutine: () => Promise.resolve(routineTest()),
    // No agent works, and none waits, unless a test says so.
    autopilotWorking: () => Promise.resolve({ working: [] }),
    autopilotRunEvents: () => Promise.resolve(runEventsPage({ done: true })),
    autopilotRunEventsBefore: () => Promise.resolve(runEventsPage({ done: true })),
    autopilotRunView: (id) => Promise.resolve(agentRunView({ run: agentRun({ id }) })),
    autopilotRunBySession: () => Promise.resolve(0),
    autopilotRunDiff: () => Promise.resolve({ from: '', patch: '', cut: false, untracked: [] }),
    autopilotRunsToday: () => Promise.resolve({ background: 0, terminal: 0, actions: 0 }),
    stopAutopilotRun: done,
    stopAllAutopilotRuns: done,
    saveRoutine: (d) =>
      Promise.resolve(routine({ id: d.id === undefined || d.id === 0 ? 11 : d.id, name: d.name, spec: d.spec })),
    previewRoutineRun: () => Promise.resolve(runPreview()),
    // Nothing waits for the user, and this Mac is ready, unless a test says otherwise.
    autopilotNeedsYou: () => Promise.resolve([]),
    approveMatches: (ids) =>
      Promise.resolve((ids ?? []).map((id) => autopilotMatch({ id, state: 'queued', approved: true, closedAt: null }))),
    postReviewAsComment: (id) =>
      Promise.resolve([autopilotMatch({ id, state: 'queued', approved: true, closedAt: null })]),
    skipMatch: (id) => Promise.resolve(autopilotMatch({ id, state: 'dismissed', reason: 'you_skipped' })),
    getAutopilotReadiness: () => Promise.resolve(readiness()),
    checkAutopilotReadiness: () => Promise.resolve(readiness()),
    runRoutineNow: (id, itemId) =>
      Promise.resolve({
        ran: true,
        match: autopilotMatch({
          id: 40,
          routineId: id,
          item: { ...autopilotMatch().item, id: itemId },
          manual: true,
          approved: true,
          state: 'queued',
          reason: '',
          words: 'It starts as soon as a slot frees.',
          closedAt: null,
        }),
      }),
    explainRoutine: (id, itemId) => Promise.resolve(explanation({ routineId: id, itemId })),
    // The gallery holds the flagship and Snooze passing bot PRs, nothing suggested, and every
    // import and export goes through, unless a test says otherwise.
    listTemplates: () => Promise.resolve([galleryTemplate(), snoozeTemplate()]),
    routineFromItem: (itemId) =>
      Promise.resolve(itemDraft({ item: { ...itemDraft().item, id: itemId, number: itemId } })),
    draftReadiness: (d) => Promise.resolve(routineReady({ routineId: 0, name: d.name })),
    declineGraduate: (id) => Promise.resolve(routine({ id })),
    exportRoutines: (_path, ids) => Promise.resolve({ routines: ids?.length ?? 2, prompts: 0 }),
    previewRoutineImport: () => Promise.resolve(routineImportPreview()),
    importRoutines: () => Promise.resolve({ routines: ['Triage bugs'], prompts: ['Triage'] }),
    addStarterPrompt: (key) => Promise.resolve(prompt({ id: 21, starterKey: key })),
    // The user has one script, Lint; Run on the matches now runs on every item it is given;
    // and a routine's week holds nothing, unless a test says otherwise.
    autopilotMyScripts: () => Promise.resolve([myScript()]),
    runOnMatches: (id, items) =>
      Promise.resolve({
        matches: (items ?? []).map((itemId, i) =>
          autopilotMatch({
            id: 50 + i,
            routineId: id,
            item: { ...autopilotMatch().item, id: itemId },
            approved: true,
            state: 'queued',
            reason: '',
            closedAt: null,
          }),
        ),
        skipped: 0,
        words: `Running on ${String((items ?? []).length)} items.`,
      }),
    routineStats: () => Promise.resolve(routineStats()),
    itemsByID: () => Promise.resolve([]),

    getInboxSnapshot: inbox,
    refreshInbox: inbox,
    refreshItem: inbox,
    dismissItem: inbox,
    snoozeItem: inbox,
    restoreItem: inbox,
    pinItem: inbox,
    unpinItem: inbox,
    unassignItem: inbox,
    removeReviewRequest: inbox,
    nudgeAuthor: () =>
      Promise.resolve({ snapshot: snapshot(), url: 'https://github.com/acme-org/example-repo/pull/7#issuecomment-1' }),
    markItemsSeen: done,
    markItemUnseen: done,
    checkItemFiles: () => Promise.resolve(fileCheck()),

    // Each time at nil is the first day, unlocking nothing, unless a test says otherwise.
    reachedNil: (reach) => Promise.resolve(nilMoment({ earned: reach.earned, cleared: reach.cleared })),

    listProfiles: () => Promise.resolve([profile(), profile({ agent: 'codex', folder: '~/.codex' })]),
    listAgents: () => Promise.resolve(agents()),
    rediscoverProfiles: () => Promise.resolve([profile(), profile({ agent: 'codex', folder: '~/.codex' })]),

    // The setup is done, so no test meets it unless it asks for it.
    getSetup: () => Promise.resolve({ done: true, step: 'welcome' }),
    setSetupStep: (step) => Promise.resolve({ done: false, step }),
    finishSetup: () => Promise.resolve({ done: true, step: 'welcome' }),
    restartSetup: () => Promise.resolve({ done: false, step: 'welcome' }),
    checkGitHub: () => Promise.resolve(githubSignIn()),
    listTerminals: () => Promise.resolve(terminalChoices()),
    setupSignals: () => Promise.resolve(setupSignals()),
    resetApp: () => Promise.resolve({ savedTo: '~/data/backups/example before reset 20261115T120000Z.json' }),

    launch: () => Promise.resolve(launchResult()),
    resolveLaunch: () => Promise.resolve(resolution()),
    resolveLaunches: (ids) => Promise.resolve((ids ?? []).map((itemId) => itemResolution({ itemId }))),
    previewLaunch: () => Promise.resolve(launchPreview()),
    cloneTarget: () => Promise.resolve(cloneTarget()),
    cloneRepo: () => Promise.resolve(cloneTarget()),
    linkClone: () => Promise.resolve(linkedClone()),
    resumeSession: () => Promise.resolve(launchResult({ worktreeAction: 'resume' })),
    takeOverRun: () => Promise.resolve(launchResult({ worktreeAction: 'resume' })),

    listSessions: () => Promise.resolve(sessionsSnapshot()),
    focusSession: done,
    showSessionFolder: done,
    forgetSession: done,
    sessionLastWords: () => Promise.resolve({ text: 'The change looks good.', partial: false }),
    sessionUsage: () =>
      Promise.resolve({
        known: false,
        fromRun: false,
        input: 0,
        output: 0,
        cacheRead: 0,
        cacheWrite: 0,
        total: 0,
        partial: false,
      }),

    openReview: () => Promise.resolve(review()),
    refreshReview: () => Promise.resolve(review()),
    readReviewAgain: () => Promise.resolve(review()),
    rebuildReview: () => Promise.resolve(review()),
    reviewPatches: (_id, _head, paths) =>
      Promise.resolve((paths ?? []).map((path) => ({ path, patch: '@@ -1 +1 @@\n-a\n+b', patchState: '' as const }))),
    reviewEarlier: () => Promise.resolve({ items: [], total: 0, before: '' }),
    actOnReview: () => Promise.resolve(reviewResult()),
    reviewerCandidates: () => Promise.resolve(reviewerCandidates()),
    learnReviewers: () => Promise.resolve(reviewerCandidates()),
    editReviewers: () => Promise.resolve({ sent: 1, failed: '' }),
    reviewAssigneeCandidates: () => Promise.resolve(assigneeCandidates()),
    learnReviewAssignees: () => Promise.resolve(assigneeCandidates()),
    searchReviewAssignees: () => Promise.resolve(assigneeCandidates()),
    editAssignees: () => Promise.resolve({ sent: 1, failed: '' }),
    peopleSummary: () => Promise.resolve({ people: 12, repos: 3 }),
    forgetPeople: () => Promise.resolve({ people: 0, repos: 0 }),
    openIssue: (id) => Promise.resolve(issue({ id })),
    refreshIssue: (id) => Promise.resolve(issue({ id })),
    readIssueAgain: (id) => Promise.resolve(issue({ id })),
    issueEarlier: () => Promise.resolve(issueTimeline({ items: [], before: '' })),
    actOnIssue: () => Promise.resolve(issueResult()),
    issueReplies: () => Promise.resolve(issueReplies()),
    fillIssueReply: () => Promise.resolve('Looking into it, @octo-user.'),
    issueLabels: () => Promise.resolve(repoLabels()),
    issueAssigneeCandidates: () => Promise.resolve(assigneeCandidates()),
    learnIssueAssignees: () => Promise.resolve(assigneeCandidates()),
    searchIssueAssignees: () => Promise.resolve(assigneeCandidates()),
    issueAvatars: () => Promise.resolve({}),
    cardPictures: () => Promise.resolve({}),
    editIssue: () => Promise.resolve({ sent: 1, failed: '' }),
    reviewReplies: () => Promise.resolve(replyList()),
    fillReply: () => Promise.resolve('Reviewed and approved. Thanks, @octo-other!'),

    listPrompts: () => Promise.resolve(prompts()),
    createPrompt: (input) => Promise.resolve(prompt({ ...fromInput(input), id: 9 })),
    updatePrompt: (id, input) => Promise.resolve(prompt({ ...fromInput(input), id })),
    deletePrompt: done,
    duplicatePrompt: (id) => Promise.resolve(prompt({ id })),
    resetPrompt: (id) => Promise.resolve(prompt({ id })),
    checkPrompt: () => Promise.resolve(checkResult()),
    listPromptVariables: () => Promise.resolve(variables()),
    getDefaultPrompts: () => Promise.resolve(defaultPrompts()),
    setDefaultPrompt: done,
    setReplyDefault: done,
    setSlotDefault: done,
    exportPrompts: (_path, ids) =>
      Promise.resolve({ prompts: ids === null || ids.length === 0 ? prompts().length : ids.length, replies: 0 }),
    listPromptBackups: () => Promise.resolve([promptBackup()]),
    previewPromptImport: () => Promise.resolve(importPreview()),
    importPrompts: (chosen) =>
      Promise.resolve({ added: chosen?.length ?? 0, replaced: 0, skipped: 0, defaults: 0, first: 9 }),

    BackupMode: { ModeMerge: 'combine', ModeReplace: 'replace' },
    exportBackup: (path, sections) =>
      Promise.resolve({ path, sections: (sections ?? []).map((section) => ({ section, items: 3 })) }),
    previewBackup: (path) => Promise.resolve(backupPreview({ path })),
    importBackup: (req) => Promise.resolve(backupImported(req.sections ?? [])),
    chooseFileToOpen: () => Promise.resolve(''),
    chooseFolder: () => Promise.resolve(''),
    chooseFileToSave: () => Promise.resolve(''),

    listLaunchDefaults: () => Promise.resolve([]),
    checkLaunchDefaults: () => Promise.resolve([]),
    saveLaunchDefaults: () => Promise.resolve(launchDefaults()),
    removeLaunchDefaults: done,
    listLaunchTargets: () => Promise.resolve([]),

    getSettings: () => stored(),
    setTerminal: (terminal) => stored({ terminal }),
    setCloneRoot: (cloneRoot) => stored({ cloneRoot }),
    addCloneLocations: (paths) =>
      stored({ cloneLocations: (paths ?? []).map((path) => ({ path, kind: 'folder' as const })) }),
    removeCloneLocation: () => stored(),
    previewCloneLocation: (path) => Promise.resolve(locationPreview({ path })),
    checkCloneLocations: () => Promise.resolve([locationState()]),
    suggestCloneRoots: () => Promise.resolve([]),
    createCloneRoot: (cloneRoot) => stored({ cloneRoot }),
    setDefaultAgent: (defaultAgent) => stored({ defaultAgent }),
    setDefaultProfile: (agent, name) => stored({ defaultProfiles: { ...settings().defaultProfiles, [agent]: name } }),
    setWorktreeDefault: (worktreeDefault) => stored({ worktreeDefault }),
    setWorktreeAutoCleanup: (worktreeAutoCleanup) => stored({ worktreeAutoCleanup }),
    setFollowSessions: (followSessions) => stored({ followSessions }),
    setCodexEffort: (codexEffort) => stored({ codexEffort }),
    setCodexFast: (codexFast) => stored({ codexFast }),
    setClaudeAPIKey: (claudeApiKey) => stored({ claudeApiKey }),
    setCodexAPIKey: (codexApiKey) => stored({ codexApiKey }),
    setMenuBar: (menuBar) => stored({ menuBar }),
    setNotificationBanner: (kind, on) => stored({ bannersOff: on ? [] : [kind] }),
    setSnoozeDays: (snoozeDays) => stored({ snoozeDays }),
    setSnoozeWake: (snoozeWake) => stored({ snoozeWake }),
    setTrustedBots: (trustedBots) => stored({ trustedBots: trustedBots ?? [] }),
    resetTrustedBots: () => stored(),
    setSensitiveScope: (scope) => stored(sensitiveFiles({ scope })),
    setSensitiveCategory: (id, on) => stored(sensitiveFiles({ off: on ? [] : [id] })),
    setSensitivePatterns: (patterns) => stored(sensitiveFiles({ patterns: patterns ?? [] })),
    matchSensitivePath: () => Promise.resolve({ sensitive: false, category: '', label: '', level: '', pattern: '' }),
    getTheme: () => Promise.resolve('system'),
    setTheme: (theme) => stored({ theme }),
    setTextSize: (textSize) => stored({ textSize }),
    setCelebrations: (celebrations) => stored({ celebrations }),
    setToastSeconds: (toastSeconds) => stored({ toastSeconds }),
    getCollapsedSections: () => Promise.resolve([]),
    setSectionCollapsed: (section, collapsed) => stored({ collapsedSections: collapsed ? [section] : [] }),
    setProjectsTipDismissed: (projectsTipDismissed) => stored({ projectsTipDismissed }),
    setReviewTab: (reviewTab) => stored({ reviewTab }),
    setNotificationsTab: (notificationsTab) => stored({ notificationsTab }),
    setConversationOrder: (conversationOrder) => stored({ conversationOrder }),
    getInboxOrder: () => Promise.resolve({ statusSorted: [], statusOrder: [], kinds: {} }),
    setStatusSort: (section, on) =>
      stored({ inboxOrder: { statusSorted: on ? [section] : [], statusOrder: [], kinds: {} } }),
    setListKind: (section, kind) =>
      stored({ inboxOrder: { statusSorted: [], statusOrder: [], kinds: kind === 'all' ? {} : { [section]: kind } } }),
    setStatusOrder: (statusOrder) =>
      stored({ inboxOrder: { statusSorted: [], statusOrder: statusOrder ?? [], kinds: {} } }),
    setHiddenLabels: (hiddenLabels) => stored({ hiddenLabels: hiddenLabels ?? [] }),
    setCardAvatars: (cardAvatars) => stored({ cardAvatars }),
    setCardDensity: (cardDensity) => stored({ cardDensity }),
    setCardUnread: (cardUnread) => stored({ cardUnread }),
    setIgnoredWorkflows: (ignoredWorkflows) => stored({ ignoredWorkflows: ignoredWorkflows ?? [] }),
    setWindowSize: (windowSize) => stored({ windowSize }),
    setBranchRules: (branches) => stored({ branches: { ...branches, protected: branches.protected ?? [] } }),
    setRepoRule: (target, rule) => stored({ repoRules: withRepoRule(settings().repoRules, target, rule) }),
    setMergeMethod: (mergeMethod) => stored({ mergeMethod }),
    setBypassOwn: (bypassOwn) => stored({ bypassOwn }),
    setNudgeAfterHours: (nudgeAfterHours) => stored({ nudgeAfterHours }),
    setNudgeSnooze: (nudgeSnooze) => stored({ nudgeSnooze }),
    setIntegrationEnabled: (id, state) =>
      stored({ integrations: integrationsConfig({ switches: { ...integrationsConfig().switches, [id]: state } }) }),
    setIntegrationPath: (id, path) =>
      stored({ integrations: integrationsConfig({ paths: { ...integrationsConfig().paths, [id]: path } }) }),
    setRunsIn: (runsIn) => stored({ integrations: integrationsConfig({ runsIn }) }),
    setOfferAfterMerge: (offerAfterMerge) => stored({ integrations: integrationsConfig({ offerAfterMerge }) }),
    setScriptSources: (magexTargets, repoFiles) =>
      stored({ integrations: integrationsConfig({ scriptSources: { magexTargets, repoFiles } }) }),
    setIntegrationsAnnounced: (announced) => stored({ integrations: integrationsConfig({ announced }) }),
    setScriptsLastPage: (lastPage) => stored({ integrations: integrationsConfig({ lastPage }) }),
    setScriptsLastRepo: (lastRepo) => stored({ integrations: integrationsConfig({ lastRepo }) }),
    setScriptsSectionOpen: (section, open) =>
      stored({ integrations: integrationsConfig({ sections: { [section]: open } }) }),
    setShowRuns: (showRuns) => stored({ integrations: integrationsConfig({ showRuns }) }),
    setReleaseSort: (releaseSort) => stored({ integrations: integrationsConfig({ releaseSort }) }),
    setRefreshInterval: (kind, seconds) => stored({ refresh: { ...settings().refresh, [kind]: seconds } }),
    setWatchPushes: (watchPushes) => stored({ watchPushes }),
    setStatusProvider: (id, on) =>
      stored({
        serviceStatus: { watch: statusWatch().watch.map((w) => (w.provider === id ? { ...w, on } : w)) },
      }),
    setStatusPart: (id, on) =>
      stored({
        serviceStatus: {
          watch: statusWatch().watch.map((w) =>
            id.startsWith(`${w.provider}.`)
              ? { ...w, parts: on ? [...w.parts.filter((p) => p !== id), id] : w.parts.filter((p) => p !== id) }
              : w,
          ),
        },
      }),
    setGitHubNotificationSwitch: (name, on) => stored(threadRules({ [name]: on })),
    setGitHubNotificationTrigger: (trigger, on) => stored(threadRules({ triggersOff: on ? [] : [trigger] })),
    setGitHubNotificationAction: (action) => stored(threadRules({ action })),
    setGitHubCleanupCardDismissed: (cleanupCardDismissed) => stored(threadRules({ cleanupCardDismissed })),

    githubThreadsSnapshot: () => Promise.resolve(githubThreads()),
    githubThreadsList: () => Promise.resolve(githubThreadList()),
    checkGitHubThreadsNow: () => Promise.resolve(githubThreads()),
    previewGitHubThreads: () => Promise.resolve(threadsPreview()),
    // A clear by hand is queued to go at once; Undo takes a queued one back, which leaves
    // none queued.
    clearGitHubThreads: (threadIDs) =>
      Promise.resolve(
        githubThreads({
          queued: threadIDs.map((threadId, i) =>
            queuedClear({ id: i + 1, threadId, trigger: 'by_hand', sendAfter: '2026-06-15T12:00:00Z' }),
          ),
        }),
      ),
    clearGitHubItemThreads: (owner, name, number) =>
      Promise.resolve(
        githubThreads({
          queued: [queuedClear({ owner, name, number, trigger: 'by_hand', sendAfter: '2026-06-15T12:00:00Z' })],
        }),
      ),
    keepGitHubThreads: () => Promise.resolve(githubThreads()),
    markGitHubThreadsDone: () => Promise.resolve(githubThreads()),
    markGitHubThreadsRead: () => Promise.resolve(githubThreads()),

    listIntegrations: () => Promise.resolve(integrationTools()),
    checkIntegrationsAgain: () => Promise.resolve(integrationTools()),
    installIntegration: () => Promise.resolve(installResult()),
    updateIntegration: () => Promise.resolve(installResult({ message: 'Updated magex to 1.30.0' })),
    reinstallIntegration: () => Promise.resolve(installResult({ message: 'Reinstalled mage-x 1.29.5' })),
    trashToolCopy: () => Promise.resolve(integrationTools()),
    revealToolCopy: done,

    listScriptRuns: () => Promise.resolve([scriptRun()]),
    getScriptRun: (id) => Promise.resolve(scriptRun({ id })),
    scriptRunOutput: () => Promise.resolve(scriptRunChunk()),
    stopScriptRun: done,
    runScriptAgain: (id) => Promise.resolve(id + 1),
    openScriptRunInTerminal: done,
    showScriptRunLog: done,
    markScriptRunSeen: done,
    clearScriptRunHistory: done,
    quitAnyway: done,
    letThemFinish: done,
    keepRunning: done,

    listReleaseCandidates: () => Promise.resolve(releaseCandidates()),
    prepareRelease: () => Promise.resolve(releaseOffer()),
    startRelease: () => Promise.resolve(7),
    getReleaseResult: () => Promise.resolve(releaseResult()),
    offerReleaseAfterMerge: () => Promise.resolve(afterMerge({ offer: false, repo: '', dir: '', branch: '' })),
    openReleaseTerminal: done,

    listScriptRepos: () => Promise.resolve(scriptClones()),
    listRepoScripts: (dir) => Promise.resolve(repoScripts({ dir })),
    runScript: () => Promise.resolve({ started: true, id: 31 }),
    listScriptRepoNames: () => Promise.resolve(['acme-org/sdk', 'acme-org/web']),
    saveUserScript: (script) => Promise.resolve(userScript({ ...script, id: script.id === 0 ? 41 : script.id })),
    deleteUserScript: done,
    duplicateUserScript: (id) => Promise.resolve(userScript({ id: id + 40, name: 'Fix all copy' })),
    moveUserScript: done,
    getBranchStatus: (dir) => Promise.resolve(branchStatus({ dir })),
    pullBranch: () => Promise.resolve(51),

    listHooks: () => Promise.resolve(hooksList()),
    installHook: () => Promise.resolve(11),
    updateHook: () => Promise.resolve(11),
    reinstallHook: () => Promise.resolve(11),
    replaceHook: () => Promise.resolve(11),
    uninstallHook: () => Promise.resolve(11),
    runChecks: () => Promise.resolve(21),
    installAllHooks: () => Promise.resolve(hookInstallAll()),
    getHookActionResult: () => Promise.resolve(hookActionResult()),
    getChecksResult: () => Promise.resolve(hookChecksResult()),
    openHooksTerminal: done,
    showHooksConfig: done,

    listWorktrees: () => Promise.resolve(worktreesSnapshot()),
    worktreeDetails: () => Promise.resolve(worktreeDetails()),
    openWorktreeTerminal: done,
    revealWorktree: done,
    removeWorktree: () => Promise.resolve(removeResult()),
    forceRemoveWorktree: () => Promise.resolve(removeResult()),
    removeAllCleanWorktrees: () => Promise.resolve([]),
    pruneWorktrees: done,
    unlockWorktree: done,
    cleanUpWorktree: () => Promise.resolve(removeResult()),
    cleanUpFinishedWorktrees: () => Promise.resolve([]),
    mergedBranches: () => Promise.resolve([]),
    deleteBranches: () => Promise.resolve([]),
    sweepWorktrees: () => Promise.resolve(sweepReport()),
    lastWorktreeSweep: () => Promise.resolve(sweepReport({ seq: 0 })),
    recentCleanups: () => Promise.resolve([]),
    clearCleanups: done,

    // A release build, up to date: no banner shows unless a test asks for one. Every
    // action answers with the status after it, as the update's service does.
    getUpdateStatus: () => Promise.resolve(updateStatus()),
    checkForUpdates: () => Promise.resolve(updateStatus()),
    downloadUpdate: () => Promise.resolve({ downloaded: true, status: updateReady() }),
    hideUpdateBanner: () => Promise.resolve(updateReady({ bannerHidden: true })),
    showUpdateBanner: () => Promise.resolve(updateReady()),
    ackUpdated: () => Promise.resolve(updateStatus()),
    showInstallLog: done,
    restartToUpdateNow: () => Promise.resolve({ restarted: true, status: updateReady({ state: 'installing' }) }),
    setUpdateMode: (updateMode) => stored({ updateMode }),
    setAutopilotEnabled: (enabled) => stored(autopilotSettings({ enabled })),
    setAutopilotPaused: (paused) => stored(autopilotSettings({ paused })),
    setAutopilotDefaultMode: (defaultMode) => stored(autopilotSettings({ defaultMode })),
    setAutopilotAgentsAtOnce: (agentsAtOnce) => stored(autopilotSettings({ agentsAtOnce })),
    setAutopilotRunsPerDay: (runsPerDay) => stored(autopilotSettings({ runsPerDay })),
    setAutopilotActionsPerDay: (actionsPerDay) => stored(autopilotSettings({ actionsPerDay })),
    setAutopilotDailyCap: (dailyCap) => stored(autopilotSettings({ dailyCap })),
    setAutopilotKeepAwake: (keepAwake) => stored(autopilotSettings({ keepAwake })),
    setAutopilotHoldOnOutage: (holdOnOutage) => stored(autopilotSettings({ holdOnOutage })),
    setAutopilotCatchUpHours: (catchUpHours) => stored(autopilotSettings({ catchUpHours })),
    setAutopilotHistoryDays: (historyDays) =>
      stored(autopilotSettings({ historyDays, logDays: Math.min(historyDays, settings().autopilot.logDays) })),
    setAutopilotLogDays: (logDays) => stored(autopilotSettings({ logDays })),
    // A move goes ahead: the app would quit, and the copy open from Applications.
    moveToApplications: () => Promise.resolve(moveResult()),
    openInApplications: () => Promise.resolve(moveResult({ outcome: 'opening' })),
    showAppInFinder: done,

    listNotifications: () => Promise.resolve({ items: [], unread: 0 }),
    markAllNotificationsRead: done,
    markNotificationsRead: done,
    markNotificationsUnread: done,
    clearNotifications: done,
    notificationBannerStatus: () => Promise.resolve('on'),
    sendTestNotification: done,

    openExternal: () => undefined,
    copyText: done,
    on: subscribe,
    // No inspector, as in a release: a test of its part of Settings gives one.
    inspector: () => null,
  }
}

// createPlatform returns a fresh fake platform, with emit to send its window events and
// listening to count who listens for one.
export function createPlatform() {
  const listeners = new Map<WindowEvent, Set<Listener>>()

  const subscribe: Loose<Platform['on']> = (event, listener) => {
    const set = listeners.get(event) ?? new Set<Listener>()
    // The set holds every event's listeners; emit hands each the data of its own event.
    const held = listener as Listener

    set.add(held)
    listeners.set(event, set)

    return () => {
      set.delete(held)
    }
  }

  // Each function is a mock whose default, which mockReset returns it to, is its answer;
  // a value stays as it is. Object.fromEntries keys them by string, so the cast goes
  // through unknown: comparing its union of every answer's type with FakePlatform is past
  // what TypeScript compares.
  const platform = Object.fromEntries(
    Object.entries(answers(subscribe)).map(([name, answer]) => [
      name,
      typeof answer === 'function' ? vi.fn(answer as (...args: unknown[]) => unknown) : answer,
    ]),
  ) as unknown as FakePlatform

  // emit sends an event as the shell would: every listener subscribed for it now gets its
  // data, which an event that carries nothing may leave out. It fails the test when
  // nothing listens for event, so a test cannot send an event into the void.
  function emit<E extends WindowEvent>(
    event: E,
    ...data: [keyof EventData<E>] extends [never] ? [data?: EventData<E>] : [data: EventData<E>]
  ): void {
    const subscribed = [...(listeners.get(event) ?? [])]

    if (subscribed.length === 0) {
      throw new Error(`nothing listens for ${event}`)
    }

    for (const listener of subscribed) {
      listener(data[0] ?? {})
    }
  }

  // listening counts the listeners subscribed for event now.
  function listening(event: WindowEvent): number {
    return listeners.get(event)?.size ?? 0
  }

  return { platform, emit, listening }
}

// The test file's fake, the one its vi.mock line installs.
export const { platform, emit, listening } = createPlatform()

// mocks lists the fake's functions by name, for a test that reads every call made, leaving
// out the values the platform hands out as they are.
export function mocks(): [string, Mock][] {
  return Object.entries(platform).flatMap(([name, value]: [string, unknown]) =>
    vi.isMockFunction(value) ? [[name, value] as [string, Mock]] : [],
  )
}

export interface RenderAppOptions {
  // now is the time the window's clock stands at, or a function it reads the time from;
  // FIXED_NOW by default.
  now?: Date | (() => Date)
  // wrapper wraps the app inside the clock, such as a Profiler.
  wrapper?: ComponentType<{ children: ReactNode }>
}

// renderApp renders the whole window, App, over the fake platform and a clock of its own.
// App is imported on the first render rather than with this module: the platform mock's
// factory imports this module, and App imports the platform.
export async function renderApp({ now = FIXED_NOW, wrapper: Inner }: RenderAppOptions = {}) {
  const { App } = await import('../App')
  const Clock = withClock(clockAt(now))

  function AppWrapper({ children }: { children: ReactNode }) {
    return <Clock>{Inner === undefined ? children : <Inner>{children}</Inner>}</Clock>
  }

  return render(<App />, { wrapper: AppWrapper })
}
