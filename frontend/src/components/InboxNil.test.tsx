import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { arrivalBurst, earnedBurst, earnedQuip, quips, replayBurst } from '../lib/nil'
import { triageInbox } from '../lib/triage'
import type { InboxSnapshot } from '../platform'
import { at, FIXED_NOW } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import { InboxNil } from './InboxNil'

const empty = snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' })

// renderNil throws the party for an inbox, as triaged at now, on a clock that stands at now.
function renderNil(inbox: InboxSnapshot, earned: boolean) {
  return render(<InboxNil stats={triageInbox(inbox, FIXED_NOW)} earned={earned} />, { wrapper: at() })
}

// party returns the party's region.
function party() {
  return screen.getByRole('region', { name: 'inbox == nil' })
}

// confetti counts the particles in flight, and flashes the bursts that light the window.
function confetti() {
  return screen.queryAllByTestId('confetti').length
}

function flashes() {
  return screen.queryAllByTestId('confetti-flash').length
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('InboxNil', () => {
  it('announces the inbox at nil with the badge, a quip, and the terminal', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderNil(empty, false)

    expect(screen.getByRole('heading', { name: 'inbox == nil' })).toBeInTheDocument()
    expect(within(party()).getByTestId('nil-badge')).toHaveTextContent('inboxnil')
    expect(screen.queryByText('Achievement unlocked')).not.toBeInTheDocument()
    expect(screen.getByText(quips[0] ?? '')).toBeInTheDocument()

    const terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText(`${brand.slug} status`)).toBeInTheDocument()
    for (const label of ['review requested', 'authored', 'assigned']) {
      expect(terminal.getByText(label)).toBeInTheDocument()
    }
    expect(terminal.getAllByText('✓ clear')).toHaveLength(3)
    expect(terminal.getByText('panic: runtime error: nothing left to do')).toBeInTheDocument()
    expect(terminal.getByText('exit status 0')).toBeInTheDocument()
    // Nothing was set aside, so the terminal does not mention it.
    expect(terminal.queryByText('snoozed')).not.toBeInTheDocument()
    expect(terminal.queryByText('dismissed')).not.toBeInTheDocument()
  })

  it('counts what was set aside and when the first snooze comes back', () => {
    const setAside = snapshot({
      lastRefreshedAt: '2026-06-15T11:55:00Z',
      authored: list([
        item({ id: 1, snoozedUntil: '2026-06-16T09:00:00Z' }),
        item({ id: 2, snoozedUntil: '2026-06-18T09:00:00Z' }),
        item({ id: 3, dismissedAt: '2026-06-14T09:00:00Z' }),
      ]),
    })
    renderNil(setAside, false)

    const terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText('snoozed').parentElement).toHaveTextContent(/snoozed2\/\/ they’ll be back/)
    expect(terminal.getByText('dismissed').parentElement).toHaveTextContent(/dismissed1\/\/ rest in peace/)
    expect(terminal.getByText(/next one back tomorrow, 9:00/)).toBeInTheDocument()
  })

  it('sprinkles confetti on arrival', () => {
    renderNil(empty, false)

    expect(confetti()).toBe(arrivalBurst)
    expect(flashes()).toBe(0)
  })

  it('starts the party once when development mounts it twice', () => {
    render(
      <StrictMode>
        <InboxNil stats={triageInbox(empty, FIXED_NOW)} earned={false} />
      </StrictMode>,
      { wrapper: at() },
    )

    expect(confetti()).toBe(arrivalBurst)
  })

  it('throws the full party, flash and all, when the inbox was emptied', () => {
    renderNil(empty, true)

    expect(screen.getByText('Achievement unlocked')).toBeInTheDocument()
    expect(screen.queryByTestId('nil-badge')).not.toBeInTheDocument()
    expect(screen.getByText(earnedQuip)).toBeInTheDocument()
    expect(confetti()).toBe(earnedBurst)
    expect(flashes()).toBe(1)
    // The confetti flies over the whole window, outside the page, hidden from assistive
    // technology.
    expect(party()).not.toContainElement(screen.getAllByTestId('confetti')[0] ?? null)
  })

  it('runs the party again, harder each time, with a new quip', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderNil(empty, false)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(confetti()).toBe(arrivalBurst + replayBurst(1))
    expect(screen.getByText(quips[1] ?? '')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'go run ./party --again' }))
    await user.click(screen.getByRole('button', { name: 'go run ./party --again --harder' }))

    expect(screen.getByRole('button', { name: 'sudo go run ./party' })).toBeInTheDocument()
    expect(confetti()).toBe(arrivalBurst + replayBurst(1) + replayBurst(2) + replayBurst(3))
    // From the third run on, each burst lights the window.
    expect(flashes()).toBe(1)
  })

  it('throws nothing with reduced motion, but the replay still changes the quip', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
    renderNil(empty, true)

    expect(confetti()).toBe(0)

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(confetti()).toBe(0)
    expect(screen.getByText(quips[0] ?? '')).toBeInTheDocument()
  })

  it('clears the confetti once it lands', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    renderNil(empty, true)

    expect(confetti()).toBe(earnedBurst)

    act(() => {
      vi.advanceTimersByTime(5_000)
    })

    expect(confetti()).toBe(0)
    expect(flashes()).toBe(0)
  })
})
