// The text size: how large the app's text is, apart from the size of its window.
//
// Text, and the spacing sized with it, is set in rem, so the root's font size scales them
// together, as a browser's own font size setting does. Chrome lined up with the native
// window, such as the toolbar beside the macOS window controls, and hairlines like borders
// stay in px.
//
// The desktop shell opens the page with the stored size in its URL (?textSize=large, from
// internal/desktop's PageURL), and main.tsx applies it before the first render, so the
// first frame already shows text at that size. From then on the page shows the size the
// settings hold, and writes each one back into its URL, so a reload keeps it.

import { useLayoutEffect } from 'react'

import type { TextSize } from '../platform'

export type { TextSize }

// TextSizeChoice is one of the sizes Settings offers: its value, and the scale it sets as
// a percent of the size the app is designed at.
export interface TextSizeChoice {
  value: TextSize
  percent: number
}

// textSizes are the choices, smallest first, in the order Settings offers them. The shell
// grows the window's smallest size by the same scales (settings.TextSize.Percent), so the
// layout is never narrower than at 100%; a Go test holds the two in step.
export const textSizes: readonly TextSizeChoice[] = [
  { value: 'default', percent: 100 },
  { value: 'large', percent: 115 },
  { value: 'larger', percent: 130 },
]

// defaultTextSize is the size of a fresh install, and the one shown until the stored size
// is known.
export const defaultTextSize: TextSize = 'default'

// textSizeParam is the query parameter of the page's URL that carries the text size
// (internal/desktop's TextSizeParam).
export const textSizeParam = 'textSize'

// isTextSize reports whether a value is one of the choices.
export function isTextSize(value: unknown): value is TextSize {
  return textSizes.some((choice) => choice.value === value)
}

// textSizeFromURL returns the text size a page URL's query string carries, or null when
// it carries none, or one this build does not know.
export function textSizeFromURL(search: string): TextSize | null {
  const value = new URLSearchParams(search).get(textSizeParam)

  return isTextSize(value) ? value : null
}

// textSizePercent is the scale a text size sets, as a percent of the size the app is
// designed at.
export function textSizePercent(size: TextSize): number {
  return textSizes.find((choice) => choice.value === size)?.percent ?? 100
}

// applyTextSize shows a text size: it scales the root's font size, which every rem
// follows, and writes the size into the page's URL, so a reload opens at it too. The
// default leaves the root at the browser's own font size.
export function applyTextSize(size: TextSize, win: Window = window): void {
  const root = win.document.documentElement
  const percent = textSizePercent(size)

  if (percent === 100) {
    root.style.removeProperty('font-size')
  } else {
    root.style.fontSize = `${String(percent)}%`
  }

  const url = new URL(win.location.href)
  if (url.searchParams.get(textSizeParam) !== size) {
    url.searchParams.set(textSizeParam, size)
    win.history.replaceState(win.history.state, '', url)
  }
}

// useTextSize shows the text size the settings hold, once they have loaded and after each
// change is stored, before the browser paints. Until then the page shows the size its
// URL carried.
export function useTextSize(size: TextSize | undefined, win: Window = window): void {
  useLayoutEffect(() => {
    if (size !== undefined) {
      applyTextSize(size, win)
    }
  }, [size, win])
}
