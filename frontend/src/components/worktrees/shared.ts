// Wording and button styles shared by the pieces of the worktrees view.

// plural is "1 file" or "3 files".
export function plural(n: number, word: string): string {
  return `${String(n)} ${word}${n === 1 ? '' : 's'}`
}

// buttonBase is the shape of the view's buttons; each adds its own border color.
export const buttonBase = 'rounded-md border px-3 py-1 text-sm font-medium disabled:opacity-60'

export const plainButton = `${buttonBase} border-border bg-surface hover:bg-raised`
