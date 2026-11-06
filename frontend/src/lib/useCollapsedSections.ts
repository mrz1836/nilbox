import { useCallback, useEffect, useRef, useState } from 'react'

import { getCollapsedSections, setSectionCollapsed } from '../platform'

// CollapsedSections is which inbox sections are collapsed, as the inbox was last left.
export interface CollapsedSections {
  // collapsed holds the collapsed sections' list names. It is null until the stored state
  // loads, so a collapsed section never shows open first.
  collapsed: ReadonlySet<string> | null
  // setCollapsed collapses or opens a section, and stores the change for the next launch.
  setCollapsed: (section: string, collapsed: boolean) => void
}

// useCollapsedSections loads which inbox sections are collapsed once, and stores each
// change. A section that cannot be read or stored still collapses for this session; the
// failure is only logged, since nothing else depends on it.
export function useCollapsedSections(): CollapsedSections {
  const [collapsed, setState] = useState<ReadonlySet<string> | null>(null)
  // saving chains the stores, so they reach the database in the order they were made and
  // a quick collapse and reopen never ends stored the wrong way round.
  const saving = useRef<Promise<void>>(Promise.resolve())

  useEffect(() => {
    let active = true

    getCollapsedSections()
      .then((sections) => {
        if (active) {
          setState(new Set(sections))
        }
      })
      .catch((err: unknown) => {
        console.error('could not read the collapsed inbox sections', err)

        if (active) {
          setState(new Set())
        }
      })

    return () => {
      active = false
    }
  }, [])

  const setCollapsed = useCallback((section: string, on: boolean) => {
    setState((prev) => {
      const next = new Set(prev)

      if (on) {
        next.add(section)
      } else {
        next.delete(section)
      }

      return next
    })

    saving.current = saving.current
      .then(() => setSectionCollapsed(section, on))
      .catch((err: unknown) => {
        console.error(`could not store whether the ${section} section is collapsed`, err)
      })
  }, [])

  return { collapsed, setCollapsed }
}
