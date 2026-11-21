import type { CSSProperties, ReactNode } from 'react'

import { darkTokens } from '../lib/theme'

export interface TerminalProps {
  // label names the figure for assistive tech: "Terminal", "Setup log".
  label: string
  // title is the window's title, the folder it runs in: "~/inbox".
  title: string
  // command is what the prompt types out once the window is up.
  command: string
  // className places the figure and caps its width, such as "mt-8".
  className?: string
  // delay is how long, in milliseconds, the window waits before it rises in.
  delay: number
  // children are the lines the command prints, each staggered in with lineStyle.
  children: ReactNode
}

// Terminal is a terminal window drawn in the page, as the inbox at nil's party and the
// setup's Finish log show one: its three dots and title, a $ prompt that types the command
// out (nil-type), then the lines it prints. It rises in (nil-rise) after delay, and wears
// the dark palette in either theme (darkTokens), as a terminal does.
export function Terminal({ label, title, command, className = '', delay, children }: TerminalProps) {
  return (
    <figure
      aria-label={label}
      className={`${className} nil-rise w-full overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-body leading-6 text-text shadow-terminal`.trim()}
      style={{ ...darkTokens, animationDelay: `${String(delay)}ms` }}
    >
      <div className="flex items-center gap-1.5 border-b border-border bg-raised px-3 py-2">
        <span aria-hidden="true" className="size-2.5 rounded-full bg-danger" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-warning" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-success" />
        <span className="flex-1 pr-10 text-center text-xs text-muted">{title}</span>
      </div>
      <div className="px-4 py-3">
        <p>
          <span className="text-success">$</span>{' '}
          <span className="nil-type" style={{ '--n': String(command.length) } as CSSProperties}>
            {command}
          </span>
        </p>
        {children}
      </div>
    </figure>
  )
}

// lineStyle staggers a line's entrance (nil-line) after the command is typed, by its place
// among the lines, at.
export function lineStyle(at: number): CSSProperties {
  return { '--i': String(at) } as CSSProperties
}
