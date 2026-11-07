// The platform module is the frontend's only door to the desktop shell: the Wails runtime
// and the generated bindings for the Go services. Everything else in src/ imports from
// here (an ESLint rule enforces it), so another shell or an OS port replaces this module's
// implementation and never its callers.

import { Browser, Events, Window } from '@wailsio/runtime'

import { Service as AppInfo } from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
import type { Info } from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
import { Service as Inbox } from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import type { Snapshot as InboxSnapshot } from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import { Service as LaunchDefaultsService } from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import type { FieldError } from '../../bindings/github.com/mrz1836/nilbox/internal/agent'
import type {
  Defaults as LaunchDefaults,
  Input as LaunchDefaultsInput,
  Target as LaunchTarget,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import { Service as Launcher } from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type {
  CloneTarget,
  ItemResolution,
  LaunchRequest,
  LaunchResult,
  Preview as LaunchPreview,
  Resolution,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import { Service as Notifications } from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
import type { Log as NotificationLog } from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
import { Service as Profiles } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import type { Profile } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import { Service as PromptsService } from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
import type {
  CheckRequest as PromptCheckRequest,
  CheckResult as PromptCheckResult,
  DefaultPrompts,
  Input as PromptInput,
  Prompt,
  Variable as PromptVariable,
} from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
import { Service as SettingsService } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
import type { Settings, WindowSize } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
import type {
  Check as FileCheck,
  PathMatch as SensitivePathMatch,
} from '../../bindings/github.com/mrz1836/nilbox/internal/sensitive'
import type { Rules as SnoozeRules } from '../../bindings/github.com/mrz1836/nilbox/internal/snooze'
import { Service as Worktrees } from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'
import type {
  Cleanup as WorktreeCleanup,
  RemoveResult,
  Snapshot as WorktreesSnapshot,
  SweepReport,
} from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'

// The bound services' data types, generated from the Go structs. Callers use these and
// never declare their own copies.
export type { Info } from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
export type {
  Account,
  Checks,
  Details as ItemDetails,
  Item as InboxItem,
  Label,
  Reviews,
  List as InboxList,
  Rate,
  RateGauge,
  Snapshot as InboxSnapshot,
} from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
export type { Agent, FieldError } from '../../bindings/github.com/mrz1836/nilbox/internal/agent'
export type {
  Defaults as LaunchDefaults,
  Input as LaunchDefaultsInput,
  Target as LaunchTarget,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
export type {
  CloneTarget,
  Hold as LaunchHold,
  ItemResolution,
  LaunchRequest,
  LaunchResult,
  Preview as LaunchPreview,
  PromptChoice,
  Resolution,
  Setting as LaunchSetting,
  Toggle as LaunchToggle,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
export type {
  Log as NotificationLog,
  Notification as AppNotification,
} from '../../bindings/github.com/mrz1836/nilbox/internal/notify'
export type { Profile } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
export type {
  Category as SensitiveCategory,
  Check as FileCheck,
  Config as SensitiveConfig,
  Finding as SensitiveFile,
  Group as SensitiveGroup,
  PathMatch as SensitivePathMatch,
} from '../../bindings/github.com/mrz1836/nilbox/internal/sensitive'
export type {
  CheckRequest as PromptCheckRequest,
  CheckResult as PromptCheckResult,
  DefaultPrompts,
  Diagnostic,
  Input as PromptInput,
  Prompt,
  Usage as PromptUsage,
  Variable as PromptVariable,
} from '../../bindings/github.com/mrz1836/nilbox/internal/prompts'
export type { Settings, WindowSize } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
export type {
  Reasons as WakeReasons,
  Rules as SnoozeRules,
} from '../../bindings/github.com/mrz1836/nilbox/internal/snooze'
export type {
  Cleanup as WorktreeCleanup,
  LinkedItem,
  RemoveResult,
  Repo as WorktreeRepo,
  Snapshot as WorktreesSnapshot,
  SweepReport,
  Worktree,
} from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'

// inboxUpdatedEvent is the event the shell emits whenever the stored inbox changed.
export const inboxUpdatedEvent = 'inbox:updated'

// worktreesUpdatedEvent is the event the shell emits when an automatic worktree sweep
// removed a worktree or changed how many need attention.
export const worktreesUpdatedEvent = 'worktrees:updated'

// notificationsUpdatedEvent is the event the shell emits when the notification log changed.
export const notificationsUpdatedEvent = 'notifications:updated'

// revealItemEvent is the event the shell emits to show an item in the inbox: a click on it
// in the menu bar, or on its notification. Its data is a RevealRequest.
export const revealItemEvent = 'inbox:reveal'

// RevealRequest is an item the shell asks the window to show, and its URL on GitHub for
// when it is no longer in the inbox.
export interface RevealRequest {
  itemId: number
  url: string
}

// App

export function getAppInfo(): Promise<Info> {
  return AppInfo.Info()
}

// reportFirstPaint tells the shell the window painted its first frame (startup timing).
export function reportFirstPaint(): Promise<void> {
  return AppInfo.ReportFirstPaint()
}

// openInBrowser opens a URL, such as a pull request, in the default browser.
export function openInBrowser(url: string): Promise<void> {
  return Browser.OpenURL(url)
}

// Inbox

export function getInboxSnapshot(): Promise<InboxSnapshot> {
  return Inbox.Snapshot()
}

export function refreshInbox(): Promise<InboxSnapshot> {
  return Inbox.Refresh()
}

// refreshItem reads one item from GitHub again, without refreshing the rest of the inbox,
// and returns the new inbox. An item merged or closed since leaves its lists.
export function refreshItem(id: number): Promise<InboxSnapshot> {
  return Inbox.RefreshItem(id)
}

// dismissItem hides an item from every list until it is restored, and returns the new
// inbox. Triage stays on this machine.
export function dismissItem(id: number): Promise<InboxSnapshot> {
  return Inbox.Dismiss(id)
}

// snoozeItem hides an item from every list for a number of days, and returns the new
// inbox. Triage stays on this machine.
export function snoozeItem(id: number, days: number): Promise<InboxSnapshot> {
  return Inbox.Snooze(id, days)
}

// restoreItem brings a dismissed or snoozed item back to its lists, and returns the new
// inbox.
export function restoreItem(id: number): Promise<InboxSnapshot> {
  return Inbox.Restore(id)
}

// pinItem keeps an item at the top of its lists, the most recently pinned first, and
// returns the new inbox. Triage stays on this machine.
export function pinItem(id: number): Promise<InboxSnapshot> {
  return Inbox.Pin(id)
}

// unpinItem puts a pinned item back among the rest, and returns the new inbox.
export function unpinItem(id: number): Promise<InboxSnapshot> {
  return Inbox.Unpin(id)
}

// checkItemFiles checks a pull request's changed files against GitHub now, stores the
// outcome (the inbox is updated with it), and returns it. It fails for an item whose files
// are not checked.
export async function checkItemFiles(id: number): Promise<FileCheck> {
  const check = await Inbox.CheckFiles(id)

  if (check === null) {
    throw new Error('This item’s files are not checked.')
  }

  return check
}

// onInboxUpdated calls listener whenever the stored inbox changed, and returns a function
// that stops listening.
export function onInboxUpdated(listener: () => void): () => void {
  return Events.On(inboxUpdatedEvent, () => {
    listener()
  })
}

// Launch

export async function listProfiles(): Promise<Profile[]> {
  return (await Profiles.List()) ?? []
}

export function launch(request: LaunchRequest): Promise<LaunchResult> {
  return Launcher.Launch(request)
}

// resolveLaunch returns what a launch on one item resolves to with the request's choices.
export function resolveLaunch(request: LaunchRequest): Promise<Resolution> {
  return Launcher.Resolve(request)
}

// resolveLaunches returns each item's default launch settings.
export async function resolveLaunches(ids: number[]): Promise<ItemResolution[]> {
  return (await Launcher.ResolveItems(ids)) ?? []
}

// previewLaunch plans a launch without starting it: the command and the filled-in prompt.
export function previewLaunch(request: LaunchRequest): Promise<LaunchPreview> {
  return Launcher.Preview(request)
}

// cloneTarget reports whether an item's repository has a local clone and, when it has
// none, where cloneRepo would create one and whether that path is already taken.
export function cloneTarget(itemId: number): Promise<CloneTarget> {
  return Launcher.CloneTarget(itemId)
}

// cloneRepo clones an item's repository into <clone root>/<name> with `gh repo clone`,
// refusing a path that already exists, and remembers the new clone for the next launch.
export function cloneRepo(itemId: number): Promise<CloneTarget> {
  return Launcher.Clone(itemId)
}

// Prompts

export async function listPrompts(): Promise<Prompt[]> {
  return (await PromptsService.List()) ?? []
}

export function createPrompt(input: PromptInput): Promise<Prompt> {
  return PromptsService.Create(input)
}

export function updatePrompt(id: number, input: PromptInput): Promise<Prompt> {
  return PromptsService.Update(id, input)
}

export function deletePrompt(id: number): Promise<void> {
  return PromptsService.Delete(id)
}

export function duplicatePrompt(id: number): Promise<Prompt> {
  return PromptsService.Duplicate(id)
}

export function resetPrompt(id: number): Promise<Prompt> {
  return PromptsService.Reset(id)
}

// checkPrompt checks a prompt being edited and fills it in for a preview.
export function checkPrompt(request: PromptCheckRequest): Promise<PromptCheckResult> {
  return PromptsService.Check(request)
}

export async function listPromptVariables(): Promise<PromptVariable[]> {
  return (await PromptsService.Variables()) ?? []
}

export function getDefaultPrompts(): Promise<DefaultPrompts> {
  return PromptsService.Defaults()
}

export function setDefaultPrompt(kind: string, id: number): Promise<void> {
  return PromptsService.SetDefault(kind, id)
}

// Launch defaults

export async function listLaunchDefaults(): Promise<LaunchDefaults[]> {
  return (await LaunchDefaultsService.List()) ?? []
}

// checkLaunchDefaults validates launch defaults being edited.
export async function checkLaunchDefaults(input: LaunchDefaultsInput): Promise<FieldError[]> {
  return (await LaunchDefaultsService.Check(input)) ?? []
}

export function saveLaunchDefaults(input: LaunchDefaultsInput): Promise<LaunchDefaults> {
  return LaunchDefaultsService.Save(input)
}

export function removeLaunchDefaults(owner: string, name: string): Promise<void> {
  return LaunchDefaultsService.Remove(owner, name)
}

export async function listLaunchTargets(): Promise<LaunchTarget[]> {
  return (await LaunchDefaultsService.Targets()) ?? []
}

// Settings

export function getSettings(): Promise<Settings> {
  return SettingsService.Get()
}

export function setTerminal(terminal: string): Promise<void> {
  return SettingsService.SetTerminal(terminal)
}

export function setCloneRoot(root: string): Promise<void> {
  return SettingsService.SetCloneRoot(root)
}

export function setDefaultProfile(name: string): Promise<void> {
  return SettingsService.SetDefaultProfile(name)
}

export function setWorktreeDefault(on: boolean): Promise<void> {
  return SettingsService.SetWorktreeDefault(on)
}

// setWorktreeAutoCleanup stores whether finished worktrees are cleaned up on their own once
// their pull request merges or their issue closes.
export function setWorktreeAutoCleanup(on: boolean): Promise<void> {
  return SettingsService.SetWorktreeAutoCleanup(on)
}

// setMenuBar shows or hides the menu bar icon. While it shows, closing the window keeps the
// app running there.
export function setMenuBar(on: boolean): Promise<void> {
  return SettingsService.SetMenuBar(on)
}

// setNotificationBanner turns the banners of one kind of notification on or off. The
// notification history keeps every kind either way.
export function setNotificationBanner(kind: string, on: boolean): Promise<void> {
  return SettingsService.SetNotificationBanner(kind, on)
}

// setSnoozeDays stores how many days an inbox row's Snooze button hides an item.
export function setSnoozeDays(days: number): Promise<void> {
  return SettingsService.SetSnoozeDays(days)
}

// setSnoozeWake stores which new activity ends a snooze early.
export function setSnoozeWake(rules: SnoozeRules): Promise<void> {
  return SettingsService.SetSnoozeWake(rules)
}

// setWindowSize stores the size the window opens at, then resizes the open window to it.
export async function setWindowSize(size: WindowSize): Promise<void> {
  await SettingsService.SetWindowSize(size)
  await Window.SetSize(size.width, size.height)
}

// setTrustedBots stores the bots whose items are trusted like a collaborator's.
export function setTrustedBots(bots: string[]): Promise<void> {
  return SettingsService.SetTrustedBots(bots)
}

// resetTrustedBots goes back to the default trusted bots.
export function resetTrustedBots(): Promise<void> {
  return SettingsService.ResetTrustedBots()
}

// setSensitiveScope stores whose pull requests have their changed files checked:
// "outside" for outside contributors, or "collaborators" for collaborators' too.
export function setSensitiveScope(scope: string): Promise<void> {
  return SettingsService.SetSensitiveScope(scope)
}

// setSensitiveCategory turns a built-in category of sensitive files on or off.
export function setSensitiveCategory(id: string, on: boolean): Promise<void> {
  return SettingsService.SetSensitiveCategory(id, on)
}

// setSensitivePatterns stores the user's own patterns of sensitive files, in order.
export function setSensitivePatterns(patterns: string[]): Promise<void> {
  return SettingsService.SetSensitivePatterns(patterns)
}

// matchSensitivePath reports whether a path counts as sensitive under the stored
// settings, and which category and pattern it matches.
export function matchSensitivePath(path: string): Promise<SensitivePathMatch> {
  return SettingsService.MatchSensitivePath(path)
}

// getTheme reads the stored theme: "system", "light", or "dark".
export function getTheme(): Promise<string> {
  return SettingsService.Theme()
}

// setTheme stores the theme, and the desktop shell brings the window's native appearance
// along with it. The page switches itself.
export function setTheme(theme: string): Promise<void> {
  return SettingsService.SetTheme(theme)
}

// getCollapsedSections lists the inbox sections the user collapsed, by list name
// ("reviewRequested", "authored", "assigned"), in the inbox's order.
export async function getCollapsedSections(): Promise<string[]> {
  return (await SettingsService.CollapsedSections()) ?? []
}

// setSectionCollapsed stores whether an inbox section is collapsed, so the inbox opens the
// same way at the next launch.
export function setSectionCollapsed(section: string, collapsed: boolean): Promise<void> {
  return SettingsService.SetSectionCollapsed(section, collapsed)
}

// Worktrees

export function listWorktrees(): Promise<WorktreesSnapshot> {
  return Worktrees.List()
}

export function removeWorktree(repoPath: string, path: string): Promise<void> {
  return Worktrees.Remove(repoPath, path)
}

export function forceRemoveWorktree(repoPath: string, path: string): Promise<void> {
  return Worktrees.ForceRemove(repoPath, path)
}

export async function removeAllCleanWorktrees(): Promise<RemoveResult[]> {
  return (await Worktrees.RemoveAllClean()) ?? []
}

export function pruneWorktrees(repoPath: string): Promise<void> {
  return Worktrees.Prune(repoPath)
}

// unlockWorktree releases a lock an agent session wrote, once that session has ended.
export function unlockWorktree(repoPath: string, path: string): Promise<void> {
  return Worktrees.Unlock(repoPath, path)
}

// cleanUpWorktree removes one finished worktree, and its own branch, after checking it
// again.
export function cleanUpWorktree(repoPath: string, path: string): Promise<RemoveResult> {
  return Worktrees.CleanUp(repoPath, path)
}

export async function cleanUpFinishedWorktrees(): Promise<RemoveResult[]> {
  return (await Worktrees.CleanUpFinished()) ?? []
}

// sweepWorktrees runs the automatic cleanup now, as it runs on its own every few minutes.
export function sweepWorktrees(): Promise<SweepReport> {
  return Worktrees.Sweep()
}

// recentCleanups lists the worktrees cleaned up most recently, newest first, by hand or by
// the automatic sweep. The history survives restarts.
export async function recentCleanups(): Promise<WorktreeCleanup[]> {
  return (await Worktrees.RecentCleanups()) ?? []
}

// clearCleanups forgets the history of cleaned-up worktrees. It removes nothing else.
export function clearCleanups(): Promise<void> {
  return Worktrees.ClearCleanups()
}

// lastWorktreeSweep returns what the latest automatic sweep found and did; its seq is 0
// before the first one.
export function lastWorktreeSweep(): Promise<SweepReport> {
  return Worktrees.LastSweep()
}

// onWorktreesUpdated calls listener whenever an automatic sweep changed something, and
// returns a function that stops listening.
export function onWorktreesUpdated(listener: () => void): () => void {
  return Events.On(worktreesUpdatedEvent, () => {
    listener()
  })
}

// Notifications

// listNotifications returns the notification history, newest first, and how many are
// unread.
export async function listNotifications(): Promise<NotificationLog> {
  const log = await Notifications.List()

  return { items: log.items ?? [], unread: log.unread }
}

// markNotificationsRead marks the whole history read, as opening it does.
export function markNotificationsRead(): Promise<void> {
  return Notifications.MarkAllRead()
}

// clearNotifications forgets the notification history.
export function clearNotifications(): Promise<void> {
  return Notifications.Clear()
}

// notificationBannerStatus reports whether banners are shown: 'on', 'off' when the user has
// not allowed them, or 'unavailable' when this build cannot show them.
export function notificationBannerStatus(): Promise<string> {
  return Notifications.BannerStatus()
}

// sendTestNotification shows a sample banner, asking the user to allow them first if they
// have not been asked.
export function sendTestNotification(): Promise<void> {
  return Notifications.SendTest()
}

// onNotificationsUpdated calls listener whenever the notification log changed, and returns
// a function that stops listening.
export function onNotificationsUpdated(listener: () => void): () => void {
  return Events.On(notificationsUpdatedEvent, () => {
    listener()
  })
}

// onRevealItem calls listener with each item the shell asks the window to show, and returns
// a function that stops listening. A request with neither an item nor a URL is ignored.
export function onRevealItem(listener: (request: RevealRequest) => void): () => void {
  return Events.On(revealItemEvent, (event: { data: unknown }) => {
    const request = revealRequest(event.data)

    if (request !== null) {
      listener(request)
    }
  })
}

// revealRequest reads a reveal event's data, or null when it is not one.
function revealRequest(data: unknown): RevealRequest | null {
  if (typeof data !== 'object' || data === null) {
    return null
  }

  const fields = data as Partial<Record<keyof RevealRequest, unknown>>
  const itemId = typeof fields.itemId === 'number' && fields.itemId > 0 ? fields.itemId : 0
  const url = typeof fields.url === 'string' ? fields.url : ''

  return itemId === 0 && url === '' ? null : { itemId, url }
}
