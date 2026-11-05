import { afterEach, describe, expect, it, vi } from 'vitest'

import { applyTheme, followTheme, resolveDark } from './theme'

// fakeWindow is a window whose system appearance the test controls.
function fakeWindow(prefersDark: boolean) {
  const listeners = new Set<() => void>()
  const query = {
    matches: prefersDark,
    addEventListener: vi.fn((_type: string, listener: () => void) => listeners.add(listener)),
    removeEventListener: vi.fn((_type: string, listener: () => void) => listeners.delete(listener)),
  }
  const matchMedia = vi.fn(() => query)
  const win = { document, matchMedia } as unknown as Window

  return {
    win,
    matchMedia,
    query,
    listeners,
    setSystemDark(dark: boolean) {
      query.matches = dark
      for (const listener of listeners) {
        listener()
      }
    },
  }
}

afterEach(() => {
  delete document.documentElement.dataset.theme
  document.documentElement.style.colorScheme = ''
})

describe('resolveDark', () => {
  it('resolveDark follows the system when preference is system', () => {
    expect(resolveDark('system', true)).toBe(false)
    expect(resolveDark('system', false)).toBe(true)
  })

  it('an explicit preference wins over the system', () => {
    expect(resolveDark('dark', true)).toBe(true)
    expect(resolveDark('dark', false)).toBe(true)
    expect(resolveDark('light', true)).toBe(false)
    expect(resolveDark('light', false)).toBe(false)
  })
})

describe('applyTheme', () => {
  it('sets data-theme and color-scheme on the root', () => {
    const root = document.documentElement

    applyTheme(root, true)
    expect(root.dataset.theme).toBe('dark')
    expect(root.style.colorScheme).toBe('dark')

    applyTheme(root, false)
    expect(root.dataset.theme).toBe('light')
    expect(root.style.colorScheme).toBe('light')
  })
})

describe('followTheme', () => {
  it('applies the system appearance and follows live changes until stopped', () => {
    const system = fakeWindow(true)
    const root = document.documentElement

    const stop = followTheme('system', system.win)
    expect(system.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)')
    expect(root.dataset.theme).toBe('dark')

    system.setSystemDark(false)
    expect(root.dataset.theme).toBe('light')

    stop()
    expect(system.listeners.size).toBe(0)

    system.setSystemDark(true)
    expect(root.dataset.theme).toBe('light')
  })

  it('keeps an explicit preference when the system changes', () => {
    const system = fakeWindow(false)

    followTheme('dark', system.win)
    expect(document.documentElement.dataset.theme).toBe('dark')

    system.setSystemDark(false)
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('falls back to light without matchMedia', () => {
    const win = { document } as unknown as Window

    const stop = followTheme('system', win)
    expect(document.documentElement.dataset.theme).toBe('light')
    stop()
  })
})
