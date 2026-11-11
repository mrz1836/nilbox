import { pings, pulses, stateDot } from '../lib/sessions'
import type { AgentSession } from '../platform'
import { StatusDot } from './StatusDot'

// SessionDot is a session's status dot, drawn alike on the card's tag and in the sessions
// panel: a ring swells slowly out of it while the session works, it pulses while it starts,
// and it holds still otherwise, as it does everywhere with reduced motion (StatusDot, which
// a running check shares). It is decorative: the text beside it says the state.
export function SessionDot({ state }: { state: AgentSession['state'] }) {
  return <StatusDot color={stateDot[state]} ping={pings(state)} pulse={pulses(state)} />
}
