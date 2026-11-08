import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../platform'
import { settings } from '../test/fixtures'
import type { Result } from './call'
import { useSettingsSave } from './useSettingsSave'

// deferred is a setter call the test settles.
function deferred() {
  let resolve: (value: Settings) => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<Settings>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

describe('useSettingsSave', () => {
  it('shows a change at once, then hands on the settings the setter returned', async () => {
    const call = deferred()
    const onSaved = vi.fn()
    const { result } = renderHook(() => useSettingsSave(settings({ menuBar: true }), onSaved))

    let saved: Promise<Result<Settings>> = Promise.resolve({ ok: false, error: '' })
    act(() => {
      saved = result.current.save({ menuBar: false }, () => call.promise)
    })

    expect(result.current.settings.menuBar).toBe(false)
    expect(onSaved).not.toHaveBeenCalled()

    const stored = settings({ menuBar: false, snoozeDays: 7 })
    await act(async () => {
      call.resolve(stored)
      expect(await saved).toEqual({ ok: true, data: stored })
    })

    expect(onSaved).toHaveBeenCalledExactlyOnceWith(stored)
    expect(result.current.error).toBe('')
  })

  it('goes back to the stored settings, and says why, when a change is refused', async () => {
    const call = deferred()
    const onSaved = vi.fn()
    const { result } = renderHook(() => useSettingsSave(settings({ menuBar: true }), onSaved))

    let saved: Promise<Result<Settings>> = Promise.resolve({ ok: true, data: settings() })
    act(() => {
      saved = result.current.save({ menuBar: false }, () => call.promise)
    })

    expect(result.current.settings.menuBar).toBe(false)

    await act(async () => {
      call.reject(new Error('database is locked'))
      expect(await saved).toEqual({ ok: false, error: 'database is locked' })
    })

    expect(result.current.settings.menuBar).toBe(true)
    expect(result.current.error).toBe('database is locked')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('stores changes one at a time, in the order they were made', async () => {
    const first = deferred()
    const second = deferred()
    const setter = vi.fn<(n: number) => Promise<Settings>>((n) => (n === 1 ? first.promise : second.promise))
    const onSaved = vi.fn()
    const { result } = renderHook(() => useSettingsSave(settings(), onSaved))

    let done: Promise<Result<Settings>>[] = []
    act(() => {
      done = [
        result.current.save({ snoozeDays: 1 }, () => setter(1)),
        result.current.save({ snoozeDays: 2 }, () => setter(2)),
      ]
    })

    await act(async () => {
      await Promise.resolve()
    })

    expect(setter).toHaveBeenCalledExactlyOnceWith(1)
    expect(result.current.settings.snoozeDays).toBe(2)

    await act(async () => {
      first.resolve(settings({ snoozeDays: 1 }))
      await done[0]
    })

    expect(setter).toHaveBeenLastCalledWith(2)

    await act(async () => {
      second.resolve(settings({ snoozeDays: 2 }))
      await done[1]
    })

    expect(onSaved.mock.calls).toEqual([[settings({ snoozeDays: 1 })], [settings({ snoozeDays: 2 })]])
  })
})
