import { describe, expect, it } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { FIXED_NOW } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import {
  earnedQuip,
  isInboxNil,
  maxBurst,
  nextQuip,
  pickQuip,
  quips,
  replayBurst,
  replayLabel,
  replayLabels,
} from './nil'
import { triageInbox } from './triage'

const refreshed = '2026-06-15T11:55:00Z'

// nil reports whether the inbox is nil as it sorts at now.
function nil(inbox: InboxSnapshot): boolean {
  return isInboxNil(inbox, triageInbox(inbox, FIXED_NOW))
}

describe('isInboxNil', () => {
  it('is nil once refreshed with every list empty', () => {
    expect(nil(snapshot({ lastRefreshedAt: refreshed }))).toBe(true)
  })

  it('is not nil before the first refresh, or after a failed one', () => {
    expect(nil(snapshot())).toBe(false)
    expect(nil(snapshot({ lastRefreshedAt: refreshed, lastError: 'rate limited' }))).toBe(false)
  })

  it('is not nil while any list holds an item', () => {
    for (const key of ['reviewRequested', 'authored', 'assigned'] as const) {
      expect(nil(snapshot({ lastRefreshedAt: refreshed, [key]: list([item()]) }))).toBe(false)
    }
  })

  it('is nil with everything snoozed or dismissed', () => {
    const lists = {
      reviewRequested: list([item({ id: 1, dismissedAt: '2026-06-14T09:00:00Z' })]),
      authored: list([item({ id: 2, snoozedUntil: '2026-06-16T09:00:00Z' })]),
    }

    expect(nil(snapshot({ lastRefreshedAt: refreshed, ...lists }))).toBe(true)
  })

  it('is not nil once a snooze has run out', () => {
    const back = list([item({ snoozedUntil: '2026-06-15T11:00:00Z' })])

    expect(nil(snapshot({ lastRefreshedAt: refreshed, authored: back }))).toBe(false)
  })

  it('is not nil while GitHub matched more than it returned', () => {
    const partial = list([item({ dismissedAt: '2026-06-14T09:00:00Z' })], 250)

    expect(nil(snapshot({ lastRefreshedAt: refreshed, assigned: partial }))).toBe(false)
  })
})

describe('quips', () => {
  it('opens on a quip from the list', () => {
    expect(pickQuip(() => 0)).toBe(quips[0])
    expect(pickQuip(() => 0.9999)).toBe(quips.at(-1))
    expect(quips).toContain(pickQuip())
  })

  it('cycles through every quip, and leads in from the earned one', () => {
    expect(nextQuip(earnedQuip)).toBe(quips[0])

    let quip = quips[0] ?? ''
    const seen = new Set<string>()
    for (let i = 0; i < quips.length; i++) {
      seen.add(quip)
      quip = nextQuip(quip)
    }

    expect(seen.size).toBe(quips.length)
    expect(quip).toBe(quips[0])
  })
})

describe('replays', () => {
  it('escalates the command with every run, then holds', () => {
    expect(replayLabel(0)).toBe('go run ./party')
    expect(replayLabel(3)).toBe('sudo go run ./party')
    expect(replayLabel(replayLabels.length - 1)).toBe('sudo rm -rf ./boredom')
    expect(replayLabel(99)).toBe('sudo rm -rf ./boredom')
  })

  it('throws more confetti with every run, up to a limit', () => {
    expect(replayBurst(2)).toBeGreaterThan(replayBurst(1))
    expect(replayBurst(99)).toBe(maxBurst)
  })
})
