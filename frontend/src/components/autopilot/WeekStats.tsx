import { costWords, tokenWords, weekCounts } from '../../lib/routineWeek'
import { useQuery } from '../../lib/useQuery'
import { routineStats } from '../../platform'

export interface WeekStatsProps {
  // routineID is the routine, and version changes whenever what it did may have, so its
  // week is read again.
  routineID: number
  version: string
}

// WeekStats is a routine's This week: what its runs of the last seven days did (its agents'
// runs, the fixes among them, merges, reviews posted, GitHub actions, its scripts' runs),
// then what they spent, the dollars as the estimate they are and the tokens as reported.
export function WeekStats({ routineID, version }: WeekStatsProps) {
  const week = useQuery(() => routineStats(routineID), [routineID, version])
  const s = week.data

  if (week.error !== '') {
    return (
      <p role="alert" className="text-xs text-danger">
        {week.error}
      </p>
    )
  }

  if (s === undefined) {
    return <p className="text-xs text-muted">Reading the week…</p>
  }

  const counts = weekCounts(s)
  const spent = [costWords(s), tokenWords(s)].filter((w) => w !== '')

  if (counts.length === 0 && spent.length === 0) {
    return <p className="text-sm text-muted">Nothing ran in the last 7 days.</p>
  }

  return (
    <div className="flex flex-col gap-1">
      {counts.length > 0 && <p className="text-sm text-text">{counts.join(' · ')}</p>}
      {spent.length > 0 && (
        <p
          className="text-xs text-muted"
          title="A subscription's sign-in reports what the same work would cost through the API, so the dollars are an estimate; the tokens are what each run reported. A run in your terminal reports neither: its session panel shows its tokens."
        >
          {spent.join(' · ')}
        </p>
      )}
    </div>
  )
}
