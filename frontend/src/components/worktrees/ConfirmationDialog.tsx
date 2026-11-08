import type { Confirmation } from '../../lib/useWorktrees'
import { ConfirmDialog } from '../ConfirmDialog'
import { plural } from './shared'

export interface ConfirmationDialogProps {
  confirmation: Confirmation
  onConfirm: () => void
  onCancel: () => void
}

// ConfirmationDialog words the confirmation for the pending destructive action.
export function ConfirmationDialog({ confirmation, onConfirm, onCancel }: ConfirmationDialogProps) {
  if (confirmation.kind === 'all') {
    return (
      <ConfirmDialog
        title="Remove all clean worktrees?"
        confirmLabel={`Remove ${plural(confirmation.count, 'worktree')}`}
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        This removes {plural(confirmation.count, 'clean, unlocked worktree')}. Their branches are kept.
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

  return (
    <ConfirmDialog
      title="Force remove this worktree?"
      confirmLabel="Force remove"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {confirmation.worktree.displayPath} has {plural(confirmation.worktree.dirtyCount, 'uncommitted file')}, counting
      untracked files. Force removing it discards them for good. Its branch is kept.
    </ConfirmDialog>
  )
}
