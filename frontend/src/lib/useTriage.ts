import type { InboxSnapshot } from '../platform'
import { triageInbox, triageTime, type TriagedInbox } from './triage'
import { useNowAs } from './useNow'

// useTriage sorts the inbox into its tabs against the window's clock, once for the
// whole window, or returns null until the inbox has loaded. It renders again only when
// the clock passes the end of a snooze, which is when the sorting changes; every other
// tick leaves it, and everything that reads it, alone.
export function useTriage(snapshot: InboxSnapshot | null): TriagedInbox | null {
  const at = useNowAs((now) => (snapshot === null ? 0 : triageTime(snapshot, now)))

  return snapshot === null ? null : triageInbox(snapshot, new Date(at))
}
