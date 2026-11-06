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
import type {
  Defaults as LaunchDefaults,
  FieldError as LaunchDefaultsFieldError,
  Input as LaunchDefaultsInput,
  Target as LaunchTarget,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
import { Service as Launcher } from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type {
  ItemResolution,
  LaunchRequest,
  LaunchResult,
  Preview as LaunchPreview,
  Resolution,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
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
import type { Rules as SnoozeRules } from '../../bindings/github.com/mrz1836/nilbox/internal/snooze'
import { Service as Worktrees } from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'
import type {
  RemoveResult,
  Snapshot as WorktreesSnapshot,
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
export type { Agent } from '../../bindings/github.com/mrz1836/nilbox/internal/agents'
export type {
  Defaults as LaunchDefaults,
  FieldError as LaunchDefaultsFieldError,
  Input as LaunchDefaultsInput,
  Target as LaunchTarget,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launchdefaults'
export type {
  ItemResolution,
  LaunchRequest,
  LaunchResult,
  Preview as LaunchPreview,
  PromptChoice,
  Resolution,
  Setting as LaunchSetting,
  Toggle as LaunchToggle,
} from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
export type { Profile } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
export type {
  CheckRequest as PromptCheckRequest,
  CheckResult as PromptCheckResult,
  DefaultPrompts,
  Diagnostic,
  FieldError,
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
  RemoveResult,
  Repo as WorktreeRepo,
  Snapshot as WorktreesSnapshot,
  Worktree,
} from '../../bindings/github.com/mrz1836/nilbox/internal/worktrees'

// inboxUpdatedEvent is the event the shell emits whenever the stored inbox changed.
export const inboxUpdatedEvent = 'inbox:updated'

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
export async function checkLaunchDefaults(input: LaunchDefaultsInput): Promise<LaunchDefaultsFieldError[]> {
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
