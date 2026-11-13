import { describe, expect, it, vi } from 'vitest'

import { FIXED_NOW, fromNow } from '../test/clock'
import {
  capitalize,
  dayPassed,
  formatAge,
  formatAgo,
  formatDateTime,
  formatDay,
  formatFull,
  formatUntil,
  formatWhen,
} from './time'

// Tests run in UTC (vitest.config.ts); FIXED_NOW is a Monday.

// plain folds the narrow and no-break spaces some locales put in times into plain ones.
function plain(text: string): string {
  return text.replace(/\s/g, ' ')
}

// ago returns the RFC 3339 time the given number of seconds before now.
function ago(seconds: number): string {
  return fromNow(-seconds * 1000).toISOString()
}

describe('formatAge', () => {
  it.each([
    [0, 'just now'],
    [59, 'just now'],
    [60, '1m'],
    [59 * 60, '59m'],
    [3600, '1h'],
    [23 * 3600, '23h'],
    [86400, '1d'],
    [6 * 86400, '6d'],
    [7 * 86400, '1w'],
    [29 * 86400, '4w'],
    [30 * 86400, '1mo'],
    [364 * 86400, '12mo'],
    [365 * 86400, '1y'],
    [3 * 365 * 86400, '3y'],
  ])('formats %i seconds ago as %s', (seconds, want) => {
    expect(formatAge(FIXED_NOW, ago(seconds))).toBe(want)
  })

  it('reads a future time as just now', () => {
    expect(formatAge(FIXED_NOW, ago(-120))).toBe('just now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatAge(FIXED_NOW, '')).toBe('')
    expect(formatAge(FIXED_NOW, 'not a time')).toBe('')
  })
})

describe('formatUntil', () => {
  it('formats the time remaining', () => {
    expect(formatUntil(FIXED_NOW, ago(-42 * 60))).toBe('42m')
    expect(formatUntil(FIXED_NOW, ago(-2 * 3600))).toBe('2h')
  })

  it('reads a passed or imminent time as now', () => {
    expect(formatUntil(FIXED_NOW, ago(30))).toBe('now')
    expect(formatUntil(FIXED_NOW, ago(-10))).toBe('now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatUntil(FIXED_NOW, '')).toBe('')
    expect(formatUntil(FIXED_NOW, 'soon')).toBe('')
  })
})

describe('formatAgo', () => {
  it('reads an age as a phrase', () => {
    expect(formatAgo(FIXED_NOW, ago(5 * 60))).toBe('5m ago')
    expect(formatAgo(FIXED_NOW, ago(3 * 86400))).toBe('3d ago')
    expect(formatAgo(FIXED_NOW, ago(20))).toBe('just now')
    expect(formatAgo(FIXED_NOW, '')).toBe('')
  })
})

describe('formatWhen', () => {
  it('names a time the way people say it', () => {
    expect(plain(formatWhen(FIXED_NOW, new Date('2026-06-15T18:30:00Z'), 'en-US'))).toBe('today, 6:30 PM')
    expect(plain(formatWhen(FIXED_NOW, new Date('2026-06-16T09:00:00Z'), 'en-US'))).toBe('tomorrow, 9:00 AM')
    expect(plain(formatWhen(FIXED_NOW, new Date('2026-06-18T12:00:00Z'), 'en-US'))).toBe('Thu, 12:00 PM')
    expect(formatWhen(FIXED_NOW, new Date('2026-06-22T12:00:00Z'), 'en-US')).toBe('Mon, Jun 22')
    expect(formatWhen(FIXED_NOW, new Date('2027-01-04T12:00:00Z'), 'en-US')).toBe('Mon, Jan 4, 2027')
  })

  it('counts calendar days, not 24-hour spans', () => {
    const late = new Date('2026-06-15T23:30:00Z')

    expect(plain(formatWhen(late, new Date('2026-06-16T00:30:00Z'), 'en-US'))).toBe('tomorrow, 12:30 AM')
  })

  it('names a time in full for a tooltip', () => {
    expect(plain(formatFull('2026-06-18T12:00:00Z', 'en-US'))).toBe('Thursday, June 18, 2026 at 12:00 PM')
    expect(formatFull('', 'en-US')).toBe('')
  })

  it('names a time with its date for a list', () => {
    expect(plain(formatDateTime('2026-06-18T12:00:00Z', 'en-US'))).toBe('Jun 18, 2026, 12:00 PM')
    expect(formatDateTime('', 'en-US')).toBe('')
    expect(formatDateTime('not a time', 'en-US')).toBe('')
  })

  it('says a day GitHub keeps at midnight UTC has passed once the user’s own day is later', () => {
    expect(dayPassed(new Date(2026, 11, 1, 23, 59), '2026-12-01T00:00:00Z'), 'the due day itself').toBe(false)
    expect(dayPassed(new Date(2026, 11, 2, 0, 1), '2026-12-01T00:00:00Z')).toBe(true)
    expect(dayPassed(new Date(2026, 10, 30, 12), '2026-12-01T00:00:00Z')).toBe(false)
    expect(dayPassed(new Date(2026, 11, 2), '')).toBe(false)
  })

  it('names a day GitHub keeps at midnight UTC as that day', () => {
    expect(formatDay('2026-12-01T00:00:00Z', 'en-US')).toBe('Dec 1, 2026')
    expect(formatDay('', 'en-US')).toBe('')
  })

  it('makes each date format once, however many times it formats', () => {
    const made = vi.spyOn(Intl, 'DateTimeFormat')

    formatFull('2026-06-18T12:00:00Z', 'en-GB')
    formatFull('2026-06-19T12:00:00Z', 'en-GB')

    expect(made).toHaveBeenCalledOnce()
  })

  it('capitalizes a phrase', () => {
    expect(capitalize('tomorrow, 9:00 AM')).toBe('Tomorrow, 9:00 AM')
    expect(capitalize('')).toBe('')
  })
})
