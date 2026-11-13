import { counted } from '../../lib/format'
import { refText } from '../../lib/format'
import { issueLook } from '../../lib/issues'
import { toneDot, toneText, type Tone } from '../../lib/signals'
import type { InboxItem, ReadyState, Review, ReviewCheck, ReviewEntry, ReviewUnrun } from '../../platform'
import { openExternal } from '../../platform'
import { chipClass } from '../controls'
import { ItemLink } from '../ItemLink'
import { Markdown } from '../Markdown'
import { SensitiveFiles } from '../SensitiveFiles'
import { StatusDot } from '../StatusDot'

export interface ReviewSummaryProps {
  review: Review
  // item is the inbox card, whose sensitive-file check the summary shows.
  item: InboxItem | undefined
  onFilesSettings?: (() => void) | undefined
}

// readyTone is each readiness's color.
const readyTone: Readonly<Record<ReadyState, Tone>> = {
  ready: 'success',
  unstable: 'warning',
  blocked: 'warning',
  behind: 'warning',
  conflicts: 'danger',
  draft: 'muted',
  queue: 'muted',
  auto_merge: 'success',
  unknown: 'muted',
  closed: 'muted',
}

// reviewWord names a review's state.
function reviewWord(state: string): string {
  switch (state) {
    case 'APPROVED':
      return 'approved'
    case 'CHANGES_REQUESTED':
      return 'requested changes'
    case 'DISMISSED':
      return 'dismissed'
    default:
      return 'commented'
  }
}

// runningOutcome is where a check GitHub has not finished counts: queued or in progress.
const runningOutcome = 'pending'

// checkTone is a check's color, by where it counts.
function checkTone(outcome: string): Tone {
  switch (outcome) {
    case 'passed':
      return 'success'
    case 'failed':
      return 'danger'
    case runningOutcome:
      return 'warning'
    default:
      return 'muted'
  }
}

// CheckDot is the dot of a check with outcome, in its tone: a ring swells slowly out of it
// while the check runs, as out of a working session's. The count of the checks running
// carries the running one, so the list and its summary always match.
function CheckDot({ outcome }: { outcome: string }) {
  return <StatusDot color={toneDot[checkTone(outcome)]} ping={outcome === runningOutcome} />
}

// checksSummary sums up the checks: how many there are and how many fail, as in "12 checks ·
// 1 failing", and how many run, as in "2 running", or '' while none does, which the summary
// marks with a running check's dot.
export function checksSummary(checks: readonly ReviewCheck[]): { counts: string; running: string } {
  const failing = checks.filter((c) => c.outcome === 'failed').length
  const pending = checks.filter((c) => c.outcome === runningOutcome).length
  const parts = [counted(checks.length, 'check', 'checks')]

  if (failing > 0) {
    parts.push(`${String(failing)} failing`)
  }

  return { counts: parts.join(' · '), running: pending > 0 ? `${String(pending)} running` : '' }
}

// BumpChip says what a dependency update changes: the dependency and its versions, and how
// big the jump is, a major one in amber.
function BumpChip({ review }: { review: Review }) {
  const bump = review.bump

  if (bump === null) {
    return null
  }

  const what =
    bump.name === ''
      ? [
          bump.group === '' ? 'dependencies' : `${bump.group} group`,
          bump.count > 0 ? counted(bump.count, 'update', 'updates') : '',
        ]
          .filter((s) => s !== '')
          .join(' · ')
      : [bump.name, bump.from === '' ? bump.to : `${bump.from} → ${bump.to}`].filter((s) => s !== '').join(' ')
  const level = bump.level === '' ? '' : ` · ${bump.level}`
  const tone = bump.level === 'major' ? 'text-warning border-warning/60' : ''

  return (
    <span className={`${chipClass} font-mono ${tone}`} title="What the dependency update's title says">
      {what}
      {level}
      {bump.security && ' · security'}
    </span>
  )
}

// areasText names the folders a change touches, as in "wallet/ and the top level": the
// files at the repository's root are its top level.
export function areasText(areas: readonly string[]): string {
  const names = areas.map((a) => (a === '.' ? 'the top level' : `${a}/`))
  const last = names.at(-1)

  return names.length < 2 || last === undefined ? names.join('') : `${names.slice(0, -1).join(', ')} and ${last}`
}

// CheckList lists checks, each with its dot and whether it is required; each opens its
// page.
function CheckList({ checks }: { checks: readonly ReviewCheck[] }) {
  return (
    <ul className="mt-1 space-y-0.5">
      {checks.map((c, i) => (
        <li key={`${c.name}-${String(i)}`} className="flex min-w-0 items-center gap-1.5">
          <CheckDot outcome={c.outcome} />
          {c.url === '' ? (
            <span className="truncate">{c.name}</span>
          ) : (
            <button
              type="button"
              title={c.url}
              onClick={() => {
                openExternal(c.url)
              }}
              className="truncate text-left hover:text-accent hover:underline"
            >
              {c.name}
            </button>
          )}
          {c.required && <span className="shrink-0 text-muted">required</span>}
        </li>
      ))}
    </ul>
  )
}

// Unrun names the head commit's workflow runs that ended without a check and without
// passing, which GitHub counts as not passing though no check shows them: those waiting for
// a maintainer's approval, as a fork's runs do, and those that failed to start, each
// workflow named once.
function Unrun({ unrun }: { unrun: readonly ReviewUnrun[] }) {
  const lines = [
    { runs: unrun.filter((u) => u.approval), words: 'awaiting approval' },
    { runs: unrun.filter((u) => !u.approval), words: 'that didn’t start' },
  ].filter((l) => l.runs.length > 0)

  if (lines.length === 0) {
    return null
  }

  return (
    <ul aria-label="Workflow runs with no check" className="mt-1.5 space-y-0.5 text-xs text-warning">
      {lines.map(({ runs, words }) => (
        <li key={words}>
          {counted(runs.length, 'workflow run', 'workflow runs')} {words}
          {': '}
          <span className="text-muted">{[...new Set(runs.map((u) => u.name || 'an app'))].join(', ')}</span>
        </li>
      ))}
    </ul>
  )
}

// Checks lists the head commit's checks that ran: the failing ones first, then those
// running, then those passed. The ones skipped, which say nothing about the change, fold
// away below them under their count. The list starts open, and folds away under its count,
// which marks the checks running with their dot, so a folded list still shows CI at work.
// Beneath it, the workflow runs that never ran a check and aren't passing (Unrun).
function Checks({ checks, unrun }: { checks: readonly ReviewCheck[]; unrun: readonly ReviewUnrun[] }) {
  if (checks.length === 0) {
    return (
      <>
        <p className="text-xs text-muted">No checks on this commit.</p>
        <Unrun unrun={unrun} />
      </>
    )
  }

  const order = { failed: 0, pending: 1, passed: 2 } as Record<string, number>
  const ran = checks.filter((c) => c.outcome in order).sort((a, b) => (order[a.outcome] ?? 0) - (order[b.outcome] ?? 0))
  const skipped = checks.filter((c) => !(c.outcome in order))
  const summary = checksSummary(checks)

  return (
    <details open className="text-xs">
      <summary className="cursor-default text-muted select-none">
        {summary.counts}
        {summary.running !== '' && (
          <>
            {' · '}
            <CheckDot outcome={runningOutcome} /> {summary.running}
          </>
        )}
      </summary>
      <CheckList checks={ran} />
      {skipped.length > 0 && (
        <details className="mt-1">
          <summary className="cursor-default text-muted select-none">{String(skipped.length)} skipped</summary>
          <CheckList checks={skipped} />
        </details>
      )}
      <Unrun unrun={unrun} />
    </details>
  )
}

// Reviews lists each reviewer's latest review, with its words.
function Reviews({ reviews }: { reviews: readonly ReviewEntry[] }) {
  if (reviews.length === 0) {
    return null
  }

  return (
    <ul className="space-y-2">
      {reviews.map((r) => (
        <li key={r.author} className="text-xs">
          <p className="text-muted">
            <span className="font-medium text-text">{r.author}</span> {reviewWord(r.state)}
            {r.outdated && ' an earlier commit'}
          </p>
          {r.body !== '' && <Markdown text={r.body} className="mt-1" />}
        </li>
      ))}
    </ul>
  )
}

// Closes names the issues the pull request closes once it merges, each opening in the issue
// sheet when the inbox holds it, and how one already closed was closed.
function Closes({ review }: { review: Review }) {
  if (review.closes.length === 0) {
    return null
  }

  return (
    <section aria-label="Closes" className="space-y-1 text-xs">
      <p className="text-muted">Closes</p>
      <ul className="space-y-1">
        {review.closes.map((c) => (
          <li key={refText(c)} className="flex min-w-0 items-baseline gap-1.5">
            <ItemLink linked={c} from={review.repo} titled />
            {/* A space keeps the title and the state apart when read aloud; the flex layout drops it. */}
            {c.state === 'CLOSED' && (
              <>
                {' '}
                <span className="shrink-0 text-muted">{issueLook(c.state, c.stateReason).words.toLowerCase()}</span>
              </>
            )}
          </li>
        ))}
      </ul>
      {review.moreCloses > 0 && <p className="text-muted">and {String(review.moreCloses)} more on GitHub</p>}
    </section>
  )
}

// Readiness says how close the pull request is to merging, in one line, as in "An approval
// · auto-merge available". The sheet keeps it in view above the summary, which scrolls.
export function Readiness({ review }: { review: Review }) {
  const { readiness } = review

  return <p className={`text-sm font-medium ${toneText[readyTone[readiness.state]]}`}>{readiness.sentence}</p>
}

// ReviewSummary sums up a pull request in a few lines: what kind of change it is and where,
// what a dependency update bumps, the issues it closes, its checks, its sensitive files,
// and its reviews.
export function ReviewSummary({ review, item, onFilesSettings }: ReviewSummaryProps) {
  const { change } = review

  return (
    <section aria-label="Summary" className="space-y-3">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className={chipClass}>{change.label}</span>
        <BumpChip review={review} />
        {item?.files != null && <SensitiveFiles itemId={item.id} check={item.files} onSettings={onFilesSettings} />}
        {change.areas.length > 0 && <span className="text-xs text-muted">in {areasText(change.areas)}</span>}
      </div>
      <Closes review={review} />
      <Checks checks={review.checks} unrun={review.unrun} />
      <Reviews reviews={review.reviews} />
    </section>
  )
}
