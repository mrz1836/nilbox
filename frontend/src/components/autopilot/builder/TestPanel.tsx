import { useState } from 'react'

import { testedLabel, testHeadline } from '../../../lib/routineBuilder'
import type { AutopilotMode, AutopilotTestItem, AutopilotTestResult, ItemKind } from '../../../platform'
import { buttonClass, captionClass, quietButtonClass } from '../../controls'
import { CheckIcon, ChevronDownIcon, ChevronRightIcon, RefreshIcon, XIcon } from '../../icons'
import { Spinner } from '../../Spinner'

export interface TestPanelProps {
  // result is the last test of the draft, undefined before one answered.
  result: AutopilotTestResult | undefined
  // testing reports a test under way, and error why the last one failed.
  testing: boolean
  error: string
  // ready reports a draft that can be tried, with no problem; why says what to fix first.
  ready: boolean
  // kinds are the kinds of item the routine acts on, which its headline names.
  kinds: readonly ItemKind[]
  // mode is the routine's mode: in Dry run, a match is what it would do.
  mode: AutopilotMode
  onTest: () => void
}

// needWords say what a read would have to bring for an unknown to decide.
const needWords: Readonly<Record<string, string>> = {
  details: 'its details',
  checks: 'the check names',
  files: 'its changed files',
}

// rowState says where an item stands with the routine: what it would do, what its history
// holds of it, what it waits to know, or that it doesn't match.
function rowState(it: AutopilotTestItem, mode: AutopilotMode): string {
  if (it.leftAlone) {
    return 'left alone: you took back what it did'
  }

  if (it.match === 'yes') {
    return it.record !== '' ? it.record : mode === 'dryRun' ? 'would run, in Dry run' : 'would run'
  }

  if (it.match === 'unknown') {
    const needs = it.needs.map((n) => needWords[n] ?? n).filter((w) => w !== '')

    return needs.length > 0 ? `waiting for data: ${needs.join(', ')}` : 'waiting for data'
  }

  return 'doesn’t match'
}

// TestPanel is the draft tried on its items now, before it is saved: "3 of 7 open pull
// requests match now", each item with whether it matches, where it stands with the routine,
// and Why ▸, every step of its decision with the value it compared; and ↻ Test, which says
// what the last test spent.
export function TestPanel({ result, testing, error, ready, kinds, mode, onTest }: TestPanelProps) {
  return (
    <section aria-label="Test" className="flex flex-col gap-2 border-t border-border py-3">
      <div className="flex flex-wrap items-center gap-3">
        <h3 className={captionClass}>Test</h3>
        <p role="status" className="min-w-0 flex-1 text-sm">
          {!ready
            ? 'Fix the problems above to try it on your items.'
            : result === undefined
              ? testing
                ? 'Trying it on your items…'
                : 'Not tried yet.'
              : testHeadline(result, kinds)}
        </p>
        {testing && <Spinner />}
        <button
          type="button"
          disabled={!ready || testing}
          title={
            result?.cached === true
              ? 'Its searches were read within the minute, so they cost nothing again.'
              : undefined
          }
          onClick={onTest}
          className={buttonClass}
        >
          <RefreshIcon className="size-3.5 text-muted" />
          {testedLabel(result)}
        </button>
      </div>
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      {result?.lowBudget === true && (
        <p className="text-xs text-warning">
          Your GitHub API budget is low, so this test read nothing new; it tried what is stored.
        </p>
      )}
      {result !== undefined && result.items.length > 0 && (
        <ul aria-label="Items it watches" className="flex flex-col">
          {result.items.map((it) => (
            <TestRow key={it.id} item={it} mode={mode} />
          ))}
        </ul>
      )}
    </section>
  )
}

// TestRow is one item a test tried: ✓ or ✗, the item, where it stands, and Why ▸.
function TestRow({ item: it, mode }: { item: AutopilotTestItem; mode: AutopilotMode }) {
  const [open, setOpen] = useState(false)
  const name = `${it.repo} #${String(it.number)}`

  return (
    <li className="flex flex-col border-b border-border/60 py-1 last:border-b-0">
      <div className="flex items-center gap-2 text-sm">
        <ResultMark result={it.match} />
        <span className="w-44 shrink-0 truncate font-mono text-[0.75rem] text-muted" title={name}>
          {name}
        </span>
        <span className="min-w-0 flex-1 truncate" title={it.title}>
          {it.title}
        </span>
        <span className={`shrink-0 text-xs ${it.match === 'yes' ? 'text-text' : 'text-muted'}`}>
          {rowState(it, mode)}
        </span>
        <button
          type="button"
          aria-expanded={open}
          aria-label={`Why, for ${name}`}
          onClick={() => {
            setOpen(!open)
          }}
          className={quietButtonClass}
        >
          {open ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
          Why
        </button>
      </div>
      {open && (
        <ul aria-label={`Why, for ${name}`} className="ml-6 flex flex-col gap-0.5 py-1">
          {it.trace.map((t) => (
            <li key={t.path} className="flex items-start gap-1.5 text-xs">
              <ResultMark result={t.result} small />
              <span className="text-text">{t.condition}</span>
              {t.actual !== '' && <span className="text-muted">{`· ${t.actual}`}</span>}
            </li>
          ))}
          {it.trace.length === 0 && <li className="text-xs text-muted">Nothing was checked.</li>}
        </ul>
      )}
    </li>
  )
}

// ResultMark marks a decision: ✓ yes, ✗ no, ? not known yet, each named for assistive tech.
function ResultMark({ result, small = false }: { result: string; small?: boolean }) {
  const size = small ? 'size-3' : 'size-3.5'

  if (result === 'yes') {
    return (
      <span className="inline-flex shrink-0 text-success" title="Matches">
        <CheckIcon className={size} />
        <span className="sr-only">Matches:</span>
      </span>
    )
  }

  if (result === 'no') {
    return (
      <span className="inline-flex shrink-0 text-danger" title="Doesn’t match">
        <XIcon className={size} />
        <span className="sr-only">Doesn’t match:</span>
      </span>
    )
  }

  return (
    <span className="inline-flex shrink-0 text-warning" title="Not known yet">
      <span aria-hidden="true" className="text-xs leading-none font-semibold">
        ?
      </span>
      <span className="sr-only">Not known yet:</span>
    </span>
  )
}
