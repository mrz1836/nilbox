import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { deferred } from '../test/async'
import { platform } from '../test/platform'
import { copiedMs, copyLabel, useCopy, type Copy } from './useCopy'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useCopy', () => {
  it('copies a text, says so for copiedMs, then goes back to idle', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const seen: Copy[] = []
    const { result } = renderHook(() => {
      const copy = useCopy('path')

      seen.push(copy.copy)

      return copy
    })

    expect(result.current.copy).toBe('idle')

    act(() => {
      result.current.onCopy('~/projects/example-repo')
    })

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('~/projects/example-repo')
    await waitFor(() => {
      expect(result.current.copy).toBe('copied')
    })

    act(() => {
      vi.advanceTimersByTime(copiedMs)
    })

    expect(result.current.copy).toBe('idle')
    // A text in hand has nothing to read first, so the copy is never copying.
    expect(seen).not.toContain('copying')
  })

  it('is copying while it reads the text, then copies what it read', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const read = deferred<string>()
    const { result } = renderHook(() => useCopy('log'))

    act(() => {
      result.current.onCopy(() => read.promise)
    })

    expect(result.current.copy).toBe('copying')
    expect(platform.copyText).not.toHaveBeenCalled()

    // Copying lasts as long as the read does, however long that is.
    act(() => {
      vi.advanceTimersByTime(copiedMs)
    })
    expect(result.current.copy).toBe('copying')

    await act(async () => {
      read.resolve('lint passed\n')
      await read.promise
    })

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('lint passed\n')
    await waitFor(() => {
      expect(result.current.copy).toBe('copied')
    })

    act(() => {
      vi.advanceTimersByTime(copiedMs)
    })

    expect(result.current.copy).toBe('idle')
  })

  it('fails a copy whose read fails, and logs why, copying nothing', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const failure = new Error('the log is gone')
    const { result } = renderHook(() => useCopy('log'))

    act(() => {
      result.current.onCopy(() => Promise.reject(failure))
    })

    await waitFor(() => {
      expect(result.current.copy).toBe('failed')
    })
    expect(platform.copyText).not.toHaveBeenCalled()
    expect(error).toHaveBeenCalledExactlyOnceWith('could not copy the log', failure)
  })

  it('fails a copy the clipboard refuses, and logs why', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const refused = new Error('denied')
    platform.copyText.mockRejectedValueOnce(refused)
    const { result } = renderHook(() => useCopy('tag'))

    act(() => {
      result.current.onCopy(() => Promise.resolve('v1.4.3'))
    })

    await waitFor(() => {
      expect(result.current.copy).toBe('failed')
    })
    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('v1.4.3')
    expect(error).toHaveBeenCalledExactlyOnceWith('could not copy the tag', refused)
  })

  it('says each state in words', () => {
    expect(copyLabel).toEqual({ idle: 'Copy', copying: 'Copying…', copied: 'Copied', failed: 'Copy failed' })
  })
})
