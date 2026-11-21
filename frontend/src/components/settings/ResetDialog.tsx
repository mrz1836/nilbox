import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { useMutation } from '../../lib/useMutation'
import { resetApp } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { ErrorText, fieldClass } from '../controls'

// confirmWord is what the user types to confirm the reset (internal/setup's ResetWord).
const confirmWord = 'reset'

// wiped and kept say what a reset starts over and what it keeps.
const wiped = [
  'Settings, repository locations, and the repositories you pinned or muted',
  'Triage, the cached inbox, and its notification history',
  'The mirror of your GitHub notifications',
  'Worktree records, session history, run history, script trust, and tool records',
  'Cached diffs, conversations, and profile pictures',
]

const kept = [
  'Your prompts, launch defaults, and your own scripts',
  'Your Autopilot routines, switched off',
  'Your backups and logs',
  'Your clones and worktrees on disk, your agents’ own config, and GitHub itself',
]

export interface ResetDialogProps {
  onCancel: () => void
}

// ResetDialog asks before starting the app over: what goes, what stays, and the word to
// type before Reset and restart turns on. Confirmed, the app saves a backup of the whole
// setup first, then quits and opens again on the first-run setup, its prompts, launch
// defaults, and scripts brought back; the dialog says it is restarting meanwhile.
export function ResetDialog({ onCancel }: ResetDialogProps) {
  const id = useId()
  const [typed, setTyped] = useState('')
  const resetter = useMutation(() => resetApp(confirmWord))
  // savedTo is where the backup went, once the reset was asked for and the app restarts.
  const [savedTo, setSavedTo] = useState<string | null>(null)
  const confirmed = typed.trim() === confirmWord
  const restarting = resetter.pending || savedTo !== null

  const reset = async () => {
    const result = await resetter.run()

    if (result.ok) {
      setSavedTo(result.data.savedTo)
    }
  }

  return (
    <ConfirmDialog
      title={`Reset ${brand.name}?`}
      confirmLabel={restarting ? 'Restarting…' : 'Reset and restart'}
      confirmDisabled={!confirmed || restarting}
      onConfirm={() => {
        void reset()
      }}
      onCancel={onCancel}
      size="wide"
    >
      <div className="flex flex-col gap-3">
        <p>
          {brand.name} saves a backup of your setup, then restarts on fresh data and walks you through the setup again.
        </p>
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <p className="font-medium text-text">Starts over</p>
            <ul className="mt-1 list-disc space-y-0.5 pl-4 text-xs">
              {wiped.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </div>
          <div>
            <p className="font-medium text-text">Stays</p>
            <ul className="mt-1 list-disc space-y-0.5 pl-4 text-xs">
              {kept.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </div>
        </div>
        <label htmlFor={`${id}-confirm`} className="text-sm text-text">
          Type <code className="font-mono font-semibold">{confirmWord}</code> to confirm
        </label>
        <input
          id={`${id}-confirm`}
          type="text"
          value={typed}
          autoComplete="off"
          spellCheck={false}
          disabled={restarting}
          aria-describedby={resetter.error === '' ? undefined : `${id}-error`}
          onChange={(e) => {
            setTyped(e.target.value)
          }}
          className={`${fieldClass} font-mono`}
        />
        {restarting && (
          <p role="status" className="text-xs text-muted">
            {savedTo === null || savedTo === ''
              ? 'Saving a backup of your setup…'
              : `Saved a backup to ${savedTo}. Restarting…`}
          </p>
        )}
        <ErrorText id={`${id}-error`} error={resetter.error} />
      </div>
    </ConfirmDialog>
  )
}
