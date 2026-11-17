import { describe, expect, it } from 'vitest'

import { settings } from '../test/fixtures'
import { defaultHiddenLabels, labelMatcher } from './labels'

describe('labelMatcher', () => {
  it('matches a whole name, ignoring case, with * for any run and ? for one character', () => {
    const hides = labelMatcher(['size/*', 'priority: ?', 'WontFix'])

    expect(hides('size/XL')).toBe(true)
    expect(hides('Size/S')).toBe(true)
    expect(hides('size/')).toBe(true)
    expect(hides('wontfix')).toBe(true)
    expect(hides('priority: p')).toBe(true)
    expect(hides('priority: p1'), '? is one character').toBe(false)
    expect(hides('pr-size/XL'), 'the whole name').toBe(false)
    expect(hides('bug')).toBe(false)
  })

  it('lets * run across slashes, as Autopilot’s label patterns do', () => {
    const hides = labelMatcher(['*.md', 'area/*/ui'])

    expect(hides('docs/readme.md')).toBe(true)
    expect(hides('area/web/app/ui')).toBe(true)
    expect(hides('area/ui')).toBe(false)
  })

  it('reads every other character as itself', () => {
    const hides = labelMatcher(['good first issue (help)', 'a.b', '[x]', 'c++', '$ ^ |'])

    expect(hides('good first issue (help)')).toBe(true)
    expect(hides('a.b')).toBe(true)
    expect(hides('aXb'), 'a dot is a dot').toBe(false)
    expect(hides('[x]')).toBe(true)
    expect(hides('x')).toBe(false)
    expect(hides('c++')).toBe(true)
    expect(hides('$ ^ |')).toBe(true)
  })

  it('counts a character outside ASCII as one', () => {
    const hides = labelMatcher(['é?', '🐛*'])

    expect(hides('éa')).toBe(true)
    expect(hides('🐛 bug')).toBe(true)
  })

  it('hides nothing with no pattern, or only blank ones', () => {
    expect(labelMatcher([])('size/XL')).toBe(false)
    expect(labelMatcher(['', '  '])('')).toBe(false)
  })

  it('starts from the backend’s default until the settings load', () => {
    expect(defaultHiddenLabels).toEqual(settings().defaultHiddenLabels)
  })
})
