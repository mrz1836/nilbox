import { useId } from 'react'

import {
  addable,
  agentless,
  moved,
  newStep,
  omit,
  onlyIfChoices,
  outcomeLabel,
  problemsAt,
  problemsExactly,
  settingRange,
  stepKindOf,
} from '../../../lib/routineBuilder'
import type {
  AutopilotMergeStep,
  AutopilotOutcome,
  AutopilotProblem,
  AutopilotSpec,
  AutopilotStep,
  ItemKind,
} from '../../../platform'
import { dangerIconButtonClass, fieldClass, quietButtonClass, Select, type SelectOption } from '../../controls'
import { PlusIcon, TrashIcon } from '../../icons'
import { MenuButton, type MenuItem } from '../../Menu'
import { AgentStepBlock, type AgentKit } from './AgentStepBlock'
import { GitHubStepBlock } from './GitHubStepBlock'
import { Block, Notes, NumberField, type BuilderKit } from './parts'
import { ScriptStepBlock } from './ScriptStepBlock'

export interface ThenBlockProps {
  kit: BuilderKit
  agentKit: AgentKit
  spec: AutopilotSpec
  kinds: readonly ItemKind[]
  onChange: (steps: AutopilotStep[]) => void
}

// ThenBlock is a routine's Then: its steps in the order they run, numbered, each with its
// own settings, its Only if on a step before it, ↑ ↓ to move it, and 🗑; then + Add a step,
// which offers only what fits at the end.
export function ThenBlock({ kit, agentKit, spec, kinds, onChange }: ThenBlockProps) {
  const steps = spec.steps
  const set = (i: number, st: AutopilotStep) => {
    onChange(steps.map((s, j) => (j === i ? st : s)))
  }
  const choices = addable(kit.catalog, spec)
  const items: MenuItem[] = choices.map((c) => ({
    label: c.label,
    hint: c.offered ? undefined : 'Dry run for now',
    title: c.help,
    onSelect: () => {
      onChange([...steps, newStep(c.kind, steps, kit.catalog.places, spec.trigger.event)])
    },
  }))

  return (
    <Block title="Then" hint="What it does, in order">
      {steps.length === 0 && (
        <p className="text-xs text-muted">No steps yet: add one, so the routine does something.</p>
      )}
      <ol className="flex flex-col gap-2">
        {steps.map((st, i) => (
          // Steps have no identity of their own: their place is what they are.
          <StepRow
            key={i}
            kit={kit}
            agentKit={agentKit}
            steps={steps}
            index={i}
            kinds={kinds}
            step={st}
            onChange={(next) => {
              set(i, next)
            }}
            onMove={(to) => {
              onChange(moved(steps, i, to))
            }}
            onRemove={() => {
              onChange(steps.filter((_, j) => j !== i))
            }}
          />
        ))}
      </ol>
      <Notes problems={problemsExactly(kit.problems, 'steps', '')} />
      {items.length > 0 && (
        <MenuButton label="Add a step" items={items} className={`${quietButtonClass} self-start`}>
          <PlusIcon className="size-3.5" />
          Add a step
        </MenuButton>
      )}
    </Block>
  )
}

interface StepRowProps {
  kit: BuilderKit
  agentKit: AgentKit
  steps: readonly AutopilotStep[]
  index: number
  kinds: readonly ItemKind[]
  step: AutopilotStep
  onChange: (step: AutopilotStep) => void
  onMove: (to: number) => void
  onRemove: () => void
}

// StepRow is one step: its number and kind, its Only if, its settings, and its controls.
function StepRow({ kit, agentKit, steps, index, kinds, step, onChange, onMove, onRemove }: StepRowProps) {
  const id = useId()
  const kind = stepKindOf(step)
  const info = kit.catalog.steps.find((s) => s.kind === kind)
  const path = `[${String(index)}]`
  const n = String(index + 1)
  const label = info?.label ?? 'A step this version doesn’t know'
  const onlyIf = step.onlyIf ?? ''
  // The outcomes it may run only if, and the one it names when that no longer fits, so the
  // problem with it shows where it is.
  const only: AutopilotOutcome[] = onlyIfChoices(kit.catalog, steps, index).map((o) => o.id)
  const shownOnly = onlyIf !== '' && !only.includes(onlyIf) ? [...only, onlyIf] : only
  // Every kind of step but a merge after an agent step may run always, whatever came
  // before: a merge with no agent step merges what the routine's checks passed on.
  const always: SelectOption<''>[] = kind === 'merge' && !agentless(steps) ? [] : [{ value: '', label: 'always' }]
  // The agent step's block, and the GitHub step's, show what is wrong inside them, and
  // Merge it's ticks the warning that the switch for going past the rules is off.
  const inside = [`${path}.agent`, `${path}.github`]
  const pastAt = `${path}.merge.allowBypass`
  const own = (list: readonly AutopilotProblem[]) =>
    problemsAt(list, 'steps', path).filter(
      (p) => !inside.some((at) => p.field === at || p.field.startsWith(`${at}.`) || p.field.startsWith(`${at}[`)),
    )
  const merge = step.merge ?? null
  // Merge it's ticks show for one with no agent step, and for any that sets one, so a tick
  // that no longer fits can be cleared where its problem shows.
  const ticks = merge !== null && (agentless(steps) || merge.allowBypass === true || merge.allowSensitive === true)
  const problems = own(kit.problems)
  const warnings = own(kit.warnings).filter((w) => !ticks || w.field !== pastAt)
  const pastWarnings = ticks ? kit.warnings.filter((w) => w.part === 'steps' && w.field === pastAt) : []

  return (
    <li className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="w-4 text-right text-xs font-semibold text-muted tabular-nums">{n}</span>
        <span className="text-sm font-medium">{label}</span>
        {info !== undefined && !info.offered && <span className="text-xs text-muted">· Dry run for now</span>}
        {/* The first step has nothing before it to run only if, but one that says so anyway,
            as an imported or reordered one can, shows it, so its problem shows where it is. */}
        {((index > 0 && shownOnly.length > 0) || onlyIf !== '') && (
          <>
            <label htmlFor={`${id}-only`} className="sr-only">
              {`Step ${n} runs`}
            </label>
            <Select<AutopilotOutcome | ''>
              id={`${id}-only`}
              value={onlyIf}
              options={[...always, ...shownOnly.map((o) => ({ value: o, label: outcomeLabel(kit.catalog, o) }))]}
              onChange={(next) => {
                onChange(next === '' ? omit(step, 'onlyIf') : { ...step, onlyIf: next })
              }}
              className={`${fieldClass} py-0.5 text-xs`}
            />
          </>
        )}
        <StepSettings kit={kit} step={step} onChange={onChange} id={id} n={n} />
        <span className="ml-auto inline-flex items-center gap-0.5">
          <button
            type="button"
            aria-label={`Move step ${n} up`}
            title="Move up"
            disabled={index === 0}
            onClick={() => {
              onMove(index - 1)
            }}
            className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-disabled"
          >
            ↑
          </button>
          <button
            type="button"
            aria-label={`Move step ${n} down`}
            title="Move down"
            disabled={index === steps.length - 1}
            onClick={() => {
              onMove(index + 1)
            }}
            className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-disabled"
          >
            ↓
          </button>
          <button
            type="button"
            aria-label={`Remove step ${n}`}
            title="Remove"
            onClick={onRemove}
            className={dangerIconButtonClass}
          >
            <TrashIcon className="size-3.5" />
          </button>
        </span>
      </div>
      {step.github !== undefined && step.github !== null && (
        <div className="pl-6">
          <GitHubStepBlock
            kit={kit}
            prompts={agentKit.prompts}
            path={path}
            kinds={kinds}
            actions={step.github}
            onChange={(github) => {
              onChange({ ...step, github })
            }}
          />
        </div>
      )}
      {step.agent !== undefined && step.agent !== null && (
        <div className="pl-6">
          <AgentStepBlock
            kit={kit}
            agentKit={agentKit}
            path={path}
            kinds={kinds}
            step={step.agent}
            onChange={(agent) => {
              onChange({ ...step, agent })
            }}
          />
        </div>
      )}
      {merge !== null && ticks && (
        <div className="pl-6">
          <MergePastRules
            kit={kit}
            id={id}
            merge={merge}
            warnings={pastWarnings}
            onChange={(next) => {
              onChange({ ...step, merge: next })
            }}
          />
        </div>
      )}
      <div className="pl-6">
        <Notes problems={problems} warnings={warnings} />
      </div>
    </li>
  )
}

interface MergePastRulesProps {
  kit: BuilderKit
  id: string
  merge: AutopilotMergeStep
  // warnings are the backend's at the ticks: that the switch for them is off.
  warnings: readonly AutopilotProblem[]
  onChange: (merge: AutopilotMergeStep) => void
}

// MergePastRules are the ticks of a Merge it with no agent step that let it go past the
// rules, each with what it lets the routine do, in the danger tone: when an approval from
// someone else is all the user's own pull request lacks, which merges when GitHub allows,
// never by auto-merge, and, only with that one, even when it changes workflow, agent, or
// tooling files. While one is ticked, it says that the switch for them in Autopilot's
// settings is off, with the way there, and whether Save asks the user to confirm them, the
// routine as saved merging only what GitHub allows until they do.
function MergePastRules({ kit, id, merge, warnings, onChange }: MergePastRulesProps) {
  const bypass = merge.allowBypass === true
  const sensitive = merge.allowSensitive === true
  const past = kit.pastRules
  const confirmLine = (() => {
    if (past === undefined || (!bypass && !sensitive)) {
      return null
    }

    if (past.saved === 'needsTick') {
      return {
        tone: 'text-warning',
        text: 'You haven’t confirmed it may merge past the rules as it is now, so it merges only what GitHub allows: Save asks you to.',
      }
    }

    return past.confirm
      ? { tone: 'text-muted', text: 'Save asks you to confirm that it may merge past the rules.' }
      : { tone: 'text-muted', text: 'You confirmed it may merge past the rules as it is now.' }
  })()

  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <div className="flex flex-col gap-0.5">
        <label htmlFor={`${id}-bypass`} className="inline-flex items-center gap-1.5">
          <input
            id={`${id}-bypass`}
            type="checkbox"
            className="checkbox"
            checked={bypass}
            aria-describedby={`${id}-bypass-help`}
            onChange={(e) => {
              // Going past the rules merges when GitHub allows; without it, sensitive files
              // never go past them either.
              onChange(
                e.target.checked
                  ? { ...merge, how: 'whenMergeable', allowBypass: true }
                  : omit(omit(merge, 'allowBypass'), 'allowSensitive'),
              )
            }}
          />
          Merge past the rules when an approval is all it lacks
        </label>
        <p id={`${id}-bypass-help`} className="pl-5 text-danger">
          On your own pull request whose every commit you signed, once its checks passed, GitHub lets you merge past the
          branch’s rules, as it lets a repository admin: this does it for you. It merges when GitHub allows, never by
          auto-merge.
        </p>
      </div>
      <div className="flex flex-col gap-0.5">
        <label
          htmlFor={`${id}-sensitive`}
          className={`inline-flex items-center gap-1.5 ${bypass ? '' : 'opacity-disabled'}`}
        >
          <input
            id={`${id}-sensitive`}
            type="checkbox"
            className="checkbox"
            checked={sensitive}
            disabled={!bypass && !sensitive}
            aria-describedby={`${id}-sensitive-help`}
            onChange={(e) => {
              onChange(e.target.checked ? { ...merge, allowSensitive: true } : omit(merge, 'allowSensitive'))
            }}
          />
          Also when it changes workflow, agent, or tooling files
        </label>
        <p id={`${id}-sensitive-help`} className={`pl-5 ${bypass ? 'text-danger' : 'text-muted'}`}>
          {bypass
            ? 'Workflows, agents’ instructions, and tooling run with your access: with this, a change to them merges with no review from anyone else too.'
            : 'Only with the tick above.'}
        </p>
      </div>
      {warnings.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <Notes problems={[]} warnings={warnings} />
          {past?.onSettings !== undefined && (
            <button type="button" onClick={past.onSettings} className={quietButtonClass}>
              Open Autopilot settings
            </button>
          )}
        </div>
      )}
      {confirmLine !== null && <p className={confirmLine.tone}>{confirmLine.text}</p>}
    </div>
  )
}

interface StepSettingsProps {
  kit: BuilderKit
  step: AutopilotStep
  onChange: (step: AutopilotStep) => void
  id: string
  n: string
}

// StepSettings are a step's own settings, on its line: a note, the days to snooze, how
// long to wait for the checks, how to merge, how to post a review, what to tell.
function StepSettings({ kit, step, onChange, id, n }: StepSettingsProps) {
  if (step.notify !== undefined && step.notify !== null) {
    const notify = step.notify

    return (
      <>
        <label htmlFor={`${id}-note`} className="sr-only">
          {`Step ${n}: a note of your own`}
        </label>
        <input
          id={`${id}-note`}
          value={notify.note ?? ''}
          placeholder="A note of your own (optional)"
          onChange={(e) => {
            onChange({ ...step, notify: { ...notify, note: e.target.value } })
          }}
          className={`${fieldClass} min-w-40 flex-1 py-0.5 text-xs`}
        />
      </>
    )
  }

  const range = (setting: string) => settingRange(kit.catalog.stepSettings, setting)

  if (step.snooze !== undefined && step.snooze !== null) {
    const snooze = step.snooze
    const days = range('snooze.days')

    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-muted">
        for
        <NumberField
          id={`${id}-days`}
          label={`Step ${n}: days to snooze`}
          value={snooze.days}
          fallback={days.default}
          min={days.min}
          max={days.max}
          unit="days"
          onChange={(days) => {
            onChange({ ...step, snooze: days === null ? omit(snooze, 'days') : { ...snooze, days } })
          }}
        />
      </span>
    )
  }

  if (step.waitForChecks !== undefined && step.waitForChecks !== null) {
    const wait = step.waitForChecks
    const settle = range('waitForChecks.settleMinutes')
    const giveUp = range('waitForChecks.giveUpHours')

    return (
      <span className="inline-flex flex-wrap items-center gap-1.5 text-xs text-muted">
        settle
        <NumberField
          id={`${id}-settle`}
          label={`Step ${n}: minutes to settle`}
          value={wait.settleMinutes}
          fallback={settle.default}
          min={settle.min}
          max={settle.max}
          unit="min"
          onChange={(v) => {
            onChange({
              ...step,
              waitForChecks: v === null ? omit(wait, 'settleMinutes') : { ...wait, settleMinutes: v },
            })
          }}
        />
        give up after
        <NumberField
          id={`${id}-giveup`}
          label={`Step ${n}: hours before giving up`}
          value={wait.giveUpHours}
          fallback={giveUp.default}
          min={giveUp.min}
          max={giveUp.max}
          unit="h"
          onChange={(v) => {
            onChange({ ...step, waitForChecks: v === null ? omit(wait, 'giveUpHours') : { ...wait, giveUpHours: v } })
          }}
        />
      </span>
    )
  }

  if (step.merge !== undefined && step.merge !== null) {
    const merge = step.merge
    // A merge past the rules merges when GitHub allows, never by auto-merge.
    const past = merge.allowBypass === true || merge.allowSensitive === true

    return (
      <>
        <label htmlFor={`${id}-how`} className="sr-only">
          {`Step ${n}: how it merges`}
        </label>
        <select
          id={`${id}-how`}
          value={merge.how}
          title={past ? 'A merge past the rules merges when GitHub allows, never by auto-merge.' : undefined}
          onChange={(e) => {
            onChange({
              ...step,
              merge: { ...merge, how: e.target.value === 'whenMergeable' ? 'whenMergeable' : 'autoMergeIfItWasOn' },
            })
          }}
          className={`${fieldClass} py-0.5 text-xs`}
        >
          <option value="autoMergeIfItWasOn" disabled={past}>
            auto-merge if it was on, else when GitHub allows
          </option>
          <option value="whenMergeable">when GitHub allows</option>
        </select>
        <label htmlFor={`${id}-method`} className="sr-only">
          {`Step ${n}: merge method`}
        </label>
        <select
          id={`${id}-method`}
          value={merge.method ?? ''}
          onChange={(e) => {
            const method = e.target.value

            onChange({
              ...step,
              merge:
                method === 'merge' || method === 'squash' || method === 'rebase'
                  ? { ...merge, method }
                  : omit(merge, 'method'),
            })
          }}
          className={`${fieldClass} py-0.5 text-xs`}
        >
          <option value="">the repository’s own method</option>
          <option value="merge">merge commit</option>
          <option value="squash">squash</option>
          <option value="rebase">rebase</option>
        </select>
      </>
    )
  }

  if (step.postReview !== undefined && step.postReview !== null) {
    const review = step.postReview

    return (
      <>
        <label htmlFor={`${id}-as`} className="sr-only">
          {`Step ${n}: post it as`}
        </label>
        <select
          id={`${id}-as`}
          value={review.as}
          onChange={(e) => {
            onChange({
              ...step,
              postReview: e.target.value === 'verdict' ? { ...review, as: 'verdict' } : { as: 'comment' },
            })
          }}
          className={`${fieldClass} py-0.5 text-xs`}
        >
          <option value="comment">as a comment</option>
          <option value="verdict">as its verdict</option>
        </select>
        {review.as === 'verdict' && (
          <label htmlFor={`${id}-approve`} className="inline-flex items-center gap-1.5 text-xs">
            <input
              id={`${id}-approve`}
              type="checkbox"
              className="checkbox"
              checked={review.allowApprove ?? false}
              onChange={(e) => {
                onChange({
                  ...step,
                  postReview: e.target.checked ? { ...review, allowApprove: true } : omit(review, 'allowApprove'),
                })
              }}
            />
            Allow approving
          </label>
        )}
      </>
    )
  }

  if (step.script !== undefined && step.script !== null) {
    return (
      <ScriptStepBlock
        n={n}
        step={step.script}
        onChange={(script) => {
          onChange({ ...step, script })
        }}
      />
    )
  }

  if (step.tellMe !== undefined && step.tellMe !== null) {
    const tell = step.tellMe

    return (
      <>
        <label htmlFor={`${id}-tell`} className="sr-only">
          {`Step ${n}: what it tells`}
        </label>
        <select
          id={`${id}-tell`}
          value={tell.when ?? 'reached'}
          onChange={(e) => {
            onChange({ ...step, tellMe: { when: e.target.value === 'verdict' ? 'verdict' : 'reached' } })
          }}
          className={`${fieldClass} py-0.5 text-xs`}
        >
          <option value="reached">that it got here</option>
          <option value="verdict">how the steps before it ended</option>
        </select>
      </>
    )
  }

  const help = kit.catalog.steps.find((s) => s.kind === stepKindOf(step))?.help

  return help === undefined ? null : <span className="text-xs text-muted">{help}</span>
}
