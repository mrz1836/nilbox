// The platform module is the frontend's only door to the desktop shell: the Wails runtime
// and the generated bindings for the Go services. Everything else in src/ imports from
// here (an ESLint rule enforces it), so another shell or an OS port replaces this module's
// implementation and never its callers.

import { Browser, Clipboard, Dialogs, Events, Window, type CancellablePromise } from '@wailsio/runtime'

// The generated event bindings register the events' data with the runtime, and declare
// each event's name and payload type for Events.On.
import '../../bindings/events'
import type * as agent from '../../bindings/github.com/mrz1836/nilbox/internal/agent'
import * as appinfo from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
import * as branchrules from '../../bindings/github.com/mrz1836/nilbox/internal/branchrules'
import * as budget from '../../bindings/github.com/mrz1836/nilbox/internal/budget'
import type * as gitread from '../../bindings/github.com/mrz1836/nilbox/internal/gitread'
import * as hooks from '../../bindings/github.com/mrz1836/nilbox/internal/hooks'
import * as inbox from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import type * as integrationprefs from '../../bindings/github.com/mrz1836/nilbox/internal/integrationprefs'
import * as integrations from '../../bindings/github.com/mrz1836/nilbox/internal/integrations'
import type * as item from '../../bindings/github.com/mrz1836/nilbox/internal/item'
import * as launchdefaults from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import * as launcher from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type * as terminal from '../../bindings/github.com/mrz1836/nilbox/internal/launcher/terminal'
import * as notify from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
import type * as notifyprefs from '../../bindings/github.com/mrz1836/nilbox/internal/notifyprefs'
import * as profiles from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import type * as bump from '../../bindings/github.com/mrz1836/nilbox/internal/bump'
import * as prompts from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
import * as release from '../../bindings/github.com/mrz1836/nilbox/internal/release'
import * as review from '../../bindings/github.com/mrz1836/nilbox/internal/review'
import * as scriptrun from '../../bindings/github.com/mrz1836/nilbox/internal/scriptrun'
import * as scripts from '../../bindings/github.com/mrz1836/nilbox/internal/scripts'
import type * as sensitive from '../../bindings/github.com/mrz1836/nilbox/internal/sensitive'
import * as sessions from '../../bindings/github.com/mrz1836/nilbox/internal/sessions'
import * as settings from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
import type * as snooze from '../../bindings/github.com/mrz1836/nilbox/internal/snooze'
import * as worktrees from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'

export type { CancellablePromise }

// NonNullLists is T with every list and map in it, at any depth, typed as present. The
// generated bindings type a Go slice or map as `T[] | null`, but Go sends every one the
// window reads as [] or {}, never null (internal/core/bound_test.go holds every bound
// method to it), so the types are narrowed once, here, and callers need no fallback. A
// value that is null for a reason, such as an optional struct or a choice left unset,
// keeps its null.
type NonNullLists<T> = null extends T
  ? Exclude<T, null> extends readonly unknown[] | Record<string, unknown>
    ? Present<Exclude<T, null>>
    : Present<Exclude<T, null>> | null
  : Present<T>

// Present applies NonNullLists to a value's elements or fields.
type Present<T> = T extends readonly (infer E)[]
  ? NonNullLists<E>[]
  : T extends object
    ? { [K in keyof T]: NonNullLists<T[K]> }
    : T

// Narrowed is a generated service with NonNullLists applied to every call's result. A call
// still returns Wails' CancellablePromise, so a caller that loses interest cancels it.
type Narrowed<S> = {
  [K in keyof S]: S[K] extends (...args: infer A) => CancellablePromise<infer R>
    ? (...args: A) => CancellablePromise<NonNullLists<R>>
    : S[K]
}

// narrow returns a generated service as it is, typed as Narrowed. Nothing is checked at
// run time: the guarantee is Go's.
function narrow<S>(service: S): Narrowed<S> {
  return service as Narrowed<S>
}

// The bound services' data types, generated from the Go structs and narrowed, and the
// closed sets of values their fields take, generated from the Go constants as unions of
// string literals. Callers use these and never declare their own copies.

export type Agent = NonNullLists<agent.Agent>
export type AgentID = agent.ID
export type AgentInfo = NonNullLists<profiles.AgentInfo>
export type AgentVersion = NonNullLists<appinfo.AgentVersion>
export type ToolVersion = NonNullLists<appinfo.ToolVersion>
export type Choice = NonNullLists<agent.Choice>
// PerAgent is a setting each agent has its own value of, as launch defaults and prompts
// store their profiles, models, and extra flags: an agent with no entry inherits.
export type PerAgent<T> = Partial<Record<Exclude<AgentID, ''>, T>>

export type BudgetGauge = NonNullLists<budget.Gauge>
export type BudgetReading = NonNullLists<budget.Reading>
export type BudgetRefresh = NonNullLists<budget.Refresh>
export type BudgetWrites = NonNullLists<budget.Writes>

export type DataReset = NonNullLists<appinfo.DataReset>
export type Info = NonNullLists<appinfo.Info>
export type Restored = NonNullLists<appinfo.Restored>

export type Account = NonNullLists<inbox.Account>
export type Checks = NonNullLists<inbox.Checks>
export type ItemDetails = NonNullLists<inbox.Details>
export type InboxItem = NonNullLists<inbox.Item>
export type Label = NonNullLists<inbox.Label>
export type InboxList = NonNullLists<inbox.List>
export type Project = NonNullLists<inbox.Project>
export type ProjectField = NonNullLists<inbox.ProjectField>
export type SubIssues = NonNullLists<inbox.SubIssues>
export type ProjectAccess = inbox.ProjectAccess
export type Rate = NonNullLists<inbox.Rate>
export type RateGauge = NonNullLists<inbox.RateGauge>
export type Reviews = NonNullLists<inbox.Reviews>
export type ReviewRequest = inbox.ReviewRequest
export type InboxSnapshot = NonNullLists<inbox.Snapshot>

export type IntegrationID = integrationprefs.ID
export type IntegrationSwitch = integrationprefs.Switch
export type IntegrationsConfig = NonNullLists<integrationprefs.Config>
export type IntegrationTools = NonNullLists<integrations.Tools>
export type IntegrationTool = NonNullLists<integrations.Tool>
export type ToolCopy = NonNullLists<integrations.Copy>
export type ToolSource = integrations.Source
export type ToolState = integrations.State
export type InstallPlan = NonNullLists<integrations.InstallPlan>
export type InstallProgress = NonNullLists<integrations.InstallProgress>
export type InstallResult = NonNullLists<integrations.InstallResult>
export type RunsIn = integrationprefs.RunsIn
export type ScriptRun = NonNullLists<scriptrun.Run>
export type ScriptRunKind = scriptrun.Kind
export type ScriptRunStatus = scriptrun.Status
export type ScriptRunResult = NonNullLists<scriptrun.Result>
export type ScriptRunFilter = NonNullLists<scriptrun.Filter>
export type ScriptRunChunk = NonNullLists<scriptrun.Chunk>
export type ScriptRunProgress = NonNullLists<scriptrun.Progress>
export type ScriptSources = NonNullLists<integrationprefs.ScriptSources>
export type ScriptsPage = integrationprefs.ScriptsPage

// UserScript is one of the user's own scripts, as saved: its scope, which clones it shows
// in, and its icon, each a closed set, and its place, where runs go, or '' for where the
// setting sends them ("As set in Settings"), which the generated type, a place, leaves
// out. The page's types carry it as they carry the user's scripts.
export type UserScript = Omit<NonNullLists<scripts.UserScript>, 'runsIn'> & { runsIn: RunsIn | '' }
export type UserScriptScope = scripts.Scope
export type UserScriptIcon = scripts.Icon
export type UserScriptMove = scripts.Direction
export type ScriptEntry = Omit<NonNullLists<scripts.Entry>, 'mine'> & { mine: UserScript | null }
export type ScriptSection = Omit<NonNullLists<scripts.Section>, 'entries'> & { entries: ScriptEntry[] }
export type RepoScripts = Omit<NonNullLists<scripts.Page>, 'sections'> & { sections: ScriptSection[] }
export type ScriptClone = NonNullLists<scripts.Clone>
export type BranchStatus = NonNullLists<scripts.BranchStatus>
export type BranchState = scripts.BranchState
export type ScriptInput = NonNullLists<scripts.Input>
export type ScriptOption = NonNullLists<scripts.Option>
export type ScriptSource = scripts.Source
export type ScriptTrust = scripts.Trust
// ScriptRunRequest's runsIn is '' for the setting, which the generated type, a place,
// leaves out.
export type ScriptRunRequest = Omit<NonNullLists<scripts.RunRequest>, 'runsIn'> & { runsIn: RunsIn | '' }

// ScriptQuestion is what the window asks before a command from a repository's own files
// runs (Go's scripts.Question): the script, the command as it will run, with the chosen
// inputs filled in, why it asks (ask, or changed, with the command trusted before), what
// the question adds, such as that a magex target runs the repository's own code, and the
// hash the user confirms. It comes as the detail of the error a run is refused with, not
// as a bound result, so nothing generates it: its fields are the Go JSON names.
export interface ScriptQuestion {
  ref: string
  name: string
  source: ScriptSource
  command: string
  trust: ScriptTrust
  trusted: string
  trustNote: string
  sha256: string
}

// ScriptStart is how asking to run a script ended: started, with its run's ID, or held
// back until the user answers its question, with the backend's words for why.
export type ScriptStart = { started: true; id: number } | { started: false; question: ScriptQuestion; message: string }

export type ReleaseCandidates = NonNullLists<release.Candidates>
export type ReleaseCandidate = NonNullLists<release.Candidate>
export type ReleaseCloneState = NonNullLists<release.CloneState>
export type ReleaseCommit = NonNullLists<release.Commit>
export type ReleaseGroup = release.Group
export type CommitChecks = release.Checks
export type ReleaseOffer = NonNullLists<release.Offer>
export type ReleaseCheck = NonNullLists<release.Check>
export type ReleaseCheckID = release.CheckID
export type ReleaseCheckState = release.CheckState
export type ReleaseFix = release.Fix
export type ReleaseBump = release.Bump
export type ReleaseBumpOffer = NonNullLists<release.BumpOffer>
export type ReleaseMajorOffer = NonNullLists<release.MajorOffer>
export type ReleaseBranchChoice = NonNullLists<release.BranchChoice>
export type ReleasePrepareRequest = NonNullLists<release.PrepareRequest>
export type ReleaseStartRequest = NonNullLists<release.StartRequest>
export type ReleaseResult = NonNullLists<release.Result>
export type ReleaseResultKind = release.ResultKind
export type ReleaseFailure = NonNullLists<release.Failure>
export type ReleaseFailureKind = release.FailureKind
export type AfterMerge = NonNullLists<release.AfterMerge>
// ReleasesProgress is how far a read of the Release page's list has got, as the
// "releases:progress" event says.
export type ReleasesProgress = Events.CustomEvents['releases:progress']
export type ReleasesPhase = ReleasesProgress['phase']
export type GitOperation = gitread.Operation

export type HooksList = Omit<NonNullLists<hooks.Page>, 'repos'> & { repos: HookRepo[] }
export type HooksTool = NonNullLists<hooks.ToolStatus>
export type HooksPathWarning = NonNullLists<hooks.PathWarning>
export type HooksToolNote = NonNullLists<hooks.ToolNote>
// HookRepo's action and HookInstallLine's are '' for none, which the generated type, an
// action, leaves out; so is a request's runsIn, for the setting.
export type HookRepo = Omit<NonNullLists<hooks.Repo>, 'action'> & { action: hooks.Action | '' }
export type HookClone = NonNullLists<hooks.Clone>
export type HookGroup = hooks.Group
export type HookState = hooks.State
export type HookAction = hooks.Action
export type HookActionOutcome = hooks.ActionOutcome
export type HookActionRequest = Omit<NonNullLists<hooks.ActionRequest>, 'runsIn'> & {
  runsIn: integrationprefs.RunsIn | ''
}
export type HookActionResult = NonNullLists<hooks.ActionResult>
export type HookCheck = hooks.Check
export type HookCheckSetting = NonNullLists<hooks.CheckSetting>
export type HookCheckState = hooks.CheckState
export type HookCheckResult = NonNullLists<hooks.CheckResult>
export type HookChangedFile = NonNullLists<hooks.ChangedFile>
export type HookLastRun = NonNullLists<hooks.LastRun>
export type HookChecksScope = hooks.Scope
// HookChecksRequest and HookChecksResult name a check only for a run of one check: Go
// sends '' for every other scope, which the generated type, a check, leaves out.
export type HookChecksRequest = Omit<NonNullLists<hooks.ChecksRequest>, 'check' | 'runsIn'> & {
  check: hooks.Check | ''
  runsIn: integrationprefs.RunsIn | ''
}
export type HookChecksResult = Omit<NonNullLists<hooks.ChecksResult>, 'check'> & { check: hooks.Check | '' }
export type HookInstallLine = Omit<NonNullLists<hooks.InstallLine>, 'action'> & { action: hooks.Action | '' }
export type HookInstallAllResult = Omit<NonNullLists<hooks.InstallAllResult>, 'lines'> & { lines: HookInstallLine[] }

export type ItemKind = item.Kind

export type LaunchDefaults = NonNullLists<launchdefaults.Defaults>
export type LaunchDefaultsInput = NonNullLists<launchdefaults.Input>
export type LaunchTarget = NonNullLists<launchdefaults.Target>

export type CloneTarget = NonNullLists<launcher.CloneTarget>
export type LaunchHold = NonNullLists<launcher.Hold>
export type ItemResolution = NonNullLists<launcher.ItemResolution>
export type LaunchRequest = NonNullLists<launcher.LaunchRequest>
export type LaunchResult = NonNullLists<launcher.LaunchResult>
export type ResumeRequest = launcher.ResumeRequest
export type LaunchPreview = NonNullLists<launcher.Preview>
export type Resolution = NonNullLists<launcher.Resolution>
export type LaunchSetting = NonNullLists<launcher.Setting>
export type LaunchSource = launcher.Source
export type WorktreeAction = launcher.WorktreeAction
export type WorktreePlan = NonNullLists<launcher.WorktreePlan>
export type TerminalID = terminal.ID

// AppNotification is an entry in the notification history. Its item's kind is '' for an
// entry about no item, such as a release reaching GitHub, which the generated type, a
// union of the item kinds alone, leaves out; NotificationLog holds such entries.
export type AppNotification = Omit<NonNullLists<notify.Notification>, 'itemKind'> & { itemKind: item.Kind | '' }
export type NotificationLog = Omit<NonNullLists<notify.Log>, 'items'> & { items: AppNotification[] }
export type BannerStatus = notify.BannerStatus
export type NotificationKind = notifyprefs.Kind

export type Profile = NonNullLists<profiles.Profile>

export type PromptCheckRequest = NonNullLists<prompts.CheckRequest>
export type PromptCheckResult = NonNullLists<prompts.CheckResult>
export type DefaultPrompts = NonNullLists<prompts.DefaultPrompts>
export type Diagnostic = NonNullLists<prompts.Diagnostic>
export type PromptInput = NonNullLists<prompts.Input>
export type Prompt = NonNullLists<prompts.Prompt>
export type PromptUsage = NonNullLists<prompts.Usage>
export type PromptVariable = NonNullLists<prompts.Variable>
export type PromptAppliesTo = prompts.AppliesTo
export type PromptPurpose = prompts.Purpose
export type PromptReplyAction = prompts.ReplyAction
export type ReplySlot = prompts.ReplySlot
export type ReplyDefaults = NonNullLists<prompts.ReplyDefaults>
export type PromptBackup = NonNullLists<prompts.Backup>
export type PromptBackupKind = prompts.BackupKind
export type BundlePrompt = NonNullLists<prompts.BundlePrompt>
export type ImportEntry = NonNullLists<prompts.ImportEntry>
export type ImportOutcome = prompts.ImportOutcome
export type ImportResult = NonNullLists<prompts.ImportResult>
export type ImportSource = NonNullLists<prompts.ImportSource>

export type Review = NonNullLists<review.Review>
export type ReviewAction = review.Action
export type ReviewActRequest = NonNullLists<review.ActRequest>
export type ReviewChange = NonNullLists<review.Change>
export type ReviewCheck = NonNullLists<review.Check>
export type ReviewEntry = NonNullLists<review.ReviewEntry>
export type ReviewFile = NonNullLists<review.File>
export type ReviewFilePatch = NonNullLists<review.FilePatch>
export type ReviewLineComment = NonNullLists<review.LineComment>
export type ReviewOffer = NonNullLists<review.Offer>
export type ReviewOutcome = review.Outcome
export type ReviewReadiness = NonNullLists<review.Readiness>
export type ReviewReply = NonNullLists<review.Reply>
export type ReviewReplyList = NonNullLists<review.ReplyList>
export type ReviewResult = NonNullLists<review.Result>
export type MergeMethod = review.MergeMethod
export type PatchState = review.PatchState
export type ReadyState = review.ReadyState
export type ReplyAction = review.ReplyAction
export type DiffSide = review.Side
export type ChangeKind = review.ChangeKind
export type DependencyUpdate = NonNullLists<bump.Update>
export type UpdateLevel = bump.Level

export type SensitiveCategory = NonNullLists<sensitive.Category>
export type FileCheck = NonNullLists<sensitive.Check>
export type SensitiveFile = NonNullLists<sensitive.Finding>
export type SensitiveGroup = NonNullLists<sensitive.Group>
export type SensitivePathMatch = NonNullLists<sensitive.PathMatch>
export type SensitiveLevel = sensitive.Level
export type SensitiveScope = sensitive.Scope

export type AgentSession = NonNullLists<sessions.Session>
export type SessionWords = NonNullLists<sessions.Words>
export type SessionsSnapshot = NonNullLists<sessions.Snapshot>
export type SessionState = sessions.State

export type Settings = NonNullLists<settings.Settings>
export type InboxOrder = NonNullLists<settings.InboxOrder>
export type WindowSize = NonNullLists<settings.WindowSize>
export type ListKey = settings.ListKey
export type Theme = settings.Theme
export type TextSize = settings.TextSize
export type MergeMethodSetting = settings.MergeMethod

export type SnoozeRules = NonNullLists<snooze.Rules>
export type WakeReasons = NonNullLists<snooze.Reasons>
export type WakeReview = snooze.ReviewOutcome

export type WorktreeCleanup = NonNullLists<worktrees.Cleanup>
export type LinkedItem = NonNullLists<worktrees.LinkedItem>
export type RemoveResult = NonNullLists<worktrees.RemoveResult>
export type WorktreeRepo = NonNullLists<worktrees.Repo>
export type WorktreesSnapshot = NonNullLists<worktrees.Snapshot>
export type SweepReport = NonNullLists<worktrees.SweepReport>
export type Worktree = NonNullLists<worktrees.Worktree>
export type WorktreeBranch = NonNullLists<worktrees.Branch>
export type BranchKind = worktrees.BranchKind
export type WorktreePick = NonNullLists<worktrees.Pick>
export type MergedBranch = NonNullLists<worktrees.MergedBranch>
export type MergedPullRequest = NonNullLists<worktrees.MergedPullRequest>
export type BranchPick = NonNullLists<worktrees.BranchPick>
export type BranchResult = NonNullLists<worktrees.BranchResult>
export type BranchRules = NonNullLists<branchrules.Rules>
export type WorktreeStage = worktrees.Stage
export type WorktreeSession = NonNullLists<worktrees.Session>

// The bound calls, under the names the app knows them by. What each one does is the Go
// method's doc comment, which the generated service carries.

export const {
  Info: getAppInfo,
  ReportFirstPaint: reportFirstPaint,
  AgentVersions: agentVersions,
  ToolVersions: toolVersions,
  BugReportURL: bugReportURL,
} = narrow(appinfo.Service)

export const { Gauge: getBudget, Check: checkBudget } = narrow(budget.Service)

export const {
  Snapshot: getInboxSnapshot,
  Refresh: refreshInbox,
  RefreshItem: refreshItem,
  Dismiss: dismissItem,
  Snooze: snoozeItem,
  Restore: restoreItem,
  Pin: pinItem,
  Unpin: unpinItem,
  Unassign: unassignItem,
  RemoveReviewRequest: removeReviewRequest,
} = narrow(inbox.Service)

export const { List: listProfiles, Agents: listAgents } = narrow(profiles.Service)

export const {
  Launch: launch,
  Resolve: resolveLaunch,
  ResolveItems: resolveLaunches,
  Preview: previewLaunch,
  CloneTarget: cloneTarget,
  Clone: cloneRepo,
  Resume: resumeSession,
} = narrow(launcher.Service)

export const { List: listSessions, Focus: focusSession, LastWords: sessionLastWords } = narrow(sessions.Service)

export const {
  Open: openReview,
  Refresh: refreshReview,
  ReadAgain: readReviewAgain,
  Patches: reviewPatches,
  Act: actOnReview,
  Replies: reviewReplies,
  FillReply: fillReply,
} = narrow(review.Service)

export const {
  List: listPrompts,
  Create: createPrompt,
  Update: updatePrompt,
  Delete: deletePrompt,
  Duplicate: duplicatePrompt,
  Reset: resetPrompt,
  Check: checkPrompt,
  Variables: listPromptVariables,
  Defaults: getDefaultPrompts,
  SetDefault: setDefaultPrompt,
  SetReplyDefault: setReplyDefault,
  Export: exportPrompts,
  Backups: listPromptBackups,
  PreviewImport: previewPromptImport,
  Import: importPrompts,
} = narrow(prompts.Service)

export const {
  List: listLaunchDefaults,
  Check: checkLaunchDefaults,
  Save: saveLaunchDefaults,
  Remove: removeLaunchDefaults,
  Targets: listLaunchTargets,
} = narrow(launchdefaults.Service)

// The settings setters store one change and return every setting as stored after it.
export const {
  Get: getSettings,
  SetTerminal: setTerminal,
  SetCloneRoot: setCloneRoot,
  SetDefaultAgent: setDefaultAgent,
  SetDefaultProfile: setDefaultProfile,
  SetWorktreeDefault: setWorktreeDefault,
  SetWorktreeAutoCleanup: setWorktreeAutoCleanup,
  SetFollowSessions: setFollowSessions,
  SetCodexEffort: setCodexEffort,
  SetCodexFast: setCodexFast,
  SetMenuBar: setMenuBar,
  SetNotificationBanner: setNotificationBanner,
  SetSnoozeDays: setSnoozeDays,
  SetSnoozeWake: setSnoozeWake,
  SetTrustedBots: setTrustedBots,
  ResetTrustedBots: resetTrustedBots,
  SetSensitiveScope: setSensitiveScope,
  SetSensitiveCategory: setSensitiveCategory,
  SetSensitivePatterns: setSensitivePatterns,
  MatchSensitivePath: matchSensitivePath,
  Theme: getTheme,
  SetTheme: setTheme,
  SetTextSize: setTextSize,
  CollapsedSections: getCollapsedSections,
  SetSectionCollapsed: setSectionCollapsed,
  SetProjectsTipDismissed: setProjectsTipDismissed,
  InboxOrder: getInboxOrder,
  SetStatusSort: setStatusSort,
  SetStatusOrder: setStatusOrder,
  SetBranchRules: setBranchRules,
  SetMergeMethod: setMergeMethod,
  SetIntegrationEnabled: setIntegrationEnabled,
  SetIntegrationPath: setIntegrationPath,
  SetRunsIn: setRunsIn,
  SetOfferAfterMerge: setOfferAfterMerge,
  SetScriptSources: setScriptSources,
  SetIntegrationsAnnounced: setIntegrationsAnnounced,
  SetScriptsLastPage: setScriptsLastPage,
  SetScriptsLastRepo: setScriptsLastRepo,
} = narrow(settings.Service)

// The tools the app finds, installs, and runs for the user. List never waits for a
// detection, and a "tools:updated" event follows each one that ends, and each step of an
// install, so the window reads the list again. An install blocks until it is done; Trash
// and Reveal take only a copy the last detection found.
export const {
  List: listIntegrations,
  CheckAgain: checkIntegrationsAgain,
  Install: installIntegration,
  Update: updateIntegration,
  Reinstall: reinstallIntegration,
  Trash: trashToolCopy,
  Reveal: revealToolCopy,
} = narrow(integrations.Service)

// The runs: releases, hooks actions, checks, and scripts, in the app or in the user's
// terminal, newest first. A "scriptruns:updated" event follows each start, end, or change
// of status, never output: the window reads a live run's output by polling Output from
// where it left off. Stop works only on a run going in the app; Run again never on a
// release. QuitAnyway answers the "app:quitRequested" question: stop the runs and quit.
export const {
  Runs: listScriptRuns,
  Run: getScriptRun,
  Output: scriptRunOutput,
  Stop: stopScriptRun,
  RunAgain: runScriptAgain,
  OpenInTerminal: openScriptRunInTerminal,
  ShowLog: showScriptRunLog,
  MarkSeen: markScriptRunSeen,
  ClearHistory: clearScriptRunHistory,
  QuitAnyway: quitAnyway,
} = narrow(scriptrun.Service)

// Releases: the repositories the Release page lists (kept however old unless refresh
// is set; "releases:progress" events say how far a read of GitHub has got, and a
// "releases:updated" event follows each new list; after a restart, the list kept from the
// last start comes at once, marked kept, while it is read again behind it), a release's sheet as Decide
// works it out, the release itself, which returns its run's ID, the result a release's
// run recorded once it was verified, and whether a merge in the review sheet offers one.
// Only Candidates, Prepare with refresh, and Prepare after its facts are two minutes old
// ask GitHub; OfferAfterMerge never does. OpenTerminal opens the terminal chosen for launches
// in a clone, for a check's fix, and runs nothing there.
export const {
  Candidates: listReleaseCandidates,
  Prepare: prepareRelease,
  Start: startRelease,
  ReleaseResult: getReleaseResult,
  OfferAfterMerge: offerReleaseAfterMerge,
  OpenTerminal: openReleaseTerminal,
} = narrow(release.Service)

// Hooks: the clones the Hooks page lists (the last scan, kept five minutes unless refresh
// is set; a "hooks:updated" event follows each scan, and each action or checks run that
// lets go of its clone), go-pre-commit's actions and checks in one clone, each returning
// its run's ID, Install and update all, which returns once every clone's run has ended,
// and the result each run recorded. OpenTerminal opens the terminal chosen for launches in
// a clone and runs nothing there; ShowConfig shows a clone's config in Finder. Nothing
// here asks GitHub.
const hooksService = narrow(hooks.Service)

// ActionCall is a hooks action as the window asks for it: where it runs may be left to the
// setting (HookActionRequest).
type ActionCall = (req: HookActionRequest) => CancellablePromise<number>

export const {
  ActionResult: getHookActionResult,
  OpenTerminal: openHooksTerminal,
  ShowConfig: showHooksConfig,
} = hooksService

// The calls whose requests or results the hooks types above widen, typed with them.
export const listHooks: (refresh: boolean) => CancellablePromise<HooksList> = hooksService.Repos
export const installHook = hooksService.Install as ActionCall
export const updateHook = hooksService.Update as ActionCall
export const reinstallHook = hooksService.Reinstall as ActionCall
export const replaceHook = hooksService.Replace as ActionCall
export const uninstallHook = hooksService.Uninstall as ActionCall
export const runChecks = hooksService.RunChecks as (req: HookChecksRequest) => CancellablePromise<number>
export const installAllHooks: (dirs: string[] | null) => CancellablePromise<HookInstallAllResult> =
  hooksService.InstallAll
export const getChecksResult: (runID: number) => CancellablePromise<HookChecksResult> = hooksService.ChecksResult

// Repo scripts: the clones the page's picker lists, the ones a script ran in most recently
// first; one clone's scripts, the user's own whose scope includes it, then its t3.json,
// its .vscode/tasks.json, and magex (refresh lists magex's built-ins again); and a run of
// one of them, which returns its run's ID. The user's own scripts are saved (a new one
// with ID 0, at the end), deleted, duplicated right after the original, and moved past the
// nearest script shown in the clone named (all of them for ''); RepoNames lists the
// clones' GitHub repositories, owner/name, which a script for chosen repositories picks
// from; cancelling it stops the reads still to start. BranchStatus says whether the
// clone's branch is behind the branch it follows on origin (origin's answer, one
// ls-remote, kept five minutes unless refresh is set; never a fetch), and Pull
// fast-forwards it, git's fast-forward-only pull, returning its run's ID; a
// "scriptruns:updated" event follows its end. Nothing here runs anything in a clone but the
// run the user asked for, and nothing asks GitHub's API.
const scriptsService = narrow(scripts.Service)

export const {
  Repos: listScriptRepos,
  RepoNames: listScriptRepoNames,
  Delete: deleteUserScript,
  Move: moveUserScript,
  BranchStatus: getBranchStatus,
  Pull: pullBranch,
} = scriptsService

// The calls whose user scripts the types above widen with '' for the setting's place.
export const listRepoScripts: (dir: string, refresh: boolean) => CancellablePromise<RepoScripts> = scriptsService.List
export const saveUserScript = scriptsService.Save as (script: UserScript) => CancellablePromise<UserScript>
export const duplicateUserScript: (id: number) => CancellablePromise<UserScript> = scriptsService.Duplicate

// questionFields are the fields every ScriptQuestion holds, each a string.
const questionFields = ['ref', 'name', 'source', 'command', 'trust', 'trusted', 'trustNote', 'sha256'] as const

// refusal reads what a failed call's cause carries beside its message: the backend's words
// for the user, and the question to ask, or null when it holds none.
function refusal(err: unknown): { question: ScriptQuestion; message: string } | null {
  if (!(err instanceof Error) || typeof err.cause !== 'object' || err.cause === null) {
    return null
  }

  const { cause } = err
  const detail: unknown = 'detail' in cause ? cause.detail : undefined

  if (typeof detail !== 'object' || detail === null) {
    return null
  }

  const fields = detail as Record<string, unknown>

  if (!questionFields.every((f) => typeof fields[f] === 'string')) {
    return null
  }

  const message = 'message' in cause && typeof cause.message === 'string' ? cause.message : ''

  return { question: detail as ScriptQuestion, message }
}

// runScript asks to run a script in its clone. It resolves with the run's ID once it
// started, or, for a command from the repository's files the user hasn't confirmed as it
// is now, with the question the refusal carries (the error's detail), for the window to
// ask; any other failure rejects.
export function runScript(req: ScriptRunRequest): CancellablePromise<ScriptStart> {
  const run = scriptsService.Run as (req: ScriptRunRequest) => CancellablePromise<number>

  return run(req).then(
    (id): ScriptStart => ({ started: true, id }),
    (err: unknown): ScriptStart => {
      const held = refusal(err)

      if (held === null) {
        throw err
      }

      return { started: false, ...held }
    },
  )
}

export const {
  List: listWorktrees,
  Remove: removeWorktree,
  ForceRemove: forceRemoveWorktree,
  RemoveAllClean: removeAllCleanWorktrees,
  Prune: pruneWorktrees,
  Unlock: unlockWorktree,
  CleanUp: cleanUpWorktree,
  CleanUpFinished: cleanUpFinishedWorktrees,
  MergedBranches: mergedBranches,
  DeleteBranches: deleteBranches,
  Sweep: sweepWorktrees,
  LastSweep: lastWorktreeSweep,
  RecentCleanups: recentCleanups,
  ClearCleanups: clearCleanups,
} = narrow(worktrees.Service)

const notifications = narrow(notify.Service)

export const {
  MarkAllRead: markNotificationsRead,
  Clear: clearNotifications,
  BannerStatus: notificationBannerStatus,
  SendTest: sendTestNotification,
} = notifications

// listNotifications reads the notification history, typed so its entries can be about no
// item (AppNotification).
export const listNotifications: () => CancellablePromise<NotificationLog> = notifications.List

// checkItemFiles checks a pull request's changed files against GitHub now, stores the
// outcome (the inbox is updated with it), and returns it. It fails for an item whose files
// are not checked.
export function checkItemFiles(id: number): CancellablePromise<FileCheck> {
  return narrow(inbox.Service)
    .CheckFiles(id)
    .then((check) => {
      if (check === null) {
        throw new Error('This item’s files are not checked.')
      }

      return check
    })
}

// setWindowSize stores the size the window opens at, then resizes the open window to it.
// A size the store refuses leaves the window as it is.
export function setWindowSize(size: WindowSize): CancellablePromise<Settings> {
  return narrow(settings.Service)
    .SetWindowSize(size)
    .then(async (stored) => {
      await Window.SetSize(size.width, size.height)

      return stored
    })
}

// openExternal opens a URL, such as a pull request, in the default browser. Nothing waits
// on it, so a failure goes to the console, as every failure no one waits on does.
export function openExternal(url: string): void {
  Browser.OpenURL(url).catch((err: unknown) => {
    console.error(`could not open ${url} in the browser`, err)
  })
}

// copyText puts text on the system clipboard. It rejects when the clipboard refuses it.
export async function copyText(text: string): Promise<void> {
  await Clipboard.SetText(text)
}

// FileChoice is what a native file dialog asks for: its title, the button that confirms
// it, the file types it offers (each a name and a pattern such as "*.json"), and, to save,
// the file name it suggests.
export interface FileChoice {
  title: string
  button: string
  filters: readonly { name: string; pattern: string }[]
  filename?: string
}

// filters is a FileChoice's file types as the runtime takes them.
function filters(choice: FileChoice) {
  return choice.filters.map((f) => ({ DisplayName: f.name, Pattern: f.pattern }))
}

// chooseFileToOpen asks the user for a file in the system's open dialog, and resolves with
// its path, or '' when they cancel.
export async function chooseFileToOpen(choice: FileChoice): Promise<string> {
  return Dialogs.OpenFile({
    Title: choice.title,
    ButtonText: choice.button,
    Filters: filters(choice),
    CanChooseFiles: true,
    CanChooseDirectories: false,
    AllowsMultipleSelection: false,
  })
}

// chooseFileToSave asks the user where to save a file in the system's save dialog, and
// resolves with the path, or '' when they cancel. The dialog itself asks before replacing a
// file that is there.
export async function chooseFileToSave(choice: FileChoice): Promise<string> {
  return Dialogs.SaveFile({
    Title: choice.title,
    ButtonText: choice.button,
    Filters: filters(choice),
    Filename: choice.filename ?? '',
    CanCreateDirectories: true,
  })
}

// WindowEvent names an event the shell sends the window, and WindowEventData is what the
// event carries. Both come from the Go event types, through the generated bindings.
type WindowEvent = keyof Events.CustomEvents
type WindowEventData<E extends WindowEvent> = NonNullLists<Events.CustomEvents[E]>

// on calls listener with the data of every event named event that the shell sends, and
// returns a function that stops listening.
export function on<E extends WindowEvent>(event: E, listener: (data: WindowEventData<E>) => void): () => void {
  return Events.On(event, (received) => {
    listener(received.data as unknown as WindowEventData<E>)
  })
}
