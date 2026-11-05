import { useEffect, useId, useState } from 'react'

import { errorMessage } from '../lib/format'
import { usePopover } from '../lib/usePopover'
import { launch, type InboxItem, type LaunchResult, type Profile } from '../platform'
import { ChevronDownIcon, TerminalIcon } from './icons'

export interface LaunchControlsProps {
  item: InboxItem
  profiles: Profile[]
  // defaultProfile is the profile preselected (the profile.default setting).
  defaultProfile: string
  // worktreeDefault presets the "Start in a worktree" choice (the launch.worktree setting).
  worktreeDefault: boolean
}

type LaunchState =
  | { kind: 'idle' }
  | { kind: 'launching' }
  | { kind: 'launched'; result: LaunchResult }
  | { kind: 'failed'; message: string }

const overrideHelp = "Lifts the outside-contributor restriction; keeps the profile's plan and permission settings."

const optionClass = 'flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 hover:bg-raised'

// initialProfile is the preselection: the default profile when it exists, otherwise the
// first profile, or "" (the backend then uses its default) when there are none.
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

// LaunchControls is one row's "Open in Claude" split button: the button launches with the
// row's choices, and the arrow beside it, which names the profile, opens those choices:
// the profile, whether to start in a worktree, and, for an outside contributor's item,
// the permissions override. Every choice is local to the row and applies to the next
// launch only: once a launch starts they return to the defaults, and no setting changes.
// A choice that differs from the defaults is marked on the arrow.
//
// It renders two grid children: the button, and below the row's content, the launch's
// outcome. The inbox row is the grid that places them.
export function LaunchControls({ item, profiles, defaultProfile, worktreeDefault }: LaunchControlsProps) {
  const id = useId()
  const { open, toggle, close, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const preset = initialProfile(profiles, defaultProfile)
  const [profile, setProfile] = useState(preset)
  const [worktree, setWorktree] = useState(worktreeDefault)
  const [override, setOverride] = useState(false)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })

  const untrusted = !item.trusted
  const shownProfile = profile === '' ? 'default' : profile
  const changed = profile !== preset || worktree !== worktreeDefault || override

  // Focus moves to the chosen profile (or the first choice) when the options open.
  useEffect(() => {
    const panel = panelRef.current

    if (!open || panel === null) {
      return
    }

    const target =
      panel.querySelector<HTMLInputElement>('input:checked') ?? panel.querySelector<HTMLInputElement>('input')

    target?.focus()
  }, [open, panelRef])

  const start = () => {
    close()
    setState({ kind: 'launching' })

    launch({ itemId: item.id, profile, worktree, fullAccessOverride: untrusted && override })
      .then((result) => {
        setState({ kind: 'launched', result })
        setProfile(preset)
        setWorktree(worktreeDefault)
        setOverride(false)
      })
      .catch((err: unknown) => {
        setState({ kind: 'failed', message: errorMessage(err) })
      })
  }

  return (
    <>
      <div className="relative self-center">
        <div className="inline-flex items-stretch rounded-md border border-border bg-surface text-[13px]">
          <button
            type="button"
            disabled={state.kind === 'launching'}
            onClick={start}
            className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60"
          >
            <TerminalIcon className="size-3.5 text-accent" />
            {state.kind === 'launching' ? 'Opening…' : 'Open in Claude'}
          </button>
          <button
            ref={triggerRef}
            type="button"
            aria-label={`Launch options: ${shownProfile}${changed ? ', changed for this launch' : ''}`}
            title="Profile and options for this launch"
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls={open ? id : undefined}
            onClick={toggle}
            className="inline-flex max-w-36 items-center gap-1 rounded-r-md border-l border-border px-2 text-xs text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
          >
            {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
            <span className={`truncate ${changed ? 'font-medium text-text' : ''}`}>{shownProfile}</span>
            <ChevronDownIcon className="size-3 shrink-0" />
          </button>
        </div>
        {open && (
          <div
            ref={panelRef}
            id={id}
            role="dialog"
            aria-label="Launch options"
            className="absolute top-full right-0 z-20 mt-1.5 w-64 origin-top-right rounded-lg border border-border bg-surface p-1.5 text-sm shadow-xl shadow-black/20 motion-safe:animate-pop-in"
          >
            <fieldset>
              <legend className="px-2 pt-1 pb-1 text-[11px] font-semibold tracking-wide text-muted uppercase">
                Profile
              </legend>
              {profiles.length === 0 ? (
                <p className="px-2 pb-1 text-xs text-muted">
                  No profiles were found; the agent&apos;s default is used.
                </p>
              ) : (
                profiles.map((p) => (
                  <label key={p.name} className={optionClass}>
                    <input
                      type="radio"
                      name={`${id}-profile`}
                      value={p.name}
                      checked={profile === p.name}
                      onChange={() => {
                        setProfile(p.name)
                      }}
                      className="accent-accent"
                    />
                    <span className="truncate">{p.name}</span>
                  </label>
                ))
              )}
            </fieldset>
            <div className="my-1.5 border-t border-border" />
            <label className={optionClass}>
              <input
                type="checkbox"
                checked={worktree}
                onChange={(e) => {
                  setWorktree(e.target.checked)
                }}
                className="accent-accent"
              />
              Start in a worktree
            </label>
            {untrusted && (
              <>
                <label className={optionClass}>
                  <input
                    type="checkbox"
                    checked={override}
                    aria-describedby={`${id}-override-help`}
                    onChange={(e) => {
                      setOverride(e.target.checked)
                    }}
                    className="accent-accent"
                  />
                  Use profile permissions for this item
                </label>
                <p id={`${id}-override-help`} className="px-2 pb-1 pl-8 text-xs text-muted">
                  {overrideHelp}
                </p>
              </>
            )}
          </div>
        )}
      </div>
      {state.kind === 'launched' && (
        <p
          role="status"
          title={launchedText(state.result)}
          className="col-span-2 col-start-2 mt-1.5 truncate text-xs text-success"
        >
          {launchedText(state.result)}
        </p>
      )}
      {state.kind === 'failed' && (
        <p role="alert" className="col-span-2 col-start-2 mt-1.5 text-xs text-danger">
          {state.message}
        </p>
      )}
    </>
  )
}
