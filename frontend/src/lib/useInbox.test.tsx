import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { at } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { findItem } from './triage'
import { useInbox } from './useInbox'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// held is an answer the test lets go of, so what shows while a call is under way can be read.
function held() {
  let resolve: (value: InboxSnapshot) => void = () => undefined
  const promise = new Promise<InboxSnapshot>((done) => {
    resolve = done
  })

  return { promise, resolve }
}

// marked is an inbox whose one item a routine snoozed and pinned.
function marked() {
  return snapshot({
    reviewRequested: list([
      item({ snoozedUntil: '2026-06-18T12:00:00Z', setAsideBy: 3, pinnedAt: '2026-06-15T11:00:00Z', pinnedBy: 3 }),
    ]),
  })
}

describe('useInbox’s triage', () => {
  it.each([
    ['restore', (s: ReturnType<typeof useInbox>) => s.restore(1), 'restoreItem', { setAsideBy: 0, pinnedBy: 3 }],
    ['dismiss', (s: ReturnType<typeof useInbox>) => s.dismiss(1), 'dismissItem', { setAsideBy: 0, pinnedBy: 3 }],
    ['snooze', (s: ReturnType<typeof useInbox>) => s.snooze(1, 2), 'snoozeItem', { setAsideBy: 0, pinnedBy: 3 }],
    ['unpin', (s: ReturnType<typeof useInbox>) => s.unpin(1), 'unpinItem', { setAsideBy: 3, pinnedBy: 0 }],
    ['pin', (s: ReturnType<typeof useInbox>) => s.pin(1), 'pinItem', { setAsideBy: 3, pinnedBy: 0 }],
  ] as const)(
    'takes over the routine’s mark a %s changes, at once, as the backend does',
    async (_, run, call, after) => {
      platform.getInboxSnapshot.mockResolvedValue(marked())

      const answer = held()

      platform[call].mockReturnValue(answer.promise)

      const { result } = renderHook(() => useInbox(), { wrapper: at() })

      await waitFor(() => {
        expect(result.current.snapshot).not.toBeNull()
      })

      let done: Promise<unknown> = Promise.resolve()

      act(() => {
        done = run(result.current)
      })

      await waitFor(() => {
        const shown = result.current.snapshot === null ? undefined : findItem(result.current.snapshot, 1)

        expect(shown).toMatchObject(after)
      })

      await act(async () => {
        answer.resolve(marked())
        await done
      })
    },
  )

  it('puts the routine’s mark back when the backend refuses the user’s triage', async () => {
    platform.getInboxSnapshot.mockResolvedValue(marked())
    platform.restoreItem.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHook(() => useInbox(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.snapshot).not.toBeNull()
    })

    let outcome: Awaited<ReturnType<ReturnType<typeof useInbox>['restore']>> | undefined

    await act(async () => {
      outcome = await result.current.restore(1)
    })

    expect(outcome).toEqual({ ok: false, error: 'database is locked' })

    const shown = result.current.snapshot === null ? undefined : findItem(result.current.snapshot, 1)

    expect(shown).toMatchObject({ setAsideBy: 3, pinnedBy: 3, snoozedUntil: '2026-06-18T12:00:00Z' })
  })
})
