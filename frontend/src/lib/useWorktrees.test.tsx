import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RemoveResult, WorktreesSnapshot } from '../platform'
import { deferred } from '../test/async'
import { mainWorktree, removeResult, worktree, worktreeRepo, worktreesSnapshot } from '../test/fixtures'
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

  it('numbers each listing that arrives, keeping the number while one fails', async () => {
    const { result } = renderHook(() => useWorktrees())

    expect(result.current.listing).toBe(0)
    await waitFor(() => {
      expect(result.current.listing).toBe(1)
    })

    act(() => {
      result.current.refresh()
    })
    await waitFor(() => {
      expect(result.current.listing).toBe(2)
    })

    platform.listWorktrees.mockRejectedValue(new Error('git: exit status 128'))
    act(() => {
      result.current.refresh()
    })
    await waitFor(() => {
      expect(result.current.loadError).toBe('git: exit status 128')
    })
    expect(result.current.listing).toBe(2)
    expect(platform.listWorktrees).toHaveBeenCalledTimes(3)
  })

  it('says on each worktree a bulk action took what became of it, and why one was left', async () => {
    const path = (name: string) => `/home/user/projects/example-repo/.claude/worktrees/${name}`
    const repo = worktreeRepo({
      worktrees: [
        mainWorktree(),
        worktree({ path: path('pr-7') }),
        worktree({ path: path('pr-8') }),
        worktree({ path: path('pr-9') }),
      ],
    })
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [repo], cleanCount: 3 }))
    const { result } = renderHook(() => useWorktrees())
    await waitFor(() => {
      expect(result.current.disabled).toBe(false)
    })

    const removal = deferred<RemoveResult[]>()
    platform.removeAllCleanWorktrees.mockReturnValue(removal.promise)
    platform.listWorktrees.mockReturnValue(deferred<WorktreesSnapshot>().promise)
    act(() => {
      result.current.askRemoveAllClean()
    })
    act(() => {
      result.current.confirm({
        picks: ['pr-7', 'pr-8', 'pr-9'].map((name) => ({ repoPath: repo.path, path: path(name), branches: [] })),
      })
    })

    expect(result.current.workOn(path('pr-7'))).toEqual({ state: 'doing', text: 'Removing…', reason: '' })
    expect(result.current.workOn(repo.path)).toBeNull()

    await act(async () => {
      removal.resolve([
        removeResult({ path: path('pr-7') }),
        removeResult({
          path: path('pr-8'),
          removed: false,
          reason: 'the worktree changed since it was checked; refresh and try again',
        }),
        removeResult({ path: path('pr-9'), removed: false, reason: '' }),
      ])
      await removal.promise
    })

    expect(result.current.workOn(path('pr-7'))).toEqual({ state: 'done', text: 'Removed', reason: '' })
    expect(result.current.workOn(path('pr-8'))).toEqual({
      state: 'left',
      text: 'Left in place',
      reason: 'Left in place: the worktree changed since it was checked; refresh and try again.',
    })
    expect(result.current.workOn(path('pr-9'))).toEqual({ state: 'left', text: 'Left in place', reason: '' })
  })
})
