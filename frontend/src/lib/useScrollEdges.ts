import { useState } from 'react'

// slack is how far from an end, in CSS pixels, still counts as at it, so a scroller left a
// fraction of a pixel short of its end by the text size's rounding shows no fade there.
const slack = 1

// Edges says which ends of a scroller have more beyond them.
export interface Edges {
  above: boolean
  below: boolean
}

// ScrollerRef goes on the scroller, whose one child holds what it scrolls.
export type ScrollerRef = (el: HTMLElement | null) => (() => void) | undefined

const noEdges: Edges = { above: false, below: false }

// useScrollEdges follows which ends of a scroller have more beyond them, for ScrollFades
// to fade out: macOS shows no scroll bar until a scroll, so a list cut off by the window
// would give no sign that it goes on. It measures again as the scroller scrolls and as it
// or its content changes size, as when the window, the text size, or what the list holds
// changes, and renders again only when an end changes.
export function useScrollEdges(): [ScrollerRef, Edges] {
  const [edges, setEdges] = useState(noEdges)

  const ref = (el: HTMLElement | null) => {
    if (el === null) {
      return undefined
    }

    const measure = () => {
      const above = el.scrollTop > slack
      const below = el.scrollHeight - el.clientHeight - el.scrollTop > slack

      setEdges((was) => (was.above === above && was.below === below ? was : { above, below }))
    }

    const sizes = new ResizeObserver(measure)

    sizes.observe(el)

    if (el.firstElementChild !== null) {
      sizes.observe(el.firstElementChild)
    }

    el.addEventListener('scroll', measure, { passive: true })
    measure()

    return () => {
      el.removeEventListener('scroll', measure)
      sizes.disconnect()
    }
  }

  return [ref, edges]
}
