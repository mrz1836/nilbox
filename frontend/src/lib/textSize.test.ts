import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  applyTextSize,
  defaultTextSize,
  isTextSize,
  textSizeFromURL,
  textSizeParam,
  textSizePercent,
  textSizes,
  useTextSize,
  type TextSize,
} from './textSize'

// fakeWindow is a window over a document of its own, at href, whose history records each
// replaced URL.
function fakeWindow(href = 'wails://localhost/') {
  const doc = document.implementation.createHTMLDocument('')
  const location = { href }
  const replaceState = vi.fn((_state: unknown, _unused: string, url: URL) => {
    location.href = url.href
  })
  const win = { document: doc, location, history: { state: { kept: true }, replaceState } } as unknown as Window

  return { win, root: doc.documentElement, location, replaceState }
}

describe('textSizes', () => {
  it('offers 100%, 115%, and 130%, smallest first, with 100% the default', () => {
    expect(textSizes).toEqual([
      { value: 'default', percent: 100 },
      { value: 'large', percent: 115 },
      { value: 'larger', percent: 130 },
    ])
    expect(defaultTextSize).toBe('default')
    expect(textSizePercent(defaultTextSize)).toBe(100)
  })

  it('reads the parameter the desktop shell sets', () => {
    expect(textSizeParam).toBe('textSize')
  })

  it('knows each choice and nothing else', () => {
    for (const { value } of textSizes) {
      expect(isTextSize(value)).toBe(true)
    }

    for (const value of ['', 'Large', 'largest', '115', null, undefined, 115]) {
      expect(isTextSize(value)).toBe(false)
    }
  })

  it('treats a size it does not know as 100%', () => {
    expect(textSizePercent('huge' as TextSize)).toBe(100)
  })
})

describe('textSizeFromURL', () => {
  it('reads the size the URL carries', () => {
    expect(textSizeFromURL('?textSize=large')).toBe('large')
    expect(textSizeFromURL('?other=1&textSize=larger')).toBe('larger')
    expect(textSizeFromURL('textSize=default')).toBe('default')
  })

  it('is null for a URL without a size this build knows', () => {
    expect(textSizeFromURL('')).toBeNull()
    expect(textSizeFromURL('?textSize=')).toBeNull()
    expect(textSizeFromURL('?textSize=largest')).toBeNull()
    expect(textSizeFromURL('?textsize=large')).toBeNull()
  })
})

describe('applyTextSize', () => {
  it('scales the root font size and writes the size into the URL', () => {
    const { win, root, location, replaceState } = fakeWindow()

    applyTextSize('large', win)

    expect(root.style.fontSize).toBe('115%')
    expect(location.href).toBe('wails://localhost/?textSize=large')
    expect(replaceState).toHaveBeenCalledWith({ kept: true }, '', expect.any(URL))

    applyTextSize('larger', win)

    expect(root.style.fontSize).toBe('130%')
    expect(location.href).toBe('wails://localhost/?textSize=larger')
  })

  it('leaves the root at the browser size for the default', () => {
    const { win, root, location } = fakeWindow('wails://localhost/?textSize=larger')
    root.style.fontSize = '130%'

    applyTextSize('default', win)

    expect(root.style.fontSize).toBe('')
    expect(root.getAttribute('style') ?? '').not.toContain('font-size')
    expect(location.href).toBe('wails://localhost/?textSize=default')
  })

  it('keeps the rest of the URL', () => {
    const { win, location } = fakeWindow('wails://localhost/?debug=1#top')

    applyTextSize('large', win)

    expect(location.href).toBe('wails://localhost/?debug=1&textSize=large#top')
  })

  it('leaves a URL that already carries the size alone', () => {
    const { win, root, replaceState } = fakeWindow('wails://localhost/?textSize=large')

    applyTextSize('large', win)

    expect(root.style.fontSize).toBe('115%')
    expect(replaceState).not.toHaveBeenCalled()
  })
})

describe('useTextSize', () => {
  it('shows each size the settings hold, and nothing before they load', () => {
    const { win, root, location } = fakeWindow('wails://localhost/?textSize=large')
    root.style.fontSize = '115%'
    const initialProps: { size: TextSize | undefined } = { size: undefined }

    const { rerender } = renderHook(
      ({ size }) => {
        useTextSize(size, win)
      },
      { initialProps },
    )

    expect(root.style.fontSize).toBe('115%')
    expect(location.href).toBe('wails://localhost/?textSize=large')

    rerender({ size: 'larger' })

    expect(root.style.fontSize).toBe('130%')
    expect(location.href).toBe('wails://localhost/?textSize=larger')

    rerender({ size: 'default' })

    expect(root.style.fontSize).toBe('')
    expect(location.href).toBe('wails://localhost/?textSize=default')
  })
})
