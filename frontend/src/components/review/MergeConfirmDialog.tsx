import { useId, useState } from 'react'

import type { MergeMethod, Review, ReviewOffer } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { WarningsToAccept } from './WarningsToAccept'

export interface MergeConfirmDialogProps {
  review: Review
  offer: ReviewOffer
  onConfirm: (method: MergeMethod) => void
  onCancel: () => void
  // comment says what is posted as a comment before the merge, as the composer says it, or
  // '' when nothing is.
  comment?: string
}

// methodNames are each merge method's name, as GitHub says it.
export const methodNames: Readonly<Record<Exclude<MergeMethod, ''>, string>> = {
  SQUASH: 'Squash and merge',
  MERGE: 'Create a merge commit',
  REBASE: 'Rebase and merge',
}

// mergeVerb is a method as a verb, as in "squash-merge".
export function mergeVerb(method: MergeMethod): string {
  switch (method) {
    case 'SQUASH':
      return 'squash-merge'
    case 'REBASE':
      return 'rebase-merge'
    default:
      return 'merge'
  }
}

// confirmText is the dialog's title and its button, for the action and method.
function confirmText(review: Review, offer: ReviewOffer, method: MergeMethod): { title: string; label: string } {
  const into = `#${String(review.number)} into ${review.baseRef}`
  const verb = mergeVerb(method)

  switch (offer.action) {
    case 'approve_merge':
      return { title: `Approve and merge ${into}?`, label: `Approve & ${verb}` }
    case 'approve_auto_merge':
      return { title: `Approve, and merge ${into} once its requirements are met?`, label: 'Approve & auto-merge' }
    case 'auto_merge':
      return { title: `Merge ${into} automatically once its requirements are met?`, label: 'Merge when ready' }
    case 'merge_bypass':
      return { title: `Merge ${into} past the repository’s rules?`, label: 'Merge past the rules' }
    default:
      return { title: `Merge ${into}?`, label: verb.charAt(0).toUpperCase() + verb.slice(1) }
  }
}

// MergeConfirmDialog asks before every merge, auto-merge included: which method, among
// those the repository allows, at which commit, and, when there are any, the warnings to
// accept first, such as optional checks failing or sensitive files. A merge past the
// repository's rules lists each rule it skips instead, says plainly what it does, and
// confirms in the danger tone. Cancel is the safe choice it starts on.
export function MergeConfirmDialog({ review, offer, onConfirm, onCancel, comment = '' }: MergeConfirmDialogProps) {
  const id = useId()
  const [method, setMethod] = useState<MergeMethod>(review.defaultMethod)
  const [accepted, setAccepted] = useState(false)
  const text = confirmText(review, offer, method)
  const warned = offer.warnings.length > 0
  const past = offer.action === 'merge_bypass'

  return (
    <ConfirmDialog
      title={text.title}
      confirmLabel={text.label}
      tone={past ? 'danger' : 'primary'}
      size="wide"
      confirmDisabled={warned && !accepted}
      onConfirm={() => {
        onConfirm(method)
      }}
      onCancel={onCancel}
    >
      <fieldset className="space-y-1.5">
        <legend className="mb-1 text-text">Merge method</legend>
        {review.mergeMethods.map((m) =>
          m === '' ? null : (
            <label key={m} className="flex items-center gap-2 text-text">
              <input
                type="radio"
                name={`${id}-method`}
                className="radio"
                checked={method === m}
                onChange={() => {
                  setMethod(m)
                }}
              />
              {methodNames[m]}
            </label>
          ),
        )}
      </fieldset>
      <p className="mt-3">
        At commit <span className="font-mono text-text">{review.head.slice(0, 7)}</span>. GitHub refuses it if anything
        is pushed first.
      </p>
      {comment !== '' && <p className="mt-2">{comment}</p>}
      <WarningsToAccept
        warnings={offer.warnings}
        heading={past ? 'GitHub would hold this merge back:' : ''}
        accept={past ? 'I’m merging past the rules' : 'I’ve looked, merge anyway'}
        accepted={accepted}
        onAccept={setAccepted}
      />
      {past && (
        <p className="mt-3">
          GitHub lets you merge past them, as it lets a repository admin. Nothing else here ever does, and no agent can.
        </p>
      )}
    </ConfirmDialog>
  )
}
