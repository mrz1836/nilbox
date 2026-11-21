// Small display helpers shared by the views.

import { causeOf } from '../platform/cause'

const counts = new Intl.NumberFormat()

// formatCount formats a whole number with the user's digit grouping, e.g. "4,980".
export function formatCount(n: number): string {
  return counts.format(n)
}

// errorMessage is the text to show for a failed call. Errors from the bound Go services
// arrive as Error instances whose message is the error's full text, for the console, and
// whose cause carries the sentence the backend worded for the user, which wins. An error
// without one shows its message verbatim.
export function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    const worded = causeOf(err)?.message ?? ''
    if (worded !== '') {
      return worded
    }

    if (err.message !== '') {
      return err.message
    }
  }

  if (typeof err === 'string' && err !== '') {
    return err
  }

  return 'Something went wrong.'
}

// counted reads a count with its noun, e.g. "1 file" or "1,204 files".
export function counted(n: number, one: string, many: string): string {
  return `${formatCount(n)} ${n === 1 ? one : many}`
}

// badgeText is a toolbar button's count, as its badge shows it: the number, up to 99,
// then 99+.
export function badgeText(count: number): string {
  return count > 99 ? '99+' : formatCount(count)
}

// joinList joins names for a sentence: "a", "a and b", or "a, b, and c".
export function joinList(names: readonly string[]): string {
  if (names.length <= 2) {
    return names.join(' and ')
  }

  return `${names.slice(0, -1).join(', ')}, and ${names.at(-1) ?? ''}`
}

// refText names a pull request or an issue as "owner/name#number".
export function refText(ref: { repo: string; number: number }): string {
  return `${ref.repo}#${String(ref.number)}`
}

// fileDate is a date as a file name carries it: 2026-11-08, in the user's own time zone.
export function fileDate(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')

  return `${String(now.getFullYear())}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

// fileName is the last part of a path: the file's name.
export function fileName(path: string): string {
  return path.split(/[\\/]/).at(-1) ?? path
}
