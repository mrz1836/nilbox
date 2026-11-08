import { startTransition, useLayoutEffect, useOptimistic, useRef, useState } from 'react'

import { getTheme, setTheme } from '../platform'
import { attempt, serial, type Result } from './call'
import { darkQuery, defaultTheme, resolveDark, schemeSwitch, type Theme } from './theme'
import { useMediaQuery } from './useMediaQuery'
import { useQuery } from './useQuery'

// ThemeState is the chosen theme and the palette it shows.
export interface ThemeState {
  // theme is the chosen theme; it is the default until the stored theme has loaded.
  theme: Theme
  // dark reports whether the page shows the dark palette.
  dark: boolean
  // chooseTheme switches to a theme at once, cross-fading the window, and stores it. It
  // resolves with how the store went; when the theme could not be stored, the page goes
  // back to the theme it showed, unless another theme was chosen since.
  chooseTheme: (theme: Theme) => Promise<Result<void>>
}

// useTheme shows the chosen theme for the life of the app: it loads the stored theme once,
// follows the appearance (which the System theme takes), and stores each choice. The
// first palette is pinned before the first paint without a cross-fade; every later change,
// chosen or from the system, cross-fades.
export function useTheme(win: Window = window): ThemeState {
  const appearanceDark = useMediaQuery(darkQuery, win)
  const stored = useQuery(() => getTheme(), [], { log: 'could not read the theme' })
  // chosen is the theme last stored from a choice, which a stored theme that loads late
  // never overrides.
  const [chosen, setChosen] = useState<Theme | null>(null)
  const [theme, showTheme] = useOptimistic<Theme, Theme>(chosen ?? stored.data ?? defaultTheme, (_, next) => next)
  const [switchScheme] = useState(() => schemeSwitch(win))
  // queue stores the choices one at a time, so they reach the database, and the native
  // appearance, in the order they were made.
  const [queue] = useState(serial)
  // painted is whether the first palette is pinned; only later switches cross-fade.
  const painted = useRef(false)

  const dark = resolveDark(theme, appearanceDark)

  useLayoutEffect(() => {
    switchScheme(dark, painted.current)
    painted.current = true
  }, [switchScheme, dark])

  const chooseTheme = (next: Theme) =>
    new Promise<Result<void>>((resolve) => {
      startTransition(async () => {
        showTheme(next)

        const result = await attempt(queue(() => setTheme(next)))

        if (result.ok) {
          startTransition(() => {
            setChosen(next)
          })
        }

        resolve(result.ok ? { ok: true, data: undefined } : result)
      })
    })

  return { theme, dark, chooseTheme }
}
