import type { Tone } from '../../lib/hooks'
import { AlertIcon, CheckIcon, InfoIcon, RingIcon, XIcon } from '../icons'

// toneWords say each tone in words, for assistive technology, before a line's text.
export const toneWords: Readonly<Record<Tone, string>> = {
  ok: 'Done:',
  info: 'Note:',
  warn: 'Warning:',
  bad: 'Problem:',
  quiet: 'Skipped:',
}

export interface ToneIconProps {
  tone: Tone
  // className sizes and places the icon; its color is the tone's.
  className?: string
}

// ToneIcon marks a line's tone beside its words, so none is told by color alone: ✓ done,
// ℹ a note, ⚠ a warning, ✗ a problem, and ◌ set aside.
export function ToneIcon({ tone, className = 'size-3.5 shrink-0' }: ToneIconProps) {
  switch (tone) {
    case 'ok':
      return <CheckIcon className={`${className} text-success`} />
    case 'info':
      return <InfoIcon className={`${className} text-info`} />
    case 'warn':
      return <AlertIcon className={`${className} text-warning`} />
    case 'bad':
      return <XIcon className={`${className} text-danger`} />
    case 'quiet':
      return <RingIcon className={`${className} text-muted`} />
  }
}
