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

// changeText words how a file changed: "added", "removed", "renamed", and so on, with
// where a moved file came from.
export function changeText(file: SensitiveFile): string {
  const moved = file.previousPath !== '' && file.previousPath !== file.path

  switch (file.change) {
    case 'renamed':
      return moved ? `moved from ${file.previousPath}` : 'renamed'
    case 'copied':
      return moved ? `copied from ${file.previousPath}` : 'copied'
    case 'added':
    case 'removed':
    case 'modified':
      return file.change
    default:
      return 'changed'
  }
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

// shortHead is a commit's SHA as GitHub abbreviates it.
export function shortHead(head: string): string {
  return head.slice(0, 7)
}
