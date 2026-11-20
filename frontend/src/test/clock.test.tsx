import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useNow } from '../lib/useNow'
import { renderAt } from './clock'

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
