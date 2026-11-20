import type { ReactNode } from 'react'

import { copyLabel, useCopy } from '../../lib/useCopy'
import { quietButtonClass } from '../controls'
import { AlertIcon, CheckIcon, CopyIcon } from '../icons'

export interface CopyButtonProps {
  // text is what the button copies.
  text: string
  // what names the text, in the console when the clipboard refuses it, as in "path".
  what: string
  // label is the button's words, which say for a moment how the copy went; with none it
  // shows its icon alone, named by ariaLabel.
  label?: string
  ariaLabel?: string
  title?: string | undefined
  // icon is the button's mark until a copy is made; the copy icon by default.
  icon?: ReactNode
  disabled?: boolean
  className?: string
}

// CopyButton copies a text to the clipboard: a quiet button whose mark, and words when it
// has any, say for a moment how the copy went, as a screen reader hears.
export function CopyButton({
  text,
  what,
  label,
  ariaLabel,
  title,
  icon = <CopyIcon className="size-3" />,
  disabled = false,
  className = quietButtonClass,
}: CopyButtonProps) {
  const { copy, onCopy } = useCopy(what)

  return (
    <>
      <button
        type="button"
        aria-label={ariaLabel}
        title={title}
        disabled={disabled}
        onClick={() => {
          onCopy(text)
        }}
        className={className}
      >
        {copy === 'copied' && <CheckIcon className="size-3 text-success" />}
        {copy === 'failed' && <AlertIcon className="size-3 text-warning" />}
        {copy === 'idle' && icon}
        {label !== undefined && (copy === 'idle' ? label : copyLabel[copy])}
      </button>
      <span role="status" className="sr-only">
        {copy === 'copied' && 'Copied to the clipboard'}
        {copy === 'failed' && `Couldn’t copy the ${what}`}
      </span>
    </>
  )
}
