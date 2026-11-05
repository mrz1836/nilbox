// Compact relative times for list rows and the rate gauge: "just now", "5m", "3h", "2d",
// "6w", "4mo", "2y". An empty or unparseable timestamp yields "".

const minute = 60
const hour = 60 * minute
const day = 24 * hour
const week = 7 * day
const month = 30 * day
const year = 365 * day

// units are checked largest first; each formats a whole number of that unit.
const units: readonly (readonly [seconds: number, suffix: string])[] = [
  [year, 'y'],
  [month, 'mo'],
  [week, 'w'],
  [day, 'd'],
  [hour, 'h'],
  [minute, 'm'],
]

// parseTime returns the timestamp in milliseconds, or undefined when it is empty or
// unparseable.
function parseTime(iso: string): number | undefined {
  if (iso === '') {
    return undefined
  }

  const ms = Date.parse(iso)

  return Number.isNaN(ms) ? undefined : ms
}

// formatSpan formats a non-negative number of seconds in the largest whole unit.
function formatSpan(seconds: number): string {
  for (const [size, suffix] of units) {
    if (seconds >= size) {
      return `${String(Math.floor(seconds / size))}${suffix}`
    }
  }

  return 'just now'
}

// formatAge is how long ago iso was, relative to now. A time in the future (clock skew)
// reads as "just now".
export function formatAge(now: Date, iso: string): string {
  const then = parseTime(iso)
  if (then === undefined) {
    return ''
  }

  return formatSpan(Math.max(0, (now.getTime() - then) / 1000))
}

// formatUntil is how long until iso, relative to now, e.g. "42m". A time already passed
// reads as "now".
export function formatUntil(now: Date, iso: string): string {
  const then = parseTime(iso)
  if (then === undefined) {
    return ''
  }

  const span = formatSpan(Math.max(0, (then - now.getTime()) / 1000))

  return span === 'just now' ? 'now' : span
}
