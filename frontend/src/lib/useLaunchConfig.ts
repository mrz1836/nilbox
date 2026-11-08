import { useMemo } from 'react'

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
import { all } from './call'
import { useQuery } from './useQuery'

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
  // setSettings records the settings a setter stored and returned. Items are resolved
  // again only when a setting launches read changed.
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

// noList is the empty list each part of the configuration shows until it loads, shared
// so it stays the same object.
const noList: never[] = []

// loadAll reads the whole launch configuration; cancelling it cancels every read.
function loadAll(): PromiseLike<Loaded> {
  const reads = all([
    getSettings(),
    listProfiles(),
    listPrompts(),
    getDefaultPrompts(),
    listPromptVariables(),
    listLaunchDefaults(),
  ])
  const loaded = reads.then(([settings, profiles, prompts, defaultPrompts, variables, launchDefaults]) => ({
    settings,
    profiles,
    prompts,
    defaultPrompts,
    variables,
    launchDefaults,
  }))

  return Object.assign(loaded, { cancel: reads.cancel })
}

// launchKey is the part of the settings a launch reads (internal/launcher's
// LaunchSettings), so a change to anything else resolves nothing again.
function launchKey(settings: Settings | undefined): string {
  return settings === undefined
    ? ''
    : JSON.stringify([
        settings.defaultProfile,
        settings.worktreeDefault,
        settings.terminal,
        settings.cloneRoot,
        settings.trustedBots,
      ])
}

// uniqueItems returns the snapshot's items, each once, in list order.
function uniqueItems(snapshot: InboxSnapshot | null): InboxItem[] {
  if (snapshot === null) {
    return []
  }

  const seen = new Set<number>()
  const out: InboxItem[] = []

  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    for (const item of list.items) {
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
  const config = useQuery(loadAll, [])
  const loaded = config.data !== undefined || config.error !== ''
  const data = config.data
  const items = useMemo(() => uniqueItems(snapshot), [snapshot])
  const ids = useMemo(() => items.map((i) => i.id).join(','), [items])

  // Resolutions are read again when the items change, when a reload brings any part of
  // the configuration anew, and when a setting launches read is saved.
  const resolved = useQuery(
    () => resolveLaunches(ids.split(',').map(Number)).then((list) => new Map(list.map((r) => [r.itemId, r]))),
    [ids, data?.profiles, data?.prompts, data?.defaultPrompts, data?.launchDefaults, launchKey(data?.settings)],
    { enabled: loaded && ids !== '' },
  )

  // Until the current items resolve, keep showing the last resolution for the items it
  // covers, so the launch buttons do not flicker on every refresh.
  const resolutions = ids === '' ? noResolutions : (resolved.data ?? noResolutions)

  return {
    settings: data?.settings ?? null,
    profiles: data?.profiles ?? noList,
    prompts: data?.prompts ?? noList,
    defaultPrompts: data?.defaultPrompts ?? null,
    variables: data?.variables ?? noList,
    launchDefaults: data?.launchDefaults ?? noList,
    resolutions,
    items,
    loaded,
    error: config.error !== '' ? config.error : resolved.error,
    reload: config.refetch,
    setSettings: (settings) => {
      config.set((was) => (was === undefined ? was : { ...was, settings }))
    },
  }
}
