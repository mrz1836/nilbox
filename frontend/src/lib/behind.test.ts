import { describe, expect, it } from 'vitest'

import { offer, review, updateBranchOffer } from '../test/fixtures'
import { behindWords, keepUpOffer } from './behind'

describe('behindWords', () => {
  it('counts the commits the branch is behind its base by, when the read could', () => {
    expect(behindWords(review({ behind: true, behindBy: 12 }))).toBe('12 commits behind main')
    expect(behindWords(review({ behind: true, behindBy: 1 }))).toBe('1 commit behind main')
    expect(behindWords(review({ behind: true, behindBy: 1500 }))).toBe('1,500 commits behind main')
  })

  it('says only that it is behind when the read couldn’t count them', () => {
    expect(behindWords(review({ behind: true, behindBy: 0 }))).toBe('Behind main')
    expect(behindWords(review({ behind: true, behindBy: 0, baseRef: '' }))).toBe('Behind its base branch')
  })

  it('says nothing of a branch up to date', () => {
    expect(behindWords(review())).toBe('')
    expect(behindWords(review({ behind: false, behindBy: 3 }))).toBe('')
  })
})

describe('keepUpOffer', () => {
  it('asks Dependabot to rebase its own pull request, over updating its branch', () => {
    const rebase = offer({ action: 'rebase_bot', label: 'Ask Dependabot to rebase', reviews: false })

    expect(keepUpOffer(review({ offers: [updateBranchOffer({ confirm: true }), rebase] }))).toBe(rebase)
  })

  it('updates anyone else’s branch, whether or not it can run', () => {
    const update = updateBranchOffer({ enabled: false, reason: 'Your account can’t update this branch.' })

    expect(keepUpOffer(review({ offers: [offer(), update] }))).toBe(update)
  })

  it('offers nothing when the sheet offers neither', () => {
    expect(keepUpOffer(review())).toBeUndefined()
  })
})
