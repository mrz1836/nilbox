import { useState } from 'react'

import { useMutation } from '../../../lib/useMutation'
import { runOnMatches, type AutopilotDraft, type AutopilotMode, type AutopilotTestResult } from '../../../platform'
import { ConfirmDialog } from '../../ConfirmDialog'
import { buttonClass } from '../../controls'

export interface RunOnMatchesProps {
  // result is the last test, whose items the routine would run on now are offered.
  result: AutopilotTestResult | undefined
  // draft is the routine as edited; saved reports it is the routine as saved, which alone
  // runs, and stale a test of an earlier draft.
  draft: AutopilotDraft
  saved: boolean
  stale: boolean
  // mode is the routine's mode: in Dry run, each records what it would do.
  mode: AutopilotMode
}

// maxListed caps the items the question names, the rest counted.
const maxListed = 12

// RunOnMatches is Test's Run on the matches now…: once the user confirms the list the test
// found (each item it matches now and would run on), the routine runs on them now, each
// with the user's OK, so a routine that asks first doesn't ask again; every limit still
// holds each. It is offered for a routine as saved alone, tested as it is.
export function RunOnMatches({ result, draft, saved, stale, mode }: RunOnMatchesProps) {
  const [asking, setAsking] = useState(false)
  const [done, setDone] = useState('')
  const run = useMutation((id: number, items: number[]) => runOnMatches(id, items))
  const items = result?.items.filter((it) => it.runsNow) ?? []
  const routineID = draft.id ?? 0

  if (!saved || stale || routineID === 0 || items.length === 0) {
    return null
  }

  const n = items.length
  const noun = n === 1 ? 'item' : 'items'
  const confirm = async () => {
    const ran = await run.run(
      routineID,
      items.map((it) => it.id),
    )

    if (ran.ok) {
      setAsking(false)
      setDone(ran.data.words)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <button
        type="button"
        onClick={() => {
          setDone('')
          run.reset()
          setAsking(true)
        }}
        className={buttonClass}
      >
        Run on the matches now…
      </button>
      {done !== '' && (
        <p role="status" className="text-xs text-muted">
          {done}
        </p>
      )}
      {asking && (
        <ConfirmDialog
          title={`Run ${draft.name} on ${String(n)} ${noun} now?`}
          confirmLabel={run.pending ? 'Starting…' : `Run on ${String(n)}`}
          tone="primary"
          busy={run.pending}
          size="wide"
          onConfirm={() => {
            void confirm()
          }}
          onCancel={() => {
            setAsking(false)
          }}
        >
          <p className="text-sm">
            {mode === 'dryRun'
              ? 'In Dry run, it records what it would do on each, and does nothing.'
              : 'It runs on each with your OK, so it doesn’t ask first; Pause, its limits, and this Mac’s readiness still hold each, and a script that asks before each run still asks.'}
          </p>
          <ul aria-label="What it runs on" className="mt-2 flex max-h-64 flex-col gap-0.5 overflow-y-auto">
            {items.slice(0, maxListed).map((it) => (
              <li key={it.id} className="flex min-w-0 items-baseline gap-2 text-sm">
                <span className="shrink-0 font-mono text-small text-muted">
                  {it.repo} #{String(it.number)}
                </span>
                <span className="min-w-0 truncate" title={it.title}>
                  {it.title}
                </span>
              </li>
            ))}
            {n > maxListed && <li className="text-xs text-muted">and {String(n - maxListed)} more</li>}
          </ul>
          {run.error !== '' && (
            <p role="alert" className="mt-2 text-xs text-danger">
              {run.error}
            </p>
          )}
        </ConfirmDialog>
      )}
    </div>
  )
}
