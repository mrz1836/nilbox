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

import type * as real from '../platform'
import { clockAt, FIXED_NOW, withClock } from './clock'
import {
  agentVersions,
  agents,
  appInfo,
  budgetGauge,
  checkResult,
  cloneTarget,
  fileCheck,
  importEntry,
  itemResolution,
  launchDefaults,
  launchPreview,
  launchResult,
  profile,
  prompt,
  promptBackup,
  prompts,
  removeResult,
  resolution,
  settings,
  snapshot,
  sweepReport,
  variables,
  worktreesSnapshot,
} from './fixtures'

type Platform = typeof real

// Loose is a platform function as the fake implements it: the real function's arguments,
// answered with a plain promise where the real one returns Wails' CancellablePromise. The
// callers treat any promise alike, and cancel only one that can be cancelled.
type Loose<F> = F extends (...args: infer A) => real.CancellablePromise<infer R> ? (...args: A) => Promise<R> : F

// FakePlatform is the platform with every function a mock of its Loose form, so a test
// that calls one with the wrong arguments, answers for one with the wrong value, or reads
// its calls (mock.calls, mock.lastCall) as the wrong types does not compile.
export type FakePlatform = { [K in keyof Platform]: Mock<Loose<Platform[K]>> }

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
    bugReportURL: () => Promise.resolve('https://github.com/acme-org/example-repo/issues/new?template=bug_report.yml'),

    getBudget: () => Promise.resolve(budgetGauge()),
    checkBudget: () => Promise.resolve(budgetGauge()),

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
    checkItemFiles: () => Promise.resolve(fileCheck()),

    listProfiles: () => Promise.resolve([profile(), profile({ agent: 'codex', folder: '~/.codex' })]),
    listAgents: () => Promise.resolve(agents()),

    launch: () => Promise.resolve(launchResult()),
    resolveLaunch: () => Promise.resolve(resolution()),
    resolveLaunches: (ids) => Promise.resolve((ids ?? []).map((itemId) => itemResolution({ itemId }))),
    previewLaunch: () => Promise.resolve(launchPreview()),
    cloneTarget: () => Promise.resolve(cloneTarget()),
    cloneRepo: () => Promise.resolve(cloneTarget()),

    listPrompts: () => Promise.resolve(prompts()),
    createPrompt: (input) => Promise.resolve(prompt({ ...fromInput(input), id: 9 })),
    updatePrompt: (id, input) => Promise.resolve(prompt({ ...fromInput(input), id })),
    deletePrompt: done,
    duplicatePrompt: (id) => Promise.resolve(prompt({ id })),
    resetPrompt: (id) => Promise.resolve(prompt({ id })),
    checkPrompt: () => Promise.resolve(checkResult()),
    listPromptVariables: () => Promise.resolve(variables()),
    getDefaultPrompts: () => Promise.resolve({ pr: 1, issue: 4 }),
    setDefaultPrompt: done,
    exportPrompts: (_path, ids) => Promise.resolve(ids === null || ids.length === 0 ? prompts().length : ids.length),
    listPromptBackups: () => Promise.resolve([promptBackup()]),
    previewPromptImport: () => Promise.resolve([importEntry()]),
    importPrompts: (chosen) => Promise.resolve({ added: chosen?.length ?? 0, replaced: 0, skipped: 0, first: 9 }),
    chooseFileToOpen: () => Promise.resolve(''),
    chooseFileToSave: () => Promise.resolve(''),

    listLaunchDefaults: () => Promise.resolve([]),
    checkLaunchDefaults: () => Promise.resolve([]),
    saveLaunchDefaults: () => Promise.resolve(launchDefaults()),
    removeLaunchDefaults: done,
    listLaunchTargets: () => Promise.resolve([]),

    getSettings: () => stored(),
    setTerminal: (terminal) => stored({ terminal }),
    setCloneRoot: (cloneRoot) => stored({ cloneRoot }),
    setDefaultAgent: (defaultAgent) => stored({ defaultAgent }),
    setDefaultProfile: (agent, name) => stored({ defaultProfiles: { ...settings().defaultProfiles, [agent]: name } }),
    setWorktreeDefault: (worktreeDefault) => stored({ worktreeDefault }),
    setWorktreeAutoCleanup: (worktreeAutoCleanup) => stored({ worktreeAutoCleanup }),
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
    getCollapsedSections: () => Promise.resolve([]),
    setSectionCollapsed: (section, collapsed) => stored({ collapsedSections: collapsed ? [section] : [] }),
    setProjectsTipDismissed: (projectsTipDismissed) => stored({ projectsTipDismissed }),
    getInboxOrder: () => Promise.resolve({ statusSorted: [], statusOrder: [] }),
    setStatusSort: (section, on) => stored({ inboxOrder: { statusSorted: on ? [section] : [], statusOrder: [] } }),
    setStatusOrder: (statusOrder) => stored({ inboxOrder: { statusSorted: [], statusOrder: statusOrder ?? [] } }),
    setWindowSize: (windowSize) => stored({ windowSize }),
    setBranchRules: (branches) => stored({ branches: { ...branches, protected: branches.protected ?? [] } }),

    listWorktrees: () => Promise.resolve(worktreesSnapshot()),
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

    listNotifications: () => Promise.resolve({ items: [], unread: 0 }),
    markNotificationsRead: done,
    clearNotifications: done,
    notificationBannerStatus: () => Promise.resolve('on'),
    sendTestNotification: done,

    openExternal: () => undefined,
    copyText: done,
    on: subscribe,
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

  // Each function is a mock whose default, which mockReset returns it to, is its answer.
  const platform = Object.fromEntries(
    Object.entries(answers(subscribe)).map(([name, answer]) => [name, vi.fn(answer)]),
  ) as FakePlatform

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
