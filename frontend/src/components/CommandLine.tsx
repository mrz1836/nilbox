import { copyLabel, useCopy } from '../lib/useCopy'
import { quietButtonClass } from './controls'
import { CheckIcon, CopyIcon } from './icons'

// copiedMs is how long the button says how a copy went before it offers to copy again.
export { copiedMs } from '../lib/useCopy'

export interface CommandLineProps {
  // command is the shell command, as the user types it.
  command: string
  // prompt shows a $ before the command, as for one to run in a terminal; false shows
  // the line alone, as for one to put in a file.
  prompt?: boolean
  // className places the line, such as its width.
  className?: string
}

// CommandLine shows a shell command to run in a terminal, with a button that copies it.
// The button says how the copy went for a moment, and a screen reader hears so. A click
// on the command selects all of it, so it can be copied by hand when the clipboard
// refuses.
export function CommandLine({ command, prompt = true, className = '' }: CommandLineProps) {
  const { copy, onCopy } = useCopy('command')

  return (
    <div
      className={`flex min-w-0 items-center gap-2 rounded-md border border-border bg-background py-1 pr-1 pl-2.5 ${className}`.trim()}
    >
      <code className="min-w-0 flex-1 truncate font-mono text-xs text-text select-all" title={command}>
        {prompt && (
          <span aria-hidden="true" className="text-muted select-none">
            ${' '}
          </span>
        )}
        {command}
      </code>
      <button
        type="button"
        aria-label="Copy the command"
        onClick={() => {
          onCopy(command)
        }}
        className={`${quietButtonClass} shrink-0`}
      >
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
