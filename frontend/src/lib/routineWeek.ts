// A routine's week, as its This week says it: what its runs of the last seven days did,
// counted, then what they spent. The dollars are an estimate: an agent signed in with a
// subscription reports what the same work would cost through the API, while the tokens are
// what each run's stream reported. A run in the user's terminal reports neither; its
// session's tokens show on the item's session panel.

import type { AutopilotStats } from '../platform'
import { counted } from './format'
import { compactCount } from './sessions'

// weekCounts says what the runs did, the counts that aren't zero, in the order a reader
// asks about them: "12 agent runs, 3 fixes, 1 merge, 4 GitHub actions"; [] for a week with
// none.
export function weekCounts(s: AutopilotStats): string[] {
  const parts: [number, string, string][] = [
    [s.runs, 'agent run', 'agent runs'],
    [s.fixes, 'fix', 'fixes'],
    [s.merges, 'merge', 'merges'],
    [s.reviews, 'review posted', 'reviews posted'],
    [s.actions, 'GitHub action', 'GitHub actions'],
    [s.scripts, 'script run', 'script runs'],
  ]

  return parts.filter(([n]) => n > 0).map(([n, one, many]) => counted(n, one, many))
}

// costWords says what the runs that reported a cost spent, always as an estimate, and how
// they reported it: "about $1.20, as 2 runs estimated it" when each gave the agent's
// estimate of what the API would charge, "as 2 runs reported it" when each was charged
// through an API key, and "as 3 runs reported it, 2 by estimate" for both; '' when none
// reported one.
export function costWords(s: AutopilotStats): string {
  if (s.costUsd === null) {
    return ''
  }

  const dollars = new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD' }).format(s.costUsd)
  const runs = s.costed === 1 ? 'its run' : `${String(s.costed)} runs`

  if (s.estimated >= s.costed) {
    return `about ${dollars}, as ${runs} estimated it`
  }

  const guessed = s.estimated > 0 ? `, ${String(s.estimated)} by estimate` : ''

  return `about ${dollars}, as ${runs} reported it${guessed}`
}

// tokenWords says how many tokens the runs used, "1.2M tokens, 380K of them from the
// cache"; '' when none reported any.
export function tokenWords(s: AutopilotStats): string {
  const total = s.tokensIn + s.tokensOut + s.cacheRead
  if (total <= 0) {
    return ''
  }

  const cached = s.cacheRead > 0 ? `, ${compactCount(s.cacheRead)} of them read from the cache` : ''

  return `${compactCount(total)} ${total === 1 ? 'token' : 'tokens'}${cached}`
}
