import { brand } from '../generated/brand'
import type { TerminalID } from '../platform'

// terminalNames are the terminal apps a launch, or a run, can open in, by the names the
// apps go by.
export const terminalNames: Readonly<Record<TerminalID, string>> = {
  iterm2: 'iTerm2',
  warp: 'Warp',
  terminal: 'Terminal',
}

// terminals lists them in the order Settings → Launching offers them, the order setup
// prefers them in.
export const terminals: readonly { id: TerminalID; label: string }[] = [
  { id: 'iterm2', label: terminalNames.iterm2 },
  { id: 'warp', label: terminalNames.warp },
  { id: 'terminal', label: terminalNames.terminal },
]

// terminalNote is what to do before the first launch in the terminal with id, as the setup
// and Settings → Launching say under the choice: allow iTerm2's Automation permission when
// macOS asks, and finish Warp's welcome, until which Warp drops every launch unseen; ''
// for Terminal.app, which needs nothing.
export function terminalNote(id: TerminalID): string {
  switch (id) {
    case 'iterm2':
      return `Your first launch asks to let ${brand.name} control iTerm2. Choose Allow.`
    case 'warp':
      return 'Open Warp once and finish its welcome before your first launch.'
    default:
      return ''
  }
}

// isTerminal reports an id this build names: one of terminalNames' own keys.
function isTerminal(id: string): id is TerminalID {
  return Object.hasOwn(terminalNames, id)
}

// terminalLabel names the terminal app with id, as terminalNames does, or says fallback
// for an id this build doesn't know and for none at all (null, or undefined).
export function terminalLabel(id: string | null | undefined, fallback: string): string {
  return id !== null && id !== undefined && isTerminal(id) ? terminalNames[id] : fallback
}
