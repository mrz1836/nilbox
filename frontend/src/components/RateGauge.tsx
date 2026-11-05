import { formatUntil } from '../lib/age'
import { formatCount } from '../lib/format'
import type { Rate, RateGauge as Gauge } from '../platform'

export interface RateGaugeProps {
  gauge: Gauge
  now: Date
}

// percentLeft is the share of the budget remaining, clamped to 0–100.
function percentLeft(rate: Rate): number {
  if (rate.limit <= 0) {
    return 0
  }

  return Math.min(100, Math.max(0, (rate.remaining / rate.limit) * 100))
}

// budgetText reads a budget as "4,980 of 5,000 left", or says it is unknown.
function budgetText(rate: Rate): string {
  if (!rate.known) {
    return 'not read yet'
  }

  return `${formatCount(rate.remaining)} of ${formatCount(rate.limit)} left`
}

// resetText reads a budget's reset as "resets in 42m", or "" when unknown.
function resetText(rate: Rate, now: Date): string {
  const until = rate.known ? formatUntil(now, rate.resetAt) : ''

  return until === '' ? '' : `resets in ${until}`
}

// RateGauge shows the GitHub API budgets: the GraphQL budget the inbox query spends as a
// meter, with the last query's cost, and the REST core and search budgets beside it.
export function RateGauge({ gauge, now }: RateGaugeProps) {
  const { graphql, core, search } = gauge
  const pct = percentLeft(graphql)
  const low = graphql.known && pct < 10

  return (
    <section
      aria-labelledby="rate-gauge-title"
      className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-3 text-sm"
    >
      <h2 id="rate-gauge-title" className="text-xs font-semibold tracking-wide text-muted uppercase">
        API budget
      </h2>
      <div
        role="meter"
        aria-label="GraphQL budget remaining"
        aria-valuemin={0}
        aria-valuemax={graphql.known ? graphql.limit : 0}
        aria-valuenow={graphql.known ? graphql.remaining : 0}
        aria-valuetext={budgetText(graphql)}
        className="h-2 overflow-hidden rounded-full bg-raised"
      >
        <div className={low ? 'h-full bg-danger' : 'h-full bg-success'} style={{ width: `${String(pct)}%` }} />
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
        <dt className="text-muted">GraphQL</dt>
        <dd>
          {budgetText(graphql)}
          {resetText(graphql, now) !== '' && <span className="text-muted"> · {resetText(graphql, now)}</span>}
        </dd>
        <dt className="text-muted">Last query cost</dt>
        <dd>{graphql.known ? formatCount(graphql.cost) : '—'}</dd>
        <dt className="text-muted">Core</dt>
        <dd>
          {budgetText(core)}
          {resetText(core, now) !== '' && <span className="text-muted"> · {resetText(core, now)}</span>}
        </dd>
        <dt className="text-muted">Search</dt>
        <dd>
          {budgetText(search)}
          {resetText(search, now) !== '' && <span className="text-muted"> · {resetText(search, now)}</span>}
        </dd>
      </dl>
    </section>
  )
}
