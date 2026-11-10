import type { TerminalID } from '../platform'

// terminalNames are the terminal apps a launch, or a run, can open in, by the names the
// apps go by.
export const terminalNames: Readonly<Record<TerminalID, string>> = { iterm2: 'iTerm2', terminal: 'Terminal' }

// terminals lists them in the order Settings → Launching offers them.
export const terminals: readonly { id: TerminalID; label: string }[] = [
  { id: 'iterm2', label: terminalNames.iterm2 },
  { id: 'terminal', label: terminalNames.terminal },
]
