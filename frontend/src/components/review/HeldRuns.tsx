import { counted } from '../../lib/format'
import { approvableRuns, onGitHubRuns, type ListMark } from '../../lib/useReview'
import { openExternal, type Review, type ReviewUnrun } from '../../platform'
import { buttonClass, quietButtonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { ReadAge } from './ReviewLoading'
import { IgnoreWorkflows, RunNames, type IgnoreProps } from './ReviewSummary'

export interface HeldRunsProps extends IgnoreProps {
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
  // mark is how the runs the sheet read stand against what it shows around them (listMark):
  // current, unless given.
  mark?: ListMark
}

// HeldRuns is the bar under the sheet's header while GitHub holds an open pull request's
// workflow runs for a maintainer's approval, as it does a fork's: it names them, each a
// link to its run's page on GitHub, beside the button that approves them, or why the user
// can't, and offers to ignore their workflows. Runs GitHub approves only on its site (a run
// a review started in the base repository, say) show here only while GitHub reports the
// merge blocked, when they may be what blocks it: named apart, as waiting for approval on
// GitHub, with a link there and no button. Runs that don't count (ignoredRuns) never show
// here; the checks list names them quietly. Once the sheet approved them, it says what
// GitHub made of it while the sheet waits for their checks to start, and names any run
// still held. GitHub otherwise counts the runs as not passing though no check shows them,
// and the inbox's card can read as passed. Runs read before the inbox's newer counts say
// when they were read (ReadAge), faded while the sheet reads them again.
export function HeldRuns({
  review,
  notice,
  following,
  error,
  busy,
  onApprove,
  ignored,
  onIgnore,
  mark = 'current',
}: HeldRunsProps) {
  const held = following ? [] : approvableRuns(review)
  const elsewhere = following ? [] : onGitHubRuns(review)
  const offer = review.offers.find((o) => o.action === 'approve_runs')

  if (review.state !== 'OPEN' || (held.length === 0 && elsewhere.length === 0 && notice === '')) {
    return null
  }

  return (
    <section
      aria-label="Workflow runs awaiting approval"
      className={`mt-2 flex items-center gap-3 rounded-md border px-3 py-2 text-sm ${
        held.length + elsewhere.length > 0 ? 'border-warning/60 text-warning' : 'border-border text-text'
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
            </span>{' '}
            <IgnoreWorkflows runs={held} ignored={ignored} onIgnore={onIgnore} />
          </p>
        )}
        {elsewhere.length > 0 && (
          <p>
            {counted(elsewhere.length, 'workflow run is', 'workflow runs are')}
            {' waiting for approval on GitHub: '}
            <span className="text-muted">
              <RunNames runs={elsewhere} />
            </span>{' '}
            <button
              type="button"
              onClick={() => {
                openExternal(onGitHubPage(review, elsewhere))
              }}
              className={quietButtonClass}
            >
              <ExternalIcon className="size-3" /> Open on GitHub
            </button>
            <IgnoreWorkflows runs={elsewhere} ignored={ignored} onIgnore={onIgnore} />
          </p>
        )}
        {elsewhere.length > 0 && (
          <p className="text-xs">
            GitHub reports the merge blocked, so {elsewhere.length === 1 ? 'it' : 'they'} may be what holds it back.
          </p>
        )}
        {held.length + elsewhere.length > 0 && <ReadAge mark={mark} fetchedAt={review.fetchedAt} what="held runs" />}
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

// onGitHubPage is where the user approves runs held for approval on GitHub: the one run's
// page, or else the pull request's checks.
function onGitHubPage(review: Review, runs: readonly ReviewUnrun[]): string {
  const pages = new Set(runs.map((u) => u.url))
  const [page] = pages

  return pages.size === 1 && page !== undefined && page !== '' ? page : `${review.url}/checks`
}
