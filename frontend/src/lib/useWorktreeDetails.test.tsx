import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode, useEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Worktree, WorktreeCopyMark, WorktreeDetails } from '../platform'
import { deferred } from '../test/async'
import {
  copiedWorktree,
  copyComparison,
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
    const first = deferred<WorktreeDetails>()
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
    const old = deferred<WorktreeDetails>()
    const now = deferred<WorktreeDetails>()
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
    const late = deferred<WorktreeDetails>()
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
    const failing = deferred<WorktreeDetails>()
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
    const old = deferred<WorktreeDetails>()
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

// copied has 10 commits no remote-tracking branch has, each with an identical copy on
// origin/feature-x, pull request #42's branch, and recorded the same once #42 merged and
// its branch is gone, the app's record vouching for them: the backend ruled them on GitHub.
const copied = copiedWorktree()
const recorded = recordedWorktree()

// read is details read for this listing.
function read(overrides: Partial<WorktreeDetails> = {}): DetailsRead {
  return { state: 'read', details: worktreeDetails(overrides) }
}

// atCopiedHead lists copied's commits from its HEAD, each marked as copy says.
function atCopiedHead(copy: WorktreeCopyMark): WorktreeDetails['commits'] {
  return [worktreeCommit({ oid: copied.head, copy, copyOid: copy === '' ? '' : '3'.repeat(40) })]
}

describe('asReadNow', () => {
  it.each<[string, Worktree, DetailsRead | undefined, Partial<Worktree>]>([
    ['nothing read yet', ahead, undefined, {}],
    ['a read under way', ahead, { state: 'reading' }, {}],
    ['a read that failed', ahead, { state: 'failed', error: 'no' }, {}],
    [
      'files and commits counted now',
      ahead,
      read({ files: worktreeFiles(3), fileCount: 3, unpushed: 5 }),
      { dirty: true, dirtyCount: 3, unpushed: 5, onGitHub: false, copies: worktreeCopies({ count: 5 }) },
    ],
    ['everything on GitHub now, and clean', dirty, read(), { dirty: false, dirtyCount: 0 }],
    ['neither counted', ahead, read({ fileCount: -1, unpushed: -1 }), {}],
    [
      'the same count, not at its pull request’s head',
      ahead,
      read({ unpushed: 2 }),
      { copies: worktreeCopies({ count: 2 }) },
    ],
    [
      'the same count at its pull request’s head, which is on GitHub',
      atHead,
      read({ unpushed: 3 }),
      { copies: worktreeCopies({ count: 3 }) },
    ],
    [
      'a count that changed since, so HEAD moved off the head',
      atHead,
      read({ unpushed: 4 }),
      { unpushed: 4, onGitHub: false, copies: worktreeCopies({ count: 4 }) },
    ],
    ['none since, off the head or on it', atHead, read({ unpushed: 0 }), { unpushed: 0, onGitHub: true }],
    [
      'the same commits at its pull request’s head, HEAD listed first',
      atHead,
      read({ unpushed: 3, commits: [worktreeCommit({ oid: atHead.head })] }),
      { copies: worktreeCopies({ count: 3 }) },
    ],
    [
      'as many commits, but HEAD moved off the head, as an amend leaves it',
      atHead,
      read({ unpushed: 3, commits: [worktreeCommit()] }),
      { onGitHub: false, copies: worktreeCopies({ count: 3 }) },
    ],
    [
      'the same commits at its pull request’s head, whatever copies they have',
      worktree({ unpushed: 1, onGitHub: true, item: linkedItem({ state: 'open' }) }),
      read({
        unpushed: 1,
        commits: [worktreeCommit({ oid: atHead.head, copy: 'edited', copyOid: '3'.repeat(40) })],
        copies: copyComparison({ identical: 0, edited: 1 }),
      }),
      { copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 1, edited: 1 }) },
    ],
    [
      'the same commits, each still with its identical copy on the same branch',
      copied,
      read({ unpushed: 10, copies: copyComparison() }),
      {},
    ],
    [
      'the same commits, HEAD listed first, each still with its identical copy',
      copied,
      read({ unpushed: 10, commits: atCopiedHead('identical'), copies: copyComparison() }),
      {},
    ],
    [
      'the same commits, their branch deleted since, so none has a copy on a pushed branch',
      copied,
      read({ unpushed: 10, commits: atCopiedHead(''), copies: copyComparison({ ref: '', branch: '', identical: 0 }) }),
      { onGitHub: false, copies: worktreeCopies({ count: 10, none: 10 }) },
    ],
    [
      'the same commits, each with an identical copy on another branch',
      copied,
      read({ unpushed: 10, copies: copyComparison({ ref: 'origin/feature-y', branch: 'feature-y' }) }),
      {
        onGitHub: false,
        copies: worktreeCopies({ ref: 'origin/feature-y', branch: 'feature-y', count: 10, identical: 10 }),
      },
    ],
    [
      'the same commits, one with only an edited copy now, as a force-push leaves it',
      copied,
      read({ unpushed: 10, commits: atCopiedHead('edited'), copies: copyComparison({ identical: 9, edited: 1 }) }),
      {
        onGitHub: false,
        copies: worktreeCopies({
          ref: 'origin/feature-x',
          branch: 'feature-x',
          count: 10,
          identical: 9,
          edited: 1,
          pr: 42,
        }),
      },
    ],
    [
      'the same commits, which Details couldn’t compare, saying why',
      copied,
      read({
        unpushed: 10,
        copies: copyComparison({
          compared: false,
          ref: '',
          branch: '',
          identical: 0,
          note: 'Couldn’t compare its commits.',
        }),
      }),
      { onGitHub: false, copies: worktreeCopies({ count: 10, note: 'Couldn’t compare its commits.' }) },
    ],
    [
      'the same count, the commits themselves unread, so not compared',
      copied,
      read({ unpushed: 10, errors: ['Couldn’t read the commits not on GitHub.'] }),
      { onGitHub: false, copies: worktreeCopies({ count: 10 }) },
    ],
    [
      'the same commits a record vouches for, whatever the pushed branches hold now',
      recorded,
      read({ unpushed: 10, commits: atCopiedHead('edited'), copies: copyComparison({ identical: 0, edited: 1 }) }),
      {},
    ],
    ['the same commits a record vouches for, with nothing compared', recorded, read({ unpushed: 10 }), {}],
    [
      'commits a record vouched for, HEAD moved since',
      recorded,
      read({
        unpushed: 10,
        commits: [worktreeCommit()],
        copies: copyComparison({ ref: '', branch: '', identical: 0 }),
      }),
      { onGitHub: false, copies: worktreeCopies({ count: 10, none: 10 }) },
    ],
    [
      'a commit more than were copied',
      copied,
      read({ unpushed: 11 }),
      { unpushed: 11, onGitHub: false, copies: worktreeCopies({ count: 11 }) },
    ],
    [
      'every copied commit pushed since',
      recorded,
      read({ unpushed: 0 }),
      { unpushed: 0, onGitHub: true, copies: worktreeCopies() },
    ],
    ['commits that couldn’t be counted, copies or not', copied, read({ unpushed: -1 }), {}],
    [
      'the same commit, its edited copy identical now, which only a listing counts on GitHub',
      partlyCopiedWorktree(),
      read({
        unpushed: 1,
        commits: [worktreeCommit({ oid: partlyCopiedWorktree().head, copy: 'identical', copyOid: '3'.repeat(40) })],
        copies: copyComparison({ identical: 1 }),
      }),
      { copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 1, identical: 1, pr: 42 }) },
    ],
    [
      'the same commit, still with only an edited copy',
      partlyCopiedWorktree(),
      read({ unpushed: 1, copies: copyComparison({ identical: 0, edited: 1 }) }),
      {},
    ],
  ])('counts what removing it would cost from %s', (_, wt, details, changed) => {
    expect(asReadNow(wt, details)).toEqual({ ...wt, ...changed })
  })

  it('words copies only while the comparison still finds them, and says why it couldn’t compare', () => {
    const gone = asReadNow(
      copied,
      read({ unpushed: 10, copies: copyComparison({ ref: '', branch: '', identical: 0 }) }),
    )
    const unknown = asReadNow(
      copied,
      read({
        unpushed: 10,
        copies: copyComparison({ compared: false, ref: '', branch: '', identical: 0, note: 'No.' }),
      }),
    )

    expect(copiedOf(copied)).not.toBeNull()
    expect(copiedOf(gone)).toBeNull()
    expect(unpushedKnown(gone)).toBe(10)
    expect(copiedOf(unknown)).toBeNull()
    expect(unknown.copies.note).toBe('No.')
  })
})

describe('notOnGitHub', () => {
  it.each<[string, Worktree, DetailsRead | undefined, number]>([
    ['the listing’s, before a read', ahead, undefined, 2],
    ['the listing’s, after a read that failed', ahead, { state: 'failed', error: 'no' }, 2],
    ['a plain count, not at the head', ahead, read({ unpushed: 2 }), 2],
    ['a fresh count, not at the head', ahead, read({ unpushed: 350 }), 350],
    ['none at the pull request’s head', atHead, read({ unpushed: 3 }), 0],
    ['none at the head before a read', atHead, undefined, 0],
    ['the fresh count once HEAD moved', atHead, read({ unpushed: 4 }), 4],
    ['none when they couldn’t be counted', worktree({ unpushed: -1, onGitHub: false }), undefined, 0],
    ['none on GitHub as copies before a read', copied, undefined, 0],
    ['none on GitHub as copies, the same once compared', copied, read({ unpushed: 10, copies: copyComparison() }), 0],
    [
      'all of them once their copies are gone',
      copied,
      read({ unpushed: 10, copies: copyComparison({ ref: '', branch: '', identical: 0 }) }),
      10,
    ],
    ['none a record vouches for once read', recorded, read({ unpushed: 10 }), 0],
    ['the fresh count past the copies', copied, read({ unpushed: 12 }), 12],
  ])('counts %s', (_, wt, details, want) => {
    expect(notOnGitHub(wt, details)).toBe(want)
  })
})
