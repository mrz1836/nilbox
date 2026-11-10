import { useEffect, useEffectEvent } from 'react'

import { checkIntegrationsAgain, listIntegrations, on, type IntegrationTools } from '../platform'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'

// IntegrationsState is the tools the app runs for the user, as the settings show them.
export interface IntegrationsState {
  // tools is what the integrations service lists, or null until it has loaded.
  tools: IntegrationTools | null
  // error is why the tools could not be listed, or ''.
  error: string
  // checking reports a Check again under way, and checkError why the last one failed,
  // or ''.
  checking: boolean
  checkError: string
  // checkAgain reads the login shell's environment, looks for every tool, and checks for
  // newer releases again; it resolves once the list shows what it found.
  checkAgain: () => Promise<void>
  // reload reads the list again, as after a change to a tool's settings; it resolves once
  // the list is in.
  reload: () => Promise<void>
  // replace shows tools in place of the list, as an install or a move to the Trash
  // returns them, without reading it again.
  replace: (tools: IntegrationTools) => void
}

// useIntegrations lists the tools as the settings open, and again whenever the shell says
// a detection or an update check ended. Listing never waits for a detection: until one
// ends, each tool shows what the last run found, marked as checking.
export function useIntegrations(): IntegrationsState {
  const list = useQuery(() => listIntegrations(), [])
  const checker = useMutation(() => checkIntegrationsAgain())
  const reloadOnUpdate = useEffectEvent(() => {
    void list.refetch()
  })

  useEffect(
    () =>
      on('tools:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const checkAgain = async () => {
    const result = await checker.run()

    if (result.ok) {
      list.set(() => result.data)
    }
  }

  return {
    tools: list.data ?? null,
    error: list.error,
    checking: checker.pending,
    checkError: checker.error,
    checkAgain,
    reload: list.refetch,
    replace: (tools) => {
      list.set(() => tools)
    },
  }
}
