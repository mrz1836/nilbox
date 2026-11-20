import { useSyncExternalStore } from 'react'

import { inspector, type Inspector, type InspectorState } from '../platform'

// unsubscribed is the subscription of a build without an inspector, which never changes.
const unsubscribed = () => () => undefined

// useInspector is the inspector's part for Settings, which a build from source carries,
// and its state, read again each time it changes; both null in a build without one.
export function useInspector(): { inspector: Inspector | null; state: InspectorState | null } {
  const found = inspector()
  const state = useSyncExternalStore(found?.subscribe ?? unsubscribed, () => found?.state() ?? null)

  return { inspector: found, state }
}
