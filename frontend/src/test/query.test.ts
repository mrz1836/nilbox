import { describe, expect, it, vi } from 'vitest'

import { item } from './fixtures'
import { editActionsOf, triageActionsOf } from './query'

describe('triageActionsOf', () => {
  it('snoozes for three days, with nothing pinned or muted, each action a mock', async () => {
    const triage = triageActionsOf()

    expect(triage.snoozeDays).toBe(3)
    expect(triage.repoRules).toEqual({ pinned: [], muted: [] })
    await expect(triage.onRefresh(item())).resolves.toBeUndefined()

    triage.onDismiss(item())
    expect(triage.onDismiss).toHaveBeenCalledExactlyOnceWith(item())
  })

  it('takes the overrides given, and makes fresh mocks and rules each time', () => {
    const muted = triageActionsOf({ repoRules: { pinned: [], muted: ['acme-org'] }, snoozeDays: 7 })

    expect(muted.repoRules).toEqual({ pinned: [], muted: ['acme-org'] })
    expect(muted.snoozeDays).toBe(7)
    expect(triageActionsOf().onPin).not.toBe(triageActionsOf().onPin)
    expect(triageActionsOf().repoRules).not.toBe(triageActionsOf().repoRules)
  })
})

describe('editActionsOf', () => {
  it('settles the edits on GitHub at once, and offers no nudge unless given', async () => {
    const edits = editActionsOf()

    await expect(edits.onUnassign(item())).resolves.toBeUndefined()
    await expect(edits.onRemoveReview(item())).resolves.toBeUndefined()
    expect(edits).not.toHaveProperty('onNudge')

    const onNudge = vi.fn()
    expect(editActionsOf({ onNudge }).onNudge).toBe(onNudge)
  })
})
