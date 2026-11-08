// Sorting an inbox list by GitHub project status. Every project names its own statuses,
// so the order is the user's: the statuses they placed keep their places, and each one
// they haven't, as a new project brings, is placed by what its name says, so work under
// way comes before work set aside.

import type { InboxItem } from '../platform'
import { isPinned } from './triage'

// statusKey is how two names of one status match: trimmed, ignoring case.
export function statusKey(name: string): string {
  return name.trim().toLowerCase()
}

// Stage is where a status's name places it: work under way, in review, ready to start,
// unknown, new, set aside, and done.
type Stage = 0 | 1 | 2 | 3 | 4 | 5 | 6

// unknownStage is where a name that says nothing known sits: after work ready to start,
// before new work.
const unknownStage: Stage = 3

// stageRules place a status by its name, the first rule that matches winning. The order
// settles names that say two things: "Ready for review" is in review, "Waiting for review"
// too, and "Not started" is ready rather than under way.
const stageRules: readonly (readonly [RegExp, Stage])[] = [
  [
    /\b(done|complete|completed|closed|shipped|released|merged|resolved|finished|won'?t|wontfix|cancell?ed|archived)\b/,
    6,
  ],
  [/\bnot started\b/, 2],
  [/\b(blocked|on[ -]?hold|paused|deferred|parked|stalled|stuck|needs? info)\b/, 5],
  [/\b(in[ -]?progress|doing|wip|working|active|started|in[ -]?dev|in development|implementing|ongoing)\b/, 0],
  [/\b(review|reviewing|in[ -]?review|qa|testing|in[ -]?test|verify|verifying|validating|validation|staging)\b/, 1],
  [/\b(waiting|pending)\b/, 5],
  [/\b(ready|to[ -]?do|todo|up[ -]?next|next|planned|selected|committed|prioritized|prioritised)\b/, 2],
  [/\b(backlog|new|triage|inbox|icebox|ideas?|later|someday|proposed|unplanned|unsorted)\b/, 4],
]

// stageOf places a status by its name.
function stageOf(name: string): Stage {
  const key = statusKey(name)

  return stageRules.find(([rule]) => rule.test(key))?.[1] ?? unknownStage
}

// byStage orders status names by stage, then alphabetically.
function byStage(a: string, b: string): number {
  return stageOf(a) - stageOf(b) || a.localeCompare(b, undefined, { sensitivity: 'base' })
}

// Status is one status found in a list: its name as the project spells it, and its color.
export interface Status {
  name: string
  // color is "#rrggbb", or '' when the project gives it none.
  color: string
}

// statusesOf lists the statuses of the projects items are in, each once, in the order
// they are first met, with the first color met for each.
export function statusesOf(items: readonly InboxItem[]): Status[] {
  const found = new Map<string, Status>()

  for (const item of items) {
    for (const project of item.details?.projects ?? []) {
      const key = statusKey(project.status)

      if (key !== '' && !found.has(key)) {
        found.set(key, { name: project.status.trim(), color: project.statusColor })
      }
    }
  }

  return [...found.values()]
}

// statusOrder is the order a list sorts by: the user's order, with every present status
// it lacks placed by its stage, after the last status at the same stage or an earlier one
// (first, when there is none). The user's statuses not present keep their places, so
// moving one never loses the others. Without a stored order, it is the present statuses
// by stage.
export function statusOrder(stored: readonly string[], present: readonly string[]): string[] {
  const order: string[] = []
  const known = new Set<string>()

  for (const name of stored) {
    const key = statusKey(name)

    if (key !== '' && !known.has(key)) {
      known.add(key)
      order.push(name.trim())
    }
  }

  const missing: string[] = []

  for (const name of present) {
    const key = statusKey(name)

    if (key !== '' && !known.has(key)) {
      known.add(key)
      missing.push(name)
    }
  }

  for (const name of missing.sort(byStage)) {
    const stage = stageOf(name)
    const after = order.findLastIndex((placed) => stageOf(placed) <= stage)

    order.splice(after + 1, 0, name.trim())
  }

  return order
}

// rankOf is where an item sorts in order: the place of the first of its projects'
// statuses, or after every status when it has none.
function rankOf(item: InboxItem, ranks: ReadonlyMap<string, number>): number {
  let rank = ranks.size

  for (const project of item.details?.projects ?? []) {
    rank = Math.min(rank, ranks.get(statusKey(project.status)) ?? ranks.size)
  }

  return rank
}

// sortByStatus orders a list's items by project status: the pinned items stay first, as
// they were, then the rest by the first of their statuses in order. Items of one status,
// and those with none, which come last, keep the order they came in: newest activity
// first.
export function sortByStatus(items: readonly InboxItem[], order: readonly string[]): InboxItem[] {
  const ranks = new Map(order.map((name, i) => [statusKey(name), i]))
  const pinned = items.filter(isPinned)
  const rest = items.filter((item) => !isPinned(item))

  return [...pinned, ...rest.sort((a, b) => rankOf(a, ranks) - rankOf(b, ranks))]
}

// moveStatus moves a status one place up (-1) or down (1) among those shown, swapping it
// with its neighbor there, and returns the whole order with the move. A status at the
// edge, or not shown, leaves the order as it was.
export function moveStatus(order: readonly string[], shown: readonly string[], name: string, by: -1 | 1): string[] {
  const keys = new Set(shown.map(statusKey))
  const visible = order.filter((status) => keys.has(statusKey(status)))
  const from = visible.findIndex((status) => statusKey(status) === statusKey(name))
  // self and neighbor are the order's own entries, so they are found in it as they are.
  const self = visible[from]
  const neighbor = visible[from + by]
  const next = [...order]

  if (self !== undefined && neighbor !== undefined) {
    next[order.indexOf(self)] = neighbor
    next[order.indexOf(neighbor)] = self
  }

  return next
}
