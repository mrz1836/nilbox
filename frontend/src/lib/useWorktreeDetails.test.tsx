import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode, useEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Worktree, WorktreeDetails } from '../platform'
import { mainWorktree, worktree, worktreeDetails, worktreeFiles } from '../test/fixtures'
import { platform } from '../test/platform'
import { changesOf, readable, useWorktreeDetails, type DetailsRead } from './useWorktreeDetails'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const repoPath = '/home/user/projects/example-repo'

// dirty is pr-7 with 30 uncommitted files, of which the listing sent the first 12.
const dirty = worktree({ dirty: true, dirtyCount: 30, files: worktreeFiles(12), canRemove: false })

// all30 is what Details reads of it: every file.
const all30 = worktreeDetails({ files: worktreeFiles(30), fileCount: 30 })

// held is a call that answers only once the test says.
function held<T>() {
  let resolve: (value: T) => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

// renderDetails keeps the details for listing, which rerender changes.
function renderDetails(listing = 1) {
  return renderHook(({ at }: { at: number }) => useWorktreeDetails(at), { initialProps: { at: listing } })
}

describe('useWorktreeDetails', () => {
  it('reads a worktree’s details once a listing, and keeps the answer', async () => {
    platform.worktreeDetails.mockResolvedValue(all30)
    const { result } = renderDetails()

    expect(result.current.of(dirty.path)).toBeUndefined()

    act(() => {
      result.current.read(repoPath, dirty)
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'reading' })
    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    })

    act(() => {
      result.current.read(repoPath, dirty)
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    expect(platform.worktreeDetails).toHaveBeenCalledExactlyOnceWith(repoPath, dirty.path)
  })

  it('reads several worktrees side by side, each kept apart', async () => {
    const other = worktree({ path: `${repoPath}/.claude/worktrees/pr-8` })
    const first = held<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(first.promise).mockResolvedValueOnce(worktreeDetails())
    const { result } = renderDetails()

    act(() => {
      result.current.read(repoPath, dirty)
      result.current.read(repoPath, other)
    })

    await waitFor(() => {
      expect(result.current.of(other.path)).toEqual({ state: 'read', details: worktreeDetails() })
    })
    expect(result.current.of(dirty.path)).toEqual({ state: 'reading' })

    await act(async () => {
      first.resolve(all30)
      await first.promise
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    expect(result.current.of(other.path)).toEqual({ state: 'read', details: worktreeDetails() })
  })

  it.each([
    ['the main checkout', mainWorktree()],
    ['a worktree whose folder is gone', worktree({ prunable: true })],
    ['a worktree git couldn’t read', worktree({ error: 'could not read the status: exit status 128' })],
  ])('never asks about %s, which Details refuses', (_, wt) => {
    const { result } = renderDetails()

    act(() => {
      result.current.read(repoPath, wt)
    })

    expect(readable(wt)).toBe(false)
    expect(result.current.of(wt.path)).toBeUndefined()
    expect(platform.worktreeDetails).not.toHaveBeenCalled()
  })

  it('drops what a listing read at the next, and reads again', async () => {
    platform.worktreeDetails.mockResolvedValueOnce(all30).mockResolvedValueOnce(worktreeDetails({ fileCount: 0 }))
    const { result, rerender } = renderDetails(1)

    act(() => {
      result.current.read(repoPath, dirty)
    })
    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    })

    rerender({ at: 2 })

    expect(result.current.of(dirty.path)).toBeUndefined()

    act(() => {
      result.current.read(repoPath, dirty)
    })
    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: worktreeDetails({ fileCount: 0 }) })
    })
    expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)
  })

  it('drops an answer that lands for an older listing', async () => {
    const old = held<WorktreeDetails>()
    const now = held<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(old.promise).mockReturnValueOnce(now.promise)
    const { result, rerender } = renderDetails(1)

    act(() => {
      result.current.read(repoPath, dirty)
    })
    rerender({ at: 2 })
    act(() => {
      result.current.read(repoPath, dirty)
    })

    await act(async () => {
      old.resolve(all30)
      await old.promise
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'reading' })

    await act(async () => {
      now.resolve(worktreeDetails())
      await now.promise
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: worktreeDetails() })

    // An older listing's failure is dropped too.
    const late = held<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(late.promise)
    rerender({ at: 3 })
    act(() => {
      result.current.read(repoPath, dirty)
    })
    rerender({ at: 4 })

    await act(async () => {
      late.reject(new Error('git status: exit status 128'))
      await late.promise.catch(() => undefined)
    })

    expect(result.current.of(dirty.path)).toBeUndefined()
  })

  it('says why a read failed, keeps the listing’s files, and asks no more that listing', async () => {
    platform.worktreeDetails.mockRejectedValue(new Error('not a linked worktree of this repository'))
    const { result } = renderDetails()

    act(() => {
      result.current.read(repoPath, dirty)
    })
    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({
        state: 'failed',
        error: 'not a linked worktree of this repository',
      })
    })

    expect(changesOf(dirty, result.current.of(dirty.path))).toEqual({
      files: worktreeFiles(12),
      count: 30,
      current: false,
    })

    act(() => {
      result.current.read(repoPath, dirty)
    })

    expect(platform.worktreeDetails).toHaveBeenCalledOnce()
  })

  it('asks once under StrictMode, whose effects run twice, as a drawer reads when it opens', async () => {
    platform.worktreeDetails.mockResolvedValue(all30)
    const { result } = renderHook(
      () => {
        const details = useWorktreeDetails(1)

        useEffect(() => {
          details.read(repoPath, dirty)
        }, [details])

        return details
      },
      { wrapper: StrictMode },
    )

    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    })
    expect(platform.worktreeDetails).toHaveBeenCalledOnce()
  })
})

describe('readable', () => {
  it.each([
    [worktree(), true],
    [worktree({ dirty: true, dirtyCount: 3, locked: true }), true],
    [mainWorktree(), false],
    [worktree({ prunable: true }), false],
    [worktree({ error: 'unreadable' }), false],
  ])('knows whether Details reads %#', (wt: Worktree, want: boolean) => {
    expect(readable(wt)).toBe(want)
  })
})

describe('changesOf', () => {
  it.each<[string, DetailsRead | undefined, ReturnType<typeof changesOf>]>([
    ['nothing read yet', undefined, { files: worktreeFiles(12), count: 30, current: false }],
    ['a read under way', { state: 'reading' }, { files: worktreeFiles(12), count: 30, current: false }],
    ['a read that failed', { state: 'failed', error: 'no' }, { files: worktreeFiles(12), count: 30, current: false }],
    [
      'Details that couldn’t read the files',
      { state: 'read', details: worktreeDetails({ fileCount: -1, errors: ['Couldn’t read the changed files.'] }) },
      { files: worktreeFiles(12), count: 30, current: false },
    ],
    ['Details, every file', { state: 'read', details: all30 }, { files: worktreeFiles(30), count: 30, current: true }],
    [
      'Details, past 200',
      { state: 'read', details: worktreeDetails({ files: worktreeFiles(200), fileCount: 1204 }) },
      { files: worktreeFiles(200), count: 1204, current: true },
    ],
    [
      'Details, clean since the listing',
      { state: 'read', details: worktreeDetails() },
      { files: [], count: 0, current: true },
    ],
  ])('lists the files from %s', (_, read, want) => {
    expect(changesOf(dirty, read)).toEqual(want)
  })
})
