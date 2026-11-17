import { autopilotRunBySession, type AutopilotTarget } from '../platform'
import { attempt } from './call'

// watchSessionWith returns what opens the run view of the agent's background run a session
// is, as the session panel's and the launch button's Watch do: open is how the window opens
// the Autopilot tab at a target, and say how it tells the user that the run is no longer in
// Autopilot's history, or why it couldn't be read.
export function watchSessionWith(
  open: (target: AutopilotTarget) => void,
  say: (text: string) => void,
): (sessionId: string) => void {
  return (sessionId) => {
    void attempt(autopilotRunBySession(sessionId)).then((result) => {
      if (!result.ok) {
        say(result.error)
      } else if (result.data === 0) {
        say('That run is no longer in Autopilot’s history.')
      } else {
        open({ routineId: 0, matchId: 0, runId: result.data, createdAt: '', activity: false })
      }
    })
  }
}
