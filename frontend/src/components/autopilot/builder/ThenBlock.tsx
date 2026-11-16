import { useId } from 'react'

import {
  addable,
  moved,
  newStep,
  omit,
  onlyIfChoices,
  outcomeLabel,
  problemsAt,
  problemsExactly,
  stepKindOf,
} from '../../../lib/routineBuilder'
import type { AutopilotOutcome, AutopilotProblem, AutopilotSpec, AutopilotStep, ItemKind } from '../../../platform'
import { fieldClass, quietButtonClass } from '../../controls'
import { PlusIcon, TrashIcon } from '../../icons'
import { MenuButton, type MenuItem } from '../../Menu'
import { AgentStepBlock, type AgentKit } from './AgentStepBlock'
import { Block, Notes, NumberField, type BuilderKit } from './parts'

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
      onChange([...steps, newStep(c.kind, steps)])
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
  // The agent step's block shows what is wrong inside it.
  const inside = `${path}.agent`
  const own = (list: readonly AutopilotProblem[]) =>
    problemsAt(list, 'steps', path).filter((p) => p.field !== inside && !p.field.startsWith(`${inside}.`))
  const problems = own(kit.problems)
  const warnings = own(kit.warnings)

  return (
    <li className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="w-4 text-right text-xs font-semibold text-muted tabular-nums">{n}</span>
        <span className="text-sm font-medium">{label}</span>
        {info !== undefined && !info.offered && <span className="text-xs text-muted">· Dry run for now</span>}
        {index > 0 && (shownOnly.length > 0 || onlyIf !== '') && (
          <>
            <label htmlFor={`${id}-only`} className="sr-only">
              {`Step ${n} runs`}
            </label>
            <select
              id={`${id}-only`}
              value={onlyIf}
              onChange={(e) => {
                const next = e.target.value as AutopilotOutcome | ''

                onChange(next === '' ? omit(step, 'onlyIf') : { ...step, onlyIf: next })
              }}
              className={`${fieldClass} py-0.5 text-xs`}
            >
              {kind !== 'merge' && <option value="">always</option>}
              {shownOnly.map((o) => (
                <option key={o} value={o}>
                  {outcomeLabel(kit.catalog, o)}
                </option>
              ))}
            </select>
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
            className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-40"
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
            className="rounded-md px-1.5 py-0.5 text-sm text-muted hover:bg-raised hover:text-text disabled:opacity-40"
          >
            ↓
          </button>
          <button
            type="button"
            aria-label={`Remove step ${n}`}
            title="Remove"
            onClick={onRemove}
            className="rounded-md p-1 text-muted hover:bg-raised hover:text-danger"
          >
            <TrashIcon className="size-3.5" />
          </button>
        </span>
      </div>
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
      <div className="pl-6">
        <Notes problems={problems} warnings={warnings} />
      </div>
    </li>
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

  if (step.snooze !== undefined && step.snooze !== null) {
    const snooze = step.snooze

    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-muted">
        for
        <NumberField
          id={`${id}-days`}
          label={`Step ${n}: days to snooze`}
          value={snooze.days}
          fallback={3}
          min={1}
          max={365}
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

    return (
      <span className="inline-flex flex-wrap items-center gap-1.5 text-xs text-muted">
        settle
        <NumberField
          id={`${id}-settle`}
          label={`Step ${n}: minutes to settle`}
          value={wait.settleMinutes}
          fallback={2}
          min={0}
          max={60}
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
          fallback={2}
          min={1}
          max={24}
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

    return (
      <>
        <label htmlFor={`${id}-how`} className="sr-only">
          {`Step ${n}: how it merges`}
        </label>
        <select
          id={`${id}-how`}
          value={merge.how}
          onChange={(e) => {
            onChange({
              ...step,
              merge: { ...merge, how: e.target.value === 'whenMergeable' ? 'whenMergeable' : 'autoMergeIfItWasOn' },
            })
          }}
          className={`${fieldClass} py-0.5 text-xs`}
        >
          <option value="autoMergeIfItWasOn">auto-merge if it was on, else when GitHub allows</option>
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
