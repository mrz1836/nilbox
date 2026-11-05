import type { ReactNode } from 'react'

import { counted, formatCount } from '../lib/format'
import {
  checksSignal,
  reviewsSignal,
  sizeOf,
  sizeText,
  toneText,
  type ChecksKind,
  type ReviewsKind,
  type Signal,
} from '../lib/signals'
import type { InboxItem, Label } from '../platform'
import { CheckIcon, CommentIcon, DotIcon, EyeIcon, RingIcon, XIcon } from './icons'

// shownLabels is how many labels a card shows before folding the rest into "+N".
const shownLabels = 3

const iconClass = 'size-3.5 shrink-0'

const checkIcons: Readonly<Record<ChecksKind, ReactNode>> = {
  passed: <CheckIcon className={iconClass} />,
  failed: <XIcon className={iconClass} />,
  pending: <DotIcon className={iconClass} />,
  none: <RingIcon className={iconClass} />,
}

const reviewIcons: Readonly<Record<ReviewsKind, ReactNode>> = {
  approved: <CheckIcon className={iconClass} />,
  changes: <XIcon className={iconClass} />,
  waiting: <EyeIcon className={iconClass} />,
}

interface SlotProps {
  // long says the signal in full: the tooltip, and what a screen reader reads.
  long: string
  // width keeps the slot the same width on every card, so the columns line up.
  width: string
  className?: string
  children: ReactNode
}

// Slot is one signal: an icon and a short form, with the full sentence for a tooltip and
// for screen readers.
function Slot({ long, width, className = '', children }: SlotProps) {
  return (
    <li title={long} className={`inline-flex shrink-0 items-center gap-1 ${width} ${className}`}>
      <span aria-hidden="true" className="inline-flex items-center gap-1">
        {children}
      </span>
      <span className="sr-only">{long}</span>
    </li>
  )
}

function SignalSlot<K extends string>({ signal, icon, width }: { signal: Signal<K>; icon: ReactNode; width: string }) {
  return (
    <Slot long={signal.long} width={width} className={toneText[signal.tone]}>
      {icon}
      {signal.short}
    </Slot>
  )
}

// Labels shows the first few labels as chips with their GitHub color, and the count of
// the rest, named in its tooltip.
function Labels({ labels }: { labels: Label[] }) {
  if (labels.length === 0) {
    return null
  }

  const rest = labels.slice(shownLabels)

  return (
    <li className="flex min-w-0 items-center gap-1 overflow-hidden">
      <span className="sr-only">Labels:</span>
      {labels.slice(0, shownLabels).map((label) => (
        <span
          key={label.name}
          className="inline-flex max-w-40 shrink-0 items-center gap-1 rounded-full border border-border px-1.5 py-px text-[11px] text-text"
        >
          <span
            aria-hidden="true"
            className={`size-2 shrink-0 rounded-full ${label.color === '' ? 'bg-muted' : ''}`}
            style={label.color === '' ? undefined : { backgroundColor: label.color }}
          />
          <span className="truncate">{label.name}</span>
        </span>
      ))}
      {rest.length > 0 && (
        <span title={rest.map((label) => label.name).join(', ')} className="shrink-0 text-[11px]">
          +{rest.length}
          <span className="sr-only"> more: {rest.map((label) => label.name).join(', ')}</span>
        </span>
      )}
    </li>
  )
}

export interface ItemSignalsProps {
  item: InboxItem
}

// ItemSignals is the card's line of facts about an item. For a pull request: its checks,
// its reviews, its comments, and its size (lines added and removed, files, commits);
// for an issue, its comments. Labels close the line. Checks, reviews, and comments keep
// fixed widths, so they line up from card to card.
export function ItemSignals({ item }: ItemSignalsProps) {
  const details = item.details

  if (details === null) {
    return <p className="mt-1.5 text-xs text-muted">Details arrive with the next refresh.</p>
  }

  const isPR = item.kind === 'pr'
  const { additions, deletions, changedFiles, commits } = details
  const checks = checksSignal(details.checks)
  const reviews = reviewsSignal(details.reviews)

  return (
    <ul
      aria-label="Details"
      className="mt-1.5 flex min-w-0 items-center gap-x-3 overflow-hidden text-xs whitespace-nowrap text-muted tabular-nums"
    >
      {isPR && <SignalSlot signal={checks} icon={checkIcons[checks.kind]} width="min-w-[4.5rem]" />}
      {isPR && <SignalSlot signal={reviews} icon={reviewIcons[reviews.kind]} width="min-w-[3.25rem]" />}
      <Slot long={counted(details.comments, 'comment', 'comments')} width="min-w-[2.75rem]">
        <CommentIcon className={iconClass} />
        {formatCount(details.comments)}
      </Slot>
      {isPR && (
        <Slot long={sizeText(additions, deletions, changedFiles, commits)} width="">
          <span className="rounded border border-border px-1 text-[10px] leading-4 font-semibold text-text">
            {sizeOf(additions + deletions)}
          </span>
          <span className="text-success">+{formatCount(additions)}</span>
          <span className="text-danger">−{formatCount(deletions)}</span>
          <span>· {counted(changedFiles, 'file', 'files')}</span>
          <span>· {counted(commits, 'commit', 'commits')}</span>
        </Slot>
      )}
      <Labels labels={details.labels ?? []} />
    </ul>
  )
}
