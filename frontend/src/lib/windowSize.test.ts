import { describe, expect, it } from 'vitest'

import { presetFor, sizeKey, sizeLabel, windowPresets } from './windowSize'

describe('window sizes', () => {
  it('offers distinct presets, smallest first, within the backend bounds', () => {
    const keys = windowPresets.map((p) => sizeKey(p.size))

    expect(new Set(keys).size).toBe(windowPresets.length)
    let previous = { width: 0, height: 0 }
    for (const { size } of windowPresets) {
      expect(size.width).toBeGreaterThanOrEqual(800)
      expect(size.height).toBeGreaterThanOrEqual(520)
      expect(size.width).toBeGreaterThan(previous.width)
      expect(size.height).toBeGreaterThan(previous.height)
      previous = size
    }
  })

  it('names a size as a key and for people', () => {
    expect(sizeKey({ width: 1280, height: 800 })).toBe('1280x800')
    expect(sizeLabel({ width: 1280, height: 800 })).toBe('1280 × 800')
  })

  it('finds the preset a key names', () => {
    expect(presetFor('1440x900')).toEqual({ label: 'Large', size: { width: 1440, height: 900 } })
    expect(presetFor('1366x768')).toBeUndefined()
  })
})
