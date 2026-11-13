import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { usePickReply } from './usePickReply'

// later is a fill whose answer for each pick the test gives when it chooses.
function later() {
  const answers: { resolve: (text: string) => void; reject: (err: Error) => void }[] = []
  const fill = vi.fn(
    () =>
      new Promise<string>((resolve, reject) => {
        answers.push({ resolve, reject })
      }),
  )

  return { fill, answers }
}

describe('usePickReply', () => {
  it('uses the latest pick’s text alone', async () => {
    const { fill, answers } = later()
    const use = vi.fn()
    const { result } = renderHook(() => usePickReply(1, fill, use))

    act(() => {
      result.current.pick(31)
      result.current.pick(32)
    })
    expect(fill).toHaveBeenLastCalledWith(1, 32)

    await act(async () => {
      answers[1]?.resolve('Thanks!')
      answers[0]?.resolve('Looking into it.')
      await Promise.resolve()
    })

    expect(use).toHaveBeenCalledExactlyOnceWith('Thanks!', 32)
  })

  it('drops a pick the user typed over', async () => {
    const { fill, answers } = later()
    const use = vi.fn()
    const { result } = renderHook(() => usePickReply(1, fill, use))

    act(() => {
      result.current.pick(31)
      result.current.drop()
    })

    await act(async () => {
      answers[0]?.resolve('Looking into it.')
      await Promise.resolve()
    })

    expect(use).not.toHaveBeenCalled()
  })

  it('says why a pick failed, for its own item alone', async () => {
    const { fill, answers } = later()
    const { result, rerender } = renderHook(({ id }) => usePickReply(id, fill, vi.fn()), {
      initialProps: { id: 1 },
    })

    act(() => {
      result.current.pick(31)
    })

    await act(async () => {
      answers[0]?.reject(new Error('the reply is gone'))
      await Promise.resolve()
    })

    expect(result.current.error).toBe('the reply is gone')

    rerender({ id: 2 })
    expect(result.current.error, 'another item').toBe('')

    rerender({ id: 1 })
    act(() => {
      result.current.pick(32)
    })
    expect(result.current.error, 'a new pick clears it').toBe('')
  })
})
