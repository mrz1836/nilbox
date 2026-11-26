import { useId, useState } from 'react'

import type { Choices } from '../lib/launchChoices'
import { terminalLabel } from '../lib/terminals'
import { useDebouncedQuery } from '../lib/useQuery'
import { counted } from '../lib/format'
import type { ItemKind, WorktreePlan } from '../platform'
import { ErrorText } from './controls'
import { ChevronRightIcon } from './icons'

// LaunchSubject is what a launch is for, as the launch options and their preview need it:
// an inbox item, by its kind, or a repository, whose launch has no item, as the Deps page's
// Upgrade starts one, by kind ''; whether its author is trusted, as a repository always is,
// since an outside contributor's item starts in plan mode until the override lifts it; and
// how its launch is planned without starting it. preview plans the launch the choices make,
// and key names everything else that plan is made with, such as the item and its situation,
// or the repository and its clone, so the preview plans again when any of it changes.
export interface LaunchSubject {
  kind: ItemKind | ''
  trusted: boolean
  key: string
  preview: (choices: Choices) => PromiseLike<Plan>
}

export interface LaunchPreviewSectionProps {
  // subject is what the launch is for, which plans it.
  subject: Pick<LaunchSubject, 'key' | 'preview'>
  // choices are the launch options' choices, which the preview plans with.
  choices: Choices
  // label is the button's words: Preview, unless the launch names it otherwise.
  label?: string
}

// PreviewState is what the preview shows: the plan still on its way, the plan, or why
// there is none.
type PreviewState = { kind: 'loading' } | { kind: 'ready'; preview: Plan } | { kind: 'failed'; message: string }

// PlannedWorktree is what a plan does to the item's worktree, as the launch menu's Preview
// and a routine's both have it.
export type PlannedWorktree = Pick<WorktreePlan, 'path' | 'branch' | 'base' | 'dirty'> & { action: string }

// Plan is a launch planned and not started, as the launch menu's Preview and a routine's
// Preview both have it: the filled-in prompt, the worktree it makes or resumes, where it
// starts, its permissions, the changes to its environment (with the variables another
// agent session would hand down, which every launch unsets, apart in inherited), the
// command, and the terminal it opens in.
export interface Plan {
  prompt: string
  worktree: PlannedWorktree | null
  dir: string
  permission: string
  env: readonly string[]
  inherited: readonly string[]
  command: string
  terminal: string
}

// inheritedEntry says, in one entry, the variables another agent session would hand down
// that a launch unsets: "unset 15 inherited session variables".
export function inheritedEntry(names: readonly string[]): string {
  return `unset ${counted(names.length, 'inherited session variable', 'inherited session variables')}`
}

// worktreeLine says what the launch does to the item's worktree: "Creates … from the pull
// request's head", or "Resumes … · 2 uncommitted files".
export function worktreeLine(wt: PlannedWorktree): string {
  const from = wt.base === '' ? '' : ` from ${wt.base}`
  const dirty = wt.dirty > 0 ? ` · ${counted(wt.dirty, 'uncommitted file', 'uncommitted files')}` : ''

  switch (wt.action) {
    case 'create':
      return `Creates ${wt.path}${from}`
    case 'attach':
      return `Makes ${wt.path} again on ${wt.branch}, keeping its commits`
    case 'adopt':
      return `Takes over ${wt.path}${dirty}`
    default:
      return `Resumes ${wt.path}${dirty}`
  }
}

// PlannedLaunch shows a plan: the prompt as the session receives it, what it does to the
// worktree, where it starts with its permissions and the terminal it opens in, the changes
// to its environment, and the command.
export function PlannedLaunch({ plan }: { plan: Plan }) {
  return (
    <>
      <pre className="max-h-44 overflow-y-auto rounded-md border border-border bg-background p-2.5 font-mono text-caption leading-[1.35rem] break-words whitespace-pre-wrap">
        {plan.prompt}
      </pre>
      {plan.worktree !== null && <p className="text-xs text-text">{worktreeLine(plan.worktree)}</p>}
      <p className="text-xs text-muted">
        In <span className="font-mono text-text">{plan.dir}</span> · {plan.permission}
        {plan.terminal !== '' && ` · opens in ${terminalLabel(plan.terminal, plan.terminal)}`}
      </p>
      {(plan.env.length > 0 || plan.inherited.length > 0) && (
        <p className="font-mono text-caption break-all text-muted">
          {plan.env.join(' · ')}
          {plan.inherited.length > 0 && (
            <>
              {plan.env.length > 0 && ' · '}
              <span title={plan.inherited.map((name) => `unset ${name}`).join('\n')}>
                {inheritedEntry(plan.inherited)}
              </span>
            </>
          )}
        </p>
      )}
      <code className="block rounded-md bg-raised px-2 py-1.5 font-mono text-caption leading-snug break-all text-text">
        {plan.command}
      </code>
    </>
  )
}

// LaunchPreviewSection is the launch menu's Preview: on request, it shows exactly what the
// launch would run, which is the filled-in prompt, the worktree it makes or resumes, where
// it starts, and the command. It reloads as the choices, or the subject, change.
export function LaunchPreviewSection({ subject, choices, label = 'Preview' }: LaunchPreviewSectionProps) {
  const id = useId()
  const [open, setOpen] = useState(false)
  // The plan is asked for again, once the choices rest, while the preview is open. While
  // they rest, the preview shows how the last ask went; once asked, it says it is planning.
  // The choices count by what they hold, so choices made again alike ask nothing.
  const planned = useDebouncedQuery(() => subject.preview(choices), [subject.key, JSON.stringify(choices)], 120, {
    enabled: open,
  })
  const state: PreviewState =
    planned.pending && !planned.waiting
      ? { kind: 'loading' }
      : planned.error !== ''
        ? { kind: 'failed', message: planned.error }
        : planned.data === undefined
          ? { kind: 'loading' }
          : { kind: 'ready', preview: planned.data }

  return (
    <div className="border-t border-border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={`${id}-preview`}
        onClick={() => {
          setOpen((was) => !was)
        }}
        className="flex w-full items-center gap-1.5 px-3.5 py-2 text-left text-body text-muted transition-colors hover:text-text"
      >
        <ChevronRightIcon className={`size-3.5 transition-transform ${open ? 'rotate-90' : ''}`} />
        {label}
      </button>
      {open && (
        <div id={`${id}-preview`} className="flex flex-col gap-2 px-3.5 pb-3">
          {state.kind === 'loading' ? (
            <p className="text-xs text-muted">Planning the launch…</p>
          ) : state.kind === 'failed' ? (
            <ErrorText error={state.message} />
          ) : (
            <PlannedLaunch plan={state.preview} />
          )}
        </div>
      )}
    </div>
  )
}
