import { useEffect, useEffectEvent } from 'react'

import {
  getAutopilotEngine,
  getAutopilotGlance,
  latestAutopilotMatches,
  listRoutines,
  on,
  type AutopilotEngine,
  type AutopilotGlance,
  type AutopilotMatch,
  type AutopilotRoutine,
} from '../platform'
import { useQuery } from './useQuery'

// AutopilotState is Autopilot as the whole window shares it: at a glance (what the tab
// counts), the routines, by ID for the names the rows say ("Snoozed by ⚡ ..."), and the
// engine (its problems color the tab's red dot), each as last read.
export interface AutopilotState {
  glance: AutopilotGlance | null
  routines: readonly AutopilotRoutine[]
  // loaded reports the routines read at least once.
  loaded: boolean
  names: ReadonlyMap<number, string>
  engine: AutopilotEngine | null
  // error is why the routines couldn't be read, or ''.
  error: string
  // reload reads them all again now, as after a change the window made.
  reload: () => void
}

// noRoutines is the list until it loads, shared so it stays the same.
const noRoutines: readonly AutopilotRoutine[] = []

// useOnAutopilot calls reload on every "autopilot:updated" event, which comes at most four
// times a second.
export function useOnAutopilot(reload: () => void) {
  const onUpdate = useEffectEvent(() => {
    reload()
  })

  useEffect(
    () =>
      on('autopilot:updated', () => {
        onUpdate()
      }),
    [],
  )
}

// useAutopilot reads Autopilot at a glance, the routines, and the engine as the window
// opens, and again whenever Autopilot says it changed. Each reads the database alone.
export function useAutopilot(): AutopilotState {
  const glance = useQuery(() => getAutopilotGlance(), [], { log: 'could not read Autopilot at a glance' })
  const routines = useQuery(() => listRoutines(), [])
  const engine = useQuery(() => getAutopilotEngine(), [], { log: 'could not read Autopilot’s engine' })

  const reload = () => {
    void glance.refetch()
    void routines.refetch()
    void engine.refetch()
  }

  useOnAutopilot(reload)

  const list = routines.data ?? noRoutines

  return {
    glance: glance.data ?? null,
    routines: list,
    loaded: routines.data !== undefined,
    names: new Map(list.map((r) => [r.id, r.name])),
    engine: engine.data ?? null,
    error: routines.error,
    reload,
  }
}

// noChips are the chips until they load, shared so they stay the same.
const noChips: ReadonlyMap<number, AutopilotMatch> = new Map()

// useAutopilotChips reads the newest match a routine has on each item among ids, by item,
// for the ⚡ chips of their rows: as ids change, and whenever Autopilot says it changed.
// ids is a list joined with commas, so it compares as a value.
export function useAutopilotChips(ids: string): ReadonlyMap<number, AutopilotMatch> {
  const latest = useQuery(
    () =>
      latestAutopilotMatches(ids.split(',').map(Number)).then(
        (matches) => new Map(matches.map((m) => [m.item.id, m])) as ReadonlyMap<number, AutopilotMatch>,
      ),
    [ids],
    { enabled: ids !== '', log: 'could not read what the routines did with the items' },
  )

  useOnAutopilot(() => {
    if (ids !== '') {
      void latest.refetch()
    }
  })

  return ids === '' ? noChips : (latest.data ?? noChips)
}
