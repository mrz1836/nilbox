import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { mainWorktree, worktreeRepo, worktreesSnapshot } from '../test/fixtures'
import { useWorktrees } from './useWorktrees'

const platform = vi.hoisted(() => ({
  listWorktrees: vi.fn(),
  recentCleanups: vi.fn(),
  clearCleanups: vi.fn(),
  removeWorktree: vi.fn(),
  forceRemoveWorktree: vi.fn(),
  unlockWorktree: vi.fn(),
  cleanUpWorktree: vi.fn(),
  pruneWorktrees: vi.fn(),
  removeAllCleanWorktrees: vi.fn(),
  cleanUpFinishedWorktrees: vi.fn(),
}))

vi.mock('../platform', () => platform)

beforeEach(() => {
  platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
  platform.recentCleanups.mockResolvedValue([])
})

describe('useWorktrees', () => {
  it('does nothing when confirmed with nothing waiting', async () => {
    const { result } = renderHook(() => useWorktrees())
    await waitFor(() => {
      expect(result.current.disabled).toBe(false)
    })

    act(() => {
      result.current.confirm()
    })

    expect(result.current.confirmation).toBeNull()
    expect(result.current.disabled).toBe(false)
    expect(platform.forceRemoveWorktree).not.toHaveBeenCalled()
    expect(platform.removeAllCleanWorktrees).not.toHaveBeenCalled()
    expect(platform.cleanUpFinishedWorktrees).not.toHaveBeenCalled()
    expect(platform.listWorktrees).toHaveBeenCalledOnce()
  })

  it('keeps a failure apart from the main worktree that shares its repository path', async () => {
    const repo = worktreeRepo({ hasStale: true, worktrees: [mainWorktree()] })
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [repo] }))
    platform.pruneWorktrees.mockRejectedValue(new Error('prune failed'))
    const { result } = renderHook(() => useWorktrees())
    await waitFor(() => {
      expect(result.current.disabled).toBe(false)
    })

    act(() => {
      result.current.prune(repo)
    })

    await waitFor(() => {
      expect(result.current.errors.repo(repo.path)).toBe('prune failed')
    })
    expect(result.current.errors.worktree(repo.path)).toBe('')
    expect(result.current.errors.repo('/elsewhere')).toBe('')
    expect(result.current.notice).toBe('')
  })
})
