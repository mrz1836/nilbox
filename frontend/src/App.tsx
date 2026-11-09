import { Activity, lazy, Suspense, useEffect, useEffectEvent, useReducer, useRef, useState } from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { DataResetNotice } from './components/DataResetNotice'
import { ErrorBanner } from './components/ErrorBanner'
import { InboxView, type LaunchOptions, type Reveal } from './components/InboxView'
import { LaunchControls } from './components/LaunchControls'
import type { SettingsSection } from './components/SettingsView'
import { Toast, type ToastMessage } from './components/Toast'
import { attempt } from './lib/call'
import { itemWorktrees } from './lib/itemWorktrees'
import { agentName } from './lib/launch'
import { offersProjects } from './lib/projects'
import { useTextSize } from './lib/textSize'
import { defaultSnoozeDays, findItem } from './lib/triage'
import { useCollapsedSections } from './lib/useCollapsedSections'
import { useBudget } from './lib/useBudget'
import { useInbox } from './lib/useInbox'
import { useInboxOrder } from './lib/useInboxOrder'
import { useLaunchConfig } from './lib/useLaunchConfig'
import { useNotifications } from './lib/useNotifications'
import { useQuery } from './lib/useQuery'
import { useTheme } from './lib/useTheme'
import { useTriage } from './lib/useTriage'
import type { Summary } from './lib/worktreeSummary'
import {
  lastWorktreeSweep,
  on,
  openExternal,
  setProjectsTipDismissed,
  type AppNotification,
  type InboxSnapshot,
  type LinkedItem,
  type RemoveResult,
  type Settings,
  type SweepReport,
  type WorktreesSnapshot,
} from './platform'

// The pages besides the inbox load on their first visit, so the code the window runs
// before its first paint is the inbox's alone. The inbox itself is never behind a lazy
// boundary: main.tsx reports the first paint once it has rendered.
const PromptsView = lazy(() => import('./components/PromptsView').then((m) => ({ default: m.PromptsView })))
const SettingsView = lazy(() => import('./components/SettingsView').then((m) => ({ default: m.SettingsView })))
const WorktreesView = lazy(() => import('./components/WorktreesView').then((m) => ({ default: m.WorktreesView })))

// PageLoading stands in for a page while its code loads, the first time it opens.
function PageLoading() {
  return (
    <p role="status" className="p-4 text-sm text-muted">
      Loading…
    </p>
  )
}

// inboxKey is the part of the settings the stored inbox shows (internal/inbox's
// settingsKey): the trusted bots decide each card's trust, and the sensitive-file
// settings what each card points out.
function inboxKey(settings: Settings): string {
  return JSON.stringify([settings.trustedBots, settings.sensitiveFiles])
}

// cleanedToast tells what an automatic sweep cleaned up.
function cleanedToast(seq: number, cleaned: readonly RemoveResult[]): ToastMessage {
  const n = cleaned.length

  return {
    id: seq,
    text: n === 1 ? 'Cleaned up a finished worktree' : `Cleaned up ${String(n)} finished worktrees`,
    detail: cleaned.map((r) => (r.item === '' ? r.displayPath : r.item)).join(', '),
  }
}

// useWorktreeSweeps follows the automatic worktree cleanup: how many linked worktrees are
// on disk and how many of them are done on GitHub, the latest sweep that changed
// something (so an open Worktrees view lists again), and a toast for each sweep that
// removed a worktree.
function useWorktreeSweeps() {
  const sweep = useQuery(() => lastWorktreeSweep(), [], { log: 'could not read the latest worktree sweep' })
  const readOnUpdate = useEffectEvent(() => {
    void sweep.refetch()
  })

  useEffect(
    () =>
      on('worktrees:updated', () => {
        readOnUpdate()
      }),
    [],
  )

  const [counts, setCounts] = useState({ count: 0, attention: 0 })
  // reported is the latest sweep reported, so each one is reported once.
  const [reported, setReported] = useState<SweepReport | null>(null)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  const report = sweep.data

  if (report !== undefined && report.seq > (reported?.seq ?? 0)) {
    setReported(report)
    setCounts({ count: report.worktreeCount, attention: report.leftover })

    if (report.cleaned.length > 0) {
      setToast(cleanedToast(report.seq, report.cleaned))
    }
  }

  // snapshot is the Worktrees view's latest listing, which the inbox's rows read their
  // worktrees from; null until the view has listed. Nothing lists for the inbox alone.
  const [snapshot, setSnapshot] = useState<WorktreesSnapshot | null>(null)

  // listed counts from a listing of the Worktrees view, which sees the same worktrees.
  const listed = (snapshot: WorktreesSnapshot) => {
    setSnapshot(snapshot)
    setCounts({ count: snapshot.worktreeCount, attention: snapshot.finishedCount + snapshot.reviewCount })
  }

  const closeToast = () => {
    setToast(null)
  }

  return { ...counts, seq: reported?.seq ?? 0, toast, listed, closeToast, snapshot }
}

// useReveal shows an item in the inbox when asked: by the menu bar, a notification, or an
// entry in the notification history. An item no longer in the inbox opens on GitHub
// instead. show opens the inbox at the item, so reveal is the request the inbox view
// follows; clear drops it once another view opens.
function useReveal(snapshot: InboxSnapshot | null, openInbox: () => void) {
  const [reveal, setReveal] = useState<Reveal | null>(null)
  // requests numbers the requests, so asking for the same item again shows it again.
  const requests = useRef(0)

  const show = (itemId: number, url: string) => {
    if (itemId > 0 && snapshot !== null && findItem(snapshot, itemId) !== undefined) {
      requests.current += 1
      openInbox()
      setReveal({ id: itemId, seq: requests.current })

      return
    }

    if (url !== '') {
      openExternal(url)
    }
  }

  // The menu bar's request is read against the inbox as shown when it arrives.
  const showOnRequest = useEffectEvent((itemId: number, url: string) => {
    show(itemId, url)
  })

  useEffect(
    () =>
      on('inbox:reveal', ({ itemId, url }) => {
        showOnRequest(itemId, url)
      }),
    [],
  )

  const clear = () => {
    setReveal(null)
  }

  return { reveal, show, clear }
}

// Nav is where the window is: the page it shows, and the pages visited so far, each of
// which stays mounted once visited; where closing the settings goes back to; the section
// the settings open at, with a count of their openings, so each opening turns to its
// section; and whether the Worktrees view opens at its recently cleaned-up list, as a
// cleanup toast's View asks.
interface Nav {
  view: View
  visited: ReadonlySet<View>
  returnView: View
  settingsSection: SettingsSection
  settingsOpened: number
  historyFocus: boolean
}

type NavAction =
  | { type: 'show'; view: View }
  | { type: 'settings'; section: SettingsSection }
  | { type: 'closeSettings' }
  | { type: 'cleanups' }

const startNav: Nav = {
  view: 'inbox',
  visited: new Set(['inbox']),
  returnView: 'inbox',
  settingsSection: 'account',
  settingsOpened: 0,
  historyFocus: false,
}

// visit shows a page, with any other change to where the window is.
function visit(nav: Nav, view: View, change: Partial<Nav> = {}): Nav {
  const visited = nav.visited.has(view) ? nav.visited : new Set([...nav.visited, view])

  return { ...nav, ...change, view, visited }
}

// navigate moves the window: show opens a page (and drops the cleaned-up list's focus);
// settings opens the settings at a section, remembering the page to go back to, unless
// they are open already; closeSettings goes back; cleanups opens the Worktrees view at
// its recently cleaned-up list.
function navigate(nav: Nav, action: NavAction): Nav {
  switch (action.type) {
    case 'show':
      return visit(nav, action.view, { historyFocus: false })
    case 'settings':
      return nav.view === 'settings'
        ? nav
        : visit(nav, 'settings', {
            returnView: nav.view,
            settingsSection: action.section,
            settingsOpened: nav.settingsOpened + 1,
          })
    case 'closeSettings':
      return visit(nav, nav.returnView)
    case 'cleanups':
      return visit(nav, 'worktrees', { historyFocus: true })
  }
}

// App is the window's root: the toolbar over the active page. It holds the state the
// pages share: the theme, the inbox (the lists, the account, and the API budget) and its
// triage, the launch configuration (settings, profiles, prompts, launch defaults, and
// each item's resolved launch settings) that the rows, the Prompts page, and the settings
// page use, which inbox lists are collapsed, the automatic worktree cleanup's latest
// news, the notification history, and the item the menu bar or a notification last
// asked to show.
//
// Each page stays mounted once visited, hidden while another shows (Activity), so it
// comes back as it was left: a list's tab, an unsaved prompt draft. A hidden page runs no
// effects, so its listeners stop and the worktrees are listed again each time their view
// opens. The settings turn to the section asked for at every opening.
export function App() {
  const theme = useTheme()
  const inbox = useInbox()
  const budget = useBudget()
  const triaged = useTriage(inbox.snapshot)
  const config = useLaunchConfig(inbox.snapshot)
  // The text size follows the stored settings; until they load, it is the size the
  // window opened at (main.tsx).
  useTextSize(config.settings?.textSize)
  const sweeps = useWorktreeSweeps()
  const notifications = useNotifications()
  // sections lives here rather than in the inbox page, so the lists stay as they were
  // left without reading them again.
  const sections = useCollapsedSections()
  const inboxOrder = useInboxOrder()
  const [nav, go] = useReducer(navigate, startNav)
  const { view } = nav

  const openInbox = () => {
    go({ type: 'show', view: 'inbox' })
  }
  const revealer = useReveal(inbox.snapshot, openInbox)
  const { clear: clearReveal, show: showItem } = revealer

  const changeView = (next: View) => {
    clearReveal()
    go({ type: 'show', view: next })
  }

  const { closeToast: closeSweepToast } = sweeps

  // viewCleanups opens the Worktrees view at the recently cleaned-up list.
  const viewCleanups = () => {
    closeSweepToast()
    go({ type: 'cleanups' })
  }

  // sweepToast offers View on a cleanup toast, unless the Worktrees view is already open.
  const sweepToast =
    sweeps.toast === null || view === 'worktrees'
      ? sweeps.toast
      : { ...sweeps.toast, action: { label: 'View', onClick: viewCleanups } }

  // changeToast confirms a change made in the Worktrees view as it finishes: what it did,
  // or why it failed. It shows over a sweep's toast, and offers View once another view is
  // open.
  const [changeToast, setChangeToast] = useState<ToastMessage | null>(null)
  const changes = useRef(0)

  const reportChange = (summary: Summary) => {
    // Its ids count down, so they never meet a sweep toast's.
    changes.current -= 1
    setChangeToast({ id: changes.current, text: summary.title, detail: summary.detail, tone: summary.tone })
  }

  const closeChangeToast = () => {
    setChangeToast(null)
  }

  const shownToast =
    changeToast === null
      ? sweepToast
      : view === 'worktrees'
        ? changeToast
        : {
            ...changeToast,
            action: {
              label: 'View',
              onClick: () => {
                closeChangeToast()
                changeView('worktrees')
              },
            },
          }

  const openSettings = () => {
    go({ type: 'settings', section: 'account' })
  }

  // openSnoozeSettings opens the setting for the Snooze button's length.
  const openSnoozeSettings = () => {
    go({ type: 'settings', section: 'snoozing' })
  }

  // openFilesSettings opens what counts as a sensitive file.
  const openFilesSettings = () => {
    go({ type: 'settings', section: 'files' })
  }

  // openNotificationSettings opens the menu bar and notification settings.
  const openNotificationSettings = () => {
    go({ type: 'settings', section: 'notifications' })
  }

  // openNotification shows a history entry's item.
  const openNotification = (entry: AppNotification) => {
    showItem(entry.itemId, entry.url)
  }

  const closeSettings = () => {
    go({ type: 'closeSettings' })
  }

  // ⌘, opens the settings page, as in every Mac app.
  const openSettingsOnKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.metaKey && e.key === ',') {
      e.preventDefault()
      openSettings()
    }
  })

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      openSettingsOnKey(e)
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

  const { reload, setSettings } = config
  const { reload: reloadInbox } = inbox

  // saveSettings records the settings a setter stored and returned. The stored inbox is
  // read again only when a setting it shows changed.
  const saveSettings = (next: Settings) => {
    const was = config.settings

    setSettings(next)

    if (was === null || inboxKey(was) !== inboxKey(next)) {
      reloadInbox()
    }
  }

  const openPrompts = () => {
    changeView('prompts')
  }

  // dismissProjects stores that the user put away the inbox's offer to show issue
  // projects. The inbox hides it at once; a store that fails only means it is offered
  // again at the next start.
  const dismissProjects = () => {
    void attempt(setProjectsTipDismissed(true)).then((result) => {
      if (result.ok) {
        saveSettings(result.data)
      } else {
        console.error('could not store that the projects tip was put away:', result.error)
      }
    })
  }

  const launch: LaunchOptions | null =
    config.settings === null
      ? null
      : {
          agents: config.agents,
          profiles: config.profiles,
          prompts: config.prompts,
          launchDefaults: config.launchDefaults,
          onDefaultsSaved: reload,
          onManagePrompts: openPrompts,
        }
  // worktrees are the inbox items' worktrees, as the Worktrees view last listed them, apart
  // from launch so a listing renders only the rows whose worktree it lists.
  const worktrees = itemWorktrees(sweeps.snapshot)

  // launchFor is the launch control for a worktree's item, when the inbox lists it: it
  // launches in the item's worktree, which resumes it.
  const launchFor = (linked: LinkedItem) => {
    const target = `${linked.owner}/${linked.name}`.toLowerCase()
    const found = config.items.find(
      (i) => i.kind === linked.kind && i.number === linked.number && i.repo.toLowerCase() === target,
    )

    if (launch === null || found === undefined) {
      return null
    }

    return (
      <LaunchControls
        item={found}
        resolution={config.resolutions.get(found.id) ?? null}
        agents={launch.agents}
        profiles={launch.profiles}
        prompts={launch.prompts}
        launchDefaults={launch.launchDefaults}
        onDefaultsSaved={launch.onDefaultsSaved}
        onManagePrompts={launch.onManagePrompts}
        initial={{ worktree: true }}
        menuAlign="left"
      />
    )
  }

  const account = inbox.snapshot?.account ?? null
  const mode = (page: View) => (view === page ? 'visible' : 'hidden')

  return (
    <div className="flex h-screen flex-col bg-background text-text">
      <AppHeader
        view={view}
        onViewChange={changeView}
        account={account}
        gauge={budget.gauge}
        onBudgetCheck={budget.check}
        inboxCount={triaged?.count ?? 0}
        worktreeCount={sweeps.count}
        worktreeAttention={sweeps.attention}
        onOpenSettings={openSettings}
        notifications={notifications}
        onOpenNotification={openNotification}
        onNotificationSettings={openNotificationSettings}
      />
      <DataResetNotice />
      {config.error !== '' && (
        <ErrorBanner className="mx-4 mt-4">Could not load the launch settings: {config.error}</ErrorBanner>
      )}
      <main className="min-h-0 flex-1 overflow-y-auto">
        <Activity mode={mode('inbox')}>
          <InboxView
            inbox={inbox}
            triaged={triaged}
            launch={launch}
            resolutions={config.resolutions}
            worktrees={worktrees}
            snoozeDays={config.settings?.snoozeDays ?? defaultSnoozeDays}
            onSnoozeSettings={openSnoozeSettings}
            onFilesSettings={openFilesSettings}
            collapsed={sections.collapsed}
            onCollapse={sections.setCollapsed}
            reveal={revealer.reveal}
            showing={view === 'inbox'}
            offerProjects={offersProjects(inbox.snapshot, config.settings)}
            onDismissProjects={dismissProjects}
            inboxOrder={inboxOrder}
          />
        </Activity>
        {nav.visited.has('worktrees') && (
          <Activity mode={mode('worktrees')}>
            <Suspense fallback={<PageLoading />}>
              <WorktreesView
                reloadKey={sweeps.seq}
                onSnapshot={sweeps.listed}
                focusHistory={nav.historyFocus}
                agentName={(id) => agentName(config.agents, id)}
                launchFor={launchFor}
                branchRules={config.settings?.branches}
                onSummary={reportChange}
              />
            </Suspense>
          </Activity>
        )}
        {nav.visited.has('prompts') && (
          <Activity mode={mode('prompts')}>
            <Suspense fallback={<PageLoading />}>
              <div className="h-full">
                <PromptsView
                  agents={config.agents}
                  prompts={config.prompts}
                  defaults={config.defaultPrompts}
                  variables={config.variables}
                  items={config.items}
                  onChanged={reload}
                />
              </div>
            </Suspense>
          </Activity>
        )}
        {nav.visited.has('settings') && (
          <Activity mode={mode('settings')}>
            <Suspense fallback={<PageLoading />}>
              <SettingsView
                initialSection={nav.settingsSection}
                opened={nav.settingsOpened}
                settings={config.settings}
                agents={config.agents}
                profiles={config.profiles}
                prompts={config.prompts}
                defaultPrompts={config.defaultPrompts}
                launchDefaults={config.launchDefaults}
                account={account}
                projectAccess={inbox.snapshot?.projectAccess ?? ''}
                theme={theme.theme}
                onThemeChange={theme.chooseTheme}
                onSaved={saveSettings}
                onLaunchChanged={reload}
                onClose={closeSettings}
              />
            </Suspense>
          </Activity>
        )}
      </main>
      <Toast toast={shownToast} onClose={changeToast === null ? closeSweepToast : closeChangeToast} />
    </div>
  )
}
