import { useState } from 'react'

import { checkServiceStatus, getServiceStatus, type ServiceStatus, type StatusConfig } from '../platform'
import { attempt } from './call'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// ServiceStatusState is the service status check as the window shows it.
export interface ServiceStatusState {
  // status is null until the first reading loads.
  status: ServiceStatus | null
  // checking reports a check the user asked for under way.
  checking: boolean
  // check reads the status pages watched now, as Check again does. A check that fails
  // keeps the last reading and says why in its provider's error.
  check: () => void
}

// useServiceStatus loads where the services the user watches stand, from the readings the
// app takes on its own, and loads it again whenever a pass ends, whenever every, how often
// the settings say to check in seconds, changes, so turning the check off or on shows at
// once, and whenever watch, what the user watches, changes: Go judges the last readings
// again, with no request, so a part ticked shows at once.
export function useServiceStatus(every: number | undefined, watch?: StatusConfig): ServiceStatusState {
  const watching = watch === undefined ? '' : JSON.stringify(watch)
  const status = useQuery(() => getServiceStatus(), [every, watching], { log: 'could not read the service status' })
  const [checking, setChecking] = useState(false)

  useOn('servicestatus:updated', () => {
    void status.refetch()
  })

  const check = () => {
    setChecking(true)
    void attempt(checkServiceStatus()).then((result) => {
      setChecking(false)

      if (result.ok) {
        status.set(() => result.data)
      } else {
        console.error('could not check the service status:', result.error)
      }
    })
  }

  return { status: status.data ?? null, checking, check }
}
