import { openExternal, type InboxItem, type Review } from '../../platform'
import { quietButtonClass } from '../controls'
import { ChevronLeftIcon, ChevronRightIcon, ExternalIcon, PullRequestIcon, RefreshIcon, XIcon } from '../icons'
import { TrustTag } from '../ItemRow'
import { Spinner } from '../Spinner'

export interface ReviewHeaderProps {
  // titleId names the sheet: the heading that holds the title.
  titleId: string
  // review is the pull request as last read, and item its inbox card, which says who
  // opened it and how far they are trusted; either may be missing while it loads.
  review: Review | null
  item: InboxItem | undefined
  // position says where the pull request sits in its list, as in "3 of 8 in Assigned".
  position: string
  // onPrev and onNext step to the pull request before and after it, or are null at the
  // ends of the list.
  onPrev: (() => void) | null
  onNext: (() => void) | null
  onClose: () => void
  // onRefresh reads the pull request from GitHub again: its description, files, checks,
  // reviews, and whether it can merge.
  onRefresh: () => void
  // checking reports a read from GitHub under way.
  checking: boolean
}

// iconButtonClass is the header's square icon buttons.
const iconButtonClass = `${quietButtonClass} p-1`

// ReviewHeader is the review sheet's top: the pull request's title and where it is, who
// opened it, which branch merges into which, where it sits in its list with buttons to
// step through it, and buttons to read it from GitHub again, to open it on GitHub, and to
// close the sheet. While GitHub is read, it says so beside a spinner, the refresh button
// turns and waits, and a light glides along its bottom edge.
export function ReviewHeader({
  titleId,
  review,
  item,
  position,
  onPrev,
  onNext,
  onClose,
  onRefresh,
  checking,
}: ReviewHeaderProps) {
  const title = review?.title ?? item?.title ?? ''
  const url = review?.url ?? item?.url ?? ''
  const repo = review?.repo ?? item?.repo ?? ''
  const number = review?.number ?? item?.number ?? 0
  const author = review?.author ?? item?.author ?? ''

  return (
    <header className="relative flex items-start gap-3 border-b border-border px-5 py-3">
      <PullRequestIcon className="mt-1 size-4 shrink-0 text-success" />
      <div className="min-w-0 flex-1">
        <h2 id={titleId} className="truncate text-base font-semibold" title={title}>
          {title}
        </h2>
        <p className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-muted">
          <span>
            {repo}#{number} · {author}
          </span>
          {item !== undefined && <TrustTag item={item} />}
          {review !== null && review.headRef !== '' && (
            <span className="font-mono text-[0.6875rem]">
              {review.headRef} → {review.baseRef}
            </span>
          )}
          {position !== '' && <span>{position}</span>}
          {checking && (
            <span aria-live="polite" className="flex items-center gap-1">
              <Spinner className="size-3" />
              Checking GitHub…
            </span>
          )}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          aria-label="Previous pull request"
          title="Previous pull request (←)"
          disabled={onPrev === null}
          onClick={() => onPrev?.()}
          className={iconButtonClass}
        >
          <ChevronLeftIcon className="size-4" />
        </button>
        <button
          type="button"
          aria-label="Next pull request"
          title="Next pull request (→)"
          disabled={onNext === null}
          onClick={() => onNext?.()}
          className={iconButtonClass}
        >
          <ChevronRightIcon className="size-4" />
        </button>
        <button
          type="button"
          aria-label="Read it from GitHub again"
          title={
            checking ? 'Checking GitHub…' : 'Read it from GitHub again: checks, reviews, files, and the description'
          }
          // It waits while a read runs, but stays focusable, so a keyboard user keeps
          // their place.
          aria-disabled={checking}
          onClick={() => {
            if (!checking) {
              onRefresh()
            }
          }}
          className={`${iconButtonClass} aria-disabled:cursor-default aria-disabled:opacity-60`}
        >
          <RefreshIcon className={`size-4 ${checking ? 'motion-safe:animate-spin' : ''}`} />
        </button>
        <button
          type="button"
          aria-label="Open on GitHub"
          title="Open on GitHub"
          disabled={url === ''}
          onClick={() => {
            openExternal(url)
          }}
          className={iconButtonClass}
        >
          <ExternalIcon className="size-4" />
        </button>
        <button type="button" aria-label="Close" title="Close (Esc)" onClick={onClose} className={iconButtonClass}>
          <XIcon className="size-4" />
        </button>
      </div>
      {/* While GitHub is read, a light glides along the header's bottom edge. */}
      {checking && <div aria-hidden="true" className="loading-bar absolute inset-x-0 -bottom-px" />}
    </header>
  )
}
