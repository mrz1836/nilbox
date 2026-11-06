import { describe, expect, it } from 'vitest'

import { item, list, snapshot } from '../test/fixtures'
import {
  earnedQuip,
  isInboxNil,
  maxBurst,
  nextQuip,
  nilStats,
  pickQuip,
  quips,
  replayBurst,
  replayLabel,
  replayLabels,
} from './nil'

const now = new Date('2026-06-15T12:00:00Z')
const refreshed = '2026-06-15T11:55:00Z'

describe('isInboxNil', () => {
  it('is nil once refreshed with every list empty', () => {
    expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed }), now)).toBe(true)
  })

  it('is not nil before the first refresh, or after a failed one', () => {
    expect(isInboxNil(snapshot(), now)).toBe(false)
    expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed, lastError: 'rate limited' }), now)).toBe(false)
  })

  it('is not nil while any list holds an item', () => {
    for (const key of ['reviewRequested', 'authored', 'assigned'] as const) {
      expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed, [key]: list([item()]) }), now)).toBe(false)
    }
  })

  it('is nil with everything snoozed or dismissed', () => {
    const lists = {
      reviewRequested: list([item({ id: 1, dismissedAt: '2026-06-14T09:00:00Z' })]),
      authored: list([item({ id: 2, snoozedUntil: '2026-06-16T09:00:00Z' })]),
    }

    expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed, ...lists }), now)).toBe(true)
  })

  it('is not nil once a snooze has run out', () => {
    const back = list([item({ snoozedUntil: '2026-06-15T11:00:00Z' })])

    expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed, authored: back }), now)).toBe(false)
  })

  it('is not nil while GitHub matched more than it returned', () => {
    const partial = list([item({ dismissedAt: '2026-06-14T09:00:00Z' })], 250)

    expect(isInboxNil(snapshot({ lastRefreshedAt: refreshed, assigned: partial }), now)).toBe(false)
  })
})

describe('nilStats', () => {
  it('counts what was set aside once each, and finds the soonest snooze', () => {
    const later = item({ id: 1, snoozedUntil: '2026-06-18T09:00:00Z' })
    const sooner = item({ id: 2, snoozedUntil: '2026-06-16T09:00:00Z' })
    const gone = item({ id: 3, dismissedAt: '2026-06-14T09:00:00Z' })

    const stats = nilStats(
      snapshot({
        reviewRequested: list([later, gone]),
        // The same items again, in another list, count once.
        authored: list([later, sooner]),
        assigned: list([gone]),
      }),
      now,
    )

    expect(stats).toEqual({ snoozed: 2, dismissed: 1, nextBack: new Date('2026-06-16T09:00:00Z') })
  })

  it('has no comeback when nothing is snoozed', () => {
    expect(nilStats(snapshot(), now)).toEqual({ snoozed: 0, dismissed: 0, nextBack: null })
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
