import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { mainWorktree, worktree, worktreeRepo, worktreesSnapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { useWorktrees } from './useWorktrees'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useWorktrees', () => {
  it('does nothing when confirmed with nothing waiting', async () => {
    const { result } = renderHook(() => useWorktrees())
    await waitFor(() => {
      expect(result.current.disabled).toBe(false)
    })

    act(() => {
      result.current.confirm({})
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
    expect(result.current.notice).toBeNull()
  })

  it('takes nothing that a confirmation did not tick', async () => {
    const repo = worktreeRepo()
    const wt = worktree()
    const { result } = renderHook(() => useWorktrees())
    await waitFor(() => {
      expect(result.current.disabled).toBe(false)
    })

    // ask opens each confirmation in turn and confirms it with nothing ticked.
    const ask = async (open: () => void) => {
      act(open)
      act(() => {
        result.current.confirm({})
      })
      await waitFor(() => {
        expect(result.current.disabled).toBe(false)
      })
    }

    await ask(() => {
      result.current.askRemove(repo, wt)
    })
    await ask(() => {
      result.current.askRemoveAllClean()
    })
    await ask(() => {
      result.current.askCleanUpFinished()
    })
    await ask(() => {
      result.current.askCleanUpMerged()
    })

    expect(platform.removeWorktree).toHaveBeenCalledWith(repo.path, wt.path, [])
    expect(platform.removeAllCleanWorktrees).toHaveBeenCalledWith([])
    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledWith([])
    expect(platform.deleteBranches).toHaveBeenCalledWith([])
  })
})
