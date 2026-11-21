import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { earnedBurst, earnedQuip, firstQuip, quips, replayBurst, type Flourish } from '../lib/nil'
import { triageInbox } from '../lib/triage'
import type { Celebrations, InboxSnapshot, NilMoment } from '../platform'
import { at, FIXED_NOW } from '../test/clock'
import { item, list, nilMoment, snapshot } from '../test/fixtures'
import { reduceMotion } from '../test/stubs'
import { InboxNil, type InboxNilProps } from './InboxNil'

const empty = snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' })
const stats = triageInbox(empty, FIXED_NOW)

// Moments the tests throw parties for: the first nil the user earns, which throws confetti;
// one that unlocked something on a day confetti already flew; an ordinary earned nil; and
// a fourth day found at nil.
const first = nilMoment({ earned: true, count: 1, unlocked: [{ kind: 'first', n: 1 }], confetti: true })
const capped = nilMoment({
  earned: true,
  count: 10,
  streak: 7,
  best: 7,
  unlocked: [
    { kind: 'streak', n: 7 },
    { kind: 'count', n: 10 },
  ],
})
const ordinary = nilMoment({ earned: true, count: 37 })
const fourthDay = nilMoment({ streak: 4, best: 9 })

// renderNil throws the party for an inbox, as triaged at now, on a clock that stands at now.
function renderNil(props: Partial<InboxNilProps> = {}, inbox: InboxSnapshot = empty) {
  return render(<InboxNil stats={triageInbox(inbox, FIXED_NOW)} earned={false} {...props} />, { wrapper: at() })
}

// party returns the party's region.
function party() {
  return screen.getByRole('region', { name: 'inbox == nil' })
}

// chip is the chip beside the heading, if one shows.
function chip() {
  return screen.queryByTestId('nil-chip')
}

// confetti counts the particles in flight, and flashes the bursts that light the window.
function confetti() {
  return screen.queryAllByTestId('confetti').length
}

function flashes() {
  return screen.queryAllByTestId('confetti-flash').length
}

// nudging reports whether the replay button wiggles, and flourish which flourish the party
// plays as it starts, if any.
function nudging(name = 'go run ./party') {
  return screen.getByRole('button', { name }).parentElement?.classList.contains('nil-nudge') ?? false
}

function flourish() {
  return party().dataset.flourish ?? null
}

describe('InboxNil', () => {
  it('announces the inbox at nil with the badge, a quip, and the terminal', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderNil()

    expect(screen.getByRole('heading', { name: 'inbox == nil' })).toBeInTheDocument()
    expect(within(party()).getByTestId('nil-badge')).toHaveTextContent('inboxnil')
    expect(chip()).not.toBeInTheDocument()
    expect(screen.getByText(quips[0] ?? '')).toBeInTheDocument()

    const terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText(`${brand.slug} status`)).toBeInTheDocument()
    for (const label of ['review requested', 'yours']) {
      expect(terminal.getByText(label)).toBeInTheDocument()
    }
    expect(terminal.getAllByText('✓ clear')).toHaveLength(2)
    expect(terminal.getByText('panic: runtime error: nothing left to do')).toBeInTheDocument()
    expect(terminal.getByText('exit status 0')).toBeInTheDocument()
    // Nothing was set aside, and no moment came back, so the terminal mentions neither.
    expect(terminal.queryByText('snoozed')).not.toBeInTheDocument()
    expect(terminal.queryByText('dismissed')).not.toBeInTheDocument()
    expect(terminal.queryByText('streak')).not.toBeInTheDocument()
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
    renderNil({}, setAside)

    const terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText('snoozed').parentElement).toHaveTextContent(/snoozed2\/\/ they’ll be back/)
    expect(terminal.getByText('dismissed').parentElement).toHaveTextContent(/dismissed1\/\/ rest in peace/)
    expect(terminal.getByText(/next one back tomorrow, 9:00/)).toBeInTheDocument()
  })

  it('keeps the streak, the best to beat, and the count in the terminal', () => {
    renderNil({ earned: true, moment: nilMoment({ earned: true, streak: 4, best: 9, count: 37 }) })

    const terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText('streak').parentElement).toHaveTextContent(/streak4\/\/ days running$/)
    expect(terminal.getByText('best').parentElement).toHaveTextContent(/best9\/\/ to beat/)
    expect(terminal.getByText('nil').parentElement).toHaveTextContent(/nil37\/\/ and counting/)
  })

  it('calls a new best a best, and day one day one, with no best or count to show', () => {
    const { unmount } = renderNil({ moment: nilMoment({ streak: 5, best: 5 }) })

    let terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText('streak').parentElement).toHaveTextContent(/streak5\/\/ days running, a new best/)
    expect(terminal.queryByText('best')).not.toBeInTheDocument()
    expect(terminal.queryByText('nil')).not.toBeInTheDocument()
    unmount()

    renderNil({ moment: nilMoment() })
    terminal = within(screen.getByRole('figure', { name: 'Terminal' }))
    expect(terminal.getByText('streak').parentElement).toHaveTextContent(/streak1\/\/ day one/)
  })

  it('throws no confetti on arrival, and plays a flourish and nudges the button instead', () => {
    renderNil({ moment: fourthDay })

    expect(confetti()).toBe(0)
    expect(flashes()).toBe(0)
    expect(flourish()).toBe('glint')
    expect(screen.getByTestId('nil-glint')).toBeInTheDocument()
    expect(nudging()).toBe(true)
    expect(chip()).toHaveTextContent('Day 4 at nil')
  })

  it('throws the full party, flash and all, for a moment that says confetti flies', () => {
    renderNil({ earned: true, moment: first, flourish: 'hop' })

    expect(chip()).toHaveTextContent('Achievement unlocked · First nil')
    expect(screen.queryByTestId('nil-badge')).not.toBeInTheDocument()
    expect(screen.getByText(firstQuip)).toBeInTheDocument()
    expect(confetti()).toBe(earnedBurst)
    expect(flashes()).toBe(1)
    // The confetti flies over the whole window, outside the page, hidden from assistive
    // technology.
    expect(party()).not.toContainElement(screen.getAllByTestId('confetti')[0] ?? null)
    // It needs no flourish or nudge.
    expect(flourish()).toBeNull()
    expect(document.querySelector('.nil-hop')).toBeNull()
    expect(nudging()).toBe(false)
  })

  it('starts the party once when development mounts it twice', () => {
    render(
      <StrictMode>
        <InboxNil stats={stats} earned moment={first} />
      </StrictMode>,
      { wrapper: at() },
    )

    expect(confetti()).toBe(earnedBurst)
  })

  it('shows what a nil unlocked without confetti once some has flown today', () => {
    renderNil({ earned: true, moment: capped })

    expect(chip()).toHaveTextContent('Achievement unlocked · 7-day streak +1 more')
    expect(chip()).toHaveAttribute('title', '7-day streak, 10 nils')
    expect(confetti()).toBe(0)
    expect(flourish()).toBe('glint')
    expect(nudging()).toBe(true)
  })

  it('notes an ordinary earned nil by its number, opening on the next earned quip', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.9)
    renderNil({ earned: true, moment: ordinary })

    expect(chip()).toHaveTextContent('Nil #37')
    expect(chip()).not.toHaveTextContent('Achievement unlocked')
    expect(screen.getByText(earnedQuip(37))).toBeInTheDocument()
    expect(confetti()).toBe(0)
  })

  it('opens an earned party with no moment on the first quip, with no chip', () => {
    renderNil({ earned: true })

    expect(screen.getByText(firstQuip)).toBeInTheDocument()
    expect(chip()).not.toBeInTheDocument()
    expect(screen.getByTestId('nil-badge')).toBeInTheDocument()
  })

  it.each([
    ['glint', '[data-testid="nil-glint"]'],
    ['hop', '.nil-hop'],
    ['glitch', '.nil-burst'],
    ['sparks', '[data-testid="nil-sparks"]'],
    ['stamp', '[data-testid="nil-chip"].nil-stamp'],
  ] satisfies [Flourish, string][])('plays the %s flourish', (name, selector) => {
    renderNil({ earned: true, moment: ordinary, flourish: name })

    expect(flourish()).toBe(name)
    expect(party().querySelector(selector)).not.toBeNull()
    // Only the one.
    expect(party().querySelectorAll('[data-testid="nil-glint"], .nil-hop, .nil-burst, .nil-stamp')).toHaveLength(
      name === 'sparks' ? 0 : 1,
    )
  })

  it('sends ten code glyphs up with the sparks', () => {
    renderNil({ moment: fourthDay, flourish: 'sparks' })

    const sparks = screen.getByTestId('nil-sparks')
    expect(sparks.querySelectorAll('.nil-spark')).toHaveLength(10)
    expect(sparks).toHaveAttribute('aria-hidden', 'true')
    // They stay inside the party, which clips them.
    expect(party()).toContainElement(sparks)
  })

  it('glints in place of a stamp with no chip to stamp', () => {
    renderNil({ moment: nilMoment(), flourish: 'stamp' })

    expect(flourish()).toBe('glint')
    expect(screen.getByTestId('nil-glint')).toBeInTheDocument()
  })

  it('runs the party again, harder each time, with a new quip', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderNil()

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
    reduceMotion()
    renderNil({ earned: true, moment: first })

    expect(confetti()).toBe(0)

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(confetti()).toBe(0)
    expect(screen.getByText(quips[0] ?? '')).toBeInTheDocument()
  })

  it('offers to collapse only when told how', async () => {
    const collapse = vi.fn()
    const { rerender } = renderNil()

    expect(screen.queryByRole('button', { name: 'Collapse the party' })).not.toBeInTheDocument()

    rerender(<InboxNil stats={stats} earned={false} onCollapse={collapse} />)
    await userEvent.click(screen.getByRole('button', { name: 'Collapse the party' }))

    expect(collapse).toHaveBeenCalledOnce()
  })

  it('clears the confetti once it lands', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    renderNil({ earned: true, moment: first })

    expect(confetti()).toBe(earnedBurst)

    act(() => {
      vi.advanceTimersByTime(5_000)
    })

    expect(confetti()).toBe(0)
    expect(flashes()).toBe(0)
  })
})

describe('InboxNil quiet', () => {
  it('keeps the chip and the flourish, and throws no confetti on its own', () => {
    renderNil({ earned: true, moment: first, mode: 'quiet', flourish: 'hop' })

    expect(chip()).toHaveTextContent('Achievement unlocked · First nil')
    expect(confetti()).toBe(0)
    expect(flourish()).toBe('hop')
    expect(nudging()).toBe(true)
  })

  it('still throws confetti from the button', async () => {
    renderNil({ earned: true, moment: first, mode: 'quiet' })

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(confetti()).toBe(replayBurst(1))
  })
})

describe('InboxNil as a strip', () => {
  // renderStrip throws the party as a strip, hearing its run and its collapse.
  function renderStrip(props: Partial<InboxNilProps> = {}) {
    const handlers = { onOpen: vi.fn(), onCollapse: vi.fn() }

    render(<InboxNil stats={stats} earned={false} compact {...handlers} {...props} />, { wrapper: at() })

    return handlers
  }

  it('keeps the heading and a quip, and leaves the terminal out', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    renderStrip()

    expect(screen.getByRole('heading', { name: 'inbox == nil' })).toBeInTheDocument()
    expect(within(party()).getByText(quips[0] ?? '')).toBeInTheDocument()
    expect(screen.queryByRole('figure', { name: 'Terminal' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('nil-badge')).not.toBeInTheDocument()
    // It is already folded, so there is nothing to collapse.
    expect(screen.queryByRole('button', { name: 'Collapse the party' })).not.toBeInTheDocument()
  })

  it('throws no confetti from the strip on arrival, and plays a flourish and nudges the button', () => {
    renderStrip({ moment: fourthDay, flourish: 'glitch' })

    expect(confetti()).toBe(0)
    expect(flourish()).toBe('glitch')
    expect(nudging()).toBe(true)
    expect(within(party()).getByTestId('nil-chip')).toHaveTextContent('Day 4 at nil')
  })

  it('unlocks the achievement in the strip, with the full burst, when the moment says so', () => {
    renderStrip({ earned: true, moment: first })

    expect(chip()).toHaveTextContent('Achievement unlocked · First nil')
    expect(screen.getByText(firstQuip)).toBeInTheDocument()
    expect(confetti()).toBe(earnedBurst)
    expect(flashes()).toBe(1)
  })

  it('runs the party, asking for it in full', async () => {
    const { onOpen } = renderStrip()

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(onOpen).toHaveBeenCalledOnce()
    expect(confetti()).toBe(replayBurst(1))
  })

  it('asks for nothing when the full party runs again', async () => {
    const open = vi.fn()
    render(<InboxNil stats={stats} earned={false} onOpen={open} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))

    expect(open).not.toHaveBeenCalled()
  })

  it('keeps its runs and quip when it opens in full', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const { rerender } = render(<InboxNil stats={stats} earned={false} compact />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'go run ./party' }))
    rerender(<InboxNil stats={stats} earned={false} />)

    expect(screen.getByRole('figure', { name: 'Terminal' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'go run ./party --again' })).toBeInTheDocument()
    expect(screen.getByText(quips[1] ?? '')).toBeInTheDocument()
  })
})

describe('InboxNil with celebrations off', () => {
  const off: Celebrations = 'off'

  it.each([
    ['a nil found empty', fourthDay],
    ['an earned nil that would throw confetti', first],
  ] satisfies [string, NilMoment][])('is a still strip for %s, with no chip, flourish, or button', (_, moment) => {
    render(<InboxNil stats={stats} earned={moment.earned} moment={moment} mode={off} compact flourish="sparks" />, {
      wrapper: at(),
    })

    expect(screen.getByRole('heading', { name: 'inbox == nil' })).toBeInTheDocument()
    expect(party()).toHaveClass('nil-still')
    expect(chip()).not.toBeInTheDocument()
    expect(flourish()).toBeNull()
    expect(screen.queryByTestId('nil-sparks')).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(confetti()).toBe(0)
  })
})
