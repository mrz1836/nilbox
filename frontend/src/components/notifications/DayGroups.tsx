import { useId, type ReactNode } from 'react'

import { dayHeading } from '../../lib/time'
import { useNowAs } from '../../lib/useNow'

export interface DayGroupsProps<T> {
  // items are what the list shows, the newest first.
  items: readonly T[]
  // when is an item's time, as RFC 3339, which picks its day.
  when: (item: T) => string
  // render draws one item as a list item (li), with its key.
  render: (item: T) => ReactNode
}

// DayGroup is the items of one day, under its heading.
interface DayGroup<T> {
  heading: string
  items: T[]
}

// groupByDay splits items, the newest first, into runs of the same day.
function groupByDay<T>(items: readonly T[], when: (item: T) => string, today: Date): DayGroup<T>[] {
  const groups: DayGroup<T>[] = []

  for (const item of items) {
    const heading = dayHeading(today, when(item))
    const last = groups.at(-1)

    if (last?.heading === heading) {
      last.items.push(item)
    } else {
      groups.push({ heading, items: [item] })
    }
  }

  return groups
}

// DayGroups lists items, the newest first, under a heading for each day ("Today",
// "Yesterday", the weekday, then the date), which stays at the top of the list's pane while
// its day scrolls past. It renders again for the clock only when the day changes.
export function DayGroups<T>({ items, when, render }: DayGroupsProps<T>) {
  const id = useId()
  // today is the clock's day, as its midnight, so a tick within the day changes nothing.
  const today = useNowAs((now) => new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime())
  const groups = groupByDay(items, when, new Date(today))

  return (
    <div className="flex flex-col">
      {groups.map((group, i) => (
        <section key={`${String(i)}-${group.heading}`} aria-labelledby={`${id}-${String(i)}`}>
          <h3
            id={`${id}-${String(i)}`}
            className="sticky top-0 z-10 bg-background/95 px-3 pt-3 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase backdrop-blur-sm"
          >
            {group.heading}
          </h3>
          <ul aria-label={group.heading} className="flex flex-col gap-0.5">
            {group.items.map((item) => render(item))}
          </ul>
        </section>
      ))}
    </div>
  )
}
