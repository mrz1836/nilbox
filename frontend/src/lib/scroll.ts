// Keeping what the user is looking at in place while the layout above it changes.

// scrollerOf is the nearest ancestor of el that scrolls vertically, or the document's own
// scroller when none does.
export function scrollerOf(el: Element): Element | null {
  for (let p = el.parentElement; p !== null; p = p.parentElement) {
    const { overflowY } = getComputedStyle(p)

    if (overflowY === 'auto' || overflowY === 'scroll') {
      return p
    }
  }

  return document.scrollingElement
}

// holdPlace scrolls el's scroller so el stands where it stood before the layout above it
// changed, top pixels from the top of the window, rather than moving out from under the
// pointer. A scroller stops at its ends, so el can still move as far as they leave it to.
export function holdPlace(el: Element, top: number): void {
  const moved = el.getBoundingClientRect().top - top
  const scroller = scrollerOf(el)

  if (moved !== 0 && scroller !== null) {
    scroller.scrollTop += moved
  }
}
