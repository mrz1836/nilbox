// Small display helpers shared by the views.

const counts = new Intl.NumberFormat()

// formatCount formats a whole number with the user's digit grouping, e.g. "4,980".
export function formatCount(n: number): string {
  return counts.format(n)
}

// errorMessage is the text to show for a failed call. Errors from the bound Go services
// arrive as Error instances carrying the backend's message, which is shown verbatim.
export function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message !== '') {
    return err.message
  }

  if (typeof err === 'string' && err !== '') {
    return err
  }

  return 'Something went wrong.'
}
