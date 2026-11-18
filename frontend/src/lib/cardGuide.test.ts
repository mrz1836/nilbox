import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { settings } from '../test/fixtures'
import { guideItem, guideLaunchOptions, guideResolution, guideSession } from './cardGuide'

describe('the card guide’s sample', () => {
  it('counts its times back from now, so its ages always read the same', () => {
    const item = guideItem(FIXED_NOW)
    const session = guideSession(FIXED_NOW)

    expect(item.updatedAt).toBe('2026-06-15T11:42:00.000Z')
    expect(session.endedAt).toBe('2026-06-15T11:59:00.000Z')
    expect(session.state).toBe('ended')
    expect(session.itemId).toBe(item.id)
  })

  it('is a re-review asked of the user, from an outside contributor whose files touch Workflows', () => {
    const item = guideItem(FIXED_NOW)

    expect(item.reviewRequest).toBe('you')
    expect(item.details?.pushedSinceReview).toBe(true)
    expect(item.trusted).toBe(false)
    expect(item.files?.groups.map((g) => g.short)).toEqual(['Workflows'])
    expect(guideResolution().situations.map((s) => s.situation)).toEqual(['rereview'])
  })

  it('offers launch options that save nothing and open nothing', async () => {
    const launch = guideLaunchOptions()

    expect(launch.agents.map((a) => a.name)).toEqual(['Claude'])
    await expect(launch.onDefaultsSaved()).resolves.toBeUndefined()
    expect(() => {
      launch.onManagePrompts()
    }).not.toThrow()
    expect(() => {
      launch.onSettingsSaved(settings())
    }).not.toThrow()
  })
})
