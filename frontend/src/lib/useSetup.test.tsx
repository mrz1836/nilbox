import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Setup } from '../platform'
import { platform } from '../test/platform'
import { useSetup } from './useSetup'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// opensAt puts the page at a URL, as the shell opens it.
function opensAt(search: string) {
  window.history.replaceState(null, '', `/${search}`)
}

// pending is a setup state that never arrives, so the first frame stays as the URL says.
function pending(): Promise<Setup> {
  return new Promise<Setup>(() => undefined)
}

afterEach(() => {
  opensAt('')
})

describe('useSetup', () => {
  it('trusts the URL for the first frame, then the stored state', async () => {
    opensAt('?setup=1&textSize=large')
    platform.getSetup.mockResolvedValue({ done: false, step: 'clones' })

    const { result } = renderHook(() => useSetup())

    expect(result.current.showing).toBe(true)
    expect(result.current.step).toBe('welcome')

    await waitFor(() => {
      expect(result.current.step).toBe('clones')
    })
    expect(result.current.showing).toBe(true)
    expect(result.current.rerun).toBe(false)
  })

  it('shows nothing on a done setup, and drops a stray parameter', async () => {
    opensAt('?setup=1&textSize=large')

    const { result } = renderHook(() => useSetup())

    await waitFor(() => {
      expect(result.current.showing).toBe(false)
    })
    expect(window.location.search).toBe('?textSize=large')
  })

  it('shows the setup the stored state asks for, though the URL never said so', async () => {
    platform.getSetup.mockResolvedValue({ done: false, step: 'welcome' })

    const { result } = renderHook(() => useSetup())

    expect(result.current.showing).toBe(false)

    await waitFor(() => {
      expect(result.current.showing).toBe(true)
    })
  })

  it('never traps the user when the state can’t be read', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    opensAt('?setup=1')
    platform.getSetup.mockRejectedValue(new Error('the database is gone'))

    const { result } = renderHook(() => useSetup())

    await waitFor(() => {
      expect(result.current.showing).toBe(false)
    })
  })

  it('remembers each step', () => {
    opensAt('?setup=1')
    platform.getSetup.mockReturnValue(pending())

    const { result } = renderHook(() => useSetup())

    act(() => {
      result.current.go('agents')
    })

    expect(result.current.step).toBe('agents')
    expect(platform.setSetupStep).toHaveBeenCalledWith('agents')
  })

  it('finishes at once, storing it and leaving the URL without the parameter', () => {
    opensAt('?setup=1&textSize=large')
    platform.getSetup.mockReturnValue(pending())

    const { result } = renderHook(() => useSetup())

    act(() => {
      result.current.finish()
    })

    expect(result.current.showing).toBe(false)
    expect(platform.finishSetup).toHaveBeenCalledOnce()
    expect(window.location.search).toBe('?textSize=large')
  })

  it('logs a finish or a step it could not store, keeping what shows', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getSetup.mockReturnValue(pending())
    platform.finishSetup.mockRejectedValue(new Error('disk full'))
    platform.setSetupStep.mockRejectedValue(new Error('disk full'))

    const { result } = renderHook(() => useSetup())

    act(() => {
      result.current.go('clones')
    })
    act(() => {
      result.current.finish()
    })

    expect(result.current.showing).toBe(false)
    await waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not store that the setup is done:', 'disk full')
    })
    expect(error).toHaveBeenCalledWith('could not remember the setup’s step:', 'disk full')
  })

  it('runs again from Welcome, welcoming the user back', async () => {
    const { result } = renderHook(() => useSetup())

    await waitFor(() => {
      expect(platform.getSetup).toHaveBeenCalled()
    })

    let outcome: Awaited<ReturnType<typeof result.current.restart>> | undefined
    await act(async () => {
      outcome = await result.current.restart()
    })

    expect(outcome).toEqual({ ok: true, data: undefined })
    expect(result.current.showing).toBe(true)
    expect(result.current.step).toBe('welcome')
    expect(result.current.rerun).toBe(true)
  })

  it('says why it could not run again', async () => {
    platform.restartSetup.mockRejectedValue(new Error('disk full'))

    const { result } = renderHook(() => useSetup())

    let outcome: Awaited<ReturnType<typeof result.current.restart>> | undefined
    await act(async () => {
      outcome = await result.current.restart()
    })

    expect(outcome).toEqual({ ok: false, error: 'disk full' })
    expect(result.current.showing).toBe(false)
  })
})
