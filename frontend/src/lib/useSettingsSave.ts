import { createContext, startTransition, use, useOptimistic, useState } from 'react'

import type { Settings } from '../platform'
import { attempt, serial, type Queue, type Result } from './call'

// SettingsQueue is the queue the window's settings saves share, wherever they are made:
// App provides one, so a change made from the toolbar and one made in Settings reach the
// backend, and their answers come back, in the order they were made, and the settings an
// older change returns never replace a newer change's. Outside it, each useSettingsSave
// keeps a queue of its own.
export const SettingsQueue = createContext<Queue | null>(null)

// SettingsSave is the settings a page shows while it changes them.
export interface SettingsSave {
  // settings are the stored settings with every change still being saved applied.
  settings: Settings
  // save shows change at once and stores it with call, which returns every setting as
  // stored after it. It resolves with that, or with why the change was refused, in which
  // case the change no longer shows; it never rejects.
  save: (change: Partial<Settings>, call: () => PromiseLike<Settings>) => Promise<Result<Settings>>
  // error is why the latest change was refused, or '' once another is made.
  error: string
}

// merge applies a change still being saved to the stored settings.
function merge(stored: Settings, change: Partial<Settings>): Settings {
  return { ...stored, ...change }
}

// useSettingsSave saves settings the one way every settings page does: a change shows at
// once, the settings the setter returns are handed to onSaved as the new stored settings,
// with nothing read again, and a refused change goes back to what is stored. Saves run one
// at a time, in the order they were made, through the window's SettingsQueue when there is
// one, so the settings the last one returns hold every change before it.
export function useSettingsSave(settings: Settings, onSaved: (settings: Settings) => void): SettingsSave {
  const [shown, show] = useOptimistic(settings, merge)
  const [own] = useState(serial)
  const queue = use(SettingsQueue) ?? own
  const [error, setError] = useState('')

  const save = (change: Partial<Settings>, call: () => PromiseLike<Settings>) =>
    new Promise<Result<Settings>>((resolve) => {
      setError('')
      startTransition(async () => {
        show(change)

        const result = await attempt(queue(call))

        if (result.ok) {
          startTransition(() => {
            onSaved(result.data)
          })
        } else {
          setError(result.error)
        }

        resolve(result)
      })
    })

  return { settings: shown, save, error }
}
