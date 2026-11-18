import type { ReviewUnrun, Settings } from '../platform'
import type { Result } from './call'

// Ignored workflows: the workflows whose runs, held for approval, never count toward where a
// pull request stands, as Settings → Reviewing stores them (internal/heldruns'
// checks.ignoredWorkflows). The backend applies them: the inbox's cards count with them, and
// the review sheet marks each held run that doesn't count (ignored). The window only names
// them and adds to them: App hands them to the review sheet, through the inbox and the
// Autopilot tab's sheets (ignoring).

// IgnoredWorkflows are the names, and how to store another list of them.
export interface IgnoredWorkflows {
  // names are the workflows ignored now, matched whole and case-sensitively.
  names: readonly string[]
  // save stores names as the list, and resolves with the settings stored, or why it failed.
  // Once stored, the window reads the inbox again, so its cards count with them.
  save: (names: string[]) => Promise<Result<Settings>>
}

// ignoreKey is the list as one value, which stays the same while the list does.
export function ignoreKey(names: readonly string[] | undefined): string {
  return (names ?? []).join('\n')
}

// workflowsOf names each workflow of runs once, in their order, leaving out a run of no
// name and those already in ignored.
export function workflowsOf(runs: readonly ReviewUnrun[], ignored: readonly string[]): string[] {
  return [...new Set(runs.map((u) => u.name))].filter((name) => name !== '' && !ignored.includes(name))
}
