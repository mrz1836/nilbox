import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { serviceIncident, serviceStatus, statusWatch } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { useServiceStatus } from './useServiceStatus'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useServiceStatus', () => {
  it('loads the last reading, and loads it again after each pass', async () => {
    const { result } = renderHook(() => useServiceStatus(1800))

    expect(result.current.status).toBeNull()
    await waitFor(() => {
      expect(result.current.status?.state).toBe('operational')
    })

    platform.getServiceStatus.mockResolvedValue(serviceIncident())
    act(() => {
      emit('servicestatus:updated')
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })
  })

  it('loads it again when the check is turned off or on', async () => {
    const { result, rerender } = renderHook(({ every }) => useServiceStatus(every), { initialProps: { every: 1800 } })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('operational')
    })

    platform.getServiceStatus.mockResolvedValue(serviceStatus({ state: 'off', checkedAt: '' }))
    rerender({ every: 0 })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('off')
    })
    expect(platform.getServiceStatus).toHaveBeenCalledTimes(2)
  })

  it('loads it again when what is watched changes, and not for the same choices again', async () => {
    const { result, rerender } = renderHook(({ watch }) => useServiceStatus(1800, watch), {
      initialProps: { watch: statusWatch() },
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('operational')
    })

    rerender({ watch: statusWatch() })
    expect(platform.getServiceStatus).toHaveBeenCalledTimes(1)

    platform.getServiceStatus.mockResolvedValue(serviceIncident())
    const ticked = statusWatch()
    ticked.watch[1]?.parts.push('claude.ai')
    rerender({ watch: ticked })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })
    expect(platform.getServiceStatus).toHaveBeenCalledTimes(2)
  })

  it('checks now and shows the answer, saying while it checks', async () => {
    let answer: (status: ReturnType<typeof serviceStatus>) => void = () => undefined
    platform.checkServiceStatus.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    const { result } = renderHook(() => useServiceStatus(1800))

    await waitFor(() => {
      expect(result.current.status).not.toBeNull()
    })

    act(() => {
      result.current.check()
    })
    expect(result.current.checking).toBe(true)

    await act(async () => {
      answer(serviceIncident())
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })
    expect(result.current.checking).toBe(false)
  })

  it('keeps the reading as it was when a check fails, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getServiceStatus.mockResolvedValue(serviceIncident())
    platform.checkServiceStatus.mockRejectedValue(new Error('the app is closing'))
    const { result } = renderHook(() => useServiceStatus(1800))

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })

    act(() => {
      result.current.check()
    })

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not check the service status:', 'the app is closing')
    })
    expect(result.current.status?.state).toBe('major')
    expect(result.current.checking).toBe(false)
  })
})
