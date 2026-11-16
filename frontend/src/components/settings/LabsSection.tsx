import { useId } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setAutopilotEnabled } from '../../platform'
import { Group, SwitchRow } from './rows'
import type { SectionProps } from './types'

// LabsSection holds the parts of the app still being built, each off until the user turns
// it on, and saved as soon as it is switched: Autopilot (beta), which shows the Autopilot
// tab, its settings, and its marks, and runs its routines. Turned off, Autopilot is hidden
// and starts nothing new; its routines and history stay as they are.
export function LabsSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const config = settings.autopilot

  return (
    <Group>
      <SwitchRow
        label="Autopilot (beta)"
        controlId={`${id}-autopilot`}
        description="Routines that triage and fix pull requests for you. Still being built."
        checked={config.enabled}
        error={error}
        onChange={(enabled) => {
          void save({ autopilot: { ...config, enabled } }, () => setAutopilotEnabled(enabled))
        }}
      />
    </Group>
  )
}
