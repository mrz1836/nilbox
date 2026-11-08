import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Result } from './call'
import { useMutation } from './useMutation'

// deferred is a write the test settles.
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

describe('useMutation', () => {
  it('is pending while the write runs, and resolves with its value', async () => {
    const write = deferred<string>()
    const mutate = vi.fn<(id: number) => Promise<string>>(() => write.promise)
    const { result } = renderHook(() => useMutation(mutate))

    let done: Promise<Result<string>> = Promise.resolve({ ok: true, data: '' })
    act(() => {
      done = result.current.run(7)
    })

    expect(mutate).toHaveBeenCalledExactlyOnceWith(7)
    expect(result.current.pending).toBe(true)

    await act(async () => {
      write.resolve('stored')
      expect(await done).toEqual({ ok: true, data: 'stored' })
    })

    expect(result.current).toMatchObject({ pending: false, error: '' })
  })

  it('reports a failure, never rejecting, and clears it on reset or the next write', async () => {
    const mutate = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error('database is locked'))
      .mockRejectedValueOnce(new Error('still locked'))
      .mockResolvedValue('stored')
    const { result } = renderHook(() => useMutation(mutate))

    await act(async () => {
      expect(await result.current.run()).toEqual({ ok: false, error: 'database is locked' })
    })

    expect(result.current).toMatchObject({ pending: false, error: 'database is locked' })

    act(() => {
      result.current.reset()
    })

    expect(result.current.error).toBe('')

    await act(async () => {
      await result.current.run()
    })

    expect(result.current.error).toBe('still locked')

    await act(async () => {
      await result.current.run()
    })

    expect(result.current.error).toBe('')
  })

  it('reports a write that throws before it starts as a failure', async () => {
    const { result } = renderHook(() =>
      useMutation((): Promise<void> => {
        throw new Error('no backend')
      }),
    )

    await act(async () => {
      expect(await result.current.run()).toEqual({ ok: false, error: 'no backend' })
    })
  })

  it('follows the latest of overlapping writes', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const mutate = vi.fn<() => Promise<string>>().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { result } = renderHook(() => useMutation(mutate))

    act(() => {
      void result.current.run()
      void result.current.run()
    })

    // The first write's failure is no longer news once a later write is under way.
    await act(async () => {
      first.reject(new Error('database is locked'))
      await first.promise.catch(() => undefined)
    })

    expect(result.current).toMatchObject({ pending: true, error: '' })

    await act(async () => {
      second.resolve('stored')
      await second.promise
    })

    expect(result.current.pending).toBe(false)
  })

  it('sets nothing once the component has gone, yet still resolves', async () => {
    const write = deferred<string>()
    const { result, unmount } = renderHook(() => useMutation(() => write.promise))

    let done: Promise<Result<string>> = Promise.resolve({ ok: true, data: '' })
    act(() => {
      done = result.current.run()
    })
    unmount()

    write.resolve('stored')

    expect(await done).toEqual({ ok: true, data: 'stored' })
    expect(result.current.pending).toBe(true)
  })
})
