import { useId } from 'react'

import { omit, problemsAt } from '../../../lib/routineBuilder'
import type { AutopilotLimits } from '../../../platform'
import { fieldClass } from '../../controls'
import { Block, Notes, NumberField, Reset, type BuilderKit } from './parts'

export interface LimitsBlockProps {
  kit: BuilderKit
  limits: AutopilotLimits
  // oncePer is what the trigger fires once per, which the routine's own Once per takes by
  // default.
  oncePer: string
  onChange: (limits: AutopilotLimits) => void
}

// rangeOf is a limit's bounds and default, from the catalog.
function rangeOf(kit: BuilderKit, id: string, fallback: { min: number; max: number; default: number }) {
  return kit.catalog.limits.find((l) => l.id === id)?.range ?? fallback
}

// LimitsBlock is a routine's own Limits: once per what, how many tries on one item, how
// many runs a day, and the wait between tries; each takes its default until set, and
// Settings → Autopilot's caps still hold over every routine together.
export function LimitsBlock({ kit, limits, oncePer, onChange }: LimitsBlockProps) {
  const id = useId()
  const tries = rangeOf(kit, 'triesPerItem', { min: 1, max: 10, default: 2 })
  const runs = rangeOf(kit, 'runsPerDay', { min: 1, max: 100, default: 10 })
  const wait = rangeOf(kit, 'waitMinutes', { min: 0, max: 1440, default: 30 })
  const choices = kit.catalog.limits.find((l) => l.id === 'oncePer')?.choices ?? []
  const number = (key: 'triesPerItem' | 'runsPerDay' | 'waitMinutes') => (n: number | null) => {
    onChange(n === null ? omit(limits, key) : { ...limits, [key]: n })
  }

  return (
    <Block title="Limits" hint="How often, how much">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
        <span className="inline-flex items-center gap-1.5">
          <label htmlFor={`${id}-once`} className="text-muted">
            Once per
          </label>
          <select
            id={`${id}-once`}
            value={limits.oncePer ?? ''}
            onChange={(e) => {
              const v = e.target.value

              onChange(
                v === 'item' || v === 'headCommit' || v === 'day' ? { ...limits, oncePer: v } : omit(limits, 'oncePer'),
              )
            }}
            className={`${fieldClass} py-1`}
          >
            <option value="">{`${oncePer} (the trigger’s)`}</option>
            {choices.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
          {(limits.oncePer ?? '') !== '' && (
            <Reset
              label="Reset once per"
              onReset={() => {
                onChange(omit(limits, 'oncePer'))
              }}
            />
          )}
        </span>
        <NumberField
          id={`${id}-tries`}
          label="Tries per item"
          value={limits.triesPerItem}
          fallback={tries.default}
          min={tries.min}
          max={tries.max}
          unit="tries per item"
          onChange={number('triesPerItem')}
        />
        <NumberField
          id={`${id}-runs`}
          label="Runs a day"
          value={limits.runsPerDay}
          fallback={runs.default}
          min={runs.min}
          max={runs.max}
          unit="runs a day"
          onChange={number('runsPerDay')}
        />
        <NumberField
          id={`${id}-wait`}
          label="Minutes between tries"
          value={limits.waitMinutes}
          fallback={wait.default}
          min={wait.min}
          max={wait.max}
          unit="min between tries"
          onChange={number('waitMinutes')}
        />
      </div>
      <p className="text-xs text-muted">Settings → Autopilot’s limits still hold over every routine together.</p>
      <Notes problems={problemsAt(kit.problems, 'limits')} />
    </Block>
  )
}
