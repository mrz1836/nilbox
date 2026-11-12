import { describe, expect, it } from 'vitest'

import { progressStep } from './worktreeProgress'

describe('progressStep', () => {
  it('words each phase of a listing, with how far through it is', () => {
    expect(progressStep(null, '~/projects')).toEqual({ text: 'Checking the worktrees…', share: null })
    expect(progressStep({ phase: 'clones', done: 0, total: 0 }, '~/projects')).toEqual({
      text: 'Finding the clones under ~/projects…',
      share: null,
    })
    expect(progressStep({ phase: 'clones', done: 0, total: 0 }, '').text).toBe('Finding the clones…')
    expect(progressStep({ phase: 'listing', done: 3, total: 12 }, '')).toEqual({
      text: 'Listing worktrees · 3 of 12 clones',
      share: 0.25,
    })
    expect(progressStep({ phase: 'listing', done: 0, total: 1 }, '').text).toBe('Listing worktrees · 0 of 1 clone')
    expect(progressStep({ phase: 'checking', done: 2, total: 4 }, '')).toEqual({
      text: 'Checking each worktree · 2 of 4 worktrees',
      share: 0.5,
    })
    expect(progressStep({ phase: 'checking', done: 0, total: 0 }, '').share).toBeNull()
    expect(progressStep({ phase: 'github', done: 0, total: 1 }, '').text).toBe(
      'Asking GitHub about 1 pull request or issue…',
    )
    expect(progressStep({ phase: 'github', done: 0, total: 5 }, '').text).toBe(
      'Asking GitHub about 5 pull requests and issues…',
    )
  })
})
