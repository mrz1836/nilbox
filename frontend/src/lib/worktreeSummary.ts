import type { BranchResult, RemoveResult, Worktree } from '../platform'
import { counted } from './format'
import { capitalize } from './time'

// Summary says what a change in the Worktrees view did, in a headline and a line of
// detail: the toast that confirms it and the card that reports it read the same.
export interface Summary {
  title: string
  // detail is the line under the headline, or ''.
  detail: string
  // tone is success when everything asked was done, warning when some of it was left,
  // and error when the change failed.
  tone: 'success' | 'warning' | 'error'
}

// shortName names a worktree by its repository and its own name, as in
// "example-repo/pr-7": the folder its worktrees are in, or, for one Claude Code made in a
// clone's .claude/worktrees, the clone.
export function shortName(displayPath: string): string {
  const parts = displayPath.split('/')
  const name = parts.at(-1) ?? ''
  const legacy = parts.at(-3) === '.claude' && parts.at(-2) === 'worktrees'
  const repo = parts.at(legacy ? -4 : -2)

  return repo === undefined ? name : `${repo}/${name}`
}

// branchList names branches for a sentence: "a", "a and b", or "a, b, and c".
function branchList(names: readonly string[]): string {
  if (names.length <= 2) {
    return names.join(' and ')
  }

  return `${names.slice(0, -1).join(', ')}, and ${names.at(-1) ?? ''}`
}

// singleSummary says what removing, force removing, or cleaning up one worktree did:
// verb is "Removed", "Force removed", or "Cleaned up". It names the branches deleted with
// it, and why any branch ticked was kept.
export function singleSummary(verb: string, wt: Worktree, res: RemoveResult): Summary {
  const deleted = res.deletedBranches
  const branches =
    deleted.length === 0
      ? 'No branch was deleted.'
      : `${deleted.length === 1 ? 'Its branch' : 'Its branches'} ${branchList(deleted)} ${deleted.length === 1 ? 'was' : 'were'} deleted.`
  const kept = res.reason === '' ? '' : ` ${capitalize(res.reason)}.`

  return {
    title: `${verb} ${shortName(wt.displayPath)}`,
    detail: `${branches}${kept}`,
    tone: res.reason === '' ? 'success' : 'warning',
  }
}

// bulkSummary says what a bulk action did: verb is "Removed" or "Cleaned up", and noun
// names what it worked on, such as "clean worktree".
export function bulkSummary(results: readonly RemoveResult[], noun: string, verb: string): Summary {
  if (results.length === 0) {
    return { title: `There were no ${noun}s to remove`, detail: 'Nothing was changed.', tone: 'success' }
  }

  const removed = results.filter((r) => r.removed).length
  const branches = results.reduce((n, r) => n + r.deletedBranches.length, 0)
  const left = results.length - removed
  const detail = [
    branches === 0 ? 'No branch was deleted' : `${counted(branches, 'branch', 'branches')} deleted`,
    left > 0 && `${String(left)} left in place, as noted below`,
  ].filter((part) => part !== false)

  return {
    title:
      left === 0
        ? `${verb} ${counted(removed, noun, `${noun}s`)}`
        : `${verb} ${String(removed)} of ${counted(results.length, noun, `${noun}s`)}`,
    detail: `${detail.join(' · ')}.`,
    tone: left === 0 && results.every((r) => r.reason === '') ? 'success' : 'warning',
  }
}

// branchSummary says what deleting merged branches did: how many went, across how many
// clones, and how many were kept.
export function branchSummary(results: readonly BranchResult[]): Summary {
  const deleted = results.filter((r) => r.deleted)
  const kept = results.length - deleted.length
  const clones = new Set(deleted.map((r) => r.repoPath)).size
  const detail = [
    deleted.length > 0 && `Across ${counted(clones, 'clone', 'clones')}`,
    kept > 0 && `${String(kept)} kept, as noted below`,
  ].filter((part) => part !== false)

  return {
    title:
      kept === 0
        ? `Deleted ${counted(deleted.length, 'branch', 'branches')}`
        : `Deleted ${String(deleted.length)} of ${counted(results.length, 'branch', 'branches')}`,
    detail: detail.length === 0 ? '' : `${detail.join(' · ')}.`,
    tone: kept === 0 ? 'success' : 'warning',
  }
}

// failedSummary says a change failed, with why.
export function failedSummary(title: string, error: string): Summary {
  return { title, detail: error, tone: 'error' }
}
