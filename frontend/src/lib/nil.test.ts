import { describe, expect, it } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { FIXED_NOW } from '../test/clock'
import { item, list, nilMoment, snapshot } from '../test/fixtures'
import {
  earnedQuip,
  earnedQuips,
  firstQuip,
  flourishes,
  isInboxNil,
  listsShow,
  maxBurst,
  nextFlourish,
  nextQuip,
  openingQuip,
  partyCompact,
  pickQuip,
  plainFlourishes,
  quips,
  replayBurst,
  replayLabel,
  replayLabels,
  type Flourish,
} from './nil'
import { inboxLists, triageInbox } from './triage'

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
    for (const key of inboxLists) {
      expect(nil(snapshot({ lastRefreshedAt: refreshed, [key]: list([item()]) }))).toBe(false)
    }
  })

  it('is nil with everything snoozed or dismissed', () => {
    const lists = {
      reviewRequested: list([item({ id: 1, dismissedAt: '2026-06-14T09:00:00Z' })]),
      yours: list([item({ id: 2, snoozedUntil: '2026-06-16T09:00:00Z' })]),
    }

    expect(nil(snapshot({ lastRefreshedAt: refreshed, ...lists }))).toBe(true)
  })

  it('is not nil once a snooze has run out', () => {
    const back = list([item({ snoozedUntil: '2026-06-15T11:00:00Z' })])

    expect(nil(snapshot({ lastRefreshedAt: refreshed, yours: back }))).toBe(false)
  })

  it('is not nil while GitHub matched more than it returned', () => {
    const partial = list([item({ dismissedAt: '2026-06-14T09:00:00Z' })], 250)

    expect(nil(snapshot({ lastRefreshedAt: refreshed, yours: partial }))).toBe(false)
  })
})

describe('listsShow', () => {
  // shows reports whether a list shows below the party for the inbox as it sorts at now.
  const shows = (inbox: InboxSnapshot) => listsShow(triageInbox(inbox, FIXED_NOW))

  it('shows no list with nothing set aside', () => {
    expect(shows(snapshot({ lastRefreshedAt: refreshed }))).toBe(false)
  })

  it('shows a list that holds anything snoozed, dismissed, or muted', () => {
    for (const setAside of [
      item({ snoozedUntil: '2026-06-16T09:00:00Z' }),
      item({ dismissedAt: '2026-06-14T09:00:00Z' }),
      item({ repo: 'acme-org/quiet', repoRule: 'muted' }),
    ]) {
      expect(shows(snapshot({ lastRefreshedAt: refreshed, yours: list([setAside]) }))).toBe(true)
    }
  })
})

describe('partyCompact', () => {
  const folds = ['auto', 'folded', 'open'] as const

  it('fills the page when no list shows below it, however it was left', () => {
    for (const mode of ['milestones', 'quiet'] as const) {
      for (const fold of folds) {
        expect(partyCompact(fold, false, mode), `${fold}, ${mode}`).toBe(false)
      }
    }
  })

  it('gives the page to the lists below it as a strip, until its run opens it in full', () => {
    for (const mode of ['milestones', 'quiet'] as const) {
      expect(partyCompact('auto', true, mode)).toBe(true)
      expect(partyCompact('folded', true, mode)).toBe(true)
      expect(partyCompact('open', true, mode)).toBe(false)
    }
  })

  it('is always the strip with celebrations off', () => {
    for (const fold of folds) {
      for (const setAside of [false, true]) {
        expect(partyCompact(fold, setAside, 'off'), `${fold}, set aside ${String(setAside)}`).toBe(true)
      }
    }
  })
})

describe('quips', () => {
  it('opens on a quip from the list', () => {
    expect(pickQuip(() => 0)).toBe(quips[0])
    expect(pickQuip(() => 0.9999)).toBe(quips.at(-1))
    expect(quips).toContain(pickQuip())
  })

  it('cycles through every quip, and leads in from any other', () => {
    expect(nextQuip(firstQuip)).toBe(quips[0])
    expect(nextQuip(earnedQuips[3] ?? '')).toBe(quips[0])

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

describe('openingQuip', () => {
  // A Monday afternoon, where no moment's quip fits on its own.
  const monday = new Date(2026, 5, 15, 14, 0)

  it('opens a party found empty on a random quip', () => {
    expect(openingQuip(false, nilMoment({ streak: 4 }), monday, () => 0)).toBe(quips[0])
    expect(openingQuip(false, null, monday, () => 0.9999)).toBe(quips.at(-1))
  })

  it('greets the first nil the user earns, and an earned one with no moment, with the first quip', () => {
    const first = nilMoment({ earned: true, count: 1, unlocked: [{ kind: 'first', n: 1 }] })

    expect(openingQuip(true, first, monday)).toBe(firstQuip)
    expect(openingQuip(true, null, monday)).toBe(firstQuip)
  })

  it('speaks to what a nil unlocked', () => {
    const swept = nilMoment({ earned: true, count: 9, cleared: 14, unlocked: [{ kind: 'sweep', n: 10 }] })

    expect(openingQuip(true, swept, monday, () => 0.99)).toBe('14 down, 0 to go.')
  })

  it('turns to the next earned quip, nil by nil, when no moment’s quip fits', () => {
    const plain = nilMoment({ earned: true, count: 5 })

    expect(openingQuip(true, plain, monday)).toBe(earnedQuip(5))
    expect(openingQuip(true, { ...plain, count: 6 }, monday)).toBe(earnedQuip(6))
    expect(earnedQuip(5)).not.toBe(earnedQuip(6))
  })

  it('is as likely to speak to the moment as to turn to the next earned quip', () => {
    const streak = nilMoment({ earned: true, count: 5, streak: 2, best: 4 })

    expect(openingQuip(true, streak, monday, () => 0.2)).toBe('Day 2 at nil. Don’t break the chain.')
    expect(openingQuip(true, streak, monday, () => 0.7)).toBe(earnedQuip(5))
  })
})

describe('earnedQuip', () => {
  it('turns through every earned quip, nil by nil', () => {
    const seen = new Set(earnedQuips.map((_, i) => earnedQuip(i + 1)))

    expect(seen.size).toBe(earnedQuips.length)
    expect(earnedQuip(1 + earnedQuips.length)).toBe(earnedQuip(1))
    expect(earnedQuips).not.toContain(firstQuip)
  })
})

describe('nextFlourish', () => {
  it('never plays the same flourish twice in a row', () => {
    for (const last of flourishes) {
      for (const r of [0, 0.3, 0.6, 0.9999]) {
        expect(nextFlourish(last, () => r)).not.toBe(last)
      }
    }
  })

  it('plays every flourish in time', () => {
    const seen = new Set<Flourish>()
    let last: Flourish | null = null

    for (let i = 0; i < 40; i++) {
      last = nextFlourish(last, () => (i * 0.37) % 1)
      seen.add(last)
    }

    expect([...seen].sort()).toEqual([...flourishes].sort())
  })

  it('picks only from the choices it is given, and plays the one there is', () => {
    expect(plainFlourishes).not.toContain('stamp')

    for (const r of [0, 0.5, 0.9999]) {
      expect(plainFlourishes).toContain(nextFlourish('glint', () => r, plainFlourishes))
    }

    expect(nextFlourish('hop', () => 0.5, ['hop'])).toBe('hop')
  })
})
