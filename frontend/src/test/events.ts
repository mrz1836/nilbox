import type { Mock } from 'vitest'

// sendEvent sends an event as the shell would, to a test whose platform mock stands in a
// vi.fn() for on(): it calls the listener most recently passed for event, the one still
// subscribed after any effect that subscribed again. It fails the test when nothing
// listens for event.
export function sendEvent(on: Mock, event: string, data: unknown = {}): void {
  const call = on.mock.calls.findLast(([name]) => name === event)

  if (call === undefined) {
    throw new Error(`nothing listens for ${event}`)
  }

  ;(call[1] as (data: unknown) => void)(data)
}
