import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ManifestsPage } from '../platform'
import { deferred } from '../test/async'
import { manifestsClone, manifestsPage } from '../test/fixtures'
import { listening, platform, send } from '../test/platform'
import { useManifests } from './useManifests'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// renderManifests reads the clones while enabled, which a rerender changes.
function renderManifests(enabled = true) {
  return renderHook(({ on }) => useManifests(on), { initialProps: { on: enabled } })
}

// held makes the next read of the clones wait until the test answers it, as a read of
// every clone does, and returns the answer.
function held(): (page: ManifestsPage) => void {
  const d = deferred<ManifestsPage>()

  platform.listManifests.mockImplementationOnce(() => d.promise)

  return d.resolve
}

describe('useManifests', () => {
  it('reads the clones, without asking for every one to be read again', async () => {
    const { result } = renderManifests()

    expect(result.current.page).toBeNull()

    await waitFor(() => {
      expect(result.current.page).toEqual(manifestsPage())
    })
    expect(platform.listManifests).toHaveBeenCalledExactlyOnceWith(false)
    expect(result.current.error).toBe('')
    expect(result.current.progress).toBeNull()
  })

  it('says how far a read has got only while one runs', async () => {
    const answer = held()
    const { result } = renderManifests()

    expect(result.current.progress).toBeNull()

    send('manifests:progress', { phase: 'clones', done: 0, total: 0 })
    expect(result.current.progress).toEqual({ phase: 'clones', done: 0, total: 0 })

    send('manifests:progress', { phase: 'reading', done: 3, total: 12 })
    expect(result.current.progress).toEqual({ phase: 'reading', done: 3, total: 12 })

    act(() => {
      answer(manifestsPage())
    })

    await waitFor(() => {
      expect(result.current.page).not.toBeNull()
    })
    expect(result.current.progress).toBeNull()
  })

  it('reads every clone again on request, from no progress, and says why when that fails', async () => {
    const { result } = renderManifests()

    await waitFor(() => {
      expect(result.current.page).not.toBeNull()
    })
    send('manifests:progress', { phase: 'reading', done: 12, total: 12 })

    const answer = held()
    let refreshing: Promise<void> = Promise.resolve()

    act(() => {
      refreshing = result.current.refresh()
    })

    expect(platform.listManifests).toHaveBeenLastCalledWith(true)
    expect(result.current.refreshing).toBe(true)
    // What was heard of the read before says nothing of this one.
    expect(result.current.progress).toBeNull()

    send('manifests:progress', { phase: 'reading', done: 1, total: 12 })
    expect(result.current.progress).toEqual({ phase: 'reading', done: 1, total: 12 })

    const fresh = manifestsPage({ clones: [manifestsClone({ repo: 'acme-org/cli', dir: '~/projects/cli' })] })

    await act(async () => {
      answer(fresh)
      await refreshing
    })

    expect(result.current.page).toEqual(fresh)
    expect(result.current.refreshing).toBe(false)
    expect(result.current.progress).toBeNull()

    platform.listManifests.mockRejectedValue(new Error('the clone root is gone'))
    await act(() => result.current.refresh())

    expect(result.current.refreshError).toBe('the clone root is gone')
    expect(result.current.page).toEqual(fresh)
  })

  it('says why the clones couldn’t be read at all', async () => {
    platform.listManifests.mockRejectedValue(new Error('database is locked'))
    const { result } = renderManifests()

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.page).toBeNull()
  })

  it('stops listening while it isn’t enabled, and forgets what it heard', async () => {
    const first = held()
    const { result, rerender } = renderManifests()

    send('manifests:progress', { phase: 'reading', done: 3, total: 12 })
    expect(result.current.progress).toEqual({ phase: 'reading', done: 3, total: 12 })
    expect(listening('manifests:progress')).toBe(1)

    rerender({ on: false })

    expect(listening('manifests:progress')).toBe(0)
    expect(result.current.progress).toBeNull()

    const second = held()

    rerender({ on: true })

    // Listening again, the read it waits for shows nothing until it says how far it has got.
    expect(listening('manifests:progress')).toBe(1)
    expect(result.current.progress).toBeNull()

    act(() => {
      first(manifestsPage())
      second(manifestsPage())
    })

    await waitFor(() => {
      expect(result.current.page).toEqual(manifestsPage())
    })
    expect(platform.listManifests).toHaveBeenCalledTimes(2)
  })

  it('reads nothing at all while it starts disabled', () => {
    const { result } = renderManifests(false)

    expect(result.current.page).toBeNull()
    expect(platform.listManifests).not.toHaveBeenCalled()
    expect(listening('manifests:progress')).toBe(0)
  })
})
