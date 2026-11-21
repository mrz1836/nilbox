import { Fragment, useState } from 'react'

import { counted, joinList } from '../../lib/format'
import { refText } from '../../lib/format'
import { workflowsOf } from '../../lib/ignoredWorkflows'
import { issueLook } from '../../lib/issues'
import { candidateHint, teamKey } from '../../lib/reviewers'
import { toneDot, toneText, type Tone } from '../../lib/signals'
import { useNow } from '../../lib/useNow'
import { ignoredRuns, type ListMark } from '../../lib/useReview'
import type {
  InboxItem,
  ReadyState,
  Review,
  ReviewCheck,
  ReviewCheckCounts,
  ReviewEntry,
  Reviewers as ReviewerCandidates,
  ReviewUnrun,
} from '../../platform'
import { openExternal } from '../../platform'
import { AssigneesSection, type AssigneesEditing } from '../Assignees'
import { PersonAvatar } from '../Avatar'
import { chipClass, quietButtonClass } from '../controls'
import { ExternalIcon, RereviewIcon, XIcon } from '../icons'
import { ItemLink } from '../ItemLink'
import { RowAction, SuggestionChips } from '../PeopleParts'
import { SensitiveFiles } from '../SensitiveFiles'
import { StatusDot } from '../StatusDot'
import { ReviewerPicker, type OnReviewers } from './ReviewerPicker'
import { ReadAge } from './ReviewLoading'

// ReviewersEditing is what the summary needs to change who the pull request asks to review:
// who the picker offers, as the sheet read them (candidates, undefined while that read
// runs), a hook for what the picker learned since, and the edit itself.
export interface ReviewersEditing {
  candidates: ReviewerCandidates | undefined
  onLearned: (learned: ReviewerCandidates) => void
  onEdit: OnReviewers
}

export interface ReviewSummaryProps extends IgnoreProps {
  review: Review
  // item is the inbox card, whose sensitive-file check the summary shows.
  item: InboxItem | undefined
  onFilesSettings?: (() => void) | undefined
  // onShowReview shows the review with url in the conversation; without it, or for one the
  // sheet holds none of, a reviewer opens their review on GitHub.
  onShowReview?: ((url: string) => void) | undefined
  // mark is how the checks the sheet listed stand against what it shows of them (listMark):
  // current, unless given.
  mark?: ListMark
  // reviewers changes who the pull request asks to review, while the user may
  // (review.canEditReviewers); without it, the reviewers only show.
  reviewers?: ReviewersEditing | undefined
  // assignees changes who the pull request is assigned to, while the user may
  // (review.canEditAssignees); without it, the assignees only show.
  assignees?: AssigneesEditing | undefined
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

// ChecksSummary sums up checks: how many there are and how many fail, as in "12 checks · 1
// failing", and how many run, as in "2 running", or '' while none does, which the summary
// marks with a running check's dot; total counts them.
interface ChecksSummary {
  counts: string
  running: string
  total: number
}

// summaryOf sums up total checks, failing and pending of them.
function summaryOf(total: number, failing: number, pending: number): ChecksSummary {
  const parts = [counted(total, 'check', 'checks')]

  if (failing > 0) {
    parts.push(`${String(failing)} failing`)
  }

  return { counts: parts.join(' · '), running: pending > 0 ? `${String(pending)} running` : '', total }
}

// checksSummary sums up a list of checks.
export function checksSummary(checks: readonly ReviewCheck[]): ChecksSummary {
  return summaryOf(
    checks.length,
    checks.filter((c) => c.outcome === 'failed').length,
    checks.filter((c) => c.outcome === runningOutcome).length,
  )
}

// countsSummary sums up the checks GitHub's rollup counted, as the inbox read them since the
// sheet listed them.
function countsSummary(counts: ReviewCheckCounts): ChecksSummary {
  return summaryOf(counts.passed + counts.failed + counts.pending + counts.skipped, counts.failed, counts.pending)
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
  return joinList(areas.map((a) => (a === '.' ? 'the top level' : `${a}/`)))
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

export interface IgnoreProps {
  // ignored are the workflows the user ignores, and onIgnore adds one to them; without
  // onIgnore, nothing offers to.
  ignored?: readonly string[] | undefined
  onIgnore?: ((workflow: string) => void) | undefined
}

// IgnoreWorkflows offers to ignore each workflow of runs the user doesn't ignore yet: Ignore
// this workflow for one, or Ignore {name} for each of several.
export function IgnoreWorkflows({ runs, ignored = [], onIgnore }: IgnoreProps & { runs: readonly ReviewUnrun[] }) {
  const names = workflowsOf(runs, ignored)

  if (onIgnore === undefined || names.length === 0) {
    return null
  }

  return names.map((name) => (
    <button
      key={name}
      type="button"
      title={`Never count ${name}’s held runs toward where a pull request stands`}
      onClick={() => {
        onIgnore(name)
      }}
      className={quietButtonClass}
    >
      {names.length === 1 ? 'Ignore this workflow' : `Ignore ${name}`}
    </button>
  ))
}

// Unrun names the head commit's workflow runs that ended without a check and without
// passing, which GitHub counts as not passing though no check shows them: those waiting for
// a maintainer's approval, as a fork's runs do, those waiting for one GitHub takes only on
// its site while they count, and those that failed to start, each workflow named once, as a
// link to its run. The held runs that don't count are Ignored's.
function Unrun({ unrun }: { unrun: readonly ReviewUnrun[] }) {
  const lines = [
    { runs: unrun.filter((u) => u.approval && !u.onGitHub && !u.ignored), words: 'awaiting approval' },
    { runs: unrun.filter((u) => u.approval && u.onGitHub && !u.ignored), words: 'waiting for approval on GitHub' },
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

// Ignored names quietly, a line a run, the held runs that don't count toward where the pull
// request stands (the backend's ignored): those GitHub approves only on its site, and those
// of a workflow the user ignores. Each opens its run on GitHub, where it can be approved,
// and offers to ignore its workflow, or says it is ignored.
function Ignored({ review, ignored = [], onIgnore }: IgnoreProps & { review: Review }) {
  const runs = ignoredRuns(review)

  if (runs.length === 0) {
    return null
  }

  return (
    <ul aria-label="Workflow runs that don’t count" className="mt-1.5 space-y-0.5 text-xs text-muted">
      {runs.map((u, i) => (
        <li key={`${u.name}-${String(u.runId)}-${String(i)}`}>
          {u.name || 'an app'} · {u.onGitHub ? 'held on GitHub' : 'held for approval'} ·{' '}
          <button
            type="button"
            onClick={() => {
              openExternal(u.url === '' ? `${review.url}/checks` : u.url)
            }}
            className={quietButtonClass}
          >
            <ExternalIcon className="size-3" />
            Open on GitHub
          </button>
          {ignored.includes(u.name) ? (
            ' · ignored'
          ) : (
            <>
              {' '}
              <IgnoreWorkflows runs={[u]} ignored={ignored} onIgnore={onIgnore} />
            </>
          )}
        </li>
      ))}
    </ul>
  )
}

// openChecks is how many checks the list shows open. A failing check always shows; the
// checks running show beside them while every one of them fits, and the ones that passed
// while they all do; any that don't fit fold away under their count.
const openChecks = 8

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
// the workflow runs that never ran a check and aren't passing (Unrun), and the held runs
// that don't count (Ignored). The count is the inbox's while it read the checks since the
// sheet listed them (counts.newer), and then the list says when the sheet listed it, faded
// while it is read again (mark); a list cut short says how many more GitHub lists.
function Checks({ review, mark, ignored, onIgnore }: IgnoreProps & { review: Review; mark: ListMark }) {
  const { checks, unrun, counts } = review
  const summary = counts.newer ? countsSummary(counts) : checksSummary(checks)
  const age = <ReadAge mark={mark} fetchedAt={review.fetchedAt} what="checks" />

  if (checks.length === 0 && summary.total === 0) {
    return (
      <>
        <p className="text-xs text-muted">No checks on this commit.</p>
        {age}
        <Unrun unrun={unrun} />
        <Ignored review={review} ignored={ignored} onIgnore={onIgnore} />
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
      {age}
      {checks.length === 0 && review.fetchedAt === '' && <p className="text-muted">Not read from GitHub yet.</p>}
      <div className={mark === 'reading' ? 'opacity-60' : undefined}>
        {open.length > 0 && <CheckList checks={open} label="Checks" />}
        {!showRunning && running.length > 0 && <CheckGroup checks={running} outcome={runningOutcome} words="running" />}
        {!showPassed && passed.length > 0 && <CheckGroup checks={passed} outcome="passed" words="passed" />}
        {skipped.length > 0 && <CheckGroup checks={skipped} outcome="skipped" words="skipped" />}
        {review.moreChecks > 0 && (
          <p className="mt-1 text-muted">
            {counted(review.moreChecks, 'more check', 'more checks')} on GitHub ·{' '}
            <button
              type="button"
              onClick={() => {
                openExternal(`${review.url}/checks`)
              }}
              className={quietButtonClass}
            >
              <ExternalIcon className="size-3" />
              Open on GitHub
            </button>
          </p>
        )}
        <Unrun unrun={unrun} />
        <Ignored review={review} ignored={ignored} onIgnore={onIgnore} />
      </div>
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

// suggestionChips is how many of the people the picker offers the summary shows as one-click
// requests while no one is asked.
const suggestionChips = 3

// requestKey is the picker's key for a reviewer's request: their login, or a team's teamKey.
function requestKey(r: ReviewEntry): string {
  return r.team ? teamKey(r.author.slice(r.author.indexOf('/') + 1)) : r.author
}

// Suggestions offers, while no one is asked to review, up to three of the people the picker
// would, each a one-click request, and More…, which opens the picker.
function Suggestions({
  candidates,
  busy,
  onAsk,
  onMore,
}: {
  candidates: ReviewerCandidates | undefined
  busy: string
  onAsk: (login: string) => void
  onMore: () => void
}) {
  const now = useNow()
  const chips = (candidates?.people ?? []).slice(0, suggestionChips).map((c) => ({
    login: c.login,
    text: c.login,
    label: `Ask ${c.login} to review`,
    title: [c.name, candidateHint(c, now)].filter((w) => w !== '').join(' · '),
  }))

  return <SuggestionChips label="Suggested reviewers" chips={chips} busy={busy} onPick={onAsk} onMore={onMore} />
}

// Reviewers lists each reviewer in a line: those who reviewed, each with their latest
// review, then the people and teams still asked to review. A review shows in the
// conversation, where its words are (onShow), or on GitHub; a request has nothing to show.
// While the user may change who the pull request asks (editing), the list always shows, with
// Edit, which opens the reviewer picker; a request offers × to take it off, and someone who
// reviewed and isn't asked now ↻ to ask again (never the user, the author, or a bot), a
// line fading while its edit runs; and with no one asked, it offers its first few
// suggestions as one-click requests.
function Reviewers({
  review,
  onShow,
  editing,
}: {
  review: Review
  onShow?: ((url: string) => void) | undefined
  editing?: ReviewersEditing | undefined
}) {
  const [picking, setPicking] = useState(false)
  // busy is the key of the request whose edit runs, or ''; failed why the last one failed.
  const [busy, setBusy] = useState('')
  const [failed, setFailed] = useState('')
  const reviews = review.reviews
  const canEdit = editing !== undefined && review.canEditReviewers

  if (reviews.length === 0 && !canEdit) {
    return null
  }

  const quick = async (key: string, add: string[], remove: string[]) => {
    if (editing === undefined) {
      return
    }

    setBusy(key)
    setFailed('')

    const why = await editing.onEdit(add, remove)

    setBusy('')
    setFailed(why)
  }

  const line = 'flex w-full min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-left text-muted'

  return (
    <section aria-label="Reviewers" className="space-y-1 text-xs">
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted">Reviewers</p>
        {canEdit && (
          <button
            type="button"
            aria-label="Edit reviewers"
            title="Edit reviewers"
            aria-haspopup="dialog"
            aria-expanded={picking}
            onClick={(e) => {
              // The desktop's WebKit focuses no button on a click. Focusing this one first
              // makes it where the picker gives focus back as it closes.
              e.currentTarget.focus()
              setPicking(true)
            }}
            className={quietButtonClass}
          >
            Edit
          </button>
        )}
      </div>
      {reviews.length === 0 ? (
        <div className="space-y-1.5">
          <p className="text-muted">No one is asked to review yet.</p>
          <Suggestions
            candidates={editing?.candidates}
            busy={busy}
            onAsk={(login) => {
              void quick(login, [login], [])
            }}
            onMore={() => {
              setPicking(true)
            }}
          />
        </div>
      ) : (
        <ul className="space-y-0.5">
          {reviews.map((r) => {
            const key = requestKey(r)
            const askAgain =
              !r.requested && r.state !== '' && !r.mine && !r.bot && !r.team && r.author !== review.author

            return (
              <li
                key={`${r.team ? 'team' : 'user'}:${r.author}`}
                aria-busy={busy === key || undefined}
                className={`group flex min-w-0 items-center gap-1 ${busy === key ? 'opacity-50' : ''}`}
              >
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
                {canEdit && r.requested && !r.mine && (
                  <RowAction
                    label={`Remove the review request for ${r.author}`}
                    title="Remove request"
                    disabled={busy !== ''}
                    onClick={() => {
                      void quick(key, [], [key])
                    }}
                  >
                    <XIcon className="size-3" />
                  </RowAction>
                )}
                {canEdit && askAgain && (
                  <RowAction
                    label={`Ask ${r.author} to review again`}
                    title="Ask again"
                    disabled={busy !== ''}
                    onClick={() => {
                      void quick(key, [key], [])
                    }}
                  >
                    <RereviewIcon className="size-3" />
                  </RowAction>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {failed !== '' && (
        <p role="alert" className="text-danger">
          {failed}
        </p>
      )}
      {picking && editing !== undefined && (
        <ReviewerPicker
          review={review}
          candidates={editing.candidates}
          onLearned={editing.onLearned}
          onEdit={editing.onEdit}
          onClose={() => {
            setPicking(false)
          }}
        />
      )}
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
// who it is assigned to, and its checks, with the held runs that don't count, whose
// workflows it offers to ignore. The reviewers and the assignees come before the checks, so
// however many checks run, who reviewed, who still has to, and whose it is stay in view.
export function ReviewSummary({
  review,
  item,
  onFilesSettings,
  onShowReview,
  ignored,
  onIgnore,
  mark = 'current',
  reviewers,
  assignees,
}: ReviewSummaryProps) {
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
      <Reviewers review={review} onShow={onShowReview} editing={reviewers} />
      <AssigneesSection
        look="summary"
        itemId={review.id}
        repo={review.repo}
        noun="pull request"
        url={review.url}
        assignees={review.assignees}
        more={review.moreAssignees}
        editing={assignees}
      />
      <Checks review={review} mark={mark} ignored={ignored} onIgnore={onIgnore} />
    </section>
  )
}
