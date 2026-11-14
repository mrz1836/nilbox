import { useEffect, useEffectEvent } from 'react'

import { checkGitHubThreadsNow, githubThreadsSnapshot, on, type GitHubThreadsSnapshot } from '../platform'
import type { Result } from './call'
import { useMutation } from './useMutation'
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
}

// useGitHubThreads reads what the app mirrors of the user's GitHub notifications, and
// reads it again whenever the shell says it changed. Reading it asks GitHub nothing and
// marks nothing read: only checkNow asks GitHub, and only for a listing. The window holds
// one, in App, which the toolbar's button and the settings share.
export function useGitHubThreads(): GitHubThreadsState {
  const mirror = useQuery(() => githubThreadsSnapshot(), [], { log: 'could not read the GitHub notifications' })
  const reloadOnUpdate = useEffectEvent(() => {
    void mirror.refetch()
  })

  useEffect(
    () =>
      on('ghthreads:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const checking = useMutation(() => checkGitHubThreadsNow())
  const snapshot = mirror.data ?? null

  const checkNow = async () => {
    const result = await checking.run()

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
  }
}
