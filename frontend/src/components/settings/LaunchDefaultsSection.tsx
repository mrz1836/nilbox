import { startTransition, useActionState, useId, useState } from 'react'

import { attempt } from '../../lib/call'
import {
  agentName,
  anyEffortChoices,
  effortLabel,
  modelChoices,
  modelLabel,
  perAgentEntries,
  pinSummary,
  promptApplies,
} from '../../lib/launch'
import { defaultsInput, emptyDefaults, findDefaults } from '../../lib/launchChoices'
import { useDebouncedQuery, useQuery } from '../../lib/useQuery'
import {
  checkLaunchDefaults,
  listLaunchTargets,
  removeLaunchDefaults,
  saveLaunchDefaults,
  type AgentID,
  type AgentInfo,
  type DefaultPrompts,
  type LaunchDefaults,
  type LaunchDefaultsInput,
  type LaunchTarget,
  type Profile,
  type Prompt,
  type Settings,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, chipClass, describedBy, Field, fieldClass, primaryButtonClass } from '../controls'
import { PlusIcon } from '../icons'
import { LaunchOverrideFields, type LaunchOverrides } from '../LaunchOverrideFields'
import { ErrorText, SectionIntro } from './rows'

export interface LaunchDefaultsSectionProps {
  settings: Settings
  // agents are the coding agents, whose profiles, models, and flags the defaults set.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  // onChanged reloads the launch configuration; it resolves once it is in.
  onChanged: () => Promise<void>
}

// Editing is the defaults being edited: new ones (target chosen in the form) or stored
// ones (target fixed).
type Editing = { kind: 'new' } | { kind: 'existing'; owner: string; name: string }

// noTargets is the list of targets offered until it loads.
const noTargets: readonly LaunchTarget[] = []

// targetText writes a target as the editor shows it: "acme-org" or "acme-org/api".
function targetText(owner: string, name: string): string {
  return name === '' ? owner : `${owner}/${name}`
}

// parseTarget reads "owner" or "owner/name".
function parseTarget(text: string): { owner: string; name: string } {
  const [owner = '', ...rest] = text.trim().split('/')

  return { owner: owner.trim(), name: rest.join('/').trim() }
}

// summary lists what a set of defaults sets, as short chips.
function summary(d: LaunchDefaults, prompts: readonly Prompt[], agents: readonly AgentInfo[]): string[] {
  const out: string[] = []
  const nickname = (id: number | null) => prompts.find((p) => p.id === id)?.nickname ?? 'a deleted prompt'

  if (d.agent !== '') {
    out.push(`starts ${agentName(agents, d.agent)}`)
  }

  for (const [id, profile] of perAgentEntries(d.profiles, agents)) {
    out.push(`${agentName(agents, id)} profile ${profile}`)
  }

  if (d.prPromptId !== null) {
    out.push(`${nickname(d.prPromptId)} for PRs`)
  }

  if (d.issuePromptId !== null) {
    out.push(`${nickname(d.issuePromptId)} for issues`)
  }

  return [...out, ...pinSummary(d, agents)]
}

// LaunchDefaultsSection lists the launch defaults saved for orgs and repositories, and
// edits them: the coding agent, each agent's profile, model, and extra flags, the default
// prompt for each kind, and the effort, plan mode, and worktree. An org's defaults apply to
// each of its repositories; a repository's own win over them. Every value left on Inherit
// shows what it inherits.
export function LaunchDefaultsSection({
  settings,
  agents,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  onChanged,
}: LaunchDefaultsSectionProps) {
  const [editing, setEditing] = useState<Editing | null>(null)
  const [status, setStatus] = useState('')
  const [removing, setRemoving] = useState<LaunchDefaults | null>(null)

  const remove = async (d: LaunchDefaults) => {
    setRemoving(null)

    const result = await attempt(removeLaunchDefaults(d.owner, d.name))

    if (!result.ok) {
      setStatus(result.error)

      return
    }

    await onChanged()
    setStatus(`Removed the defaults for ${targetText(d.owner, d.name)}.`)
  }

  return (
    <div className="flex flex-col gap-3">
      <SectionIntro
        title="Launch defaults"
        action={
          <button
            type="button"
            disabled={editing !== null}
            onClick={() => {
              setEditing({ kind: 'new' })
              setStatus('')
            }}
            className={`${buttonClass} shrink-0`}
          >
            <PlusIcon className="size-4" />
            Add defaults
          </button>
        }
      >
        What launches use for an org&apos;s repositories, or for one repository. A repository&apos;s own defaults win
        over its org&apos;s, and a prompt&apos;s settings win over both.
      </SectionIntro>

      {editing?.kind === 'new' && (
        <DefaultsEditor
          editing={editing}
          settings={settings}
          agents={agents}
          profiles={profiles}
          prompts={prompts}
          defaultPrompts={defaultPrompts}
          launchDefaults={launchDefaults}
          onDone={(message) => {
            setEditing(null)
            setStatus(message)
          }}
          onChanged={onChanged}
        />
      )}

      {launchDefaults.length === 0 && editing === null ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-5 text-center text-sm text-muted">
          No launch defaults yet. Launches use the app&apos;s settings under Launching, and each profile&apos;s own.
        </p>
      ) : (
        launchDefaults.length > 0 && (
          <ul
            aria-label="Launch defaults"
            className="divide-y divide-border rounded-lg border border-border bg-surface"
          >
            {launchDefaults.map((d) => {
              const isEditing = editing?.kind === 'existing' && editing.owner === d.owner && editing.name === d.name

              return (
                <li key={`${d.owner}/${d.name}`} className={d.name === '' ? '' : 'bg-background/40'}>
                  {isEditing ? (
                    <div className="p-2">
                      <DefaultsEditor
                        editing={editing}
                        settings={settings}
                        agents={agents}
                        profiles={profiles}
                        prompts={prompts}
                        defaultPrompts={defaultPrompts}
                        launchDefaults={launchDefaults}
                        onDone={(message) => {
                          setEditing(null)
                          setStatus(message)
                        }}
                        onChanged={onChanged}
                      />
                    </div>
                  ) : (
                    <div className={`flex items-center gap-4 py-3 pr-3 ${d.name === '' ? 'pl-4' : 'pl-8'}`}>
                      <div className="min-w-0 flex-1">
                        <p className="flex items-baseline gap-2">
                          <span className="truncate text-sm font-medium">{targetText(d.owner, d.name)}</span>
                          <span className="shrink-0 text-xs text-muted">
                            {d.name === '' ? 'All repositories' : 'Repository'}
                          </span>
                        </p>
                        <ul className="mt-1 flex flex-wrap gap-1.5">
                          {summary(d, prompts, agents).map((chip) => (
                            <li key={chip} className={chipClass}>
                              {chip}
                            </li>
                          ))}
                        </ul>
                      </div>
                      <button
                        type="button"
                        disabled={editing !== null}
                        onClick={() => {
                          setEditing({ kind: 'existing', owner: d.owner, name: d.name })
                          setStatus('')
                        }}
                        className={buttonClass}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        disabled={editing !== null}
                        onClick={() => {
                          setRemoving(d)
                        }}
                        className={`${buttonClass} text-danger`}
                      >
                        Remove
                      </button>
                    </div>
                  )}
                </li>
              )
            })}
          </ul>
        )
      )}
      {status !== '' && (
        <p role="status" className="text-xs text-muted">
          {status}
        </p>
      )}
      {removing !== null && (
        <ConfirmDialog
          title={`Remove the defaults for ${targetText(removing.owner, removing.name)}?`}
          confirmLabel="Remove defaults"
          onCancel={() => {
            setRemoving(null)
          }}
          onConfirm={() => {
            void remove(removing)
          }}
        >
          {removing.name === ''
            ? `Launches on ${removing.owner}'s repositories go back to the app's settings, except where a repository has its own.`
            : `Launches on ${targetText(removing.owner, removing.name)} go back to its org's defaults and the app's settings.`}
        </ConfirmDialog>
      )}
    </div>
  )
}

interface DefaultsEditorProps {
  editing: Editing
  settings: Settings
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  defaultPrompts: DefaultPrompts | null
  launchDefaults: readonly LaunchDefaults[]
  // onDone closes the editor with a message to show, or '' when cancelled.
  onDone: (message: string) => void
  onChanged: () => Promise<void>
}

// DefaultsEditor is the form for one org's or repository's launch defaults.
function DefaultsEditor({
  editing,
  settings,
  agents,
  profiles,
  prompts,
  defaultPrompts,
  launchDefaults,
  onDone,
  onChanged,
}: DefaultsEditorProps) {
  const id = useId()
  const stored = editing.kind === 'existing' ? findDefaults(launchDefaults, editing.owner, editing.name) : undefined
  const [targetDraft, setTargetDraft] = useState(
    editing.kind === 'existing' ? targetText(editing.owner, editing.name) : '',
  )
  const [input, setInput] = useState<LaunchDefaultsInput>(
    stored === undefined ? emptyDefaults('', '') : defaultsInput(stored),
  )
  // targets are the orgs and repositories offered for new defaults.
  const targets = useQuery(() => listLaunchTargets(), [], {
    enabled: editing.kind === 'new',
    log: 'could not list the repositories',
  })

  const target = editing.kind === 'existing' ? { owner: editing.owner, name: editing.name } : parseTarget(targetDraft)
  const { owner: targetOwner, name: targetName } = target
  // Check the form as it changes, once a target is named.
  const check = useDebouncedQuery(
    () =>
      checkLaunchDefaults({ ...input, owner: targetOwner, name: targetName }).then(
        (list): Record<string, string | undefined> => Object.fromEntries(list.map((f) => [f.field, f.message])),
      ),
    [input, targetOwner, targetName],
    150,
    { enabled: targetOwner !== '', log: 'could not check the launch defaults' },
  )
  // errors are the last check's, once a target is named.
  const errors: Record<string, string | undefined> = targetOwner === '' ? {} : (check.data ?? {})
  const full: LaunchDefaultsInput = { ...input, owner: target.owner, name: target.name }
  const org = target.name === '' ? undefined : findDefaults(launchDefaults, target.owner, '')
  const exists = editing.kind === 'new' && findDefaults(launchDefaults, target.owner, target.name) !== undefined
  const set = (patch: Partial<LaunchDefaultsInput>) => {
    setInput((was) => ({ ...was, ...patch }))
  }
  // Model names hold no spaces, so a typed one is kept trimmed.
  const setOverride = (patch: Partial<LaunchOverrides>) => {
    set(
      patch.models === undefined
        ? patch
        : { ...patch, models: Object.fromEntries(Object.entries(patch.models).map(([k, v]) => [k, v.trim()])) },
    )
  }

  // inherited names what each setting falls back to: the org's defaults for a
  // repository, else the app's settings or the profile's own.
  const inherited = (() => {
    const promptName = (pid: number | null | undefined) => prompts.find((p) => p.id === pid)?.nickname ?? 'none'
    const keys = agents.map((a) => a.id).filter((a): a is Exclude<AgentID, ''> => a !== '')
    const perAgent = (pick: (id: Exclude<AgentID, ''>) => string) => Object.fromEntries(keys.map((k) => [k, pick(k)]))

    return {
      agent:
        org !== undefined && org.agent !== ''
          ? `${agentName(agents, org.agent)} (org)`
          : `${agentName(agents, settings.defaultAgent)} (app)`,
      profiles: perAgent((k) =>
        org?.profiles[k] !== undefined
          ? `${org.profiles[k]} (org)`
          : `${settings.defaultProfiles[k] ?? 'default'} (app)`,
      ),
      pr: org?.prPromptId != null ? `${promptName(org.prPromptId)} (org)` : `${promptName(defaultPrompts?.pr)} (app)`,
      issue:
        org?.issuePromptId != null
          ? `${promptName(org.issuePromptId)} (org)`
          : `${promptName(defaultPrompts?.issue)} (app)`,
      models: perAgent((k) =>
        org?.models[k] !== undefined ? `${modelLabel(org.models[k], modelChoices(agents, k))} (org)` : 'the profile’s',
      ),
      effort:
        org !== undefined && org.effort !== ''
          ? `${effortLabel(org.effort, anyEffortChoices(agents))} (org)`
          : 'the profile’s',
      planMode: org?.planMode != null ? `${org.planMode ? 'on' : 'off'} (org)` : 'the profile’s',
      worktree:
        org?.worktree != null
          ? `${org.worktree ? 'on' : 'off'} (org)`
          : `${settings.worktreeDefault ? 'on' : 'off'} (app)`,
    }
  })()

  const problems = Object.keys(errors).length > 0 || exists || target.owner === ''

  // Save stores the form, loads the launch configuration again, and closes the editor; a
  // refusal stays on the form.
  const [refusal, save, saving] = useActionState(async (_: string, defaults: LaunchDefaultsInput) => {
    const result = await attempt(saveLaunchDefaults(defaults))

    if (!result.ok) {
      return result.error
    }

    await onChanged()
    onDone(`Saved the defaults for ${targetText(defaults.owner, defaults.name)}.`)

    return ''
  }, '')
  const saveError = saving ? '' : refusal

  const prPrompts = prompts.filter((p) => promptApplies(p, 'pr'))
  const issuePrompts = prompts.filter((p) => promptApplies(p, 'issue'))

  return (
    <form
      aria-label={
        editing.kind === 'new' ? 'New launch defaults' : `Launch defaults for ${targetText(target.owner, target.name)}`
      }
      onSubmit={(e) => {
        e.preventDefault()

        if (!problems && !saving) {
          startTransition(() => {
            save(full)
          })
        }
      }}
      className="flex flex-col gap-4 rounded-lg border border-accent/40 bg-surface p-4 shadow-sm"
    >
      {editing.kind === 'new' ? (
        <Field
          label="Defaults for"
          controlId={`${id}-target`}
          hint="An org, such as acme-org, for all its repositories, or one repository, such as acme-org/api."
          error={exists ? 'It already has defaults; edit them in the list.' : (errors.owner ?? errors.name)}
        >
          <input
            id={`${id}-target`}
            list={`${id}-targets`}
            value={targetDraft}
            placeholder="acme-org or acme-org/api"
            spellCheck={false}
            autoComplete="off"
            aria-invalid={exists || errors.owner !== undefined || errors.name !== undefined}
            aria-describedby={describedBy(`${id}-target`, exists ? 'x' : (errors.owner ?? errors.name), true)}
            onChange={(e) => {
              setTargetDraft(e.target.value)
            }}
            className={`${fieldClass} w-full font-mono text-[13px]`}
          />
          <datalist id={`${id}-targets`}>
            {(targets.data ?? noTargets).map((t) => (
              <option key={targetText(t.owner, t.name)} value={targetText(t.owner, t.name)}>
                {t.name === '' ? 'All repositories' : `${String(t.items)} in the inbox`}
              </option>
            ))}
          </datalist>
        </Field>
      ) : (
        <p className="text-sm font-semibold">
          {targetText(target.owner, target.name)}
          <span className="ml-2 text-xs font-normal text-muted">
            {target.name === '' ? 'All repositories' : 'Repository'}
          </span>
        </p>
      )}

      <div className="grid grid-cols-2 gap-x-5 gap-y-4">
        <Field label="Agent" controlId={`${id}-agent`} error={errors.agent}>
          <select
            id={`${id}-agent`}
            value={input.agent}
            aria-describedby={describedBy(`${id}-agent`, errors.agent, false)}
            onChange={(e) => {
              set({ agent: agents.find((a) => a.id === e.target.value)?.id ?? '' })
            }}
            className={`${fieldClass} w-full`}
          >
            <option value="">Inherit · {inherited.agent}</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </Field>
        <div />
        {agents.map((a) => {
          const key = a.id as Exclude<AgentID, ''>
          const error = errors[`profiles.${key}`] ?? errors.profiles

          return (
            <Field key={a.id} label={`${a.name} profile`} controlId={`${id}-profile-${key}`} error={error}>
              <select
                id={`${id}-profile-${key}`}
                value={input.profiles[key] ?? ''}
                aria-describedby={describedBy(`${id}-profile-${key}`, error, false)}
                onChange={(e) => {
                  const value = e.target.value
                  // Inherit leaves the agent out, rather than storing an empty name.
                  const others = Object.fromEntries(Object.entries(input.profiles).filter(([k]) => k !== key))

                  set({ profiles: value === '' ? others : { ...others, [key]: value } })
                }}
                className={`${fieldClass} w-full`}
              >
                <option value="">Inherit · {inherited.profiles[key]}</option>
                {profiles
                  .filter((p) => p.agent === a.id)
                  .map((p) => (
                    <option key={p.name} value={p.name}>
                      {p.name}
                    </option>
                  ))}
              </select>
            </Field>
          )
        })}
        {agents.length % 2 === 1 && <div />}
        <Field label="Prompt for pull requests" controlId={`${id}-pr`} error={errors.prPromptId}>
          <select
            id={`${id}-pr`}
            value={input.prPromptId ?? 0}
            aria-describedby={describedBy(`${id}-pr`, errors.prPromptId, false)}
            onChange={(e) => {
              const v = Number(e.target.value)

              set({ prPromptId: v === 0 ? null : v })
            }}
            className={`${fieldClass} w-full`}
          >
            <option value={0}>Inherit · {inherited.pr}</option>
            {prPrompts.map((p) => (
              <option key={p.id} value={p.id}>
                {p.nickname}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Prompt for issues" controlId={`${id}-issue`} error={errors.issuePromptId}>
          <select
            id={`${id}-issue`}
            value={input.issuePromptId ?? 0}
            aria-describedby={describedBy(`${id}-issue`, errors.issuePromptId, false)}
            onChange={(e) => {
              const v = Number(e.target.value)

              set({ issuePromptId: v === 0 ? null : v })
            }}
            className={`${fieldClass} w-full`}
          >
            <option value={0}>Inherit · {inherited.issue}</option>
            {issuePrompts.map((p) => (
              <option key={p.id} value={p.id}>
                {p.nickname}
              </option>
            ))}
          </select>
        </Field>
        <LaunchOverrideFields
          id={id}
          agents={agents}
          values={input}
          onChange={setOverride}
          errors={errors}
          inherited={inherited}
          flagsHint={
            target.name === ''
              ? 'Added after the profile’s flags, such as --add-dir /srv/shared.'
              : 'Added after the profile’s and the org’s flags.'
          }
        />
      </div>

      <div className="flex items-center justify-end gap-2">
        <ErrorText error={saveError} className="mr-auto" />
        <button
          type="button"
          onClick={() => {
            onDone('')
          }}
          className={buttonClass}
        >
          Cancel
        </button>
        <button type="submit" disabled={problems || saving} className={primaryButtonClass}>
          {saving ? 'Saving…' : 'Save defaults'}
        </button>
      </div>
    </form>
  )
}
