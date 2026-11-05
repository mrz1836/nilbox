import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { item, itemResolution, list, profile, prompts, settings, snapshot, variables } from '../test/fixtures'
import { useLaunchConfig } from './useLaunchConfig'

const platform = vi.hoisted(() => ({
  getSettings: vi.fn(),
  listProfiles: vi.fn(),
  listPrompts: vi.fn(),
  getDefaultPrompts: vi.fn(),
  listPromptVariables: vi.fn(),
  listLaunchDefaults: vi.fn(),
  resolveLaunches: vi.fn(),
}))

vi.mock('../platform', () => platform)

beforeEach(() => {
  vi.clearAllMocks()
  platform.getSettings.mockResolvedValue(settings())
  platform.listProfiles.mockResolvedValue([profile()])
  platform.listPrompts.mockResolvedValue(prompts())
  platform.getDefaultPrompts.mockResolvedValue({ pr: 1, issue: 4 })
  platform.listPromptVariables.mockResolvedValue(variables())
  platform.listLaunchDefaults.mockResolvedValue([])
  platform.resolveLaunches.mockResolvedValue([itemResolution()])
})

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
    expect(result.current.defaultPrompts).toEqual({ pr: 1, issue: 4 })
    expect(result.current.items.map((i) => i.id)).toEqual([1, 2])
    expect(platform.resolveLaunches).toHaveBeenCalledWith([1, 2])
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

    await act(async () => {
      await result.current.reload()
    })

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

  it('reports a configuration or resolution that could not load', async () => {
    platform.getSettings.mockRejectedValueOnce(new Error('database is locked'))
    const { result } = renderHook(() => useLaunchConfig(snapshot({ reviewRequested: list([item()]) })))

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.settings).toBeNull()

    platform.resolveLaunches.mockRejectedValueOnce(new Error('item not found'))

    await act(async () => {
      await result.current.reload()
    })

    await waitFor(() => {
      expect(result.current.error).toBe('item not found')
    })

    platform.listPrompts.mockRejectedValueOnce(new Error('read the prompts: busy'))

    await act(async () => {
      await result.current.reload()
    })

    expect(result.current.error).toBe('read the prompts: busy')
  })
})
