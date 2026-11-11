import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { scriptRun } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { useScriptRuns } from './useScriptRuns'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useScriptRuns', () => {
  it('lists every run, and again whenever they change, until unmounted', async () => {
    const { result, unmount } = renderHook(() => useScriptRuns())

    await waitFor(() => {
      expect(result.current.runs?.map((r) => r.id)).toEqual([1])
    })
    expect(platform.listScriptRuns).toHaveBeenLastCalledWith({ kinds: [], repo: '' })

    platform.listScriptRuns.mockResolvedValue([scriptRun({ id: 2 }), scriptRun()])
    act(() => {
      emit('scriptruns:updated')
    })

    await waitFor(() => {
      expect(result.current.runs?.map((r) => r.id)).toEqual([2, 1])
    })
    expect(platform.listScriptRuns).toHaveBeenCalledTimes(2)

    expect(listening('scriptruns:updated')).toBe(1)
    unmount()
    expect(listening('scriptruns:updated')).toBe(0)
  })

  it('never lists again for one run’s progress, which changes nothing it lists', async () => {
    const { result } = renderHook(() => useScriptRuns())

    await waitFor(() => {
      expect(result.current.runs).not.toBeNull()
    })

    expect(listening('scriptruns:progress')).toBe(0)
  })

  it('lists nothing, and listens for nothing, while it isn’t enabled', () => {
    const { result } = renderHook(() => useScriptRuns(false))

    expect(result.current.runs).toBeNull()
    expect(platform.listScriptRuns).not.toHaveBeenCalled()
    expect(listening('scriptruns:updated')).toBe(0)
  })

  it('lists them again when asked', async () => {
    const { result } = renderHook(() => useScriptRuns())

    await waitFor(() => {
      expect(result.current.runs).not.toBeNull()
    })

    platform.listScriptRuns.mockResolvedValue([])
    let reloaded = Promise.resolve()

    act(() => {
      reloaded = result.current.reload()
    })
    await reloaded

    await waitFor(() => {
      expect(result.current.runs).toEqual([])
    })
    expect(platform.listScriptRuns).toHaveBeenCalledTimes(2)
  })

  it('says why the runs could not be listed', async () => {
    platform.listScriptRuns.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHook(() => useScriptRuns())

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.runs).toBeNull()
  })
})
