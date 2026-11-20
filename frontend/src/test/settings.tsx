import { render, type RenderResult } from '@testing-library/react'
import { useState, type ComponentType } from 'react'
import { vi, type Mock } from 'vitest'

import type { Settings } from '../platform'
import { at, FIXED_NOW } from './clock'

// SavingProps are what a settings page hands what it shows: the settings as stored, null
// while they load where S allows it, and onSaved, which takes the settings a change stored.
export interface SavingProps<S extends Settings | null = Settings> {
  settings: S | Settings
  onSaved: (settings: Settings) => void
}

export interface SavingOptions {
  // now is the time the clock stands at, FIXED_NOW unless given; null renders on the
  // window's own clock, which reads the real time, for a test that never wrapped one.
  now?: Date | null
}

// Saving is what renderSaving returns: the render, and onSaved, the mock that every
// settings a change stored reach.
export type Saving = RenderResult & { onSaved: Mock<(settings: Settings) => void> }

// renderSaving renders View as a settings page holds it: the settings it is shown start as
// saved, and each one a change stores reaches onSaved and is shown from then on, so a
// second change starts from the first. View is a component, so it may call hooks; it takes
// any other props it needs from the test, as in
//
//   renderSaving((props) => <ScriptsCard {...props} tools={tools} />, saved)
//
// It renders on a clock standing at FIXED_NOW unless options say otherwise.
export function renderSaving<S extends Settings | null = Settings>(
  View: ComponentType<SavingProps<S>>,
  saved: S,
  { now = FIXED_NOW }: SavingOptions = {},
): Saving {
  const onSaved = vi.fn<(settings: Settings) => void>()

  function Stored() {
    const [current, setCurrent] = useState<S | Settings>(saved)

    return (
      <View
        settings={current}
        onSaved={(next) => {
          onSaved(next)
          setCurrent(next)
        }}
      />
    )
  }

  const view = render(<Stored />, now === null ? {} : { wrapper: at(now) })

  return { ...view, onSaved }
}
