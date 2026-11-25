import { describe, expect, it } from 'vitest'

import { allNotificationKinds, sessionKinds } from '../../lib/notifications'
import { kindIcons, kindTones } from './EntryRow'

describe('EntryRow’s lists', () => {
  it('gives every kind the history keeps an icon of its own, the sessions’ none', () => {
    const iconless = allNotificationKinds.filter((kind) => kindIcons[kind] === undefined)

    expect(iconless).toEqual(sessionKinds.map((k) => k.kind))
  })

  it('tones what went wrong red, and what waits for you amber', () => {
    expect(kindTones).toEqual({
      release_failed: 'text-danger',
      run_failed: 'text-danger',
      budget_draining: 'text-warning',
      autopilot_needs_you: 'text-warning',
      autopilot_problem: 'text-danger',
    })
  })
})
