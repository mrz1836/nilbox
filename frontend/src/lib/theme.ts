// The theme: the user's choice of palette, and switching the page to it.
//
// The generated theme.css sets every brand token for the light palette, and again for the
// dark one under prefers-color-scheme: dark, so the first paint follows the web view's
// appearance before any script runs. On macOS the desktop shell pins that appearance to a
// Light or Dark theme before the window opens, and leaves it to the system for System, so
// the first frame already shows the chosen palette. From then on the page pins the palette
// it shows on <html> (data-theme wins over the media query) and cross-fades on a switch.

import type { Theme } from '../platform'
import { prefersReducedMotion } from './motion'

export type { Theme }

// themes are the choices, in the order Settings offers them.
export const themes: readonly Theme[] = ['system', 'light', 'dark']

// defaultTheme is the theme of a fresh install, and the one shown until the stored theme
// has loaded.
export const defaultTheme: Theme = 'system'

// darkQuery matches while the web view's appearance is dark: the system's, or the one the
// desktop shell pinned for a Light or Dark theme.
const darkQuery = '(prefers-color-scheme: dark)'

// resolveDark reports whether a theme shows the dark palette while the appearance is dark
// or light.
export function resolveDark(theme: Theme, appearanceDark: boolean): boolean {
  switch (theme) {
    case 'dark':
      return true
    case 'light':
      return false
    case 'system':
      return appearanceDark
  }
}

// appearanceIsDark reports whether the web view's appearance is dark. Without matchMedia
// it reports false, matching the tokens' light default.
export function appearanceIsDark(win: Window = window): boolean {
  return typeof win.matchMedia === 'function' && win.matchMedia(darkQuery).matches
}

// onAppearanceChange calls listener with whether the appearance is dark each time it
// changes, and returns a function that stops listening. Without matchMedia it never
// changes.
export function onAppearanceChange(listener: (dark: boolean) => void, win: Window = window): () => void {
  if (typeof win.matchMedia !== 'function') {
    return () => undefined
  }

  const query = win.matchMedia(darkQuery)
  const changed = () => {
    listener(query.matches)
  }

  query.addEventListener('change', changed)

  return () => {
    query.removeEventListener('change', changed)
  }
}

type Scheme = 'light' | 'dark'

// pin shows a palette: data-theme selects the matching block of the theme tokens, and
// color-scheme draws native controls and scrollbars to match.
function pin(root: HTMLElement, scheme: Scheme): void {
  root.dataset.theme = scheme
  root.style.colorScheme = scheme
}

// SchemeSwitch moves the page to the dark (true) or light (false) palette. With animate,
// the whole window cross-fades.
export type SchemeSwitch = (dark: boolean, animate: boolean) => void

// schemeSwitch returns the switch for a window's page. A switch to the palette already
// shown, or already on its way, does nothing. An animated switch runs as a view transition
// (styled in index.css); without view transitions, or when the system asks for less
// motion, it switches at once. Each switch shows the latest palette asked for, so a quick
// second switch can never be undone by the first one finishing late.
export function schemeSwitch(win: Window = window): SchemeSwitch {
  const doc = win.document
  const root = doc.documentElement
  // target is the palette shown, or on its way while a cross-fade is under way.
  let target = root.dataset.theme

  const show = () => {
    if (target === 'light' || target === 'dark') {
      pin(root, target)
    }
  }

  return (dark, animate) => {
    const scheme: Scheme = dark ? 'dark' : 'light'
    if (scheme === target) {
      return
    }

    target = scheme

    if (!animate || !('startViewTransition' in doc) || prefersReducedMotion(win)) {
      show()

      return
    }

    // A newer switch skips this one's cross-fade, which rejects ready; its update still
    // runs and shows the latest palette, so there is nothing to recover.
    doc.startViewTransition(show).ready.catch(() => undefined)
  }
}
