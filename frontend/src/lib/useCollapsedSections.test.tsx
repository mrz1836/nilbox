import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ListKey, Settings } from '../platform'
import { settings } from '../test/fixtures'
import { platform } from '../test/platform'
import { useCollapsedSections } from './useCollapsedSections'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// loaded renders the hook and waits for the stored state.
async function loaded() {
  const view = renderHook(() => useCollapsedSections())

  await waitFor(() => {
    expect(view.result.current.collapsed).not.toBeNull()
  })

  return view
}

describe('useCollapsedSections', () => {
  it('is null until the stored state loads, then holds it', async () => {
    platform.getCollapsedSections.mockResolvedValue(['authored', 'assigned'])
    const { result } = renderHook(() => useCollapsedSections())

    expect(result.current.collapsed).toBeNull()

    await waitFor(() => {
      expect(result.current.collapsed).toEqual(new Set(['authored', 'assigned']))
    })

    expect(platform.getCollapsedSections).toHaveBeenCalledOnce()
  })

  it('collapses and opens a section, storing each change', async () => {
    const { result } = await loaded()

    act(() => {
      result.current.setCollapsed('authored', true)
    })

    expect(result.current.collapsed).toEqual(new Set(['authored']))

    act(() => {
      result.current.setCollapsed('reviewRequested', true)
      result.current.setCollapsed('authored', false)
    })

    expect(result.current.collapsed).toEqual(new Set(['reviewRequested']))

    await waitFor(() => {
      expect(platform.setSectionCollapsed).toHaveBeenCalledTimes(3)
    })

    expect(platform.setSectionCollapsed.mock.calls).toEqual([
      ['authored', true],
      ['reviewRequested', true],
      ['authored', false],
    ])
  })

  it('stores changes one at a time, in the order they were made', async () => {
    const { result } = await loaded()
    const finish: (() => void)[] = []
    const started: [string, boolean][] = []

    platform.setSectionCollapsed.mockImplementation((section, on) => {
      started.push([section, on])

      return new Promise<Settings>((resolve) => {
        finish.push(() => {
          resolve(settings())
        })
      })
    })

    act(() => {
      result.current.setCollapsed('assigned', true)
      result.current.setCollapsed('assigned', false)
    })

    await waitFor(() => {
      expect(started).toEqual([['assigned', true]])
    })

    finish[0]?.()

    await waitFor(() => {
      expect(started).toEqual([
        ['assigned', true],
        ['assigned', false],
      ])
    })
  })

  it('keeps a change for the session when it cannot be stored, and stores the next one', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const { result } = await loaded()

    platform.setSectionCollapsed.mockRejectedValueOnce(new Error('database is locked'))

    act(() => {
      result.current.setCollapsed('authored', true)
      result.current.setCollapsed('assigned', true)
    })

    expect(result.current.collapsed).toEqual(new Set(['authored', 'assigned']))

    await waitFor(() => {
      expect(platform.setSectionCollapsed).toHaveBeenCalledWith('assigned', true)
    })

    expect(logged).toHaveBeenCalledWith(
      'could not store whether the authored section is collapsed',
      new Error('database is locked'),
    )
  })

  it('opens every section when the stored state cannot be read', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getCollapsedSections.mockRejectedValue(new Error('database is locked'))

    const { result } = await loaded()

    expect(result.current.collapsed).toEqual(new Set())
    expect(logged).toHaveBeenCalledWith('could not read the collapsed inbox sections', new Error('database is locked'))
  })

  it('ignores a load that finishes after unmounting', async () => {
    let resolve: (sections: ListKey[]) => void = () => undefined
    platform.getCollapsedSections.mockReturnValue(
      new Promise<ListKey[]>((r) => {
        resolve = r
      }),
    )

    const { result, unmount } = renderHook(() => useCollapsedSections())
    unmount()

    await act(async () => {
      resolve(['authored'])
      await Promise.resolve()
    })

    expect(result.current.collapsed).toBeNull()
  })

  it('ignores a failed load that finishes after unmounting', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    let reject: (err: Error) => void = () => undefined
    platform.getCollapsedSections.mockReturnValue(
      new Promise<ListKey[]>((_, r) => {
        reject = r
      }),
    )

    const { result, unmount } = renderHook(() => useCollapsedSections())
    unmount()

    await act(async () => {
      reject(new Error('database is locked'))
      await Promise.resolve()
    })

    expect(result.current.collapsed).toBeNull()
  })
})
