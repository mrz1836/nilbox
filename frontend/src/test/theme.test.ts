import { describe, expect, it } from 'vitest'

import { palettes } from '../generated/brand'
import { themeColors } from './theme'

describe('themeColors', () => {
  it('reads every color token theme.css defines, in its order, once each', () => {
    const names = themeColors()

    expect(names[0]).toBe('--color-background')
    expect(names).toContain('--color-danger')
    expect(names).toHaveLength(Object.keys(palettes.light).length)
    expect(new Set(names).size).toBe(names.length)
  })
})
