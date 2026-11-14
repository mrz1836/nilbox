import { Fragment } from 'react'

import { counted } from '../../lib/format'
import { refText } from '../../lib/format'
import { issueLook } from '../../lib/issues'
import { toneDot, toneText, type Tone } from '../../lib/signals'
import type { InboxItem, ReadyState, Review, ReviewCheck, ReviewEntry, ReviewUnrun } from '../../platform'
import { openExternal } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { chipClass } from '../controls'
import { ItemLink } from '../ItemLink'
import { SensitiveFiles } from '../SensitiveFiles'
import { StatusDot } from '../StatusDot'

export interface ReviewSummaryProps {
  review: Review
  // item is the inbox card, whose sensitive-file check the summary shows.
  item: InboxItem | undefined
  onFilesSettings?: (() => void) | undefined
  // onShowReview shows the review with url in the conversation; without it, or for one the
  // sheet holds none of, a reviewer opens their review on GitHub.
  onShowReview?: ((url: string) => void) | undefined
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

// reviewTone is a reviewer's word's color: an approval is good news, a request for changes
// an alarm, and the rest plain.
function reviewTone(state: string): Tone {
  switch (state) {
    case 'APPROVED':
      return 'success'
    case 'CHANGES_REQUESTED':
      return 'danger'
    default:
      return 'muted'
  }
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
function CheckList({ checks, label }: { checks: readonly ReviewCheck[]; label: string }) {
  return (
    <ul aria-label={label} className="mt-1 space-y-0.5">
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

// RunNames names each workflow of runs once, each a link to its run's page on GitHub when
// the sheet read one: where to approve a run GitHub wouldn't let the sheet approve.
export function RunNames({ runs }: { runs: readonly ReviewUnrun[] }) {
  const pages = new Map<string, string>()

  for (const u of runs) {
    const name = u.name || 'an app'

    if (!pages.has(name) || pages.get(name) === '') {
      pages.set(name, u.url)
    }
  }

  return [...pages].map(([name, url], i) => (
    <Fragment key={name}>
      {i > 0 && ', '}
      {url === '' ? (
        name
      ) : (
        <button
          type="button"
          title={url}
          onClick={() => {
            openExternal(url)
          }}
          className="hover:text-accent hover:underline"
        >
          {name}
        </button>
      )}
    </Fragment>
  ))
}

// Unrun names the head commit's workflow runs that ended without a check and without
// passing, which GitHub counts as not passing though no check shows them: those waiting for
// a maintainer's approval, as a fork's runs do, and those that failed to start, each
// workflow named once, as a link to its run.
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
          <span className="text-muted">
            <RunNames runs={runs} />
          </span>
        </li>
      ))}
    </ul>
  )
}

// openChecks is how many checks the list shows open. A failing check always shows; the
// checks running show beside them while every one of them fits, and the ones that passed
// while they all do; any that don't fit fold away under their count.
export const openChecks = 8

// CheckGroup is a group of checks folded away under its count, as in "23 passed · 2
// required", with the dot of their outcome.
function CheckGroup({ checks, outcome, words }: { checks: readonly ReviewCheck[]; outcome: string; words: string }) {
  const required = checks.filter((c) => c.required).length

  return (
    <details className="mt-1">
      <summary className="cursor-default text-muted select-none">
        <CheckDot outcome={outcome} /> {String(checks.length)} {words}
        {required > 0 && ` · ${String(required)} required`}
      </summary>
      <CheckList checks={checks} label={`Checks ${words}`} />
    </details>
  )
}

// requiredFirst sorts checks with the required ones first, each kind in GitHub's order.
function requiredFirst(checks: readonly ReviewCheck[]): ReviewCheck[] {
  return checks.toSorted((a, b) => Number(b.required) - Number(a.required))
}

// Checks lists the head commit's checks, as few as tell what matters: every failing one,
// then those running and those passed while they fit (openChecks), else each group folded
// under its count, the required ones first; the skipped ones, which say nothing about the
// change, always fold. The list starts open, and folds away under its count, which marks
// the checks running with their dot, so a folded list still shows CI at work. Beneath it,
// the workflow runs that never ran a check and aren't passing (Unrun).
function Checks({ checks, unrun }: { checks: readonly ReviewCheck[]; unrun: readonly ReviewUnrun[] }) {
  if (checks.length === 0) {
    return (
      <>
        <p className="text-xs text-muted">No checks on this commit.</p>
        <Unrun unrun={unrun} />
      </>
    )
  }

  const failing = requiredFirst(checks.filter((c) => c.outcome === 'failed'))
  const running = requiredFirst(checks.filter((c) => c.outcome === runningOutcome))
  const passed = requiredFirst(checks.filter((c) => c.outcome === 'passed'))
  const skipped = checks.filter((c) => !['failed', runningOutcome, 'passed'].includes(c.outcome))
  const showRunning = failing.length + running.length <= openChecks
  const showPassed = showRunning && failing.length + running.length + passed.length <= openChecks
  const open = [...failing, ...(showRunning ? running : []), ...(showPassed ? passed : [])]
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
      {open.length > 0 && <CheckList checks={open} label="Checks" />}
      {!showRunning && running.length > 0 && <CheckGroup checks={running} outcome={runningOutcome} words="running" />}
      {!showPassed && passed.length > 0 && <CheckGroup checks={passed} outcome="passed" words="passed" />}
      {skipped.length > 0 && <CheckGroup checks={skipped} outcome="skipped" words="skipped" />}
      <Unrun unrun={unrun} />
    </details>
  )
}

// reviewerWords says what a reviewer did, or that they are still asked to: an earlier
// review's state and, for one asked again, that they were.
function reviewerWords(r: ReviewEntry): string {
  if (r.state === '') {
    return 'awaiting review'
  }

  return r.requested ? `${reviewWord(r.state)} · asked again` : reviewWord(r.state)
}

// ReviewerLine is one reviewer's line: their picture, their name, what they did, and
// whether it was of an earlier commit.
function ReviewerLine({ r }: { r: ReviewEntry }) {
  return (
    <>
      <PersonAvatar login={r.author} />
      <span className="min-w-0 truncate font-medium text-text">{r.author}</span>
      <span className={`shrink-0 ${r.state === '' ? 'text-muted' : toneText[reviewTone(r.state)]}`}>
        {reviewerWords(r)}
      </span>
      {r.outdated && <span className="shrink-0">an earlier commit</span>}
    </>
  )
}

// Reviewers lists each reviewer in a line: those who reviewed, each with their latest
// review, then the people and teams still asked to review. A review shows in the
// conversation, where its words are (onShow), or on GitHub; a request has nothing to show.
function Reviewers({
  reviews,
  onShow,
}: {
  reviews: readonly ReviewEntry[]
  onShow?: ((url: string) => void) | undefined
}) {
  if (reviews.length === 0) {
    return null
  }

  const line = 'flex w-full min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-left text-muted'

  return (
    <section aria-label="Reviewers" className="space-y-1 text-xs">
      <p className="text-muted">Reviewers</p>
      <ul className="space-y-0.5">
        {reviews.map((r) => (
          <li key={`${r.team ? 'team' : 'user'}:${r.author}`}>
            {r.url === '' ? (
              <p className={line}>
                <ReviewerLine r={r} />
              </p>
            ) : (
              <button
                type="button"
                title={onShow === undefined ? r.url : 'Show the review in Comments'}
                onClick={() => {
                  if (onShow === undefined) {
                    openExternal(r.url)
                  } else {
                    onShow(r.url)
                  }
                }}
                className={`${line} hover:bg-raised`}
              >
                <ReviewerLine r={r} />
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
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
// what a dependency update bumps, its sensitive files, the issues it closes, its reviewers,
// and its checks. The reviewers come before the checks, so however many checks run, who
// reviewed and who still has to stays in view.
export function ReviewSummary({ review, item, onFilesSettings, onShowReview }: ReviewSummaryProps) {
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
      <Reviewers reviews={review.reviews} onShow={onShowReview} />
      <Checks checks={review.checks} unrun={review.unrun} />
    </section>
  )
}
