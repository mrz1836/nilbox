import { applySettings, type SettingsPart } from '../../../lib/describeit'
import { ChangeRow, type RowProps } from './parts'

// SettingsChange is a card's change to Autopilot's settings: each setting it changes, as
// "Agents at once 2 → 1", its problems and risks, and Apply, which sets each through its
// own setter as Settings does, or Discard.
export function SettingsChange(props: RowProps<SettingsPart>) {
  const { checked } = props.p

  return (
    <ChangeRow
      {...props}
      title="Autopilot’s settings"
      kind="Settings"
      items={checked.changes.map((c) => (
        <li key={c.key} className="text-sm text-text">
          {c.label} <span className="text-muted">{c.before}</span> → <span className="font-medium">{c.after}</span>
        </li>
      ))}
      apply={() => applySettings(checked)}
    />
  )
}
