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
import * as inbox from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import type * as item from '../../bindings/github.com/mrz1836/nilbox/internal/item'
import * as launchdefaults from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import * as launcher from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type * as terminal from '../../bindings/github.com/mrz1836/nilbox/internal/launcher/terminal'
import * as notify from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
import type * as notifyprefs from '../../bindings/github.com/mrz1836/nilbox/internal/notifyprefs'
import * as profiles from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import type * as bump from '../../bindings/github.com/mrz1836/nilbox/internal/bump'
import * as prompts from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
import * as review from '../../bindings/github.com/mrz1836/nilbox/internal/review'
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

export type AppNotification = NonNullLists<notify.Notification>
export type NotificationLog = NonNullLists<notify.Log>
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
} = narrow(settings.Service)

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

export const {
  List: listNotifications,
  MarkAllRead: markNotificationsRead,
  Clear: clearNotifications,
  BannerStatus: notificationBannerStatus,
  SendTest: sendTestNotification,
} = narrow(notify.Service)

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
