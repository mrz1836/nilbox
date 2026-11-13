import { useState } from 'react'

import { ruleFor } from '../lib/repoRules'
import type { RowWork, StartWork } from '../lib/rowWork'
import { capitalize, formatWhen } from '../lib/time'
import {
  isPinned,
  isPinnedItself,
  snoozeChoices,
  snoozeEnd,
  snoozeLength,
  snoozeShort,
  type TriageTab,
} from '../lib/triage'
import { useClock, useNowAs } from '../lib/useNow'
import type { InboxItem, RepoRule, RepoRules } from '../platform'
import {
  ArchiveIcon,
  ChevronDownIcon,
  ClockIcon,
  MuteIcon,
  PinIcon,
  RefreshIcon,
  ResetIcon,
  SlidersIcon,
} from './icons'
import { MenuButton, type MenuItem } from './Menu'

// TriageActions are what the rows' triage controls do; the inbox shares one set across
// every row.
export interface TriageActions {
  // snoozeDays is the Snooze button's length, from the settings.
  snoozeDays: number
  onDismiss: (item: InboxItem) => void
  onSnooze: (item: InboxItem, days: number) => void
  // onRestore brings back a snoozed or dismissed item.
  onRestore: (item: InboxItem) => void
  // onPin keeps an item at the top of its lists, and onUnpin puts it back among the rest.
  onPin: (item: InboxItem) => void
  onUnpin: (item: InboxItem) => void
  // onRefresh reads one item from GitHub again, and settles once the inbox shows it. It
  // reports its own failures, so it never rejects.
  onRefresh: (item: InboxItem) => Promise<void>
  // onChangeDefault opens the snooze settings: the Snooze button's length and what
  // wakes a snooze early.
  onChangeDefault: () => void
  // repoRules are the pinned and muted repositories and orgs, from the settings, and
  // onRepoRule sets the rule of a target ("owner" or "owner/name"), '' to take it off;
  // onRepoRuleSettings opens them in Settings → Repositories.
  repoRules: RepoRules
  onRepoRule: (target: string, rule: RepoRule) => void
  onRepoRuleSettings: () => void
}

// mutedTarget is the target whose rule mutes an item: its repository's own, or its org's,
// as the settings say, else the repository, when the settings haven't caught up with the
// inbox yet.
export function mutedTarget(item: InboxItem, rules: RepoRules): string {
  const applied = ruleFor(rules, item.repo)

  return applied.rule === 'muted' ? applied.target : item.repo
}

// pinnedTarget is the target whose rule pins an item, as mutedTarget finds a mute's.
function pinnedTarget(item: InboxItem, rules: RepoRules): string {
  const applied = ruleFor(rules, item.repo)

  return applied.rule === 'pinned' ? applied.target : item.repo
}

export interface TriageControlsProps {
  item: InboxItem
  // tab is the list tab the row is in, which decides the controls it gets.
  tab: TriageTab
  actions: TriageActions
  // work is what the row is doing with GitHub, and start begins more (see rowWork); the
  // Refresh button shares them with the row's menu.
  work: RowWork
  start: StartWork
}

// hoverClass shows a row's quiet controls while the pointer is on the row or focus is in
// it, and while the row is busy with GitHub, so a refresh or an edit under way stays in
// sight. It sits on a wrapper, since the buttons' own transition is for color.
export function hoverClass(work: RowWork): string {
  return `flex items-center gap-0.5 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 ${work === null ? 'opacity-0' : 'opacity-100'}`
}

// ghostShape is a quiet button without its text color, and ghostClass one in the muted
// color that brightens on hover.
const ghostShape =
  'inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs font-medium transition-colors hover:bg-raised aria-expanded:bg-raised'
export const ghostClass = `${ghostShape} text-muted hover:text-text aria-expanded:text-text`

// DefaultTag marks the Snooze button's own length in the menu.
function DefaultTag() {
  return (
    <>
      <span className="sr-only">, </span>
      <span className="rounded border border-border px-1 text-[0.625rem] leading-4 font-medium tracking-wide uppercase">
        Default
      </span>
    </>
  )
}

// SnoozeEnd says when a snooze of days started now would end, kept current while the
// menu that shows it is open.
function SnoozeEnd({ days }: { days: number }) {
  return useNowAs((now) => capitalize(formatWhen(now, snoozeEnd(now, days))))
}

// snoozeItems are the snooze lengths an item can be snoozed for, each with when it would
// end and the Snooze button's own length marked, then the snooze settings. They are built
// only as a menu opens; each one's end follows the clock while it is open.
export function snoozeItems(item: InboxItem, actions: TriageActions): MenuItem[] {
  return [
    ...snoozeChoices.map((days) => ({
      label: snoozeLength(days),
      hint: (
        <>
          <SnoozeEnd days={days} />
          {days === actions.snoozeDays && <DefaultTag />}
        </>
      ),
      onSelect: () => {
        actions.onSnooze(item, days)
      },
    })),
    {
      label: 'Snooze settings…',
      icon: <SlidersIcon className="size-3.5 text-muted" />,
      divided: true,
      onSelect: actions.onChangeDefault,
    },
  ]
}

// PinButton pins an item to the top of its lists, or, on a pinned item, marks it in the
// accent color and unpins it. An item pinned only with its repository or org gets a mark
// that says so instead: a row doesn't unpin a whole repository.
function PinButton({ item, actions }: { item: InboxItem; actions: TriageActions }) {
  const pinned = isPinnedItself(item)

  if (!pinned && isPinned(item)) {
    const label = `Pinned with ${pinnedTarget(item, actions.repoRules)}`

    return (
      <span
        role="img"
        aria-label={label}
        title={`${label}: unpin it from the card’s menu or in Settings → Repositories`}
        className={`${ghostShape} text-accent hover:bg-transparent`}
      >
        <PinIcon filled className="size-3.5" />
      </span>
    )
  }

  return (
    <button
      type="button"
      aria-label={pinned ? 'Unpin' : 'Pin to top'}
      title={pinned ? 'Pinned to the top of the list: click to unpin' : 'Pin to the top of the list'}
      onClick={() => {
        if (pinned) {
          actions.onUnpin(item)
        } else {
          actions.onPin(item)
        }
      }}
      className={pinned ? `${ghostShape} text-accent` : ghostClass}
    >
      <PinIcon filled={pinned} className="size-3.5" />
    </button>
  )
}

// SnoozeSplit is the Snooze split button: the Snooze button's own length in one click,
// with the arrow for another length.
function SnoozeSplit({ item, actions }: { item: InboxItem; actions: TriageActions }) {
  const clock = useClock()
  // pointedAt is when the pointer or focus last reached the Snooze button, whose tooltip
  // says when a snooze from then would end. It is worked out only then, so the rows
  // have nothing to render as the clock moves on.
  const [pointedAt, setPointedAt] = useState<Date | null>(null)
  const { snoozeDays } = actions
  const length = snoozeLength(snoozeDays)
  const snoozeTitle =
    pointedAt === null
      ? `Snooze for ${length}`
      : `Snooze for ${length}: back ${formatWhen(pointedAt, snoozeEnd(pointedAt, snoozeDays))}`

  const point = () => {
    setPointedAt(clock.getSnapshot())
  }

  return (
    <div className="flex items-center">
      <button
        type="button"
        aria-label={`Snooze for ${length}`}
        title={snoozeTitle}
        onPointerEnter={point}
        onFocus={point}
        onClick={() => {
          actions.onSnooze(item, snoozeDays)
        }}
        className={`${ghostClass} rounded-r-none pr-1`}
      >
        <ClockIcon className="size-3.5" />
        {snoozeShort(snoozeDays)}
      </button>
      <MenuButton
        label="Snooze for…"
        title="Snooze for another length"
        heading="Snooze until"
        items={() => snoozeItems(item, actions)}
        className={`${ghostClass} rounded-l-none px-1`}
      >
        <ChevronDownIcon className="size-3" />
      </MenuButton>
    </div>
  )
}

// DismissButton hides an item until the user restores it.
function DismissButton({ item, actions }: { item: InboxItem; actions: TriageActions }) {
  return (
    <button
      type="button"
      aria-label="Dismiss"
      title="Dismiss: hide it until you restore it"
      onClick={() => {
        actions.onDismiss(item)
      }}
      className={ghostClass}
    >
      <ArchiveIcon className="size-3.5" />
    </button>
  )
}

// BringBack is the one button of the tabs an item is set aside in, always shown, since
// bringing items back is what those tabs are for: Unsnooze or Restore, which put it back in
// the list, or, in the Muted tab, Unmute, which unmutes the repository or org that mutes it.
// A pinned item's pin sits beside it.
function BringBack({
  item,
  tab,
  actions,
}: {
  item: InboxItem
  tab: Exclude<TriageTab, 'inbox'>
  actions: TriageActions
}) {
  if (tab === 'muted') {
    const target = mutedTarget(item, actions.repoRules)

    return (
      <div className="flex items-center gap-0.5 self-center">
        <button
          type="button"
          aria-label={`Unmute ${target}`}
          title={`Unmute ${target}: its items go back to the lists`}
          onClick={() => {
            actions.onRepoRule(target, '')
          }}
          className={ghostClass}
        >
          <MuteIcon className="size-3.5" />
          Unmute
        </button>
        {isPinned(item) && <PinButton item={item} actions={actions} />}
      </div>
    )
  }

  const snoozed = tab === 'snoozed'

  return (
    <div className="flex items-center gap-0.5 self-center">
      <button
        type="button"
        title={snoozed ? 'End the snooze and put it back in the list now' : 'Put it back in the list'}
        onClick={() => {
          actions.onRestore(item)
        }}
        className={ghostClass}
      >
        <ResetIcon className="size-3.5" />
        {snoozed ? 'Unsnooze' : 'Restore'}
      </button>
      {isPinned(item) && <PinButton item={item} actions={actions} />}
    </div>
  )
}

// TriageControls are a row's quiet triage buttons. In a list's inbox tab they are
// Refresh (this item alone, from GitHub), the Snooze split button (the default length in
// one click, with the arrow for another), Dismiss, and Pin, shown while the pointer is on
// the row or focus is in it, so the cards stay clean, and while a refresh runs. A pinned
// item's pin is always shown, filled, and unpins it. In the Snoozed, Dismissed, and Muted
// tabs they are the one button that brings it back (BringBack).
export function TriageControls({ item, tab, actions, work, start }: TriageControlsProps) {
  if (tab !== 'inbox') {
    return <BringBack item={item} tab={tab} actions={actions} />
  }

  const refreshing = work === 'refresh'

  const refresh = () => {
    start('refresh', () => actions.onRefresh(item))
  }

  const hover = hoverClass(work)

  return (
    <div className="flex items-center gap-0.5 self-center">
      <div className={hover}>
        <button
          type="button"
          aria-label="Refresh from GitHub"
          title="Refresh this item from GitHub, without refreshing the rest of the inbox"
          disabled={work !== null}
          onClick={refresh}
          className={`${ghostClass} disabled:cursor-default`}
        >
          <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
        </button>
        <SnoozeSplit item={item} actions={actions} />
        <DismissButton item={item} actions={actions} />
      </div>
      {/* A pinned item's pin shows without hovering, to mark it and unpin it. */}
      <div className={isPinned(item) ? 'flex' : hover}>
        <PinButton item={item} actions={actions} />
      </div>
    </div>
  )
}

export interface SheetTriageProps {
  item: InboxItem
  // tab is the list tab the card is in now, which decides the buttons it gets.
  tab: TriageTab
  actions: TriageActions
}

// SheetTriage is the card's triage in a sheet's header, the row's buttons always shown: in
// the inbox tab the Snooze split button, Dismiss, and Pin, and in the tabs an item is set
// aside in, the one that brings it back. The sheet reads the card from GitHub itself, so it
// has no Refresh of the row's.
export function SheetTriage({ item, tab, actions }: SheetTriageProps) {
  if (tab !== 'inbox') {
    return <BringBack item={item} tab={tab} actions={actions} />
  }

  return (
    <div className="flex items-center gap-0.5">
      <SnoozeSplit item={item} actions={actions} />
      <DismissButton item={item} actions={actions} />
      <PinButton item={item} actions={actions} />
    </div>
  )
}
