import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import { brand } from '../../generated/brand'
import { useMutation } from '../../lib/useMutation'
import { loginCommand } from '../../lib/profiles'
import { agentRepos } from '../../lib/setup'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  openExternal,
  rediscoverProfiles,
  setDefaultAgent,
  setDefaultProfile,
  setTerminal,
  setWorktreeDefault,
  type AgentInfo,
  type AgentVersion,
  type Profile,
  type Settings,
  type TerminalChoice,
  type TerminalID,
} from '../../platform'
import { CommandLine } from '../CommandLine'
import { buttonClass, chipClass, ErrorText, fieldClass, Switch } from '../controls'
import { CircleCheckIcon, ExternalIcon, RefreshIcon, TerminalIcon } from '../icons'

// switchedAgent says the default agent moved to one that is installed, or ''.
function switchedAgent(from: AgentInfo | undefined, to: AgentInfo): string {
  return `${from?.name ?? 'The default agent'} isn’t installed, so launches start ${to.name} instead.`
}

interface AgentCardProps {
  agent: AgentInfo
  version: string
  profiles: readonly Profile[]
  isDefault: boolean
  defaultProfile: string
  groupName: string
  onDefault: () => void
  onProfile: (name: string) => void
}

// AgentCard is one coding agent: its version, or that it isn't found with where to get
// it; its profiles, with the command that signs one in; whether launches start it; and,
// for an agent with more than one profile, which one they run with.
function AgentCard({
  agent,
  version,
  profiles,
  isDefault,
  defaultProfile,
  groupName,
  onDefault,
  onProfile,
}: AgentCardProps) {
  const id = useId()
  const repo = agentRepos[agent.id] ?? ''
  const signedOut = profiles.filter((p) => !p.signedIn)

  return (
    <section
      aria-label={agent.name}
      className={`flex flex-col gap-3 rounded-lg border px-4 py-4 transition-colors ${
        isDefault ? 'border-accent/60 bg-accent/5' : 'border-border bg-surface'
      } ${agent.installed ? '' : 'opacity-80'}`}
    >
      <div className="flex items-center gap-3">
        <TerminalIcon className={`size-5 shrink-0 ${agent.installed ? 'text-accent' : 'text-muted'}`} />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold">{agent.name}</p>
          <p className="font-mono text-xs text-muted">
            {agent.installed ? (version === '' ? agent.command : `${agent.command} ${version}`) : 'Not found'}
          </p>
        </div>
        {agent.installed ? (
          <label className="flex shrink-0 cursor-pointer items-center gap-2 text-sm">
            <input type="radio" className="radio" name={groupName} checked={isDefault} onChange={onDefault} />
            Use by default
          </label>
        ) : (
          repo !== '' && (
            <button
              type="button"
              onClick={() => {
                openExternal(repo)
              }}
              className={buttonClass}
            >
              Get {agent.name}
              <ExternalIcon className="size-3.5 text-muted" />
            </button>
          )
        )}
      </div>

      {agent.installed && profiles.length > 0 && (
        <ul aria-label={`${agent.name} profiles`} className="flex flex-wrap gap-1.5">
          {profiles.map((p) => (
            <li key={p.name} className={p.signedIn ? chipClass : `${chipClass} border-warning text-warning`}>
              {p.name}
              {!p.signedIn && ' · not signed in'}
            </li>
          ))}
        </ul>
      )}

      {agent.installed && profiles.length === 0 && (
        <p className="text-xs text-muted">
          No profile yet: run <code className="font-mono">{agent.command}</code> once, then check again.
        </p>
      )}

      {signedOut.map((p) => (
        <div key={p.name} className="flex flex-col gap-1.5">
          <p className="text-xs text-warning">Sign in with the {p.name} profile:</p>
          <CommandLine command={loginCommand(agent, p)} className="max-w-md" />
        </div>
      ))}

      {agent.installed && profiles.length > 1 && (
        <div className="flex items-center justify-between gap-4">
          <label htmlFor={`${id}-profile`} className="text-xs font-medium text-muted">
            Profile launches run with
          </label>
          <select
            id={`${id}-profile`}
            value={defaultProfile}
            onChange={(e) => {
              onProfile(e.target.value)
            }}
            className={fieldClass}
          >
            {profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
      )}
    </section>
  )
}

interface TerminalTilesProps {
  terminals: readonly TerminalChoice[]
  chosen: TerminalID
  onChoose: (id: TerminalID) => void
}

// TerminalTiles chooses the terminal launches open in, from the ones installed.
function TerminalTiles({ terminals, chosen, onChoose }: TerminalTilesProps) {
  const name = useId()

  return (
    <div role="radiogroup" aria-label="Terminal" className="grid grid-cols-2 gap-3">
      {terminals.map((t) => (
        <label
          key={t.id}
          className={`flex items-center gap-3 rounded-lg border border-border bg-surface px-4 py-3 transition-colors has-checked:border-accent has-checked:bg-accent/10 has-focus-visible:outline-2 has-focus-visible:outline-accent ${
            t.installed ? 'cursor-pointer hover:border-accent/50' : 'opacity-disabled'
          }`}
        >
          <input
            type="radio"
            name={name}
            value={t.id}
            className="radio"
            checked={chosen === t.id}
            disabled={!t.installed}
            onChange={() => {
              onChoose(t.id)
            }}
          />
          <span className="min-w-0 flex-1 text-sm font-medium">
            {t.name}
            <span className="block text-xs font-normal text-muted">{t.installed ? 'Installed' : 'Not installed'}</span>
          </span>
        </label>
      ))}
    </div>
  )
}

export interface AgentsStepProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  versions: readonly AgentVersion[]
  // terminals are the terminals launches can open in, null until they are listed.
  terminals: readonly TerminalChoice[] | null
  // onRediscovered reads the agents and profiles again, once Check again looked for them.
  onRediscovered: () => Promise<void>
}

// AgentsStep picks the coding agent: a card for each, with its version or where to get
// it, its profiles, and the command that signs one in; the default, of the agents
// installed; the profile a launch runs with; the terminal it opens in, of those installed;
// and whether each session gets its own worktree. A default agent that isn't installed
// moves to one that is, and iTerm2 chosen but missing moves to Terminal, each saying so.
export function AgentsStep(props: AgentsStepProps) {
  const { agents, profiles, versions, terminals } = props
  const id = useId()
  const { settings, save, error } = useSettingsSave(props.settings, props.onSaved)
  const [notes, setNotes] = useState<string[]>([])
  const looker = useMutation(() => rediscoverProfiles())

  const chooseAgent = (agent: AgentInfo) => {
    void save({ defaultAgent: agent.id }, () => setDefaultAgent(agent.id))
  }

  const chooseTerminal = (terminal: TerminalID) => {
    void save({ terminal }, () => setTerminal(terminal))
  }

  // The settings a fresh install starts with may name what isn't here: each is moved, once,
  // to what is.
  const fixed = useRef({ agent: false, terminal: false })
  const fixDefaults = useEffectEvent(() => {
    const current = agents.find((a) => a.id === settings.defaultAgent)
    const installed = agents.find((a) => a.installed)

    if (!fixed.current.agent && agents.length > 0 && current?.installed !== true && installed !== undefined) {
      fixed.current.agent = true
      void save({ defaultAgent: installed.id }, () => setDefaultAgent(installed.id)).then((result) => {
        if (result.ok) {
          setNotes((was) => [...was, switchedAgent(current, installed)])
        }
      })
    }

    const iterm = terminals?.find((t) => t.id === 'iterm2')

    if (!fixed.current.terminal && settings.terminal === 'iterm2' && iterm?.installed === false) {
      fixed.current.terminal = true
      void save({ terminal: 'terminal' }, () => setTerminal('terminal')).then((result) => {
        if (result.ok) {
          setNotes((was) => [...was, 'iTerm2 isn’t installed, so launches open in Terminal.'])
        }
      })
    }
  })

  useEffect(() => {
    fixDefaults()
  }, [agents, terminals])

  const checkAgain = async () => {
    const result = await looker.run()

    if (result.ok) {
      await props.onRediscovered()
    }
  }

  return (
    <div className="flex flex-col gap-8">
      <section aria-labelledby={`${id}-agents`} className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-4">
          <h3 id={`${id}-agents`} className="text-sm font-semibold">
            Coding agents
          </h3>
          <button
            type="button"
            disabled={looker.pending}
            onClick={() => {
              void checkAgain()
            }}
            className={buttonClass}
          >
            <RefreshIcon className={`size-3.5 text-muted ${looker.pending ? 'motion-safe:animate-spin' : ''}`} />
            {looker.pending ? 'Looking…' : 'Check again'}
          </button>
        </div>
        {agents.map((agent) => (
          <AgentCard
            key={agent.id}
            agent={agent}
            version={versions.find((v) => v.agent === agent.id)?.version ?? ''}
            profiles={profiles.filter((p) => p.agent === agent.id)}
            isDefault={settings.defaultAgent === agent.id}
            defaultProfile={settings.defaultProfiles[agent.id] ?? 'default'}
            groupName={`${id}-default`}
            onDefault={() => {
              chooseAgent(agent)
            }}
            onProfile={(name) => {
              void save({ defaultProfiles: { ...settings.defaultProfiles, [agent.id]: name } }, () =>
                setDefaultProfile(agent.id, name),
              )
            }}
          />
        ))}
        <ErrorText error={looker.error}>Couldn’t look for profiles: </ErrorText>
      </section>

      <section aria-labelledby={`${id}-terminal`} className="flex flex-col gap-3">
        <h3 id={`${id}-terminal`} className="text-sm font-semibold">
          Terminal
        </h3>
        {terminals === null ? (
          <p className="text-sm text-muted">Looking for terminals…</p>
        ) : (
          <TerminalTiles terminals={terminals} chosen={settings.terminal} onChoose={chooseTerminal} />
        )}
        {settings.terminal === 'iterm2' && (
          <p className="text-xs text-muted">Your first launch asks to let {brand.name} control iTerm2. Choose Allow.</p>
        )}
      </section>

      <div className="flex items-start justify-between gap-6 rounded-lg border border-border bg-surface px-4 py-3.5">
        <div className="min-w-0">
          <label htmlFor={`${id}-worktree`} className="text-sm font-medium">
            Start each session in its own worktree
          </label>
          <p id={`${id}-worktree-help`} className="text-xs text-muted">
            Your clone’s checkout stays exactly as it is.
          </p>
        </div>
        <Switch
          id={`${id}-worktree`}
          checked={settings.worktreeDefault}
          describedBy={`${id}-worktree-help`}
          onChange={(on) => {
            void save({ worktreeDefault: on }, () => setWorktreeDefault(on))
          }}
        />
      </div>

      {notes.map((note) => (
        <p key={note} role="status" className="flex items-center gap-1.5 text-xs text-success">
          <CircleCheckIcon className="size-3.5" />
          {note}
        </p>
      ))}
      <ErrorText error={error} />
    </div>
  )
}
