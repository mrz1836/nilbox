import { useEffect, useId, useMemo, useState } from 'react'

import { AppHeader, type View } from './components/AppHeader'
import { InboxView, type LaunchOptions } from './components/InboxView'
import { SettingsPanel } from './components/SettingsPanel'
import { WorktreesView } from './components/WorktreesView'
import { errorMessage } from './lib/format'
import { getSettings, listProfiles, type Profile, type Settings } from './platform'

// App is the window's root: the header, the optional settings panel, and the active view.
// It loads the settings and the launch profiles once, for the rows' launch controls.
export function App() {
  const settingsPanelId = useId()
  const [view, setView] = useState<View>('inbox')
  const [settingsOpen, setSettingsOpen] = useState(false)
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

  const launch = useMemo<LaunchOptions | null>(
    () =>
      settings === null
        ? null
        : { profiles, defaultProfile: settings.defaultProfile, worktreeDefault: settings.worktreeDefault },
    [settings, profiles],
  )

  return (
    <div className="flex min-h-screen flex-col bg-background text-text">
      <AppHeader
        view={view}
        onViewChange={setView}
        settingsOpen={settingsOpen}
        onToggleSettings={() => {
          setSettingsOpen((open) => !open)
        }}
        settingsPanelId={settingsPanelId}
      />
      {settingsOpen && settings !== null && (
        <SettingsPanel id={settingsPanelId} settings={settings} onSaved={setSettings} />
      )}
      {setupError !== '' && (
        <p role="alert" className="mx-4 mt-4 rounded-md border border-danger px-3 py-2 text-sm text-danger">
          Could not load the launch settings: {setupError}
        </p>
      )}
      <main className="flex-1">
        {/* Only the active view is mounted, so the worktrees are listed when that view opens. */}
        {view === 'inbox' ? <InboxView launch={launch} /> : <WorktreesView />}
      </main>
    </div>
  )
}
