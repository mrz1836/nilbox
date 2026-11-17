import { useEffect, useState, type ReactNode } from 'react'

// countAfter is how many seconds a read runs before the banner counts them.
const countAfter = 3

export interface GitHubReadingProps {
  // icon stands for what is read, drawn inside the pulsing ring.
  icon: ReactNode
  // heading says what is read, as in "Filling your inbox from GitHub…".
  heading: ReactNode
  // coming are the parts the read brings, each with a pulsing mark.
  coming: readonly string[]
}

// GitHubReading says something is read from GitHub for the first time, and what that read
// brings: an accent ring pulses out from its icon, each part to come carries a pulsing
// mark, and once the read has run a few seconds it counts them, so a slow GitHub reads as
// slow rather than stuck. It is a named region, not a live one: a status beside it already
// announces that GitHub is being read. The review and issue sheets read an item so, and the
// inbox its first fill.
export function GitHubReading({ icon, heading, coming }: GitHubReadingProps) {
  const [seconds, setSeconds] = useState(0)

  useEffect(() => {
    const timer = setInterval(() => {
      setSeconds((s) => s + 1)
    }, 1000)

    return () => {
      clearInterval(timer)
    }
  }, [])

  return (
    <section
      aria-label="Reading from GitHub"
      className="flex items-start gap-3 rounded-md border border-border bg-raised/50 p-3"
    >
      <span aria-hidden="true" className="relative mt-0.5 flex size-8 shrink-0 items-center justify-center">
        <span className="absolute inset-0 rounded-full bg-accent/25 motion-safe:animate-ping" />
        <span className="relative flex size-8 items-center justify-center rounded-full bg-accent/15">{icon}</span>
      </span>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-2 text-sm font-medium">
          <span>{heading}</span>
          {seconds >= countAfter && (
            <span aria-hidden="true" className="text-xs font-normal text-muted tabular-nums">
              {seconds}s
            </span>
          )}
        </p>
        <ul aria-label="Coming from GitHub" className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
          {coming.map((part, i) => (
            <li key={part} className="flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className="size-1.5 rounded-full bg-accent motion-safe:animate-pulse"
                style={{ animationDelay: `${String(i * 250)}ms` }}
              />
              {part}
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
