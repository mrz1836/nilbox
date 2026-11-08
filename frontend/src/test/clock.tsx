import type { ReactNode } from 'react'

import { ClockContext, createClock, type Clock } from '../lib/useNow'

// FIXED_NOW is the time the tests' clocks stand at: noon UTC on Monday 15 June 2026. The
// fixtures' times are written around it (an item updated at nine that morning, a budget
// that resets at 12:42), so a test that reads the time from here sees them as the fixtures
// describe them.
export const FIXED_NOW = new Date('2026-06-15T12:00:00Z')

// fixedNow reads FIXED_NOW, for a component or hook that takes a function reading the time.
export function fixedNow(): Date {
  return FIXED_NOW
}

// fromNow is the time ms milliseconds after FIXED_NOW, or before it when ms is negative.
export function fromNow(ms: number): Date {
  return new Date(FIXED_NOW.getTime() + ms)
}

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

// at is a render wrapper whose clock stands still at now, FIXED_NOW unless given.
export function at(now: Date = FIXED_NOW) {
  return withClock(clockAt(now))
}
