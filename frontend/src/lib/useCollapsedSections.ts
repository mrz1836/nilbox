import { useState } from 'react'

import { getCollapsedSections, setSectionCollapsed, type ListKey } from '../platform'
import { serial } from './call'
import { useQuery } from './useQuery'

// CollapsedSections is which inbox sections are collapsed, as the inbox was last left.
export interface CollapsedSections {
  // collapsed holds the collapsed sections' list names. It is null until the stored state
  // loads, so a collapsed section never shows open first.
  collapsed: ReadonlySet<ListKey> | null
  // setCollapsed collapses or opens a section, and stores the change for the next launch.
  setCollapsed: (section: ListKey, collapsed: boolean) => void
}

// noneCollapsed is every section open, as a stored state that cannot be read shows.
const noneCollapsed: ReadonlySet<ListKey> = new Set()

// useCollapsedSections loads which inbox sections are collapsed once, and stores each
// change. A section that cannot be read or stored still collapses for this session; the
// failure is only logged, since nothing else depends on it.
export function useCollapsedSections(): CollapsedSections {
  const stored = useQuery(
    () => getCollapsedSections().then((sections): ReadonlySet<ListKey> => new Set(sections)),
    [],
    { log: 'could not read the collapsed inbox sections' },
  )
  // queue stores the changes one at a time, so they reach the database in the order they
  // were made and a quick collapse and reopen never ends stored the wrong way round.
  const [queue] = useState(serial)
  const loaded = stored.data !== undefined || stored.error !== ''

  const setCollapsed = (section: ListKey, on: boolean) => {
    stored.set((was) => {
      const next = new Set(was ?? noneCollapsed)

      if (on) {
        next.add(section)
      } else {
        next.delete(section)
      }

      return next
    })

    queue(() => setSectionCollapsed(section, on)).catch((err: unknown) => {
      console.error(`could not store whether the ${section} section is collapsed`, err)
    })
  }

  return { collapsed: loaded ? (stored.data ?? noneCollapsed) : null, setCollapsed }
}
