import { describe, expect, it, vi } from 'vitest'

import { FIXED_NOW, isoAgo } from '../test/clock'
import {
  capitalize,
  dayHeading,
  dayPassed,
  formatAge,
  formatAgo,
  formatDateTime,
  formatDay,
  formatFull,
  formatTimeOfDay,
  formatUntil,
  formatWhen,
  unixISO,
} from './time'

// Tests run in UTC (vitest.config.ts); FIXED_NOW is a Monday.

// plain folds the narrow and no-break spaces some locales put in times into plain ones.
function plain(text: string): string {
  return text.replace(/\s/g, ' ')
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
    expect(formatAge(FIXED_NOW, isoAgo({ s: seconds }))).toBe(want)
  })

  it('reads a future time as just now', () => {
    expect(formatAge(FIXED_NOW, isoAgo({ s: -120 }))).toBe('just now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatAge(FIXED_NOW, '')).toBe('')
    expect(formatAge(FIXED_NOW, 'not a time')).toBe('')
  })
})

describe('formatUntil', () => {
  it('formats the time remaining', () => {
    expect(formatUntil(FIXED_NOW, isoAgo({ s: -42 * 60 }))).toBe('42m')
    expect(formatUntil(FIXED_NOW, isoAgo({ s: -2 * 3600 }))).toBe('2h')
  })

  it('reads a passed or imminent time as now', () => {
    expect(formatUntil(FIXED_NOW, isoAgo({ s: 30 }))).toBe('now')
    expect(formatUntil(FIXED_NOW, isoAgo({ s: -10 }))).toBe('now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatUntil(FIXED_NOW, '')).toBe('')
    expect(formatUntil(FIXED_NOW, 'soon')).toBe('')
  })
})

describe('formatAgo', () => {
  it('reads an age as a phrase', () => {
    expect(formatAgo(FIXED_NOW, isoAgo({ s: 5 * 60 }))).toBe('5m ago')
    expect(formatAgo(FIXED_NOW, isoAgo({ s: 3 * 86400 }))).toBe('3d ago')
    expect(formatAgo(FIXED_NOW, isoAgo({ s: 20 }))).toBe('just now')
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

describe('dayHeading', () => {
  it.each([
    ['2026-06-15T23:59:00Z', 'Today'],
    ['2026-06-15T00:00:00Z', 'Today'],
    ['2026-06-16T09:00:00Z', 'Today'],
    ['2026-06-14T23:59:00Z', 'Yesterday'],
    ['2026-06-13T12:00:00Z', 'Saturday'],
    ['2026-06-09T12:00:00Z', 'Tuesday'],
    ['2026-06-08T12:00:00Z', 'Jun 8'],
    ['2026-01-02T12:00:00Z', 'Jan 2'],
    ['2025-12-30T12:00:00Z', 'Dec 30, 2025'],
  ])('names %s against Monday noon as %j', (iso, want) => {
    expect(dayHeading(FIXED_NOW, iso, 'en-US')).toBe(want)
  })

  it('names nothing for a time it cannot read', () => {
    expect(dayHeading(FIXED_NOW, '', 'en-US')).toBe('')
    expect(dayHeading(FIXED_NOW, 'not a time', 'en-US')).toBe('')
  })

  it('follows the locale', () => {
    expect(dayHeading(FIXED_NOW, '2026-06-13T12:00:00Z', 'de-DE')).toBe('Samstag')
    expect(dayHeading(FIXED_NOW, '2025-12-30T12:00:00Z', 'de-DE')).toBe('30. Dez. 2025')
  })
})

describe('formatTimeOfDay', () => {
  it.each([
    ['2026-06-18T11:30:00Z', '11:30 AM'],
    ['2026-06-18T00:05:00Z', '12:05 AM'],
    ['2026-06-18T12:00:00Z', '12:00 PM'],
    ['2026-06-18T23:59:59Z', '11:59 PM'],
  ])('names %s as %j', (iso, want) => {
    expect(plain(formatTimeOfDay(iso, 'en-US'))).toBe(want)
  })

  it('follows the locale', () => {
    expect(formatTimeOfDay('2026-06-18T23:59:00Z', 'de-DE')).toBe('23:59')
  })

  it('names nothing for a time it cannot read', () => {
    expect(formatTimeOfDay('')).toBe('')
    expect(formatTimeOfDay('not a time')).toBe('')
  })

  // localeTime is how the activity list named a line's time before it shared this one.
  const localeTime = (iso: string, locale?: string) => {
    const ms = Date.parse(iso)

    return Number.isNaN(ms) ? '' : new Date(ms).toLocaleTimeString(locale, { hour: 'numeric', minute: '2-digit' })
  }

  it.each([undefined, 'en-US', 'en-GB', 'de-DE', 'ja-JP'])('writes what toLocaleTimeString does, in %s', (locale) => {
    const times = ['2026-06-18T11:30:00Z', '2026-06-18T00:05:00Z', '2026-06-18T23:59:59Z', '', 'not a time']

    expect(times.map((iso) => formatTimeOfDay(iso, locale))).toEqual(times.map((iso) => localeTime(iso, locale)))
  })
})

describe('unixISO', () => {
  it('writes seconds since the epoch as an ISO timestamp', () => {
    expect(unixISO(1781517600)).toBe('2026-06-15T10:00:00.000Z')
    expect(unixISO(1.5)).toBe('1970-01-01T00:00:01.500Z')
    expect(unixISO(-1)).toBe('1969-12-31T23:59:59.000Z')
  })

  it('writes 0 as the epoch itself', () => {
    expect(unixISO(0)).toBe('1970-01-01T00:00:00.000Z')
  })

  it('reads back as the same time', () => {
    expect(formatAgo(FIXED_NOW, unixISO(FIXED_NOW.getTime() / 1000 - 3 * 3600))).toBe('3h ago')
  })

  it('throws for a time Date can’t hold, as new Date does', () => {
    expect(() => unixISO(Number.NaN)).toThrow(RangeError)
    expect(() => unixISO(8.64e12 + 1)).toThrow(RangeError)
  })
})
