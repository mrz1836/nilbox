import { brand } from '../../generated/brand'
import { counted } from '../../lib/format'
import type { Profile } from '../../platform'
import { chipClass } from '../controls'
import { Group, Note } from './rows'

// permissionLabels name a profile's permission mode for people. A mode not listed here
// shows as stored.
const permissionLabels: Readonly<Record<string, string>> = {
  full: 'Full access',
  bypassPermissions: 'Full access',
  '': 'Asks first',
  default: 'Asks first',
  acceptEdits: 'Accepts edits',
  plan: 'Plans only',
}

// profileTraits lists what a profile sets for a session, in the order a row shows them:
// model, effort, plan mode, any extras, and its permissions.
function profileTraits(profile: Profile): string[] {
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
    traits.push('Starts in plan mode')
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

  traits.push(permissionLabels[profile.permissionMode] ?? profile.permissionMode)

  return traits
}

// ProfilesSection lists every profile with the folder it runs from, what it sets for a
// session, and whether its folder has gone missing, then says how profiles are found.
export function ProfilesSection({ profiles }: { profiles: readonly Profile[] }) {
  return (
    <div className="flex flex-col gap-3">
      {profiles.length === 0 ? (
        <Group>
          <p className="px-4 py-3.5 text-sm text-muted">No agent profiles were found in your home folder.</p>
        </Group>
      ) : (
        <ul aria-label="Profiles" className="divide-y divide-border rounded-lg border border-border bg-surface">
          {profiles.map((profile) => (
            <li key={profile.name} className="flex items-center justify-between gap-6 px-4 py-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{profile.name}</p>
                <p title={profile.folder} className="truncate font-mono text-xs text-muted">
                  {profile.folder}
                </p>
              </div>
              <ul aria-label={`${profile.name} settings`} className="flex shrink-0 flex-wrap justify-end gap-1.5">
                {profile.missing && (
                  <li className="rounded-full border border-warning px-2 py-0.5 text-xs whitespace-nowrap text-warning">
                    Folder missing
                  </li>
                )}
                {profileTraits(profile).map((trait) => (
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
        {brand.name} looks for profiles in your home folder each time it starts.{' '}
        <code className="font-mono">~/.claude</code> is the default profile, and each{' '}
        <code className="font-mono">~/.claude_&lt;name&gt;</code> or{' '}
        <code className="font-mono">~/.claude-&lt;name&gt;</code> folder the agent has run with becomes profile{' '}
        <code className="font-mono">&lt;name&gt;</code>. A folder counts only when it holds{' '}
        <code className="font-mono">.claude.json</code>, <code className="font-mono">settings.json</code>, or{' '}
        <code className="font-mono">projects/</code>, so folders other tools name the same way are left out.
      </Note>
    </div>
  )
}
