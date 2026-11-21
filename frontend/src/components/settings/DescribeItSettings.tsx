import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { attempt } from '../../lib/call'
import { agentOf, effortLabel, modelChoices, modelLabel } from '../../lib/launch'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import {
  describeItAgent,
  describeItPlugins,
  installDescribeItPlugin,
  removeDescribeItPlugin,
  setDescribeItAgent,
  type AgentInfo,
  type Choice,
  type DescribeAgentChoice,
  type DescribePluginState,
  type Profile,
} from '../../platform'
import { buttonClass, ErrorText } from '../controls'
import { rowMatchClass, useSetting } from './match'
import { Group, rowDescribedBy, SectionIntro } from './rows'

// agentLabel and pluginLabel name the group's two rows, as "Find a setting" finds them.
const agentLabel = 'Describe it’s agent'
export const pluginLabel = `Use ${brand.name} in your own Claude Code`

// pluginHelp says what the plugin does, and how much less a session in the user's own
// Claude Code sees than Describe it in the app, and why.
const pluginHelp =
  'With it, a session in your own Claude Code can propose routines, prompts, and settings, which wait in the Autopilot tab for your review. It sees less than Describe it here: none of your inbox’s names, and of your setup only your routines’ names and sentences, your prompts’ nicknames, and your settings. It asks before each of its tool calls, since it may run in a repository you don’t trust.'

// inheritAll is the agent with every part Inherit, as a new conversation's starts.
const inheritAll: DescribeAgentChoice = { profile: '', model: '', effort: '' }

// withValue lists choices with value among them, so a stored choice the list no longer
// holds, such as a profile since removed or a model typed elsewhere, still shows.
function withValue(choices: readonly Choice[], value: string): readonly Choice[] {
  return value === '' || choices.some((c) => c.value === value) ? choices : [...choices, { value, label: value }]
}

// installedText says whether the plugin is installed in a profile, with its version.
export function installedText(state: Pick<DescribePluginState, 'installed' | 'version'>): string {
  if (!state.installed) {
    return 'Not installed'
  }

  return state.version === '' ? 'Installed' : `Installed · ${state.version}`
}

interface AgentRowProps {
  // defaultProfile is the launch defaults' Claude Code profile, which Inherit takes.
  defaultProfile: string
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
}

// AgentRow is the agent Describe it's new conversations start with: Claude Code's profile,
// model, and effort, each Inherit until chosen, which says what it comes to: the launch
// defaults' profile, and that profile's own model and effort. A choice shows at once and
// is stored as made; a refused one goes back, saying why.
function AgentRow({ defaultProfile, agents, profiles }: AgentRowProps) {
  const id = useId()
  const [marks] = useSetting(agentLabel)
  const stored = useQuery(() => describeItAgent(), [])
  const saving = useMutation((choice: DescribeAgentChoice) => setDescribeItAgent(choice))
  // choice is the agent as stored, every part Inherit until it is read, and held keeps the
  // choices where they are until then, and while one is stored.
  const choice = stored.data ?? inheritAll
  const held = stored.data === undefined || saving.pending
  const own = profiles.filter((p) => p.agent === 'claude')
  const profile = own.find((p) => p.name === (choice.profile === '' ? defaultProfile : choice.profile))
  const models = modelChoices(agents, 'claude')
  const efforts = agentOf(agents, 'claude')?.effortLevels ?? []
  const error = stored.error === '' ? saving.error : stored.error

  const change = (key: keyof DescribeAgentChoice, value: string) => {
    const next: DescribeAgentChoice = { ...choice, [key]: value }

    stored.set(() => next)
    void saving.run(next).then((result) => {
      stored.set(() => (result.ok ? result.data : choice))
    })
  }

  // field is one of the three choices: its label, what Inherit comes to, and the rest.
  const field = (key: keyof DescribeAgentChoice, label: string, inherited: string, choices: readonly Choice[]) => {
    const value = choice[key]

    return (
      <>
        <label htmlFor={`${id}-${key}`} className="text-xs font-medium text-muted">
          {label}
        </label>
        <select
          id={`${id}-${key}`}
          value={value}
          disabled={held}
          aria-describedby={rowDescribedBy(id, error)}
          onChange={(e) => {
            change(key, e.target.value)
          }}
          className="min-w-0 rounded-md border border-border bg-surface px-2 py-1 text-sm disabled:opacity-disabled"
        >
          <option value="">Inherit · {inherited}</option>
          {withValue(choices, value).map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </>
    )
  }

  return (
    <div {...marks} className={`flex items-start justify-between gap-8 px-4 py-3.5 ${rowMatchClass}`}>
      <div className="min-w-0">
        <p id={`${id}-label`} className="text-sm font-medium">
          {agentLabel}
        </p>
        <p id={`${id}-help`} className="mt-0.5 text-xs text-muted">
          The Claude Code profile, model, and effort a new conversation starts with. Inherit takes the launch defaults’
          profile, and that profile’s own model and effort.
        </p>
        <ErrorText id={`${id}-error`} error={error} className="mt-1" />
      </div>
      <div
        role="group"
        aria-labelledby={`${id}-label`}
        className="grid shrink-0 grid-cols-[auto_minmax(0,13rem)] items-center gap-x-2 gap-y-1.5"
      >
        {field(
          'profile',
          'Profile',
          defaultProfile,
          own.map((p) => ({ value: p.name, label: p.name })),
        )}
        {field('model', 'Model', modelLabel(profile?.model ?? '', models), models)}
        {field('effort', 'Effort', effortLabel(profile?.effort ?? '', efforts), efforts)}
      </div>
    </div>
  )
}

// PluginRow is the plugin for the user's own Claude Code, one line per Claude Code profile:
// whether it is installed there, with its version, Install or Remove, and why it can't be,
// when it can't. Install and Remove each answer with every profile's state, which the
// lines show; one runs at a time.
function PluginRow() {
  const id = useId()
  const [marks] = useSetting(pluginLabel)
  const states = useQuery(() => describeItPlugins(), [])
  // busy is the profile whose plugin is being installed or removed, '' for none, and
  // failed the profile whose last change was refused, and why.
  const [busy, setBusy] = useState('')
  const [failed, setFailed] = useState<{ profile: string; error: string } | null>(null)

  const act = async (profile: string, install: boolean) => {
    setBusy(profile)
    setFailed(null)

    const result = await attempt(install ? installDescribeItPlugin(profile) : removeDescribeItPlugin(profile))

    setBusy('')

    if (result.ok) {
      states.set(() => result.data)
    } else {
      setFailed({ profile, error: result.error })
    }
  }

  return (
    <div {...marks} className={`flex flex-col gap-3 px-4 py-3.5 ${rowMatchClass}`}>
      <div className="min-w-0">
        <p id={`${id}-label`} className="text-sm font-medium">
          {pluginLabel}
        </p>
        <p className="mt-0.5 text-xs text-muted">{pluginHelp}</p>
      </div>
      {states.error !== '' ? (
        <ErrorText error={states.error} />
      ) : states.data === undefined ? (
        <p className="text-xs text-muted">Reading Claude Code’s plugins…</p>
      ) : states.data.length === 0 ? (
        <p className="text-xs text-muted">No Claude Code profile was found.</p>
      ) : (
        <ul aria-labelledby={`${id}-label`} className="divide-y divide-border rounded-md border border-border">
          {states.data.map((p, i) => {
            const lineId = `${id}-${String(i)}`
            const verb = p.installed ? 'Remove' : 'Install'
            const working = p.installed ? 'Removing…' : 'Installing…'

            return (
              <li key={p.profile} className="flex items-center gap-4 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <p id={`${lineId}-name`} className="truncate text-sm font-medium">
                    {p.profile}
                  </p>
                  <p id={`${lineId}-state`} className="text-xs text-muted">
                    {installedText(p)}
                  </p>
                  {p.problem !== '' && <p className="text-xs text-warning">{p.problem}</p>}
                  {failed?.profile === p.profile && <ErrorText error={failed.error} />}
                </div>
                <button
                  type="button"
                  disabled={busy !== '' || (!p.installed && p.problem !== '')}
                  aria-describedby={`${lineId}-name ${lineId}-state`}
                  title={
                    p.installed
                      ? 'Uninstall the plugin from this profile’s Claude Code'
                      : 'Install the plugin in this profile’s Claude Code'
                  }
                  onClick={() => {
                    void act(p.profile, !p.installed)
                  }}
                  className={`${buttonClass} shrink-0`}
                >
                  {busy === p.profile ? working : verb}
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

export interface DescribeItSettingsProps {
  // defaultProfile is the launch defaults' Claude Code profile, which Inherit takes.
  defaultProfile: string
  // agents and profiles are the coding agents and their profiles, of which Describe it
  // runs Claude Code's.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
}

// DescribeItSettings is Settings → Autopilot's Claude Code: the agent Describe it's
// conversations start with, and the plugin that lets the user's own Claude Code propose
// routines too.
export function DescribeItSettings({ defaultProfile, agents, profiles }: DescribeItSettingsProps) {
  return (
    <div className="flex flex-col gap-3">
      <SectionIntro title="Claude Code">
        Describe it, in the Autopilot tab, runs Claude Code to turn what you say into routines, prompts, and settings,
        which change nothing until you apply them.
      </SectionIntro>
      <Group>
        <AgentRow defaultProfile={defaultProfile} agents={agents} profiles={profiles} />
        <PluginRow />
      </Group>
    </div>
  )
}
