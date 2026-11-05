import { useEffect, useId, useState } from 'react'

import { errorMessage } from '../lib/format'
import { effortChoices, effortLabel, modelChoices, modelLabel, promptApplies, sourceLabel } from '../lib/launch'
import { launchRequest, type Choices, type SaveStatus } from '../lib/launchChoices'
import {
  previewLaunch,
  type InboxItem,
  type LaunchPreview,
  type LaunchSetting,
  type Profile,
  type Prompt,
  type Resolution,
} from '../platform'
import { primaryButtonClass, quietButtonClass } from './controls'
import { AlertIcon, ChevronRightIcon, TerminalIcon } from './icons'

export interface LaunchMenuProps {
  item: InboxItem
  // resolved is what the item's settings resolve to with the chosen prompt and profile;
  // its inherited values are what each setting shows as its default.
  resolved: Resolution
  choices: Choices
  onChoices: (c: Choices) => void
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // agentName names the coding tool, as in "Claude".
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

const sectionTitle = 'px-2 pb-1 text-[11px] font-semibold tracking-wide text-muted uppercase'
const overrideHelp = "Lifts the outside-contributor restriction; keeps the profile's plan and permission settings."

// defaultLabel names a setting's default: its value and where it comes from.
function defaultLabel(s: LaunchSetting, label: (v: string) => string): string {
  if (s.inheritedSource === 'agent') {
    return 'Default (the agent’s)'
  }

  return `Default · ${label(s.inherited)} (${sourceLabel(s.inheritedSource)})`
}

// LaunchMenu is the launch button's panel: the prompt, the profile, the model, effort,
// worktree, and plan mode for this launch, a preview of what it runs, and buttons to
// save the choices as defaults or to launch. Every choice starts at what the item's
// settings resolve to, each showing where that comes from.
export function LaunchMenu({
  item,
  resolved,
  choices,
  onChoices,
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
  const applicable = prompts.filter((p) => promptApplies(p, item.kind))
  const promptId = choices.promptId === 0 ? resolved.prompt.id : choices.promptId
  const profile = choices.profile === '' ? resolved.profile.value : choices.profile
  const worktree = choices.worktree ?? resolved.worktree.inherited
  const planMode = choices.planMode ?? resolved.planMode.inherited
  // forcedPlan: an outside contributor's item starts in plan mode until the override lifts it.
  const forcedPlan = untrusted && !choices.override
  const [owner, name] = [resolved.owner, resolved.repo.split('/').at(-1) ?? '']
  const set = (patch: Partial<Choices>) => {
    onChoices({ ...choices, ...patch })
  }

  return (
    <div className="flex flex-col text-sm">
      <div className="px-1.5 pt-2">
        <div className="flex items-baseline justify-between">
          <p id={`${id}-prompt-title`} className={sectionTitle}>
            Prompt
          </p>
          <button type="button" onClick={onManagePrompts} className={quietButtonClass}>
            Manage
          </button>
        </div>
        <div role="radiogroup" aria-labelledby={`${id}-prompt-title`} className="max-h-48 overflow-y-auto">
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
                className="row-span-2 translate-y-0.5 accent-accent"
              />
              <span className="truncate font-medium">{p.nickname}</span>
              <span className="text-[11px] text-muted">
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
          {profiles.map((p) => (
            <label
              key={p.name}
              title={p.name === resolved.profile.inherited ? sourceLabel(resolved.profile.inheritedSource) : p.folder}
              className="cursor-pointer rounded-full border border-border px-2.5 py-0.5 text-[13px] text-muted transition-colors select-none hover:text-text has-checked:border-accent has-checked:bg-accent/10 has-checked:text-text has-focus-visible:outline-2 has-focus-visible:outline-accent"
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
            className="w-full truncate rounded-md border border-border bg-surface px-2 py-1 text-[13px]"
          >
            <option value="">{defaultLabel(resolved.model, modelLabel)}</option>
            {modelChoices.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
          <label htmlFor={`${id}-effort`} className="text-xs text-muted">
            Effort
          </label>
          <select
            id={`${id}-effort`}
            value={choices.effort ?? ''}
            onChange={(e) => {
              const value = e.target.value

              set({ effort: value === '' || value === resolved.effort.inherited ? null : value })
            }}
            className="w-full truncate rounded-md border border-border bg-surface px-2 py-1 text-[13px]"
          >
            <option value="">{defaultLabel(resolved.effort, effortLabel)}</option>
            {effortChoices.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        </div>
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            checked={worktree}
            onChange={(e) => {
              set({ worktree: e.target.checked === resolved.worktree.inherited ? null : e.target.checked })
            }}
            className="accent-accent"
          />
          Start in a worktree
          <span className="ml-auto text-[11px] text-muted">
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
            className="accent-accent"
          />
          <span className={forcedPlan ? 'text-muted' : ''}>Start in plan mode</span>
          <span className="ml-auto text-[11px] text-muted">
            {forcedPlan
              ? 'outside contributor'
              : choices.planMode === null
                ? sourceLabel(resolved.planMode.inheritedSource)
                : 'this launch'}
          </span>
        </label>
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
                className="accent-accent"
              />
              Use profile permissions for this item
            </label>
            <p id={`${id}-override-help`} className="pt-0.5 pl-6 text-xs text-muted">
              {overrideHelp}
            </p>
          </div>
        )}
        {[
          ...(resolved.warnings ?? []),
          ...(resolveError === '' ? [] : [`Couldn’t work out the settings for this choice: ${resolveError}`]),
        ].map((w) => (
          <p key={w} className="flex items-start gap-1.5 text-xs text-warning">
            <AlertIcon className="mt-px size-3.5 shrink-0" />
            {w}
          </p>
        ))}
      </div>

      <PreviewSection item={item} choices={choices} untrusted={untrusted} />

      <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
        <div className="mr-auto flex min-w-0 flex-col">
          <span className="text-[11px] text-muted">Save these choices as the default for</span>
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
              className={`text-[11px] leading-snug ${status.tone === 'error' ? 'text-danger' : status.tone === 'warning' ? 'text-warning' : 'text-success'}`}
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

type PreviewState =
  | { kind: 'closed' }
  | { kind: 'loading' }
  | { kind: 'ready'; preview: LaunchPreview }
  | { kind: 'failed'; message: string }

// PreviewSection shows, on request, exactly what the launch would run: the filled-in
// prompt, where it starts, and the command. It reloads as the choices change.
function PreviewSection({ item, choices, untrusted }: { item: InboxItem; choices: Choices; untrusted: boolean }) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [state, setState] = useState<PreviewState>({ kind: 'closed' })

  useEffect(() => {
    if (!open) {
      return undefined
    }

    let current = true
    const timer = window.setTimeout(() => {
      setState({ kind: 'loading' })
      previewLaunch(launchRequest(item.id, choices, untrusted))
        .then((preview) => {
          if (current) {
            setState({ kind: 'ready', preview })
          }
        })
        .catch((err: unknown) => {
          if (current) {
            setState({ kind: 'failed', message: errorMessage(err) })
          }
        })
    }, 120)

    return () => {
      current = false
      window.clearTimeout(timer)
    }
  }, [open, item.id, choices, untrusted])

  return (
    <div className="border-t border-border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={`${id}-preview`}
        onClick={() => {
          setOpen((was) => !was)
        }}
        className="flex w-full items-center gap-1.5 px-3.5 py-2 text-left text-[13px] text-muted transition-colors hover:text-text"
      >
        <ChevronRightIcon className={`size-3.5 transition-transform ${open ? 'rotate-90' : ''}`} />
        Preview
      </button>
      {open && (
        <div id={`${id}-preview`} className="flex flex-col gap-2 px-3.5 pb-3">
          {state.kind === 'loading' || state.kind === 'closed' ? (
            <p className="text-xs text-muted">Planning the launch…</p>
          ) : state.kind === 'failed' ? (
            <p role="alert" className="text-xs text-danger">
              {state.message}
            </p>
          ) : (
            <>
              <pre className="max-h-44 overflow-y-auto rounded-md border border-border bg-background p-2.5 font-mono text-[11px] leading-[1.35rem] break-words whitespace-pre-wrap">
                {state.preview.prompt}
              </pre>
              <p className="text-xs text-muted">
                In <span className="font-mono text-text">{state.preview.dir}</span>
                {state.preview.worktree !== '' && (
                  <>
                    {' '}
                    · worktree <span className="font-mono text-text">{state.preview.worktree}</span>
                  </>
                )}{' '}
                · {state.preview.permission}
              </p>
              <code className="block rounded-md bg-raised px-2 py-1.5 font-mono text-[11px] leading-snug break-all text-text">
                {state.preview.command}
              </code>
            </>
          )}
        </div>
      )}
    </div>
  )
}
