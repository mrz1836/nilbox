import { describe, expect, it } from 'vitest'

import { terminalLabel } from './terminals'

describe('terminalLabel', () => {
  it('names each terminal app this build knows, whatever the fallback', () => {
    expect(terminalLabel('iterm2', '')).toBe('iTerm2')
    expect(terminalLabel('terminal', 'your terminal')).toBe('Terminal')
  })

  it.each<[string | null | undefined, string]>([
    [null, 'your terminal'],
    [undefined, 'your terminal'],
    ['warp', 'warp'],
    ['warp', ''],
    ['', ''],
    ['constructor', 'constructor'],
    ['toString', ''],
  ])('says the fallback for %j', (id, fallback) => {
    expect(terminalLabel(id, fallback)).toBe(fallback)
  })
})
