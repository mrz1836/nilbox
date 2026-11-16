import { useEffect, useState, useSyncExternalStore } from 'react'

import {
  autopilotActivity,
  autopilotActivityFrom,
  type AutopilotActivityDay,
  type AutopilotActivityPage,
  type AutopilotLeftAlone,
  type AutopilotMatch,
} from '../platform'
import { mergeDays } from './autopilot'
import { attempt, cancel, type Result } from './call'
import { useOnAutopilot } from './useAutopilot'

// pageSize is how many matches a page of Activity holds, the first one and each the user
// asks for, and sliceSize the most one read again of a stretch takes (the backend's own
// cap): a stretch that holds more is read again in slices that meet.
const pageSize = 100
const sliceSize = 500

// Stretch is a run of Activity as one read holds it: the matches from lower on (0: from
// the first) and before upper (0: up to the newest), newest first.
interface Stretch {
  lower: number
  upper: number
  page: AutopilotActivityPage
}

// Loaded is Activity as read, its stretches newest first, never none: the first page, with
// what was recorded since it was read, then each page the user asked for, or a slice of
// one a read again cut. Each ends where the one after it begins (its lower is the next's
// upper), and a read again reads each stretch by its own bounds, so they keep meeting with
// nothing between, whatever a read again finds in each.
type Loaded = readonly Stretch[]

// firstStretch is Activity as its first page, read from its newest match, holds it.
function firstStretch(page: AutopilotActivityPage): Stretch {
  return { lower: page.more ? page.next : 0, upper: 0, page }
}

// cursorOf is where the next page starts, and whether there is one: where the oldest
// stretch begins.
function cursorOf(l: Loaded): { more: boolean; next: number } {
  const lower = l.at(-1)?.lower ?? 0

  return { more: lower > 0, next: lower }
}

// matchesOf are the matches a page holds.
function matchesOf(page: AutopilotActivityPage): AutopilotMatch[] {
  return page.days.flatMap((d) => d.matches)
}

// edgeOf is the match the newest stretch begins at, or undefined while it begins at the
// first. Every match recorded since it was read has a higher ID, so a read of the newest
// stretch again finds it, until the history is cleared: the IDs are then handed out again
// from the first, and the newest stretch no longer holds what is new.
function edgeOf(s: Stretch | undefined): AutopilotMatch | undefined {
  return s === undefined || s.lower <= 0 ? undefined : matchesOf(s.page).find((m) => m.id === s.lower)
}

// holds reports that a page still holds the match edge, recorded when it was.
function holds(page: AutopilotActivityPage | undefined, edge: AutopilotMatch): boolean {
  return page !== undefined && matchesOf(page).some((m) => m.id === edge.id && m.createdAt === edge.createdAt)
}

// Kind is a read again: of the newest stretch alone, as an event asks for ('head'), or of
// every stretch loaded ('deep'), as the tab asks for once it shows again or changed a line
// itself, when what changed may be further down.
type Kind = 'head' | 'deep'

// Running is the read under way: a read again of a kind, or the first page, read as the
// tab first shows, as a read again while it is all that is loaded, or from scratch; and
// the call it waits on.
interface Running {
  kind: Kind | 'first' | 'scratch'
  call: PromiseLike<unknown> | null
}

// View is what the tab shows of Activity.
interface View {
  days: readonly AutopilotActivityDay[]
  leftAlone: readonly AutopilotLeftAlone[]
  loaded: boolean
  // current reports that the days are read since the tab last showed: false while it is
  // hidden, and from then until the read as it shows again lands, so nothing is looked
  // for in days the tab held before it hid.
  current: boolean
  error: string
  more: boolean
  loadingMore: boolean
}

// noView is Activity until it loads.
const noView: View = {
  days: [],
  leftAlone: [],
  loaded: false,
  current: false,
  error: '',
  more: false,
  loadingMore: false,
}

// Pager is Activity as one tab reads it: what it has loaded, and the reads that keep it
// current. One read runs at a time, and one more waits, so however often Autopilot
// changes, the reads never overlap or pile up; a read again of every stretch drops a read
// of the newest under way, and one waiting, which it covers; and a page read meanwhile is
// never lost.
interface Pager {
  subscribe: (listener: () => void) => () => void
  view: () => View
  // show starts reading, as the tab shows: the newest page the first time, every stretch
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
  let current = false
  let view = noView
  let running: Running | null = null
  let queued: Kind | null = null
  let more: PromiseLike<unknown> | null = null

  const tell = () => {
    const [newest, ...rest] = loaded ?? []

    view = {
      days: newest === undefined ? [] : rest.reduce((days, s) => mergeDays(days, s.page.days), newest.page.days),
      leftAlone: newest?.page.leftAlone ?? [],
      loaded: newest !== undefined,
      current,
      error: error !== '' ? error : moreError,
      more: loaded === undefined ? false : cursorOf(loaded).more,
      loadingMore: more !== null,
    }

    for (const listener of listeners) {
      listener()
    }
  }

  // ask makes one call for the read mine, and resolves with its answer, or with undefined
  // once the read was dropped (the tab hid, or another replaced it), whose answer nothing
  // shows.
  const ask = async (
    mine: Running,
    call: PromiseLike<AutopilotActivityPage>,
  ): Promise<Result<AutopilotActivityPage> | undefined> => {
    mine.call = call

    const result = await attempt(call)

    return running === mine ? result : undefined
  }

  // reread reads the stretch from lower on and before upper again, in slices of at most
  // sliceSize, newest first, each beginning where the one before it ended.
  const reread = async (mine: Running, lower: number, upper: number): Promise<Result<Stretch[]> | undefined> => {
    const out: Stretch[] = []
    let top = upper

    for (;;) {
      const result = await ask(mine, autopilotActivityFrom(lower, top, sliceSize))

      if (result === undefined || !result.ok) {
        return result
      }

      const page = result.data

      if (!page.more) {
        out.push({ lower, upper: top, page })

        return { ok: true, data: out }
      }

      out.push({ lower: page.next, upper: top, page })
      top = page.next
    }
  }

  // rereadNewest reads the newest stretch, from lower on, again: as one stretch while it
  // holds at most sliceSize; past that, its newest page becomes the newest stretch and the
  // rest is read again in slices, so the newest stretch starts a page long again rather
  // than every later change cutting a slice off it.
  const rereadNewest = async (mine: Running, lower: number): Promise<Result<Stretch[]> | undefined> => {
    const result = await ask(mine, autopilotActivityFrom(lower, 0, sliceSize))

    if (result === undefined || !result.ok) {
      return result
    }

    const page = result.data

    if (!page.more) {
      return { ok: true, data: [{ lower, upper: 0, page }] }
    }

    const top = matchesOf(page).at(pageSize - 1)?.id ?? page.next
    const newer = await rereadNewest(mine, top)

    if (newer === undefined || !newer.ok) {
      return newer
    }

    const rest = await reread(mine, lower, top)

    return rest === undefined || !rest.ok ? rest : { ok: true, data: [...newer.data, ...rest.data] }
  }

  // place puts a read's stretches in place of the replaced stretches it read again,
  // keeping every stretch past them.
  const place = (kind: Running['kind'], read: Stretch[], replaced: number) => {
    const was = loaded

    if (kind === 'scratch' || was === undefined) {
      // From scratch, a page read past the old ones goes with them.
      if (more !== null) {
        cancel(more)
        more = null
      }

      loaded = read

      return
    }

    if (kind === 'first') {
      // A page read past the first while it was read as the first, or still on its way,
      // wouldn't meet it: the newest are read again by their bounds, which does, and the
      // page keeps its place.
      if (was.length > 1 || more !== null) {
        queued = queued ?? 'head'

        return
      }

      loaded = read

      return
    }

    loaded = [...read, ...was.slice(replaced)]
  }

  // settle ends the read mine with its result, then starts the one waiting: a read from
  // scratch once restart says the history was cleared.
  const settle = (mine: Running, result: Result<Stretch[]>, replaced: number, restart = false) => {
    running = null

    if (restart) {
      queued = null
      current = false
      tell()
      begin('scratch')

      return
    }

    if (mine.kind !== 'head') {
      current = true
    }

    if (result.ok) {
      error = ''
      place(mine.kind, result.data, replaced)
    } else {
      error = result.error
    }

    const next = queued

    queued = null
    tell()

    if (next !== null) {
      begin(next)
    }
  }

  // run reads what mine asks for over was, what was loaded as it began, and settles it.
  const run = async (mine: Running, was: Loaded | undefined) => {
    if (mine.kind === 'first' || mine.kind === 'scratch' || was === undefined) {
      const result = await ask(mine, autopilotActivity(0, pageSize))

      if (result !== undefined) {
        settle(mine, result.ok ? { ok: true, data: [firstStretch(result.data)] } : result, 0)
      }

      return
    }

    const stretches = mine.kind === 'head' ? was.slice(0, 1) : was
    const edge = edgeOf(was[0])
    const out: Stretch[] = []

    for (const [i, s] of stretches.entries()) {
      const result = i === 0 ? await rereadNewest(mine, s.lower) : await reread(mine, s.lower, s.upper)

      if (result === undefined) {
        return
      }

      if (!result.ok) {
        settle(mine, result, 0)

        return
      }

      // The newest stretch read again ends at the match it began at, unless the history
      // was cleared since: then nothing loaded holds, and Activity starts again.
      if (i === 0 && edge !== undefined && !holds(result.data.at(-1)?.page, edge)) {
        settle(mine, result, 0, true)

        return
      }

      out.push(...result.data)
    }

    settle(mine, { ok: true, data: out }, stretches.length)
  }

  const begin = (kind: Kind | 'scratch') => {
    const was = loaded
    // The first page alone, while nothing past it is loaded or on its way, is read as the
    // first page is, so it stays a page long.
    const first = was === undefined || (was.length === 1 && more === null)
    const mine: Running = { kind: kind === 'scratch' ? 'scratch' : first ? 'first' : kind, call: null }

    running = mine
    void run(mine, was)
  }

  const refresh = (kind: Kind) => {
    if (running !== null) {
      // A read of every stretch covers a read of the newest under way, and one waiting,
      // which go.
      if (kind === 'deep' && (running.kind === 'head' || running.kind === 'first')) {
        if (running.call !== null) {
          cancel(running.call)
        }

        running = null
        queued = null
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
        if (running.call !== null) {
          cancel(running.call)
        }

        running = null
      }

      if (more !== null) {
        cancel(more)
        more = null
      }

      queued = null
      current = false
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

      // The page goes past what is loaded only while it still begins where the oldest
      // stretch does; after a fresh start from the newest it would leave a gap, and the
      // user asks again.
      const now = loaded

      if (now !== undefined && cursorOf(now).more && cursorOf(now).next === cursor.next) {
        const page = result.data

        loaded = [...now, { lower: page.more ? page.next : 0, upper: cursor.next, page }]
      }

      tell()
    },
  }
}

// ActivityPages is Activity as the tab shows it: the days loaded, newest first, the items
// the routines leave alone, whether there is more, and a way to load it.
export interface ActivityPages extends View {
  loadMore: () => Promise<void>
  // reload reads every stretch loaded again now, as after a change the tab made to a line
  // further down than the newest.
  reload: () => void
}

// useAutopilotActivity reads Activity's newest page as the tab first shows, a page more
// each time the user scrolls to the end (loadMore), the newest stretch again whenever
// Autopilot says it changed, and every stretch loaded again whenever the tab shows again,
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
