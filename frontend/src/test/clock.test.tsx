import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useNow } from '../lib/useNow'
import { FIXED_NOW, fromNow, isoAgo, renderAt } from './clock'

// Now shows the clock's time, after a label.
function Now({ label }: { label: string }) {
  return (
    <p>
      {label} {useNow().toISOString()}
    </p>
  )
}

describe('renderAt', () => {
  it('renders on a clock standing at FIXED_NOW, and keeps it for a rerender', () => {
    const { rerender } = renderAt(<Now label="first" />)

    expect(screen.getByText('first 2026-06-15T12:00:00.000Z')).toBeInTheDocument()

    rerender(<Now label="again" />)
    expect(screen.getByText('again 2026-06-15T12:00:00.000Z')).toBeInTheDocument()
  })

  it('renders on a clock standing at the time given', () => {
    renderAt(<Now label="then" />, new Date('2026-06-14T09:30:00Z'))

    expect(screen.getByText('then 2026-06-14T09:30:00.000Z')).toBeInTheDocument()
  })
})

describe('isoAgo', () => {
  it('is the RFC 3339 time a span before FIXED_NOW, adding up its units', () => {
    expect(isoAgo({})).toBe('2026-06-15T12:00:00.000Z')
    expect(isoAgo({ ms: 1500 })).toBe('2026-06-15T11:59:58.500Z')
    expect(isoAgo({ s: 90 })).toBe('2026-06-15T11:58:30.000Z')
    expect(isoAgo({ m: 2, s: 30 })).toBe('2026-06-15T11:57:30.000Z')
    expect(isoAgo({ h: 26 })).toBe('2026-06-14T10:00:00.000Z')
    expect(isoAgo({ m: -5 }), 'a negative span is later').toBe('2026-06-15T12:05:00.000Z')
  })

  it('writes what the helpers it stands in for wrote', () => {
    for (const n of [0, 1, 7, 59, 61, 1440, 4321]) {
      expect(isoAgo({ ms: n })).toBe(fromNow(-n).toISOString())
      expect(isoAgo({ s: n })).toBe(new Date(FIXED_NOW.getTime() - n * 1000).toISOString())
      expect(isoAgo({ m: n })).toBe(new Date(FIXED_NOW.getTime() - n * 60_000).toISOString())
      expect(isoAgo({ m: n, s: 7 })).toBe(fromNow(-(n * 60_000 + 7 * 1000)).toISOString())
    }
  })
})
