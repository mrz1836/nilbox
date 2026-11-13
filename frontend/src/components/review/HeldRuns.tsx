import { counted } from '../../lib/format'
import type { Review } from '../../platform'
import { buttonClass } from '../controls'
import { RunNames } from './ReviewSummary'

export interface HeldRunsProps {
  review: Review
  // notice is what the sheet's approval of the runs came to, as GitHub answered it, or ''.
  notice: string
  // following reports the sheet reading the pull request again, waiting for the approved
  // runs' checks to start.
  following: boolean
  // error is why the sheet's last approval failed, as GitHub said it, or ''.
  error: string
  busy: boolean
  onApprove: () => void
}

// HeldRuns is the bar under the sheet's header while GitHub holds an open pull request's
// workflow runs for a maintainer's approval, as it does a fork's: it names them, each a
// link to its run's page on GitHub, beside the button that approves them, or why the user
// can't. Once the sheet approved them, it says what GitHub made of it while the sheet waits
// for their checks to start, and names any run still held. GitHub otherwise counts the
// runs as not passing though no check shows them, and the inbox's card can read as passed.
export function HeldRuns({ review, notice, following, error, busy, onApprove }: HeldRunsProps) {
  const held = following ? [] : review.unrun.filter((u) => u.approval)
  const offer = review.offers.find((o) => o.action === 'approve_runs')

  if (review.state !== 'OPEN' || (held.length === 0 && notice === '')) {
    return null
  }

  return (
    <section
      aria-label="Workflow runs awaiting approval"
      className={`mt-2 flex items-center gap-3 rounded-md border px-3 py-2 text-sm ${
        held.length > 0 ? 'border-warning/60 text-warning' : 'border-border text-text'
      }`}
    >
      <div className="min-w-0 flex-1 space-y-0.5">
        {notice !== '' && (
          <p role="status">
            {notice}.{following && ' Waiting for their checks to start…'}
          </p>
        )}
        {held.length > 0 && (
          <p>
            {counted(held.length, 'workflow run is', 'workflow runs are')}
            {notice === '' ? ' waiting for approval: ' : ' still waiting for approval: '}
            <span className="text-muted">
              <RunNames runs={held} />
            </span>
          </p>
        )}
        {error !== '' && (
          <p role="alert" className="text-danger">
            {error}
          </p>
        )}
      </div>
      {held.length > 0 &&
        offer !== undefined &&
        (offer.enabled ? (
          <button type="button" disabled={busy} onClick={onApprove} className={buttonClass}>
            {offer.label}
          </button>
        ) : (
          <span className="max-w-[18rem] shrink-0 text-xs text-muted">{offer.reason}</span>
        ))}
    </section>
  )
}
