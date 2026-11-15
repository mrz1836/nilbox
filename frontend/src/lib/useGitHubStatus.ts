import { useEffect, useEffectEvent, useState } from 'react'

import { checkGitHubStatus, getGitHubStatus, on, type GitHubStatus } from '../platform'
import { attempt } from './call'
import { useQuery } from './useQuery'

// GitHubStatusState is GitHub's status page as the window shows it.
export interface GitHubStatusState {
  // status is null until the first reading loads.
  status: GitHubStatus | null
  // checking reports a check the user asked for under way.
  checking: boolean
  // check reads GitHub's status page now, as Check again does. A check that fails keeps
  // the last reading and says why in its error.
  check: () => void
}

// useGitHubStatus loads the last reading of GitHub's status page, which the app takes on
// its own, and loads it again whenever a check ends, and whenever every, how often the
// settings say to check in seconds, changes, so turning the check off or on shows at once.
export function useGitHubStatus(every: number | undefined): GitHubStatusState {
  const status = useQuery(() => getGitHubStatus(), [every], { log: 'could not read GitHub’s status' })
  const [checking, setChecking] = useState(false)
  const reloadOnUpdate = useEffectEvent(() => {
    void status.refetch()
  })

  useEffect(
    () =>
      on('githubstatus:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const check = () => {
    setChecking(true)
    void attempt(checkGitHubStatus()).then((result) => {
      setChecking(false)

      if (result.ok) {
        status.set(() => result.data)
      } else {
        console.error('could not check GitHub’s status:', result.error)
      }
    })
  }

  return { status: status.data ?? null, checking, check }
}
