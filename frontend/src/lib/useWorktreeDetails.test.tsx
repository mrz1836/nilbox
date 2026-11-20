import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode, useEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Worktree, WorktreeCopyMark, WorktreeDetails } from '../platform'
import {
  copiedWorktree,
  linkedItem,
  mainWorktree,
  partlyCopiedWorktree,
  recordedWorktree,
  worktree,
  worktreeCommit,
  worktreeCopies,
  worktreeDetails,
  worktreeFiles,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { asReadNow, changesOf, notOnGitHub, readable, useWorktreeDetails, type DetailsRead } from './useWorktreeDetails'
import { copiedOf, unpushedKnown } from './worktreeParts'

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

  it('says why a read failed, keeps the listing’s files, and asks again only when read again', async () => {
    const failing = held<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(failing.promise).mockResolvedValueOnce(all30)
    const { result } = renderDetails()

    act(() => {
      result.current.read(repoPath, dirty)
      // One ask at a time: a second while the first is under way does nothing.
      result.current.read(repoPath, dirty)
    })

    await act(async () => {
      failing.reject(new Error('not a linked worktree of this repository'))
      await failing.promise.catch(() => undefined)
    })

    expect(result.current.of(dirty.path)).toEqual({
      state: 'failed',
      error: 'not a linked worktree of this repository',
    })
    expect(changesOf(dirty, result.current.of(dirty.path))).toEqual({
      files: worktreeFiles(12),
      count: 30,
      current: false,
    })
    // Nothing asks again on its own.
    expect(platform.worktreeDetails).toHaveBeenCalledOnce()

    act(() => {
      result.current.read(repoPath, dirty)
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'reading' })
    await waitFor(() => {
      expect(result.current.of(dirty.path)).toEqual({ state: 'read', details: all30 })
    })
    expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)
  })

  it('lets an older listing’s failure leave the newer listing’s ask be', async () => {
    const old = held<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(old.promise).mockReturnValueOnce(new Promise(() => undefined))
    const { result, rerender } = renderDetails(1)

    act(() => {
      result.current.read(repoPath, dirty)
    })
    rerender({ at: 2 })
    act(() => {
      result.current.read(repoPath, dirty)
    })

    await act(async () => {
      old.reject(new Error('git status: exit status 128'))
      await old.promise.catch(() => undefined)
    })

    act(() => {
      result.current.read(repoPath, dirty)
    })

    expect(result.current.of(dirty.path)).toEqual({ state: 'reading' })
    expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)
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

// ahead is a clean worktree with 2 commits not on GitHub, as the listing counted them.
const ahead = worktree({ unpushed: 2, onGitHub: false })

// atHead is one at its pull request's head, a fork's, which no remote-tracking branch has:
// the listing counted 3 commits there, and the backend ruled them on GitHub.
const atHead = worktree({ unpushed: 3, onGitHub: true })

// copied has 10 commits no remote-tracking branch has, each with an identical copy on a
// pushed branch, and recorded the same once that branch is gone: the backend ruled them on
// GitHub.
const copied = copiedWorktree()
const recorded = recordedWorktree()

// two is copied with two commits, HEAD and older, each with an identical copy, and
// recordedTwo the same, vouched for by a record; mark is one of their commits as Details
// lists it, with the copy it found of it.
const two = copiedWorktree({
  unpushed: 2,
  copies: worktreeCopies({ ref: 'origin/feature-x', count: 2, identical: 2 }),
})
const recordedTwo = recordedWorktree({
  unpushed: 2,
  copies: worktreeCopies({ ref: 'origin/feature-x', count: 2, identical: 2, recorded: true }),
})
const older = '1'.repeat(40)

function mark(oid: string, copy: WorktreeCopyMark) {
  return worktreeCommit({ oid, copy, copyOid: copy === '' ? '' : '2'.repeat(40) })
}

describe('asReadNow', () => {
  it.each<[string, Worktree, DetailsRead | undefined, Partial<Worktree>]>([
    ['nothing read yet', ahead, undefined, {}],
    ['a read under way', ahead, { state: 'reading' }, {}],
    ['a read that failed', ahead, { state: 'failed', error: 'no' }, {}],
    [
      'files and commits counted now',
      ahead,
      { state: 'read', details: worktreeDetails({ files: worktreeFiles(3), fileCount: 3, unpushed: 5 }) },
      { dirty: true, dirtyCount: 3, unpushed: 5, onGitHub: false },
    ],
    [
      'everything on GitHub now, and clean',
      dirty,
      { state: 'read', details: worktreeDetails() },
      { dirty: false, dirtyCount: 0, unpushed: 0, onGitHub: true },
    ],
    ['neither counted', ahead, { state: 'read', details: worktreeDetails({ fileCount: -1, unpushed: -1 }) }, {}],
    [
      'the same count, not at its pull request’s head',
      ahead,
      { state: 'read', details: worktreeDetails({ unpushed: 2 }) },
      { unpushed: 2, onGitHub: false },
    ],
    [
      'the same count at its pull request’s head, which is on GitHub',
      atHead,
      { state: 'read', details: worktreeDetails({ unpushed: 3 }) },
      { unpushed: 3, onGitHub: true },
    ],
    [
      'a count that changed since, so HEAD moved off the head',
      atHead,
      { state: 'read', details: worktreeDetails({ unpushed: 4 }) },
      { unpushed: 4, onGitHub: false },
    ],
    [
      'none since, off the head or on it',
      atHead,
      { state: 'read', details: worktreeDetails({ unpushed: 0 }) },
      { unpushed: 0, onGitHub: true },
    ],
    [
      'the same commits at its pull request’s head, HEAD listed first',
      atHead,
      { state: 'read', details: worktreeDetails({ unpushed: 3, commits: [worktreeCommit({ oid: atHead.head })] }) },
      {},
    ],
    [
      'as many commits, but HEAD moved off the head, as an amend leaves it',
      atHead,
      { state: 'read', details: worktreeDetails({ unpushed: 3, commits: [worktreeCommit()] }) },
      { onGitHub: false },
    ],
    [
      'the same commits on GitHub as copies, counted alone',
      copied,
      { state: 'read', details: worktreeDetails({ unpushed: 10 }) },
      {},
    ],
    [
      'the same commits on GitHub as copies, HEAD listed first',
      copied,
      {
        state: 'read',
        details: worktreeDetails({
          unpushed: 10,
          commits: [worktreeCommit({ oid: copied.head, copy: 'identical', copyOid: '3'.repeat(40) })],
        }),
      },
      {},
    ],
    [
      'the same commits a record vouches for',
      recorded,
      { state: 'read', details: worktreeDetails({ unpushed: 10, commits: [worktreeCommit({ oid: recorded.head })] }) },
      {},
    ],
    [
      'a commit more than were copied',
      copied,
      { state: 'read', details: worktreeDetails({ unpushed: 11 }) },
      { unpushed: 11, onGitHub: false, copies: worktreeCopies() },
    ],
    [
      'as many commits as were copied, but HEAD moved',
      copied,
      { state: 'read', details: worktreeDetails({ unpushed: 10, commits: [worktreeCommit({ copy: 'edited' })] }) },
      { onGitHub: false, copies: worktreeCopies() },
    ],
    [
      'every copied commit pushed since',
      recorded,
      { state: 'read', details: worktreeDetails({ unpushed: 0 }) },
      { unpushed: 0, onGitHub: true, copies: worktreeCopies() },
    ],
    [
      'commits that couldn’t be counted, copies or not',
      copied,
      { state: 'read', details: worktreeDetails({ unpushed: -1 }) },
      {},
    ],
    [
      'the same commits, each still with its identical copy',
      two,
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 2, commits: [mark(two.head, 'identical'), mark(older, 'identical')] }),
      },
      {},
    ],
    [
      'the same commits, one with only an edited copy now, as a force-push leaves it',
      two,
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 2, commits: [mark(two.head, 'identical'), mark(older, 'edited')] }),
      },
      { onGitHub: false, copies: worktreeCopies() },
    ],
    [
      'the same commits, one with no copy now',
      two,
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 2, commits: [mark(two.head, 'identical'), mark(older, '')] }),
      },
      { onGitHub: false, copies: worktreeCopies() },
    ],
    [
      'the same commits, which Details couldn’t compare',
      two,
      { state: 'read', details: worktreeDetails({ unpushed: 2, commits: [mark(two.head, ''), mark(older, '')] }) },
      {},
    ],
    [
      'the same commits a record vouches for, whatever the branches there are have',
      recordedTwo,
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 2, commits: [mark(two.head, 'edited'), mark(older, '')] }),
      },
      {},
    ],
    [
      'the same commits at its pull request’s head, whatever copies they have',
      worktree({ unpushed: 1, onGitHub: true, item: linkedItem({ state: 'open' }) }),
      { state: 'read', details: worktreeDetails({ unpushed: 1, commits: [mark(atHead.head, 'edited')] }) },
      {},
    ],
    [
      'the same commit, its edited copy identical now, which only a listing counts',
      partlyCopiedWorktree(),
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 1, commits: [mark(partlyCopiedWorktree().head, 'identical')] }),
      },
      { copies: worktreeCopies() },
    ],
    [
      'the same commit, still with only an edited copy',
      partlyCopiedWorktree(),
      {
        state: 'read',
        details: worktreeDetails({ unpushed: 1, commits: [mark(partlyCopiedWorktree().head, 'edited')] }),
      },
      {},
    ],
  ])('counts what removing it would cost from %s', (_, wt, read, changed) => {
    expect(asReadNow(wt, read)).toEqual({ ...wt, ...changed })
  })

  it('words copies only while the commits are those compared', () => {
    const moved = asReadNow(copied, {
      state: 'read',
      details: worktreeDetails({ unpushed: 10, commits: [worktreeCommit()] }),
    })

    expect(copiedOf(copied)).not.toBeNull()
    expect(copiedOf(moved)).toBeNull()
    expect(unpushedKnown(moved)).toBe(10)
  })
})

describe('notOnGitHub', () => {
  it.each<[string, Worktree, DetailsRead | undefined, number]>([
    ['the listing’s, before a read', ahead, undefined, 2],
    ['the listing’s, after a read that failed', ahead, { state: 'failed', error: 'no' }, 2],
    ['a plain count, not at the head', ahead, { state: 'read', details: worktreeDetails({ unpushed: 2 }) }, 2],
    ['a fresh count, not at the head', ahead, { state: 'read', details: worktreeDetails({ unpushed: 350 }) }, 350],
    ['none at the pull request’s head', atHead, { state: 'read', details: worktreeDetails({ unpushed: 3 }) }, 0],
    ['none at the head before a read', atHead, undefined, 0],
    ['the fresh count once HEAD moved', atHead, { state: 'read', details: worktreeDetails({ unpushed: 4 }) }, 4],
    ['none when they couldn’t be counted', worktree({ unpushed: -1, onGitHub: false }), undefined, 0],
    ['none on GitHub as copies before a read', copied, undefined, 0],
    [
      'none on GitHub as copies, the same once read',
      copied,
      { state: 'read', details: worktreeDetails({ unpushed: 10 }) },
      0,
    ],
    ['none a record vouches for once read', recorded, { state: 'read', details: worktreeDetails({ unpushed: 10 }) }, 0],
    ['the fresh count past the copies', copied, { state: 'read', details: worktreeDetails({ unpushed: 12 }) }, 12],
  ])('counts %s', (_, wt, read, want) => {
    expect(notOnGitHub(wt, read)).toBe(want)
  })
})
