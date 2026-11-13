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

// formats holds every date format made so far, by locale and options. Making one is far
// slower than formatting with it, and the inbox formats a time for each row on every
// render, so each is made once.
const formats = new Map<string, Intl.DateTimeFormat>()

// dateFormat returns the format for a locale (the system's when undefined) and options.
function dateFormat(locale: string | undefined, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = JSON.stringify([locale ?? '', options])
  let format = formats.get(key)

  if (format === undefined) {
    format = new Intl.DateTimeFormat(locale, options)
    formats.set(key, format)
  }

  return format
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
  const time = dateFormat(locale, { hour: 'numeric', minute: '2-digit' }).format(when)

  if (days <= 0) {
    return `today, ${time}`
  }

  if (days === 1) {
    return `tomorrow, ${time}`
  }

  if (days < 7) {
    return `${dateFormat(locale, { weekday: 'short' }).format(when)}, ${time}`
  }

  const year = when.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' as const }

  return dateFormat(locale, { weekday: 'short', month: 'short', day: 'numeric', ...year }).format(when)
}

// formatFull names a time in full, for a tooltip: "Saturday, November 8, 2026 at 3:42 PM".
export function formatFull(iso: string, locale?: string): string {
  const ms = parseTime(iso)

  return ms === undefined ? '' : dateFormat(locale, { dateStyle: 'full', timeStyle: 'short' }).format(ms)
}

// formatDateTime names a time with its date, for a list of things made at different
// times: "Nov 8, 2026, 11:51 AM". locale is for tests; the app uses the system's.
export function formatDateTime(iso: string, locale?: string): string {
  const ms = parseTime(iso)

  return ms === undefined ? '' : dateFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(ms)
}

// formatDay names a day GitHub keeps as midnight UTC, such as a milestone's due date, as that
// day wherever the user is: "Dec 1, 2026". locale is for tests; the app uses the system's.
export function formatDay(iso: string, locale?: string): string {
  const ms = parseTime(iso)

  return ms === undefined ? '' : dateFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(ms)
}

// dayPassed reports that the day GitHub keeps as midnight UTC, iso, such as a milestone's due
// date, has passed by now's own calendar, as formatDay names it: it is over once the user's
// day is a later one.
export function dayPassed(now: Date, iso: string): boolean {
  if (parseTime(iso) === undefined) {
    return false
  }

  const today = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, '0'))

  return today.join('-') > iso.slice(0, 10)
}

// capitalize starts a phrase with a capital, for formatWhen at the start of a line.
export function capitalize(phrase: string): string {
  return phrase.charAt(0).toUpperCase() + phrase.slice(1)
}
