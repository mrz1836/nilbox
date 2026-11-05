// What an inbox card says about an item's details: its checks, its reviews, and its size.
// Each signal has a tone (the color), a short form for the card, and a full sentence for
// the tooltip and screen readers.

import type { Checks, Reviews } from '../platform'
import { counted, formatCount } from './format'

export type Tone = 'success' | 'warning' | 'danger' | 'muted'

// toneText is each tone's text color, spelled out in full so Tailwind finds it.
export const toneText: Readonly<Record<Tone, string>> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  muted: 'text-muted',
}

export interface Signal<K extends string> {
  // kind picks the icon.
  kind: K
  tone: Tone
  short: string
  long: string
}

export type ChecksKind = 'passed' | 'failed' | 'pending' | 'none'

// checksSignal reads the head commit's checks. Skipped checks count toward neither side,
// so a pull request whose remaining checks all pass reads as fully passed.
export function checksSignal(checks: Checks): Signal<ChecksKind> {
  const { state, passed, failed, pending, skipped } = checks
  const ran = passed + failed + pending

  if (state === '' && ran === 0 && skipped === 0) {
    return { kind: 'none', tone: 'muted', short: 'No checks', long: 'No checks on the latest commit' }
  }

  const parts = [
    passed > 0 ? `${formatCount(passed)} passed` : '',
    failed > 0 ? `${formatCount(failed)} failing` : '',
    pending > 0 ? `${formatCount(pending)} running` : '',
    skipped > 0 ? `${formatCount(skipped)} skipped` : '',
  ].filter((part) => part !== '')
  const long = `Checks: ${parts.join(', ')}`
  const fraction = `${formatCount(passed)}/${formatCount(ran)}`

  if (failed > 0 || state === 'FAILURE' || state === 'ERROR') {
    return {
      kind: 'failed',
      tone: 'danger',
      short: ran > 0 ? fraction : 'Failing',
      long: ran > 0 ? long : 'Checks failing',
    }
  }

  if (pending > 0 || state === 'PENDING' || state === 'EXPECTED') {
    return {
      kind: 'pending',
      tone: 'warning',
      short: ran > 0 ? fraction : 'Running',
      long: ran > 0 ? long : 'Checks running',
    }
  }

  if (ran === 0) {
    return {
      kind: 'none',
      tone: 'muted',
      short: 'Skipped',
      long: `Checks: ${counted(skipped, 'check', 'checks')} skipped`,
    }
  }

  return { kind: 'passed', tone: 'success', short: fraction, long }
}

export type ReviewsKind = 'approved' | 'changes' | 'waiting'

// reviewsSignal reads who has reviewed: green once approved, red when changes were
// requested, muted while waiting.
export function reviewsSignal(reviews: Reviews): Signal<ReviewsKind> {
  const { decision, reviewers, reviewed, approvals, changesRequested } = reviews

  const parts = [
    reviewers === 0
      ? 'no reviewers yet'
      : `${formatCount(reviewed)} of ${counted(reviewers, 'reviewer', 'reviewers')} reviewed`,
    approvals > 0 ? `${formatCount(approvals)} approved` : '',
    changesRequested > 0 ? `${formatCount(changesRequested)} requested changes` : '',
    decision === 'REVIEW_REQUIRED' ? 'approval required' : '',
  ].filter((part) => part !== '')
  const long = `Reviews: ${parts.join(', ')}`
  const short = reviewers === 0 ? '—' : `${formatCount(reviewed)}/${formatCount(reviewers)}`

  if (decision === 'CHANGES_REQUESTED' || (decision === '' && changesRequested > 0)) {
    return { kind: 'changes', tone: 'danger', short, long }
  }

  if (decision === 'APPROVED' || (decision === '' && approvals > 0)) {
    return { kind: 'approved', tone: 'success', short, long }
  }

  return { kind: 'waiting', tone: 'muted', short, long }
}

export type Size = 'XS' | 'S' | 'M' | 'L' | 'XL' | 'XXL'

// sizes are the upper bounds, in changed lines, of every size but the largest: the
// common size-label scale.
const sizes: readonly (readonly [below: number, size: Size])[] = [
  [10, 'XS'],
  [30, 'S'],
  [100, 'M'],
  [500, 'L'],
  [1000, 'XL'],
]

// sizeOf grades a pull request by the lines it adds and removes together.
export function sizeOf(lines: number): Size {
  return sizes.find(([below]) => lines < below)?.[1] ?? 'XXL'
}

// sizeText says how big a pull request is in words.
export function sizeText(additions: number, deletions: number, files: number, commits: number): string {
  const size = sizeOf(additions + deletions)
  const lines = `${counted(additions, 'line', 'lines')} added and ${formatCount(deletions)} removed`

  return `Size ${size}: ${lines} across ${counted(files, 'file', 'files')} in ${counted(commits, 'commit', 'commits')}`
}
