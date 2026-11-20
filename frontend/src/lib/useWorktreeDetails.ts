import { useRef, useState } from 'react'

import { worktreeDetails, type Worktree, type WorktreeDetails, type WorktreeFile } from '../platform'
import { attempt, start } from './call'

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
  // dialog as it opens. A second ask for the same listing does nothing, and so does one
  // for a worktree Details can't read (readable).
  read: (repoPath: string, wt: Worktree) => void
}

// readable reports a worktree Details reads: a linked one whose folder is there and whose
// status git could read. The backend refuses the main checkout and a folder that's gone.
export function readable(wt: Worktree): boolean {
  return !wt.main && !wt.prunable && wt.error === ''
}

// Changes are a worktree's uncommitted files as a drawer lists them: Details' once read,
// which are the current truth (at most 200, and how many there are now), else the
// listing's first 12 and its count. current reports Details'.
export interface Changes {
  files: readonly WorktreeFile[]
  count: number
  current: boolean
}

// changesOf is wt's uncommitted files, with read, what the window has of its details.
// Details that couldn't read the files leave the listing's.
export function changesOf(wt: Worktree, read: DetailsRead | undefined): Changes {
  if (read?.state === 'read' && read.details.fileCount >= 0) {
    return { files: read.details.files, count: read.details.fileCount, current: true }
  }

  return { files: wt.files, count: wt.dirtyCount, current: false }
}

// asReadNow is wt as its details last read it, for saying what removing it would cost, so
// the drawer's sentence and the confirmations agree with the files and commits listed under
// them: its uncommitted files and its commits not on GitHub counted as Details counted them,
// each once it could. Everything else is the listing's, the backend's decisions (its can*
// flags) included, since the backend checks again as it removes.
export function asReadNow(wt: Worktree, read: DetailsRead | undefined): Worktree {
  if (read?.state !== 'read') {
    return wt
  }

  const { fileCount, unpushed } = read.details
  const files = fileCount >= 0 ? { dirty: fileCount > 0, dirtyCount: fileCount } : {}
  const commits = unpushed >= 0 ? { unpushed, onGitHub: unpushed === 0 } : {}

  return { ...wt, ...files, ...commits }
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
// dropped. It never asks twice for a worktree in one listing, StrictMode's doubled effects
// included, and never asks GitHub: Details reads local git alone.
export function useWorktreeDetails(listing: number): WorktreeDetailsState {
  const [reads, setReads] = useState<Reads>({ listing, byPath: noReads })
  // asked are the paths already asked about, for the listing it names.
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
