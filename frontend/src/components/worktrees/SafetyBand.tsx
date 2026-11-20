import type { ReactNode } from 'react'

import type { Safety, SafetyIcon, Words as WordsData } from '../../lib/worktrees'
import { AlertIcon, ArrowUpIcon, GhostIcon, InfoIcon, LockIcon, RingIcon, ShieldCheckIcon } from '../icons'
import { CopyButton } from './CopyButton'

// Words show a sentence as the page words it (lib/worktrees' Words): its plain text, and
// the parts set in code, such as a branch's name, as code.
export function Words({ words }: { words: WordsData }) {
  return words.map((part, i) =>
    typeof part === 'string' ? (
      part
    ) : (
      // A sentence's parts never move, so their places key them.
      <code key={i} className="font-mono text-[0.71875rem]">
        {part.code}
      </code>
    ),
  )
}

// safetyIcons draw the mark of each answer.
const safetyIcons: Readonly<Record<SafetyIcon, (className: string) => ReactNode>> = {
  alert: (className) => <AlertIcon className={className} />,
  ghost: (className) => <GhostIcon className={className} />,
  info: (className) => <InfoIcon className={className} />,
  lock: (className) => <LockIcon className={className} />,
  up: (className) => <ArrowUpIcon className={className} />,
  ring: (className) => <RingIcon className={className} />,
  shield: (className) => <ShieldCheckIcon className={className} />,
}

// tones color the band and its mark by its tone: green when nothing would be lost, amber
// when work would be, or is only here, blue while something runs in it, and red for a real
// problem, each spelled out in full so Tailwind finds it.
const tones: Readonly<Record<Safety['tone'], { band: string; icon: string }>> = {
  success: { band: 'bg-success/9', icon: 'text-success' },
  warning: { band: 'bg-warning/10', icon: 'text-warning' },
  info: { band: 'bg-info/11', icon: 'text-info' },
  danger: { band: 'bg-danger/9', icon: 'text-danger' },
}

export interface SafetyBandProps {
  // safety is what removing the worktree would cost (lib/worktrees' safety).
  safety: Safety
  // className adds to the band's own classes, such as a border where it stands alone, as
  // in a confirmation; by default it's a drawer's top, a rule under it.
  className?: string
}

// SafetyBand says what removing a worktree would cost, in one sentence and its tone, with
// a command to copy when there's one to run first, and a second, muted line when there's
// more to say.
export function SafetyBand({ safety, className = 'border-b border-border/70' }: SafetyBandProps) {
  const tone = tones[safety.tone]

  return (
    <div
      data-tone={safety.tone}
      className={`flex items-start gap-2.25 px-3 py-2 text-[0.78125rem] ${tone.band} ${className}`}
    >
      {safetyIcons[safety.icon](`mt-0.5 size-3.5 shrink-0 ${tone.icon}`)}
      <div className="flex min-w-0 flex-col">
        <span className="font-medium">
          <Words words={safety.text} />
        </span>
        {safety.command !== '' && (
          <span className="mt-1.25 inline-flex max-w-full min-w-0 items-center gap-1.5">
            <code
              title={safety.command}
              className="min-w-0 truncate rounded-sm border border-border/70 bg-surface/80 px-1.25 font-mono text-[0.71875rem]"
            >
              {safety.command}
            </code>
            <CopyButton text={safety.command} what="command" label="Copy" />
          </span>
        )}
        {safety.also.length > 0 && (
          <span className="mt-px text-muted">
            <Words words={safety.also} />
          </span>
        )}
      </div>
    </div>
  )
}
