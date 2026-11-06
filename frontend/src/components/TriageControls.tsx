import { useState } from 'react'

import {
  capitalize,
  formatWhen,
  snoozeChoices,
  snoozeEnd,
  snoozeLength,
  snoozeShort,
  type TriageTab,
} from '../lib/triage'
import type { InboxItem } from '../platform'
import { ArchiveIcon, ChevronDownIcon, ClockIcon, RefreshIcon, ResetIcon, SlidersIcon } from './icons'
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
  // onRefresh reads one item from GitHub again, and settles once the inbox shows it. It
  // reports its own failures, so it never rejects.
  onRefresh: (item: InboxItem) => Promise<void>
  // onChangeDefault opens the snooze settings: the Snooze button's length and what
  // wakes a snooze early.
  onChangeDefault: () => void
}

export interface TriageControlsProps {
  item: InboxItem
  // tab is the list tab the row is in, which decides the controls it gets.
  tab: TriageTab
  now: Date
  actions: TriageActions
}

const ghostClass =
  'inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text'

// DefaultTag marks the Snooze button's own length in the menu.
function DefaultTag() {
  return (
    <>
      <span className="sr-only">, </span>
      <span className="rounded border border-border px-1 text-[10px] leading-4 font-medium tracking-wide uppercase">
        Default
      </span>
    </>
  )
}

// TriageControls are a row's quiet triage buttons. In a list's inbox tab they are
// Refresh (this item alone, from GitHub), the Snooze split button (the default length in
// one click, with the arrow for another), and Dismiss, shown while the pointer is on the
// row or focus is in it, so the cards stay clean, and while a refresh runs. In the
// Snoozed and Dismissed tabs they are a single Unsnooze or Restore, always shown, since
// bringing items back is what those tabs are for.
export function TriageControls({ item, tab, now, actions }: TriageControlsProps) {
  const [refreshing, setRefreshing] = useState(false)

  if (tab !== 'inbox') {
    const snoozed = tab === 'snoozed'

    return (
      <button
        type="button"
        title={snoozed ? 'End the snooze and put it back in the list now' : 'Put it back in the list'}
        onClick={() => {
          actions.onRestore(item)
        }}
        className={`${ghostClass} self-center`}
      >
        <ResetIcon className="size-3.5" />
        {snoozed ? 'Unsnooze' : 'Restore'}
      </button>
    )
  }

  const { snoozeDays } = actions
  const length = snoozeLength(snoozeDays)

  const choices: MenuItem[] = [
    ...snoozeChoices.map((days) => ({
      label: snoozeLength(days),
      hint: (
        <>
          {capitalize(formatWhen(now, snoozeEnd(now, days)))}
          {days === snoozeDays && <DefaultTag />}
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

  const refresh = () => {
    setRefreshing(true)
    void actions.onRefresh(item).finally(() => {
      setRefreshing(false)
    })
  }

  return (
    <div
      className={`flex items-center gap-0.5 self-center transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 ${refreshing ? 'opacity-100' : 'opacity-0'}`}
    >
      <button
        type="button"
        aria-label="Refresh from GitHub"
        title="Refresh this item from GitHub, without refreshing the rest of the inbox"
        disabled={refreshing}
        onClick={refresh}
        className={`${ghostClass} disabled:cursor-default`}
      >
        <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
      </button>
      <div className="flex items-center">
        <button
          type="button"
          aria-label={`Snooze for ${length}`}
          title={`Snooze for ${length}: back ${formatWhen(now, snoozeEnd(now, snoozeDays))}`}
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
          items={choices}
          className={`${ghostClass} rounded-l-none px-1`}
        >
          <ChevronDownIcon className="size-3" />
        </MenuButton>
      </div>
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
    </div>
  )
}
