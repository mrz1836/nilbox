import type { ReviewCommit, TimelineEntry, YourReview } from '../platform'
import { counted } from './format'

// commitsSinceAnchor is the ID of the place in the Commits tab a jump to the commits since
// the user's review goes to: the divider before them, or the note that says why the list
// can't show them.
export const commitsSinceAnchor = 'commits-since-your-review'

// sinceIndex is where the commits pushed since the user's review start among commits: the
// first after the reviewed one, or -1 when the list shows none, as at the head, or when the
// reviewed commit isn't among them.
export function sinceIndex(commits: readonly ReviewCommit[], yours: YourReview | null): number {
  if (yours?.since !== 'pushed') {
    return -1
  }

  const at = commits.findIndex((c) => c.oid.toLowerCase() === yours.commit.toLowerCase())

  return at < 0 || at === commits.length - 1 ? -1 : at + 1
}

// verdictWords says what the user's review was, after "since": "you requested changes" or
// "you approved".
export function verdictWords(yours: Pick<YourReview, 'state'>): string {
  return yours.state === 'APPROVED' ? 'you approved' : 'you requested changes'
}

// sinceWords says what happened to the branch since the user's review, as the sheet's chip
// does, with shown the commits the sheet lists: "3 commits since you requested changes", "1
// commit since you approved", "100+ commits since you requested changes", or "The branch was
// rewritten since you requested changes". It is '' when nothing is known to have happened.
export function sinceWords(yours: YourReview | null, shown: number): string {
  switch (yours?.since) {
    case 'pushed':
      return `${counted(yours.count, 'commit', 'commits')} since ${verdictWords(yours)}`
    case 'beyond':
      return `${String(shown)}+ commits since ${verdictWords(yours)}`
    case 'rewritten':
      return `The branch was rewritten since ${verdictWords(yours)}`
    default:
      return ''
  }
}

// rewritePush is the force push that rewrote the branch after the user's review, the first
// in the conversation the sheet holds, or undefined when it holds none.
export function rewritePush(entries: readonly TimelineEntry[], yours: YourReview | null): TimelineEntry | undefined {
  if (yours?.since !== 'rewritten') {
    return undefined
  }

  return entries.find((e) => e.kind === 'force_pushed' && (yours.at === '' || e.at > yours.at))
}
