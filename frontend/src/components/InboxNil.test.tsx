import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { confettiCooldownMs, earnedBurst, earnedQuip, quips, replayBurst } from '../lib/nil'
import { triageInbox } from '../lib/triage'
import type { InboxSnapshot } from '../platform'
import { at, FIXED_NOW, fromNow } from '../test/clock'
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

// nudging reports whether the replay button wiggles, and glinting whether a glint sweeps
// across the party: what a quiet entrance does in place of the confetti.
function nudging(name = 'go run ./party') {
  return screen.getByRole('button', { name }).parentElement?.classList.contains('nil-nudge') ?? false
}

function glinting() {
  return screen.queryByTestId('nil-glint') !== null
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
    for (const label of ['review requested', 'yours']) {
      expect(terminal.getByText(label)).toBeInTheDocument()
    }
    expect(terminal.getAllByText('✓ clear')).toHaveLength(2)
    expect(terminal.getByText('panic: runtime error: nothing left to do')).toBeInTheDocument()
    expect(terminal.getByText('exit status 0')).toBeInTheDocument()
    // Nothing was set aside, so the terminal does not mention it.
    expect(terminal.queryByText('snoozed')).not.toBeInTheDocument()
    expect(terminal.queryByText('dismissed')).not.toBeInTheDocument()
  })

  it('counts what was set aside and when the first snooze comes back', () => {
    const setAside = snapshot({
      lastRefreshedAt: '2026-06-15T11:55:00Z',
      yours: list([
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

  it('throws no confetti on arrival, and glints and nudges the button instead', () => {
    renderNil(empty, false)

    expect(confetti()).toBe(0)
    expect(flashes()).toBe(0)
    expect(glinting()).toBe(true)
    expect(nudging()).toBe(true)
  })

  it('starts the party once when development mounts it twice', () => {
    const fly = vi.fn()
    render(
      <StrictMode>
        <InboxNil stats={triageInbox(empty, FIXED_NOW)} earned onConfetti={fly} />
      </StrictMode>,
      { wrapper: at() },
    )

    expect(confetti()).toBe(earnedBurst)
    expect(fly).toHaveBeenCalledOnce()
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
    // It needs no glint or nudge.
    expect(glinting()).toBe(false)
    expect(nudging()).toBe(false)
  })

  it('holds the confetti back for a while after it last flew on its own', () => {
    const fly = vi.fn()
    const stats = triageInbox(empty, FIXED_NOW)
    const { unmount } = render(
      <InboxNil stats={stats} earned confettiAt={fromNow(1 - confettiCooldownMs)} onConfetti={fly} />,
      { wrapper: at() },
    )

    // Earned all the same, but quiet.
    expect(screen.getByText('Achievement unlocked')).toBeInTheDocument()
    expect(confetti()).toBe(0)
    expect(glinting()).toBe(true)
    expect(nudging()).toBe(true)
    expect(fly).not.toHaveBeenCalled()
    unmount()

    render(<InboxNil stats={stats} earned confettiAt={fromNow(-confettiCooldownMs)} onConfetti={fly} />, {
      wrapper: at(),
    })

    expect(confetti()).toBe(earnedBurst)
    expect(fly).toHaveBeenCalledExactlyOnceWith(FIXED_NOW)
  })

  it('runs the party again, harder each time, with a new quip', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderNil(empty, false)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(confetti()).toBe(replayBurst(1))
    expect(screen.getByText(quips[1] ?? '')).toBeInTheDocument()
    // Once run, the button has made its point and stops wiggling.
    expect(nudging('go run ./party --again')).toBe(false)

    await user.click(screen.getByRole('button', { name: 'go run ./party --again' }))
    await user.click(screen.getByRole('button', { name: 'go run ./party --again --harder' }))

    expect(screen.getByRole('button', { name: 'sudo go run ./party' })).toBeInTheDocument()
    expect(confetti()).toBe(replayBurst(1) + replayBurst(2) + replayBurst(3))
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

  it('offers to collapse only when told how', async () => {
    const collapse = vi.fn()
    const { rerender } = renderNil(empty, false)

    expect(screen.queryByRole('button', { name: 'Collapse the party' })).not.toBeInTheDocument()

    rerender(<InboxNil stats={triageInbox(empty, FIXED_NOW)} earned={false} onCollapse={collapse} />)
    await userEvent.click(screen.getByRole('button', { name: 'Collapse the party' }))

    expect(collapse).toHaveBeenCalledOnce()
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

describe('InboxNil as a strip', () => {
  // renderStrip throws the party as a strip, hearing its run and its collapse.
  function renderStrip(earned: boolean, onOpen = vi.fn(), onCollapse = vi.fn()) {
    return render(
      <InboxNil
        stats={triageInbox(empty, FIXED_NOW)}
        earned={earned}
        compact
        onOpen={onOpen}
        onCollapse={onCollapse}
      />,
      { wrapper: at() },
    )
  }

  it('keeps the heading and a quip, and leaves the terminal out', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderStrip(false)

    expect(screen.getByRole('heading', { name: 'inbox == nil' })).toBeInTheDocument()
    expect(within(party()).getByText(quips[0] ?? '')).toBeInTheDocument()
    expect(screen.queryByRole('figure', { name: 'Terminal' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('nil-badge')).not.toBeInTheDocument()
    // It is already folded, so there is nothing to collapse.
    expect(screen.queryByRole('button', { name: 'Collapse the party' })).not.toBeInTheDocument()
  })

  it('throws no confetti from the strip on arrival, and glints and nudges the button', () => {
    renderStrip(false)

    expect(confetti()).toBe(0)
    expect(glinting()).toBe(true)
    expect(nudging()).toBe(true)
  })

  it('unlocks the achievement in the strip, with the full burst, when earned', () => {
    renderStrip(true)

    expect(screen.getByText('Achievement unlocked')).toBeInTheDocument()
    expect(screen.getByText(earnedQuip)).toBeInTheDocument()
    expect(confetti()).toBe(earnedBurst)
    expect(flashes()).toBe(1)
  })

  it('runs the party, asking for it in full', async () => {
    const open = vi.fn()
    renderStrip(false, open)

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(open).toHaveBeenCalledOnce()
    expect(confetti()).toBe(replayBurst(1))
  })

  it('asks for nothing when the full party runs again', async () => {
    const open = vi.fn()
    render(<InboxNil stats={triageInbox(empty, FIXED_NOW)} earned={false} onOpen={open} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(open).not.toHaveBeenCalled()
  })

  it('keeps its runs and quip when it opens in full', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const stats = triageInbox(empty, FIXED_NOW)
    const { rerender } = renderStrip(false)

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))
    rerender(<InboxNil stats={stats} earned={false} />)

    expect(screen.getByRole('figure', { name: 'Terminal' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'go run ./party --again' })).toBeInTheDocument()
    expect(screen.getByText(quips[1] ?? '')).toBeInTheDocument()
  })
})
