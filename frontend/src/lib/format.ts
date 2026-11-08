// Small display helpers shared by the views.

const counts = new Intl.NumberFormat()

// formatCount formats a whole number with the user's digit grouping, e.g. "4,980".
export function formatCount(n: number): string {
  return counts.format(n)
}

// errorMessage is the text to show for a failed call. Errors from the bound Go services
// arrive as Error instances whose message is the error's full text, for the console, and
// whose cause carries the sentence the backend worded for the user, which wins. An error
// without one shows its message verbatim.
export function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    const worded = userMessage(err.cause)
    if (worded !== '') {
      return worded
    }

    if (err.message !== '') {
      return err.message
    }
  }

  if (typeof err === 'string' && err !== '') {
    return err
  }

  return 'Something went wrong.'
}

// userMessage reads the message for the user from a failed call's cause, or '' when the
// cause carries none.
function userMessage(cause: unknown): string {
  if (typeof cause !== 'object' || cause === null || !('message' in cause)) {
    return ''
  }

  return typeof cause.message === 'string' ? cause.message : ''
}

// counted reads a count with its noun, e.g. "1 file" or "1,204 files".
export function counted(n: number, one: string, many: string): string {
  return `${formatCount(n)} ${n === 1 ? one : many}`
}
