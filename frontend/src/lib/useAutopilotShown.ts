import { useEffect, useEffectEvent } from 'react'

import { getAutopilotGlance, on } from '../platform'
import { useQuery } from './useQuery'

// useAutopilotShown reports whether the window names Autopilot at all: only while at least
// one routine exists, so a user without one never sees it (its banner switches, the
// Notifications page's Autopilot type). It reads Autopilot at a glance as it mounts, and
// again whenever Autopilot changes; it is false until the first read answers, and after one
// that fails.
export function useAutopilotShown(): boolean {
  const glance = useQuery(() => getAutopilotGlance(), [], { log: 'could not read whether a routine exists' })
  const reloadOnUpdate = useEffectEvent(() => {
    void glance.refetch()
  })

  useEffect(
    () =>
      on('autopilot:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  return (glance.data?.routines ?? 0) > 0
}
