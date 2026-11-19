import { useState } from 'react'

// How many of a card's labels its title line shows. A label shows whole or folds into the
// "+N" after the labels shown: none is ever cut off. The labels take the room the title
// leaves, or a third of the line when the title is long, so a long title is cut short to
// make room for them; the first label always shows.

// LabelRoom is what a card's title line measures for its labels, in CSS pixels: the width
// its title and labels share (room), the title's whole width however much of it shows
// (title), the width of each label chip that may show (chips), of the "+N" (count), and
// the gap between them. None of it changes with how many labels show, so what it decides
// never changes what it measures: the labels settle in one pass, and never flip back and
// forth.
export interface LabelRoom {
  room: number
  title: number
  chips: readonly number[]
  count: number
  gap: number
}

// labelShare is the part of a title line its labels may take from a title too long for
// the line.
const labelShare = 1 / 3

// slack is the pixel the labels leave spare beside a title that shows whole, so a rounding
// in what the line measures never cuts the title's last letter for an ellipsis.
const slack = 1

// labelsShown is how many of a card's first labels its title line shows, of total labels
// with up to max of them shown: the most whose chips, and the "+N" for the rest, fit in
// the room the title leaves, or in a third of the line when the title is too long for it,
// and always the first. Until the line is measured, or while it was measured for other
// labels, it is up to max.
export function labelsShown(room: LabelRoom | null, total: number, max: number): number {
  const candidates = Math.min(total, max)

  if (room === null || room.chips.length !== candidates) {
    return candidates
  }

  const fits = Math.max(room.room - room.title - slack, room.room * labelShare)

  for (let shown = candidates; shown > 1; shown--) {
    if (needed(room, shown, total) <= fits) {
      return shown
    }
  }

  return Math.min(candidates, 1)
}

// needed is the width of the first shown labels, with the "+N" after them when some are
// left over.
function needed({ chips, count, gap }: LabelRoom, shown: number, total: number): number {
  const width = chips.slice(0, shown).reduce((sum, chip) => sum + chip, gap * (shown - 1))

  return total > shown ? width + gap + count : width
}

// measureLabelRoom measures a card's labels on its title line: strip holds them, last on
// the line (its parent), which also holds the title, marked data-card-title. Each chip
// that may show is marked data-label-chip and the "+N" data-label-count, laid out at its
// whole width whether it shows or not. It is null while the strip is not laid out, as in a
// list too narrow for labels, where they do not show.
export function measureLabelRoom(strip: HTMLElement): LabelRoom | null {
  const line = strip.parentElement
  const title = line?.querySelector<HTMLElement>('[data-card-title]') ?? null
  const box = strip.getBoundingClientRect()

  if (line === null || title === null || box.width === 0) {
    return null
  }

  const titleBox = title.getBoundingClientRect()
  const width = (el: Element | null) => (el === null ? 0 : el.getBoundingClientRect().width)

  return {
    room: titleBox.width + line.getBoundingClientRect().right - box.left,
    title: Math.max(titleBox.width, title.scrollWidth),
    chips: Array.from(strip.querySelectorAll('[data-label-chip]'), width),
    count: width(strip.querySelector('[data-label-count]')),
    gap: parseFloat(getComputedStyle(strip).columnGap) || 0,
  }
}

// sameRoom says whether two measures are the same, so measuring again without a change
// renders nothing again.
function sameRoom(a: LabelRoom | null, b: LabelRoom): boolean {
  return (
    a !== null &&
    a.room === b.room &&
    a.title === b.title &&
    a.count === b.count &&
    a.gap === b.gap &&
    a.chips.length === b.chips.length &&
    a.chips.every((chip, i) => chip === b.chips[i])
  )
}

// useLabelRoom measures a card's labels on its title line (measureLabelRoom), for
// labelsShown: as the strip that holds them is laid out, before the card first shows, and
// again when the line, the title, or the strip changes size, or the line changes, as when
// the title, the labels, or the marks before the title do.
export function useLabelRoom(): [(strip: HTMLElement | null) => (() => void) | undefined, LabelRoom | null] {
  const [room, setRoom] = useState<LabelRoom | null>(null)

  const ref = (strip: HTMLElement | null) => {
    const line = strip?.parentElement ?? null

    if (strip === null || line === null) {
      return undefined
    }

    const measure = () => {
      const next = measureLabelRoom(strip)

      if (next !== null) {
        setRoom((was) => (sameRoom(was, next) ? was : next))
      }
    }

    const sizes = new ResizeObserver(measure)
    const changes = new MutationObserver(measure)

    for (const el of [line, strip, ...line.querySelectorAll('[data-card-title]')]) {
      sizes.observe(el)
    }

    changes.observe(line, { childList: true, subtree: true, characterData: true })
    measure()

    return () => {
      sizes.disconnect()
      changes.disconnect()
    }
  }

  return [ref, room]
}
