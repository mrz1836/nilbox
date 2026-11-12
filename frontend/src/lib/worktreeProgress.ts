import type { WorktreesProgress } from '../platform'

// The words for how far a listing of the worktrees has got, as the Worktrees page shows
// them while it lists (see useWorktrees). The backend names the phases
// (internal/event.WorktreesPhase); this file only words them.

// Step is a listing's progress in words, with the share of its phase done, from 0 to 1,
// or null for a phase that counts nothing.
export interface Step {
  text: string
  share: number | null
}

// counted is "1 clone" or "3 clones".
function counted(n: number, one: string, many: string): string {
  return `${String(n)} ${n === 1 ? one : many}`
}

// of is "3 of 12 clones", and "1 of 1 clone".
function of(done: number, total: number, one: string, many: string): string {
  return `${String(done)} of ${counted(total, one, many)}`
}

// progressStep words where a listing stands: what it is doing now, and how far through.
// where names the repository locations as the last listing found them, such as "under
// ~/projects" (see whereClones), or ''. Before the listing first says how far it has got,
// it is only checking.
export function progressStep(progress: WorktreesProgress | null, where: string): Step {
  if (progress === null) {
    return { text: 'Checking the worktrees…', share: null }
  }

  const share = progress.total > 0 ? progress.done / progress.total : null

  switch (progress.phase) {
    case 'clones':
      return { text: where === '' ? 'Finding the clones…' : `Finding the clones ${where}…`, share: null }
    case 'listing':
      return { text: `Listing worktrees · ${of(progress.done, progress.total, 'clone', 'clones')}`, share }
    case 'checking':
      return { text: `Checking each worktree · ${of(progress.done, progress.total, 'worktree', 'worktrees')}`, share }
    case 'github':
      return {
        text: `Asking GitHub about ${counted(progress.total, 'pull request or issue', 'pull requests and issues')}…`,
        share: null,
      }
  }
}
