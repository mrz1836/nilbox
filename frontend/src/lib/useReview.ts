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
  // followRuns reads the pull request again after the sheet approved its held workflow runs,
  // held of them when it did, until their checks start (approvalRetries), and following
  // reports those reads under way or waiting.
  followRuns: (held: number) => void
  following: boolean
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

// Once the sheet approved a pull request's held workflow runs, GitHub takes a few seconds to
// create their checks. The sheet reads again at once, then approvalWait apart, up to
// approvalRetries times, until fewer runs wait for approval and a check is running: at most
// five reads, each of which rewrites the inbox card, so it shows the checks running and the
// inbox's checks follower takes them from there.
export const approvalRetries = 4
export const approvalWait = 3000

// Retries counts the reads again for one pull request.
interface Retries {
  id: number
  count: number
}

// Follow counts the reads after an approval of a pull request's held runs, held of them
// when the sheet approved them.
interface Follow extends Retries {
  held: number
}

// heldRuns counts a pull request's workflow runs waiting for approval.
export function heldRuns(review: Review): number {
  return review.unrun.filter((u) => u.approval).length
}

// runsStarted reports a pull request whose approved workflow runs have started: fewer wait
// for approval than the held when the sheet approved them (GitHub may have refused some),
// and a check is running. One no longer open has nothing left to wait for.
export function runsStarted(review: Review, held: number): boolean {
  return review.state !== 'OPEN' || (heldRuns(review) < held && review.checks.some((c) => c.outcome === 'pending'))
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
  // round counts the reads asked for after the first, for one pull request; force marks
  // one the user asked for, which reads GitHub however recently the pull request was read.
  // Stepping to another pull request starts its rounds afresh, so a reload asked for on one
  // never forces the reads of the next.
  const [round, setRound] = useState({ id, n: 0, force: false })

  if (round.id !== id) {
    setRound({ id, n: 0, force: false })
  }

  const force = round.id === id && round.force
  const refreshed = useQuery(() => (force ? readReviewAgain(id) : refreshReview(id)), [id, round.n])
  const [patches, setPatches] = useState(noPatches)
  const [loading, setLoading] = useState<ReadonlySet<string>>(new Set())
  const [patchError, setPatchError] = useState('')
  const [retries, setRetries] = useState<Retries>({ id, count: 0 })
  // follow counts the reads after an approval, for the pull request it was made on.
  const [follow, setFollow] = useState<Follow | null>(null)

  const mine = (r: Review | undefined) => (r?.id === id ? r : undefined)
  const fresh = mine(refreshed.data)
  const base = fresh ?? mine(opened.data) ?? null
  const review = base === null ? null : withPatches(base, patches)

  const tries = retries.id === id ? retries.count : 0
  const unsure =
    !refreshed.pending && fresh !== undefined && fresh.state === 'OPEN' && fresh.readiness.state === 'unknown'

  // follows is how many reads after an approval of this pull request's runs have run, or -1
  // without one. It ends once a read answers that the runs started, or the last read
  // answers; a read under way stops the wait for the next, and its answer starts it again.
  const follows = follow?.id === id ? follow.count : -1
  const held = follow?.held ?? 0
  const followEnded =
    !refreshed.pending && (fresh === undefined || runsStarted(fresh, held) || follows >= approvalRetries)
  const following = follows >= 0 && !followEnded
  const waitsAgain = following && !refreshed.pending

  useEffect(() => {
    if (!waitsAgain) {
      return
    }

    const timer = setTimeout(() => {
      setFollow({ id, count: follows + 1, held })
      setRound((r) => ({ id, n: r.n + 1, force: true }))
    }, approvalWait)

    return () => {
      clearTimeout(timer)
    }
  }, [waitsAgain, follows, held, id])

  useEffect(() => {
    if (!unsure || tries >= unknownRetries) {
      return
    }

    const timer = setTimeout(() => {
      setRetries({ id, count: tries + 1 })
      setRound((r) => ({ id, n: r.n + 1, force: false }))
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
      // A read the user asked for ends a follow of approved runs: they read it themselves.
      setFollow(null)
      setRound((r) => ({ id, n: r.n + 1, force: true }))
    },
    followRuns: (heldNow) => {
      setFollow({ id, count: 0, held: heldNow })
      setRound((r) => ({ id, n: r.n + 1, force: true }))
    },
    following,
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
