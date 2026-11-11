import { useState, type ReactNode } from 'react'

import type { Review, ReviewOffer } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { WarningsToAccept } from './WarningsToAccept'

export interface ActionConfirmDialogProps {
  review: Review
  offer: ReviewOffer
  onConfirm: () => void
  onCancel: () => void
}

// actionText is the confirmation's title, what the action does, and the box that accepts
// its warnings, for an action that isn't a merge.
function actionText(review: Review, offer: ReviewOffer): { title: string; does: ReactNode; accept: string } {
  const pull = `#${String(review.number)}`
  const commit = <span className="font-mono text-text">{review.head.slice(0, 7)}</span>

  switch (offer.action) {
    case 'update_branch':
      return {
        title: `Update the branch of ${pull} with ${review.baseRef}?`,
        does: (
          <>
            GitHub merges {review.baseRef} into its branch, at commit {commit}. GitHub refuses it if anything is pushed
            first.
          </>
        ),
        accept: 'I’ve looked, update anyway',
      }
    default:
      return {
        title: `${offer.label} on ${pull}?`,
        does: (
          <>
            On {pull}, at commit {commit}.
          </>
        ),
        accept: 'I’ve looked, go ahead',
      }
  }
}

// ActionConfirmDialog asks before an action that isn't a merge but asks first, as every
// action with warnings to accept does, such as Update branch on Dependabot's pull request:
// Dependabot stops rebasing a branch someone else changed. It names the action by its
// offer's label, says what it does at which commit, and lists the warnings to accept; it
// never offers merge methods. Cancel is the safe choice it starts on.
export function ActionConfirmDialog({ review, offer, onConfirm, onCancel }: ActionConfirmDialogProps) {
  const [accepted, setAccepted] = useState(false)
  const text = actionText(review, offer)

  return (
    <ConfirmDialog
      title={text.title}
      confirmLabel={offer.label}
      tone="primary"
      size="wide"
      confirmDisabled={offer.warnings.length > 0 && !accepted}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>{text.does}</p>
      <WarningsToAccept warnings={offer.warnings} accept={text.accept} accepted={accepted} onAccept={setAccepted} />
    </ConfirmDialog>
  )
}
