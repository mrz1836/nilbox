import type { Confirmation } from '../../lib/useWorktrees'
import type { Worktree } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { plural } from './shared'

export interface ConfirmationDialogProps {
  confirmation: Confirmation
  onConfirm: () => void
  onCancel: () => void
}

// itemText names the item a worktree was made for, as in "issue #202", or ''.
function itemText(wt: Worktree): string {
  if (wt.item === null) {
    return ''
  }

  return `${wt.item.kind === 'pr' ? 'pull request' : 'issue'} #${String(wt.item.number)}`
}

// branchFate says what removing a worktree does with its branch, the way the app decides
// it: the worktree's own worktree-… branch goes too when every commit on it is on GitHub,
// so nothing is lost, and stays, with its commits, when some are not; any other branch
// stays. The removal checks again as it runs, and says if it decided otherwise.
function branchFate(wt: Worktree): string {
  const item = itemText(wt)

  if (wt.detached || wt.branch === '') {
    return 'It has no branch.'
  }

  if (!wt.ownBranch) {
    return `Its branch ${wt.branch} stays.`
  }

  if (wt.onGitHub) {
    const fresh = item === '' ? '' : `, and the next launch on ${item} starts afresh`

    return `Its branch ${wt.branch} goes with it: every commit on it is on GitHub, so nothing is lost${fresh}.`
  }

  const commits = wt.unpushed > 0 ? plural(wt.unpushed, 'commit') : 'commits'
  const again = item === '' ? '' : `, and the next launch on ${item} picks them up again`

  return `Its branch ${wt.branch} stays, with the ${commits} on it that aren’t on GitHub${again}.`
}

// ConfirmationDialog words the confirmation for the pending destructive action, saying
// what it does to the worktree and to its branch.
export function ConfirmationDialog({ confirmation, onConfirm, onCancel }: ConfirmationDialogProps) {
  if (confirmation.kind === 'all') {
    return (
      <ConfirmDialog
        title="Remove all clean worktrees?"
        confirmLabel={`Remove ${plural(confirmation.count, 'worktree')}`}
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        This removes {plural(confirmation.count, 'clean, unlocked worktree')}. Each one’s own worktree-… branch goes
        with it when every commit on it is on GitHub; a branch with commits that aren’t, or that isn’t the worktree’s
        own, stays.
      </ConfirmDialog>
    )
  }

  if (confirmation.kind === 'finished') {
    return (
      <ConfirmDialog
        title="Clean up finished worktrees?"
        confirmLabel={`Clean up ${plural(confirmation.count, 'worktree')}`}
        tone="primary"
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        This removes {plural(confirmation.count, 'worktree')} whose pull request merged or issue closed. Each has no
        uncommitted changes and every commit on GitHub, and each is checked again first. A worktree&apos;s own
        worktree-… branch is deleted with it.
      </ConfirmDialog>
    )
  }

  const wt = confirmation.worktree

  if (confirmation.kind === 'remove') {
    return (
      <ConfirmDialog title="Remove this worktree?" confirmLabel="Remove" onConfirm={onConfirm} onCancel={onCancel}>
        This deletes the folder {wt.displayPath}, which has no uncommitted changes. {branchFate(wt)}
      </ConfirmDialog>
    )
  }

  return (
    <ConfirmDialog
      title="Force remove this worktree?"
      confirmLabel="Force remove"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {wt.displayPath} has {plural(wt.dirtyCount, 'uncommitted file')}, counting untracked files. Force removing it
      discards them for good. {branchFate(wt)}
    </ConfirmDialog>
  )
}
