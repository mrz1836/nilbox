import { describe, expect, it } from 'vitest'

import { capitalize, formatAge, formatAgo, formatFull, formatUntil, formatWhen } from './time'

// Tests run in UTC (vitest.config.ts); now is a Monday.
const now = new Date('2026-06-15T12:00:00Z')

// plain folds the narrow and no-break spaces some locales put in times into plain ones.
function plain(text: string): string {
  return text.replace(/\s/g, ' ')
}

// ago returns the RFC 3339 time the given number of seconds before now.
function ago(seconds: number): string {
  return new Date(now.getTime() - seconds * 1000).toISOString()
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
    expect(formatAge(now, ago(seconds))).toBe(want)
  })

  it('reads a future time as just now', () => {
    expect(formatAge(now, ago(-120))).toBe('just now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatAge(now, '')).toBe('')
    expect(formatAge(now, 'not a time')).toBe('')
  })
})

describe('formatUntil', () => {
  it('formats the time remaining', () => {
    expect(formatUntil(now, ago(-42 * 60))).toBe('42m')
    expect(formatUntil(now, ago(-2 * 3600))).toBe('2h')
  })

  it('reads a passed or imminent time as now', () => {
    expect(formatUntil(now, ago(30))).toBe('now')
    expect(formatUntil(now, ago(-10))).toBe('now')
  })

  it('returns an empty string for an empty or invalid time', () => {
    expect(formatUntil(now, '')).toBe('')
    expect(formatUntil(now, 'soon')).toBe('')
  })
})

describe('formatAgo', () => {
  it('reads an age as a phrase', () => {
    expect(formatAgo(now, ago(5 * 60))).toBe('5m ago')
    expect(formatAgo(now, ago(3 * 86400))).toBe('3d ago')
    expect(formatAgo(now, ago(20))).toBe('just now')
    expect(formatAgo(now, '')).toBe('')
  })
})

describe('formatWhen', () => {
  it('names a time the way people say it', () => {
    expect(plain(formatWhen(now, new Date('2026-06-15T18:30:00Z'), 'en-US'))).toBe('today, 6:30 PM')
    expect(plain(formatWhen(now, new Date('2026-06-16T09:00:00Z'), 'en-US'))).toBe('tomorrow, 9:00 AM')
    expect(plain(formatWhen(now, new Date('2026-06-18T12:00:00Z'), 'en-US'))).toBe('Thu, 12:00 PM')
    expect(formatWhen(now, new Date('2026-06-22T12:00:00Z'), 'en-US')).toBe('Mon, Jun 22')
    expect(formatWhen(now, new Date('2027-01-04T12:00:00Z'), 'en-US')).toBe('Mon, Jan 4, 2027')
  })

  it('counts calendar days, not 24-hour spans', () => {
    const late = new Date('2026-06-15T23:30:00Z')

    expect(plain(formatWhen(late, new Date('2026-06-16T00:30:00Z'), 'en-US'))).toBe('tomorrow, 12:30 AM')
  })

  it('names a time in full for a tooltip', () => {
    expect(plain(formatFull('2026-06-18T12:00:00Z', 'en-US'))).toBe('Thursday, June 18, 2026 at 12:00 PM')
    expect(formatFull('', 'en-US')).toBe('')
  })

  it('capitalizes a phrase', () => {
    expect(capitalize('tomorrow, 9:00 AM')).toBe('Tomorrow, 9:00 AM')
    expect(capitalize('')).toBe('')
  })
})
