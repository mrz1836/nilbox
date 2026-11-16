import type { Restored } from '../platform'
import { counted } from './format'

// cameBack names what a start brought back of the user's own: "your prompt", "your 4
// prompts and 2 launch defaults", or "your 4 prompts, 2 launch defaults, 1 script, and 3
// Autopilot routines".
export function cameBack(r: Restored): string {
  if (r.prompts + r.launchDefaults + r.scripts + r.routines === 1) {
    if (r.prompts === 1) {
      return 'your prompt'
    }

    if (r.launchDefaults === 1) {
      return 'your launch default'
    }

    return r.scripts === 1 ? 'your script' : 'your Autopilot routine'
  }

  const parts = [
    ...(r.prompts > 0 ? [counted(r.prompts, 'prompt', 'prompts')] : []),
    ...(r.launchDefaults > 0 ? [counted(r.launchDefaults, 'launch default', 'launch defaults')] : []),
    ...(r.scripts > 0 ? [counted(r.scripts, 'script', 'scripts')] : []),
    ...(r.routines > 0 ? [counted(r.routines, 'Autopilot routine', 'Autopilot routines')] : []),
  ]

  const last = parts.pop() ?? ''

  if (parts.length === 0) {
    return `your ${last}`
  }

  return `your ${parts.join(', ')}${parts.length > 1 ? ',' : ''} and ${last}`
}
