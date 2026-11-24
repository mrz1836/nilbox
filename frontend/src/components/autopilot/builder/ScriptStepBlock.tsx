import { useId } from 'react'

import { useQuery } from '../../../lib/useQuery'
import { autopilotMyScripts, type AutopilotMyScript } from '../../../platform'
import { fieldClass } from '../../controls'

// ScriptStep is a step's Run one of my scripts: the script, by its UUID, with its ID and the
// name it was picked under, which find it where no script has the UUID.
export interface ScriptStep {
  uuid?: string
  id: number
  name: string
}

export interface ScriptStepBlockProps {
  // n is the step's number, which its control's label names.
  n: string
  step: ScriptStep
  onChange: (step: ScriptStep) => void
}

// ScriptStepBlock is Run one of my scripts' settings, on its step's line: which of the
// user's own Repo scripts it runs, in the item's clone, in the app. A script that asks
// before each run waits for the user's OK every time, which the line says.
export function ScriptStepBlock({ n, step, onChange }: ScriptStepBlockProps) {
  const id = useId()
  const mine = useQuery(() => autopilotMyScripts(), [])
  const scripts = mine.data ?? []
  // The script with the step's UUID is the step's, whatever it is called now; a step without
  // one, or whose UUID no script has, finds its script by ID.
  const byUUID = step.uuid !== undefined && step.uuid !== '' ? scripts.find((s) => s.uuid === step.uuid) : undefined
  const chosen = byUUID ?? scripts.find((s) => s.id === step.id)
  // A script the routine names that isn't one of the user's any more stays listed, so the
  // problem with it shows where it is; so does one found by its ID alone that was saved
  // under another name since it was picked (an ID from another Mac may name another script
  // here), until it is picked again.
  const gone = step.id > 0 && chosen === undefined && mine.data !== undefined
  const renamed = byUUID === undefined && chosen !== undefined && chosen.name !== step.name

  if (mine.data !== undefined && scripts.length === 0 && step.id === 0) {
    return (
      <span className="text-xs text-muted">
        You have no scripts of your own yet: add one in Scripts → Repo scripts, then pick it here.
      </span>
    )
  }

  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-muted">
      <label htmlFor={`${id}-script`} className="sr-only">
        {`Step ${n}: the script it runs`}
      </label>
      <select
        id={`${id}-script`}
        value={renamed ? 'renamed' : String(chosen?.id ?? step.id)}
        onChange={(e) => {
          const picked = scripts.find((s) => String(s.id) === e.target.value)

          if (picked !== undefined) {
            onChange({ uuid: picked.uuid, id: picked.id, name: picked.name })
          }
        }}
        className={`${fieldClass} py-0.5 text-xs`}
      >
        {step.id === 0 && <option value={0}>Pick one of your scripts…</option>}
        {gone && <option value={step.id}>{`${step.name} (no longer one of your scripts)`}</option>}
        {renamed && <option value="renamed">{`${step.name} (now “${chosen.name}”: pick it again)`}</option>}
        {scripts.map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      {chosen !== undefined && !renamed && <ScriptLine script={chosen} />}
      {mine.error !== '' && <span className="text-danger">{mine.error}</span>}
    </span>
  )
}

// ScriptLine says what the chosen script runs, where it shows, and whether it asks first.
function ScriptLine({ script }: { script: AutopilotMyScript }) {
  return (
    <>
      <code className="max-w-64 truncate font-mono text-caption text-text" title={script.command}>
        {script.command}
      </code>
      <span>in the item’s clone, where it shows in {script.shows}</span>
      {script.asks && <span className="text-warning">· asks before each run: it waits for your OK every time</span>}
    </>
  )
}
