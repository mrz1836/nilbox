import { describe, expect, it } from 'vitest'

import type { Checks, Reviews } from '../platform'
import { checksSignal, reviewsSignal, sizeOf, sizeText, type Size } from './signals'

function checks(overrides: Partial<Checks> = {}): Checks {
  return { state: 'SUCCESS', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 0, ...overrides }
}

function reviews(overrides: Partial<Reviews> = {}): Reviews {
  return { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0, ...overrides }
}

describe('checksSignal', () => {
  it('passes when every check that ran passed, skipped ones aside', () => {
    expect(checksSignal(checks({ passed: 12, skipped: 15 }))).toEqual({
      kind: 'passed',
      tone: 'success',
      short: '12/12',
      long: 'Checks: 12 passed, 15 skipped',
    })
  })

  it('fails when any check failed, even with others still running', () => {
    expect(checksSignal(checks({ state: 'FAILURE', passed: 18, failed: 5, pending: 4, skipped: 6 }))).toEqual({
      kind: 'failed',
      tone: 'danger',
      short: '18/27',
      long: 'Checks: 18 passed, 5 failing, 4 running, 6 skipped',
    })
  })

  it('runs while any check is still running', () => {
    expect(checksSignal(checks({ state: 'PENDING', passed: 10, pending: 2 }))).toMatchObject({
      kind: 'pending',
      tone: 'warning',
      short: '10/12',
      long: 'Checks: 10 passed, 2 running',
    })
  })

  it('follows the overall state when there are no counts', () => {
    expect(checksSignal(checks({ state: 'ERROR' }))).toMatchObject({
      kind: 'failed',
      short: 'Failing',
      long: 'Checks failing',
    })
    expect(checksSignal(checks({ state: 'EXPECTED' }))).toMatchObject({
      kind: 'pending',
      short: 'Running',
      long: 'Checks running',
    })
  })

  it('marks workflow runs held for approval, which GitHub leaves out of its state, in place of a pass', () => {
    // A fork's runs wait for a maintainer while another app's check passed: GitHub reads SUCCESS.
    expect(checksSignal(checks({ passed: 1, awaiting: 4 }))).toEqual({
      kind: 'awaiting',
      tone: 'warning',
      short: '4 held',
      long: 'Checks: 4 workflow runs awaiting approval, 1 passed',
    })
    expect(checksSignal(checks({ state: '', awaiting: 1 }))).toEqual({
      kind: 'awaiting',
      tone: 'warning',
      short: '1 held',
      long: 'Checks: 1 workflow run awaiting approval',
    })
  })

  it('puts a failure before held runs, and held runs before running checks', () => {
    expect(checksSignal(checks({ state: 'FAILURE', passed: 2, failed: 1, awaiting: 3 }))).toMatchObject({
      kind: 'failed',
      long: 'Checks: 2 passed, 1 failing',
    })
    expect(checksSignal(checks({ state: 'PENDING', passed: 2, pending: 1, awaiting: 3 }))).toMatchObject({
      kind: 'awaiting',
      short: '3 held',
      long: 'Checks: 3 workflow runs awaiting approval, 2 passed, 1 running',
    })
  })

  it('says when every check was skipped or there are none', () => {
    expect(checksSignal(checks({ skipped: 1 }))).toEqual({
      kind: 'none',
      tone: 'muted',
      short: 'Skipped',
      long: 'Checks: 1 check skipped',
    })
    expect(checksSignal(checks({ state: '' }))).toEqual({
      kind: 'none',
      tone: 'muted',
      short: 'No checks',
      long: 'No checks on the latest commit',
    })
  })
})

describe('reviewsSignal', () => {
  it('turns green once approved', () => {
    expect(reviewsSignal(reviews({ decision: 'APPROVED', reviewers: 5, reviewed: 2, approvals: 2 }))).toEqual({
      kind: 'approved',
      tone: 'success',
      short: '2/5',
      long: 'Reviews: 2 of 5 reviewers reviewed, 2 approved',
    })
  })

  it('turns red when changes were requested', () => {
    expect(
      reviewsSignal(reviews({ decision: 'CHANGES_REQUESTED', reviewers: 1, reviewed: 1, changesRequested: 1 })),
    ).toEqual({
      kind: 'changes',
      tone: 'danger',
      short: '1/1',
      long: 'Reviews: 1 of 1 reviewer reviewed, 1 requested changes',
    })
  })

  it('reads approvals and change requests where no review is required', () => {
    expect(reviewsSignal(reviews({ reviewers: 2, reviewed: 1, approvals: 1 }))).toMatchObject({ kind: 'approved' })
    expect(reviewsSignal(reviews({ reviewers: 2, reviewed: 2, approvals: 1, changesRequested: 1 }))).toMatchObject({
      kind: 'changes',
    })
  })

  it('waits while a required review is outstanding', () => {
    expect(reviewsSignal(reviews({ decision: 'REVIEW_REQUIRED', reviewers: 3, reviewed: 1, approvals: 1 }))).toEqual({
      kind: 'waiting',
      tone: 'muted',
      short: '1/3',
      long: 'Reviews: 1 of 3 reviewers reviewed, 1 approved, approval required',
    })
  })

  it('says when no one was asked yet', () => {
    expect(reviewsSignal(reviews())).toEqual({
      kind: 'waiting',
      tone: 'muted',
      short: '—',
      long: 'Reviews: no reviewers yet',
    })
  })
})

describe('sizeOf', () => {
  it.each<[number, Size]>([
    [0, 'XS'],
    [9, 'XS'],
    [10, 'S'],
    [29, 'S'],
    [30, 'M'],
    [99, 'M'],
    [100, 'L'],
    [499, 'L'],
    [500, 'XL'],
    [999, 'XL'],
    [1000, 'XXL'],
    [250_000, 'XXL'],
  ])('grades %d changed lines as %s', (lines, size) => {
    expect(sizeOf(lines)).toBe(size)
  })

  it('says the size in words', () => {
    expect(sizeText(120, 30, 6, 3)).toBe('Size L: 120 lines added and 30 removed across 6 files in 3 commits')
    expect(sizeText(1, 0, 1, 1)).toBe('Size XS: 1 line added and 0 removed across 1 file in 1 commit')
  })
})
