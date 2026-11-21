import { copyLabel, useCopy } from '../../lib/useCopy'
import { quietButtonClass } from '../controls'
import { CheckIcon, CopyIcon } from '../icons'

export interface CopyCommandProps {
  // command is the command shown, for the user to run themselves; the app never does.
  command: string
  // label says what the command does, before it, as in "To push it:".
  label?: string
}

// CopyCommand shows a command to run in the terminal, with a button that copies it. The
// button says for a moment how the copy went, and a screen reader hears so, since the
// button's name is the command's.
export function CopyCommand({ command, label = '' }: CopyCommandProps) {
  const { copy, onCopy } = useCopy('command')

  return (
    <div className="flex flex-col gap-1">
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
        {label !== '' && <span className="text-muted">{label}</span>}
        <code className="rounded bg-background px-1.5 py-0.5 font-mono break-all text-text">{command}</code>
        <button
          type="button"
          aria-label={`Copy ${command}`}
          onClick={() => {
            onCopy(command)
          }}
          className={quietButtonClass}
        >
          {copy === 'copied' ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
          {copyLabel[copy]}
        </button>
      </p>
      <span role="status" className="sr-only">
        {copy === 'copied' && 'Copied to the clipboard'}
        {copy === 'failed' && `Couldn’t copy ${command}`}
      </span>
    </div>
  )
}
