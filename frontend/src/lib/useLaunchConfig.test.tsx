import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { defaultPrompts, item, itemResolution, list, prompts, replyPrompts, settings, snapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { useLaunchConfig } from './useLaunchConfig'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

beforeEach(() => {
  platform.resolveLaunches.mockResolvedValue([itemResolution()])
})

// reloaded asks the hook to reload and waits until the new values are in. The reload's
// request starts once React renders, so it is awaited after the act that asked for it.
async function reloaded(reload: () => Promise<void>) {
  let done: Promise<void> = Promise.resolve()

  act(() => {
    done = reload()
  })
  await act(() => done)
}

describe('useLaunchConfig', () => {
  it('loads the configuration and resolves each listed item once', async () => {
    const shared = item()
    const inbox = snapshot({ reviewRequested: list([shared]), assigned: list([shared, item({ id: 2 })]) })
    const { result } = renderHook(() => useLaunchConfig(inbox))

    await waitFor(() => {
      expect(result.current.resolutions.get(1)).toEqual(itemResolution())
    })

    expect(result.current.loaded).toBe(true)
    expect(result.current.settings).toEqual(settings())
    expect(result.current.prompts).toHaveLength(5)
    expect(result.current.defaultPrompts).toEqual(defaultPrompts())
    expect(result.current.items.map((i) => i.id)).toEqual([1, 2])
    expect(platform.resolveLaunches).toHaveBeenCalledWith([1, 2])
  })

  it('keeps the review replies apart from the prompts a launch can use, in the library’s order', async () => {
    const replies = replyPrompts()
    platform.listPrompts.mockResolvedValue([
      ...replies.slice(3),
      ...prompts().slice(0, 2),
      ...replies.slice(0, 1),
      ...prompts().slice(2),
      ...replies.slice(1, 3),
    ])
    const { result } = renderHook(() => useLaunchConfig(snapshot()))

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })

    expect(result.current.prompts.map((p) => p.id)).toEqual(prompts().map((p) => p.id))
    expect(result.current.replies.map((p) => p.nickname)).toEqual(['Question', 'LGTM', 'Update OK', 'Needs changes'])
  })

  it('shows the same empty lists until the configuration loads', () => {
    platform.listPrompts.mockReturnValue(new Promise(() => undefined))
    const { result, rerender } = renderHook(() => useLaunchConfig(snapshot()))
    const first = result.current

    rerender()

    expect(first.replies).toEqual([])
    expect(result.current.replies).toBe(first.replies)
    expect(result.current.prompts).toBe(first.prompts)
  })

  it('resolves nothing for an empty inbox', async () => {
    const { result } = renderHook(() => useLaunchConfig(snapshot()))

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })

    expect(result.current.resolutions.size).toBe(0)
    expect(platform.resolveLaunches).not.toHaveBeenCalled()
  })

  it('reloads and re-resolves on request, and after settings change', async () => {
    const inbox: InboxSnapshot = snapshot({ reviewRequested: list([item()]) })
    const { result } = renderHook(() => useLaunchConfig(inbox))

    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledOnce()
    })

    platform.listPrompts.mockResolvedValue(prompts().slice(0, 2))

    await reloaded(result.current.reload)

    expect(result.current.prompts).toHaveLength(2)
    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledTimes(2)
    })

    act(() => {
      result.current.setSettings(settings({ worktreeDefault: false }))
    })

    expect(result.current.settings?.worktreeDefault).toBe(false)
    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledTimes(3)
    })
  })

  it('re-resolves after the Codex effort or Fast mode setting changes', async () => {
    const { result } = renderHook(() => useLaunchConfig(snapshot({ reviewRequested: list([item()]) })))

    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledOnce()
    })

    act(() => {
      result.current.setSettings(settings({ codexEffort: true }))
    })

    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledTimes(2)
    })

    act(() => {
      result.current.setSettings(settings({ codexEffort: true, codexFast: true }))
    })

    await waitFor(() => {
      expect(platform.resolveLaunches).toHaveBeenCalledTimes(3)
    })

    act(() => {
      result.current.setSettings(settings({ codexEffort: true, codexFast: true, menuBar: false }))
    })

    expect(result.current.settings?.menuBar).toBe(false)
    expect(platform.resolveLaunches).toHaveBeenCalledTimes(3)
  })

  it('reports a configuration or resolution that could not load', async () => {
    platform.getSettings.mockRejectedValueOnce(new Error('database is locked'))
    const { result } = renderHook(() => useLaunchConfig(snapshot({ reviewRequested: list([item()]) })))

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.settings).toBeNull()

    platform.resolveLaunches.mockRejectedValueOnce(new Error('item not found'))

    await reloaded(result.current.reload)

    await waitFor(() => {
      expect(result.current.error).toBe('item not found')
    })

    platform.listPrompts.mockRejectedValueOnce(new Error('read the prompts: busy'))

    await reloaded(result.current.reload)

    expect(result.current.error).toBe('read the prompts: busy')
  })
})
