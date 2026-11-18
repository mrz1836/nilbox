import { render } from '@testing-library/react'
import type { ComponentType } from 'react'
import { describe, expect, it } from 'vitest'

import type { ItemDetails } from '../../platform'
import { details, item } from '../../test/fixtures'
import { AutopilotIcon, CheckIcon, ClockIcon, ConflictIcon, RereviewIcon, XIcon } from '../icons'
import { MiniTrack, StateTrack } from './StateTrack'

// shapes describes the shapes an svg draws, each by its tag and attributes, to tell one
// icon from another, or '' for none.
function shapes(svg: Element | null | undefined): string {
  return [...(svg?.children ?? [])]
    .map((shape) => `${shape.tagName}(${[...shape.attributes].map((a) => `${a.name}=${a.value}`).join(' ')})`)
    .join(' ')
}

// drawing is what an icon draws, or '' for none, to tell which icon a step shows.
function drawing(Icon: ComponentType | null): string {
  if (Icon === null) {
    return ''
  }

  const { container, unmount } = render(<Icon />)
  const drawn = shapes(container.querySelector('svg'))
  unmount()

  return drawn
}

// renderTrack renders the track of a pull request with d's details, or none yet, and
// returns each step by name, with the track itself.
function renderTrack(d: Partial<ItemDetails> | null, isDraft = false) {
  const { container } = render(<StateTrack item={item({ isDraft, details: d === null ? null : details(d) })} />)
  const step = (name: string) => {
    const found = container.querySelector(`[data-step="${name}"]`)
    if (found === null) {
      throw new Error(`no ${name} step`)
    }

    return found
  }

  return { container, checks: step('checks'), review: step('review'), merge: step('merge') }
}

// shown is what a step draws inside its circle: an icon's shapes, or '' for a ring.
function shown(step: Element): string {
  return shapes(step.querySelector('svg'))
}

const checks = (c: Partial<ItemDetails['checks']>): Partial<ItemDetails> => ({
  checks: { state: 'SUCCESS', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 0, ...c },
})
const reviews = (r: Partial<ItemDetails['reviews']>): Partial<ItemDetails> => ({
  reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0, ...r },
})

describe('StateTrack', () => {
  it.each([
    ['passed', checks({ passed: 12 }), CheckIcon, 'Checks: 12 passed'],
    ['failed', checks({ state: 'FAILURE', passed: 13, failed: 1 }), XIcon, 'Checks: 13 passed, 1 failing'],
    ['awaiting', checks({ state: '', awaiting: 2 }), ClockIcon, 'Checks: 2 workflow runs awaiting approval'],
    ['none', checks({ state: '' }), null, 'No checks on the latest commit'],
  ])('shows the checks %s', (state, d, icon, sentence) => {
    const { container, checks: step } = renderTrack(d)

    expect(step).toHaveAttribute('data-state', state)
    expect(shown(step)).toBe(drawing(icon))
    expect(container).toHaveTextContent(sentence)
  })

  it('marks held runs and running checks, the running ones with a moving dot', () => {
    const { checks: held } = renderTrack(checks({ state: '', awaiting: 1 }))
    expect(held.querySelector('[data-checks-held]')).not.toBeNull()
    expect(held.querySelector('[data-moving]')).toBeNull()

    const { container, checks: running } = renderTrack(checks({ state: 'PENDING', passed: 16, pending: 1 }))
    expect(running).toHaveAttribute('data-state', 'pending')
    expect(running.querySelector('[data-checks-running] [data-moving]')).not.toBeNull()
    expect(running).toHaveClass('text-warning')
    expect(container).toHaveTextContent('Checks: 16 passed, 1 running')
  })

  it.each([
    [
      'rereview',
      { pushedSinceReview: true, ...reviews({ decision: 'CHANGES_REQUESTED', reviewers: 1, changesRequested: 1 }) },
      RereviewIcon,
      'You asked for changes, and the author pushed since.',
    ],
    [
      'rereview',
      { pushedSinceReview: true, reviewRequestedAgain: true },
      RereviewIcon,
      'You asked for changes, and the author pushed since, and asked for your review again.',
    ],
    [
      'approved',
      reviews({ decision: 'APPROVED', reviewers: 2, reviewed: 1, approvals: 1 }),
      CheckIcon,
      'Reviews: 1 of 2 reviewers reviewed, 1 approved.',
    ],
    [
      'changes',
      reviews({ decision: 'CHANGES_REQUESTED', reviewers: 1, reviewed: 1, changesRequested: 1 }),
      XIcon,
      'Reviews: 1 of 1 reviewer reviewed, 1 requested changes.',
    ],
    [
      'waiting',
      reviews({ decision: 'REVIEW_REQUIRED', reviewers: 1 }),
      null,
      'Reviews: 0 of 1 reviewer reviewed, approval required.',
    ],
  ])('shows the review %s', (state, d, icon, sentence) => {
    const { container, review } = renderTrack(d)

    expect(review).toHaveAttribute('data-state', state)
    expect(shown(review)).toBe(drawing(icon))
    expect(container).toHaveTextContent(sentence)
  })

  it('draws re-review in the accent color, not as the changes it once requested', () => {
    const { review } = renderTrack({
      pushedSinceReview: true,
      ...reviews({ decision: 'CHANGES_REQUESTED', reviewers: 1, changesRequested: 1 }),
    })

    expect(review).toHaveClass('text-accent')
    expect(review).not.toHaveClass('text-danger')
  })

  it.each([
    ['conflicts', { mergeable: 'CONFLICTING', autoMerge: 'SQUASH' }, ConflictIcon, 'Conflicts with its base branch.'],
    ['auto', { autoMerge: 'SQUASH' }, AutopilotIcon, 'Auto-merge is on: squash.'],
    ['auto', { autoMerge: 'MERGE' }, AutopilotIcon, 'Auto-merge is on: merge commit.'],
    ['mergeable', { mergeable: 'MERGEABLE' }, CheckIcon, 'No conflicts.'],
    ['unknown', { mergeable: 'UNKNOWN' }, null, 'GitHub is still working out whether it can merge.'],
  ])('shows the merge %s', (state, d, icon, sentence) => {
    const { container, merge } = renderTrack(d)

    expect(merge).toHaveAttribute('data-state', state)
    expect(shown(merge)).toBe(drawing(icon))
    expect(container).toHaveTextContent(sentence)
  })

  it('draws auto-merge as a dashed ring in the success color', () => {
    const { merge } = renderTrack({ autoMerge: 'REBASE' })

    expect(merge).toHaveClass('border-dashed', 'text-success')
  })

  it('reads every step’s sentence to a screen reader, and has no tooltips', () => {
    const { container } = renderTrack({})

    expect(container.querySelector('.sr-only')).toHaveTextContent(
      'Checks: 12 passed, 3 skipped. Reviews: 1 of 2 reviewers reviewed, 1 approved. No conflicts.',
    )
    expect(container.querySelectorAll('[title]')).toHaveLength(0)
    for (const step of container.querySelectorAll('[data-step]')) {
      expect(step).toHaveAttribute('aria-hidden', 'true')
    }
  })

  it('shows a draft’s checks as they are, and its review and merge as dotted rings', () => {
    const {
      container,
      checks: step,
      review,
      merge,
    } = renderTrack({ ...checks({ state: 'FAILURE', passed: 2, failed: 1 }), mergeable: 'CONFLICTING' }, true)

    expect(step).toHaveAttribute('data-state', 'failed')
    expect(review).toHaveAttribute('data-state', 'none')
    expect(merge).toHaveAttribute('data-state', 'none')
    expect(review).toHaveClass('border-dotted')
    expect(merge).toHaveClass('border-dotted')
    expect(shown(review)).toBe('')
    expect(shown(merge)).toBe('')
    expect(container.querySelector('.sr-only')).toHaveTextContent(
      'Checks: 2 passed, 1 failing. Reviews: 1 of 2 reviewers reviewed, 1 approved. Conflicts with its base branch.',
    )
  })

  it('keeps its shape with three dotted rings while the details load', () => {
    const { container, checks: step, review, merge } = renderTrack(null)

    for (const s of [step, review, merge]) {
      expect(s).toHaveAttribute('data-state', 'none')
      expect(s).toHaveClass('border-dotted')
    }
    expect(container.querySelector('.sr-only')).toHaveTextContent('Checks, review, and merge: not read yet')
  })
})

describe('MiniTrack', () => {
  // dots renders the mini track of a pull request with d's details, and returns each
  // dot's state and classes, with the track.
  function dots(d: Partial<ItemDetails>) {
    const { container } = render(<MiniTrack item={item({ details: details(d) })} />)
    const track = container.firstElementChild as HTMLElement
    const steps = [...track.querySelectorAll('[data-step]')]

    return { track, steps }
  }

  it('says the three steps in one tooltip, each a small dot', () => {
    const { track, steps } = dots({})

    expect(track).toHaveAttribute('role', 'img')
    expect(track).toHaveClass('relative', 'z-10')
    expect(track).toHaveAttribute('title', track.getAttribute('aria-label'))
    expect(track.getAttribute('title')).toMatch(/^Checks: .*\. Reviews: .*\. No conflicts\.$/)
    expect(steps.map((s) => s.getAttribute('data-step'))).toEqual(['checks', 'review', 'merge'])
    for (const step of steps) {
      expect(step).toHaveAttribute('aria-hidden', 'true')
      expect(step).toHaveClass('size-2.25')
    }
  })

  it('fills a cross, rings auto-merge in the success color, and dashes a review still waiting', () => {
    const { steps } = dots({
      checks: { ...details().checks, failed: 1 },
      reviews: { ...details().reviews, decision: 'REVIEW_REQUIRED', approvals: 0, reviewed: 0 },
      autoMerge: 'SQUASH',
    })
    const [checks, review, merge] = steps

    expect(checks).toHaveClass('border-danger', 'bg-danger')
    expect(review).toHaveAttribute('data-state', 'waiting')
    expect(review).toHaveClass('border-dashed')
    expect(merge).toHaveAttribute('data-state', 'auto')
    expect(merge).toHaveClass('border-success')
    expect(merge).not.toHaveClass('bg-success')
  })
})
