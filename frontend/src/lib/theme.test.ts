import { describe, expect, it, vi } from 'vitest'

import { defaultTheme, resolveDark, schemeSwitch, themes, type Theme } from './theme'

// fakeAppearance is a window whose reduced-motion setting the test controls, over a
// document of its own.
function fakeAppearance({ reducedMotion = false } = {}) {
  const matchMedia = vi.fn(() => ({ matches: reducedMotion }))
  const doc = document.implementation.createHTMLDocument('')
  const win = { document: doc, matchMedia } as unknown as Window

  return { win, doc, root: doc.documentElement }
}

// withTransitions gives a document view transitions whose update callbacks the test runs,
// returning the callbacks in the order the transitions started.
function withTransitions(doc: Document, ready: () => Promise<void> = () => Promise.resolve()) {
  const updates: (() => void)[] = []
  const startViewTransition = vi.fn((update: () => void) => {
    updates.push(update)

    return { ready: ready() }
  })

  Object.assign(doc, { startViewTransition })

  return { startViewTransition, updates }
}

describe('themes', () => {
  it('offers the choices in order, System first and by default', () => {
    expect(defaultTheme).toBe('system')
    expect(themes).toEqual(['system', 'light', 'dark'])
  })
})

describe('resolveDark', () => {
  it('follows the appearance for System', () => {
    expect(resolveDark('system', true)).toBe(true)
    expect(resolveDark('system', false)).toBe(false)
  })

  it('pins Light and Dark whatever the appearance', () => {
    const cases: [Theme, boolean, boolean][] = [
      ['dark', true, true],
      ['dark', false, true],
      ['light', true, false],
      ['light', false, false],
    ]

    for (const [theme, appearanceDark, want] of cases) {
      expect(resolveDark(theme, appearanceDark)).toBe(want)
    }
  })
})

describe('schemeSwitch', () => {
  it('pins the palette on the root, at once when not animated', () => {
    const appearance = fakeAppearance()
    const { startViewTransition } = withTransitions(appearance.doc)
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(true, false)
    expect(appearance.root.dataset.theme).toBe('dark')
    expect(appearance.root.style.colorScheme).toBe('dark')

    switchScheme(false, false)
    expect(appearance.root.dataset.theme).toBe('light')
    expect(appearance.root.style.colorScheme).toBe('light')

    expect(startViewTransition).not.toHaveBeenCalled()
  })

  it('cross-fades an animated switch as a view transition', () => {
    const appearance = fakeAppearance()
    const { startViewTransition, updates } = withTransitions(appearance.doc)
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(false, false)
    switchScheme(true, true)

    expect(startViewTransition).toHaveBeenCalledOnce()
    expect(appearance.root.dataset.theme).toBe('light')

    updates[0]?.()
    expect(appearance.root.dataset.theme).toBe('dark')
    expect(appearance.root.style.colorScheme).toBe('dark')
  })

  it('does nothing for the palette already shown or on its way', () => {
    const appearance = fakeAppearance()
    const { startViewTransition } = withTransitions(appearance.doc)
    appearance.root.dataset.theme = 'dark'
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(true, true)
    expect(startViewTransition).not.toHaveBeenCalled()

    switchScheme(false, true)
    switchScheme(false, true)
    expect(startViewTransition).toHaveBeenCalledOnce()
  })

  it('shows the latest palette asked for when a quick second switch finishes first', () => {
    const appearance = fakeAppearance()
    const { updates } = withTransitions(appearance.doc)
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(false, false)
    switchScheme(true, true)
    switchScheme(false, true)

    // The second transition's update runs first, then the first one's, late.
    updates[1]?.()
    updates[0]?.()

    expect(appearance.root.dataset.theme).toBe('light')
  })

  it('swallows a cross-fade that a newer switch skipped', async () => {
    const appearance = fakeAppearance()
    const skipped = () => Promise.reject(new DOMException('Skipped by a newer transition', 'AbortError'))
    const { updates } = withTransitions(appearance.doc, skipped)
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(true, true)
    switchScheme(false, true)
    updates[0]?.()
    updates[1]?.()

    // An unhandled rejection would fail the run; let it surface if there is one.
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(appearance.root.dataset.theme).toBe('light')
  })

  it('switches at once when the system asks for less motion', () => {
    const appearance = fakeAppearance({ reducedMotion: true })
    const { startViewTransition } = withTransitions(appearance.doc)
    const switchScheme = schemeSwitch(appearance.win)

    switchScheme(true, true)

    expect(startViewTransition).not.toHaveBeenCalled()
    expect(appearance.root.dataset.theme).toBe('dark')
  })

  it('switches at once without view transitions', () => {
    const appearance = fakeAppearance()
    const switchScheme = schemeSwitch(appearance.win)

    expect('startViewTransition' in appearance.doc).toBe(false)

    switchScheme(true, true)
    expect(appearance.root.dataset.theme).toBe('dark')
  })
})
