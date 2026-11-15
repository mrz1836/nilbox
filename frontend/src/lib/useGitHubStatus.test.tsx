import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { githubIncident, githubStatus } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { useGitHubStatus } from './useGitHubStatus'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useGitHubStatus', () => {
  it('loads the last reading, and loads it again after each check', async () => {
    const { result } = renderHook(() => useGitHubStatus(1800))

    expect(result.current.status).toBeNull()
    await waitFor(() => {
      expect(result.current.status?.state).toBe('operational')
    })

    platform.getGitHubStatus.mockResolvedValue(githubIncident())
    act(() => {
      emit('githubstatus:updated')
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })
  })

  it('loads it again when the check is turned off or on', async () => {
    const { result, rerender } = renderHook(({ every }) => useGitHubStatus(every), { initialProps: { every: 1800 } })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('operational')
    })

    platform.getGitHubStatus.mockResolvedValue(githubStatus({ state: 'off', checkedAt: '' }))
    rerender({ every: 0 })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('off')
    })
    expect(platform.getGitHubStatus).toHaveBeenCalledTimes(2)
  })

  it('checks now and shows the answer, saying while it checks', async () => {
    let answer: (status: ReturnType<typeof githubStatus>) => void = () => undefined
    platform.checkGitHubStatus.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    const { result } = renderHook(() => useGitHubStatus(1800))

    await waitFor(() => {
      expect(result.current.status).not.toBeNull()
    })

    act(() => {
      result.current.check()
    })
    expect(result.current.checking).toBe(true)

    await act(async () => {
      answer(githubIncident())
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })
    expect(result.current.checking).toBe(false)
  })

  it('keeps the reading as it was when a check fails, and logs why', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getGitHubStatus.mockResolvedValue(githubIncident())
    platform.checkGitHubStatus.mockRejectedValue(new Error('the app is closing'))
    const { result } = renderHook(() => useGitHubStatus(1800))

    await waitFor(() => {
      expect(result.current.status?.state).toBe('major')
    })

    act(() => {
      result.current.check()
    })

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not check GitHub’s status:', 'the app is closing')
    })
    expect(result.current.status?.state).toBe('major')
    expect(result.current.checking).toBe(false)
  })
})
