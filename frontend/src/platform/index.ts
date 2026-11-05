// The platform module is the frontend's only door to the desktop shell: the Wails runtime
// and the generated bindings for the Go services. Everything else in src/ imports from
// here (an ESLint rule enforces it), so another shell or an OS port replaces this module's
// implementation and never its callers.

import { Browser, Events } from '@wailsio/runtime'

import { Service as AppInfo } from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
import type { Info } from '../../bindings/github.com/mrz1836/nilbox/internal/appinfo'
import { Service as Inbox } from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import type { Snapshot as InboxSnapshot } from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
import { Service as Launcher } from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import type { LaunchRequest, LaunchResult } from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
import { Service as Profiles } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import type { Profile } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
import { Service as SettingsService } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
import type { Settings } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
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
  Item as InboxItem,
  List as InboxList,
  Rate,
  RateGauge,
  Snapshot as InboxSnapshot,
} from '../../bindings/github.com/mrz1836/nilbox/internal/inbox'
export type { LaunchRequest, LaunchResult } from '../../bindings/github.com/mrz1836/nilbox/internal/launcher'
export type { Profile } from '../../bindings/github.com/mrz1836/nilbox/internal/profiles'
export type { Settings } from '../../bindings/github.com/mrz1836/nilbox/internal/settings'
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
