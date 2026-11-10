import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { IntegrationTools } from '../platform'
import { goPreCommitTool, integrationTool, integrationTools } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { useIntegrations } from './useIntegrations'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// names lists the tools a state holds, by name and state.
function names(tools: IntegrationTools | null): string[] {
  return (tools?.tools ?? []).map((t) => `${t.name} ${t.state}`)
}

describe('useIntegrations', () => {
  it('lists the tools, and again whenever a look for them ends, until unmounted', async () => {
    platform.listIntegrations.mockResolvedValue(
      integrationTools({ tools: [integrationTool({ checking: true }), goPreCommitTool({ checking: true })] }),
    )

    const { result, unmount } = renderHook(() => useIntegrations())

    expect(result.current.tools).toBeNull()
    await waitFor(() => {
      expect(result.current.tools?.tools.every((t) => t.checking)).toBe(true)
    })

    platform.listIntegrations.mockResolvedValue(
      integrationTools({ tools: [integrationTool({ state: 'update' }), goPreCommitTool({ state: 'missing' })] }),
    )
    act(() => {
      emit('tools:updated')
    })

    await waitFor(() => {
      expect(names(result.current.tools)).toEqual(['mage-x update', 'go-pre-commit missing'])
    })

    expect(platform.listIntegrations).toHaveBeenCalledTimes(2)
    expect(listening('tools:updated')).toBe(1)
    unmount()
    expect(listening('tools:updated')).toBe(0)
  })

  it('says why the tools could not be listed', async () => {
    platform.listIntegrations.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHook(() => useIntegrations())

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
    expect(result.current.tools).toBeNull()
  })

  it('reads the list again on request, resolving once it is in', async () => {
    const { result } = renderHook(() => useIntegrations())

    await waitFor(() => {
      expect(result.current.tools).not.toBeNull()
    })

    platform.listIntegrations.mockResolvedValue(integrationTools({ tools: [integrationTool({ switch: 'off' })] }))

    // The read starts once the request renders, so it is awaited after that render.
    let done: Promise<void> = Promise.resolve()
    act(() => {
      done = result.current.reload()
    })
    await act(async () => {
      await done
    })

    expect(result.current.tools?.tools.map((t) => t.switch)).toEqual(['off'])
  })

  it('checks again, showing what it found, and is checking until then', async () => {
    let finish: (tools: IntegrationTools) => void = () => undefined
    platform.checkIntegrationsAgain.mockReturnValue(
      new Promise<IntegrationTools>((resolve) => {
        finish = resolve
      }),
    )

    const { result } = renderHook(() => useIntegrations())

    await waitFor(() => {
      expect(result.current.tools).not.toBeNull()
    })

    let done: Promise<void> = Promise.resolve()
    act(() => {
      done = result.current.checkAgain()
    })

    expect(platform.checkIntegrationsAgain).toHaveBeenCalledOnce()
    expect(result.current.checking).toBe(true)

    await act(async () => {
      finish(integrationTools({ tools: [integrationTool({ state: 'tooOld' })], shellRead: false }))
      await done
    })

    expect(result.current.checking).toBe(false)
    expect(names(result.current.tools)).toEqual(['mage-x tooOld'])
    expect(result.current.tools?.shellRead).toBe(false)
  })

  it('keeps the list as it was when checking again fails, and says why', async () => {
    platform.checkIntegrationsAgain.mockRejectedValue(new Error('the shell timed out'))

    const { result } = renderHook(() => useIntegrations())

    await waitFor(() => {
      expect(result.current.tools).not.toBeNull()
    })

    await act(async () => {
      await result.current.checkAgain()
    })

    expect(result.current.checkError).toBe('the shell timed out')
    expect(names(result.current.tools)).toEqual(['mage-x found', 'go-pre-commit found'])
  })
})
