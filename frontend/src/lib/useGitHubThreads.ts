import { useState } from 'react'

import {
  checkGitHubThreadsNow,
  githubThreadsList,
  githubThreadsSnapshot,
  keepGitHubThreads,
  markGitHubThreadsDone,
  markGitHubThreadsRead,
  type GitHubThreadList,
  type GitHubThreadsSnapshot,
} from '../platform'
import { attempt, start, type Result } from './call'
import { useMutation } from './useMutation'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// GitHubThreadsState is the user's GitHub notifications as the window shows them.
export interface GitHubThreadsState {
  // snapshot is what the app mirrors of them, or null until it has loaded.
  snapshot: GitHubThreadsSnapshot | null
  // error is why the mirror could not be read, or ''.
  error: string
  // reload reads the mirror again, from the database alone.
  reload: () => void
  // checkNow reads them from GitHub now (CheckNow) and shows what it found. It resolves
  // with how that ended, and never rejects; checking reports one under way, and
  // checkError why the last one failed. A pass that GitHub refused is no failure: the
  // status says so.
  checkNow: () => Promise<Result<GitHubThreadsSnapshot>>
  checking: boolean
  checkError: string
  // markDone marks the notifications with ids done on GitHub, which takes them out of the
  // user's inbox there, and markRead marks them read, as opening one on GitHub does. Each
  // shows the mirror as GitHub left it, resolves with how it ended, and never rejects;
  // finishing holds every id on its way to done and reading every one on its way to read,
  // and markError says why the latest one failed, until dismissMarkError or the next mark.
  markDone: (ids: readonly number[]) => Promise<Result<GitHubThreadsSnapshot>>
  markRead: (ids: readonly number[]) => Promise<Result<GitHubThreadsSnapshot>>
  finishing: ReadonlySet<number>
  reading: ReadonlySet<number>
  markError: string
  dismissMarkError: () => void
  // list is every notification the app mirrors, read and unread, for the Notifications
  // page, or null until it has been listed: only while listing, and kept as it was once
  // the page stops listing. listError is why it could not be listed, or ''.
  list: GitHubThreadList | null
  listError: string
  // keep takes back the queued clears with clearIDs, the Cleared view's Undo, and shows
  // the mirror as Keep left it. It resolves with how that ended, and never rejects.
  keep: (clearIDs: readonly number[]) => Promise<Result<GitHubThreadsSnapshot>>
}

// noIDs is the empty set, shared so it stays the same object.
const noIDs: ReadonlySet<number> = new Set()

// useGitHubThreads reads what the app mirrors of the user's GitHub notifications, and
// reads it again whenever the shell says it changed. Reading it asks GitHub nothing and
// marks nothing read: only checkNow asks GitHub for a listing, and only markDone,
// markRead, and keep, the user's clicks, change anything there. While listing, as the
// Notifications page's GitHub tab shows, it lists every notification too, again on each
// change and after each mark. The window holds one, in App, which the toolbar's button,
// the page, and the settings share.
export function useGitHubThreads(listing = false): GitHubThreadsState {
  const mirror = useQuery(() => githubThreadsSnapshot(), [], { log: 'could not read the GitHub notifications' })
  const listed = useQuery(() => githubThreadsList(), [], { enabled: listing })

  // reread reads the mirror again, and the list while listing.
  const reread = async () => {
    await Promise.all([mirror.refetch(), listing ? listed.refetch() : Promise.resolve()])
  }

  useOn('ghthreads:updated', () => {
    void reread()
  })

  const checking = useMutation(() => checkGitHubThreadsNow())
  const snapshot = mirror.data ?? null

  const [finishing, setFinishing] = useState<ReadonlySet<number>>(noIDs)
  const [reading, setReading] = useState<ReadonlySet<number>>(noIDs)
  const [markError, setMarkError] = useState('')

  // mark sends call's mark of ids, holding them in hold's set until the mirror has been read
  // again after it: the mark's own answer may be older than a read an event started, so the
  // list shows only what a read made after the mark holds, and a line never comes back
  // between the two.
  const mark = async (
    ids: readonly number[],
    call: (ids: number[]) => PromiseLike<GitHubThreadsSnapshot>,
    hold: (update: (was: ReadonlySet<number>) => ReadonlySet<number>) => void,
  ) => {
    const own = [...ids]

    setMarkError('')
    hold((was) => new Set([...was, ...own]))

    const result = await attempt(start(() => call(own)))

    if (!result.ok) {
      setMarkError(result.error)
    }

    await reread()

    hold((was) => {
      const next = new Set(was)

      for (const id of own) {
        next.delete(id)
      }

      return next.size === 0 ? noIDs : next
    })

    return result
  }

  const checkNow = async () => {
    const result = await checking.run()

    if (result.ok) {
      mirror.set(() => result.data)

      if (listing) {
        void listed.refetch()
      }
    }

    return result
  }

  const keep = async (clearIDs: readonly number[]) => {
    const result = await attempt(start(() => keepGitHubThreads([...clearIDs])))

    if (result.ok) {
      mirror.set(() => result.data)
    }

    return result
  }

  return {
    snapshot,
    error: mirror.error,
    reload: () => {
      void mirror.refetch()
    },
    checkNow,
    checking: checking.pending,
    checkError: checking.error,
    markDone: (ids) => mark(ids, markGitHubThreadsDone, setFinishing),
    markRead: (ids) => mark(ids, markGitHubThreadsRead, setReading),
    finishing,
    reading,
    markError,
    dismissMarkError: () => {
      setMarkError('')
    },
    list: listed.data ?? null,
    listError: listed.error,
    keep,
  }
}
