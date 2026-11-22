import { cadenceOf } from '../../lib/autopilot'
import { formatAgo } from '../../lib/time'
import type { AutopilotEngine, AutopilotGlance } from '../../platform'
import { RelativeTime } from '../RelativeTime'

export interface CadenceProps {
  glance: AutopilotGlance | null
  engine: AutopilotEngine | null
  className?: string
}

// Cadence says when Autopilot runs the routines: every minute, after each refresh of the
// inbox, and at once on a change to one, with when it last checked them; or why it runs
// none now. The banners say what stops everything (Pause, another copy, signed out), so
// it says nothing then.
export function Cadence({ glance, engine, className = '' }: CadenceProps) {
  const cadence = cadenceOf(glance, engine)

  if (cadence === null) {
    return null
  }

  return (
    <p data-testid="autopilot-cadence" className={`text-xs text-muted ${className}`}>
      {cadence.text}
      {cadence.lastAt !== '' && (
        <>
          {' · last check '}
          <RelativeTime iso={cadence.lastAt} show={formatAgo} />
        </>
      )}
    </p>
  )
}
