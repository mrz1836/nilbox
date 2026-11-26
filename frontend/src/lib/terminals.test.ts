import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { terminalLabel, terminalNote, terminals } from './terminals'

describe('terminalNote', () => {
  it('says what each terminal needs before the first launch, and nothing for Terminal.app', () => {
    expect(terminalNote('iterm2')).toBe(`Your first launch asks to let ${brand.name} control iTerm2. Choose Allow.`)
    expect(terminalNote('warp')).toBe('Open Warp once and finish its welcome before your first launch.')
    expect(terminalNote('terminal')).toBe('')
  })
})

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
