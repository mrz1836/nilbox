import { behindWords, keepUpOffer } from '../../lib/behind'
import type { Review, ReviewAction } from '../../platform'
import { buttonClass } from '../controls'

export interface BehindBaseProps {
  review: Review
  busy: boolean
  // onRun runs the offer that brings the branch up to date, as the sheet's button would:
  // one that asks first opens its confirmation.
  onRun: (action: ReviewAction) => void
}

// BehindBase says, in the sheet's notice strip, that an open pull request's branch is behind
// its base, and by how many commits when the sheet could count them, beside the button that
// brings it up to date (asking Dependabot to rebase its own, else Update branch), or why the
// user can't. It is amber while the rules hold the merge back for it, and otherwise only
// says so: a branch behind its base merges all the same there. It shows nothing for a branch
// up to date, or once the pull request is closed.
export function BehindBase({ review, busy, onRun }: BehindBaseProps) {
  const words = behindWords(review)

  if (review.state !== 'OPEN' || words === '') {
    return null
  }

  const offer = keepUpOffer(review)
  const blocks = review.readiness.state === 'behind'

  return (
    <section
      aria-label="Behind its base"
      className={`mt-2 flex items-center gap-3 rounded-md border px-3 py-2 text-sm ${
        blocks ? 'border-warning/60 text-warning' : 'border-border text-text'
      }`}
    >
      <span className="flex-1">{words}</span>
      {offer !== undefined &&
        (offer.enabled ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              onRun(offer.action)
            }}
            className={buttonClass}
          >
            {offer.label}
            {offer.confirm && '…'}
          </button>
        ) : (
          <span className="max-w-[18rem] shrink-0 text-xs text-muted">{offer.reason}</span>
        ))}
    </section>
  )
}
