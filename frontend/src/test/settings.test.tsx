import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { useNow } from '../lib/useNow'
import type { Settings } from '../platform'
import { FIXED_NOW } from './clock'
import { settings } from './fixtures'
import { renderSaving, type SavingProps } from './settings'

// Snooze is a section that makes Snooze a day longer, and says when it shows it.
function Snooze({ settings: current, onSaved }: SavingProps) {
  const now = useNow()

  return (
    <button
      type="button"
      onClick={() => {
        onSaved({ ...current, snoozeDays: current.snoozeDays + 1 })
      }}
    >
      {current.snoozeDays} days at {now.getTime() === FIXED_NOW.getTime() ? 'noon' : 'another time'}
    </button>
  )
}

describe('renderSaving', () => {
  it('shows each stored change, so the next starts from it, and hands each to onSaved', async () => {
    const { onSaved } = renderSaving(Snooze, settings({ snoozeDays: 3 }))

    await userEvent.click(screen.getByRole('button', { name: '3 days at noon' }))
    await userEvent.click(screen.getByRole('button', { name: '4 days at noon' }))

    expect(screen.getByRole('button', { name: '5 days at noon' })).toBeInTheDocument()
    expect(onSaved.mock.calls).toEqual([[settings({ snoozeDays: 4 })], [settings({ snoozeDays: 5 })]])
  })

  it('renders on the clock given, or on the window’s own without one', () => {
    const { unmount } = renderSaving(Snooze, settings(), { now: new Date('2026-06-15T13:00:00Z') })
    expect(screen.getByRole('button', { name: '3 days at another time' })).toBeInTheDocument()
    unmount()

    renderSaving(Snooze, settings(), { now: null })
    expect(screen.getByRole('button', { name: '3 days at another time' })).toBeInTheDocument()
  })

  it('shows settings that are still loading', () => {
    renderSaving<Settings | null>(({ settings: current }) => <p>{current === null ? 'Loading' : 'Loaded'}</p>, null)

    expect(screen.getByText('Loading')).toBeInTheDocument()
  })
})
