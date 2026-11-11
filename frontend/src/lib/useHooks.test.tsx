import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ScriptRun } from '../platform'
import { hookRepo, hooksList, scriptRun } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { useHooks } from './useHooks'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// renderHooks reads the list with runs, which a rerender changes.
function renderHooks(enabled = true, runs: readonly ScriptRun[] | null = []) {
  return renderHook(({ on, listed }) => useHooks(on, listed), { initialProps: { on: enabled, listed: runs } })
}

describe('useHooks', () => {
  it('reads the list, and again whenever the shell says a clone was scanned, until unmounted', async () => {
    const { result, unmount } = renderHooks()

    await waitFor(() => {
      expect(result.current.list?.repos).toHaveLength(6)
    })
    expect(platform.listHooks).toHaveBeenCalledExactlyOnceWith(false)

    platform.listHooks.mockResolvedValue(hooksList({ repos: [hookRepo()] }))
    act(() => {
      emit('hooks:updated')
    })

    await waitFor(() => {
      expect(result.current.list?.repos).toHaveLength(1)
    })
    expect(platform.listHooks).toHaveBeenLastCalledWith(false)

    expect(listening('hooks:updated')).toBe(1)
    unmount()
    expect(listening('hooks:updated')).toBe(0)
  })

  it('reads the list again when a run starts or ends in a clone, and not for any other change', async () => {
    const { result, rerender } = renderHooks(true, null)

    await waitFor(() => {
      expect(result.current.list).not.toBeNull()
    })

    // The runs listed, none going: nothing to read again.
    rerender({ on: true, listed: [scriptRun()] })
    // A run starts in a clone.
    rerender({ on: true, listed: [scriptRun({ id: 2, status: 'running', dir: '~/projects/web' }), scriptRun()] })
    await waitFor(() => {
      expect(platform.listHooks).toHaveBeenCalledTimes(2)
    })

    // Its status changes, but it still holds the same clone.
    rerender({ on: true, listed: [scriptRun({ id: 2, status: 'orphaned', dir: '~/projects/web' }), scriptRun()] })
    await act(async () => {
      await Promise.resolve()
    })
    expect(platform.listHooks).toHaveBeenCalledTimes(2)

    // It ends.
    rerender({ on: true, listed: [scriptRun({ id: 2, dir: '~/projects/web' }), scriptRun()] })

    await waitFor(() => {
      expect(platform.listHooks).toHaveBeenCalledTimes(3)
    })
    expect(platform.listHooks.mock.calls.every(([refresh]) => !refresh)).toBe(true)
  })

  it('reads nothing, and listens for nothing, while it isn’t enabled', () => {
    const { result } = renderHooks(false)

    expect(result.current.list).toBeNull()
    expect(platform.listHooks).not.toHaveBeenCalled()
    expect(listening('hooks:updated')).toBe(0)
  })

  it('scans every clone again on request, and says why when that fails', async () => {
    const { result } = renderHooks()

    await waitFor(() => {
      expect(result.current.list).not.toBeNull()
    })

    platform.listHooks.mockResolvedValue(hooksList({ repos: [] }))
    await act(() => result.current.refresh())

    expect(platform.listHooks).toHaveBeenLastCalledWith(true)
    expect(result.current.list?.repos).toEqual([])
    expect(result.current.refreshing).toBe(false)

    platform.listHooks.mockRejectedValue(new Error('the clone root is gone'))
    await act(() => result.current.refresh())

    expect(result.current.refreshError).toBe('the clone root is gone')
    expect(result.current.list?.repos).toEqual([])
  })

  it('says why the list couldn’t be read at all', async () => {
    platform.listHooks.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHooks()

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.list).toBeNull()
  })
})
