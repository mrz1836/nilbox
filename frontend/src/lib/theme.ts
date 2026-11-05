// Theme selection. The generated theme.css already follows the system appearance through
// prefers-color-scheme, so the first paint is correct before any script runs; this module
// pins the resolved choice on <html> so components and a future theme setting agree.

export type ThemePreference = 'system' | 'light' | 'dark'

// darkQuery matches when the system appearance is dark.
const darkQuery = '(prefers-color-scheme: dark)'

// resolveDark reports whether the dark palette applies for a preference, given whether
// the system currently prefers the light appearance.
export function resolveDark(preference: ThemePreference, systemPrefersLight: boolean): boolean {
  switch (preference) {
    case 'dark':
      return true
    case 'light':
      return false
    case 'system':
      return !systemPrefersLight
  }
}

// applyTheme sets data-theme and color-scheme on the root element, which selects the
// matching block of the generated theme tokens.
export function applyTheme(root: HTMLElement, dark: boolean): void {
  const scheme = dark ? 'dark' : 'light'

  root.dataset.theme = scheme
  root.style.colorScheme = scheme
}

// followTheme applies the preference now and again whenever the system appearance
// changes. It returns a function that stops following. Without matchMedia the system is
// treated as preferring light, matching the tokens' default.
export function followTheme(preference: ThemePreference, win: Window = window): () => void {
  const root = win.document.documentElement

  if (typeof win.matchMedia !== 'function') {
    applyTheme(root, resolveDark(preference, true))

    return () => undefined
  }

  const query = win.matchMedia(darkQuery)
  const update = () => {
    applyTheme(root, resolveDark(preference, !query.matches))
  }

  update()
  query.addEventListener('change', update)

  return () => {
    query.removeEventListener('change', update)
  }
}
