import type { ReactNode } from 'react'

import type { Edge } from '../../lib/nextStep'
import { formatAgo, formatFull, formatWhen } from '../../lib/time'
import { isPinnedItself, type TriageTab } from '../../lib/triage'
import { useNowAs } from '../../lib/useNow'
import type { InboxItem, RepoRules } from '../../platform'
import { useMarkedBy, useRoutineName } from '../autopilot/AutopilotChip'
import { ArchiveIcon, AutopilotIcon, ClockIcon, PinIcon } from '../icons'
import { pinnedTarget } from '../TriageControls'

// The parts both of a card's densities share: the edge's color, the repository and author
// that filter the inbox, the pin and the unread dot beside the title, and the story of a
// set-aside card.

// tagClass is a status tag: weight and color mark it, with no pill around it.
export const tagClass = 'shrink-0 text-[0.6875rem] leading-4 font-medium'

export interface FilterTextProps {
  // filterKey and value are the filter the text adds, as in repo and its name; name
  // says what it is, as in "repository".
  filterKey: string
  value: string
  name: string
  onFilterBy: ((key: string, value: string) => void) | undefined
  // full is what the text stands for, in its tooltip, and children what the card says,
  // such as "you" for the user's own login; both default to value.
  full?: string
  children?: ReactNode
}

// FilterText is a card's repository or author: plain text, or plain text under a quiet
// button laid over it that filters the inbox by it. The text stays out of the button so a
// line that ends in an ellipsis can cut it like the rest of the line: a button is one box
// to its line, which the ellipsis never cuts, only hides whole, leaving a gap as wide as
// the text it held.
export function FilterText({ filterKey, value, name, onFilterBy, full = value, children = value }: FilterTextProps) {
  if (onFilterBy === undefined) {
    return full === value ? children : <span title={full}>{children}</span>
  }

  return (
    <span data-filter-text className="relative hover:text-text hover:underline">
      <span aria-hidden="true">{children}</span>
      <button
        type="button"
        aria-label={`Filter by ${name} ${full}`}
        title={`Filter by ${name} ${full}`}
        onClick={() => {
          onFilterBy(filterKey, value)
        }}
        className="absolute inset-0 rounded-sm"
      />
    </span>
  )
}

// ByRoutine names the Autopilot routine that set an item aside, after what it did: "by ⚡
// Snooze passing bot PRs".
function ByRoutine({ id }: { id: number }) {
  const name = useRoutineName(id)

  return (
    <>
      {' by '}
      <AutopilotIcon className="inline size-3 align-[-0.125rem]" />
      <span className="sr-only">the routine </span>
      <span className="inline-block max-w-48 truncate align-bottom">{name}</span>
    </>
  )
}

// SnoozedTag says when a snoozed item comes back, and which routine snoozed it, when one did.
function SnoozedTag({ item, className }: { item: InboxItem; className: string }) {
  const back = useNowAs((now) => formatWhen(now, new Date(item.snoozedUntil)))
  const by = useMarkedBy(item.setAsideBy)
  const name = useRoutineName(by)
  const until = formatFull(item.snoozedUntil)

  if (by > 0) {
    return (
      <span title={`Snoozed by the routine ${name} until ${until}`} className={className}>
        <ClockIcon className="size-3 shrink-0" />
        <span className="truncate">
          Snoozed
          <ByRoutine id={by} /> · back {back}
        </span>
      </span>
    )
  }

  return (
    <span title={`Snoozed until ${until}`} className={className}>
      <ClockIcon className="size-3" />
      Back {back}
    </span>
  )
}

// DismissedTag says how long ago an item was dismissed, and by which routine, when one
// did.
function DismissedTag({ item, className }: { item: InboxItem; className: string }) {
  const ago = useNowAs((now) => formatAgo(now, item.dismissedAt))
  const by = useMarkedBy(item.setAsideBy)
  const name = useRoutineName(by)
  const when = formatFull(item.dismissedAt)

  if (by > 0) {
    return (
      <span title={`Dismissed by the routine ${name}, ${when}`} className={className}>
        <ArchiveIcon className="size-3 shrink-0" />
        <span className="truncate">
          Dismissed
          <ByRoutine id={by} /> · {ago}
        </span>
      </span>
    )
  }

  return (
    <span title={`Dismissed ${when}`} className={className}>
      <ArchiveIcon className="size-3" />
      Dismissed {ago}
    </span>
  )
}

// TriageTag is the story of a card in the Snoozed or Dismissed tab: when the item comes
// back, or how long ago it was dismissed, and which routine did it, when one did, until
// the user changes it. Each tag reads the clock itself, so a tick renders a tag only when
// its words change. While Autopilot (beta) is off, no tag names a routine (useMarkedBy).
export function TriageTag({ item, tab }: { item: InboxItem; tab: TriageTab }) {
  const quiet = `${tagClass} relative z-10 inline-flex min-w-0 items-center gap-1 text-muted`

  return tab === 'snoozed' ? (
    <SnoozedTag item={item} className={quiet} />
  ) : (
    <DismissedTag item={item} className={quiet} />
  )
}

// PinGlyph marks a pinned card before its title: pinned by the user, by a routine, or with
// its repository or org, which the card's menu or Settings unpins.
export function PinGlyph({ item, rules }: { item: InboxItem; rules: RepoRules | undefined }) {
  const by = useMarkedBy(item.pinnedBy)
  const name = useRoutineName(by)
  const itself = isPinnedItself(item)
  const label = !itself
    ? `Pinned with ${rules === undefined ? item.repo : pinnedTarget(item, rules)}`
    : by > 0
      ? `Pinned by the routine ${name}`
      : 'Pinned to the top of the list'
  const title = itself ? label : `${label}: unpin it from the card’s menu or in Settings → Repositories`

  return (
    <span role="img" aria-label={label} title={title} className="relative z-10 inline-flex shrink-0 text-accent">
      <PinIcon filled className="size-3.5" />
    </span>
  )
}

// UnreadDot marks a card with activity the user hasn't seen since they last opened it,
// while Settings → Cards' Unread dots is on.
export function UnreadDot() {
  return (
    <span data-unread title="New since you last opened it" className="relative z-10 inline-flex shrink-0">
      <span aria-hidden="true" className="block size-1.75 rounded-full bg-info ring-3 ring-info/20" />
      <span className="sr-only">Unread</span>
    </span>
  )
}

// edgeBar is each edge's color, the bar down a card's left side, spelled out in full so
// Tailwind finds them.
export const edgeBar: Readonly<Record<Edge, string>> = {
  urgent: 'bg-warning',
  blocked: 'bg-danger',
  yours: 'bg-accent',
  ready: 'bg-success',
  agent: 'bg-info',
  waiting: 'bg-muted/30',
  none: 'bg-transparent',
}
