import { sinceWords } from '../../lib/commits'
import type { Review } from '../../platform'
import { buttonClass } from '../controls'
import { CommitIcon } from '../icons'

export interface SinceYourReviewProps {
  review: Review
  // onShow shows the commits since the review, in the Commits tab.
  onShow: () => void
  busy: boolean
  // onRerequest asks GitHub for the user's review again.
  onRerequest: () => void
}

// SinceYourReview says, in the sheet's notice strip, what happened to an open pull request's
// branch since the user's own standing review: commits pushed on top of the one they
// reviewed, or the branch rewritten, in amber, with a button to the commits. After a request
// for changes, it offers to ask GitHub for the user's review again (Re-request my review),
// or says why it can't, as when GitHub asks already. It shows nothing when nothing is known
// to have happened, or once the pull request is closed.
export function SinceYourReview({ review, onShow, busy, onRerequest }: SinceYourReviewProps) {
  const words = sinceWords(review.yourReview, review.commitList.length)

  if (review.state !== 'OPEN' || words === '') {
    return null
  }

  const rewritten = review.yourReview?.since === 'rewritten'
  const again = review.offers.find((o) => o.action === 'rerequest_review')

  return (
    <div
      className={`mt-2 flex items-center gap-3 rounded-md border px-3 py-2 text-sm ${
        rewritten ? 'border-warning/60 text-warning' : 'border-accent/60 text-accent'
      }`}
    >
      <CommitIcon className="size-4 shrink-0" />
      <span className="flex-1">{words}</span>
      <button type="button" onClick={onShow} className={buttonClass}>
        Show commits
      </button>
      {again !== undefined &&
        (again.enabled ? (
          <button type="button" disabled={busy} onClick={onRerequest} className={buttonClass}>
            {again.label}
          </button>
        ) : (
          <span className="max-w-[18rem] shrink-0 text-xs text-muted">{again.reason}</span>
        ))}
    </div>
  )
}
