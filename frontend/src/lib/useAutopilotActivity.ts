import { useRef, useState } from 'react'

import { autopilotActivity, type AutopilotActivityDay, type AutopilotLeftAlone } from '../platform'
import { mergeDays } from './autopilot'
import { attempt } from './call'
import { useOnAutopilot } from './useAutopilot'
import { useQuery } from './useQuery'

// pageSize is how many matches a page of Activity the user asks for holds, and readSize
// how many a read of the pages already loaded takes at a time (the backend's own cap).
const pageSize = 100
const readSize = 500

// Loaded is Activity as read: the days, newest first, the items the routines leave alone
// (the first page's), whether there is more, and where it starts.
interface Loaded {
  days: AutopilotActivityDay[]
  leftAlone: AutopilotLeftAlone[]
  more: boolean
  next: number
}

// readDown reads Activity from its newest match down to the match floor, every match
// between included, a page at a time, so nothing loaded before drops out of the middle; with
// no floor, it reads the first page alone.
async function readDown(floor: number): Promise<Loaded> {
  const first = await autopilotActivity(0, floor === 0 ? pageSize : readSize)
  const out: Loaded = { days: first.days, leftAlone: first.leftAlone, more: first.more, next: first.next }

  while (floor > 0 && out.more && out.next > floor) {
    const page = await autopilotActivity(out.next, readSize)

    out.days = mergeDays(out.days, page.days)
    out.more = page.more
    out.next = page.next
  }

  return out
}

// oldestOf is the oldest match a page holds, 0 for none.
function oldestOf(days: readonly AutopilotActivityDay[]): number {
  return days.reduce((low, day) => day.matches.reduce((min, m) => (min === 0 || m.id < min ? m.id : min), low), 0)
}

// ActivityPages is Activity as the tab shows it: the days loaded, newest first, the items
// the routines leave alone, whether there is more, and a way to load it.
export interface ActivityPages {
  days: readonly AutopilotActivityDay[]
  leftAlone: readonly AutopilotLeftAlone[]
  loaded: boolean
  error: string
  more: boolean
  loadingMore: boolean
  loadMore: () => Promise<void>
  // reload reads the pages loaded again now, as after a change the tab made.
  reload: () => void
}

// noDays is Activity until it loads, shared so it stays the same.
const noDays: readonly AutopilotActivityDay[] = []
const noneLeftAlone: readonly AutopilotLeftAlone[] = []

// useAutopilotActivity reads Activity's newest page as the tab opens, a page more each time
// the user scrolls to the end (loadMore), and, whenever Autopilot says it changed, every
// match from the newest down to the oldest loaded, so a read again never leaves a gap
// where matches it pushed past a page's end would have been.
export function useAutopilotActivity(): ActivityPages {
  // floor is the oldest match loaded past the first page, 0 while only it is.
  const floor = useRef(0)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState('')
  const pages = useQuery(() => readDown(floor.current), [])

  useOnAutopilot(() => {
    void pages.refetch()
  })

  const loaded = pages.data

  const loadMore = async () => {
    if (loaded === undefined || !loaded.more || loadingMore) {
      return
    }

    setLoadingMore(true)

    const result = await attempt(autopilotActivity(loaded.next, pageSize))

    setLoadingMore(false)

    if (!result.ok) {
      setMoreError(result.error)

      return
    }

    const page = result.data

    setMoreError('')
    floor.current = oldestOf(page.days) || floor.current
    pages.set((was) =>
      was === undefined ? was : { ...was, days: mergeDays(was.days, page.days), more: page.more, next: page.next },
    )
  }

  return {
    days: loaded?.days ?? noDays,
    leftAlone: loaded?.leftAlone ?? noneLeftAlone,
    loaded: loaded !== undefined,
    error: pages.error !== '' ? pages.error : moreError,
    more: loaded?.more ?? false,
    loadingMore,
    loadMore,
    reload: () => {
      void pages.refetch()
    },
  }
}
