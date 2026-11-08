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

// columns is the width each value takes on every card, whatever it says, so the values
// line up from card to card. Each fits the longest value a card should meet, measured in
// the line's own font: a ch is one digit, since the line uses tabular figures. A longer
// value still shows in full; it pushes the rest of its line along instead of overlapping.
// Labels close the line and take what they need.
const columns = {
  checks: 'min-w-[8ch]', // "No checks", the longest; fractions up to "999/999"
  reviews: 'min-w-[4.5ch]', // up to "99/99"
  comments: 'min-w-[3ch]', // up to "999"
  size: 'min-w-[5ch]', // "XXL" with its border, in the badge's smaller font
  added: 'min-w-[6.5ch]', // up to "+99,999"
  removed: 'min-w-[6.5ch]', // up to "−99,999"
  files: 'min-w-[6.6ch]', // up to "999 files"
  commits: 'min-w-[10ch]', // up to "999 commits"
} as const

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
  className?: string
  children: ReactNode
}

// Slot is one signal: an icon and a short form, with the full sentence for a tooltip and
// for screen readers. Each value in it sits in its column (see columns).
function Slot({ long, className = '', children }: SlotProps) {
  return (
    <li title={long} className={`inline-flex shrink-0 items-center gap-1 ${className}`}>
      <span aria-hidden="true" className="inline-flex items-center gap-1">
        {children}
      </span>
      <span className="sr-only">{long}</span>
    </li>
  )
}

function SignalSlot<K extends string>({
  signal,
  icon,
  column,
}: {
  signal: Signal<K>
  icon: ReactNode
  column: string
}) {
  return (
    <Slot long={signal.long} className={toneText[signal.tone]}>
      {icon}
      <span className={column}>{signal.short}</span>
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
// for an issue, its comments. Labels close the line. Every value before the labels keeps
// a fixed width, so the values line up from card to card.
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
      {isPR && <SignalSlot signal={checks} icon={checkIcons[checks.kind]} column={columns.checks} />}
      {isPR && <SignalSlot signal={reviews} icon={reviewIcons[reviews.kind]} column={columns.reviews} />}
      <Slot long={counted(details.comments, 'comment', 'comments')}>
        <CommentIcon className={iconClass} />
        <span className={columns.comments}>{formatCount(details.comments)}</span>
      </Slot>
      {isPR && (
        <Slot long={sizeText(additions, deletions, changedFiles, commits)}>
          <span
            className={`${columns.size} rounded border border-border px-1 text-center text-[10px] leading-4 font-semibold text-text`}
          >
            {sizeOf(additions + deletions)}
          </span>
          <span className={`${columns.added} text-success`}>+{formatCount(additions)}</span>
          <span className={`${columns.removed} text-danger`}>−{formatCount(deletions)}</span>
          <span className={columns.files}>{counted(changedFiles, 'file', 'files')}</span>
          <span className={columns.commits}>{counted(commits, 'commit', 'commits')}</span>
        </Slot>
      )}
      <Labels labels={details.labels} />
    </ul>
  )
}
