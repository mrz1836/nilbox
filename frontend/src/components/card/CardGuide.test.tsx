import { act, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CardPrefsContext, defaultCardPrefs, type CardPrefs } from '../../lib/cardPrefs'
import { at } from '../../test/clock'
import { mocks } from '../../test/platform'
import { CardGuide, guideParts } from './CardGuide'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

afterEach(() => {
  vi.unstubAllGlobals()
})

// showGuide renders the guide in variant, with the cards' settings as prefs says.
function showGuide(variant: 'full' | 'compact', prefs: Partial<CardPrefs> = {}) {
  return render(
    <CardPrefsContext value={{ ...defaultCardPrefs, ...prefs }}>
      <CardGuide variant={variant} />
    </CardPrefsContext>,
    { wrapper: at() },
  )
}

// sample is the guide's sample card, the real one, inside its inert wrapper.
function sample(): HTMLElement {
  const card = document.querySelector<HTMLElement>('[inert] li[data-item-id]')

  if (card === null) {
    throw new Error('no sample card')
  }

  return card
}

// markers are the guide's numbered markers, by the part each numbers.
function markers(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>('[data-marker]')]
}

// rect is a box at left and top, width by height, as the layout would measure it.
function rect(left: number, top: number, width: number, height: number): DOMRect {
  return DOMRect.fromRect({ x: left, y: top, width, height })
}

// box is where the sample's box, which the markers are placed in, sits on the page.
const box = rect(100, 50, 900, 120)

// laidOut is each part's box, as a layout of the sample would put it: the action column's
// first button for its actions.
const laidOut: Readonly<Record<string, DOMRect>> = {
  edge: rect(100, 78, 3, 64),
  avatar: rect(117, 96, 28, 28),
  title: rect(159, 88, 340, 20),
  story: rect(159, 112, 70, 16),
  byline: rect(237, 112, 260, 16),
  risk: rect(505, 112, 120, 18),
  track: rect(700, 92, 70, 19),
  actions: rect(840, 95, 70, 30),
}

// nothing is the box of what isn't laid out.
const nothing = rect(0, 0, 0, 0)

// layOut stands in for the layout jsdom lacks: the sample's box and its parts measure as
// parts says (the action column by its first button), and everything else as nothing. A
// test changes parts to lay the sample out again.
function layOut(parts: Record<string, DOMRect>) {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function measure(this: Element) {
    if (this.firstElementChild?.hasAttribute('inert') === true) {
      return box
    }

    const parent = this.parentElement

    if (parent?.getAttribute('data-guide') === 'actions' && parent.firstElementChild === this) {
      return parts.actions ?? nothing
    }

    const part = this.getAttribute('data-guide')

    return part === null || part === 'actions' ? nothing : (parts[part] ?? nothing)
  })
}

// relayOut tells the guide its box changed size, as the layout would.
function relayOut() {
  act(() => {
    for (const callback of resized) {
      callback()
    }
  })
}

// resized are the callbacks of the ResizeObservers made, which a test calls as a layout
// change would.
let resized: (() => void)[] = []

// followSizes stands in for ResizeObserver, keeping each callback for the test to call.
function followSizes() {
  resized = []
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: () => void) {
        resized.push(callback)
      }

      observe() {
        // The test reports changes itself.
      }

      disconnect() {
        // Nothing to stop following.
      }
    },
  )
}

describe('CardGuide', () => {
  it('shows the real card on its sample, inert, with every part it numbers', () => {
    showGuide('full')

    const card = sample()

    expect(card.closest('[inert]')).not.toBeNull()
    expect(card).toHaveAttribute('data-rule', 'rereview')
    expect(card).not.toHaveAttribute('data-density')
    expect(within(card).getByText('feat(client): accept a caller-supplied connection gater')).toBeInTheDocument()
    expect(within(card).getByText('Re-review', { selector: '[data-guide="story"]' })).toBeInTheDocument()
    expect(card.querySelector('[data-guide="byline"]')).toHaveTextContent(
      'acme-org/example-repo #150 · contributor-a · 18m',
    )
    expect(within(card).getByText('Outside · Workflows')).toBeInTheDocument()
    expect(within(card).getByText('feature')).toBeInTheDocument()
    expect(within(card).getByText('fork-pr')).toBeInTheDocument()
    expect(within(card).getByText('Findings')).toBeInTheDocument()
    expect(card.querySelector('[data-avatar="initials"]')).toHaveTextContent('CA')
    expect(card.querySelector('[data-unread]')).not.toBeNull()
    expect(card.querySelector('[data-step="review"]')).toHaveAttribute('data-state', 'rereview')

    for (const { part } of guideParts) {
      expect(card.querySelector(`[data-guide="${part}"]`), `the ${part} part`).not.toBeNull()
    }
  })

  it('asks nothing of the platform while it shows', () => {
    const { unmount } = showGuide('full')

    unmount()
    showGuide('compact')

    expect(mocks().filter(([, fn]) => fn.mock.calls.length > 0)).toEqual([])
  })

  it('stays the comfortable card while the cards are compact, numbering the parts the legend names', () => {
    showGuide('full', { density: 'compact' })

    expect(sample()).not.toHaveAttribute('data-density')
    expect(sample().querySelector('[data-guide="avatar"]')).not.toBeNull()
  })

  it('hides its markers until their parts are measured, then places each over its part', () => {
    followSizes()
    showGuide('full')

    expect(markers().map((m) => m.getAttribute('data-marker'))).toEqual(guideParts.map((g) => g.part))
    expect(markers().map((m) => m.textContent)).toEqual(['1', '2', '3', '4', '5', '6', '7', '8'])
    for (const marker of markers()) {
      expect(marker).not.toBeVisible()
      expect(marker).toHaveAttribute('aria-hidden', 'true')
    }

    layOut({ ...laidOut })
    relayOut()

    // place is where a marker's anchor sits in the box: the edge's center, the start of the
    // top of a part it sits above, or the start of the bottom of a part it sits under.
    const place = (part: string): [string, string] => {
      const r = laidOut[part] ?? nothing
      const side = guideParts.find((g) => g.part === part)?.side

      if (side === 'center') {
        return [`${String(r.left + r.width / 2 - box.left)}px`, `${String(r.top + r.height / 2 - box.top)}px`]
      }

      return [`${String(r.left - box.left)}px`, `${String((side === 'below' ? r.bottom : r.top) - box.top)}px`]
    }

    for (const marker of markers()) {
      const part = marker.getAttribute('data-marker') ?? ''

      expect(marker, `the ${part} marker`).toBeVisible()
      expect([marker.style.left, marker.style.top], `the ${part} marker`).toEqual(place(part))
    }

    expect(place('edge')).toEqual(['1.5px', '60px'])
    expect(place('story')).toEqual(['59px', '78px'])
    expect(place('actions')).toEqual(['740px', '45px'])
  })

  it('hides a marker again once its part is no longer laid out, as a narrow list drops the track', () => {
    const parts = { ...laidOut }
    followSizes()
    layOut(parts)
    showGuide('full')

    const track = () => document.querySelector('[data-marker="track"]')
    expect(track()).toBeVisible()

    parts.track = nothing
    relayOut()

    expect(track()).not.toBeVisible()
    expect(document.querySelector('[data-marker="title"]')).toBeVisible()
  })

  it('names each numbered part in its legend', () => {
    showGuide('full')

    const legend = screen.getByRole('list', { name: 'The parts of a card' })

    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual([
      '1Whose moveThe left edge: accent, your move; amber, something waits on you now; blue, an agent is on it; green, ready to ship; red, blocked; grey, waiting on others.',
      '2Who and whatThe author’s picture, people round and bots square, with a pull request or issue badge (grey for a draft).',
      '3Title and labelsUp to two labels after the title, the rest as “+N”.',
      '4The storyWhat changed, or what’s asked of you, in one sentence.',
      '5The bylineRepository, number, author, why it’s here, and when it last moved. Click the repository or the author to filter by it.',
      '6RiskShown only when there is some: an outside contributor, or sensitive files.',
      '7Where it standsChecks, review, merge: a tick when fine, a symbol when not, with the size below. Hover the card for Snooze, Dismiss, Pin, and more.',
      '8Your move, and the agent’sYour move is the filled button; the agent’s sits beside it, or its live status while it works.',
    ])
  })

  // key reads the key's groups: each caption with its entries' words.
  function key(): [string, string[]][] {
    return ['Whose move', 'The track', 'Who', 'Everything else'].map((caption) => [
      caption,
      within(screen.getByRole('list', { name: caption }))
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ])
  }

  it('keys the edge’s colors, the track’s symbols, who opened it, and the unread dot', () => {
    showGuide('full')

    expect(key()).toEqual([
      [
        'Whose move',
        [
          'Something waits on you now',
          'Blocked',
          'Your move',
          'Ready to ship',
          'An agent is on it',
          'Waiting on others',
        ],
      ],
      [
        'The track',
        [
          'Fine',
          'Failing, or changes requested',
          'Pushed since your review',
          'Running',
          'Waiting for approval',
          'Waiting',
        ],
      ],
      ['Who', ['CAA person', 'A bot', 'New since you last opened it']],
      ['Everything else', ['Click a card’s empty space to see everything else.']],
    ])

    const swatches = [...document.querySelectorAll('[data-swatch]')].map((s) => s.getAttribute('data-swatch'))
    expect(swatches).toEqual(['urgent', 'blocked', 'yours', 'ready', 'agent', 'waiting'])
  })

  it('leaves the unread dot out of the card and the key while Unread dots is off', () => {
    showGuide('full', { unread: false })

    expect(sample().querySelector('[data-unread]')).toBeNull()
    expect(key()[2]).toEqual(['Who', ['CAA person', 'A bot']])
  })

  it('shows the sample card and the key alone when compact, with no markers and no legend', () => {
    showGuide('compact')

    expect(sample()).toBeInTheDocument()
    expect(markers()).toEqual([])
    expect(screen.queryByRole('list', { name: 'The parts of a card' })).not.toBeInTheDocument()
    expect(key().map(([caption]) => caption)).toEqual(['Whose move', 'The track', 'Who', 'Everything else'])
  })
})
