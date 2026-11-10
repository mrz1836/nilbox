import { useState } from 'react'

import { attempt, start, type Result } from '../../lib/call'
import { revealToolCopy, type IntegrationID, type ToolCopy } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass } from '../controls'
import { TrashIcon } from '../icons'
import { ErrorText } from './rows'

export interface TrashCopyProps {
  // toolId and copy are the tool and the copy of it to move.
  toolId: IntegrationID
  copy: ToolCopy
  // label names the button, as the sentence beside it reads.
  label: string
  // disabled holds the button while another change runs.
  disabled: boolean
  // trash moves the copy at path to the Trash, showing the tools as it left them, and
  // resolves with how it went.
  trash: (path: string) => Promise<Result<unknown>>
}

// TrashCopy moves a copy of a tool to the Trash, once the user confirms, through Finder,
// so Finder's Put Back can restore it. When Finder can't, it says why and offers to show
// the copy in Finder, to move it by hand.
export function TrashCopy({ toolId, copy, label, disabled, trash }: TrashCopyProps) {
  const [asking, setAsking] = useState(false)
  const [error, setError] = useState('')
  const [revealError, setRevealError] = useState('')

  const confirm = async () => {
    setAsking(false)
    setError('')
    setRevealError('')

    const result = await trash(copy.path)

    if (!result.ok) {
      setError(result.error)
    }
  }

  const reveal = async () => {
    setRevealError('')

    const result = await attempt(start(() => revealToolCopy(toolId, copy.path)))

    if (!result.ok) {
      setRevealError(result.error)
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          setAsking(true)
        }}
        className={quietButtonClass}
      >
        <TrashIcon className="size-3.5" />
        {label}
      </button>
      {error !== '' && (
        <div className="flex flex-col items-start gap-1">
          <ErrorText error={error} />
          <button
            type="button"
            onClick={() => {
              void reveal()
            }}
            className={quietButtonClass}
          >
            Reveal in Finder
          </button>
          <ErrorText error={revealError} />
        </div>
      )}
      {asking && (
        <ConfirmDialog
          title={`Move ${copy.displayPath} to the Trash?`}
          confirmLabel="Move to the Trash"
          onCancel={() => {
            setAsking(false)
          }}
          onConfirm={() => {
            void confirm()
          }}
        >
          Finder’s Put Back can restore it.
        </ConfirmDialog>
      )}
    </div>
  )
}
