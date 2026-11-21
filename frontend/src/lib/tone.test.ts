import { describe, expect, it } from 'vitest'

import { themeColors } from '../test/theme'
import { toneBorder, toneFill, toneText, type Tone } from './tone'

// tones are every tone, in the order the maps list them.
const tones: readonly Tone[] = ['accent', 'info', 'success', 'warning', 'caution', 'danger', 'muted']

describe('tone maps', () => {
  it('spell out every tone’s text, fill, and border class', () => {
    expect(toneText).toStrictEqual({
      accent: 'text-accent',
      info: 'text-info',
      success: 'text-success',
      warning: 'text-warning',
      caution: 'text-caution',
      danger: 'text-danger',
      muted: 'text-muted',
    })
    expect(toneFill).toStrictEqual({
      accent: 'bg-accent',
      info: 'bg-info',
      success: 'bg-success',
      warning: 'bg-warning',
      caution: 'bg-caution',
      danger: 'bg-danger',
      muted: 'bg-muted',
    })
    expect(toneBorder).toStrictEqual({
      accent: 'border-accent',
      info: 'border-info',
      success: 'border-success',
      warning: 'border-warning',
      caution: 'border-caution',
      danger: 'border-danger',
      muted: 'border-muted',
    })
  })

  it('list the tones in one order', () => {
    expect(Object.keys(toneText)).toEqual(tones)
    expect(Object.keys(toneFill)).toEqual(tones)
    expect(Object.keys(toneBorder)).toEqual(tones)
  })

  it.each(tones)('name %s after a color the theme defines', (tone) => {
    expect(themeColors()).toContain(`--color-${tone}`)
  })
})
