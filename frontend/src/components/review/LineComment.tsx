import { useId, useState } from 'react'

import { canSuggest, suggestionBlock, type DiffSide } from '../../lib/diff'
import { buttonClass, fieldClass, primaryButtonClass, quietButtonClass } from '../controls'

export interface LineCommentProps {
  // where names the lines, as in "line 12" or "lines 10–12".
  where: string
  side: DiffSide
  // lines are the commented lines' text on the new version, for a suggested change.
  lines: readonly string[]
  // initial is the comment's text when editing one already added.
  initial: string
  onAdd: (body: string) => void
  onCancel: () => void
}

// LineComment writes a comment on lines of a diff: plain text, or, on the new version of
// the file, a suggested change that starts with those lines as they are. The comment waits
// in the draft and goes with the review.
export function LineComment({ where, side, lines, initial, onAdd, onCancel }: LineCommentProps) {
  const id = useId()
  const [body, setBody] = useState(initial)

  return (
    <div className="my-1 ml-24 space-y-2 rounded-md border border-border bg-raised/60 p-2 font-sans">
      <label htmlFor={id} className="block text-xs text-muted">
        Comment on {where}
      </label>
      <textarea
        id={id}
        rows={3}
        // The editor opens where the user clicked, to type at once.
        // eslint-disable-next-line jsx-a11y/no-autofocus
        autoFocus
        value={body}
        onChange={(e) => {
          setBody(e.target.value)
        }}
        className={`${fieldClass} block w-full font-mono text-xs`}
      />
      <div className="flex flex-wrap items-center gap-2">
        {canSuggest(side) && (
          <button
            type="button"
            onClick={() => {
              setBody((was) => `${was}${was === '' || was.endsWith('\n') ? '' : '\n'}${suggestionBlock(lines)}\n`)
            }}
            className={quietButtonClass}
          >
            Suggest a change
          </button>
        )}
        <span className="flex-1" />
        <button type="button" onClick={onCancel} className={buttonClass}>
          Cancel
        </button>
        <button
          type="button"
          disabled={body.trim() === ''}
          onClick={() => {
            onAdd(body.trim())
          }}
          className={primaryButtonClass}
        >
          {initial === '' ? 'Add to review' : 'Save comment'}
        </button>
      </div>
    </div>
  )
}
