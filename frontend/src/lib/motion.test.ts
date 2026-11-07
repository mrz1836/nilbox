import { describe, expect, it } from 'vitest'

import { prefersReducedMotion } from './motion'

describe('prefersReducedMotion', () => {
  const windowWith = (matches: boolean) =>
    ({
      matchMedia: (query: string) => ({ matches: matches && query === '(prefers-reduced-motion: reduce)' }),
    }) as unknown as Window

  it('follows the system setting', () => {
    expect(prefersReducedMotion(windowWith(true))).toBe(true)
    expect(prefersReducedMotion(windowWith(false))).toBe(false)
  })

  it('reports false without matchMedia', () => {
    expect(prefersReducedMotion({} as Window)).toBe(false)
  })
})
