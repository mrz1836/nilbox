import { useEffect, useState } from 'react'

import { formatCountdown } from '../lib/time'
import { useNow } from '../lib/useNow'

export interface RetryStatusProps {
  // at is when the inbox tries the refresh again, as RFC 3339 text.
  at: string
  // failures counts the refreshes that failed in a row, the last included.
  failures: number
  // error is what the last one failed with.
  error: string
}

// tickMs is how often the countdown moves on.
const tickMs = 1000

// RetryStatus says, beside the time of the last refresh, that a refresh failed for a
// reason a later try may well not meet, as GitHub answering a 500, and counts down to the
// inbox's next try, in place of the error banner, since it needs nothing of the user: a
// small pill in the warning color, whose tooltip holds the error and how many tries
// failed. The Refresh button beside it tries at once.
//
// The window's clock moves by the minute, so the countdown ticks each second on an
// interval of its own while it shows, from the later of that clock (moved on as each
// inbox answer arrives) and its own last tick. A status region tells a screen reader once
// that the inbox will try again, never each second.
export function RetryStatus({ at, failures, error }: RetryStatusProps) {
  const shown = useNow()
  const [ticked, setTicked] = useState(0)

  useEffect(() => {
    const timer = setInterval(() => {
      setTicked(Date.now())
    }, tickMs)

    return () => {
      clearInterval(timer)
    }
  }, [])

  const now = Math.max(shown.getTime(), ticked)
  const left = (Date.parse(at) - now) / 1000
  const tries = failures === 1 ? 'Tried once' : `Tried ${String(failures)} times`

  return (
    <span
      title={`${error}\n${tries}; trying again on its own.`}
      className="inline-flex min-w-0 items-center gap-1.5 rounded-full border border-warning/45 bg-warning/10 px-2 py-0.5 text-xs whitespace-nowrap text-warning"
    >
      <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-warning motion-safe:animate-pulse" />
      <span role="status" className="sr-only">
        The last refresh failed. The inbox will try again on its own.
      </span>
      <span aria-hidden="true">
        Couldn’t refresh ·{' '}
        {left > 0 ? (
          <>
            retrying in <span className="tabular-nums">{formatCountdown(left)}</span>
          </>
        ) : (
          'retrying now…'
        )}
      </span>
    </span>
  )
}
