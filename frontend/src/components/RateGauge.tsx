import { brand } from '../generated/brand'
import {
  budgets,
  levelFill,
  levelLabels,
  levelText,
  newestRead,
  paceOf,
  readBudget,
  shareOf,
  summarize,
  writesHeld,
  type Budget,
  type Pace,
} from '../lib/budget'
import { counted, formatCount } from '../lib/format'
import { formatAge, formatUntil } from '../lib/time'
import type { BudgetGauge, BudgetReading, BudgetWrites } from '../platform'

export interface RateGaugeProps {
  // gauge is null until the budgets have loaded.
  gauge: BudgetGauge | null
  now: Date
  // titleId is the id for the heading, which names the panel around it.
  titleId: string
}

const unread: BudgetReading = {
  known: false,
  limit: 0,
  remaining: 0,
  used: 0,
  resetAt: '',
  takenAt: '',
  spent: 0,
  requests: 0,
  complete: false,
}

// amountText reads a budget as "4,980 of 5,000 left", or says it is unknown.
function amountText(budget: Budget): string {
  return budget.known ? `${formatCount(budget.remaining)} of ${formatCount(budget.limit)} left` : 'not read yet'
}

// resetText says when a budget refills: "Resets in 42m", or that it already has.
function resetText(rate: BudgetReading, budget: Budget, now: Date): string {
  if (!budget.known) {
    return 'Not read yet'
  }

  if (budget.refilled) {
    return 'Refilled since it was read'
  }

  const until = formatUntil(now, rate.resetAt)

  if (until === '') {
    return ''
  }

  return until === 'now' ? 'Resets within a minute' : `Resets in ${until}`
}

// shareText says who used a budget since it refilled: the app's name and what it spent,
// then everything else's ("<app> 45 · everything else 62"), or what the app spent since it
// opened, when it opened after the refill.
function shareText(rate: BudgetReading, now: Date): string {
  const share = shareOf(rate, now)

  if (share === null) {
    return ''
  }

  if (!share.complete) {
    return `${brand.name} ${formatCount(share.app)} since it opened`
  }

  return `${brand.name} ${formatCount(share.app)} · everything else ${formatCount(share.others)}`
}

// readText says how fresh the readings are: each GitHub response the app gets, and each
// look at this panel, reads them again.
function readText(gauge: BudgetGauge | null, now: Date): string {
  const age = formatAge(now, newestRead(gauge))

  if (age === '') {
    return 'Read with every GitHub request'
  }

  return age === 'just now' ? 'Live · read just now' : `Read ${age} ago`
}

// percentText reads a small share: "<1%" below one percent, else whole percents.
function percentText(percent: number): string {
  return percent > 0 && percent < 1 ? '<1%' : `${String(Math.round(percent))}%`
}

interface BudgetRowProps {
  label: string
  rate: BudgetReading
  now: Date
}

function BudgetRow({ label, rate, now }: BudgetRowProps) {
  const budget = readBudget(rate, now)
  const reset = resetText(rate, budget, now)
  const share = shareText(rate, now)

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
      {reset !== '' && <p className="mt-1 text-xs whitespace-nowrap text-muted">{reset}</p>}
      {share !== '' && (
        <p
          className="mt-0.5 text-xs whitespace-nowrap text-muted tabular-nums"
          title={`Used since it last refilled: by ${brand.name}, and by everything else signed in as you`}
        >
          Used: {share}
        </p>
      )}
    </li>
  )
}

// PaceRow says what the inbox's own refreshes spend: what the last cost for the items it
// tracks, and what that comes to in an hour of refreshes.
function PaceRow({ pace, perHour }: { pace: Pace | null; perHour: number }) {
  return (
    <div className="border-t border-border px-4 py-3">
      <div className="flex items-baseline justify-between gap-4 text-sm">
        <span className="font-medium">Inbox refresh</span>
        {pace !== null && (
          <span className="whitespace-nowrap tabular-nums">
            {counted(pace.points, 'point', 'points')}
            <span className="text-muted"> · {counted(pace.items, 'item', 'items')}</span>
          </span>
        )}
      </div>
      <p className="mt-1 text-xs text-muted">
        {pace === null
          ? 'Its cost shows after the first refresh.'
          : `${String(perHour)} refreshes an hour: about ${formatCount(pace.perHour)} GraphQL points (${percentText(pace.percent)} of the budget). More items cost more.`}
      </p>
    </div>
  )
}

// WritesRow says what the app changed on GitHub for the user in the last hour, such as
// taking them off an item's assignees, and how it paces those changes: what each spends,
// the most it makes, and the reserve it leaves for everything else signed in as the user.
// While GitHub has asked for a pause, it says until when.
function WritesRow({ writes, now }: { writes: BudgetWrites; now: Date }) {
  const held = writesHeld(writes, now)
  const until = formatUntil(now, writes.heldUntil)

  return (
    <div className="border-t border-border px-4 py-3">
      <div className="flex items-baseline justify-between gap-4 text-sm">
        <span className="font-medium">Changes on GitHub</span>
        <span className="whitespace-nowrap tabular-nums">
          {formatCount(writes.lastHour)}
          <span className="text-muted"> in the last hour</span>
        </span>
      </div>
      <p className="mt-1 text-xs text-muted">
        {`Each spends 1 REST point. ${brand.name} makes at most ${String(writes.perMinute)} a minute and ${String(writes.perHour)} an hour, and none while REST is down to its last ${formatCount(writes.reserve)}.`}
      </p>
      {held && (
        <p className="mt-1 text-xs font-medium text-warning">
          {until === 'now' || until === ''
            ? 'GitHub asked for a pause: changes resume shortly'
            : `GitHub asked for a pause: changes resume in ${until}`}
        </p>
      )}
    </div>
  )
}

// RateGauge details the GitHub API budgets behind the toolbar gauge: an overall level set
// by the budget with the least left, then each budget's meter, what is left, when it
// refills, and who used it, then what the inbox's own refreshes cost for the items they
// track, and the changes the app made on GitHub. Every GitHub response the app gets
// updates it.
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
          <BudgetRow key={key} label={label} rate={gauge?.[key] ?? unread} now={now} />
        ))}
      </ul>
      {gauge !== null && <PaceRow pace={paceOf(gauge)} perHour={gauge.refreshesPerHour} />}
      {gauge !== null && <WritesRow writes={gauge.writes} now={now} />}
      <p className="border-t border-border px-4 py-2.5 text-xs text-muted">
        Shared with gh, your agents, and anything else signed in as you.
      </p>
    </div>
  )
}
