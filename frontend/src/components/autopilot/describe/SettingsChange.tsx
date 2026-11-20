import { useId } from 'react'

import { applySettings, type SettingsPart } from '../../../lib/describeit'
import { OutcomeLine, Problems, Risks, RowActions, rowClass, RowTitle, useAct, type RowProps } from './parts'

// SettingsChange is a card's change to Autopilot's settings: each setting it changes, as
// "Agents at once 2 → 1", its problems and risks, and Apply, which sets each through its
// own setter as Settings does, or Discard.
export function SettingsChange({ p, outcome, held, onSettle, kit }: RowProps<SettingsPart>) {
  const id = useId()
  const act = useAct(p, onSettle)

  return (
    <div role="group" aria-labelledby={`${id}-title`} className={rowClass}>
      <RowTitle id={`${id}-title`} title="Autopilot’s settings" kind="Settings" />
      <ul aria-label="What it changes" className="flex flex-col gap-0.5">
        {p.checked.changes.map((c) => (
          <li key={c.key} className="text-sm text-text">
            {c.label} <span className="text-muted">{c.before}</span> → <span className="font-medium">{c.after}</span>
          </li>
        ))}
      </ul>
      <Problems problems={p.checked.problems} />
      <Risks risks={p.checked.risks} />
      {outcome !== undefined ? (
        <OutcomeLine part="settings" outcome={outcome} onShowRoutine={kit.onShowRoutine} />
      ) : (
        <RowActions
          label="Apply"
          blocked={p.checked.problems.length > 0}
          busy={held || act.busy}
          error={act.error}
          onApply={() => {
            void act.act('applied', async () => {
              const done = await applySettings(p.checked)

              return done.ok ? { ok: true, data: 0 } : done
            })
          }}
          onDiscard={() => {
            void act.act('discarded')
          }}
        />
      )}
    </div>
  )
}
