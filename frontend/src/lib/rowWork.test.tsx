import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { deferred } from '../test/async'
import { useRowWork } from './rowWork'

describe('useRowWork', () => {
  it('marks the row busy until the work settles, and starts nothing more meanwhile', async () => {
    const work = deferred()
    const first = vi.fn(() => work.promise)
    const second = vi.fn(() => Promise.resolve())
    const { result } = renderHook(() => useRowWork())

    expect(result.current[0]).toBeNull()

    act(() => {
      result.current[1]('unassign', first)
      result.current[1]('refresh', second)
    })

    expect(result.current[0]).toBe('unassign')
    expect(first).toHaveBeenCalledOnce()
    expect(second).not.toHaveBeenCalled()

    await act(async () => {
      work.resolve()
      await Promise.resolve()
    })

    expect(result.current[0]).toBeNull()

    await act(async () => {
      result.current[1]('refresh', second)
      await Promise.resolve()
    })

    expect(second).toHaveBeenCalledOnce()
  })
})
