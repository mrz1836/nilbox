import { useState, type ReactNode } from 'react'

import { noTicks, pastRulesAccepted, pastTicks, type PastTicks } from '../../lib/routineBuilder'
import type { AutopilotDraft } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

// The user's confirmation that a routine's Merge it may go past the rules, as the builder's
// Save asks for it and a Describe it card's routine collects it on its row: what each tick
// lets the routine do, one box for each, which the backend saves the routine with only once
// every one is ticked (acceptPastRules).

// pastTickOrder are the ticks in the order they are confirmed.
const pastTickOrder: readonly (keyof PastTicks)[] = ['bypass', 'sensitive']

// pastRulesWords say what each tick lets a routine do, as the user confirms it.
const pastRulesWords: Readonly<Record<keyof PastTicks, string>> = {
  bypass: 'Merge your own pull request past the rules when an approval from someone else is all it lacks.',
  sensitive: 'Do so even when it changes workflow, agent, or tooling files.',
}

// pastRulesTerms say when a routine the user let merge past the rules does so.
const pastRulesTerms =
  'Only on your own pull request whose every commit you signed, on the commit whose checks passed, and only while Let routines merge your own pull requests past the rules is on in Autopilot’s settings. Any change to the routine asks you again.'

export interface PastRulesAcceptProps {
  // ticks are the ticks the routine sets, each of which the user confirms.
  ticks: PastTicks
  accepted: PastTicks
  onAccept: (accepted: PastTicks) => void
  disabled?: boolean
}

// PastRulesAccept lists what a routine's Merge it may do past the rules, in the danger tone,
// with the box the user ticks to confirm each, as WarningsToAccept does a merge's warnings.
// It shows nothing for a routine that sets no tick.
export function PastRulesAccept({ ticks, accepted, onAccept, disabled = false }: PastRulesAcceptProps) {
  const shown = pastTickOrder.filter((k) => ticks[k])

  if (shown.length === 0) {
    return null
  }

  return (
    <div className="rounded-md border border-danger/60 p-2 text-danger">
      <p className="mb-1">Once it’s on, this routine may:</p>
      <ul className="flex flex-col gap-1">
        {shown.map((k) => (
          <li key={k}>
            <label className="flex items-start gap-2 text-text">
              <input
                type="checkbox"
                className="checkbox mt-0.5"
                checked={accepted[k]}
                disabled={disabled}
                onChange={(e) => {
                  onAccept({ ...accepted, [k]: e.target.checked })
                }}
              />
              {pastRulesWords[k]}
            </label>
          </li>
        ))}
      </ul>
    </div>
  )
}

interface PastRulesDialogProps {
  draft: AutopilotDraft
  onConfirm: () => void
  onCancel: () => void
}

// PastRulesDialog asks before a routine whose Merge it may go past the rules is saved: what
// it may do, a box for each tick to confirm, and when it does so. Save stays off until every
// box is ticked, and Cancel, the safe choice, saves nothing.
function PastRulesDialog({ draft, onConfirm, onCancel }: PastRulesDialogProps) {
  const [accepted, setAccepted] = useState<PastTicks>(noTicks)
  const ticks = pastTicks(draft.spec)
  const made = (draft.id ?? 0) === 0

  return (
    <ConfirmDialog
      title={`Let “${draft.name.trim()}” merge past the rules?`}
      confirmLabel={made ? 'Create routine' : 'Save'}
      tone="danger"
      size="wide"
      confirmDisabled={!pastRulesAccepted(ticks, accepted)}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <PastRulesAccept ticks={ticks} accepted={accepted} onAccept={setAccepted} />
      <p className="mt-3">{pastRulesTerms}</p>
    </ConfirmDialog>
  )
}

// PastRulesConfirm is the question a save of a routine that may go past the rules asks.
export interface PastRulesConfirm {
  // ask asks the user to confirm what draft's Merge it may do past the rules: go hears the
  // draft with their confirmation once they do, and Cancel or Escape saves nothing.
  ask: (draft: AutopilotDraft, go: (accepted: AutopilotDraft) => void) => void
  // asking reports the question open, and dialog is it, while it is.
  asking: boolean
  dialog: ReactNode
}

// usePastRulesConfirm asks, before a routine whose Merge it may go past the rules is saved,
// that the user confirm each thing it may do, and hands the draft on with their
// confirmation (acceptPastRules) once they do.
export function usePastRulesConfirm(): PastRulesConfirm {
  const [pending, setPending] = useState<{ draft: AutopilotDraft; go: (d: AutopilotDraft) => void } | null>(null)

  return {
    ask: (draft, go) => {
      setPending({ draft, go })
    },
    asking: pending !== null,
    dialog:
      pending === null ? null : (
        <PastRulesDialog
          draft={pending.draft}
          onConfirm={() => {
            setPending(null)
            pending.go({ ...pending.draft, acceptPastRules: true })
          }}
          onCancel={() => {
            setPending(null)
          }}
        />
      ),
  }
}
