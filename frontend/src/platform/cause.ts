// Reading what a failed call to the desktop shell carries. A bound Go service's error
// reaches the window as an Error whose cause holds the backend's words for the user and
// its detail. The platform reads the refusals it answers from the detail, and the window
// words a failure from the message (errorMessage), both through causeOf. It lives apart
// from the platform module, which tests replace with a fake, so every caller reads a
// failure the same way whatever stands in for the shell.

// causeOf reads what a failed call's cause carries beside its message: the backend's words
// for the user, and its detail, or null when the failure carries no cause.
export function causeOf(err: unknown): { detail: unknown; message: string } | null {
  if (!(err instanceof Error) || typeof err.cause !== 'object' || err.cause === null) {
    return null
  }

  const { cause } = err
  const message = 'message' in cause && typeof cause.message === 'string' ? cause.message : ''

  return { detail: 'detail' in cause ? cause.detail : undefined, message }
}
