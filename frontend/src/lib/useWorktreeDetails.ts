import { useRef, useState } from 'react'

import {
  worktreeDetails,
  type Worktree,
  type WorktreeCopies,
  type WorktreeCopyComparison,
  type WorktreeDetails,
  type WorktreeFile,
} from '../platform'
import { attempt, start } from './call'
import { copiedOf, unpushedKnown } from './worktreeParts'

// DetailsRead is what the window has of one worktree's details (Worktrees.Details) for the
// latest listing: a read under way, its answer, or why it failed, in which case the drawer
// keeps what the listing had.
export type DetailsRead =
  { state: 'reading' } | { state: 'read'; details: WorktreeDetails } | { state: 'failed'; error: string }

export interface WorktreeDetailsState {
  // of is what the latest listing has read of the worktree at path, or undefined when
  // nothing has asked.
  of: (path: string) => DetailsRead | undefined
  // read asks for the details of wt, a worktree of the clone at repoPath, once a listing:
  // a drawer asks as it opens and again after each new listing, and the Force remove
  // dialog as it opens. A second ask for the same listing does nothing while the first is
  // under way or once it answered, and so does one for a worktree Details can't read
  // (readable); only an ask after one that failed asks again.
  read: (repoPath: string, wt: Worktree) => void
}

// readable reports a worktree Details reads: a linked one whose folder is there and whose
// status git could read. The backend refuses the main checkout and a folder that's gone.
export function readable(wt: Worktree): boolean {
  return !wt.main && !wt.prunable && wt.error === ''
}

// Changes are a worktree's uncommitted files as a drawer lists them: Details' once read,
// which are the current truth (at most 200, how many there are now, and how many of them
// are missing from its folder), else the listing's first 12 and its counts. current reports
// Details'.
export interface Changes {
  files: readonly WorktreeFile[]
  count: number
  missing: number
  current: boolean
}

// changesOf is wt's uncommitted files, with read, what the window has of its details.
// Details that couldn't read the files leave the listing's.
export function changesOf(wt: Worktree, read: DetailsRead | undefined): Changes {
  if (read?.state === 'read' && read.details.fileCount >= 0) {
    return {
      files: read.details.files,
      count: read.details.fileCount,
      missing: read.details.missingCount,
      current: true,
    }
  }

  return { files: wt.files, count: wt.dirtyCount, missing: wt.missingCount, current: false }
}

// asReadNow is wt as its details last read it, for saying what removing it would cost, so
// the drawer's sentence and the confirmations agree with the files and commits listed under
// them: its uncommitted files, those missing from its folder among them, and its commits not
// on GitHub counted as Details counted them, each once it could. Everything else is the
// listing's, the backend's decisions (its can* flags) included, since the backend checks
// again as it removes.
//
// The backend rules commits that no remote-tracking branch holds on GitHub anyway when HEAD
// is its pull request's head commit, as a fork's pull request's head is, or when each has an
// identical copy on a pushed branch, or in the pull request its copies went into, as the app
// recorded them (copies). Details reads no pull request and no record, but compares the
// commits with the pushed branches again (Details.copies), so (commitsNow):
//
//   - commits it found as the listing did keep the listing's word, but copies on a pushed
//     branch only while its comparison ran and found every one with an identical copy on
//     the same branch, as it no longer does once that branch was deleted or force-pushed
//     without them, and copies the app recorded whatever it finds there, as the record
//     vouches for them;
//   - any others mean HEAD moved, and only none puts them on GitHub.
//
// Whatever no longer holds is counted as Details counted it, and its copies as it compared
// them, with why when it couldn't.
export function asReadNow(wt: Worktree, read: DetailsRead | undefined): Worktree {
  if (read?.state !== 'read') {
    return wt
  }

  const { fileCount, missingCount } = read.details
  const files = fileCount >= 0 ? { dirty: fileCount > 0, dirtyCount: fileCount, missingCount } : {}

  return { ...wt, ...files, ...commitsNow(wt, read.details) }
}

// commitsNow is what details, as asReadNow reads them, change of wt's commits not on GitHub
// and their copies: nothing when they couldn't be counted, or when the listing's word on
// them holds, else their count, whether they're on GitHub, and the copies Details found.
function commitsNow(wt: Worktree, details: WorktreeDetails): Partial<Worktree> {
  const { unpushed } = details

  if (unpushed < 0) {
    return {}
  }

  const same = sameCommits(wt, details)
  const byCopies = copiedOf(wt) !== null

  if (same && (wt.copies.recorded || (byCopies && copiesHold(wt, details.copies)))) {
    return {}
  }

  return {
    unpushed,
    onGitHub: unpushed === 0 || (same && wt.onGitHub && !byCopies),
    copies: copiesNow(wt, details),
  }
}

// sameCommits reports details that found, as far as they tell, the commits not on GitHub
// the listing found on wt: as many, and the newest of those they list, which git lists
// first, at the HEAD the listing found. Details that counted them but couldn't list them
// tell only how many.
function sameCommits(wt: Worktree, details: WorktreeDetails): boolean {
  const newest = details.commits.at(0)

  return details.unpushed === wt.unpushed && (newest === undefined || newest.oid === wt.head)
}

// copiesHold reports a comparison, compared, that still finds what the listing found of
// wt's commits on a pushed branch: it ran, and every one of them has an identical copy on
// the branch the listing compared them with.
function copiesHold(wt: Worktree, compared: WorktreeCopyComparison): boolean {
  return compared.compared && compared.ref === wt.copies.ref && compared.identical === wt.unpushed
}

// copiesNow is what Details' comparison found of the copies of its count commits not on
// GitHub, as Worktree.copies says it: none recorded, and the pull request the listing found
// the branch heads, while it's the same branch.
function copiesNow(wt: Worktree, details: WorktreeDetails): WorktreeCopies {
  const c = details.copies

  return {
    ref: c.ref,
    branch: c.branch,
    count: details.unpushed,
    identical: c.identical,
    edited: c.edited,
    none: c.compared ? details.unpushed - c.identical - c.edited : 0,
    recorded: false,
    pr: c.ref !== '' && c.ref === wt.copies.ref ? wt.copies.pr : 0,
    gone: false,
    note: c.note,
  }
}

// notOnGitHub is how many commits on wt aren't on GitHub, as its details count them now
// once read, else as the listing did (asReadNow): 0 when every one is, at its pull
// request's head or as identical copies included, or when they couldn't be counted. The
// drawer's commits and Force remove's count and list them by it, so they agree with the
// sentence over them.
export function notOnGitHub(wt: Worktree, read: DetailsRead | undefined): number {
  return unpushedKnown(asReadNow(wt, read))
}

// Reads are the details read for one listing, by the worktree's path.
interface Reads {
  listing: number
  byPath: ReadonlyMap<string, DetailsRead>
}

// noReads is the empty map, shared so it stays the same object.
const noReads: ReadonlyMap<string, DetailsRead> = new Map()

// useWorktreeDetails keeps each worktree's details, read on demand, for one listing, which
// listing numbers (useWorktrees' listing): a new listing drops them all, so a drawer opened
// again costs nothing until the next one, and an answer that lands for an older listing is
// dropped. It never has two asks under way for a worktree, nor asks again in one listing
// once one answered, StrictMode's doubled effects included; a read that failed is asked
// again only when something reads it again (a drawer opening, Force remove's dialog), never
// on its own. It never asks GitHub: Details reads local git alone.
export function useWorktreeDetails(listing: number): WorktreeDetailsState {
  const [reads, setReads] = useState<Reads>({ listing, byPath: noReads })
  // asked are the paths asked about for the listing it names, whose asks are under way or
  // answered; a failed one's path leaves it.
  const asked = useRef<{ listing: number; paths: Set<string> } | null>(null)

  // A new listing drops what the last one read. Setting state while rendering, as React
  // suggests for state that follows a changed prop, does it without an extra render.
  if (reads.listing !== listing) {
    setReads({ listing, byPath: noReads })
  }

  const byPath = reads.listing === listing ? reads.byPath : noReads

  // keep records a read of the worktree at path for the listing at, unless a newer
  // listing has come since.
  const keep = (at: number, path: string, read: DetailsRead) => {
    setReads((was) => (was.listing === at ? { listing: at, byPath: new Map(was.byPath).set(path, read) } : was))
  }

  const ask = async (repoPath: string, wt: Worktree) => {
    const at = listing

    keep(at, wt.path, { state: 'reading' })

    const answer = await attempt(start(() => worktreeDetails(repoPath, wt.path)))

    if (!answer.ok && asked.current?.listing === at) {
      asked.current.paths.delete(wt.path)
    }

    keep(at, wt.path, answer.ok ? { state: 'read', details: answer.data } : { state: 'failed', error: answer.error })
  }

  return {
    of: (path) => byPath.get(path),
    read: (repoPath, wt) => {
      if (!readable(wt)) {
        return
      }

      if (asked.current?.listing !== listing) {
        asked.current = { listing, paths: new Set() }
      }

      if (asked.current.paths.has(wt.path)) {
        return
      }

      asked.current.paths.add(wt.path)
      void ask(repoPath, wt)
    },
  }
}
