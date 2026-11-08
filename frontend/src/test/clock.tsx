import type { ReactNode } from 'react'

import { ClockContext, createClock, type Clock } from '../lib/useNow'

// clockAt is a clock that reads the time from read, or stands still at a Date.
export function clockAt(read: Date | (() => Date)): Clock {
  return createClock(read instanceof Date ? () => read : read)
}

// withClock returns a wrapper for render that gives every component the clock, the way
// the window gives them its own.
export function withClock(clock: Clock) {
  return function ClockWrapper({ children }: { children: ReactNode }) {
    return <ClockContext value={clock}>{children}</ClockContext>
  }
}

// at is a render wrapper whose clock stands still at now.
export function at(now: Date) {
  return withClock(clockAt(now))
}
