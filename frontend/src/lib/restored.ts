import type { Restored } from '../platform'
import { counted } from './format'

// cameBack names what a start brought back of the user's own: "your prompt", "your 4
// prompts and 2 launch defaults", or "your 4 prompts, 2 launch defaults, and 1 script".
export function cameBack(r: Restored): string {
  if (r.prompts + r.launchDefaults + r.scripts === 1) {
    if (r.prompts === 1) {
      return 'your prompt'
    }

    return r.launchDefaults === 1 ? 'your launch default' : 'your script'
  }

  const parts = [
    ...(r.prompts > 0 ? [counted(r.prompts, 'prompt', 'prompts')] : []),
    ...(r.launchDefaults > 0 ? [counted(r.launchDefaults, 'launch default', 'launch defaults')] : []),
    ...(r.scripts > 0 ? [counted(r.scripts, 'script', 'scripts')] : []),
  ]

  const last = parts.pop() ?? ''

  if (parts.length === 0) {
    return `your ${last}`
  }

  return `your ${parts.join(', ')}${parts.length > 1 ? ',' : ''} and ${last}`
}
