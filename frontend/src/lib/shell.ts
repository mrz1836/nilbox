// Lines for the user's shell that the window offers to copy, which the app never writes
// into a file itself.

// exportLine is the line for ~/.zprofile that puts folder on the PATH: double-quoted, with
// the characters special inside double quotes escaped, and a leading ~ written as $HOME,
// since the shell expands $HOME inside double quotes but not ~.
export function exportLine(folder: string): string {
  const quoted = folder.replace(/["$`\\]/g, '\\$&').replace(/^~(?=\/|$)/, '$$HOME')

  return `export PATH="${quoted}:$PATH"`
}
