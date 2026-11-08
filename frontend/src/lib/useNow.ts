import { useSyncExternalStore } from 'react'

// tickMs is how often the clock moves on. The window shows time to the minute ("5m",
// "resets in 42m"), and snoozes and budget levels change at most that often.
const tickMs = 60_000

// Clock is a time the whole window shares, moving on every interval while anything
// reads it.
export interface Clock {
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => Date
}

// createClock returns a Clock that reads the time with read. It runs one interval
// however many components read it, and none while nothing does; the first reader after
// a pause longer than the interval reads the time afresh.
export function createClock(read: () => Date = () => new Date(), intervalMs: number = tickMs): Clock {
  const listeners = new Set<() => void>()
  let now = read()
  let timer: ReturnType<typeof setInterval> | undefined

  const tick = () => {
    now = read()

    for (const listener of listeners) {
      listener()
    }
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
  }
}

// appClock is the window's clock.
const appClock = createClock()

// useNow returns the shared clock's time and renders again each time it moves on, so a
// component reads the time where it shows it and nothing else renders for the tick.
export function useNow(clock: Clock = appClock): Date {
  return useSyncExternalStore(clock.subscribe, clock.getSnapshot)
}
