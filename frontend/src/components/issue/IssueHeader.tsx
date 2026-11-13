import type { ReactNode } from 'react'

import { issueLook } from '../../lib/issues'
import type { SheetBack, SheetStep } from '../../lib/sheet'
import { formatAgo } from '../../lib/time'
import type { InboxItem, Issue } from '../../platform'
import { IssueIcon } from '../icons'
import { TrustTag } from '../ItemRow'
import { RelativeTime } from '../RelativeTime'
import { SheetHeader } from '../SheetHeader'

export interface IssueHeaderProps {
  // titleId names the sheet: the heading that holds the title.
  titleId: string
  // issue is the issue as last read, and item its inbox card, which says who opened it and
  // how far they are trusted; either may be missing while it loads.
  issue: Issue | null
  item: InboxItem | undefined
  // position says where the issue sits in its list, as in "3 of 8 in Assigned".
  position: string
  // checking reports a read from GitHub under way.
  checking: boolean
  prev: SheetStep | null
  next: SheetStep | null
  // back returns to the sheet a link was followed from, or is null.
  back?: SheetBack | null
  // actions are the card's own buttons before the steps, such as its triage.
  actions?: ReactNode
  // onRefresh reads the issue from GitHub again: its conversation, details, and what the
  // user may do.
  onRefresh: () => void
  onClose: () => void
}

// tagClass is the header's small tags.
const tagClass = 'rounded-full border border-border px-1.5 py-px text-[0.6875rem]'

// IssueHeader is the issue sheet's top (SheetHeader): the issue's state, as its icon's
// color, its title and where it is, who opened it and when, and whether the conversation is
// locked, and its type.
export function IssueHeader({
  titleId,
  issue,
  item,
  position,
  checking,
  prev,
  next,
  back = null,
  actions,
  onRefresh,
  onClose,
}: IssueHeaderProps) {
  const look = issueLook(issue?.state ?? 'OPEN', issue?.stateReason ?? '')
  const repo = issue?.repo ?? item?.repo ?? ''
  const number = issue?.number ?? item?.number ?? 0
  const author = issue?.author ?? item?.author ?? ''
  const openedAt = issue?.openedAt ?? ''

  return (
    <SheetHeader
      titleId={titleId}
      icon={
        <span role="img" aria-label={look.words} className="mt-1 shrink-0">
          <IssueIcon className={`size-4 ${look.color}`} />
        </span>
      }
      title={issue?.title ?? item?.title ?? ''}
      url={issue?.url ?? item?.url ?? ''}
      meta={
        <>
          <span>
            {repo}#{number} · {author}
          </span>
          {item !== undefined && <TrustTag item={item} />}
          {openedAt !== '' && (
            <span>
              opened <RelativeTime iso={openedAt} show={formatAgo} />
            </span>
          )}
          {issue?.locked === true && <span className={`${tagClass} text-warning`}>Locked</span>}
          {issue?.type != null && (
            <span title={issue.type.description} className={`${tagClass} inline-flex items-center gap-1 text-text`}>
              <span
                aria-hidden="true"
                className={`size-2 rounded-full ${issue.type.color === '' ? 'bg-muted' : ''}`}
                style={issue.type.color === '' ? undefined : { backgroundColor: issue.type.color }}
              />
              {issue.type.name}
            </span>
          )}
        </>
      }
      position={position}
      checking={checking}
      prev={prev}
      next={next}
      back={back}
      actions={actions}
      refreshTitle="Read it from GitHub again: the conversation, the details, and what you can do"
      onRefresh={onRefresh}
      onClose={onClose}
    />
  )
}
