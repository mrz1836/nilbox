import { brand } from '../../generated/brand'
import { counted } from '../../lib/format'
import { planPhrase } from '../../lib/launch'
import { useQuery } from '../../lib/useQuery'
import { agentVersions, type AgentInfo, type Profile } from '../../platform'
import { chipClass } from '../controls'
import { headingMatchClass, useSetting } from './match'
import { Group, Note } from './rows'

// permissionLabel names a profile's permission mode for people, in its agent's words. A
// mode the agent does not list shows as stored.
function permissionLabel(agent: AgentInfo, mode: string): string {
  switch (mode) {
    case 'full':
      return 'Full access'
    case '':
      return 'Asks first'
    default:
      return agent.permissionModes.find((m) => m.value === mode)?.label ?? mode
  }
}

// profileTraits lists what a profile sets for a session, in the order a row shows them:
// model, effort, plan mode, any extras, and its permissions.
function profileTraits(agent: AgentInfo, profile: Profile): string[] {
  const traits: string[] = []
  const addDirs = profile.addDirs.length
  const extraArgs = profile.extraArgs.length
  const env = Object.keys(profile.env).length

  if (profile.model !== '') {
    traits.push(profile.model)
  }

  if (profile.effort !== '') {
    traits.push(`${profile.effort} effort`)
  }

  if (profile.planMode) {
    traits.push(`Starts ${planPhrase(agent)}`)
  }

  if (addDirs > 0) {
    traits.push(counted(addDirs, 'extra directory', 'extra directories'))
  }

  if (extraArgs > 0) {
    traits.push(counted(extraArgs, 'extra argument', 'extra arguments'))
  }

  if (env > 0) {
    traits.push(counted(env, 'environment variable', 'environment variables'))
  }

  traits.push(permissionLabel(agent, profile.permissionMode))

  return traits
}

// loginCommand is the command that signs an agent in with a profile.
function loginCommand(agent: AgentInfo, profile: Profile): string {
  return profile.configDir === ''
    ? `${agent.command} login`
    : `${agent.configEnv}=${profile.folder} ${agent.command} login`
}

// Code is a short span of code in a note.
function Code({ children }: { children: string }) {
  return <code className="font-mono">{children}</code>
}

// markerList names an agent's config markers, as in ".claude.json, settings.json, or
// projects/".
function markerList(agent: AgentInfo) {
  const names = agent.markers.map((m) => (m.dir ? `${m.name}/` : m.name))

  return names.map((name, i) => (
    <span key={name}>
      {i === 0 ? '' : i === names.length - 1 ? ', or ' : ', '}
      <Code>{name}</Code>
    </span>
  ))
}

interface AgentProfilesProps {
  agent: AgentInfo
  // version is the agent's installed version, or "" while it is checked or unknown.
  version: string
  profiles: readonly Profile[]
}

// AgentProfiles lists one agent's profiles, each with the folder it runs from, what it
// sets for a session, whether its folder has gone missing, and whether it is signed in,
// under the agent's version and where its command is.
function AgentProfiles({ agent, version, profiles }: AgentProfilesProps) {
  const [marks, match] = useSetting(`${agent.name} profiles`)

  return (
    <section {...marks} aria-label={`${agent.name} profiles`} className="flex flex-col gap-2">
      <h3 className={`flex items-baseline gap-2 text-sm font-semibold ${headingMatchClass(match)}`}>
        {agent.name}
        {version !== '' && <span className="font-mono text-xs font-normal text-muted">{version}</span>}
        <span className={`text-xs font-normal ${agent.installed ? 'text-muted' : 'text-warning'}`}>
          {agent.installed ? agent.path : `${agent.command} isn’t installed`}
        </span>
      </h3>
      {profiles.length === 0 ? (
        <Group>
          <p className="px-4 py-3.5 text-sm text-muted">
            No {agent.name} profiles were found in your home folder. Run <Code>{agent.command}</Code> once so it makes{' '}
            <Code>{`~/${agent.configDir}`}</Code>.
          </p>
        </Group>
      ) : (
        <ul
          aria-label={`${agent.name} profiles`}
          className="divide-y divide-border rounded-lg border border-border bg-surface"
        >
          {profiles.map((profile) => (
            <li key={profile.name} className="flex items-center justify-between gap-6 px-4 py-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{profile.name}</p>
                <p title={profile.folder} className="truncate font-mono text-xs text-muted">
                  {profile.folder}
                </p>
                {!profile.signedIn && (
                  <p className="mt-0.5 text-xs text-warning">
                    Not signed in: run <Code>{loginCommand(agent, profile)}</Code>
                  </p>
                )}
              </div>
              <ul aria-label={`${profile.name} settings`} className="flex shrink-0 flex-wrap justify-end gap-1.5">
                {profile.missing && (
                  <li className="rounded-full border border-warning px-2 py-0.5 text-xs whitespace-nowrap text-warning">
                    Folder missing
                  </li>
                )}
                {!profile.signedIn && (
                  <li className="rounded-full border border-warning px-2 py-0.5 text-xs whitespace-nowrap text-warning">
                    Not signed in
                  </li>
                )}
                {profileTraits(agent, profile).map((trait) => (
                  <li key={trait} className={chipClass}>
                    {trait}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      <Note>
        <Code>{`~/${agent.configDir}`}</Code> is {agent.name}&apos;s default profile, and each{' '}
        <Code>{`~/${agent.configDir}_<name>`}</Code> or <Code>{`~/${agent.configDir}-<name>`}</Code> folder becomes
        profile <Code>{'<name>'}</Code>, run with <Code>{agent.configEnv}</Code> set to it. A folder counts only when it
        holds {markerList(agent)}, so folders other tools name the same way are left out.
      </Note>
    </section>
  )
}

// ProfilesSection lists every coding agent's profiles, grouped by agent, then says how
// profiles are found.
export function ProfilesSection({ agents, profiles }: { agents: readonly AgentInfo[]; profiles: readonly Profile[] }) {
  // The versions come from the agents' commands, read once for each build and then kept.
  const versions = useQuery(() => agentVersions(), [])

  return (
    <div className="flex flex-col gap-6">
      {agents.map((agent) => (
        <AgentProfiles
          key={agent.id}
          agent={agent}
          version={versions.data?.find((v) => v.agent === agent.id)?.version ?? ''}
          profiles={profiles.filter((p) => p.agent === agent.id)}
        />
      ))}
      <Note>{brand.name} looks for profiles in your home folder each time it starts.</Note>
    </div>
  )
}
