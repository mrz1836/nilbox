// Shared wording for a backup of the user's whole setup: the categories it holds, and
// what importing one would do, or did.

import { brand } from '../generated/brand'
import type { BackupChange, BackupCount, BackupSection } from '../platform'

// backupSections are the categories a backup holds, in the order it lists them, each
// with the name the window gives it and what it holds.
export const backupSections: readonly { section: BackupSection; label: string; about: string }[] = [
  {
    section: 'settings',
    label: 'Settings',
    about: 'Every setting on this page but the repositories: launching, notifications, appearance, and the rest.',
  },
  {
    section: 'repositories',
    label: 'Repositories',
    about: 'The clone folder, the other repository locations, and the repositories and orgs you pinned or muted.',
  },
  {
    section: 'prompts',
    label: 'Prompts and replies',
    about: 'Your prompts and review replies, and which of them are the defaults.',
  },
  {
    section: 'launchDefaults',
    label: 'Launch defaults',
    about: 'The launch defaults of each org and repository.',
  },
  {
    section: 'scripts',
    label: 'Your scripts',
    about: 'The scripts you saved in Repo scripts, in their order.',
  },
]

// sectionLabel is the name the window gives a category.
export function sectionLabel(section: BackupSection): string {
  return backupSections.find((s) => s.section === section)?.label ?? section
}

// neverBacked are what a backup never holds, each with why.
export const neverBacked: readonly string[] = [
  `Your GitHub sign-in, which ${brand.name} never stores.`,
  'Caches and mirrors: the inbox, diffs, issues, profile pictures, and your GitHub notifications, all read again.',
  'History: runs, agent sessions, notifications, and worktree records.',
  'What you did to each item, such as a dismissal or a snooze, which goes with the items read again.',
  `Agent profiles, which ${brand.name} finds on disk at each start.`,
  'Which repository scripts you trusted: a file from elsewhere could approve a command for you, so you’re asked again.',
  'The window’s memory: its size, the sections you folded, the tips you put away, and the last Scripts page.',
  'Which copies of the tools were found, and which ones the app installed.',
]

// planText says what importing a category would change, as in "2 to add, 1 to change ·
// 5 already here", or that it changes nothing.
export function planText(count: BackupCount): string {
  const changes = [
    count.add > 0 ? `${String(count.add)} to add` : '',
    count.update > 0 ? `${String(count.update)} to change` : '',
    count.remove > 0 ? `${String(count.remove)} to delete` : '',
  ].filter((part) => part !== '')
  const same = count.same > 0 ? `${String(count.same)} already here` : ''

  if (changes.length === 0) {
    return same === '' ? 'Nothing to import' : `Nothing to change · ${same}`
  }

  return same === '' ? changes.join(', ') : `${changes.join(', ')} · ${same}`
}

// doneText says what importing a category did, as in "2 added, 1 deleted", or "no change".
export function doneText(count: BackupCount): string {
  const parts = [
    count.add > 0 ? `${String(count.add)} added` : '',
    count.update > 0 ? `${String(count.update)} changed` : '',
    count.remove > 0 ? `${String(count.remove)} deleted` : '',
  ].filter((part) => part !== '')

  return parts.length === 0 ? 'no change' : parts.join(', ')
}

// changeText names what importing does to one item.
export const changeText: Readonly<Record<BackupChange, string>> = {
  add: 'New',
  update: 'Changes',
  same: 'Already here',
  remove: 'Deleted',
}
