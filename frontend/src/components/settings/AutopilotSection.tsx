import { useEffect, useEffectEvent, useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { counted, formatCount } from '../../lib/format'
import { dayHeading, formatDateTime, formatFull, formatWhen } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import { useNowAs } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  AutopilotMode,
  clearAutopilotHistory,
  getAutopilotEngine,
  on,
  setAutopilotActionsPerDay,
  setAutopilotAgentsAtOnce,
  setAutopilotCatchUpHours,
  setAutopilotDailyCap,
  setAutopilotDefaultMode,
  setAutopilotHistoryDays,
  setAutopilotHoldOnOutage,
  setAutopilotKeepAwake,
  setAutopilotLogDays,
  setAutopilotPaused,
  setAutopilotRunsPerDay,
  type AutopilotConfig,
  type AutopilotDailyCap,
  type AutopilotEngine,
  type AutopilotUnit,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, fieldClass, Segmented, type SegmentOption } from '../controls'
import { rowMatchClass, useSetting } from './match'
import { ChoiceRow, Group, Note, rowDescribedBy, SectionIntro, SettingRow, SwitchRow } from './rows'
import type { SectionProps } from './types'

// limits are the ranges the settings' setters accept, as internal/autopilotprefs declares
// them (Min… and Max…); a value outside one is refused there.
const limits = {
  agentsAtOnce: { min: 1, max: 3 },
  runsPerDay: { min: 1, max: 200 },
  actionsPerDay: { min: 1, max: 200 },
  catchUpHours: { min: 1, max: 168 },
  historyDays: { min: 7, max: 365 },
  logDays: { min: 1, max: 90 },
  usd: { min: 1, max: 500 },
  tokens: { min: 100_000, max: 50_000_000 },
} as const

// defaults are a fresh database's settings (autopilotprefs.Defaults), which each choice
// marks, and the amount a daily cap starts at as it is turned on in a unit.
const defaults = {
  agentsAtOnce: 2,
  runsPerDay: 20,
  actionsPerDay: 50,
  catchUpHours: 24,
  historyDays: 90,
  logDays: 30,
  cap: { usd: 10, tokens: 1_000_000 },
} as const

// The choices each select offers. A stored value that isn't among them, as one a backup
// brought, is offered too (withValue).
const agentChoices = [1, 2, 3]
const runChoices = [5, 10, 20, 50, 100, 200]
const actionChoices = [10, 25, 50, 100, 200]
const catchUpChoices = [6, 12, 24, 48, 72, 168]
const historyChoices = [7, 14, 30, 60, 90, 180, 365]
const logChoices = [1, 7, 14, 30, 60, 90]

// modeOptions are the modes a new routine can start in, from the most careful.
const modeOptions: readonly SegmentOption<AutopilotMode>[] = [
  {
    value: AutopilotMode.ModeDryRun,
    label: 'Dry run',
    title: 'Records what a routine would have done, and nothing else',
  },
  {
    value: AutopilotMode.ModeAsk,
    label: 'Ask first',
    title: 'The default: local steps run at once, and the rest wait for your OK',
  },
  { value: AutopilotMode.ModeAuto, label: 'Autopilot', title: 'Every step runs at once, within the limits' },
]

// CapChoice is what the daily cap counts, or off.
type CapChoice = 'off' | AutopilotUnit

// Range is the least and the most a setting takes.
interface Range {
  min: number
  max: number
}

// offered is the choices within range, with value among them, in order.
function offered(choices: readonly number[], range: Range, value: number): number[] {
  const within = choices.filter((n) => n >= range.min && n <= range.max)

  return within.includes(value) ? within : [...within, value].sort((a, b) => a - b)
}

// dayWords and hourWords name a choice of days or hours: "90 days", "1 week".
function dayWords(days: number): string {
  return counted(days, 'day', 'days')
}

function hourWords(hours: number): string {
  return hours === 168 ? '1 week' : counted(hours, 'hour', 'hours')
}

// marked adds "(default)" to the default choice's words.
function marked(words: string, value: number, fallback: number): string {
  return value === fallback ? `${words} (default)` : words
}

// clearedText says what Clear history did.
function clearedText(removed: number, kept: number): string {
  if (removed === 0 && kept === 0) {
    return 'There was nothing to clear.'
  }

  const gone = `Removed ${counted(removed, 'match', 'matches')} from the history.`

  if (kept === 0) {
    return gone
  }

  return kept === 1
    ? `${gone} Kept 1 match without its details, since it still keeps a routine from acting twice.`
    : `${gone} Kept ${formatCount(kept)} matches without their details, since they still keep a routine from acting twice.`
}

// checkedAt names when the engine last checked: "today, 11:59 AM", or the date and time
// for an earlier day.
function checkedAt(now: Date, iso: string): string {
  return dayHeading(now, iso) === 'Today' ? formatWhen(now, new Date(iso)) : formatDateTime(iso)
}

// lastCheckedText is the engine's heartbeat, as Settings says it: what its last cycle did
// and when, that it waits for the first refresh, that it hasn't checked yet, or that
// another copy of the app runs it. It is '' until the engine is read.
export function lastCheckedText(now: Date, engine: AutopilotEngine | undefined): string {
  if (engine === undefined) {
    return ''
  }

  if (engine.elsewhere) {
    return `Autopilot is running in another copy of ${brand.name}. This one shows them and starts nothing.`
  }

  const cycle = engine.lastCycle

  if (cycle === null) {
    return 'Not checked yet.'
  }

  if (cycle.waiting === 'refresh') {
    return 'Waiting for the first refresh.'
  }

  return [
    `Last checked ${checkedAt(now, cycle.at)}`,
    `${formatCount(cycle.tookMs)} ms`,
    counted(cycle.matches, 'match', 'matches'),
    counted(cycle.starts, 'start', 'starts'),
  ].join(' · ')
}

interface CapRowProps {
  controlId: string
  cap: AutopilotDailyCap
  error: string
  onChange: (cap: AutopilotDailyCap) => void
}

// CapRow is the daily cap: off, or an amount in dollars or in tokens. Turning it on in a
// unit starts it at a sensible amount; an amount typed is stored once it is in range and
// the field is left, or Return is pressed. Turning it off keeps its unit.
function CapRow({ controlId, cap, error, onChange }: CapRowProps) {
  const choice: CapChoice = cap.amount > 0 ? cap.unit : 'off'
  const range = limits[cap.unit]
  // draft is the amount typed and not yet stored, or null while the field shows the cap.
  const [draft, setDraft] = useState<string | null>(null)
  const shown = draft ?? String(cap.amount)
  const typed = Number(shown)
  const valid = Number.isInteger(typed) && typed >= range.min && typed <= range.max
  const rangeError =
    draft === null || valid
      ? ''
      : cap.unit === 'usd'
        ? `Choose a whole number of dollars from $${formatCount(range.min)} to $${formatCount(range.max)}.`
        : `Choose from ${formatCount(range.min)} to ${formatCount(range.max)} tokens.`
  const amountId = `${controlId}-amount`
  const shownError = rangeError === '' ? error : rangeError
  const describedBy = rowDescribedBy(controlId, shownError)

  const commit = () => {
    if (draft === null) {
      return
    }

    if (valid && typed !== cap.amount) {
      onChange({ amount: typed, unit: cap.unit })
    }

    if (valid) {
      setDraft(null)
    }
  }

  return (
    <SettingRow
      label="Daily cap"
      controlId={controlId}
      description={
        <>
          The most your routines’ coding agents may use in a day, together. Dollars count what an agent reports a run
          cost; tokens count any agent that reports its usage.
        </>
      }
      error={shownError}
    >
      {choice !== 'off' && (
        <input
          id={amountId}
          type="number"
          inputMode="numeric"
          min={range.min}
          max={range.max}
          step={cap.unit === 'usd' ? 1 : 100_000}
          aria-label={cap.unit === 'usd' ? 'Daily cap in dollars' : 'Daily cap in tokens'}
          aria-invalid={rangeError !== '' || undefined}
          aria-describedby={describedBy}
          value={shown}
          onChange={(e) => {
            setDraft(e.target.value)
          }}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              commit()
            }
          }}
          className={`${fieldClass} w-32 text-right tabular-nums`}
        />
      )}
      <select
        id={controlId}
        value={choice}
        aria-describedby={describedBy}
        onChange={(e) => {
          const next = e.target.value as CapChoice

          setDraft(null)
          onChange(next === 'off' ? { amount: 0, unit: cap.unit } : { amount: defaults.cap[next], unit: next })
        }}
        className="rounded-md border border-border bg-surface px-2 py-1 text-sm"
      >
        <option value="off">Off (default)</option>
        <option value="usd">In dollars</option>
        <option value="tokens">In tokens</option>
      </select>
    </SettingRow>
  )
}

interface NumberRowProps {
  label: string
  controlId: string
  description: string
  value: number
  // choices are the numbers offered within range, fallback the default among them, and
  // words names each.
  choices: readonly number[]
  range: Range
  fallback: number
  words: (n: number) => string
  error: string
  // over disables the choices above it, as the run logs never outlast the history.
  over?: number
  onChange: (n: number) => void
}

// NumberRow is a setting chosen from a few numbers, the default one marked.
function NumberRow({
  label,
  controlId,
  description,
  value,
  choices,
  range,
  fallback,
  words,
  error,
  over,
  onChange,
}: NumberRowProps) {
  return (
    <SettingRow label={label} controlId={controlId} description={description} error={error}>
      <select
        id={controlId}
        value={value}
        aria-describedby={rowDescribedBy(controlId, error)}
        onChange={(e) => {
          onChange(Number(e.target.value))
        }}
        className="rounded-md border border-border bg-surface px-2 py-1 text-sm"
      >
        {offered(choices, range, value).map((n) => (
          <option key={n} value={n} disabled={over !== undefined && n > over}>
            {marked(words(n), n, fallback)}
          </option>
        ))}
      </select>
    </SettingRow>
  )
}

// LastChecked is the engine's heartbeat: when it last checked the routines and what it
// did, kept current as the clock moves on and read again whenever Autopilot changes.
function LastChecked() {
  const [marks] = useSetting('Last checked')
  const engine = useQuery(() => getAutopilotEngine(), [], { log: 'could not read what Autopilot last did' })
  const readAgain = useEffectEvent(() => {
    void engine.refetch()
  })

  useEffect(
    () =>
      on('autopilot:updated', () => {
        readAgain()
      }),
    [],
  )

  const data = engine.data
  const text = useNowAs((now) => lastCheckedText(now, data))
  const at = data?.elsewhere === false ? (data.lastCycle?.at ?? '') : ''

  return (
    <div {...marks} className={`px-4 py-3.5 ${rowMatchClass}`}>
      <p className="text-sm font-medium">Last checked</p>
      <p
        role="status"
        title={at === '' ? undefined : formatFull(at)}
        className="mt-0.5 text-xs text-muted tabular-nums"
      >
        {text}
      </p>
    </div>
  )
}

// ClearHistory clears the history now, rather than after the days it is kept, asking
// first, and says what it removed.
function ClearHistory({ controlId }: { controlId: string }) {
  const [asking, setAsking] = useState(false)
  const clearing = useMutation(() => clearAutopilotHistory())
  const [done, setDone] = useState('')

  const clear = async () => {
    setDone('')

    const result = await clearing.run()

    setAsking(false)

    if (result.ok) {
      setDone(clearedText(result.data.removed, result.data.kept))
    }
  }

  return (
    <ChoiceRow
      label="Clear history"
      description={
        <span id={`${controlId}-help`} role={done === '' ? undefined : 'status'}>
          {done === ''
            ? 'Removes what the routines did with items that are done with now, rather than after the days it is kept.'
            : done}
        </span>
      }
      error={clearing.error}
      errorId={`${controlId}-error`}
    >
      <button
        type="button"
        aria-describedby={rowDescribedBy(controlId, clearing.error)}
        onClick={() => {
          clearing.reset()
          setAsking(true)
        }}
        className={`${buttonClass} shrink-0`}
      >
        Clear history…
      </button>
      {asking && (
        <ConfirmDialog
          title="Clear Autopilot’s history?"
          confirmLabel="Clear history"
          busy={clearing.pending}
          onConfirm={() => {
            void clear()
          }}
          onCancel={() => {
            setAsking(false)
          }}
        >
          This removes what the routines did with items that are done with: merged, closed, or out of every list and
          search. Your routines and what still waits stay, and a record that still keeps a routine from acting twice on
          the same thing stays without its details.
        </ConfirmDialog>
      )}
    </ChoiceRow>
  )
}

// AutopilotSection holds Autopilot's settings, each saved as soon as it is set: Pause
// Autopilot and the mode new routines start in; the limits every routine keeps to, across
// them all; what holds a coding agent back; how long the history and the run logs are
// kept, and Clear history; and when the engine last checked the routines.
export function AutopilotSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const config = settings.autopilot
  // changed is the setting changed last, beside which a refusal shows.
  const [changed, setChanged] = useState<keyof AutopilotConfig>('paused')
  const errorFor = (key: keyof AutopilotConfig) => (changed === key ? error : '')

  // change shows a change to one setting at once and stores it with call. Lowering the
  // history lowers the run logs with it, as the setter does.
  function change<K extends keyof AutopilotConfig>(
    key: K,
    value: AutopilotConfig[K],
    call: () => PromiseLike<Settings>,
  ) {
    setChanged(key)

    const autopilot: AutopilotConfig = { ...config, [key]: value }

    if (key === 'historyDays') {
      autopilot.logDays = Math.min(autopilot.logDays, autopilot.historyDays)
    }

    void save({ autopilot }, call)
  }

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <SwitchRow
          label="Pause Autopilot"
          controlId={`${id}-paused`}
          description="While paused, no routine starts anything new, a snooze or a pin included; what is already under way carries on. The Autopilot tab and the menu bar pause it too."
          checked={config.paused}
          error={errorFor('paused')}
          onChange={(paused) => {
            change('paused', paused, () => setAutopilotPaused(paused))
          }}
        />
        <ChoiceRow
          label="New routines start in"
          description="The mode a routine you make starts in. Ask first, the default, takes local steps at once and waits for your OK before the rest. A routine keeps its own mode once made."
          error={errorFor('defaultMode')}
          errorId={`${id}-mode-error`}
        >
          <Segmented
            label="New routines start in"
            size="sm"
            value={config.defaultMode}
            options={modeOptions}
            describedBy={errorFor('defaultMode') === '' ? undefined : `${id}-mode-error`}
            onChange={(mode) => {
              change('defaultMode', mode, () => setAutopilotDefaultMode(mode))
            }}
          />
        </ChoiceRow>
      </Group>
      <div className="flex flex-col gap-3">
        <SectionIntro title="Limits">
          Across every routine. A limit delays a match and never loses it: one over today’s limit starts tomorrow. A day
          runs from local midnight. Local steps, such as a snooze, keep to none of these.
        </SectionIntro>
        <Group>
          <NumberRow
            label="Agents at once"
            controlId={`${id}-agents`}
            description="How many coding agents your routines may run at the same time. At most three, so a script of yours always has room."
            value={config.agentsAtOnce}
            choices={agentChoices}
            range={limits.agentsAtOnce}
            fallback={defaults.agentsAtOnce}
            words={(n) => counted(n, 'agent', 'agents')}
            error={errorFor('agentsAtOnce')}
            onChange={(n) => {
              change('agentsAtOnce', n, () => setAutopilotAgentsAtOnce(n))
            }}
          />
          <NumberRow
            label="Agent runs a day"
            controlId={`${id}-runs`}
            description="How many coding agent runs your routines may start in a day, together."
            value={config.runsPerDay}
            choices={runChoices}
            range={limits.runsPerDay}
            fallback={defaults.runsPerDay}
            words={(n) => counted(n, 'run', 'runs')}
            error={errorFor('runsPerDay')}
            onChange={(n) => {
              change('runsPerDay', n, () => setAutopilotRunsPerDay(n))
            }}
          />
          <NumberRow
            label="GitHub actions a day"
            controlId={`${id}-actions`}
            description="How many actions on GitHub your routines may send in a day, together, such as asking Dependabot to rebase."
            value={config.actionsPerDay}
            choices={actionChoices}
            range={limits.actionsPerDay}
            fallback={defaults.actionsPerDay}
            words={(n) => counted(n, 'action', 'actions')}
            error={errorFor('actionsPerDay')}
            onChange={(n) => {
              change('actionsPerDay', n, () => setAutopilotActionsPerDay(n))
            }}
          />
          <CapRow
            controlId={`${id}-cap`}
            cap={config.dailyCap}
            error={errorFor('dailyCap')}
            onChange={(dailyCap) => {
              change('dailyCap', dailyCap, () => setAutopilotDailyCap(dailyCap))
            }}
          />
        </Group>
      </div>
      <div className="flex flex-col gap-3">
        <SectionIntro title="While it runs" />
        <Group>
          <SwitchRow
            label="Keep the Mac awake while agents work"
            controlId={`${id}-awake`}
            description="While one of your routines’ coding agents works, the Mac doesn’t go to sleep on its own. The display still can."
            checked={config.keepAwake}
            error={errorFor('keepAwake')}
            onChange={(on) => {
              change('keepAwake', on, () => setAutopilotKeepAwake(on))
            }}
          />
          <SwitchRow
            label="Hold agent starts while their provider reports an outage"
            controlId={`${id}-outage`}
            description="A routine’s coding agent waits to start while its provider’s status page reports a major outage, and starts once it’s back."
            checked={config.holdOnOutage}
            error={errorFor('holdOnOutage')}
            onChange={(on) => {
              change('holdOnOutage', on, () => setAutopilotHoldOnOutage(on))
            }}
          />
          <NumberRow
            label="Ask before catching up after"
            controlId={`${id}-catch-up`}
            description={`When ${brand.name} was closed or asleep this long, a routine’s coding agent and GitHub steps wait for your OK before they catch up on what changed. Local steps never wait.`}
            value={config.catchUpHours}
            choices={catchUpChoices}
            range={limits.catchUpHours}
            fallback={defaults.catchUpHours}
            words={hourWords}
            error={errorFor('catchUpHours')}
            onChange={(hours) => {
              change('catchUpHours', hours, () => setAutopilotCatchUpHours(hours))
            }}
          />
        </Group>
      </div>
      <div className="flex flex-col gap-3">
        <SectionIntro title="History">
          What the routines did, which Activity and each item’s ⚡ chip show. Lowering the history lowers the run logs
          with it.
        </SectionIntro>
        <Group>
          <NumberRow
            label="History kept"
            controlId={`${id}-history`}
            description="How long what the routines did is kept. A record that still keeps a routine from acting twice stays, without its details."
            value={config.historyDays}
            choices={historyChoices}
            range={limits.historyDays}
            fallback={defaults.historyDays}
            words={dayWords}
            error={errorFor('historyDays')}
            onChange={(days) => {
              change('historyDays', days, () => setAutopilotHistoryDays(days))
            }}
          />
          <NumberRow
            label="Run logs kept"
            controlId={`${id}-logs`}
            description="How long a coding agent run’s log is kept, never longer than the history."
            value={config.logDays}
            choices={logChoices}
            range={limits.logDays}
            fallback={defaults.logDays}
            words={dayWords}
            error={errorFor('logDays')}
            over={config.historyDays}
            onChange={(days) => {
              change('logDays', days, () => setAutopilotLogDays(days))
            }}
          />
          <ClearHistory controlId={`${id}-clear`} />
        </Group>
      </div>
      <Group>
        <LastChecked />
      </Group>
      <Note>
        Local steps, such as a snooze or a pin, keep only to Pause Autopilot and a routine’s own active hours; Undo in
        Activity takes them back.
      </Note>
    </div>
  )
}
