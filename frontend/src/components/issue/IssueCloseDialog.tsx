import type { Issue, IssueOffer } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

export interface IssueCloseDialogProps {
  issue: Issue
  offer: IssueOffer
  // comment says what is posted as a comment first, as the composer says it, or '' when
  // nothing is.
  comment: string
  onConfirm: () => void
  onCancel: () => void
}

// IssueCloseDialog asks before a close, which always does: it names the issue and what it
// closes as, says what is posted as a comment first, if anything, that nothing goes should
// someone have commented since the user opened it, and that it can be reopened on GitHub.
// Cancel is the safe choice it starts on.
export function IssueCloseDialog({ issue, offer, comment, onConfirm, onCancel }: IssueCloseDialogProps) {
  const as = offer.action === 'close_not_planned' ? 'not planned' : 'completed'
  const number = `#${String(issue.number)}`

  return (
    <ConfirmDialog
      title={`Close ${number} as ${as}?`}
      confirmLabel={offer.label}
      tone="danger"
      size="wide"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>
        {`${issue.repo}${number}`}, “{issue.title}”, closes as {as}. Nothing is sent if someone commented since you
        opened it. You can reopen it on GitHub.
      </p>
      {comment !== '' && <p className="mt-2">{comment}</p>}
    </ConfirmDialog>
  )
}
