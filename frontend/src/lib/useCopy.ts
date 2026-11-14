import { useEffect, useState } from 'react'

import { copyText } from '../platform'

// copiedMs is how long a copy button says how a copy went before it offers to copy again.
export const copiedMs = 2000

// Copy is where the last copy stands: not tried, done, or refused by the clipboard.
export type Copy = 'idle' | 'copied' | 'failed'

// copyLabel is what a copy button says at each.
export const copyLabel: Readonly<Record<Copy, string>> = { idle: 'Copy', copied: 'Copied', failed: 'Copy failed' }

// useCopy copies text to the clipboard and says how it went for copiedMs, then goes back
// to idle. what names the text in the console when the clipboard refuses it.
export function useCopy(what: string): { copy: Copy; onCopy: (text: string) => void } {
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

  const onCopy = (text: string) => {
    copyText(text).then(
      () => {
        setCopy('copied')
      },
      (err: unknown) => {
        console.error(`could not copy the ${what}`, err)
        setCopy('failed')
      },
    )
  }

  return { copy, onCopy }
}
