import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useDebouncedQuery, useQuery } from './useQuery'

// Call is a request the test answers, which records whether it was cancelled, as a bound
// call's CancellablePromise does.
interface Call<T> extends Promise<T> {
  cancel: () => void
  cancelled: boolean
  resolve: (value: T) => void
  reject: (err: unknown) => void
}

function call<T>(): Call<T> {
  let resolve: (value: T) => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  const made = Object.assign(promise, {
    cancelled: false,
    resolve,
    reject,
    cancel: () => {
      made.cancelled = true
      reject(new Error('cancelled'))
    },
  })

  return made
}

// calls hands out a fresh call per request and keeps them in order.
function calls<T>() {
  const made: Call<T>[] = []
  const load = vi.fn<(...inputs: unknown[]) => Call<T>>(() => {
    const next = call<T>()
    made.push(next)

    return next
  })

  return { made, load }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useQuery', () => {
  it('is pending until the answer arrives, then holds it', async () => {
    const { made, load } = calls<string>()
    const { result } = renderHook(() => useQuery(load, []))

    expect(result.current.pending).toBe(true)
    expect(result.current.data).toBeUndefined()

    await act(async () => {
      made[0]?.resolve('first')
      await made[0]
    })

    expect(result.current).toMatchObject({ pending: false, data: 'first', error: '' })
    expect(load).toHaveBeenCalledOnce()
  })

  it('asks again when an input changes, cancelling the request it replaces', async () => {
    const { made, load } = calls<string>()
    const { result, rerender } = renderHook(({ id }) => useQuery(() => load(id), [id]), { initialProps: { id: 1 } })

    rerender({ id: 2 })

    expect(load).toHaveBeenLastCalledWith(2)
    expect(made[0]?.cancelled).toBe(true)

    // The replaced request's answer never shows, even when it arrives.
    await act(async () => {
      made[1]?.resolve('second')
      await made[1]
    })

    expect(result.current.data).toBe('second')
  })

  it('never shows a replaced answer that cannot be cancelled', async () => {
    let answer: (value: string) => void = () => undefined
    const slow = new Promise<string>((resolve) => {
      answer = resolve
    })
    const load = vi.fn<(id: number) => Promise<string>>().mockReturnValueOnce(slow).mockResolvedValue('newer')
    const { result, rerender } = renderHook(({ id }) => useQuery(() => load(id), [id]), { initialProps: { id: 1 } })

    rerender({ id: 2 })
    await waitFor(() => {
      expect(result.current.data).toBe('newer')
    })

    await act(async () => {
      answer('older')
      await slow
    })

    expect(result.current.data).toBe('newer')
  })

  it('cancels the request under way when the component goes, and sets nothing after', () => {
    const { made, load } = calls<string>()
    const { result, unmount } = renderHook(() => useQuery(load, []))

    unmount()

    expect(made[0]?.cancelled).toBe(true)
    expect(result.current.data).toBeUndefined()
  })

  it('reports a failure, keeping the data, and clears it on the next answer', async () => {
    const { made, load } = calls<string>()
    const { result } = renderHook(() => useQuery(load, []))

    await act(async () => {
      made[0]?.resolve('kept')
      await made[0]
    })

    let refetched: Promise<void> = Promise.resolve()
    act(() => {
      refetched = result.current.refetch()
    })
    await act(async () => {
      made[1]?.reject(new Error('database is locked'))
      await refetched
    })

    expect(result.current).toMatchObject({ pending: false, data: 'kept', error: 'database is locked' })

    act(() => {
      refetched = result.current.refetch()
    })
    await act(async () => {
      made[2]?.resolve('fresh')
      await refetched
    })

    expect(result.current).toMatchObject({ data: 'fresh', error: '' })
  })

  it('logs a failure it is asked to log', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const failure = new Error('database is locked')
    const { result } = renderHook(() => useQuery(() => Promise.reject(failure), [], { log: 'could not read it' }))

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(logged).toHaveBeenCalledExactlyOnceWith('could not read it', failure)
  })

  it('refetches on request, resolving once the new answer is in', async () => {
    const load = vi.fn<() => Promise<number>>().mockResolvedValueOnce(1).mockResolvedValueOnce(2)
    const { result } = renderHook(() => useQuery(load, []))

    await waitFor(() => {
      expect(result.current.data).toBe(1)
    })

    // The request starts once React renders, so the refetch is awaited after the act that
    // made it.
    let refetched: Promise<void> = Promise.resolve()
    act(() => {
      refetched = result.current.refetch()
    })
    await act(() => refetched)

    expect(result.current.data).toBe(2)
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('asks nothing while disabled, keeping the data, and asks once enabled', async () => {
    const load = vi.fn(() => Promise.resolve('asked'))
    const { result, rerender } = renderHook(({ enabled }) => useQuery(load, [], { enabled }), {
      initialProps: { enabled: false },
    })

    expect(result.current.pending).toBe(false)

    let refetched: Promise<void> = Promise.resolve()
    act(() => {
      refetched = result.current.refetch()
    })
    await act(() => refetched)
    expect(load).not.toHaveBeenCalled()

    rerender({ enabled: true })

    await waitFor(() => {
      expect(result.current.data).toBe('asked')
    })
  })

  it('lets go of a refetch still waiting when the component goes', async () => {
    const { load } = calls<string>()
    const { result, unmount } = renderHook(() => useQuery(load, []))

    let refetched: Promise<void> = Promise.resolve()
    act(() => {
      refetched = result.current.refetch()
    })
    unmount()

    await expect(refetched).resolves.toBeUndefined()
  })

  it('replaces the data as a write leaves it, without asking', async () => {
    const load = vi.fn(() => Promise.resolve(['a']))
    const { result } = renderHook(() => useQuery(load, []))

    await waitFor(() => {
      expect(result.current.data).toEqual(['a'])
    })

    act(() => {
      result.current.set((was) => [...(was ?? []), 'b'])
    })

    expect(result.current.data).toEqual(['a', 'b'])
    expect(load).toHaveBeenCalledOnce()
  })
})

describe('useDebouncedQuery', () => {
  it('asks once the inputs rest for the delay, and only for the latest', async () => {
    vi.useFakeTimers()
    const load = vi.fn((text: string) => Promise.resolve(text.toUpperCase()))
    const { result, rerender } = renderHook(({ text }) => useDebouncedQuery(() => load(text), [text], 150), {
      initialProps: { text: 'a' },
    })

    expect(result.current).toMatchObject({ pending: true, waiting: true })

    act(() => {
      vi.advanceTimersByTime(100)
    })
    rerender({ text: 'ab' })
    act(() => {
      vi.advanceTimersByTime(149)
    })

    expect(load).not.toHaveBeenCalled()
    expect(result.current.waiting).toBe(true)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(load).toHaveBeenCalledExactlyOnceWith('ab')
    expect(result.current).toMatchObject({ pending: false, waiting: false, data: 'AB' })
  })

  it('stops waiting once it asks, while the answer is still on its way', async () => {
    vi.useFakeTimers()
    const { made, load } = calls<string>()
    const { result } = renderHook(() => useDebouncedQuery(load, [], 150))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150)
    })

    expect(load).toHaveBeenCalledOnce()
    expect(result.current).toMatchObject({ pending: true, waiting: false })

    await act(async () => {
      made[0]?.resolve('done')
      await made[0]
    })

    expect(result.current).toMatchObject({ pending: false, waiting: false, data: 'done' })
  })

  it('cancels the request under way when an input changes', async () => {
    vi.useFakeTimers()
    const { made, load } = calls<string>()
    const { rerender } = renderHook(({ text }) => useDebouncedQuery(() => load(text), [text], 150), {
      initialProps: { text: 'a' },
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(150)
    })
    expect(load).toHaveBeenCalledOnce()

    rerender({ text: 'ab' })

    expect(made[0]?.cancelled).toBe(true)
    expect(load).toHaveBeenCalledOnce()
  })
})
