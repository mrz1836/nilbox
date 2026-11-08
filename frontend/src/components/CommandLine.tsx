import { useEffect, useState } from 'react'

import { copyText } from '../platform'
import { quietButtonClass } from './controls'
import { CheckIcon, CopyIcon } from './icons'

// copiedMs is how long the button says how a copy went before it offers to copy again.
export const copiedMs = 2000

// Copy is where the last copy stands: not tried, done, or refused by the clipboard.
type Copy = 'idle' | 'copied' | 'failed'

// copyLabel is what the button says at each.
const copyLabel: Readonly<Record<Copy, string>> = { idle: 'Copy', copied: 'Copied', failed: 'Copy failed' }

export interface CommandLineProps {
  // command is the shell command, as the user types it.
  command: string
  // className places the line, such as its width.
  className?: string
}

// CommandLine shows a shell command to run in a terminal, with a button that copies it.
// The button says how the copy went for a moment, and a screen reader hears so. A click
// on the command selects all of it, so it can be copied by hand when the clipboard
// refuses.
export function CommandLine({ command, className = '' }: CommandLineProps) {
  const [copy, setCopy] = useState<Copy>('idle')

  useEffect(() => {
    if (copy === 'idle') {
      return undefined
    }

    const timer = setTimeout(() => {
      setCopy('idle')
    }, copiedMs)

    return () => {
      clearTimeout(timer)
    }
  }, [copy])

  const onCopy = () => {
    copyText(command).then(
      () => {
        setCopy('copied')
      },
      (err: unknown) => {
        console.error('could not copy the command', err)
        setCopy('failed')
      },
    )
  }

  return (
    <div
      className={`flex min-w-0 items-center gap-2 rounded-md border border-border bg-background py-1 pr-1 pl-2.5 ${className}`.trim()}
    >
      <code className="min-w-0 flex-1 truncate font-mono text-xs text-text select-all" title={command}>
        <span aria-hidden="true" className="text-muted select-none">
          ${' '}
        </span>
        {command}
      </code>
      <button type="button" aria-label="Copy the command" onClick={onCopy} className={`${quietButtonClass} shrink-0`}>
        {copy === 'copied' ? <CheckIcon className="size-3.5 text-success" /> : <CopyIcon className="size-3.5" />}
        {copyLabel[copy]}
      </button>
      <span role="status" className="sr-only">
        {copy === 'copied' && 'Copied to the clipboard'}
        {copy === 'failed' && 'Could not copy. Select the command and copy it by hand.'}
      </span>
    </div>
  )
}
