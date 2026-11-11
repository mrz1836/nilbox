import { pings, pulses, stateDot } from '../lib/sessions'
import type { AgentSession } from '../platform'

// SessionDot is a session's status dot, drawn alike on the card's tag and in the sessions
// panel: a ring swells slowly out of it while the session works, it pulses while it starts,
// and it holds still otherwise, as it does everywhere with reduced motion. It is
// decorative: the text beside it says the state.
export function SessionDot({ state }: { state: AgentSession['state'] }) {
  const dot = stateDot[state]

  return (
    <span aria-hidden="true" className="relative flex size-2 shrink-0 items-center justify-center">
      {pings(state) && (
        <span className={`absolute inset-0 rounded-full ${dot} [animation-duration:2.2s] motion-safe:animate-ping`} />
      )}
      <span className={`relative size-2 rounded-full ${dot} ${pulses(state) ? 'motion-safe:animate-pulse' : ''}`} />
    </span>
  )
}
