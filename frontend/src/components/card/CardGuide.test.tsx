import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CardPrefsContext, defaultCardPrefs, type CardPrefs } from '../../lib/cardPrefs'
import { at } from '../../test/clock'
import { mocks } from '../../test/platform'
import { followResizes } from '../../test/stubs'
import { CardGuide, guideParts } from './CardGuide'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

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
// first button, the primary, for its actions.
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

// columns are the boxes around the track and the primary, each wider than its part and
// starting before it: the state column, the track at its end; the action column; and the
// primary's slot in it. No marker sits over one.
const columns: Readonly<Record<string, DOMRect>> = {
  '[data-state-column]': rect(638, 88, 132, 40),
  '[data-guide="actions"]': rect(784, 92, 216, 36),
  '[data-primary-slot]': rect(784, 95, 140, 30),
}

// nothing is the box of what isn't laid out.
const nothing = rect(0, 0, 0, 0)

// isBox reports the sample's box, which the markers are placed in.
function isBox(el: Element): boolean {
  return el.firstElementChild?.hasAttribute('inert') === true
}

// isPrimary reports the action column's first button, the primary.
function isPrimary(el: Element): boolean {
  return el.closest('[data-guide="actions"]')?.querySelector('button') === el
}

// laidOutBox is where an element is laid out: the sample's box, a part as parts says (the
// action column by its primary), a column as columns says, and everything else nowhere.
function laidOutBox(el: Element, parts: Record<string, DOMRect>): DOMRect {
  if (isBox(el)) {
    return box
  }

  if (isPrimary(el)) {
    return parts.actions ?? nothing
  }

  const column = Object.keys(columns).find((selector) => el.matches(selector))

  if (column !== undefined) {
    return columns[column] ?? nothing
  }

  const part = el.getAttribute('data-guide')

  return part === null ? nothing : (parts[part] ?? nothing)
}

// popIn is a box as the dialog's pop-in (index.css) first draws it on the screen: scaled to
// 0.97 about the dialog's center, below the sample, and lifted 4px.
function popIn(r: DOMRect): DOMRect {
  const scale = 0.97
  const [x, y] = [550, 300]

  return rect(x + (r.left - x) * scale, y + (r.top - y) * scale - 4, r.width * scale, r.height * scale)
}

// layOut stands in for the layout jsdom lacks: everything is laid out as laidOutBox says,
// with parts, and measures on the screen as drawn draws it (as laid out, unless a test
// passes the pop-in), while the sample's box keeps its laid-out size. A test changes parts
// to lay the sample out again.
function layOut(parts: Record<string, DOMRect>, drawn: (r: DOMRect) => DOMRect = (r) => r) {
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function width(this: HTMLElement) {
    return isBox(this) ? box.width : 0
  })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function height(this: HTMLElement) {
    return isBox(this) ? box.height : 0
  })
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function measure(this: Element) {
    return drawn(laidOutBox(this, parts))
  })
}

// place is where a part's marker is anchored in the sample's box, from its laid-out box:
// the edge's center, the start of the top of a part it sits above, or the start of the
// bottom of a part it sits under.
function place(part: string): [number, number] {
  const r = laidOut[part] ?? nothing
  const side = guideParts.find((g) => g.part === part)?.side

  if (side === 'center') {
    return [r.left + r.width / 2 - box.left, r.top + r.height / 2 - box.top]
  }

  return [r.left - box.left, (side === 'below' ? r.bottom : r.top) - box.top]
}

// spotOf is where a marker is anchored, as its style places it.
function spotOf(marker: HTMLElement): [number, number] {
  return [Number.parseFloat(marker.style.left), Number.parseFloat(marker.style.top)]
}

// marker is the marker of a part.
function marker(part: string): HTMLElement {
  const found = document.querySelector<HTMLElement>(`[data-marker="${part}"]`)

  if (found === null) {
    throw new Error(`no ${part} marker`)
  }

  return found
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
    const resizes = followResizes()
    showGuide('full')

    expect(markers().map((m) => m.getAttribute('data-marker'))).toEqual(guideParts.map((g) => g.part))
    expect(markers().map((m) => m.textContent)).toEqual(['1', '2', '3', '4', '5', '6', '7', '8'])
    for (const marker of markers()) {
      expect(marker).not.toBeVisible()
      expect(marker).toHaveAttribute('aria-hidden', 'true')
    }

    layOut({ ...laidOut })
    resizes.resize()

    for (const m of markers()) {
      const part = m.getAttribute('data-marker') ?? ''

      expect(m, `the ${part} marker`).toBeVisible()
      expect([m.style.left, m.style.top], `the ${part} marker`).toEqual(place(part).map((at) => `${String(at)}px`))
    }

    expect(place('edge')).toEqual([1.5, 60])
    expect(place('story')).toEqual([59, 78])
    expect(place('actions')).toEqual([740, 45])
  })

  it('puts the track’s marker over the track and the action column’s over its primary, not over their columns', () => {
    followResizes()
    layOut({ ...laidOut })
    showGuide('full')

    expect(document.querySelector('[data-guide="actions"] button')).toHaveAttribute('data-verb', 'rereview')
    expect(spotOf(marker('track'))).toEqual([600, 42])
    expect(spotOf(marker('actions'))).toEqual([740, 45])

    // Where each marker would sit over a column instead: the start of its top.
    const columnSpots = Object.values(columns).map((r) => [r.left - box.left, r.top - box.top])

    for (const part of ['track', 'actions']) {
      expect(columnSpots, `the ${part} marker`).not.toContainEqual(spotOf(marker(part)))
    }
  })

  it('places each marker over its part while the dialog’s pop-in still scales the sample', () => {
    followResizes()
    layOut({ ...laidOut }, popIn)
    showGuide('full')

    for (const m of markers()) {
      const part = m.getAttribute('data-marker') ?? ''
      const [left, top] = spotOf(m)
      const [wantLeft, wantTop] = place(part)

      expect(m, `the ${part} marker`).toBeVisible()
      expect(left, `the ${part} marker's left`).toBeCloseTo(wantLeft)
      expect(top, `the ${part} marker's top`).toBeCloseTo(wantTop)
    }
  })

  it('hides a marker again once its part is no longer laid out, as a narrow list drops the track', () => {
    const parts = { ...laidOut }
    const resizes = followResizes()
    layOut(parts)
    showGuide('full')

    const track = () => document.querySelector('[data-marker="track"]')
    expect(track()).toBeVisible()

    parts.track = nothing
    resizes.resize()

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
    expect(screen.queryByRole('region', { name: 'When the author goes quiet' })).not.toBeInTheDocument()
  })

  it('shows, in full, a card whose quiet author it offers to nudge', () => {
    showGuide('full')

    const nudge = within(screen.getByRole('region', { name: 'When the author goes quiet' }))
    const card = nudge.getByRole('listitem')
    expect(card).toHaveAttribute('data-rule', 'awaiting_author')
    expect(card).toHaveTextContent('Waiting on the author · you requested changes, quiet for 2d')
    expect(nudge.getByRole('button', { name: 'Nudge', hidden: true })).toHaveAttribute('data-verb', 'nudge')
  })
})
