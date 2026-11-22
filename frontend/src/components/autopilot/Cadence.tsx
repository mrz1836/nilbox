import { cadenceOf, untilWords, type GitHubReads } from '../../lib/autopilot'
import { formatAgo } from '../../lib/time'
import type { AutopilotEngine, AutopilotGlance } from '../../platform'
import { quietButtonClass } from '../controls'
import { RelativeTime } from '../RelativeTime'

export interface CadenceProps {
  glance: AutopilotGlance | null
  engine: AutopilotEngine | null
  // reads is how the app reads GitHub on its own, null until the settings and the inbox load.
  reads: GitHubReads | null
  // onRefresh reads the inbox from GitHub now, as the inbox's Refresh does.
  onRefresh?: (() => void) | undefined
  // compact leaves out how often, for a routine's pane: Activity says it in full.
  compact?: boolean
  className?: string
}

// Cadence says when Autopilot runs: a dot and Running while it checks the routines, when it
// last did, and when the app reads GitHub next (counting down), or that it reads it now,
// with Refresh now; then how often it does each, which is what it acts on and when (compact
// leaves that out). It says why it runs nothing when no routine is on, or it starts; the
// banners say what stops everything (Pause, another copy, signed out), so it says nothing
// then.
export function Cadence({ glance, engine, reads, onRefresh, compact = false, className = '' }: CadenceProps) {
  const cadence = cadenceOf(glance, engine, reads)

  if (cadence === null) {
    return null
  }

  return (
    <div data-testid="autopilot-cadence" className={`flex flex-col gap-1 ${className}`}>
      <p className="flex min-h-6 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted">
        {cadence.running && (
          <span aria-hidden="true" className="mr-0.5 size-1.5 shrink-0 rounded-full bg-accent ring-2 ring-accent/25" />
        )}
        <span className={cadence.running ? 'font-medium text-text' : ''}>{cadence.text}</span>
        {cadence.lastAt !== '' && (
          <span>
            {'· last check '}
            <RelativeTime iso={cadence.lastAt} show={formatAgo} />
          </span>
        )}
        {cadence.reading ? (
          <span role="status">· reading GitHub now…</span>
        ) : (
          cadence.nextAt !== '' && (
            <span>
              {'· reads GitHub again '}
              <RelativeTime iso={cadence.nextAt} show={untilWords} />
            </span>
          )
        )}
        {onRefresh !== undefined && (
          <button
            type="button"
            disabled={cadence.reading}
            onClick={onRefresh}
            title="Read your inbox and your routines’ searches from GitHub now, as the inbox’s Refresh does"
            className={`${quietButtonClass} ml-1`}
          >
            Refresh now
          </button>
        )}
      </p>
      {!compact && cadence.how !== '' && <p className="text-xs text-muted">{cadence.how}</p>}
    </div>
  )
}
