import { useEffect, useState, useSyncExternalStore } from 'react'

import {
  autopilotActivity,
  autopilotActivityFrom,
  type AutopilotActivityDay,
  type AutopilotActivityPage,
  type AutopilotLeftAlone,
} from '../platform'
import { mergeDays } from './autopilot'
import { attempt, cancel } from './call'
import { useOnAutopilot } from './useAutopilot'

// pageSize is how many matches a page of Activity holds, the first one and each the user
// asks for, and readSize the most a read again of the pages loaded takes (the backend's
// own cap): past it, Activity starts again from its newest.
const pageSize = 100
const readSize = 500

// Older is a page read after the newest ones, as the user asked for more: the match it
// was read before, and what it holds.
interface Older {
  before: number
  page: AutopilotActivityPage
}

// Loaded is Activity as read: the newest matches, from lower on (0: every match), with
// the items left alone; whether matches were recorded before lower; and the pages read
// past it, oldest last. The newest are read again as Autopilot changes, from lower on, so
// they always meet the pages past them, which keep their place.
interface Loaded {
  head: AutopilotActivityPage
  lower: number
  below: boolean
  older: readonly Older[]
}

// fromNewest is Activity as a page read from its newest match holds it.
function fromNewest(page: AutopilotActivityPage): Loaded {
  return { head: page, lower: page.more ? page.next : 0, below: page.more, older: [] }
}

// cursorOf is where the next page starts, and whether there is one.
function cursorOf(l: Loaded): { more: boolean; next: number } {
  const last = l.older.at(-1)

  return last === undefined ? { more: l.below, next: l.lower } : { more: last.page.more, next: last.page.next }
}

// Kind is a read again: of the newest matches alone, as an event asks for ('head'), or of
// every page loaded ('deep'), as the tab asks for once it shows again or changed one
// itself, when what changed may be further down.
type Kind = 'head' | 'deep'

// View is what the tab shows of Activity.
interface View {
  days: readonly AutopilotActivityDay[]
  leftAlone: readonly AutopilotLeftAlone[]
  loaded: boolean
  error: string
  more: boolean
  loadingMore: boolean
}

// noView is Activity until it loads.
const noView: View = { days: [], leftAlone: [], loaded: false, error: '', more: false, loadingMore: false }

// Pager is Activity as one tab reads it: what it has loaded, and the reads that keep it
// current. One read runs at a time, and one more waits, so however often Autopilot
// changes, the reads never overlap or pile up; a read again of every page loaded drops a
// read of the newest under way, which it covers; and a page read meanwhile is never lost.
interface Pager {
  subscribe: (listener: () => void) => () => void
  view: () => View
  // show starts reading, as the tab shows: the newest page the first time, every page
  // loaded after that. hide cancels every read under way, as the tab hides.
  show: () => void
  hide: () => void
  refresh: (kind: Kind) => void
  loadMore: () => Promise<void>
}

// pager makes a Pager.
function pager(): Pager {
  const listeners = new Set<() => void>()
  let loaded: Loaded | undefined
  let error = ''
  let moreError = ''
  let view = noView
  let running: { kind: Kind | 'first'; call: PromiseLike<unknown> } | null = null
  let queued: Kind | null = null
  let more: PromiseLike<unknown> | null = null

  const tell = () => {
    const shown = loaded

    view = {
      days: shown === undefined ? [] : shown.older.reduce((days, o) => mergeDays(days, o.page.days), shown.head.days),
      leftAlone: shown?.head.leftAlone ?? [],
      loaded: shown !== undefined,
      error: error !== '' ? error : moreError,
      more: shown === undefined ? false : cursorOf(shown).more,
      loadingMore: more !== null,
    }

    for (const listener of listeners) {
      listener()
    }
  }

  // apply puts a read's page in place of what it read again, keeping every page past it.
  const apply = (kind: Kind | 'first', from: number, below: boolean, page: AutopilotActivityPage) => {
    const was = loaded

    if (kind === 'first' || was === undefined) {
      // A page read past the newest while they were read from scratch, or still on its
      // way, wouldn't meet them: the newest are read again from where they were, which it
      // does, and the page keeps its place.
      if (was !== undefined && (was.older.length > 0 || more !== null)) {
        queued = queued ?? 'head'

        return
      }

      loaded = fromNewest(page)

      return
    }

    if (page.more) {
      // More was recorded than one read holds: start again from the newest.
      loaded = { head: page, lower: page.next, below: true, older: [] }

      return
    }

    loaded =
      kind === 'head'
        ? { ...was, head: page }
        : { head: page, lower: from, below, older: was.older.filter((o) => o.before <= from) }
  }

  const begin = (kind: Kind) => {
    const was = loaded
    // The newest alone, while nothing past them is loaded or on its way, read as the
    // first page is, so the read stays a page long.
    const first = was === undefined || (kind === 'head' && was.older.length === 0 && more === null)
    const cursor = was === undefined ? { more: false, next: 0 } : cursorOf(was)
    const from = was === undefined ? 0 : kind === 'head' ? was.lower : cursor.more ? cursor.next : 0
    const below = cursor.more
    const call = first ? autopilotActivity(0, pageSize) : autopilotActivityFrom(from, readSize)
    const mine = { kind: first ? ('first' as const) : kind, call }

    running = mine

    void attempt(call).then((result) => {
      if (running !== mine) {
        return
      }

      running = null

      if (result.ok) {
        error = ''
        apply(mine.kind, from, below, result.data)
      } else {
        error = result.error
      }

      const next = queued

      queued = null
      tell()

      if (next !== null) {
        begin(next)
      }
    })
  }

  const refresh = (kind: Kind) => {
    if (running !== null) {
      // A read of every page covers a read of the newest under way, which goes.
      if (kind === 'deep' && running.kind !== 'deep') {
        cancel(running.call)
        running = null
        begin('deep')

        return
      }

      queued = queued === 'deep' ? 'deep' : kind

      return
    }

    begin(kind)
  }

  return {
    subscribe: (listener) => {
      listeners.add(listener)

      return () => {
        listeners.delete(listener)
      }
    },
    view: () => view,
    show: () => {
      refresh('deep')
    },
    hide: () => {
      if (running !== null) {
        cancel(running.call)
        running = null
      }

      if (more !== null) {
        cancel(more)
        more = null
      }

      queued = null
      tell()
    },
    refresh,
    loadMore: async () => {
      const was = loaded

      if (was === undefined || more !== null) {
        return
      }

      const cursor = cursorOf(was)

      if (!cursor.more) {
        return
      }

      const call = autopilotActivity(cursor.next, pageSize)

      more = call
      tell()

      const result = await attempt(call)

      if (more !== call) {
        return
      }

      more = null

      if (!result.ok) {
        moreError = result.error
        tell()

        return
      }

      moreError = ''

      // The page goes past what is loaded only while it still ends where the page began;
      // after a fresh start from the newest it would leave a gap, and the user asks again.
      const now = loaded

      if (now !== undefined && cursorOf(now).more && cursorOf(now).next === cursor.next) {
        loaded = { ...now, older: [...now.older, { before: cursor.next, page: result.data }] }
      }

      tell()
    },
  }
}

// ActivityPages is Activity as the tab shows it: the days loaded, newest first, the items
// the routines leave alone, whether there is more, and a way to load it.
export interface ActivityPages extends View {
  loadMore: () => Promise<void>
  // reload reads every page loaded again now, as after a change the tab made to a line
  // further down than the newest.
  reload: () => void
}

// useAutopilotActivity reads Activity's newest page as the tab first shows, a page more
// each time the user scrolls to the end (loadMore), the newest matches again whenever
// Autopilot says it changed, and every page loaded again whenever the tab shows again,
// so what changed while it was away shows too. See Pager for how the reads keep to one at
// a time and never leave a gap.
export function useAutopilotActivity(): ActivityPages {
  const [activity] = useState(pager)
  const view = useSyncExternalStore(activity.subscribe, activity.view)

  useEffect(() => {
    activity.show()

    return () => {
      activity.hide()
    }
  }, [activity])

  useOnAutopilot(() => {
    activity.refresh('head')
  })

  return {
    ...view,
    loadMore: activity.loadMore,
    reload: () => {
      activity.refresh('deep')
    },
  }
}
