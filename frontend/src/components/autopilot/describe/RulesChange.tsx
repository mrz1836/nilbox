import { useId } from 'react'

import { applyRules, type RulesPart } from '../../../lib/describeit'
import { OutcomeLine, Problems, Risks, RowActions, rowClass, RowTitle, useAct, type RowProps } from './parts'

// RulesChange is a card's change to the rules routines lean on: each change, as "Trust
// renovate[bot]" or "Mute acme-org/legacy", its problems and risks, and Apply, which sets
// the trusted bots, each repository's or org's rule, and the ignored workflows as Settings
// does, or Discard.
export function RulesChange({ p, outcome, held, onSettle, kit }: RowProps<RulesPart>) {
  const id = useId()
  const act = useAct(p, onSettle)

  return (
    <div role="group" aria-labelledby={`${id}-title`} className={rowClass}>
      <RowTitle id={`${id}-title`} title="The rules routines lean on" kind="Rules" />
      <ul aria-label="What it changes" className="flex flex-col gap-0.5">
        {p.checked.changes.map((c) => (
          <li key={`${c.kind}:${c.target}`} className="text-sm text-text">
            {c.words}
          </li>
        ))}
      </ul>
      <Problems problems={p.checked.problems} />
      <Risks risks={p.checked.risks} />
      {outcome !== undefined ? (
        <OutcomeLine part="rules" outcome={outcome} onShowRoutine={kit.onShowRoutine} />
      ) : (
        <RowActions
          label="Apply"
          blocked={p.checked.problems.length > 0}
          busy={held || act.busy}
          error={act.error}
          onApply={() => {
            void act.act('applied', async () => {
              const done = await applyRules(p.checked)

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
