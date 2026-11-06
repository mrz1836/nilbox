import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { InboxView, type LaunchOptions } from './components/InboxView'
import { PromptsView } from './components/PromptsView'
import { SettingsView, type SettingsSection } from './components/SettingsView'
import { Toast, type ToastMessage } from './components/Toast'
import { WorktreesView } from './components/WorktreesView'
import { defaultSnoozeDays } from './lib/triage'
import { useCollapsedSections } from './lib/useCollapsedSections'
import { useInbox } from './lib/useInbox'
import { useLaunchConfig } from './lib/useLaunchConfig'
import {
  lastWorktreeSweep,
  onWorktreesUpdated,
  type RemoveResult,
  type Settings,
  type WorktreesSnapshot,
} from './platform'

// cleanedToast tells what an automatic sweep cleaned up.
function cleanedToast(seq: number, cleaned: readonly RemoveResult[]): ToastMessage {
  const n = cleaned.length

  return {
    id: seq,
    text: n === 1 ? 'Cleaned up a finished worktree' : `Cleaned up ${String(n)} finished worktrees`,
    detail: cleaned.map((r) => (r.item === '' ? r.displayPath : r.item)).join(', '),
  }
}

// useWorktreeSweeps follows the automatic worktree cleanup: how many worktrees are done
// on GitHub but still on disk, the latest sweep that changed something (so an open
// Worktrees view lists again), and a toast for each sweep that removed a worktree.
function useWorktreeSweeps() {
  const [attention, setAttention] = useState(0)
  const [seq, setSeq] = useState(0)
  const [toast, setToast] = useState<ToastMessage | null>(null)
  // seen is the latest sweep read, so each one is reported once.
  const seen = useRef(0)

  useEffect(() => {
    let active = true

    const read = () => {
      lastWorktreeSweep()
        .then((report) => {
          if (!active || report.seq <= seen.current) {
            return
          }

          seen.current = report.seq
          setAttention(report.leftover)
          setSeq(report.seq)

          const cleaned = report.cleaned ?? []
          if (cleaned.length > 0) {
            setToast(cleanedToast(report.seq, cleaned))
          }
        })
        .catch((err: unknown) => {
          console.error('could not read the latest worktree sweep', err)
        })
    }

    read()
    const stop = onWorktreesUpdated(read)

    return () => {
      active = false
      stop()
    }
  }, [])

  // listed counts from a listing of the Worktrees view, which sees the same worktrees.
  const listed = useCallback((snapshot: WorktreesSnapshot) => {
    setAttention(snapshot.finishedCount + snapshot.reviewCount)
  }, [])

  const closeToast = useCallback(() => {
    setToast(null)
  }, [])

  return { attention, seq, toast, listed, closeToast }
}

// App is the window's root: the toolbar over the active page. It holds the state the
// pages share: the inbox (the lists, the account, and the API budget), the launch
// configuration (settings, profiles, prompts, launch defaults, and each item's resolved
// launch settings) that the rows, the Prompts page, and the settings page use, which
// inbox lists are collapsed, and the automatic worktree cleanup's latest news.
export function App() {
  const inbox = useInbox()
  const config = useLaunchConfig(inbox.snapshot)
  const sweeps = useWorktreeSweeps()
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

  const changeView = useCallback((next: View) => {
    if (next === 'prompts') {
      setPromptsOpened(true)
    }

    setHistoryFocus(false)
    setView(next)
  }, [])

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

  const closeSettings = useCallback(() => {
    setView(returnView)
  }, [returnView])

  // ⌘, opens the settings page, as in every Mac app.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey && e.key === ',') {
        e.preventDefault()
        openSettings()
      }
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [openSettings])

  const { reload, setSettings } = config
  const { reload: reloadInbox } = inbox

  // saveSettings records changed settings: every row's launch settings resolve again, and
  // the stored inbox is read again, since the trusted bots decide each card's trust.
  const saveSettings = useCallback(
    (next: Settings) => {
      setSettings(next)
      reloadInbox()
    },
    [setSettings, reloadInbox],
  )

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

  return (
    <div className="flex h-screen flex-col bg-background text-text">
      <AppHeader
        view={view}
        onViewChange={changeView}
        account={account}
        gauge={inbox.snapshot?.rate ?? null}
        now={inbox.now}
        worktreeAttention={sweeps.attention}
        onOpenSettings={openSettings}
      />
      {config.error !== '' && (
        <p role="alert" className="mx-4 mt-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">
          Could not load the launch settings: {config.error}
        </p>
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
            collapsed={sections.collapsed}
            onCollapse={sections.setCollapsed}
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
