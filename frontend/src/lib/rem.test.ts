import { describe, expect, it, vi } from 'vitest'

import { remToPx } from './rem'

// docWithRootFontSize is a document whose root computes to the given font size.
function docWithRootFontSize(fontSize: string): Document {
  const documentElement = document.createElement('html')
  const getComputedStyle = vi.fn(() => ({ fontSize }))

  return { documentElement, defaultView: { getComputedStyle } } as unknown as Document
}

describe('remToPx', () => {
  it('converts at the root font size now', () => {
    expect(remToPx(18, docWithRootFontSize('16px'))).toBe(288)
    expect(remToPx(18, docWithRootFontSize('18.4px'))).toBeCloseTo(331.2)
    expect(remToPx(18, docWithRootFontSize('20.8px'))).toBeCloseTo(374.4)
  })

  it('falls back to 16px when the root size cannot be read', () => {
    expect(remToPx(2, docWithRootFontSize(''))).toBe(32)
    expect(remToPx(2, docWithRootFontSize('0px'))).toBe(32)
    expect(remToPx(2, document.implementation.createHTMLDocument(''))).toBe(32)
  })

  it('reads the page document by default', () => {
    expect(remToPx(1)).toBeGreaterThan(0)
  })
})
