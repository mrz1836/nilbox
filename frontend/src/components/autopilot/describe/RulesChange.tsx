import { applyRules, type RulesPart } from '../../../lib/describeit'
import { ChangeRow, type RowProps } from './parts'

// RulesChange is a card's change to the rules routines lean on: each change, as "Trust
// renovate[bot]" or "Mute acme-org/legacy", its problems and risks, and Apply, which sets
// the trusted bots, each repository's or org's rule, and the ignored workflows as Settings
// does, or Discard.
export function RulesChange(props: RowProps<RulesPart>) {
  const { checked } = props.p

  return (
    <ChangeRow
      {...props}
      title="The rules routines lean on"
      kind="Rules"
      items={checked.changes.map((c) => (
        <li key={`${c.kind}:${c.target}`} className="text-sm text-text">
          {c.words}
        </li>
      ))}
      apply={() => applyRules(checked)}
    />
  )
}
