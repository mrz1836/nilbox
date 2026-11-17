import { describe, expect, it } from 'vitest'

import { progressStep } from './inboxProgress'

describe('progressStep', () => {
  it('words each phase of a refresh, with how far through it is', () => {
    expect(progressStep(null)).toEqual({ text: 'Refreshing from GitHub…', share: null })
    expect(progressStep({ phase: 'search', done: 0, total: 0 })).toEqual({ text: 'Asking GitHub…', share: null })
    expect(progressStep({ phase: 'details', done: 3, total: 8 })).toEqual({
      text: 'Reading 3 of 8 new items…',
      share: 0.375,
    })
    expect(progressStep({ phase: 'details', done: 0, total: 1 }).text).toBe('Reading 0 of 1 new item…')
    expect(progressStep({ phase: 'files', done: 2, total: 5 })).toEqual({ text: 'Checking files: 2 of 5…', share: 0.4 })
  })

  it('counts nothing for a phase with no total, and never past the whole', () => {
    expect(progressStep({ phase: 'details', done: 0, total: 0 }).share).toBeNull()
    expect(progressStep({ phase: 'files', done: 7, total: 5 }).share).toBe(1)
  })
})
