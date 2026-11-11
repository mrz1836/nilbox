// useReview loads the review sheet's pull request: at once from what the sheet read
// before (Open, which asks GitHub nothing), then from GitHub (Refresh), and the diffs
// Open left out, on demand.

import { useEffect, useState } from 'react'

import {
  openReview,
  readReviewAgain,
  refreshReview,
  reviewPatches,
  type Review,
  type ReviewFilePatch,
} from '../platform'
import { attempt } from './call'
import { useQuery } from './useQuery'

export interface ReviewState {
  // review is the pull request as last read, or null until the first read answers.
  review: Review | null
  // checking reports a read from GitHub under way.
  checking: boolean
  // error is why the last read failed, or ''.
  error: string
  // reload reads the pull request from GitHub again, however recently it was read: the
  // user asked for it.
  reload: () => void
  // loadPatches reads the diffs of paths that Open left out; loading reports those under
  // way, and patchError why the last read of them failed.
  loadPatches: (paths: readonly string[]) => Promise<void>
  loading: ReadonlySet<string>
  patchError: string
}

// Patches are the diffs read on demand for one head, by path.
interface Patches {
  head: string
  byPath: ReadonlyMap<string, ReviewFilePatch>
}

const noPatches: Patches = { head: '', byPath: new Map() }

// GitHub works out whether a pull request can merge only once asked, so a read right after
// a push can find it still working. The sheet reads again, unknownWait apart, up to
// unknownRetries times, rather than leave the merge offers off until it is reopened.
export const unknownRetries = 2
export const unknownWait = 2000

// Retries counts the reads again for one pull request.
interface Retries {
  id: number
  count: number
}

// withPatches returns review with the diffs read on demand for its head filled in.
function withPatches(review: Review, patches: Patches): Review {
  if (patches.head !== review.head || patches.byPath.size === 0) {
    return review
  }

  return {
    ...review,
    files: review.files.map((f) => {
      const p = patches.byPath.get(f.path)

      return p === undefined ? f : { ...f, patch: p.patch, patchState: p.patchState }
    }),
  }
}

// useReview loads the pull request with id. An answer about another pull request, from
// before the sheet stepped to this one, is never shown.
export function useReview(id: number): ReviewState {
  const opened = useQuery(() => openReview(id), [id])
  // round counts the reads asked for after the first; force marks one the user asked for,
  // which reads GitHub however recently the pull request was read.
  const [round, setRound] = useState({ n: 0, force: false })
  const refreshed = useQuery(() => (round.force ? readReviewAgain(id) : refreshReview(id)), [id, round.n])
  const [patches, setPatches] = useState(noPatches)
  const [loading, setLoading] = useState<ReadonlySet<string>>(new Set())
  const [patchError, setPatchError] = useState('')
  const [retries, setRetries] = useState<Retries>({ id, count: 0 })

  const mine = (r: Review | undefined) => (r?.id === id ? r : undefined)
  const fresh = mine(refreshed.data)
  const base = fresh ?? mine(opened.data) ?? null
  const review = base === null ? null : withPatches(base, patches)

  const tries = retries.id === id ? retries.count : 0
  const unsure =
    !refreshed.pending && fresh !== undefined && fresh.state === 'OPEN' && fresh.readiness.state === 'unknown'

  useEffect(() => {
    if (!unsure || tries >= unknownRetries) {
      return
    }

    const timer = setTimeout(() => {
      setRetries({ id, count: tries + 1 })
      setRound((r) => ({ n: r.n + 1, force: false }))
    }, unknownWait)

    return () => {
      clearTimeout(timer)
    }
  }, [unsure, tries, id])

  return {
    review,
    checking: refreshed.pending,
    error: refreshed.error === '' ? opened.error : refreshed.error,
    reload: () => {
      setRound((r) => ({ n: r.n + 1, force: true }))
    },
    loading,
    patchError,
    loadPatches: async (paths) => {
      if (review === null || paths.length === 0) {
        return
      }

      const head = review.head

      setLoading((was) => new Set([...was, ...paths]))

      const result = await attempt(reviewPatches(id, head, [...paths]))

      setLoading((was) => new Set([...was].filter((p) => !paths.includes(p))))

      if (!result.ok) {
        setPatchError(result.error)

        return
      }

      setPatchError('')
      setPatches((was) => {
        const byPath = new Map(was.head === head ? was.byPath : [])

        for (const p of result.data) {
          byPath.set(p.path, p)
        }

        return { head, byPath }
      })
    },
  }
}
