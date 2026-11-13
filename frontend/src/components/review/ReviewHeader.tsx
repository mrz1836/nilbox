import type { ReactNode } from 'react'

import type { SheetBack, SheetStep } from '../../lib/sheet'
import type { InboxItem, Review } from '../../platform'
import { PullRequestIcon } from '../icons'
import { TrustTag } from '../ItemRow'
import { SheetHeader } from '../SheetHeader'

export interface ReviewHeaderProps {
  // titleId names the sheet: the heading that holds the title.
  titleId: string
  // review is the pull request as last read, and item its inbox card, which says who
  // opened it and how far they are trusted; either may be missing while it loads.
  review: Review | null
  item: InboxItem | undefined
  // position says where the pull request sits in its list, as in "3 of 8 in Assigned".
  position: string
  // onPrev and onNext step to the card before and after it, or are null at the ends of
  // the list.
  onPrev: SheetStep | null
  onNext: SheetStep | null
  // back returns to the sheet a link was followed from, or is null.
  back?: SheetBack | null
  // actions are the card's own buttons before the steps, such as its triage.
  actions?: ReactNode
  onClose: () => void
  // onRefresh reads the pull request from GitHub again: its description, files, checks,
  // reviews, and whether it can merge.
  onRefresh: () => void
  // checking reports a read from GitHub under way.
  checking: boolean
}

// ReviewHeader is the review sheet's top (SheetHeader): the pull request's title and where
// it is, who opened it, and which branch merges into which.
export function ReviewHeader({
  titleId,
  review,
  item,
  position,
  onPrev,
  onNext,
  back = null,
  actions,
  onClose,
  onRefresh,
  checking,
}: ReviewHeaderProps) {
  const repo = review?.repo ?? item?.repo ?? ''
  const number = review?.number ?? item?.number ?? 0
  const author = review?.author ?? item?.author ?? ''

  return (
    <SheetHeader
      titleId={titleId}
      icon={<PullRequestIcon className="mt-1 size-4 shrink-0 text-success" />}
      title={review?.title ?? item?.title ?? ''}
      url={review?.url ?? item?.url ?? ''}
      meta={
        <>
          <span>
            {repo}#{number} · {author}
          </span>
          {item !== undefined && <TrustTag item={item} />}
          {review !== null && review.headRef !== '' && (
            <span className="font-mono text-[0.6875rem]">
              {review.headRef} → {review.baseRef}
            </span>
          )}
        </>
      }
      position={position}
      checking={checking}
      prev={onPrev}
      back={back}
      actions={actions}
      next={onNext}
      refreshTitle="Read it from GitHub again: checks, reviews, files, and the description"
      onRefresh={onRefresh}
      onClose={onClose}
    />
  )
}
