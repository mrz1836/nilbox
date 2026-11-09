import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { agentSession, item, sessionsSnapshot } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { sessionsOf } from './sessions'
import { useSessions } from './useSessions'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useSessions', () => {
  it('loads the sessions, and again whenever one changes, until unmounted', async () => {
    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession()]))

    const { result, unmount } = renderHook(() => useSessions())

    await waitFor(() => {
      expect(sessionsOf(result.current.byItem, item())?.[0]?.state).toBe('needs_approval')
    })

    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession({ state: 'idle' })]))
    act(() => {
      emit('sessions:updated')
    })

    await waitFor(() => {
      expect(sessionsOf(result.current.byItem, item())?.[0]?.state).toBe('idle')
    })

    expect(listening('sessions:updated')).toBe(1)
    unmount()
    expect(listening('sessions:updated')).toBe(0)
  })

  it('holds nothing while the app does not follow sessions', async () => {
    platform.listSessions.mockResolvedValue(sessionsSnapshot([agentSession()], false))

    const { result } = renderHook(() => useSessions())

    await waitFor(() => {
      expect(result.current.snapshot?.following).toBe(false)
    })

    expect(result.current.byItem.size).toBe(0)
  })
})
