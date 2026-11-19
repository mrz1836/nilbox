import { act, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { labelsShown, measureLabelRoom, useLabelRoom, type LabelRoom } from './labelFit'

// room is a title line's measure with a 600px line, a title that fits it, two chips, and
// a 14px "+N".
function room(over: Partial<LabelRoom> = {}): LabelRoom {
  return { room: 600, title: 300, chips: [80, 90], count: 14, gap: 4, ...over }
}

describe('labelsShown', () => {
  it('shows up to the most until the line is measured', () => {
    expect(labelsShown(null, 5, 2)).toBe(2)
    expect(labelsShown(null, 1, 2)).toBe(1)
    expect(labelsShown(null, 0, 2)).toBe(0)
  })

  it('shows them all beside a title that leaves room for them', () => {
    expect(labelsShown(room(), 2, 2)).toBe(2)
    // 200 + 4 + 195 beside a 500px title in a 900px line, with a pixel spare.
    expect(labelsShown(room({ room: 900, title: 500, chips: [200, 195] }), 2, 2)).toBe(2)
  })

  it('folds a label into the +N when the title leaves no room for it', () => {
    expect(labelsShown(room({ room: 900, title: 500, chips: [200, 196] }), 2, 2), 'no pixel spare').toBe(1)
    expect(labelsShown(room({ chips: [80, 300] }), 2, 2)).toBe(1)
  })

  it('takes a third of the line from a long title', () => {
    // 80 + 4 + 90, and 4 + 14 for the "+1".
    expect(labelsShown(room({ title: 2000 }), 3, 2), '192 of 200').toBe(2)
    expect(labelsShown(room({ title: 2000, chips: [80, 120] }), 3, 2), '222 of 200').toBe(1)
    expect(labelsShown(room({ title: 2000, chips: [80, 120] }), 2, 2), '204 of 200').toBe(1)
  })

  it('always shows the first, however long', () => {
    expect(labelsShown(room({ title: 2000, chips: [250, 90] }), 2, 2)).toBe(1)
    expect(labelsShown(room({ title: 2000, chips: [250] }), 1, 2)).toBe(1)
  })

  it('shows up to the most while the line was measured for other labels', () => {
    expect(labelsShown(room({ title: 2000, chips: [250] }), 2, 2)).toBe(2)
  })
})

// box lays an element out where jsdom, which lays nothing out, would leave it empty.
function box(el: Element, left: number, width: number) {
  Object.defineProperty(el, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({ left, right: left + width, width, top: 0, bottom: 20, height: 20, x: left, y: 0 }),
  })
}

// line builds a card's title line: a title, then the labels' strip with two chips and a
// "+N", laid out as a long title cut short beside them.
function line() {
  const el = document.createElement('div')
  const title = document.createElement('button')
  const strip = document.createElement('span')

  title.dataset.cardTitle = ''
  Object.defineProperty(title, 'scrollWidth', { configurable: true, value: 900 })
  strip.style.columnGap = '4px'
  el.append(title, strip)

  const chips = [80, 90].map((width, i) => {
    const chip = document.createElement('span')

    chip.dataset.labelChip = ''
    box(chip, 400 + i * 84, width)

    return chip
  })
  const count = document.createElement('span')

  count.dataset.labelCount = ''
  box(count, 578, 14)
  strip.append(...chips, count)
  box(el, 0, 600)
  box(title, 20, 376)
  box(strip, 400, 192)

  return { el, title, strip }
}

describe('measureLabelRoom', () => {
  it('measures the room the title and the labels share, and every chip and the +N whole', () => {
    expect(measureLabelRoom(line().strip)).toEqual({ room: 576, title: 900, chips: [80, 90], count: 14, gap: 4 })
  })

  it('takes the title as it shows when it shows whole', () => {
    const { title, strip } = line()

    Object.defineProperty(title, 'scrollWidth', { configurable: true, value: 376 })
    box(title, 20, 376.4)

    expect(measureLabelRoom(strip)?.title).toBe(376.4)
  })

  it('measures nothing while the labels are not laid out, or there is no title', () => {
    const { title, strip } = line()

    box(strip, 0, 0)
    expect(measureLabelRoom(strip)).toBeNull()

    box(strip, 400, 192)
    title.remove()
    expect(measureLabelRoom(strip)).toBeNull()
    expect(measureLabelRoom(document.createElement('span'))).toBeNull()
  })
})

describe('useLabelRoom', () => {
  // resized are the callbacks of the ResizeObservers made, which a test calls as a layout
  // change would; observed is what each one follows.
  let resized: (() => void)[] = []
  let observed: Element[] = []

  beforeEach(() => {
    resized = []
    observed = []
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(callback: () => void) {
          resized.push(callback)
        }

        observe(el: Element) {
          observed.push(el)
        }

        disconnect() {
          observed = []
        }
      },
    )
  })

  // Line is a title line whose labels' strip follows its room, and says what it measured.
  function Line({ text }: { text: string }) {
    const [fit, measured] = useLabelRoom()

    return (
      <div data-testid="line">
        <button type="button" data-card-title>
          {text}
        </button>
        <span ref={fit} data-testid="strip">
          bug
        </span>
        <output>{measured === null ? 'unmeasured' : `${String(measured.room)} ${String(measured.title)}`}</output>
      </div>
    )
  }

  // layOut lays the line out with a title of the width given.
  function layOut(title: number) {
    const titleEl = screen.getByRole('button')

    box(screen.getByTestId('line'), 0, 600)
    box(titleEl, 0, title)
    Object.defineProperty(titleEl, 'scrollWidth', { configurable: true, value: title })
    box(screen.getByTestId('strip'), title + 8, 60)
  }

  it('measures as the line changes size, and follows the line, the title, and the strip', () => {
    const { unmount } = render(<Line text="A title" />)

    expect(screen.getByRole('status')).toHaveTextContent('unmeasured')
    expect(observed).toEqual([screen.getByTestId('line'), screen.getByTestId('strip'), screen.getByRole('button')])

    layOut(200)
    act(() => {
      resized.forEach((callback) => {
        callback()
      })
    })

    expect(screen.getByRole('status')).toHaveTextContent('592 200')

    unmount()
    expect(observed).toEqual([])
  })

  it('measures again when the line changes, as when its title does', async () => {
    const { rerender } = render(<Line text="A title" />)

    // The line as it lays out with the longer title; no observer reports a size.
    layOut(250)
    rerender(<Line text="A longer title" />)
    await act(async () => {
      await Promise.resolve()
    })

    expect(screen.getByRole('status')).toHaveTextContent('592 250')
  })
})
