import { useId, useState } from 'react'

import { agentName, anyEffortChoices, effortLabel, isAgentPrompt, modelLabel, promptApplies } from '../../../lib/launch'
import { omit, placeOf, problemsAt, toolsOf } from '../../../lib/routineBuilder'
import type {
  AgentID,
  AgentInfo,
  AutopilotAgentStep,
  AutopilotFence,
  AutopilotLaunchRow,
  AutopilotPreset,
  ItemKind,
  PerAgent,
  Profile,
  Prompt,
} from '../../../platform'
import { fieldClass, quietButtonClass, Segmented } from '../../controls'
import { ChevronDownIcon, ChevronRightIcon, ExternalIcon, PlusIcon, TrashIcon } from '../../icons'
import { LaunchOverrideFields } from '../../LaunchOverrideFields'
import { Notes, NumberField, Reset, type BuilderKit } from './parts'

// AgentKit is what the agent step's block reads beside the builder's kit: the prompt
// library, the coding agents and their profiles, the launch defaults a click would use at
// the app's level, and the way to the Prompts page.
export interface AgentKit {
  prompts: readonly Prompt[]
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  defaultAgent: AgentID
  defaultProfiles: PerAgent<string>
  onEditPrompts: () => void
}

export interface AgentStepBlockProps {
  kit: BuilderKit
  agentKit: AgentKit
  // path is where the step is in the steps: "[0]".
  path: string
  // kinds are the kinds of item the routine acts on, whose prompts fit.
  kinds: readonly ItemKind[]
  step: AutopilotAgentStep
  onChange: (step: AutopilotAgentStep) => void
}

// promptValue is a prompt reference as the prompt menu holds it.
function promptValue(step: AutopilotAgentStep): string {
  if (step.prompt.itemDefault === true) {
    return 'item'
  }

  if ((step.prompt.starter ?? '') !== '') {
    return `starter:${step.prompt.starter ?? ''}`
  }

  return `nickname:${step.prompt.nickname ?? ''}`
}

// promptRef is a prompt menu's value as the spec names it: by its starter's key, which
// outlives a rename, else by its nickname.
function promptRef(value: string): AutopilotAgentStep['prompt'] {
  if (value === 'item') {
    return { itemDefault: true }
  }

  const at = value.indexOf(':')
  const kind = value.slice(0, at)
  const name = value.slice(at + 1)

  return kind === 'starter' ? { starter: name } : { nickname: name }
}

// agentKey is a real agent's ID, as per-agent settings are keyed.
type AgentKey = Exclude<AgentID, ''>

// AgentStepBlock is the agent step's settings: the prompt it runs, where it runs, its
// launch settings on one line (As a click would: Claude · default · Opus · high) with
// Change ▸ and + Per org, and in the background its fence: the budget, the time limit, and
// the tools. Every value set here says so, with Reset; every other inherits what a click
// would use.
export function AgentStepBlock({ kit, agentKit, path, kinds, step, onChange }: AgentStepBlockProps) {
  const id = useId()
  const [changing, setChanging] = useState(false)
  const [perOrg, setPerOrg] = useState((step.launchFor?.length ?? 0) > 0)
  const field = `${path}.agent`
  const place = placeOf(step)
  const inherits = (step.agent ?? 'inherit') === 'inherit'
  const agentID: AgentID = inherits ? agentKit.defaultAgent : ((step.agent ?? '') as AgentID)
  const agent = agentKit.agents.find((a) => a.id === agentID)
  const profileName =
    (step.profile ?? '') === '' ? (agentKit.defaultProfiles[agentID as AgentKey] ?? 'default') : (step.profile ?? '')
  const profile = agentKit.profiles.find((p) => p.agent === agentID && p.name === profileName)
  const model = (step.model ?? '') === '' ? (profile?.model ?? '') : (step.model ?? '')
  const effort = (step.effort ?? '') === '' ? (profile?.effort ?? '') : (step.effort ?? '')
  const allInherit = inherits && (step.profile ?? '') === '' && (step.model ?? '') === '' && (step.effort ?? '') === ''
  const line = [
    agentName(agentKit.agents, agentID),
    profileName,
    model === '' ? 'its default model' : modelLabel(model, agent?.models),
    effort === '' ? 'default effort' : effortLabel(effort, agent?.effortLevels).toLowerCase(),
  ].join(' · ')
  const prompts = agentKit.prompts.filter(isAgentPrompt)
  const offered = prompts.filter((p) => kinds.every((k) => promptApplies(p, k)))
  const current = promptValue(step)
  const promptProblems = problemsAt(kit.problems, 'steps', `${field}.prompt`)
  const known =
    current === 'item' ||
    offered.some((p) => (p.starterKey !== '' ? `starter:${p.starterKey}` : `nickname:${p.nickname}`) === current)
  const patch = (change: Partial<AutopilotAgentStep>) => {
    onChange({ ...step, ...change })
  }
  const fence = step.fence ?? {}
  const setFence = (next: AutopilotFence) => {
    onChange(Object.keys(next).length === 0 ? omit(step, 'fence') : { ...step, fence: next })
  }
  const errors: Record<string, string | undefined> = {
    [`models.${agentID}`]: problemsAt(kit.problems, 'steps', `${field}.model`)[0]?.message,
    effort: problemsAt(kit.problems, 'steps', `${field}.effort`)[0]?.message,
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-2">
      <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-x-2 gap-y-2">
        <label htmlFor={`${id}-prompt`} className="text-xs text-muted">
          Prompt
        </label>
        <div className="flex flex-wrap items-center gap-2">
          <select
            id={`${id}-prompt`}
            value={current}
            aria-invalid={promptProblems.length > 0}
            onChange={(e) => {
              patch({ prompt: promptRef(e.target.value) })
            }}
            className={`${fieldClass} py-1`}
          >
            <option value="item">The item’s own default prompt</option>
            {offered.map((p) => {
              const value = p.starterKey !== '' ? `starter:${p.starterKey}` : `nickname:${p.nickname}`

              return (
                <option key={p.id} value={value}>
                  {p.nickname}
                </option>
              )
            })}
            {!known && <option value={current}>{current.slice(current.indexOf(':') + 1)}</option>}
          </select>
          <button type="button" onClick={agentKit.onEditPrompts} className={`${quietButtonClass} ml-auto`}>
            Edit in Prompts
            <ExternalIcon className="size-3" />
          </button>
        </div>
        <span className="text-xs text-muted">Where</span>
        <Segmented
          label="Where it runs"
          size="sm"
          value={place}
          options={[
            { value: 'background', label: 'In the background' },
            { value: 'terminal', label: 'In my terminal' },
          ]}
          onChange={(where) => {
            // A terminal run has a click's access, and no fence; in the background the
            // tools decide plan mode, and the worktree is always on.
            onChange(
              where === 'terminal'
                ? { ...omit(step, 'fence'), where }
                : { ...omit(omit(step, 'planMode'), 'worktree'), where },
            )
          }}
        />
        <span className="text-xs text-muted">Launch</span>
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span>
            {allInherit && <span className="text-muted">As a click would: </span>}
            {line}
          </span>
          <button
            type="button"
            aria-expanded={changing}
            onClick={() => {
              setChanging(!changing)
            }}
            className={quietButtonClass}
          >
            {changing ? <ChevronDownIcon className="size-3.5" /> : <ChevronRightIcon className="size-3.5" />}
            Change
          </button>
          <button
            type="button"
            aria-expanded={perOrg}
            onClick={() => {
              setPerOrg(!perOrg)
            }}
            className={quietButtonClass}
          >
            <PlusIcon className="size-3.5" />
            Per org
          </button>
        </div>
      </div>
      {changing && (
        <div className="flex flex-col gap-3 rounded-md bg-raised/40 p-2">
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1">
              <label htmlFor={`${id}-agent`} className="text-xs font-medium text-muted">
                Agent
              </label>
              <select
                id={`${id}-agent`}
                value={inherits ? 'inherit' : (step.agent ?? '')}
                onChange={(e) => {
                  // A model and a profile are one agent's own.
                  patch({ agent: e.target.value, profile: '', model: '' })
                }}
                className={`${fieldClass} py-1`}
              >
                <option value="inherit">{`Inherit · ${agentName(agentKit.agents, agentKit.defaultAgent)} (app)`}</option>
                {agentKit.agents
                  .filter((a) => a.id !== '')
                  .map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <label htmlFor={`${id}-profile`} className="text-xs font-medium text-muted">
                Profile
              </label>
              <select
                id={`${id}-profile`}
                value={step.profile ?? ''}
                onChange={(e) => {
                  patch({ profile: e.target.value })
                }}
                className={`${fieldClass} py-1`}
              >
                <option value="">{`Inherit · ${agentKit.defaultProfiles[agentID as AgentKey] ?? 'default'} (app)`}</option>
                {agentKit.profiles
                  .filter((p) => p.agent === agentID)
                  .map((p) => (
                    <option key={p.name} value={p.name}>
                      {p.name}
                    </option>
                  ))}
                {(step.profile ?? '') !== '' &&
                  !agentKit.profiles.some((p) => p.agent === agentID && p.name === step.profile) && (
                    <option value={step.profile ?? ''}>{step.profile}</option>
                  )}
              </select>
            </div>
            <LaunchOverrideFields
              id={`${id}-launch`}
              agents={agentKit.agents}
              only={[agentID]}
              withFlags={false}
              background={place === 'background'}
              values={{
                models: { [agentID]: step.model ?? '' },
                effort: step.effort ?? '',
                planMode: step.planMode ?? null,
                worktree: step.worktree ?? null,
                extraFlags: {},
              }}
              onChange={(change) => {
                const next: AutopilotAgentStep = { ...step }

                if (change.models !== undefined) {
                  next.model = change.models[agentID as AgentKey] ?? ''
                }

                if (change.effort !== undefined) {
                  next.effort = change.effort
                }

                onChange(withTri(withTri(next, 'planMode', change.planMode), 'worktree', change.worktree))
              }}
              errors={errors}
              inherited={{
                models: {
                  [agentID]:
                    profile?.model === undefined || profile.model === ''
                      ? 'the profile’s'
                      : modelLabel(profile.model, agent?.models),
                },
                effort:
                  profile?.effort === undefined || profile.effort === ''
                    ? 'the agent’s default'
                    : effortLabel(profile.effort, agent?.effortLevels),
                planMode: 'as a click',
                worktree: 'as a click',
              }}
              flagsHint={undefined}
            />
          </div>
          <Fallbacks agentKit={agentKit} step={step} onChange={onChange} />
          <p className="text-xs text-muted">
            Inherit takes each repository’s and org’s own launch defaults first, as a click does; these show the app’s.
          </p>
        </div>
      )}
      {perOrg && <LaunchRows kit={kit} agentKit={agentKit} step={step} onChange={onChange} />}
      {place === 'background' ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          <span className="text-xs text-muted">Fence</span>
          <Budget id={`${id}-budget`} fence={fence} onChange={setFence} />
          <span className="inline-flex items-center gap-1.5">
            <span className="text-muted">Time</span>
            <NumberField
              id={`${id}-time`}
              label="Time limit"
              value={fence.timeoutMinutes}
              fallback={30}
              min={5}
              max={180}
              unit="min"
              onChange={(n) => {
                setFence(n === null ? omit(fence, 'timeoutMinutes') : { ...fence, timeoutMinutes: n })
              }}
            />
          </span>
          <span className="inline-flex items-center gap-1.5">
            <label htmlFor={`${id}-tools`} className="text-muted">
              Tools
            </label>
            <select
              id={`${id}-tools`}
              value={toolsOf(step)}
              onChange={(e) => {
                setFence({ ...fence, tools: e.target.value as AutopilotPreset })
              }}
              className={`${fieldClass} py-1`}
            >
              {kit.catalog.presets.map((p) => (
                <option key={p.id} value={p.id} title={p.help}>
                  {p.label}
                </option>
              ))}
            </select>
            {(fence.tools ?? '') !== '' ? (
              <Reset
                label="Reset the tools"
                onReset={() => {
                  setFence(omit(fence, 'tools'))
                }}
              />
            ) : (
              <span className="text-xs text-muted">default</span>
            )}
          </span>
        </div>
      ) : (
        <p className="text-xs text-muted">
          A terminal run is exactly a click’s launch, with its access; nothing the app can see follows it but a notice.
        </p>
      )}
      <Notes problems={problemsAt(kit.problems, 'steps', field)} warnings={problemsAt(kit.warnings, 'steps', field)} />
    </div>
  )
}

// withTri sets a terminal run's plan mode or worktree, or leaves it to inherit for null;
// undefined changes nothing.
function withTri(step: AutopilotAgentStep, key: 'planMode' | 'worktree', value: boolean | null | undefined) {
  if (value === undefined) {
    return step
  }

  return value === null ? omit(step, key) : { ...step, [key]: value }
}

// Budget is a background run's budget per run, in dollars or in tokens: the default $3.00
// until set, with Reset.
function Budget({
  id,
  fence,
  onChange,
}: {
  id: string
  fence: AutopilotFence
  onChange: (fence: AutopilotFence) => void
}) {
  const budget = fence.budget
  const tokens = budget?.tokens !== undefined && budget.tokens !== null
  const set = budget !== undefined && budget !== null

  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="text-muted">Budget</span>
      <label htmlFor={`${id}-unit`} className="sr-only">
        Budget in
      </label>
      <select
        id={`${id}-unit`}
        value={tokens ? 'tokens' : 'usd'}
        onChange={(e) => {
          onChange({ ...fence, budget: e.target.value === 'tokens' ? { tokens: 200_000 } : { usd: 3 } })
        }}
        className={`${fieldClass} py-1`}
      >
        <option value="usd">dollars</option>
        <option value="tokens">tokens</option>
      </select>
      <label htmlFor={id} className="sr-only">
        Budget per run
      </label>
      <input
        id={id}
        type="number"
        inputMode="decimal"
        min={tokens ? 10_000 : 0.5}
        max={tokens ? 50_000_000 : 50}
        step={tokens ? 10_000 : 0.5}
        value={tokens ? (budget.tokens ?? '') : (budget?.usd ?? '')}
        placeholder={tokens ? '200000' : '3.00'}
        onChange={(e) => {
          const n = e.target.valueAsNumber

          if (e.target.value === '' || Number.isNaN(n)) {
            onChange(omit(fence, 'budget'))
          } else {
            onChange({ ...fence, budget: tokens ? { tokens: Math.trunc(n) } : { usd: n } })
          }
        }}
        className={`${fieldClass} w-24 py-1 text-right tabular-nums`}
      />
      {set ? (
        <Reset
          label="Reset the budget"
          onReset={() => {
            onChange(omit(fence, 'budget'))
          }}
        />
      ) : (
        <span className="text-xs text-muted">default $3.00</span>
      )}
    </span>
  )
}

interface RowsProps {
  agentKit: AgentKit
  step: AutopilotAgentStep
  onChange: (step: AutopilotAgentStep) => void
}

// maxFallbacks caps the profiles an agent step falls back to.
const maxFallbacks = 3

// Fallbacks are the profiles the step falls back to, in order, when one is out of usage:
// another agent's included.
function Fallbacks({ agentKit, step, onChange }: RowsProps) {
  const id = useId()
  const rows = step.fallbacks ?? []
  const set = (next: NonNullable<AutopilotAgentStep['fallbacks']>) => {
    onChange(next.length === 0 ? omit(step, 'fallbacks') : { ...step, fallbacks: next })
  }

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-muted">Fallbacks</span>
      {rows.length === 0 && <p className="text-xs text-muted">None: a profile out of usage waits.</p>}
      {rows.map((r, i) => (
        // A fallback has no identity but its place.
        <div key={i} className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted">{`${String(i + 1)}.`}</span>
          <label htmlFor={`${id}-${String(i)}-agent`} className="sr-only">
            {`Fallback ${String(i + 1)}: agent`}
          </label>
          <select
            id={`${id}-${String(i)}-agent`}
            value={r.agent}
            onChange={(e) => {
              set(rows.map((x, j) => (j === i ? { agent: e.target.value, profile: '' } : x)))
            }}
            className={`${fieldClass} py-1`}
          >
            {agentKit.agents
              .filter((a) => a.id !== '')
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
          <label htmlFor={`${id}-${String(i)}-profile`} className="sr-only">
            {`Fallback ${String(i + 1)}: profile`}
          </label>
          <input
            id={`${id}-${String(i)}-profile`}
            list={`${id}-${String(i)}-profiles`}
            value={r.profile}
            placeholder="Profile"
            spellCheck={false}
            onChange={(e) => {
              set(rows.map((x, j) => (j === i ? { ...x, profile: e.target.value } : x)))
            }}
            className={`${fieldClass} w-36 py-1`}
          />
          <datalist id={`${id}-${String(i)}-profiles`}>
            {agentKit.profiles
              .filter((p) => p.agent === r.agent)
              .map((p) => (
                <option key={p.name} value={p.name} />
              ))}
          </datalist>
          <button
            type="button"
            aria-label={`Remove fallback ${String(i + 1)}`}
            title="Remove"
            onClick={() => {
              set(rows.filter((_, j) => j !== i))
            }}
            className="rounded-md p-1 text-muted hover:bg-raised hover:text-danger"
          >
            <TrashIcon className="size-3.5" />
          </button>
        </div>
      ))}
      {rows.length < maxFallbacks && (
        <button
          type="button"
          onClick={() => {
            set([...rows, { agent: agentKit.agents.find((a) => a.id !== '')?.id ?? 'claude', profile: '' }])
          }}
          className={`${quietButtonClass} self-start`}
        >
          <PlusIcon className="size-3.5" />
          Add a fallback
        </button>
      )}
    </div>
  )
}

// LaunchRows are the step's launch settings for one org or repository each, for this
// routine only: for acme-org, the work profile on Opus.
function LaunchRows({ kit, agentKit, step, onChange }: RowsProps & { kit: BuilderKit }) {
  const id = useId()
  const rows = step.launchFor ?? []
  const set = (next: AutopilotLaunchRow[]) => {
    onChange(next.length === 0 ? omit(step, 'launchFor') : { ...step, launchFor: next })
  }
  const patch = (i: number, change: Partial<AutopilotLaunchRow>) => {
    set(rows.map((r, j) => (j === i ? { ...r, ...change } : r)))
  }

  return (
    <div className="flex flex-col gap-1.5 rounded-md bg-raised/40 p-2">
      <span className="text-xs font-medium text-muted">Per org or repository</span>
      {rows.map((r, i) => {
        const at = `${id}-${String(i)}`
        const name = `Row ${String(i + 1)}`

        return (
          // A row has no identity but its place.
          <div key={i} className="flex flex-wrap items-center gap-1.5">
            <label htmlFor={`${at}-target`} className="sr-only">
              {`${name}: org or repository`}
            </label>
            <input
              id={`${at}-target`}
              list={`${at}-owners`}
              value={r.target}
              placeholder="acme-org or acme-org/api"
              spellCheck={false}
              onChange={(e) => {
                patch(i, { target: e.target.value })
              }}
              className={`${fieldClass} w-44 py-1`}
            />
            <datalist id={`${at}-owners`}>
              {kit.suggestions.owners.map((o) => (
                <option key={o} value={o} />
              ))}
            </datalist>
            <label htmlFor={`${at}-agent`} className="sr-only">
              {`${name}: agent`}
            </label>
            <select
              id={`${at}-agent`}
              value={r.agent ?? ''}
              onChange={(e) => {
                patch(i, { agent: e.target.value })
              }}
              className={`${fieldClass} py-1`}
            >
              <option value="">Its own agent</option>
              {agentKit.agents
                .filter((a) => a.id !== '')
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
            </select>
            <label htmlFor={`${at}-profile`} className="sr-only">
              {`${name}: profile`}
            </label>
            <input
              id={`${at}-profile`}
              value={r.profile ?? ''}
              placeholder="Its profile"
              spellCheck={false}
              onChange={(e) => {
                patch(i, { profile: e.target.value })
              }}
              className={`${fieldClass} w-28 py-1`}
            />
            <label htmlFor={`${at}-model`} className="sr-only">
              {`${name}: model`}
            </label>
            <input
              id={`${at}-model`}
              value={r.model ?? ''}
              placeholder="Its model"
              spellCheck={false}
              onChange={(e) => {
                patch(i, { model: e.target.value })
              }}
              className={`${fieldClass} w-28 py-1`}
            />
            <label htmlFor={`${at}-effort`} className="sr-only">
              {`${name}: effort`}
            </label>
            <select
              id={`${at}-effort`}
              value={r.effort ?? ''}
              onChange={(e) => {
                patch(i, { effort: e.target.value })
              }}
              className={`${fieldClass} py-1`}
            >
              <option value="">Its effort</option>
              {anyEffortChoices(agentKit.agents).map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
            <button
              type="button"
              aria-label={`Remove row ${String(i + 1)}`}
              title="Remove"
              onClick={() => {
                set(rows.filter((_, j) => j !== i))
              }}
              className="rounded-md p-1 text-muted hover:bg-raised hover:text-danger"
            >
              <TrashIcon className="size-3.5" />
            </button>
          </div>
        )
      })}
      <button
        type="button"
        onClick={() => {
          set([...rows, { target: '' }])
        }}
        className={`${quietButtonClass} self-start`}
      >
        <PlusIcon className="size-3.5" />
        Add an org or repository
      </button>
    </div>
  )
}
