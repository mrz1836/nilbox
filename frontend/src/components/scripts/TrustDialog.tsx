import { useId, useState } from 'react'

import { changedCommand, trustTitle } from '../../lib/repoScripts'
import type { ScriptQuestion } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { AlertIcon, InfoIcon } from '../icons'

// commandClass sets a command apart as the text that runs: monospace, kept whole.
export const commandClass =
  'rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-[0.8125rem] break-all whitespace-pre-wrap text-text'

export interface TrustDialogProps {
  question: ScriptQuestion
  // repo is the clone's repository, owner/name, under which "Don't ask again" keeps the
  // command trusted, or '' for a clone whose origin isn't on GitHub, which asks every
  // time; name is what the page calls the clone, and dir its folder, with ~.
  repo: string
  name: string
  dir: string
  // fromBranch is the branch the clone is on when it isn't the default, or ''.
  fromBranch: string
  // onRun runs the command as the question showed it, remembering it when remember is
  // set; onCancel runs nothing.
  onRun: (remember: boolean) => void
  onCancel: () => void
}

// TrustDialog asks before a command from a repository's own files runs (D13): the command
// as it will run, inputs filled in; that it runs in the clone with the user's
// permissions; and "Don't ask again" for it, ticked. A changed command shows what was
// trusted before above it, under "Changed since you last ran it"; a clone on another
// branch says so, since a branch can change what the files say; and a custom magex
// target says it runs the repository's own magefiles code.
export function TrustDialog({ question, repo, name, dir, fromBranch, onRun, onCancel }: TrustDialogProps) {
  const id = useId()
  const [remember, setRemember] = useState(true)
  const before = changedCommand(question)
  const changed = question.trust === 'changed'

  return (
    <ConfirmDialog
      title={trustTitle(question, name)}
      confirmLabel="Run"
      tone="primary"
      size="wide"
      onConfirm={() => {
        onRun(repo !== '' && remember)
      }}
      onCancel={onCancel}
    >
      <div className="flex flex-col gap-3">
        {changed && (
          <h3 className="flex items-center gap-1.5 text-sm font-semibold text-text">
            <AlertIcon className="size-4 shrink-0 text-warning" />
            Changed since you last ran it
          </h3>
        )}
        {before === '' ? (
          <div className="flex flex-col gap-1.5">
            <p>This command comes from the repository, not from you:</p>
            <pre className={commandClass}>{question.command}</pre>
          </div>
        ) : (
          <dl className="flex flex-col gap-1.5">
            <dt>You ran</dt>
            <dd className={commandClass}>{before}</dd>
            <dt>It now runs</dt>
            <dd className={commandClass}>{question.command}</dd>
          </dl>
        )}
        {question.trustNote !== '' && <p className="text-text">{question.trustNote}</p>}
        {fromBranch !== '' && (
          <p className="flex items-start gap-1.5">
            <InfoIcon className="mt-0.5 size-4 shrink-0 text-info" />
            <span>
              From branch <span className="font-mono text-text">{fromBranch}</span>, which can change what the
              repository’s files say.
            </span>
          </p>
        )}
        <p>
          It runs in <span className="font-mono break-all text-text">{dir}</span> with your permissions, as if you typed
          it in a terminal.
        </p>
        {repo !== '' && (
          <label htmlFor={`${id}-remember`} className="flex cursor-pointer items-center gap-2 text-text">
            <input
              id={`${id}-remember`}
              type="checkbox"
              checked={remember}
              onChange={(e) => {
                setRemember(e.target.checked)
              }}
              className="checkbox"
            />
            Don’t ask again for this command in {repo}
          </label>
        )}
      </div>
    </ConfirmDialog>
  )
}
