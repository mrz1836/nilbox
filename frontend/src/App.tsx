import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { DataResetNotice } from './components/DataResetNotice'
import { ErrorBanner } from './components/ErrorBanner'
import { InboxView, type LaunchOptions, type Reveal } from './components/InboxView'
import { PromptsView } from './components/PromptsView'
import { SettingsView, type SettingsSection } from './components/SettingsView'
import { Toast, type ToastMessage } from './components/Toast'
import { WorktreesView } from './components/WorktreesView'
import { defaultSnoozeDays, findItem, inboxCount } from './lib/triage'
import { useCollapsedSections } from './lib/useCollapsedSections'
import { useInbox } from './lib/useInbox'
import { useLaunchConfig } from './lib/useLaunchConfig'
import { useNotifications } from './lib/useNotifications'
import { useQuery } from './lib/useQuery'
import { useTheme } from './lib/useTheme'
import {
  lastWorktreeSweep,
  on,
  openExternal,
  type AppNotification,
  type InboxSnapshot,
  type RemoveResult,
  type Settings,
  type SweepReport,
  type WorktreesSnapshot,
} from './platform'

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

  // listed counts from a listing of the Worktrees view, which sees the same worktrees.
  const listed = useCallback((snapshot: WorktreesSnapshot) => {
    setCounts({ count: snapshot.worktreeCount, attention: snapshot.finishedCount + snapshot.reviewCount })
  }, [])

  const closeToast = useCallback(() => {
    setToast(null)
  }, [])

  return { ...counts, seq: reported?.seq ?? 0, toast, listed, closeToast }
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

  const clear = useCallback(() => {
    setReveal(null)
  }, [])

  return { reveal, show, clear }
}

// App is the window's root: the toolbar over the active page. It holds the state the
// pages share: the theme, the inbox (the lists, the account, and the API budget), the
// launch configuration (settings, profiles, prompts, launch defaults, and each item's
// resolved launch settings) that the rows, the Prompts page, and the settings page use,
// which inbox lists are collapsed, the automatic worktree cleanup's latest news, the
// notification history, and the item the menu bar or a notification last asked to show.
export function App() {
  const theme = useTheme()
  const inbox = useInbox()
  const config = useLaunchConfig(inbox.snapshot)
  const sweeps = useWorktreeSweeps()
  const notifications = useNotifications()
  // sections lives here rather than in the inbox page, so leaving the page and coming
  // back keeps the lists as they were without reading them again.
  const sections = useCollapsedSections()
  const [view, setView] = useState<View>('inbox')
  // returnView is where closing the settings page goes back to.
  const [returnView, setReturnView] = useState<View>('inbox')
  // settingsSection is the section the settings page opens at.
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('account')
  // promptsOpened keeps the Prompts page mounted once opened, so an unsaved draft
  // survives switching tabs.
  const [promptsOpened, setPromptsOpened] = useState(false)
  // historyFocus opens the Worktrees view at its recently cleaned-up list, as a cleanup
  // toast's View asks; switching views clears it.
  const [historyFocus, setHistoryFocus] = useState(false)

  const openInbox = useCallback(() => {
    setHistoryFocus(false)
    setView('inbox')
  }, [])
  const revealer = useReveal(inbox.snapshot, openInbox)
  const { clear: clearReveal } = revealer

  const changeView = useCallback(
    (next: View) => {
      if (next === 'prompts') {
        setPromptsOpened(true)
      }

      setHistoryFocus(false)
      clearReveal()
      setView(next)
    },
    [clearReveal],
  )

  const { closeToast: closeSweepToast } = sweeps

  // viewCleanups opens the Worktrees view at the recently cleaned-up list.
  const viewCleanups = useCallback(() => {
    closeSweepToast()
    setHistoryFocus(true)
    setView('worktrees')
  }, [closeSweepToast])

  // sweepToast offers View on a cleanup toast, unless the Worktrees view is already open.
  const sweepToast = useMemo(
    () =>
      sweeps.toast === null || view === 'worktrees'
        ? sweeps.toast
        : { ...sweeps.toast, action: { label: 'View', onClick: viewCleanups } },
    [sweeps.toast, view, viewCleanups],
  )

  const openSettingsAt = useCallback(
    (section: SettingsSection) => {
      if (view !== 'settings') {
        setSettingsSection(section)
        setReturnView(view)
        setView('settings')
      }
    },
    [view],
  )

  const openSettings = useCallback(() => {
    openSettingsAt('account')
  }, [openSettingsAt])

  // openSnoozeSettings opens the setting for the Snooze button's length.
  const openSnoozeSettings = useCallback(() => {
    openSettingsAt('snoozing')
  }, [openSettingsAt])

  // openFilesSettings opens what counts as a sensitive file.
  const openFilesSettings = useCallback(() => {
    openSettingsAt('files')
  }, [openSettingsAt])

  // openNotificationSettings opens the menu bar and notification settings.
  const openNotificationSettings = useCallback(() => {
    openSettingsAt('notifications')
  }, [openSettingsAt])

  const { show: showItem } = revealer

  // openNotification shows a history entry's item.
  const openNotification = useCallback(
    (entry: AppNotification) => {
      showItem(entry.itemId, entry.url)
    },
    [showItem],
  )

  const closeSettings = useCallback(() => {
    setView(returnView)
  }, [returnView])

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

  const openPrompts = useCallback(() => {
    changeView('prompts')
  }, [changeView])

  const launch = useMemo<LaunchOptions | null>(
    () =>
      config.settings === null
        ? null
        : {
            profiles: config.profiles,
            prompts: config.prompts,
            launchDefaults: config.launchDefaults,
            resolutions: config.resolutions,
            onDefaultsSaved: reload,
            onManagePrompts: openPrompts,
          },
    [config.settings, config.profiles, config.prompts, config.launchDefaults, config.resolutions, reload, openPrompts],
  )

  const account = inbox.snapshot?.account ?? null
  const inboxItems = useMemo(
    () => (inbox.snapshot === null ? 0 : inboxCount(inbox.snapshot, inbox.now)),
    [inbox.snapshot, inbox.now],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-text">
      <AppHeader
        view={view}
        onViewChange={changeView}
        account={account}
        gauge={inbox.snapshot?.rate ?? null}
        now={inbox.now}
        inboxCount={inboxItems}
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
        {/* Only the active page is mounted, so the worktrees are listed when that view opens.
            The Prompts page stays mounted once opened, hidden, to keep an unsaved draft. */}
        {view === 'inbox' && (
          <InboxView
            inbox={inbox}
            launch={launch}
            snoozeDays={config.settings?.snoozeDays ?? defaultSnoozeDays}
            onSnoozeSettings={openSnoozeSettings}
            onFilesSettings={openFilesSettings}
            collapsed={sections.collapsed}
            onCollapse={sections.setCollapsed}
            reveal={revealer.reveal}
          />
        )}
        {view === 'worktrees' && (
          <WorktreesView reloadKey={sweeps.seq} onSnapshot={sweeps.listed} focusHistory={historyFocus} />
        )}
        {promptsOpened && (
          <div hidden={view !== 'prompts'} className="h-full">
            <PromptsView
              prompts={config.prompts}
              defaults={config.defaultPrompts}
              variables={config.variables}
              items={config.items}
              active={view === 'prompts'}
              onChanged={reload}
            />
          </div>
        )}
        {view === 'settings' && (
          <SettingsView
            initialSection={settingsSection}
            settings={config.settings}
            profiles={config.profiles}
            prompts={config.prompts}
            defaultPrompts={config.defaultPrompts}
            launchDefaults={config.launchDefaults}
            account={account}
            theme={theme.theme}
            onThemeChange={theme.chooseTheme}
            onSaved={saveSettings}
            onLaunchChanged={reload}
            onClose={closeSettings}
          />
        )}
      </main>
      <Toast toast={sweepToast} onClose={closeSweepToast} />
    </div>
  )
}
