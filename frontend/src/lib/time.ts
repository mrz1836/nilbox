// Times as the app writes them: compact relative times for list rows and the rate gauge
// ("just now", "5m", "3h", "2d", "6w", "4mo", "2y"), a time from now as people say it, and
// a time in full for a tooltip. An empty or unparseable timestamp yields "".

const minute = 60
const hour = 60 * minute
const day = 24 * hour
const week = 7 * day
const month = 30 * day
const year = 365 * day

const dayMs = day * 1000

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

// formatAgo is how long ago iso was as a phrase: "5m ago", or "just now".
export function formatAgo(now: Date, iso: string): string {
  const age = formatAge(now, iso)

  return age === 'just now' || age === '' ? age : `${age} ago`
}

// startOfDay is the local midnight that starts d's day.
function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

// formatWhen names a time from now on the way people say it: "today, 3:42 PM",
// "tomorrow, 9:00 AM", "Sat, 3:42 PM" within the week, then "Thu, Nov 19", with the year
// when it is not this one. locale is for tests; the app uses the system's.
export function formatWhen(now: Date, when: Date, locale?: string): string {
  // Rounding absorbs the 23- and 25-hour days around a change of the clocks.
  const days = Math.round((startOfDay(when) - startOfDay(now)) / dayMs)
  const time = new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit' }).format(when)

  if (days <= 0) {
    return `today, ${time}`
  }

  if (days === 1) {
    return `tomorrow, ${time}`
  }

  if (days < 7) {
    return `${new Intl.DateTimeFormat(locale, { weekday: 'short' }).format(when)}, ${time}`
  }

  const year = when.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' as const }

  return new Intl.DateTimeFormat(locale, { weekday: 'short', month: 'short', day: 'numeric', ...year }).format(when)
}

// formatFull names a time in full, for a tooltip: "Saturday, November 8, 2026 at 3:42 PM".
export function formatFull(iso: string, locale?: string): string {
  const ms = parseTime(iso)

  return ms === undefined ? '' : new Intl.DateTimeFormat(locale, { dateStyle: 'full', timeStyle: 'short' }).format(ms)
}

// capitalize starts a phrase with a capital, for formatWhen at the start of a line.
export function capitalize(phrase: string): string {
  return phrase.charAt(0).toUpperCase() + phrase.slice(1)
}
