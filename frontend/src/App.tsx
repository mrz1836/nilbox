import { useCallback, useEffect, useMemo, useState } from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { InboxView, type LaunchOptions } from './components/InboxView'
import { SettingsView } from './components/SettingsView'
import { WorktreesView } from './components/WorktreesView'
import { errorMessage } from './lib/format'
import { useInbox } from './lib/useInbox'
import { getSettings, listProfiles, type Profile, type Settings } from './platform'

// App is the window's root: the toolbar over the active page. It holds the state the
// pages share: the inbox (the lists, the account, and the API budget), and the settings
// and launch profiles, loaded once for the rows' launch controls and the settings page.
export function App() {
  const inbox = useInbox()
  const [view, setView] = useState<View>('inbox')
  // returnView is where closing the settings page goes back to.
  const [returnView, setReturnView] = useState<View>('inbox')
  const [settings, setSettings] = useState<Settings | null>(null)
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [setupError, setSetupError] = useState('')

  useEffect(() => {
    let active = true

    Promise.all([getSettings(), listProfiles()])
      .then(([loadedSettings, loadedProfiles]) => {
        if (active) {
          setSettings(loadedSettings)
          setProfiles(loadedProfiles)
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setSetupError(errorMessage(err))
        }
      })

    return () => {
      active = false
    }
  }, [])

  const openSettings = useCallback(() => {
    if (view !== 'settings') {
      setReturnView(view)
      setView('settings')
    }
  }, [view])

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

  const launch = useMemo<LaunchOptions | null>(
    () =>
      settings === null
        ? null
        : { profiles, defaultProfile: settings.defaultProfile, worktreeDefault: settings.worktreeDefault },
    [settings, profiles],
  )

  const account = inbox.snapshot?.account ?? null

  return (
    <div className="flex h-screen flex-col bg-background text-text">
      <AppHeader
        view={view}
        onViewChange={setView}
        account={account}
        gauge={inbox.snapshot?.rate ?? null}
        now={inbox.now}
        onOpenSettings={openSettings}
      />
      {setupError !== '' && (
        <p role="alert" className="mx-4 mt-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">
          Could not load the launch settings: {setupError}
        </p>
      )}
      <main className="min-h-0 flex-1 overflow-y-auto">
        {/* Only the active page is mounted, so the worktrees are listed when that view opens. */}
        {view === 'inbox' && <InboxView inbox={inbox} launch={launch} />}
        {view === 'worktrees' && <WorktreesView />}
        {view === 'settings' && (
          <SettingsView
            settings={settings}
            profiles={profiles}
            account={account}
            onSaved={setSettings}
            onClose={closeSettings}
          />
        )}
      </main>
    </div>
  )
}
