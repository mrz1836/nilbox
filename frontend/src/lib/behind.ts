import type { Review, ReviewOffer } from '../platform'
import { counted } from './format'

// behindWords says how far an open pull request's branch is behind its base, as the sheet's
// strip says it: "12 commits behind main", "1 commit behind main", or "Behind main" when the
// read couldn't count the commits. It is '' when the branch isn't behind.
export function behindWords(review: Pick<Review, 'behind' | 'behindBy' | 'baseRef'>): string {
  if (!review.behind) {
    return ''
  }

  const base = review.baseRef === '' ? 'its base branch' : review.baseRef

  return review.behindBy > 0 ? `${counted(review.behindBy, 'commit', 'commits')} behind ${base}` : `Behind ${base}`
}

// keepUpOffer is the offer that brings the branch up to date with its base: asking
// Dependabot to rebase, on its own pull request, which keeps it managed, else Update branch,
// which merges the base in; undefined when the sheet offers neither.
export function keepUpOffer(review: Pick<Review, 'offers'>): ReviewOffer | undefined {
  return review.offers.find((o) => o.action === 'rebase_bot') ?? review.offers.find((o) => o.action === 'update_branch')
}
