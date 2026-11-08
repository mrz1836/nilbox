import { createContext, use, useSyncExternalStore } from 'react'

// tickMs is how often the clock moves on. The window shows time to the minute ("5m",
// "resets in 42m"), and snoozes and budget levels change at most that often.
const tickMs = 60_000

// Clock is a time the whole window shares, moving on every interval while anything
// reads it.
export interface Clock {
  subscribe: (listener: () => void) => () => void
  // getSnapshot is the clock's time: as of its last tick, or its last read.
  getSnapshot: () => Date
  // read reads the time afresh and moves the clock on to it, telling every reader, so
  // whatever just happened (an inbox arriving, an item snoozed) is measured from the same
  // moment everywhere. Call it from an event or an answer, never while rendering.
  read: () => Date
}

// createClock returns a Clock that reads the time with read. It runs one interval
// however many components read it, and none while nothing does; the first reader after
// a pause longer than the interval reads the time afresh.
export function createClock(read: () => Date = () => new Date(), intervalMs: number = tickMs): Clock {
  const listeners = new Set<() => void>()
  let now = read()
  let timer: ReturnType<typeof setInterval> | undefined

  const tell = () => {
    for (const listener of listeners) {
      listener()
    }
  }

  const tick = () => {
    now = read()
    tell()
  }

  return {
    subscribe: (listener) => {
      listeners.add(listener)

      if (timer === undefined) {
        const fresh = read()

        if (fresh.getTime() - now.getTime() >= intervalMs) {
          now = fresh
        }

        timer = setInterval(tick, intervalMs)
      }

      return () => {
        listeners.delete(listener)

        if (listeners.size === 0) {
          clearInterval(timer)
          timer = undefined
        }
      }
    },
    getSnapshot: () => now,
    read: () => {
      const fresh = read()

      if (fresh.getTime() !== now.getTime()) {
        now = fresh
        tell()
      }

      return fresh
    },
  }
}

// ClockContext is the clock every component below it reads: the window's own, unless a
// test provides one that stands still.
export const ClockContext = createContext<Clock>(createClock())

// useClock returns the window's clock, for reading the time when something happens
// rather than while rendering.
export function useClock(): Clock {
  return use(ClockContext)
}

// useNow returns the clock's time and renders again each time it moves on, so a
// component reads the time where it shows it and nothing else renders for the tick.
export function useNow(): Date {
  const clock = use(ClockContext)

  return useSyncExternalStore(clock.subscribe, clock.getSnapshot)
}

// useNowAs returns what show makes of the clock's time, and renders again only when that
// changes: a row that says "3d" sits out every tick until it says "4d". show must return
// a plain value (a string, a number, a boolean), which compares equal while it means
// the same.
export function useNowAs<T extends string | number | boolean | null>(show: (now: Date) => T): T {
  const clock = use(ClockContext)

  return useSyncExternalStore(clock.subscribe, () => show(clock.getSnapshot()))
}
