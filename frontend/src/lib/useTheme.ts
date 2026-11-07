import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { getTheme, setTheme } from '../platform'
import {
  appearanceIsDark,
  defaultTheme,
  onAppearanceChange,
  resolveDark,
  schemeSwitch,
  toTheme,
  type Theme,
} from './theme'

// ThemeState is the chosen theme and the palette it shows.
export interface ThemeState {
  // theme is the chosen theme; it is the default until the stored theme has loaded.
  theme: Theme
  // dark reports whether the page shows the dark palette.
  dark: boolean
  // chooseTheme switches to a theme at once, cross-fading the window, and stores it. It
  // rejects when the theme could not be stored, and the page goes back to the theme it
  // showed, unless another theme was chosen since.
  chooseTheme: (theme: Theme) => Promise<void>
}

// useTheme shows the chosen theme for the life of the app: it loads the stored theme once,
// follows the appearance (which the System theme takes), and stores each choice. The
// first palette is pinned before the first paint without a cross-fade; every later change,
// chosen or from the system, cross-fades.
export function useTheme(win: Window = window): ThemeState {
  const [theme, setThemeState] = useState<Theme>(defaultTheme)
  const [appearanceDark, setAppearanceDark] = useState(() => appearanceIsDark(win))
  const [switchScheme] = useState(() => schemeSwitch(win))
  // picks counts the choices made, so a stored theme that loads late never overrides one,
  // and a failed store only puts back the theme it replaced while it is still the latest.
  const picks = useRef(0)
  // saving chains the stores, so they reach the database, and the native appearance, in
  // the order they were made.
  const saving = useRef<Promise<void>>(Promise.resolve())
  // painted is whether the first palette is pinned; only later switches cross-fade.
  const painted = useRef(false)

  useEffect(() => onAppearanceChange(setAppearanceDark, win), [win])

  useEffect(() => {
    let active = true

    getTheme()
      .then((stored) => {
        if (active && picks.current === 0) {
          setThemeState(toTheme(stored))
        }
      })
      .catch((err: unknown) => {
        console.error('could not read the theme', err)
      })

    return () => {
      active = false
    }
  }, [])

  const dark = resolveDark(theme, appearanceDark)

  useLayoutEffect(() => {
    switchScheme(dark, painted.current)
    painted.current = true
  }, [switchScheme, dark])

  const chooseTheme = useCallback(
    async (next: Theme) => {
      const pick = ++picks.current
      const previous = theme

      setThemeState(next)

      const stored = saving.current.catch(() => undefined).then(() => setTheme(next))
      saving.current = stored

      try {
        await stored
      } catch (err: unknown) {
        if (picks.current === pick) {
          setThemeState(previous)
        }

        throw err
      }
    },
    [theme],
  )

  return { theme, dark, chooseTheme }
}
