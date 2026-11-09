import { useId } from 'react'

import { agentOf, effortLabel, modelLabel, planToggleLabel, promptApplies, sourceLabel } from '../lib/launch'
import { withAgent, type Choices, type SaveStatus } from '../lib/launchChoices'
import type { AgentInfo, InboxItem, LaunchSetting, Profile, Prompt, Resolution } from '../platform'
import { primaryButtonClass, quietButtonClass } from './controls'
import { AlertIcon, TerminalIcon } from './icons'
import { LaunchPreviewSection } from './LaunchPreviewSection'

export interface LaunchMenuProps {
  item: InboxItem
  // resolved is what the item's settings resolve to with the chosen prompt and profile;
  // its inherited values are what each setting shows as its default.
  resolved: Resolution
  choices: Choices
  onChoices: (c: Choices) => void
  // agents are the coding agents a launch can start, each with whether it is installed.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // agentName names the coding agent the launch starts, as in "Claude".
  agentName: string
  onLaunch: () => void
  // onSave saves the choices as defaults for the repository, or for its whole owner.
  onSave: (scope: 'repo' | 'org') => void
  saving: boolean
  // status reports the last save, or null.
  status: SaveStatus | null
  // resolveError says why the settings under the chosen prompt and profile could not be
  // worked out, or ''.
  resolveError: string
  onManagePrompts: () => void
}

const sectionTitle = 'px-2 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase'
const overrideHelp = "Lifts the outside-contributor restriction; keeps the profile's plan and permission settings."

// defaultLabel names a setting's default: its value and where it comes from.
function defaultLabel(s: LaunchSetting, label: (v: string) => string): string {
  if (s.inheritedSource === 'agent') {
    return 'Default (the agent’s)'
  }

  return `Default · ${label(s.inherited)} (${sourceLabel(s.inheritedSource)})`
}

// LaunchMenu is the launch button's panel: the coding agent, the prompt, the agent's
// profile, its model and effort, the worktree, and plan mode for this launch, a preview of
// what it runs, and buttons to save the choices as defaults or to launch. Every choice
// starts at what the item's settings resolve to, each showing where that comes from; the
// profiles, models, and effort levels offered are the chosen agent's.
export function LaunchMenu({
  item,
  resolved,
  choices,
  onChoices,
  agents,
  profiles,
  prompts,
  agentName,
  onLaunch,
  onSave,
  saving,
  status,
  resolveError,
  onManagePrompts,
}: LaunchMenuProps) {
  const id = useId()
  const untrusted = !item.trusted
  // agent is the agent this launch starts, as resolved under the choices, with the models
  // its cache lists when the app read one.
  const agent = agentOf(agents, resolved.agent.id) ?? { ...resolved.agent, installed: true, path: '' }
  const agentProfiles = profiles.filter((p) => p.agent === agent.id)
  const agentId = choices.agent === '' ? resolved.agentInherited : choices.agent
  const applicable = prompts.filter((p) => promptApplies(p, item.kind))
  const promptId = choices.promptId === 0 ? resolved.prompt.id : choices.promptId
  const profile = choices.profile === '' ? resolved.profile.value : choices.profile
  const worktree = choices.worktree ?? resolved.worktree.inherited
  const planMode = choices.planMode ?? resolved.planMode.inherited
  const fast = choices.fast ?? resolved.fast.inherited
  // forcedPlan: an outside contributor's item starts in plan mode until the override lifts it.
  const forcedPlan = untrusted && !choices.override
  const [owner, name] = [resolved.owner, resolved.repo.split('/').at(-1) ?? '']
  const set = (patch: Partial<Choices>) => {
    onChoices({ ...choices, ...patch })
  }

  return (
    <div className="flex flex-col text-sm">
      {agents.length > 1 && (
        <div className="px-1.5 pt-2">
          <p id={`${id}-agent-title`} className={sectionTitle}>
            Agent
          </p>
          <div role="radiogroup" aria-labelledby={`${id}-agent-title`} className="flex gap-1.5 px-2 pb-1">
            {agents.map((a) => (
              <label
                key={a.id}
                title={
                  !a.installed
                    ? `${a.name} isn’t installed (${a.command} wasn’t found)`
                    : a.id === resolved.agentInherited
                      ? sourceLabel(resolved.agentInheritedSource)
                      : a.path
                }
                className="flex-1 cursor-pointer rounded-md border border-border px-2.5 py-1 text-center text-[0.8125rem] text-muted transition-colors select-none hover:text-text has-checked:border-accent has-checked:bg-accent/10 has-checked:text-text has-focus-visible:outline-2 has-focus-visible:outline-accent has-disabled:cursor-not-allowed has-disabled:opacity-50"
              >
                <input
                  type="radio"
                  name={`${id}-agent`}
                  value={a.id}
                  checked={agentId === a.id}
                  disabled={!a.installed}
                  onChange={() => {
                    onChoices(withAgent(choices, a.id === resolved.agentInherited ? '' : a.id))
                  }}
                  className="sr-only"
                />
                {a.name}
              </label>
            ))}
          </div>
        </div>
      )}

      <div className={`px-1.5 pt-2 ${agents.length > 1 ? 'mt-1 border-t border-border' : ''}`}>
        <div className="flex items-baseline justify-between">
          <p id={`${id}-prompt-title`} className={sectionTitle}>
            Prompt
          </p>
          <button type="button" onClick={onManagePrompts} className={quietButtonClass}>
            Manage
          </button>
        </div>
        {/* The menu opens with focus on the chosen prompt, the choice most often changed. */}
        <div
          role="radiogroup"
          aria-labelledby={`${id}-prompt-title`}
          data-initial-focus
          className="max-h-48 overflow-y-auto"
        >
          {applicable.map((p) => (
            <label
              key={p.id}
              className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)_auto] items-baseline gap-x-2 rounded-md px-2 py-1 hover:bg-raised has-checked:bg-raised/70"
            >
              <input
                type="radio"
                name={`${id}-prompt`}
                value={p.id}
                checked={promptId === p.id}
                onChange={() => {
                  set({ promptId: p.id === resolved.prompt.inheritedId ? 0 : p.id })
                }}
                className="radio row-span-2 translate-y-0.5"
              />
              <span className="truncate font-medium">{p.nickname}</span>
              <span className="text-[0.6875rem] text-muted">
                {p.id === resolved.prompt.inheritedId ? sourceLabel(resolved.prompt.inheritedSource) : ''}
              </span>
              <span className="col-span-2 truncate text-xs text-muted">{p.name}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="mt-1 border-t border-border px-1.5 pt-2">
        <p id={`${id}-profile-title`} className={sectionTitle}>
          Profile
        </p>
        <div role="radiogroup" aria-labelledby={`${id}-profile-title`} className="flex flex-wrap gap-1.5 px-2 pb-1">
          {agentProfiles.length === 0 && (
            <p className="text-xs text-muted">{`No ${agent.name} profiles: run ${agent.command} once to make ~/${agent.configDir}.`}</p>
          )}
          {agentProfiles.map((p) => (
            <label
              key={p.name}
              title={
                !p.signedIn
                  ? `Not signed in: run ${p.configDir === '' ? '' : `${agent.configEnv}=${p.folder} `}${agent.command} login`
                  : p.name === resolved.profile.inherited
                    ? sourceLabel(resolved.profile.inheritedSource)
                    : p.folder
              }
              className="cursor-pointer rounded-full border border-border px-2.5 py-0.5 text-[0.8125rem] text-muted transition-colors select-none hover:text-text has-checked:border-accent has-checked:bg-accent/10 has-checked:text-text has-focus-visible:outline-2 has-focus-visible:outline-accent"
            >
              <input
                type="radio"
                name={`${id}-profile`}
                value={p.name}
                checked={profile === p.name}
                onChange={() => {
                  set({ profile: p.name === resolved.profile.inherited ? '' : p.name })
                }}
                className="sr-only"
              />
              {p.name}
              {!p.signedIn && <span className="ml-1 text-[0.6875rem] text-warning">· not signed in</span>}
            </label>
          ))}
        </div>
      </div>

      <div className="mt-1 flex flex-col gap-2 border-t border-border px-3.5 pt-3 pb-2">
        <div className="grid grid-cols-[4rem_minmax(0,1fr)] items-center gap-x-2 gap-y-2">
          <label htmlFor={`${id}-model`} className="text-xs text-muted">
            Model
          </label>
          <select
            id={`${id}-model`}
            value={choices.model ?? ''}
            onChange={(e) => {
              const value = e.target.value

              set({ model: value === '' || value === resolved.model.inherited ? null : value })
            }}
            className="w-full truncate rounded-md border border-border bg-surface px-2 py-1 text-[0.8125rem]"
          >
            <option value="">{defaultLabel(resolved.model, (v) => modelLabel(v, agent.models))}</option>
            {choices.model !== null && !agent.models.some((c) => c.value === choices.model) && (
              <option value={choices.model}>{choices.model}</option>
            )}
            {agent.models.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
          <label htmlFor={`${id}-effort`} className={`text-xs text-muted ${resolved.effortOff ? 'opacity-60' : ''}`}>
            Effort
          </label>
          {resolved.effortOff ? (
            <select
              id={`${id}-effort`}
              value=""
              disabled
              aria-describedby={`${id}-effort-help`}
              className="w-full truncate rounded-md border border-border bg-surface px-2 py-1 text-[0.8125rem] disabled:cursor-not-allowed disabled:opacity-60"
            >
              <option value="">{agent.name}’s own</option>
            </select>
          ) : (
            <select
              id={`${id}-effort`}
              value={choices.effort ?? ''}
              onChange={(e) => {
                const value = e.target.value

                set({ effort: value === '' || value === resolved.effort.inherited ? null : value })
              }}
              className="w-full truncate rounded-md border border-border bg-surface px-2 py-1 text-[0.8125rem]"
            >
              <option value="">{defaultLabel(resolved.effort, (v) => effortLabel(v, agent.effortLevels))}</option>
              {agent.effortLevels.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          )}
        </div>
        {resolved.effortOff && (
          <p id={`${id}-effort-help`} className="-mt-1 text-xs text-muted">
            {agent.name} uses the effort in its own config; change it in the session with /model. To set it here, turn
            on Settings → Launching → Set Codex’s effort at launch.
          </p>
        )}
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            checked={worktree}
            onChange={(e) => {
              set({ worktree: e.target.checked === resolved.worktree.inherited ? null : e.target.checked })
            }}
            className="checkbox"
          />
          Start in a worktree
          <span className="ml-auto text-[0.6875rem] text-muted">
            {choices.worktree === null ? sourceLabel(resolved.worktree.inheritedSource) : 'this launch'}
          </span>
        </label>
        <label className={`flex items-center gap-2 ${forcedPlan ? '' : 'cursor-pointer'}`}>
          <input
            type="checkbox"
            checked={forcedPlan || planMode}
            disabled={forcedPlan}
            onChange={(e) => {
              set({ planMode: e.target.checked === resolved.planMode.inherited ? null : e.target.checked })
            }}
            className="checkbox"
          />
          <span className={forcedPlan ? 'text-muted' : ''}>{planToggleLabel(agent)}</span>
          <span className="ml-auto text-[0.6875rem] text-muted">
            {forcedPlan
              ? 'outside contributor'
              : choices.planMode === null
                ? sourceLabel(resolved.planMode.inheritedSource)
                : 'this launch'}
          </span>
        </label>
        {agent.fastMode && (
          <div>
            <label className="flex cursor-pointer items-center gap-2">
              <input
                type="checkbox"
                checked={fast}
                aria-describedby={fast ? `${id}-fast-help` : undefined}
                onChange={(e) => {
                  set({ fast: e.target.checked === resolved.fast.inherited ? null : e.target.checked })
                }}
                className="checkbox"
              />
              Fast mode
              <span className="ml-auto text-[0.6875rem] text-muted">
                {choices.fast === null ? sourceLabel(resolved.fast.inheritedSource) : 'this launch'}
              </span>
            </label>
            {fast && (
              <p id={`${id}-fast-help`} className="pt-0.5 pl-6 text-xs text-muted">
                {agent.name} runs a session started in Fast mode outside its app server, so its card can’t show when it
                waits for you.
              </p>
            )}
          </div>
        )}
        {untrusted && (
          <div>
            <label className="flex cursor-pointer items-center gap-2">
              <input
                type="checkbox"
                checked={choices.override}
                aria-describedby={`${id}-override-help`}
                onChange={(e) => {
                  set({ override: e.target.checked })
                }}
                className="checkbox"
              />
              Use profile permissions for this item
            </label>
            <p id={`${id}-override-help`} className="pt-0.5 pl-6 text-xs text-muted">
              {overrideHelp}
            </p>
          </div>
        )}
        {[
          ...resolved.warnings,
          ...(resolveError === '' ? [] : [`Couldn’t work out the settings for this choice: ${resolveError}`]),
        ].map((w) => (
          <p key={w} className="flex items-start gap-1.5 text-xs text-warning">
            <AlertIcon className="mt-px size-3.5 shrink-0" />
            {w}
          </p>
        ))}
      </div>

      <LaunchPreviewSection item={item} choices={choices} untrusted={untrusted} />

      <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
        <div className="mr-auto flex min-w-0 flex-col">
          <span className="text-[0.6875rem] text-muted">Save these choices as the default for</span>
          <span className="flex gap-1">
            <button
              type="button"
              disabled={saving}
              onClick={() => {
                onSave('repo')
              }}
              title={`Every launch on ${resolved.repo}`}
              className={`${quietButtonClass} -ml-1.5 max-w-36 truncate text-text`}
            >
              {name}
            </button>
            <button
              type="button"
              disabled={saving}
              onClick={() => {
                onSave('org')
              }}
              title={`Every launch on ${owner}'s repositories`}
              className={`${quietButtonClass} max-w-36 truncate text-text`}
            >
              all of {owner}
            </button>
          </span>
          {status !== null && (
            <span
              role={status.tone === 'error' ? 'alert' : 'status'}
              className={`text-[0.6875rem] leading-snug ${status.tone === 'error' ? 'text-danger' : status.tone === 'warning' ? 'text-warning' : 'text-success'}`}
            >
              {status.text}
            </span>
          )}
        </div>
        <button type="button" onClick={onLaunch} className={`${primaryButtonClass} shrink-0`}>
          <TerminalIcon className="size-4" />
          Open in {agentName}
        </button>
      </div>
    </div>
  )
}
