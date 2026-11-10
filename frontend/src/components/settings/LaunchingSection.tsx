import { useId, useState } from 'react'

import { attempt } from '../../lib/call'
import { kindPlural, promptApplies } from '../../lib/launch'
import { terminals } from '../../lib/terminals'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  setDefaultAgent,
  setDefaultProfile,
  setCodexEffort,
  setCodexFast,
  setDefaultPrompt,
  setFollowSessions,
  setTerminal,
  setWorktreeDefault,
  type AgentID,
  type AgentInfo,
  type DefaultPrompts,
  type Profile,
  type Prompt,
  type Settings,
} from '../../platform'
import { fieldClass } from '../controls'
import { Group, rowDescribedBy, SettingRow, SwitchRow } from './rows'
import type { SectionProps } from './types'

interface LaunchingSectionProps extends SectionProps {
  // agents are the coding agents, each with whether it is installed.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // defaultPrompts is null until it has loaded.
  defaultPrompts: DefaultPrompts | null
  // onLaunchChanged reloads the prompts and launch defaults after a change; it resolves
  // once they are in.
  onLaunchChanged: () => Promise<void>
}

// LaunchingSection holds the app defaults every launch starts from: the terminal, the
// coding agent and each agent's profile, the prompt for each kind of item, and whether a
// session runs in a worktree. Each is saved as soon as it changes, with why a change was
// refused shown under its row.
export function LaunchingSection({
  settings: stored,
  agents,
  profiles,
  prompts,
  defaultPrompts,
  onSaved,
  onLaunchChanged,
}: LaunchingSectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [errors, setErrors] = useState<Record<string, string>>({})

  // shows sets the message under one field: why its change was refused, or ''.
  const shows = (field: string, message: string) => {
    setErrors((was) => ({ ...was, [field]: message }))
  }

  // errorOf is the message under one field, or ''.
  const errorOf = (field: string) => errors[field] ?? ''

  // store saves one change, showing why it was refused under its field.
  const store = async (field: string, change: Partial<Settings>, call: () => PromiseLike<Settings>) => {
    shows(field, '')

    const result = await save(change, call)

    if (!result.ok) {
      shows(field, result.error)
    }
  }

  // savePrompt makes a prompt the app's default for a kind of item, then loads the
  // prompts again, since which one is the default shows in the library.
  const savePrompt = async (kind: 'pr' | 'issue', promptId: number) => {
    shows(kind, '')

    const result = await attempt(setDefaultPrompt(kind, promptId))

    if (result.ok) {
      await onLaunchChanged()
    } else {
      shows(kind, result.error)
    }
  }

  return (
    <Group>
      <SettingRow
        label="Terminal"
        controlId={`${id}-terminal`}
        description="The app new sessions open in."
        error={errorOf('terminal')}
      >
        <select
          id={`${id}-terminal`}
          aria-describedby={rowDescribedBy(`${id}-terminal`, errorOf('terminal'))}
          value={settings.terminal}
          onChange={(e) => {
            const terminal = terminals.find((t) => t.id === e.target.value)?.id

            if (terminal !== undefined) {
              void store('terminal', { terminal }, () => setTerminal(terminal))
            }
          }}
          className={fieldClass}
        >
          {terminals.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
        </select>
      </SettingRow>
      <SettingRow
        label="Default agent"
        controlId={`${id}-agent`}
        description="The coding agent a launch starts, unless an org or repository chooses another. Each launch can switch."
        error={errorOf('agent')}
      >
        <select
          id={`${id}-agent`}
          aria-describedby={rowDescribedBy(`${id}-agent`, errorOf('agent'))}
          value={settings.defaultAgent}
          onChange={(e) => {
            const defaultAgent = agents.find((a) => a.id === e.target.value)?.id

            if (defaultAgent !== undefined) {
              void store('agent', { defaultAgent }, () => setDefaultAgent(defaultAgent))
            }
          }}
          className={fieldClass}
        >
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.installed ? a.name : `${a.name} (not installed)`}
            </option>
          ))}
        </select>
      </SettingRow>
      {agents.map((a) => {
        const key = a.id as Exclude<AgentID, ''>
        const own = profiles.filter((p) => p.agent === a.id)
        const field = `profile-${key}`

        return (
          <SettingRow
            key={a.id}
            label={`${a.name} profile`}
            controlId={`${id}-${field}`}
            description={
              own.length === 0
                ? `No ${a.name} profiles were found: run ${a.command} once so it makes ~/${a.configDir}.`
                : `The ${a.name} profile a launch uses, unless an org or repository chooses another.`
            }
            error={errorOf(field)}
          >
            <select
              id={`${id}-${field}`}
              aria-describedby={rowDescribedBy(`${id}-${field}`, errorOf(field))}
              value={settings.defaultProfiles[key] ?? ''}
              disabled={own.length === 0}
              onChange={(e) => {
                const name = e.target.value
                const defaultProfiles = { ...settings.defaultProfiles, [key]: name }

                void store(field, { defaultProfiles }, () => setDefaultProfile(key, name))
              }}
              className={fieldClass}
            >
              {own.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.name}
                </option>
              ))}
            </select>
          </SettingRow>
        )
      })}
      {(['pr', 'issue'] as const).map((kind) => (
        <SettingRow
          key={kind}
          label={kind === 'pr' ? 'Prompt for pull requests' : 'Prompt for issues'}
          controlId={`${id}-prompt-${kind}`}
          description={`What a launch on ${kindPlural(kind)} asks, unless an org or repository chooses another. Edit prompts on the Prompts tab.`}
          error={errorOf(kind)}
        >
          <select
            id={`${id}-prompt-${kind}`}
            aria-describedby={rowDescribedBy(`${id}-prompt-${kind}`, errorOf(kind))}
            value={defaultPrompts === null ? 0 : defaultPrompts[kind]}
            disabled={defaultPrompts === null}
            onChange={(e) => {
              void savePrompt(kind, Number(e.target.value))
            }}
            className={fieldClass}
          >
            {prompts
              .filter((p) => promptApplies(p, kind))
              .map((p) => (
                <option key={p.id} value={p.id}>
                  {p.nickname}
                </option>
              ))}
          </select>
        </SettingRow>
      ))}
      <SwitchRow
        label="Start in a worktree"
        controlId={`${id}-worktree`}
        description="Presets each row's “Start in a worktree” box. You can still change it per launch. How worktrees are cleaned up is under Worktrees."
        error={errorOf('worktree')}
        checked={settings.worktreeDefault}
        onChange={(worktreeDefault) => {
          void store('worktree', { worktreeDefault }, () => setWorktreeDefault(worktreeDefault))
        }}
      />
      <SwitchRow
        label="Follow agent sessions"
        controlId={`${id}-follow`}
        description="Each item's card shows what its Claude Code or Codex session is doing (working, waiting for you, or done) and what it last said, read from the agent on this Mac. Turn it off and nothing asks the agents; launches are still remembered."
        error={errorOf('follow')}
        checked={settings.followSessions}
        onChange={(followSessions) => {
          void store('follow', { followSessions }, () => setFollowSessions(followSessions))
        }}
      />
      <SwitchRow
        label="Set Codex’s effort at launch"
        controlId={`${id}-codex-effort`}
        description="Passes Codex the effort a launch resolves to. Codex takes it only as a config override (-c), and runs a session started with one outside its app server, so the card can’t show when it waits for you. Off, Codex uses the effort in its own config, which you change in a session with /model."
        error={errorOf('codexEffort')}
        checked={settings.codexEffort}
        onChange={(codexEffort) => {
          void store('codexEffort', { codexEffort }, () => setCodexEffort(codexEffort))
        }}
      />
      <SwitchRow
        label="Start Codex in Fast mode"
        controlId={`${id}-codex-fast`}
        description="Presets each Codex launch’s Fast mode, which you can still change per launch. It is a config override too, with the same cost: the card can’t show when the session waits for you. Off, turn it on in a session with /fast."
        error={errorOf('codexFast')}
        checked={settings.codexFast}
        onChange={(codexFast) => {
          void store('codexFast', { codexFast }, () => setCodexFast(codexFast))
        }}
      />
    </Group>
  )
}
