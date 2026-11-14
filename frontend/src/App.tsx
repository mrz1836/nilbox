import {
  Activity,
  lazy,
  Suspense,
  useDeferredValue,
  useEffect,
  useEffectEvent,
  useReducer,
  useRef,
  useState,
} from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { DataResetNotice } from './components/DataResetNotice'
import { ErrorBanner } from './components/ErrorBanner'
import { InboxView, type LaunchOptions, type Reveal } from './components/InboxView'
import { LaunchControls } from './components/LaunchControls'
import { QuitDialog } from './components/QuitDialog'
import type { SearchFieldProps } from './components/SearchField'
import type { SettingsSection } from './components/SettingsView'
import { defaultToastSeconds, Toast, type ToastMessage } from './components/Toast'
import { attempt, serial } from './lib/call'
import { filterInbox, inboxItems, inboxSpec, inboxSuggestions } from './lib/inboxFilter'
import { isFindKey, isTyping } from './lib/keys'
import { cloneWhere } from './lib/locations'
import { toggleFilter } from './lib/query'
import { opensRun } from './lib/notifications'
import type { ReleaseRequest } from './lib/release'
import type { RunRequest } from './lib/scriptRuns'
import { foundToolsText, scriptsBadge, showsScripts } from './lib/scriptsTab'
import { matchSettings } from './lib/settingsIndex'
import { itemWorktrees } from './lib/itemWorktrees'
import { runningSession, sessionsOf } from './lib/sessions'
import { useGitHubThreads } from './lib/useGitHubThreads'
import { useSessions } from './lib/useSessions'
import { agentName } from './lib/launch'
import { offersProjects } from './lib/projects'
import { useTextSize } from './lib/textSize'
import { defaultSnoozeDays, findItem } from './lib/triage'
import { useCollapsedSections } from './lib/useCollapsedSections'
import { useBudget } from './lib/useBudget'
import { useInbox } from './lib/useInbox'
import { useInboxOrder } from './lib/useInboxOrder'
import { useIntegrations } from './lib/useIntegrations'
import { useScriptRuns } from './lib/useScriptRuns'
import { SettingsQueue } from './lib/useSettingsSave'
import { useIssueDrafts } from './lib/issueDrafts'
import { useReviewDrafts } from './lib/reviewDrafts'
import { useLaunchConfig } from './lib/useLaunchConfig'
import { useNotifications } from './lib/useNotifications'
import { useQuery } from './lib/useQuery'
import { useTheme } from './lib/useTheme'
import { useTriage } from './lib/useTriage'
import { filterWorktrees, worktreeSpec, worktreeSuggestions } from './lib/worktreeFilter'
import type { Summary } from './lib/worktreeSummary'
import {
  getScriptRun,
  lastWorktreeSweep,
  on,
  openExternal,
  setProjectsTipDismissed,
  setIntegrationsAnnounced,
  setScriptsLastPage,
  setScriptsLastRepo,
  setScriptsSectionOpen,
  setShowRuns,
  type AfterMerge,
  type AgentID,
  type AppNotification,
  type IntegrationTools,
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
const ScriptsView = lazy(() => import('./components/scripts/ScriptsView'))
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
// follows; clear drops it once another view opens. A notification about a run, as a
// failed run's banner is, opens the run with openRun instead.
function useReveal(
  snapshot: InboxSnapshot | null,
  openInbox: () => void,
  openRun: (id: number, started: string) => void,
) {
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
  const showOnRequest = useEffectEvent((itemId: number, url: string, runId: number, runStarted: string) => {
    if (runId > 0) {
      openRun(runId, runStarted)

      return
    }

    show(itemId, url)
  })

  useEffect(
    () =>
      on('inbox:reveal', ({ itemId, url, runId, runStarted }) => {
        showOnRequest(itemId, url, runId, runStarted)
      }),
    [],
  )

  const clear = () => {
    setReveal(null)
  }

  return { reveal, show, clear }
}

// useFoundTools announces the Scripts tab once, as it first appears because a tool was
// found: shown is whether the tab shows, and ready whether that is known yet (the tools
// and the settings have loaded). A tab already showing when that is first known was
// announced before, so only a tab that appears while the window is open is announced, and
// only once while it is; announcedBefore, the stored integrations.announced, says it was
// announced at a past start, so it never is again. announced says the toast showed, which
// the window then stores.
function useFoundTools(tools: IntegrationTools | null, shown: boolean, ready: boolean, announcedBefore: boolean) {
  // was is whether the tab showed when last looked at, or null until it is known.
  const [was, setWas] = useState<boolean | null>(null)
  const [announced, setAnnounced] = useState(false)
  const [toast, setToast] = useState<ToastMessage | null>(null)

  if (ready && was !== shown) {
    setWas(shown)

    const text = foundToolsText(tools)

    if (was === false && shown && !announced && !announcedBefore && text !== '') {
      setAnnounced(true)
      setToast({ id: 0, text, tone: 'success' })
    }
  }

  const close = () => {
    setToast(null)
  }

  return { toast, close, announced }
}

// SearchView is a page the toolbar's field filters; Prompts searches in its own sidebar,
// and Scripts in each of its pages.
type SearchView = Exclude<View, 'prompts' | 'scripts'>

// placeholders say what the field does on each page.
const placeholders: Readonly<Record<SearchView, string>> = {
  inbox: 'Filter inbox',
  worktrees: 'Filter worktrees',
  settings: 'Find a setting',
}

// Nav is where the window is: the page it shows, and the pages visited so far, each of
// which stays mounted once visited; where closing the settings goes back to; the section
// the settings open at, with a count of their openings, so each opening turns to its
// section; whether the Worktrees view opens at its recently cleaned-up list, as a
// cleanup toast's View asks; and each page's query in the toolbar's field. The queries
// last only as long as the window: a filter that came back after a relaunch would make
// items look lost.
interface Nav {
  view: View
  visited: ReadonlySet<View>
  returnView: View
  settingsSection: SettingsSection
  settingsOpened: number
  historyFocus: boolean
  queries: Readonly<Record<SearchView, string>>
}

type NavAction =
  | { type: 'show'; view: View }
  | { type: 'settings'; section: SettingsSection }
  | { type: 'closeSettings' }
  | { type: 'cleanups' }
  | { type: 'reveal' }
  | { type: 'search'; view: SearchView; query: string }
  | { type: 'filterBy'; key: string; value: string }

const startNav: Nav = {
  view: 'inbox',
  visited: new Set(['inbox']),
  returnView: 'inbox',
  settingsSection: 'account',
  settingsOpened: 0,
  historyFocus: false,
  queries: { inbox: '', worktrees: '', settings: '' },
}

// visit shows a page, with any other change to where the window is. Leaving the settings,
// by any way out, forgets what was being found there.
function visit(nav: Nav, view: View, change: Partial<Nav> = {}): Nav {
  const visited = nav.visited.has(view) ? nav.visited : new Set([...nav.visited, view])
  const next = { ...nav, ...change, view, visited }

  return nav.view === 'settings' && view !== 'settings' && next.queries.settings !== ''
    ? { ...next, queries: { ...next.queries, settings: '' } }
    : next
}

// search sets one page's query.
function search(nav: Nav, view: SearchView, query: string): Nav {
  return nav.queries[view] === query ? nav : { ...nav, queries: { ...nav.queries, [view]: query } }
}

// navigate moves the window: show opens a page (and drops the cleaned-up list's focus);
// settings opens the settings at a section, remembering the page to go back to, unless
// they are open already; closeSettings goes back; cleanups opens the Worktrees view at
// its recently cleaned-up list; reveal opens the inbox unfiltered, so the item asked for
// is surely in it; search sets a page's query; and filterBy adds a filter to the inbox's
// query, or takes it away.
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
    case 'reveal':
      return visit(search(nav, 'inbox', ''), 'inbox', { historyFocus: false })
    case 'search':
      return search(nav, action.view, action.query)
    case 'filterBy':
      return search(nav, 'inbox', toggleFilter(nav.queries.inbox, action.key, action.value, inboxSpec))
  }
}

// useQueryOf is a page's query as its page filters by it: a step behind while the window
// is busy, so typing stays quick, but at once when cleared, so a reveal that clears the
// inbox's query finds its row in the same render.
function useQueryOf(query: string): string {
  const deferred = useDeferredValue(query)

  return query === '' ? '' : deferred
}

// App is the window's root: the toolbar over the active page. It holds the state the
// pages share: the theme, the inbox (the lists, the account, and the API budget) and its
// triage, the launch configuration (settings, profiles, prompts, launch defaults, and
// each item's resolved launch settings) that the rows, the Prompts page, and the settings
// page use, which inbox lists are collapsed, the automatic worktree cleanup's latest
// news, the notification history, the user's GitHub notifications, and the item the menu
// bar or a notification last asked to show.
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
  // reviewDrafts keeps what the user wrote in the review sheet, for the session.
  const reviewDrafts = useReviewDrafts()
  // issueDrafts keeps what the user wrote in the issue sheet, for the session.
  const issueDrafts = useIssueDrafts()
  // The text size follows the stored settings; until they load, it is the size the
  // window opened at (main.tsx).
  useTextSize(config.settings?.textSize)
  const sweeps = useWorktreeSweeps()
  // agentSessions are the agent sessions the inbox's cards follow, loaded as the window
  // opens and again whenever one changes.
  const agentSessions = useSessions()
  const notifications = useNotifications()
  // githubThreads are the user's GitHub notifications, as the app mirrors them: the
  // toolbar's button, the inbox's cards, and their settings share them.
  const githubThreads = useGitHubThreads()
  // settingsQueue is the one queue every settings save in the window goes through
  // (useSettingsSave), the toolbar's and the settings page's alike.
  const [settingsQueue] = useState(serial)
  // sections lives here rather than in the inbox page, so the lists stay as they were
  // left without reading them again.
  const sections = useCollapsedSections()
  const inboxOrder = useInboxOrder()
  // The tools the app runs, and the runs, decide whether the Scripts tab shows and what
  // its badge says; the tab itself reads both from here. The runs are listed only while
  // the tab shows.
  const integrations = useIntegrations()
  const scriptsShown = showsScripts(integrations.tools, config.settings?.integrations ?? null)
  const scriptRuns = useScriptRuns(scriptsShown)
  const found = useFoundTools(
    integrations.tools,
    scriptsShown,
    integrations.tools !== null && config.settings !== null,
    config.settings?.integrations.announced ?? false,
  )
  const [nav, go] = useReducer(navigate, startNav)
  // A Scripts tab gone, as when every integration was turned off, shows the inbox.
  const view = nav.view === 'scripts' && !scriptsShown ? 'inbox' : nav.view
  const inboxQuery = useQueryOf(nav.queries.inbox)
  const worktreesQuery = useQueryOf(nav.queries.worktrees)
  const settingsQuery = useQueryOf(nav.queries.settings)
  // onFilterBy is made once, so the inbox's cards, which take it, keep their identity.
  const [onFilterBy] = useState(() => (key: string, value: string) => {
    go({ type: 'filterBy', key, value })
  })
  // searchRef is the toolbar's field, which ⌘F focuses.
  const searchRef = useRef<HTMLInputElement>(null)

  // openInbox shows the inbox at an item the menu bar or a notification asked for. It
  // clears the inbox's filter, so the item is always found.
  const openInbox = () => {
    go({ type: 'reveal' })
  }

  // releaseRequest is the release sheet a merge's toast asked for last, and runRequest the
  // run a failed run's notification asked for last, which the Scripts tab opens; requests
  // numbers them.
  const [releaseRequest, setReleaseRequest] = useState<ReleaseRequest | null>(null)
  const [runRequest, setRunRequest] = useState<RunRequest | null>(null)
  const requests = useRef(0)

  // changes numbers the toasts the window raises itself, counting down, so they never meet
  // a sweep toast's: a change made in the Worktrees view, or a run a notification asked for
  // that can't open.
  const changes = useRef(0)

  // runToast says why the run a failed run's notification asked for didn't open.
  const [runToast, setRunToast] = useState<ToastMessage | null>(null)
  const sayOfRun = (text: string) => {
    changes.current -= 1
    setRunToast({ id: changes.current, text, tone: 'warning' })
  }

  // openRun opens Scripts → Runs with a run in its panel, as a failed run's notification
  // asks, once the run with that id says it started when the notification says: the history
  // hands an id out again once it is cleared, and a later run must never open in its place.
  // With the Scripts tab gone, or the run no longer in the history, a toast says so instead.
  const openRun = (id: number, started: string) => {
    if (!scriptsShown) {
      sayOfRun('Turn on an integration in Settings to see your runs.')

      return
    }

    void attempt(getScriptRun(id)).then((result) => {
      if (!result.ok || started === '' || result.data.startedAt !== started) {
        sayOfRun('That run is no longer in the history.')

        return
      }

      requests.current += 1
      setRunRequest({ id, seq: requests.current })
      go({ type: 'show', view: 'scripts' })
    })
  }
  const revealer = useReveal(inbox.snapshot, openInbox, openRun)
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

  const reportChange = (summary: Summary) => {
    // Its ids count down, so they never meet a sweep toast's.
    changes.current -= 1
    setChangeToast({ id: changes.current, text: summary.title, detail: summary.detail, tone: summary.tone })
  }

  const closeChangeToast = () => {
    setChangeToast(null)
  }

  // openRelease opens Scripts → Release at a repository's sheet, as a merge offers.
  const [openRelease] = useState(() => (offer: AfterMerge) => {
    requests.current += 1
    setReleaseRequest({ repo: offer.repo, dir: offer.dir, branch: offer.branch, seq: requests.current })
    go({ type: 'show', view: 'scripts' })
  })

  // foundToast announces the Scripts tab, over every other toast, with a way to open it.
  const foundToast =
    found.toast === null
      ? null
      : {
          ...found.toast,
          action: {
            label: 'Open Scripts',
            onClick: () => {
              found.close()
              changeView('scripts')
            },
          },
        }

  const changeShown =
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
  // A run that couldn't open is said over a change's or a sweep's toast, as it answers the
  // click just made.
  const closeRunToast = () => {
    setRunToast(null)
  }
  const shownToast = foundToast ?? runToast ?? changeShown
  const closeToast =
    foundToast !== null
      ? found.close
      : runToast !== null
        ? closeRunToast
        : changeToast === null
          ? closeSweepToast
          : closeChangeToast
  // toastSeconds is how long every toast shows, the inbox's too, as set in Settings.
  const toastSeconds = config.settings?.toastSeconds ?? defaultToastSeconds

  const openSettings = () => {
    go({ type: 'settings', section: 'account' })
  }

  // openSettingsAt opens the settings at a section, as the Scripts tab's fixes do.
  const openSettingsAt = (section: SettingsSection) => {
    go({ type: 'settings', section })
  }

  // openSnoozeSettings opens the setting for the Snooze button's length.
  const openSnoozeSettings = () => {
    go({ type: 'settings', section: 'snoozing' })
  }

  // openRepoRuleSettings opens the pinned and muted repositories and orgs. It is made
  // once, so the inbox's cards, which take it, keep their identity.
  const [openRepoRuleSettings] = useState(() => () => {
    go({ type: 'settings', section: 'repositories' })
  })

  // openFilesSettings opens what counts as a sensitive file.
  const openFilesSettings = () => {
    go({ type: 'settings', section: 'files' })
  }

  // openNotificationSettings opens the menu bar and notification settings.
  const openNotificationSettings = () => {
    go({ type: 'settings', section: 'notifications' })
  }

  // openNotification shows a history entry's item, or opens its page or its run.
  const openNotification = (entry: AppNotification) => {
    if (opensRun(entry)) {
      openRun(entry.runId, entry.runStarted)

      return
    }

    showItem(entry.itemId, entry.url)
  }

  // openGitHubThreadsSettings opens the GitHub notification settings.
  const openGitHubThreadsSettings = () => {
    go({ type: 'settings', section: 'github' })
  }

  const closeSettings = () => {
    go({ type: 'closeSettings' })
  }

  // ⌘, opens the settings page, as in every Mac app. ⌘F, or / while not typing, focuses
  // the toolbar's field with its text selected; on Prompts, ⌘F belongs to the page's own
  // search.
  const onShortcut = useEffectEvent((e: KeyboardEvent) => {
    if (e.metaKey && e.key === ',') {
      e.preventDefault()
      openSettings()

      return
    }

    const slash = e.key === '/' && !e.metaKey && !e.ctrlKey && !e.altKey && !e.defaultPrevented && !isTyping(e)

    if (view !== 'prompts' && view !== 'scripts' && (isFindKey(e) || slash)) {
      e.preventDefault()
      searchRef.current?.focus()
      searchRef.current?.select()
    }
  })

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      onShortcut(e)
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

  // remember stores a change to the window's memory of the Scripts tab: that it was
  // announced, or the page or clone it reopens on. What shows has changed already; a store
  // that fails only means the next start doesn't remember it.
  const remember = (store: Promise<Settings>, what: string) => {
    void attempt(store).then((result) => {
      if (result.ok) {
        saveSettings(result.data)
      } else {
        console.error(`could not remember ${what}:`, result.error)
      }
    })
  }

  // Once the found-tools toast shows, that is stored, so a later start never shows it again.
  const storeAnnounced = useEffectEvent(() => {
    remember(setIntegrationsAnnounced(true), 'that the Scripts tab was announced')
  })

  useEffect(() => {
    if (found.announced) {
      storeAnnounced()
    }
  }, [found.announced])

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
          // A location added from a launch changes nothing the inbox shows, so the stored
          // settings are only recorded; the launch options stay the same between renders.
          onSettingsSaved: setSettings,
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
        onSettingsSaved={launch.onSettingsSaved}
        initial={{ worktree: true }}
        running={runningSession(sessionsOf(agentSessions.byItem, found))}
      />
    )
  }

  const account = inbox.snapshot?.account ?? null
  const mode = (page: View) => (view === page ? 'visible' : 'hidden')
  const viewer = account?.login ?? ''
  const inboxFilter = triaged === null ? null : filterInbox(triaged, inboxQuery, viewer)
  const wtRepos = sweeps.snapshot?.repos ?? []
  const wtOptions = { agentName: (id: AgentID) => agentName(config.agents, id) }

  // searchFor is the toolbar's field on a page that has one: its query, what it counts,
  // and what it suggests.
  const searchFor = (page: SearchView): SearchFieldProps => {
    const base = {
      value: nav.queries[page],
      onChange: (query: string) => {
        go({ type: 'search', view: page, query })
      },
      placeholder: placeholders[page],
    }

    switch (page) {
      case 'inbox': {
        const items = triaged === null ? [] : inboxItems(triaged)

        return {
          ...base,
          spec: inboxSpec,
          matches: inboxFilter?.matches ?? null,
          suggest: (src, caret) => inboxSuggestions(items, src, caret, viewer),
        }
      }
      case 'worktrees':
        return {
          ...base,
          spec: worktreeSpec,
          matches:
            sweeps.snapshot === null ? null : (filterWorktrees(wtRepos, worktreesQuery, wtOptions)?.matches ?? null),
          suggest: (src, caret) => worktreeSuggestions(wtRepos, src, caret, wtOptions),
        }
      case 'settings':
        return { ...base, matches: matchSettings(settingsQuery)?.matches ?? null }
    }
  }

  // The window is one column the height of the screen: the toolbar, then the page area,
  // which alone scrolls (a page scrolls in it, or in panes of its own), so the toolbar never
  // leaves the window. The column is the containing block of everything absolutely
  // positioned inside it that no nearer box holds, and clips it. Such an element, like a
  // label only screen readers see, sits where it would with every pane scrolled to its top,
  // so one far down a long page (the protected branches in Settings → Worktrees) reached
  // past the window and let the document scroll, toolbar and all. Clip, where hidden would
  // leave the column itself scrollable, leaves scrollIntoView and focus nothing to scroll.
  return (
    <SettingsQueue value={settingsQueue}>
      <div className="relative flex h-screen flex-col overflow-clip bg-background text-text">
        <AppHeader
          view={view}
          onViewChange={changeView}
          account={account}
          gauge={budget.gauge}
          onBudgetCheck={budget.check}
          inboxCount={triaged?.count ?? 0}
          worktreeCount={sweeps.count}
          worktreeAttention={sweeps.attention}
          scripts={scriptsShown ? scriptsBadge(scriptRuns.runs, integrations.tools) : null}
          onOpenSettings={openSettings}
          notifications={notifications}
          onOpenNotification={openNotification}
          onNotificationSettings={openNotificationSettings}
          githubThreads={githubThreads}
          settings={config.settings}
          onGitHubThreadsSettings={openGitHubThreadsSettings}
          onSettingsSaved={saveSettings}
          search={view === 'prompts' || view === 'scripts' ? undefined : searchFor(view)}
          searchRef={searchRef}
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
              sessions={agentSessions.byItem}
              snoozeDays={config.settings?.snoozeDays ?? defaultSnoozeDays}
              toastSeconds={toastSeconds}
              onSnoozeSettings={openSnoozeSettings}
              onFilesSettings={openFilesSettings}
              collapsed={sections.collapsed}
              onCollapse={sections.setCollapsed}
              reveal={revealer.reveal}
              showing={view === 'inbox'}
              offerProjects={offersProjects(inbox.snapshot, config.settings)}
              onDismissProjects={dismissProjects}
              inboxOrder={inboxOrder}
              filter={inboxFilter}
              onFilterBy={onFilterBy}
              onClearFilter={() => {
                go({ type: 'search', view: 'inbox', query: '' })
              }}
              drafts={{ review: reviewDrafts, issue: issueDrafts }}
              onManageReplies={openPrompts}
              onRelease={openRelease}
              repoRules={config.settings?.repoRules}
              onSettingsSaved={setSettings}
              onRepoRuleSettings={openRepoRuleSettings}
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
                  query={worktreesQuery}
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
                    replies={config.replies}
                    defaults={config.defaultPrompts}
                    variables={config.variables}
                    items={config.items}
                    onChanged={reload}
                  />
                </div>
              </Suspense>
            </Activity>
          )}
          {nav.visited.has('scripts') && (
            <Activity mode={mode('scripts')}>
              <Suspense fallback={<PageLoading />}>
                <div className="h-full">
                  <ScriptsView
                    terminal={config.settings?.terminal ?? null}
                    runsIn={config.settings?.integrations.runsIn ?? 'app'}
                    where={config.settings === null ? '' : cloneWhere(config.settings)}
                    integrations={integrations}
                    scriptsSwitch={config.settings?.integrations.switches.scripts ?? 'auto'}
                    runs={scriptRuns}
                    request={releaseRequest}
                    runRequest={runRequest}
                    onOpenSettings={openSettingsAt}
                    lastPage={config.settings?.integrations.lastPage ?? ''}
                    lastRepo={config.settings?.integrations.lastRepo ?? ''}
                    onPage={(page) => {
                      remember(setScriptsLastPage(page), 'the Scripts page shown')
                    }}
                    onRepo={(dir) => {
                      remember(setScriptsLastRepo(dir), 'the clone Repo scripts shows')
                    }}
                    sections={config.settings?.integrations.sections ?? {}}
                    onSection={(section, open) => {
                      remember(setScriptsSectionOpen(section, open), 'a Scripts section opened or folded')
                    }}
                    showRuns={config.settings?.integrations.showRuns ?? false}
                    onShowRuns={(on) => {
                      remember(setShowRuns(on), 'whether a run shows beside Repo scripts')
                    }}
                    repoRules={config.settings?.repoRules}
                    onSettingsSaved={setSettings}
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
                  query={settingsQuery}
                  githubThreads={githubThreads}
                />
              </Suspense>
            </Activity>
          )}
        </main>
        <Toast toast={shownToast} seconds={toastSeconds} onClose={closeToast} />
        <QuitDialog />
      </div>
    </SettingsQueue>
  )
}
