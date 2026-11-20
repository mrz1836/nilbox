import { useEffect, useEffectEvent } from 'react'

import { on } from '../platform'

// WindowEvent names an event the shell sends the window, and Listener is what on() calls
// with its data, both as the platform's on() takes them.
type WindowEvent = Parameters<typeof on>[0]
type Listener<E extends WindowEvent> = Parameters<typeof on<E>>[1]

// useOn calls listener with the data of every event named event that the shell sends,
// from the moment the component mounts, or enabled turns true, until it unmounts, or
// enabled turns false. The listener is always the latest one rendered, so a new one never
// subscribes again; a new event or enabled does.
export function useOn<E extends WindowEvent>(event: E, listener: Listener<E>, enabled = true): void {
  const fire = useEffectEvent(listener)

  useEffect(
    () =>
      enabled
        ? on(event, (data) => {
            fire(data)
          })
        : undefined,
    [event, enabled],
  )
}
