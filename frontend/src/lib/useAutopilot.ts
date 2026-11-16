import { useEffect, useEffectEvent } from 'react'

import {
  autopilotNeedsYou,
  checkAutopilotReadiness,
  getAutopilotEngine,
  getAutopilotGlance,
  getAutopilotReadiness,
  latestAutopilotMatches,
  listRoutines,
  on,
  routineNames,
  type AutopilotEngine,
  type AutopilotGlance,
  type AutopilotMatch,
  type AutopilotReadiness,
  type AutopilotRoutine,
} from '../platform'
import { inChunks } from './autopilot'
import { all, andThen } from './call'
import { useMutation } from './useMutation'
import { useNowAs } from './useNow'
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
// opens, and again whenever Autopilot says it changed. Each reads the database alone. The
// routines are read again as a new local day begins too, so a status line's "today" turns
// over at midnight even while nothing changes, on the window's own clock.
export function useAutopilot(): AutopilotState {
  const day = useNowAs((now) => now.toDateString())
  const glance = useQuery(() => getAutopilotGlance(), [], { log: 'could not read Autopilot at a glance' })
  const routines = useQuery(() => listRoutines(), [day])
  const engine = useQuery(() => getAutopilotEngine(), [], { log: 'could not read Autopilot’s engine' })
  const named = useQuery(() => routineNames(), [], { log: 'could not read the routines’ names' })

  const reload = () => {
    void glance.refetch()
    void routines.refetch()
    void engine.refetch()
    void named.refetch()
  }

  useOnAutopilot(reload)

  const list = routines.data ?? noRoutines

  return {
    glance: glance.data ?? null,
    routines: list,
    loaded: routines.data !== undefined,
    // Every routine's name, a deleted one's too, so a mark it left still names it; a
    // routine listed names itself while the names load.
    names: new Map([
      ...(named.data ?? []).map((n) => [n.id, n.name] as const),
      ...list.map((r) => [r.id, r.name] as const),
    ]),
    engine: engine.data ?? null,
    error: routines.error,
    reload,
  }
}

// noChips are the chips until they load, shared so they stay the same.
const noChips: ReadonlyMap<number, AutopilotMatch> = new Map()

// useAutopilotChips reads the newest match a routine has on each item among ids, by item,
// for the ⚡ chips of their rows: as ids change, and whenever Autopilot says it changed,
// as many at a time as the backend reads (inChunks), so no item loses its chip. ids is a
// list joined with commas, so it compares as a value.
export function useAutopilotChips(ids: string): ReadonlyMap<number, AutopilotMatch> {
  const latest = useQuery(
    () =>
      andThen(
        all(inChunks(ids.split(',').map(Number)).map((chunk) => latestAutopilotMatches(chunk))),
        (pages) => new Map(pages.flat().map((m) => [m.item.id, m])) as ReadonlyMap<number, AutopilotMatch>,
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

// noMatches are Needs you's matches until they load, shared so they stay the same.
const noMatches: readonly AutopilotMatch[] = []

// NeedsYouState is what waits for the user, as Activity's Needs you shows it.
export interface NeedsYouState {
  // matches are the open matches that haven't started, newest first.
  matches: readonly AutopilotMatch[]
  // settled reports a read that answered, or failed, at least once; error is why the last
  // read failed, or ''.
  settled: boolean
  error: string
  // reload reads them again now, as after the window approved or skipped one.
  reload: () => void
}

// useNeedsYou reads what waits for the user (NeedsYou: the matches waiting for an OK, those
// that can't start until something on this Mac is fixed, and those queued), as it mounts
// and whenever Autopilot says it changed. It reads the database alone.
export function useNeedsYou(): NeedsYouState {
  const read = useQuery(() => autopilotNeedsYou(), [])

  useOnAutopilot(() => {
    void read.refetch()
  })

  return {
    matches: read.data ?? noMatches,
    settled: read.data !== undefined || read.error !== '',
    error: read.error,
    reload: () => {
      void read.refetch()
    },
  }
}

// ReadinessState is this Mac's readiness to run the routines.
export interface ReadinessState {
  // readiness is how the checks last came out, null until it is read.
  readiness: AutopilotReadiness | null
  // checking reports checks under way: the window's Check again, or the backend's own.
  checking: boolean
  // error is why the last read or check failed, or ''.
  error: string
  // check runs every check again now (it asks GitHub, one point), and wakes the engine,
  // so a routine waiting to be ready starts.
  check: () => void
}

// useReadiness reads this Mac's readiness as it mounts and whenever Autopilot says it
// changed, which a check that ends does; Check again runs every check now.
export function useReadiness(): ReadinessState {
  const read = useQuery(() => getAutopilotReadiness(), [], { log: 'could not read this Mac’s readiness' })
  const checker = useMutation(() => checkAutopilotReadiness())

  useOnAutopilot(() => {
    void read.refetch()
  })

  return {
    readiness: read.data ?? null,
    checking: checker.pending || read.data?.checking === true,
    error: checker.error,
    check: () => {
      void checker.run().then((result) => {
        if (result.ok) {
          read.set(() => result.data)
        }
      })
    },
  }
}
