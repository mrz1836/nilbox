import { describe, expect, it } from 'vitest'

import { terminalLabel, terminals } from './terminals'

describe('terminalLabel', () => {
  it('names each terminal app this build knows, whatever the fallback', () => {
    expect(terminalLabel('iterm2', '')).toBe('iTerm2')
    expect(terminalLabel('warp', 'your terminal')).toBe('Warp')
    expect(terminalLabel('terminal', 'your terminal')).toBe('Terminal')
  })

  it.each<[string | null | undefined, string]>([
    [null, 'your terminal'],
    [undefined, 'your terminal'],
    ['no-such-terminal', 'no-such-terminal'],
    ['no-such-terminal', ''],
    ['warp-preview', ''],
    ['', ''],
    ['constructor', 'constructor'],
    ['toString', ''],
  ])('says the fallback for %j', (id, fallback) => {
    expect(terminalLabel(id, fallback)).toBe(fallback)
  })
})

describe('terminals', () => {
  it('offers iTerm2, Warp, then Terminal', () => {
    expect(terminals.map((t) => t.label)).toEqual(['iTerm2', 'Warp', 'Terminal'])
  })
})
