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
import * as autopilot from '../../bindings/github.com/mrz1836/nilbox/internal/autopilot'
import type * as autopilotcatalog from '../../bindings/github.com/mrz1836/nilbox/internal/autopilot/catalog'
import type * as autopilotspec from '../../bindings/github.com/mrz1836/nilbox/internal/autopilot/spec'
import * as autopilotprefs from '../../bindings/github.com/mrz1836/nilbox/internal/autopilotprefs'
import * as backup from '../../bindings/github.com/mrz1836/nilbox/internal/backup'
import * as branchrules from '../../bindings/github.com/mrz1836/nilbox/internal/branchrules'
import * as budget from '../../bindings/github.com/mrz1836/nilbox/internal/budget'
import type * as event from '../../bindings/github.com/mrz1836/nilbox/internal/event'
import * as ghthreads from '../../bindings/github.com/mrz1836/nilbox/internal/ghthreads'
import type * as gitread from '../../bindings/github.com/mrz1836/nilbox/internal/gitread'
import * as hooks from '../../bindings/github.com/mrz1836/nilbox/internal/hooks'
import * as inbox from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import type * as integrationprefs from '../../bindings/github.com/mrz1836/nilbox/internal/integrationprefs'
import * as integrations from '../../bindings/github.com/mrz1836/nilbox/internal/integrations'
import * as issues from '../../bindings/github.com/mrz1836/nilbox/internal/issues'
import type * as item from '../../bindings/github.com/mrz1836/nilbox/internal/item'
import * as launchdefaults from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import * as launcher from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type * as terminal from '../../bindings/github.com/mrz1836/nilbox/internal/launcher/terminal'
import type * as localrepos from '../../bindings/github.com/mrz1836/nilbox/internal/localrepos'
import * as notify from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
import type * as notifyprefs from '../../bindings/github.com/mrz1836/nilbox/internal/notifyprefs'
import * as profiles from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import type * as bump from '../../bindings/github.com/mrz1836/nilbox/internal/bump'
import * as prompts from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
import * as release from '../../bindings/github.com/mrz1836/nilbox/internal/release'
import type * as reporules from '../../bindings/github.com/mrz1836/nilbox/internal/reporules'
import * as review from '../../bindings/github.com/mrz1836/nilbox/internal/review'
import * as scriptrun from '../../bindings/github.com/mrz1836/nilbox/internal/scriptrun'
import * as scripts from '../../bindings/github.com/mrz1836/nilbox/internal/scripts'
import type * as sensitive from '../../bindings/github.com/mrz1836/nilbox/internal/sensitive'
import * as sessions from '../../bindings/github.com/mrz1836/nilbox/internal/sessions'
import * as settings from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
import * as setup from '../../bindings/github.com/mrz1836/nilbox/internal/setup'
import type * as snooze from '../../bindings/github.com/mrz1836/nilbox/internal/snooze'
import type * as statusprefs from '../../bindings/github.com/mrz1836/nilbox/internal/statusprefs'
import * as svcstatus from '../../bindings/github.com/mrz1836/nilbox/internal/svcstatus'
import type * as threadrules from '../../bindings/github.com/mrz1836/nilbox/internal/threadrules'
import type * as timeline from '../../bindings/github.com/mrz1836/nilbox/internal/timeline'
import * as update from '../../bindings/github.com/mrz1836/nilbox/internal/update'
import type * as updateprefs from '../../bindings/github.com/mrz1836/nilbox/internal/updateprefs'
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

// The user's GitHub notifications as the app mirrors them: the status of reading them, the
// threads about each pull request or issue, and the newest unread. A thread about no item
// (a release, a check suite, ...) has '' for its kind, which the generated type, a union of
// the item kinds alone, leaves out. No URL GitHub gave is in any of them: the window
// builds an item's link from its repository, kind, and number.
export type GitHubThreadsSnapshot = Omit<NonNullLists<ghthreads.Snapshot>, 'unread'> & { unread: GitHubThread[] }
export type GitHubThreadsStatus = NonNullLists<ghthreads.Status>
export type GitHubThreadsAccess = ghthreads.Access
export type GitHubThread = Omit<NonNullLists<ghthreads.Thread>, 'kind'> & { kind: item.Kind | '' }
// GitHubThreadList is every thread the mirror holds, read and unread, for the Notifications
// page. A thread's state is its item's as the last state check read it, '' until one did
// and for a thread about no item.
export type GitHubThreadList = Omit<NonNullLists<ghthreads.ThreadList>, 'threads'> & { threads: GitHubThread[] }
export type GitHubThreadState = threadrules.State
// Clearing them: the clears waiting to be sent (each of which Undo keeps), those that
// settled, and how each stands; and what clearing would do now, for the cleanup sheet.
export type QueuedClear = NonNullLists<ghthreads.QueuedClear>
export type SettledClear = NonNullLists<ghthreads.SettledClear>
export type ClearState = ghthreads.ClearState
export type ClearSkipReason = ghthreads.SkipReason
export type GitHubThreadsPreview = NonNullLists<ghthreads.Preview>
export type PreviewRepo = NonNullLists<ghthreads.PreviewRepo>
export type PreviewHeld = NonNullLists<ghthreads.PreviewHeld>
export type PreviewThread = NonNullLists<ghthreads.PreviewThread>
// The user's rules for their GitHub notifications, from the settings, and the names their
// setters take.
export type ThreadRules = NonNullLists<threadrules.Rules>
export type ThreadSwitch = threadrules.Switch
export type ThreadTrigger = threadrules.Trigger
export type ThreadAction = threadrules.Action

export type DataReset = NonNullLists<appinfo.DataReset>
export type Info = NonNullLists<appinfo.Info>
export type Restored = NonNullLists<appinfo.Restored>
export type ResetReason = appinfo.ResetReason

// The app's own updates: where the update stands (UpdateStatus, with the newer version
// found, the download's progress, the restart under way, and how the last restart went),
// what kind of build this is, where it runs from, and the setting that says what to do
// when a new version is out (Settings.updateMode).
export type UpdateStatus = NonNullLists<update.Status>
export type UpdateState = update.State
export type UpdateFlavor = update.Flavor
export type UpdateLocation = NonNullLists<update.Location>
export type UpdateLocationKind = update.LocationKind
// MoveResult is what moving the app into Applications, or opening the copy there, did:
// moved (the app quits and the copy opens), exists (a copy is there already, which Open it
// opens), or opening (the copy there opens once the app quits), and the copy's path.
export type MoveResult = NonNullLists<update.MoveResult>
export type MoveOutcome = update.MoveOutcome
export type InstallFailure = NonNullLists<update.InstallFailure>
export type UpdateMode = updateprefs.Mode

// The first-run setup: where it stands (whether it is done, and the step it opens on), the
// GitHub step's check of the GitHub CLI's sign-in, the terminals launches can open in,
// what the user's clones say about the tools the Tools step offers, and what a reset saved
// before it asked for the restart.
export type Setup = NonNullLists<setup.State>
export type SetupStep = setup.Step
export type GitHubSignIn = NonNullLists<setup.GitHub>
export type GitHubState = setup.GitHubState
export type GitHubProjects = setup.ProjectAccess
export type TerminalChoice = NonNullLists<setup.TerminalChoice>
export type SetupSignals = NonNullLists<setup.Signals>
export type ResetStarted = NonNullLists<setup.ResetStarted>

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
export type ScriptsSection = integrationprefs.ScriptsSection

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
export type ReleaseFollow = NonNullLists<release.Follow>
export type ReleaseFollowState = release.FollowState
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
export type LinkedClone = NonNullLists<launcher.LinkedClone>
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

// The user's repository locations: the clone root and the other locations, each a folder
// looked into one level deep or a single clone; how each reads now; and what adding a
// folder would add, with what the app suggests instead.
export type CloneLocation = NonNullLists<localrepos.Location>
export type LocationKind = localrepos.Kind
export type LocationState = NonNullLists<localrepos.LocationState>
export type LocationStatus = localrepos.State
export type LocationPreview = NonNullLists<localrepos.Preview>
export type LocationSuggestion = NonNullLists<localrepos.Suggestion>
// CloneCandidate is a folder the setup suggests as the clone root or another location:
// one in the home folder that holds clones, whatever its name, with ~ for home.
export type CloneCandidate = LocationSuggestion

// AppNotification is an entry in the notification history. Its item's kind is '' for an
// entry about no item, such as a release reaching GitHub, which the generated type, a
// union of the item kinds alone, leaves out; NotificationLog holds such entries.
export type AppNotification = Omit<NonNullLists<notify.Notification>, 'itemKind'> & { itemKind: item.Kind | '' }
export type NotificationLog = Omit<NonNullLists<notify.Log>, 'items'> & { items: AppNotification[] }
export type BannerStatus = notify.BannerStatus
export type NotificationKind = notifyprefs.Kind

// AutopilotTarget is what an Autopilot notification, or a row of the menu bar's Autopilot
// group, opens in the Autopilot tab; AutopilotGlance is Autopilot at a glance: the
// routines, what waits for the user, and the agents working, which the tab counts.
export type AutopilotTarget = NonNullLists<event.AutopilotTarget>
export type AutopilotGlance = NonNullLists<autopilot.Glance>

// Autopilot's routines and their history, as the Autopilot tab shows them: each routine
// (its mode, whether it is on, why it is broken), the question asked as one is turned on
// (how many items it matches now), what the routines did, a page of Activity at a time by
// day with its grouped lines of local steps, an item's own history, which its ⚡ chip
// opens, what an Undo did, and the engine as Settings → Autopilot shows it. The settings
// (Pause Autopilot, the default mode, the limits) are Settings' own (AutopilotConfig). A
// routine that loads has '' for why it is broken, an open match '' for why it closed, and
// a closed one '' for what it waits on, which the generated types, unions of the values
// alone, leave out.
export type AutopilotRoutine = Omit<NonNullLists<autopilot.Routine>, 'brokenReason'> & {
  brokenReason: autopilotspec.Reason | ''
}
export type AutopilotDraft = NonNullLists<autopilot.Draft>
export type AutopilotMode = autopilotprefs.Mode
export const AutopilotMode = autopilotprefs.Mode
export type AutopilotCurrent = autopilot.Current
export type AutopilotMatching = NonNullLists<autopilot.Matching>
export type AutopilotMatch = Omit<NonNullLists<autopilot.Match>, 'blockedBy' | 'reason'> & {
  blockedBy: autopilot.Blocker | ''
  reason: autopilot.Reason | ''
}
export type AutopilotEffect = NonNullLists<autopilot.Effect>
export type AutopilotStepKind = autopilotcatalog.StepKind
export type AutopilotState = autopilot.State
export type AutopilotReason = autopilot.Reason
export type AutopilotItemRef = NonNullLists<autopilot.ItemRef>
export type AutopilotActivityDay = Omit<NonNullLists<autopilot.ActivityDay>, 'matches'> & {
  matches: AutopilotMatch[]
}
export type AutopilotActivityPage = Omit<NonNullLists<autopilot.ActivityPage>, 'days'> & {
  days: AutopilotActivityDay[]
}
export type AutopilotGroup = NonNullLists<autopilot.ActivityGroup>
export type AutopilotLeftAlone = NonNullLists<autopilot.LeftAlone>
export type AutopilotHistory = Omit<NonNullLists<autopilot.ItemHistory>, 'matches'> & { matches: AutopilotMatch[] }
export type AutopilotUndone = NonNullLists<autopilot.Undone>
export type AutopilotCleared = NonNullLists<autopilot.Cleared>
export type AutopilotEngine = NonNullLists<autopilot.EngineStatus>
export type AutopilotCycle = NonNullLists<autopilot.Cycle>
export type AutopilotConfig = NonNullLists<autopilotprefs.Config>
export type AutopilotDailyCap = NonNullLists<autopilotprefs.DailyCap>
export type AutopilotUnit = autopilotprefs.Unit

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
export type ImportPreview = NonNullLists<prompts.ImportPreview>
export type ImportResult = NonNullLists<prompts.ImportResult>
export type ImportSource = NonNullLists<prompts.ImportSource>
export type BundleDefaults = NonNullLists<prompts.BundleDefaults>
export type DefaultOffer = NonNullLists<prompts.DefaultOffer>
export type DefaultSlot = prompts.DefaultSlot
export type PromptExport = NonNullLists<prompts.ExportResult>

// A backup of the user's whole setup, in Settings: what one export wrote, what importing
// one would do under each mode, and what it did. The mode's wire value is Go's, so the
// window names it by the generated constant's name (BackupMode.ModeMerge), never by its
// value.
export type BackupSection = backup.Section
export type BackupMode = backup.Mode
export const BackupMode = backup.Mode
export type BackupChange = backup.Change
export type BackupCount = NonNullLists<backup.Count>
export type BackupItem = NonNullLists<backup.Item>
export type BackupPlan = NonNullLists<backup.Plan>
export type BackupExported = NonNullLists<backup.Exported>
export type BackupPreview = NonNullLists<backup.Preview>
export type BackupSectionPreview = NonNullLists<backup.SectionPreview>
export type BackupRequest = NonNullLists<backup.Request>
export type BackupImported = NonNullLists<backup.Imported>
export type BackupSectionResult = NonNullLists<backup.SectionResult>

export type Review = NonNullLists<review.Review>
export type ReviewAction = review.Action
export type ReviewActRequest = NonNullLists<review.ActRequest>
export type ReviewChange = NonNullLists<review.Change>
export type ReviewClosing = NonNullLists<review.Closing>
export type ReviewCheck = NonNullLists<review.Check>
export type ReviewUnrun = NonNullLists<review.Unrun>
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

export type Issue = NonNullLists<issues.Issue>
export type IssueAction = issues.Action
export type IssueActRequest = NonNullLists<issues.ActRequest>
export type IssueFix = NonNullLists<issues.Fix>
export type IssueLabel = NonNullLists<timeline.Label>
export type IssueLinked = NonNullLists<timeline.Ref>
export type IssueMilestone = NonNullLists<issues.Milestone>
export type IssueOffer = NonNullLists<issues.Offer>
export type IssueOutcome = issues.Outcome
export type IssuePerson = NonNullLists<issues.Person>
export type IssueProject = NonNullLists<issues.Project>
export type IssueCandidate = NonNullLists<issues.Candidate>
export type IssueEditRequest = NonNullLists<issues.EditRequest>
export type IssueRepoLabel = NonNullLists<issues.RepoLabel>
export type IssueRepoLabels = NonNullLists<issues.RepoLabels>
export type IssueReply = NonNullLists<issues.Reply>
export type IssueResult = NonNullLists<issues.Result>
export type IssueSeen = NonNullLists<issues.Seen>
export type IssueStanding = NonNullLists<issues.Standing>
export type IssueStandingState = issues.StandingState
export type IssueStateReason = timeline.StateReason
export type IssueType = NonNullLists<issues.IssueType>
export type ChecksWord = issues.ChecksWord
export type RefState = timeline.RefState
export type ReviewWord = issues.ReviewWord

// The conversation both sheets show, an issue's and a pull request's alike: its entries
// (comments, reviews, and events), a page of them, what each is, the items they name, and
// a review's comments on lines of the diff.
export type TimelineEntry = NonNullLists<timeline.Entry>
export type TimelinePage = NonNullLists<timeline.Page>
export type TimelineKind = timeline.Kind
export type TimelineRef = NonNullLists<timeline.Ref>
export type TimelineLineComment = NonNullLists<timeline.LineComment>
export type TimelineReviewState = timeline.ReviewState
export type TimelineReaction = NonNullLists<timeline.Reaction>
export type TimelineLabel = NonNullLists<timeline.Label>

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
export type SessionWorktreeAction = sessions.WorktreeAction

export type Settings = NonNullLists<settings.Settings>
export type InboxOrder = NonNullLists<settings.InboxOrder>
export type WindowSize = NonNullLists<settings.WindowSize>
export type ListKey = settings.ListKey
export type ListKind = settings.ListKind
export type Theme = settings.Theme
export type TextSize = settings.TextSize
export type MergeMethodSetting = settings.MergeMethod
export type ReviewTab = settings.ReviewTab
export type NotificationsTab = settings.NotificationsTab
export type ConversationOrder = settings.ConversationOrder
export type RefreshKind = settings.RefreshKind

// The service status check: where the services the user watches stand (GitHub, Claude,
// OpenAI), each provider's parts affected and incidents that touch them; and, from the
// settings, the catalog of what can be watched and the user's choices.
export type ServiceStatus = NonNullLists<svcstatus.Status>
export type ServiceProvider = NonNullLists<svcstatus.Provider>
export type ServiceIncident = NonNullLists<svcstatus.Incident>
export type ServiceState = svcstatus.State
export type IncidentStage = svcstatus.Stage
export type StatusConfig = NonNullLists<statusprefs.Config>
export type StatusProvider = NonNullLists<statusprefs.Provider>
export type StatusPart = NonNullLists<statusprefs.Part>
export type StatusProviderID = statusprefs.ProviderID
export type StatusPartID = statusprefs.PartID

export type SnoozeRules = NonNullLists<snooze.Rules>
export type WakeReasons = NonNullLists<snooze.Reasons>
export type WakeReview = snooze.ReviewOutcome

export type WorktreeCleanup = NonNullLists<worktrees.Cleanup>
export type LinkedItem = NonNullLists<worktrees.LinkedItem>
export type RemoveResult = NonNullLists<worktrees.RemoveResult>
export type WorktreeRepo = NonNullLists<worktrees.Repo>
export type WorktreesSnapshot = NonNullLists<worktrees.Snapshot>
// WorktreesProgress is how far the window's listing of the worktrees has got, as the
// "worktrees:progress" event says.
export type WorktreesProgress = Events.CustomEvents['worktrees:progress']
export type WorktreesPhase = WorktreesProgress['phase']
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
// RepoRule is what the user set for a repository or an org, and RepoRules every one: the
// pinned and the muted, each "owner" for an org or "owner/name" for a repository.
export type RepoRule = reporules.Rule
export type RepoRules = NonNullLists<reporules.Rules>
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
  FeedbackURL: feedbackURL,
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
  // ItemsByID returns the stored items among ids, listed or not, each as a list shows it,
  // newest activity first: the rows of a routine's Watching list.
  ItemsByID: itemsByID,
} = narrow(inbox.Service)

export const { List: listProfiles, Agents: listAgents, Rediscover: rediscoverProfiles } = narrow(profiles.Service)

// The first-run setup. State says whether it shows and the step it opens on; SetStep
// remembers the step, so a quit part way through opens there again; Finish marks it done
// (and the shell then asks to allow banners, if the setup held the question), and Restart
// opens it again from Welcome. GitHub checks the GitHub CLI's sign-in, one GraphQL point;
// Terminals says which terminals are installed; Signals counts the clones that hold each
// sign the Tools step looks for, with lstat alone. Reset asks for a fresh start once the
// user typed reset: it saves a backup first, and the app quits and opens again on the
// setup, its prompts, launch defaults, and scripts brought back.
export const {
  State: getSetup,
  SetStep: setSetupStep,
  Finish: finishSetup,
  Restart: restartSetup,
  GitHub: checkGitHub,
  Terminals: listTerminals,
  Signals: setupSignals,
  Reset: resetApp,
} = narrow(setup.Service)

export const {
  Launch: launch,
  Resolve: resolveLaunch,
  ResolveItems: resolveLaunches,
  Preview: previewLaunch,
  CloneTarget: cloneTarget,
  Clone: cloneRepo,
  LinkClone: linkClone,
  Resume: resumeSession,
} = narrow(launcher.Service)

export const {
  List: listSessions,
  Focus: focusSession,
  LastWords: sessionLastWords,
  ShowFolder: showSessionFolder,
  Forget: forgetSession,
} = narrow(sessions.Service)

export const {
  Open: openReview,
  Refresh: refreshReview,
  ReadAgain: readReviewAgain,
  Patches: reviewPatches,
  Earlier: reviewEarlier,
  Act: actOnReview,
  Replies: reviewReplies,
  FillReply: fillReply,
} = narrow(review.Service)

export const {
  Open: openIssue,
  Refresh: refreshIssue,
  ReadAgain: readIssueAgain,
  Earlier: issueEarlier,
  Act: actOnIssue,
  Replies: issueReplies,
  FillReply: fillIssueReply,
  Labels: issueLabels,
  Assignable: issueAssignable,
  Edit: editIssue,
  Avatars: issueAvatars,
} = narrow(issues.Service)

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

export const { Export: exportBackup, Preview: previewBackup, Import: importBackup } = narrow(backup.Service)

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
  AddCloneLocations: addCloneLocations,
  RemoveCloneLocation: removeCloneLocation,
  PreviewCloneLocation: previewCloneLocation,
  CheckCloneLocations: checkCloneLocations,
  SuggestCloneRoots: suggestCloneRoots,
  CreateCloneRoot: createCloneRoot,
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
  SetToastSeconds: setToastSeconds,
  CollapsedSections: getCollapsedSections,
  SetSectionCollapsed: setSectionCollapsed,
  SetProjectsTipDismissed: setProjectsTipDismissed,
  SetKeptRunningTold: setKeptRunningTold,
  SetReviewTab: setReviewTab,
  SetNotificationsTab: setNotificationsTab,
  SetConversationOrder: setConversationOrder,
  InboxOrder: getInboxOrder,
  SetStatusSort: setStatusSort,
  SetListKind: setListKind,
  SetStatusOrder: setStatusOrder,
  SetBranchRules: setBranchRules,
  SetRepoRule: setRepoRule,
  SetMergeMethod: setMergeMethod,
  SetIntegrationEnabled: setIntegrationEnabled,
  SetIntegrationPath: setIntegrationPath,
  SetRunsIn: setRunsIn,
  SetOfferAfterMerge: setOfferAfterMerge,
  SetScriptSources: setScriptSources,
  SetIntegrationsAnnounced: setIntegrationsAnnounced,
  SetScriptsLastPage: setScriptsLastPage,
  SetScriptsLastRepo: setScriptsLastRepo,
  SetScriptsSectionOpen: setScriptsSectionOpen,
  SetShowRuns: setShowRuns,
  SetRefreshInterval: setRefreshInterval,
  SetStatusProvider: setStatusProvider,
  SetStatusPart: setStatusPart,
  SetGitHubNotificationSwitch: setGitHubNotificationSwitch,
  SetGitHubNotificationTrigger: setGitHubNotificationTrigger,
  SetGitHubNotificationAction: setGitHubNotificationAction,
  SetGitHubCleanupCardDismissed: setGitHubCleanupCardDismissed,
  SetUpdateMode: setUpdateMode,
  SetAutopilotPaused: setAutopilotPaused,
  SetAutopilotDefaultMode: setAutopilotDefaultMode,
  SetAutopilotAgentsAtOnce: setAutopilotAgentsAtOnce,
  SetAutopilotRunsPerDay: setAutopilotRunsPerDay,
  SetAutopilotActionsPerDay: setAutopilotActionsPerDay,
  SetAutopilotDailyCap: setAutopilotDailyCap,
  SetAutopilotKeepAwake: setAutopilotKeepAwake,
  SetAutopilotHoldOnOutage: setAutopilotHoldOnOutage,
  SetAutopilotCatchUpHours: setAutopilotCatchUpHours,
  SetAutopilotHistoryDays: setAutopilotHistoryDays,
  SetAutopilotLogDays: setAutopilotLogDays,
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
// release. QuitAnyway answers the "app:quitRequested" question: stop the runs and quit;
// KeepRunning its other answer, which also forgets the copy a move asked to open at the quit.
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
  KeepRunning: keepRunning,
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

// The user's GitHub notifications. Snapshot reads what the app mirrors from the database
// alone and never asks GitHub, and List every thread it mirrors, read and unread, for the
// Notifications page; CheckNow runs a pass now, once any pass under way has ended, and
// returns the Snapshot after it. A pass also sends the clears that are due, once the user
// turned clearing on or cleared one by hand. A "ghthreads:updated" event follows each pass
// that changed what Snapshot or List shows.
//
// Preview says, from the database alone, what clearing would do now, as if it were on;
// Clear and ClearItem clear threads by the user's own hand, at once and whether clearing is
// on or off; Keep takes back queued clears, the toast's Undo; and MarkDone and MarkRead
// mark threads done or read on GitHub at the user's click in the panel, whatever their
// subject. Each returns the Snapshot.
const githubThreads = narrow(ghthreads.Service)

export const githubThreadsSnapshot: () => CancellablePromise<GitHubThreadsSnapshot> = githubThreads.Snapshot
export const githubThreadsList: () => CancellablePromise<GitHubThreadList> = githubThreads.List
export const checkGitHubThreadsNow: () => CancellablePromise<GitHubThreadsSnapshot> = githubThreads.CheckNow
export const previewGitHubThreads: () => CancellablePromise<GitHubThreadsPreview> = githubThreads.Preview
export const clearGitHubThreads: (threadIDs: number[]) => CancellablePromise<GitHubThreadsSnapshot> =
  githubThreads.Clear
export const clearGitHubItemThreads: (
  owner: string,
  name: string,
  number: number,
) => CancellablePromise<GitHubThreadsSnapshot> = githubThreads.ClearItem
export const keepGitHubThreads: (clearIDs: number[]) => CancellablePromise<GitHubThreadsSnapshot> = githubThreads.Keep
export const markGitHubThreadsDone: (threadIDs: number[]) => CancellablePromise<GitHubThreadsSnapshot> =
  githubThreads.MarkDone
export const markGitHubThreadsRead: (threadIDs: number[]) => CancellablePromise<GitHubThreadsSnapshot> =
  githubThreads.MarkRead

// The status pages of the services the user watches. Status judges each provider's last
// reading against what the user watches now, reading nothing from the network itself, so
// a change of the ticked parts shows at once; a provider not watched, or every one while
// the check is off, is off. CheckNow reads again each page watched that it didn't read
// moments ago. A failed check keeps the last reading and says why in its provider's error.
// A "servicestatus:updated" event follows each pass, and each change of what is watched.
const serviceStatus = narrow(svcstatus.Service)

export const { Status: getServiceStatus, CheckNow: checkServiceStatus } = serviceStatus

// Autopilot, the Autopilot tab's. Glance is Autopilot at a glance, which the tab counts;
// List the routines, each marked when it belongs to another account or is broken; Engine
// what the engine last did, and whether this copy runs it. Activity is a page of what the
// routines did before a match (0 for the newest), by day; ItemHistory one item's, which its
// ⚡ chip opens; LatestMatches each item's newest match, which the chip says. Watching lists
// the items a routine watches, which the inbox's ItemsByID turns into rows. Matching counts
// what a routine matches now that it has no record of, the question SetEnabled and SetMode
// take the answer to (current). Undo, TryAgain, LetActAgain, and ClearHistory are commands
// the engine applies. Every read is of the database alone, and an "autopilot:updated" event
// follows each change, at most four times a second.
const autopilotService = narrow(autopilot.Service)

export const {
  Glance: getAutopilotGlance,
  Delete: deleteRoutine,
  Matching: routineMatching,
  Watching: routineWatching,
  Engine: getAutopilotEngine,
  Undo: undoAutopilot,
  LetActAgain: letRoutineActAgain,
  ClearHistory: clearAutopilotHistory,
} = autopilotService

// The calls whose results hold routines or matches, typed with the values the generated
// unions leave out (AutopilotRoutine, AutopilotMatch).
export const listRoutines = autopilotService.List as () => CancellablePromise<AutopilotRoutine[]>
export const duplicateRoutine = autopilotService.Duplicate as (id: number) => CancellablePromise<AutopilotRoutine>
export const setRoutineEnabled = autopilotService.SetEnabled as (
  id: number,
  on: boolean,
  current: AutopilotCurrent,
) => CancellablePromise<AutopilotRoutine>
export const setRoutineMode = autopilotService.SetMode as (
  id: number,
  mode: AutopilotMode,
  current: AutopilotCurrent,
) => CancellablePromise<AutopilotRoutine>
export const autopilotActivity = autopilotService.Activity as (
  before: number,
  limit: number,
) => CancellablePromise<AutopilotActivityPage>
export const autopilotItemHistory = autopilotService.ItemHistory as (id: number) => CancellablePromise<AutopilotHistory>
export const latestAutopilotMatches = autopilotService.LatestMatches as (
  ids: number[] | null,
) => CancellablePromise<AutopilotMatch[]>
export const tryRoutineAgain = autopilotService.TryAgain as (
  id: number,
  itemID: number,
) => CancellablePromise<AutopilotMatch[]>

// The notification history. MarkAllRead marks every entry read (Mark all done), and
// MarkRead and MarkUnread the entries with the ids given; opening the history marks
// nothing. Clear forgets them all. A "notifications:updated" event follows each change.
const notifications = narrow(notify.Service)

export const {
  MarkAllRead: markAllNotificationsRead,
  MarkRead: markNotificationsRead,
  MarkUnread: markNotificationsUnread,
  Clear: clearNotifications,
  BannerStatus: notificationBannerStatus,
  SendTest: sendTestNotification,
} = notifications

// The app's own updates. Status reads where the update stands, from memory and the stored
// state alone, never the network; CheckForUpdates checks now, in every mode, and returns the
// status, whose lastError tells a check that failed (the call itself succeeds); HideBanner
// is the banner's Later, for a day, and ShowBanner lifts it, as a click on the update's
// notification does; AckUpdated forgets the version this launch updated to, once the window
// has said so; ShowInstallLog shows the log of an update that didn't install in Finder. An
// "updates:updated" event follows every change, a download's progress included, and
// "updates:restart" asks the window to restart to update (restartToUpdate in
// lib/useUpdate.ts), as the menu bar's Restart to Update does. downloadUpdate and
// restartToUpdateNow, below, read what their refusals carry.
//
// Where the app can't update itself (status.location.movable), MoveToApplications copies it
// into Applications, checks the copy, and quits the normal way, which asks first while runs
// go, opening the copy once it has; with a copy there already it replaces nothing and says
// so (exists), and OpenInApplications opens that one the same way. ShowAppInFinder shows the
// copy the user opened, which a move leaves where it is. A failure rejects with the
// backend's words, and nothing has changed.
export const {
  Status: getUpdateStatus,
  CheckNow: checkForUpdates,
  HideBanner: hideUpdateBanner,
  ShowBanner: showUpdateBanner,
  AckUpdated: ackUpdated,
  ShowInstallLog: showInstallLog,
  MoveToApplications: moveToApplications,
  OpenInApplications: openInApplications,
  ShowAppInFinder: showAppInFinder,
} = narrow(update.Service)

// RunGoing is a run going in the app that restarting to update would stop, as Go's
// update.Run names it: its id, kind, and title, its repository ("owner/name", or '') and
// folder (with ~ for home), and why it can't be stopped right now, or ''.
export interface RunGoing {
  id: number
  kind: string
  title: string
  repo: string
  dir: string
  stopRefused: string
}

// UpdateRefusal names a refusal of a download or a restart that the window answers on its
// own, as Go's update.RefusalCode does: settling, while the last update may still be
// finishing, or finishing, while a restart whose quit was held sees the app out, each of
// which a moment's wait answers; or uninstallable, an update that failed to install for
// good, which no retry fixes, so only its release page is offered.
export type UpdateRefusal = 'settling' | 'finishing' | 'uninstallable'

// Refused is a download or a restart refused for one of those reasons, with the backend's
// words for it.
export interface Refused {
  refusal: UpdateRefusal
  message: string
}

// RestartAnswer is how asking to restart to update ended: the update is installing, and
// the app quits (Restart rarely returns then); the restart was held back because runs go in
// the app, which it would stop: those runs, or an empty list when they couldn't be listed,
// for the window to ask about, with the backend's words for why; or it was refused for a
// reason the window answers on its own.
export type RestartAnswer =
  | { restarted: true; status: UpdateStatus }
  | { restarted: false; runsGoing: RunGoing[]; message: string }
  | ({ restarted: false } & Refused)

// DownloadAnswer is how asking to download the newer version ended: downloaded and
// checked, with the status, ready; or refused for a reason the window answers on its own.
export type DownloadAnswer = { downloaded: true; status: UpdateStatus } | ({ downloaded: false } & Refused)

// causeOf reads what a failed call's cause carries beside its message: the backend's words
// for the user, and its detail, or null when the failure carries no cause.
function causeOf(err: unknown): { detail: unknown; message: string } | null {
  if (!(err instanceof Error) || typeof err.cause !== 'object' || err.cause === null) {
    return null
  }

  const { cause } = err
  const message = 'message' in cause && typeof cause.message === 'string' ? cause.message : ''

  return { detail: 'detail' in cause ? cause.detail : undefined, message }
}

// runFields are the fields of a RunGoing, with the type each holds.
const runFields = {
  id: 'number',
  kind: 'string',
  title: 'string',
  repo: 'string',
  dir: 'string',
  stopRefused: 'string',
}

// runGoingOf reads one run of a refusal's detail, or null when it isn't one.
function runGoingOf(value: unknown): RunGoing | null {
  if (typeof value !== 'object' || value === null) {
    return null
  }

  const fields = value as Record<string, unknown>

  return Object.entries(runFields).every(([name, type]) => typeof fields[name] === type) ? (value as RunGoing) : null
}

// runsGoingOf reads the runs a refused restart names from the failed call's cause, whose
// detail Go sends as {"runsGoing": [...]} (update.RunsGoingDetail), with the backend's
// words for the user. It returns null for any other failure, and leaves out an entry it
// can't read; a list it can't read at all, like one Go couldn't list, is empty, which
// means runs are going without saying which.
function runsGoingOf(err: unknown): { runsGoing: RunGoing[]; message: string } | null {
  const cause = causeOf(err)
  const detail = cause?.detail

  if (cause === null || typeof detail !== 'object' || detail === null || !('runsGoing' in detail)) {
    return null
  }

  const listed = Array.isArray(detail.runsGoing) ? (detail.runsGoing as unknown[]) : []

  return { runsGoing: listed.map(runGoingOf).filter((run) => run !== null), message: cause.message }
}

// refusals are the codes of the refusals the window knows.
const refusals: readonly UpdateRefusal[] = ['settling', 'finishing', 'uninstallable']

// isRefusal reports whether code is one of the refusals the window knows.
function isRefusal(code: unknown): code is UpdateRefusal {
  return refusals.some((r) => r === code)
}

// refusedOf reads a refusal the window answers on its own from the failed call's cause,
// whose detail Go sends as {"code": "..."} (update.RefusalDetail), with the backend's
// words for the user. It returns null for any other failure, a code this build doesn't
// know included.
function refusedOf(err: unknown): Refused | null {
  const cause = causeOf(err)
  const detail = cause?.detail

  if (cause === null || typeof detail !== 'object' || detail === null || !('code' in detail)) {
    return null
  }

  return isRefusal(detail.code) ? { refusal: detail.code, message: cause.message } : null
}

// downloadUpdate downloads the newer version found and checks it, as Just tell me's Update
// does before it restarts, and resolves with the status, ready. While the last update may
// still be finishing, it resolves refused, for the window to wait out; any other failure
// rejects with the backend's words.
export function downloadUpdate(): CancellablePromise<DownloadAnswer> {
  return narrow(update.Service)
    .Download()
    .then(
      (status): DownloadAnswer => ({ downloaded: true, status }),
      (err: unknown): DownloadAnswer => {
        const refused = refusedOf(err)

        if (refused === null) {
          throw err
        }

        return { downloaded: false, ...refused }
      },
    )
}

// restartToUpdateNow installs the update that is ready and restarts into it. Unless
// confirmed, it is held back while runs go in the app, which it would stop: it then
// resolves with those runs, for the window to ask about first. Confirmed, it stops them.
// While the last update may still be finishing, or after a quit was held in this run, it
// resolves refused, saying which. It backs up the data first. Any other failure, which
// leaves the update ready again, rejects with the backend's words. It may wait for a check
// or a download under way first, for up to the download's limit.
export function restartToUpdateNow(confirmed: boolean): CancellablePromise<RestartAnswer> {
  return narrow(update.Service)
    .Restart(confirmed)
    .then(
      (status): RestartAnswer => ({ restarted: true, status }),
      (err: unknown): RestartAnswer => {
        const held = runsGoingOf(err) ?? refusedOf(err)

        if (held === null) {
          throw err
        }

        return { restarted: false, ...held }
      },
    )
}

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

// FolderChoice is what a native folder dialog asks for: its title and the button that
// confirms it.
export interface FolderChoice {
  title: string
  button: string
}

// chooseFolder asks the user for a folder in the system's open dialog, and resolves with
// its path, or '' when they cancel.
export async function chooseFolder(choice: FolderChoice): Promise<string> {
  return Dialogs.OpenFile({
    Title: choice.title,
    ButtonText: choice.button,
    CanChooseFiles: false,
    CanChooseDirectories: true,
    CanCreateDirectories: false,
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
