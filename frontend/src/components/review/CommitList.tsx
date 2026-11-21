import { Fragment, useLayoutEffect, useRef } from 'react'

import { commitsSinceAnchor, rewritePush, sinceIndex, verdictWords } from '../../lib/commits'
import { counted } from '../../lib/format'
import { formatAgo } from '../../lib/time'
import { openExternal, type Review, type ReviewCommit, type YourReview } from '../../platform'
import { Divider } from '../Divider'
import { ExternalIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'

export interface CommitListProps {
  review: Review
  // onShowPush shows the conversation's entry with the ID, the force push that rewrote the
  // branch.
  onShowPush: (entryId: string) => void
}

// tagClass is a small rounded tag beside a commit, as the conversation's are.
const tagClass = 'shrink-0 rounded-full border px-1.5 py-px text-caption'

// CommitList is the pull request's newest commits, oldest first, each opening on GitHub: the
// commit the user's standing review was left on says so, and a divider marks the commits
// pushed since, which the list scrolls to the first time it shows. When the branch was
// rewritten since, or the reviewed commit is before the newest, a note says so instead.
export function CommitList({ review, onShowPush }: CommitListProps) {
  const { commitList: commits, earlierCommits, yourReview: yours } = review
  const since = sinceIndex(commits, yours)
  const divider = useRef<HTMLLIElement>(null)
  const scrolled = useRef(false)

  // A list in a hidden Activity runs no effect, so this scrolls the first time it shows, in
  // the commit that shows it and before the window paints, as the conversation does.
  useLayoutEffect(() => {
    if (since >= 0 && !scrolled.current) {
      scrolled.current = true
      divider.current?.scrollIntoView({ block: 'center' })
    }
  }, [since])

  if (commits.length === 0) {
    return <p className="text-sm text-muted">{review.fetchedAt === '' ? 'Not read from GitHub yet.' : 'No commits.'}</p>
  }

  return (
    <section aria-label="Commits" className="mx-auto max-w-3xl space-y-3">
      {earlierCommits > 0 && (
        <button
          type="button"
          onClick={() => {
            openExternal(`${review.url}/commits`)
          }}
          className="inline-flex items-center gap-1 text-xs font-medium text-accent hover:underline"
        >
          {counted(earlierCommits, 'earlier commit', 'earlier commits')} on GitHub
          <ExternalIcon className="size-3" />
        </button>
      )}
      <SinceNote review={review} onShowPush={onShowPush} />
      <ol aria-label="Commits" className="space-y-1">
        {commits.map((commit, i) => (
          <Fragment key={commit.oid}>
            {i === since && (
              <Divider label="Since your review" dividerRef={divider} id={commitsSinceAnchor}>
                Since your review
              </Divider>
            )}
            <CommitRow commit={commit} reviewed={reviewedHere(commit, yours)} />
          </Fragment>
        ))}
      </ol>
    </section>
  )
}

// reviewedHere is the user's review when it was left on commit, else null.
function reviewedHere(commit: ReviewCommit, yours: YourReview | null): YourReview | null {
  return yours !== null && commit.oid.toLowerCase() === yours.commit.toLowerCase() ? yours : null
}

// CommitRow is one commit: its short SHA and headline, which open it on GitHub, the tag of
// the user's review when it was left on it, its author, and when it was committed.
function CommitRow({ commit, reviewed }: { commit: ReviewCommit; reviewed: YourReview | null }) {
  return (
    <li className="flex items-baseline gap-3 rounded-md px-2 py-1.5 text-sm hover:bg-raised">
      <button
        type="button"
        title={`Open ${commit.short} on GitHub`}
        onClick={() => {
          openExternal(commit.url)
        }}
        className="group flex min-w-0 flex-1 items-baseline gap-3 text-left"
      >
        <code className="shrink-0 font-mono text-xs text-muted">{commit.short}</code>{' '}
        <span className="min-w-0 truncate group-hover:text-accent group-hover:underline">{commit.headline}</span>
      </button>
      {reviewed !== null && (
        <span
          className={`${tagClass} ${reviewed.state === 'APPROVED' ? 'border-success/60 text-success' : 'border-danger/60 text-danger'}`}
        >
          {reviewed.state === 'APPROVED' ? 'You approved' : 'You requested changes'}
        </span>
      )}
      <span className="max-w-40 shrink-0 truncate text-xs text-muted">{commit.author}</span>
      {commit.committedAt !== '' && (
        <RelativeTime iso={commit.committedAt} show={formatAgo} className="shrink-0 text-xs text-muted" />
      )}
    </li>
  )
}

// SinceNote says why the list marks no commits since the user's review, when it can't: the
// branch was rewritten, as by a rebase, with the force push that did it in the conversation,
// or the reviewed commit is before the newest ones the sheet reads.
function SinceNote({ review, onShowPush }: CommitListProps) {
  const yours = review.yourReview
  const push = rewritePush(review.timeline.items, yours)

  if (yours?.since === 'rewritten') {
    return (
      <div
        id={commitsSinceAnchor}
        className="scroll-mt-4 rounded-md border border-warning/60 px-3 py-2 text-sm text-warning"
      >
        The branch was rewritten since {verdictWords(yours)}, so the commit you reviewed is no longer on it.{' '}
        {push !== undefined && (
          <button
            type="button"
            onClick={() => {
              onShowPush(push.id)
            }}
            className="font-medium underline hover:no-underline"
          >
            Show the force push
          </button>
        )}
      </div>
    )
  }

  if (yours?.since === 'beyond') {
    return (
      <p id={commitsSinceAnchor} className="scroll-mt-4 text-sm text-muted">
        The commit you reviewed is before the newest {String(review.commitList.length)}: every one shown came since{' '}
        {verdictWords(yours)}.
      </p>
    )
  }

  return null
}
