import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  getDefaultPrompts,
  getSettings,
  listLaunchDefaults,
  listProfiles,
  listPrompts,
  listPromptVariables,
  resolveLaunches,
  type DefaultPrompts,
  type InboxItem,
  type InboxSnapshot,
  type ItemResolution,
  type LaunchDefaults,
  type Profile,
  type Prompt,
  type PromptVariable,
  type Settings,
} from '../platform'
import { errorMessage } from './format'

// LaunchConfig is everything launching reads: the settings, profiles, prompts, default
// prompts, variables, and launch defaults, and each inbox item's resolved settings.
export interface LaunchConfig {
  // settings is null until the first load finishes.
  settings: Settings | null
  profiles: Profile[]
  prompts: Prompt[]
  defaultPrompts: DefaultPrompts | null
  variables: PromptVariable[]
  launchDefaults: LaunchDefaults[]
  // resolutions holds each listed item's resolved launch settings by item ID.
  resolutions: ReadonlyMap<number, ItemResolution>
  // items are the inbox's items, each once, newest first.
  items: InboxItem[]
  // loaded reports whether the first load finished.
  loaded: boolean
  error: string
  // reload loads everything again and re-resolves every item; it resolves once the
  // new values are in.
  reload: () => Promise<void>
  // setSettings records settings saved elsewhere, and re-resolves every item.
  setSettings: (settings: Settings) => void
}

// Loaded is the launch configuration as the services return it.
interface Loaded {
  settings: Settings
  profiles: Profile[]
  prompts: Prompt[]
  defaultPrompts: DefaultPrompts
  variables: PromptVariable[]
  launchDefaults: LaunchDefaults[]
}

// noResolutions is the empty resolution map, shared so it stays the same object.
const noResolutions: ReadonlyMap<number, ItemResolution> = new Map()

// loadAll reads the whole launch configuration.
async function loadAll(): Promise<Loaded> {
  const [settings, profiles, prompts, defaultPrompts, variables, launchDefaults] = await Promise.all([
    getSettings(),
    listProfiles(),
    listPrompts(),
    getDefaultPrompts(),
    listPromptVariables(),
    listLaunchDefaults(),
  ])

  return { settings, profiles, prompts, defaultPrompts, variables, launchDefaults }
}

// uniqueItems returns the snapshot's items, each once, in list order.
function uniqueItems(snapshot: InboxSnapshot | null): InboxItem[] {
  if (snapshot === null) {
    return []
  }

  const seen = new Set<number>()
  const out: InboxItem[] = []

  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    for (const item of list.items ?? []) {
      if (!seen.has(item.id)) {
        seen.add(item.id)
        out.push(item)
      }
    }
  }

  return out
}

// useLaunchConfig loads the launch configuration once, and resolves the launch settings
// of every item in snapshot, again whenever the items or the configuration change.
export function useLaunchConfig(snapshot: InboxSnapshot | null): LaunchConfig {
  const [settings, setSettingsState] = useState<Settings | null>(null)
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [prompts, setPrompts] = useState<Prompt[]>([])
  const [defaultPrompts, setDefaultPrompts] = useState<DefaultPrompts | null>(null)
  const [variables, setVariables] = useState<PromptVariable[]>([])
  const [launchDefaults, setLaunchDefaults] = useState<LaunchDefaults[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [version, setVersion] = useState(0)
  // apply records a loaded configuration and re-resolves every item.
  const apply = useCallback((loaded: Loaded) => {
    setSettingsState(loaded.settings)
    setProfiles(loaded.profiles)
    setPrompts(loaded.prompts)
    setDefaultPrompts(loaded.defaultPrompts)
    setVariables(loaded.variables)
    setLaunchDefaults(loaded.launchDefaults)
    setError('')
    setVersion((n) => n + 1)
  }, [])

  useEffect(() => {
    let active = true

    loadAll()
      .then((next) => {
        if (active) {
          apply(next)
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setError(errorMessage(err))
        }
      })
      .finally(() => {
        if (active) {
          setLoaded(true)
        }
      })

    return () => {
      active = false
    }
  }, [apply])

  const reload = useCallback(async () => {
    try {
      apply(await loadAll())
    } catch (err: unknown) {
      setError(errorMessage(err))
    }
  }, [apply])

  const items = useMemo(() => uniqueItems(snapshot), [snapshot])
  const ids = useMemo(() => items.map((i) => i.id).join(','), [items])
  // resolved is the latest resolution of the listed items.
  const [resolved, setResolved] = useState<ReadonlyMap<number, ItemResolution>>(noResolutions)

  useEffect(() => {
    if (!loaded || ids === '') {
      return undefined
    }

    let current = true

    resolveLaunches(ids.split(',').map(Number))
      .then((list) => {
        if (current) {
          setResolved(new Map(list.map((r) => [r.itemId, r])))
        }
      })
      .catch((err: unknown) => {
        if (current) {
          setError(errorMessage(err))
        }
      })

    return () => {
      current = false
    }
  }, [loaded, ids, version])

  // Until the current items resolve, keep showing the last resolution for the items it
  // covers, so the launch buttons do not flicker on every refresh.
  const resolutions = ids === '' ? noResolutions : resolved

  const setSettings = useCallback((next: Settings) => {
    setSettingsState(next)
    setVersion((n) => n + 1)
  }, [])

  return {
    settings,
    profiles,
    prompts,
    defaultPrompts,
    variables,
    launchDefaults,
    resolutions,
    items,
    loaded,
    error,
    reload,
    setSettings,
  }
}
