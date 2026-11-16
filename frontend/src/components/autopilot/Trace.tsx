import type { AutopilotTrace } from '../../platform'
import { CheckIcon, XIcon } from '../icons'

// ResultMark marks a decision: ✓ yes, ✗ no, ? not known yet, each named for assistive tech.
export function ResultMark({ result, small = false }: { result: string; small?: boolean }) {
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

export interface TraceListProps {
  // label names the list for assistive tech, such as "Why, for example-repo #7".
  label: string
  trace: readonly AutopilotTrace[]
  className?: string
}

// TraceList is every step of a routine's decision on an item: each condition as the
// routine's sentence says it, what it came to, and the value it compared.
export function TraceList({ label, trace, className = '' }: TraceListProps) {
  return (
    <ul aria-label={label} className={`flex flex-col gap-0.5 ${className}`.trim()}>
      {trace.map((t) => (
        <li key={t.path} className="flex items-start gap-1.5 text-xs">
          <ResultMark result={t.result} small />
          <span className="text-text">{t.condition}</span>
          {t.actual !== '' && <span className="text-muted">{`· ${t.actual}`}</span>}
        </li>
      ))}
      {trace.length === 0 && <li className="text-xs text-muted">Nothing was checked.</li>}
    </ul>
  )
}
