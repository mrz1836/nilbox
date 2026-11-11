import { useState } from 'react'

import type { Review, ReviewOffer } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { WarningsToAccept } from './WarningsToAccept'

export interface UpdateBranchConfirmDialogProps {
  review: Review
  offer: ReviewOffer
  onConfirm: () => void
  onCancel: () => void
}

// UpdateBranchConfirmDialog asks before an update of the branch that has warnings to accept
// first, as an update of Dependabot's branch does: Dependabot stops rebasing a branch
// someone else changed. It says what GitHub does, at which commit, and the warnings to
// accept. Cancel is the safe choice it starts on.
export function UpdateBranchConfirmDialog({ review, offer, onConfirm, onCancel }: UpdateBranchConfirmDialogProps) {
  const [accepted, setAccepted] = useState(false)

  return (
    <ConfirmDialog
      title={`Update the branch of #${String(review.number)} with ${review.baseRef}?`}
      confirmLabel={offer.label}
      tone="primary"
      size="wide"
      confirmDisabled={offer.warnings.length > 0 && !accepted}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>
        GitHub merges {review.baseRef} into its branch, at commit{' '}
        <span className="font-mono text-text">{review.head.slice(0, 7)}</span>. GitHub refuses it if anything is pushed
        first.
      </p>
      <WarningsToAccept
        warnings={offer.warnings}
        accept="I’ve looked, update anyway"
        accepted={accepted}
        onAccept={setAccepted}
      />
    </ConfirmDialog>
  )
}
