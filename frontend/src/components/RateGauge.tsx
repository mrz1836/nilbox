import { formatAge, formatUntil } from '../lib/age'
import { budgets, levelFill, levelLabels, levelText, readBudget, summarize, type Budget } from '../lib/budget'
import { formatCount } from '../lib/format'
import type { Rate, RateGauge as Gauge } from '../platform'

export interface RateGaugeProps {
  // gauge is null until the inbox has loaded.
  gauge: Gauge | null
  now: Date
  // titleId is the id for the heading, which names the panel around it.
  titleId: string
}

const unread: Rate = { known: false, limit: 0, remaining: 0, used: 0, cost: 0, resetAt: '', takenAt: '' }

// amountText reads a budget as "4,980 of 5,000 left", or says it is unknown.
function amountText(budget: Budget): string {
  return budget.known ? `${formatCount(budget.remaining)} of ${formatCount(budget.limit)} left` : 'not read yet'
}

// resetText says when a budget refills: "Resets in 42m", or that it already has.
function resetText(rate: Rate, budget: Budget, now: Date): string {
  if (!budget.known) {
    return 'Not read yet'
  }

  if (budget.refilled) {
    return 'Refilled since the last refresh'
  }

  const until = formatUntil(now, rate.resetAt)

  if (until === '') {
    return ''
  }

  return until === 'now' ? 'Resets within a minute' : `Resets in ${until}`
}

// costText is the inbox query's cost, e.g. "last refresh cost 1 point".
function costText(cost: number): string {
  return `last refresh cost ${formatCount(cost)} ${cost === 1 ? 'point' : 'points'}`
}

// readText says how fresh the readings are; the three are read in the same refresh.
function readText(gauge: Gauge | null, now: Date): string {
  const age = gauge === null ? '' : formatAge(now, gauge.graphql.takenAt)

  if (age === '') {
    return 'Read on every inbox refresh'
  }

  return age === 'just now' ? 'Read just now' : `Read ${age} ago`
}

interface BudgetRowProps {
  label: string
  rate: Rate
  now: Date
  // showCost adds the inbox query's cost (GraphQL only).
  showCost: boolean
}

function BudgetRow({ label, rate, now, showCost }: BudgetRowProps) {
  const budget = readBudget(rate, now)
  const details = [resetText(rate, budget, now), showCost && budget.known ? costText(rate.cost) : '']
    .filter((part) => part !== '')
    .join(' · ')

  return (
    <li>
      <div className="flex items-baseline justify-between gap-4 text-sm">
        <span className="font-medium">{label}</span>
        <span className="whitespace-nowrap tabular-nums">
          {budget.known ? (
            <>
              {formatCount(budget.remaining)}
              <span className="text-muted"> of {formatCount(budget.limit)}</span>
            </>
          ) : (
            <span className="text-muted">—</span>
          )}
        </span>
      </div>
      <div
        role="meter"
        aria-label={`${label} budget remaining`}
        aria-valuemin={0}
        aria-valuemax={budget.limit}
        aria-valuenow={budget.remaining}
        aria-valuetext={amountText(budget)}
        className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-raised"
      >
        <div
          className={`h-full rounded-full transition-[width] duration-300 ${levelFill[budget.level]}`}
          style={{ width: `${String(Math.round(budget.percent))}%` }}
        />
      </div>
      {details !== '' && <p className="mt-1 text-xs whitespace-nowrap text-muted">{details}</p>}
    </li>
  )
}

// RateGauge details the GitHub API budgets behind the toolbar gauge: an overall level set
// by the budget with the least left, then each budget's meter, what is left, and when it
// refills. The inbox spends GraphQL; REST and search are spent by everything else that
// uses the same account, such as gh and the agents.
export function RateGauge({ gauge, now, titleId }: RateGaugeProps) {
  const summary = summarize(gauge, now)

  return (
    <div className="flex flex-col">
      <div className="flex items-center justify-between gap-4 px-4 pt-3.5">
        <h2 id={titleId} className="text-sm font-semibold">
          API budget
        </h2>
        <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${levelText[summary.level]}`}>
          <span aria-hidden="true" className={`size-1.5 rounded-full ${levelFill[summary.level]}`} />
          {levelLabels[summary.level]}
        </span>
      </div>
      <p className="px-4 pt-0.5 text-xs text-muted">{readText(gauge, now)}</p>
      <ul className="flex flex-col gap-3.5 px-4 py-3.5">
        {budgets.map(({ key, label }) => (
          <BudgetRow key={key} label={label} rate={gauge?.[key] ?? unread} now={now} showCost={key === 'graphql'} />
        ))}
      </ul>
      <p className="border-t border-border px-4 py-2.5 text-xs text-muted">
        Shared with gh, your agents, and anything else signed in as you.
      </p>
    </div>
  )
}
