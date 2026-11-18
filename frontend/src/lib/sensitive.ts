// What an inbox card says about the sensitive files a pull request changes: the chip on
// the card, the words for each file's change, and the dot for each category's level. The
// check itself (which files count, whose pull requests are checked) happens in the
// backend; this only words its outcome.

import type { FileCheck, SensitiveFile, SensitiveLevel } from '../platform'
import { counted, formatCount } from './format'
import type { Tone } from './signals'

// levelDot is a category's level as the color of the dot beside its name, on the card's
// panel and in Settings.
export const levelDot: Readonly<Record<SensitiveLevel, string>> = {
  danger: 'bg-danger',
  warning: 'bg-warning',
  '': 'bg-muted',
}

// FilesChipIcon picks the chip's icon: a warning shield, a checked shield, or none.
export type FilesChipIcon = 'alert' | 'clear' | 'none'

// FilesChip is the card's chip for a check: its tone, its short text ('' for the icon
// alone), and the full sentence for the tooltip and screen readers.
export interface FilesChip {
  tone: Tone
  icon: FilesChipIcon
  short: string
  long: string
}

// shownCategories is how many categories the chip names before folding the rest into
// "+N".
const shownCategories = 2

// sensitiveCount is how many sensitive files a check found.
export function sensitiveCount(check: FileCheck): number {
  return check.groups.reduce((n, g) => n + g.files.length, 0)
}

// filesChip words a check for the card. Agent configuration, workflows, build tooling,
// and the user's own patterns are danger; review rules and dependencies, and a check
// that could not finish, a warning. A pull request whose every file was checked and none
// is sensitive shows a quiet checked shield, so "checked, nothing found" never looks like
// "not checked".
export function filesChip(check: FileCheck): FilesChip {
  const groups = check.groups

  if (groups.length > 0) {
    const names = groups.slice(0, shownCategories).map((g) => g.short)
    const rest = groups.length - shownCategories
    const parts = groups.map((g) => `${g.short} (${counted(g.files.length, 'file', 'files')})`)
    const unchecked = check.complete ? '' : '; not every file was checked'

    return {
      tone: groups.some((g) => g.level === 'danger') ? 'danger' : 'warning',
      icon: 'alert',
      short: rest > 0 ? `${names.join(' · ')} +${String(rest)}` : names.join(' · '),
      long: `Changes sensitive files: ${parts.join(', ')}${unchecked}`,
    }
  }

  switch (check.status) {
    case 'failed':
      return {
        tone: 'warning',
        icon: 'alert',
        short: 'Files not checked',
        long:
          check.error === '' ? 'Couldn’t check the changed files' : `Couldn’t check the changed files: ${check.error}`,
      }
    case 'partial':
      return {
        tone: 'warning',
        icon: 'alert',
        short: 'Not all files checked',
        long: `Only ${formatCount(check.checked)} of ${counted(check.total, 'changed file', 'changed files')} were checked, none of them sensitive`,
      }
    case 'pending':
      return {
        tone: 'muted',
        icon: 'none',
        short: 'Files not checked yet',
        long: 'Not checked since its last push; the next refresh checks it',
      }
    default:
      return {
        tone: 'muted',
        icon: 'clear',
        short: '',
        long: `No sensitive files among its ${counted(check.total, 'changed file', 'changed files')}`,
      }
  }
}

// ChangeWords is how a file changed, in two parts: the words, as in "added" or "moved
// from", and the path a moved or copied file came from, or ''.
export interface ChangeWords {
  words: string
  from: string
}

// changeWords words how a file changed: "added", "removed", "renamed", and so on, with
// where a moved file came from apart, so a narrow row can cut that path short and keep
// the words whole.
export function changeWords(file: SensitiveFile): ChangeWords {
  const from = file.previousPath !== file.path ? file.previousPath : ''

  switch (file.change) {
    case 'renamed':
      return { words: from === '' ? 'renamed' : 'moved from', from }
    case 'copied':
      return { words: from === '' ? 'copied' : 'copied from', from }
    case 'added':
    case 'removed':
    case 'modified':
      return { words: file.change, from: '' }
    default:
      return { words: 'changed', from: '' }
  }
}

// changeText words how a file changed in one line: "added", "removed", "renamed", and so
// on, with where a moved file came from.
export function changeText(file: SensitiveFile): string {
  const { words, from } = changeWords(file)

  return from === '' ? words : `${words} ${from}`
}

// changeTone colors a change: an added file stands out, a removed one warns.
export function changeTone(change: string): Tone {
  switch (change) {
    case 'added':
      return 'success'
    case 'removed':
    case 'renamed':
      return 'danger'
    default:
      return 'muted'
  }
}
