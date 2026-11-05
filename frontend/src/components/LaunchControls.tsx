import { useId, useState } from 'react'

import { errorMessage } from '../lib/format'
import { launch, type InboxItem, type LaunchResult, type Profile } from '../platform'

export interface LaunchControlsProps {
  item: InboxItem
  profiles: Profile[]
  // defaultProfile is the profile the dropdown preselects (the profile.default setting).
  defaultProfile: string
  // worktreeDefault presets the "Start in a worktree" checkbox (the launch.worktree setting).
  worktreeDefault: boolean
}

type LaunchState =
  | { kind: 'idle' }
  | { kind: 'launching' }
  | { kind: 'launched'; result: LaunchResult }
  | { kind: 'failed'; message: string }

const overrideHelp = "Lifts the outside-contributor restriction; keeps the profile's plan and permission settings."

// initialProfile is the dropdown's preselection: the default profile when it exists,
// otherwise the first profile, or "" (the backend then uses its default) when there are none.
function initialProfile(profiles: Profile[], defaultProfile: string): string {
  if (profiles.some((p) => p.name === defaultProfile)) {
    return defaultProfile
  }

  return profiles[0]?.name ?? ''
}

// launchedText summarizes a started launch for the row.
function launchedText(result: LaunchResult): string {
  const where = result.worktree === '' ? result.clonePath : `${result.clonePath} (worktree ${result.worktree})`

  return `Opened in ${result.terminal}: ${where} · ${result.profile} · ${result.permission}`
}

// LaunchControls is one row's "Open in Claude" control: a profile dropdown, the worktree
// checkbox and, for items from outside contributors, a badge and the permissions override.
// Every choice is local to this row and applies to the next launch only: after a launch
// starts, the controls return to their defaults, and no setting is ever changed.
export function LaunchControls({ item, profiles, defaultProfile, worktreeDefault }: LaunchControlsProps) {
  const id = useId()
  const [profile, setProfile] = useState(() => initialProfile(profiles, defaultProfile))
  const [worktree, setWorktree] = useState(worktreeDefault)
  const [override, setOverride] = useState(false)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })

  const untrusted = !item.trusted

  const open = () => {
    setState({ kind: 'launching' })

    launch({ itemId: item.id, profile, worktree, fullAccessOverride: untrusted && override })
      .then((result) => {
        setState({ kind: 'launched', result })
        setProfile(initialProfile(profiles, defaultProfile))
        setWorktree(worktreeDefault)
        setOverride(false)
      })
      .catch((err: unknown) => {
        setState({ kind: 'failed', message: errorMessage(err) })
      })
  }

  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        {untrusted && (
          <span className="rounded-full border border-warning px-2 py-0.5 text-xs font-medium text-warning">
            outside contributor
          </span>
        )}
        <label htmlFor={`${id}-profile`} className="sr-only">
          Profile
        </label>
        <select
          id={`${id}-profile`}
          value={profile}
          disabled={profiles.length === 0}
          onChange={(e) => {
            setProfile(e.target.value)
          }}
          className="rounded-md border border-border bg-surface px-2 py-1"
        >
          {profiles.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name}
            </option>
          ))}
        </select>
        <label className="flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={worktree}
            onChange={(e) => {
              setWorktree(e.target.checked)
            }}
          />
          Start in a worktree
        </label>
        <button
          type="button"
          disabled={state.kind === 'launching'}
          onClick={open}
          className="rounded-md bg-accent px-3 py-1 font-medium text-background hover:opacity-90 disabled:opacity-60"
        >
          {state.kind === 'launching' ? 'Opening…' : 'Open in Claude'}
        </button>
      </div>
      {untrusted && (
        <div className="flex flex-col gap-0.5">
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={override}
              aria-describedby={`${id}-override-help`}
              onChange={(e) => {
                setOverride(e.target.checked)
              }}
            />
            Use profile permissions for this item
          </label>
          <p id={`${id}-override-help`} className="text-xs text-muted">
            {overrideHelp}
          </p>
        </div>
      )}
      {state.kind === 'launched' && (
        <p role="status" className="text-xs text-success">
          {launchedText(state.result)}
        </p>
      )}
      {state.kind === 'failed' && (
        <p role="alert" className="text-xs text-danger">
          {state.message}
        </p>
      )}
    </div>
  )
}
