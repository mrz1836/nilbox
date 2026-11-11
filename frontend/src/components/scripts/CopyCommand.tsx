import { useState } from 'react'

import { useMutation } from '../../lib/useMutation'
import { copyText } from '../../platform'
import { quietButtonClass } from '../controls'
import { CheckIcon, CopyIcon } from '../icons'
import { ErrorText } from '../settings/rows'

export interface CopyCommandProps {
  // command is the command shown, for the user to run themselves; the app never does.
  command: string
  // label says what the command does, before it, as in "To push it:".
  label?: string
}

// CopyCommand shows a command to run in the terminal, with a button that copies it. It
// says so once copied, and why when the clipboard refused it.
export function CopyCommand({ command, label = '' }: CopyCommandProps) {
  const copy = useMutation(() => copyText(command))
  const [copied, setCopied] = useState(false)

  const copyIt = async () => {
    setCopied(false)
    setCopied((await copy.run()).ok)
  }

  return (
    <div className="flex flex-col gap-1">
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
        {label !== '' && <span className="text-muted">{label}</span>}
        <code className="rounded bg-background px-1.5 py-0.5 font-mono break-all text-text">{command}</code>
        <button
          type="button"
          aria-label={`Copy ${command}`}
          disabled={copy.pending}
          onClick={() => {
            void copyIt()
          }}
          className={quietButtonClass}
        >
          {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </p>
      <ErrorText error={copy.error} />
    </div>
  )
}
