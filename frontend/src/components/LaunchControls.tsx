import { useEffect, useId, useLayoutEffect, useState } from 'react'

import { errorMessage } from '../lib/format'
import {
  defaultsInput,
  emptyDefaults,
  findDefaults,
  hasChoices,
  launchRequest,
  noChoices,
  savableChoices,
  saveStatus,
  shadowedChoices,
  withChoices,
  type Choices,
  type SaveStatus,
} from '../lib/launchChoices'
import { usePopover } from '../lib/usePopover'
import {
  cloneRepo,
  cloneTarget,
  launch,
  resolveLaunch,
  saveLaunchDefaults,
  type CloneTarget,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type LaunchRequest,
  type LaunchResult,
  type Profile,
  type Prompt,
  type Resolution,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { AlertIcon, ChevronDownIcon, TerminalIcon } from './icons'
import { LaunchMenu } from './LaunchMenu'

export interface LaunchControlsProps {
  item: InboxItem
  // resolution is the item's default launch settings, or null while they load.
  resolution: ItemResolution | null
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // launchDefaults are every saved org and repo default, for saving this launch's choices.
  launchDefaults: readonly LaunchDefaults[]
  // onDefaultsSaved reloads the launch defaults and every row's settings.
  onDefaultsSaved: () => Promise<void>
  // onManagePrompts opens the Prompts page.
  onManagePrompts: () => void
}

// LaunchState is where the row's launch stands. A launch that finds no local clone ends
// in noClone, which keeps the request so a confirmed clone can continue it.
type LaunchState =
  | { kind: 'idle' }
  | { kind: 'launching' }
  | { kind: 'cloning'; target: CloneTarget }
  | { kind: 'launched'; result: LaunchResult }
  | { kind: 'failed'; message: string }
  | { kind: 'noClone'; target: CloneTarget; request: LaunchRequest }

// launchedText summarizes a started launch for the row.
function launchedText(result: LaunchResult): string {
  const where = result.worktree === '' ? result.clonePath : `${result.clonePath} (worktree ${result.worktree})`

  return `Opened “${result.prompt}” in ${result.terminal}: ${where} · ${result.profile} · ${result.permission}`
}

// LaunchControls is one row's "Open in Claude" split button. The button launches with
// the item's resolved settings: the prompt and profile its org's and repository's
// defaults choose, named on the arrow beside it ("Review · default"). The arrow opens the
// launch menu, where this launch can use another prompt, profile, model, effort,
// worktree, or plan mode, preview exactly what it runs, or save those choices as the
// repository's or the org's defaults. Choices apply to the next launch only, and the
// arrow shows a dot while any differs from the defaults. A launch that finds no local
// clone offers to clone the repository into the clone root and, once confirmed, continues
// with the same choices.
//
// It renders two grid children: the button, and below the row's content, the launch's
// outcome, which runs to the card's right edge. The inbox row is the grid that places
// them.
export function LaunchControls({
  item,
  resolution,
  profiles,
  prompts,
  launchDefaults,
  onDefaultsSaved,
  onManagePrompts,
}: LaunchControlsProps) {
  const id = useId()
  const { open, toggle, close, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const [choices, setChoices] = useState<Choices>(noChoices)
  // fetched is the resolution under the chosen prompt and profile, with what it was
  // fetched for, so a stale one is never shown.
  const [fetched, setFetched] = useState<{ base: ItemResolution; key: string; value: Resolution } | null>(null)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })
  const [confirmingClone, setConfirmingClone] = useState(false)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<SaveStatus | null>(null)
  // resolveError says why the settings under the chosen prompt and profile could not be
  // worked out, keyed by those choices.
  const [resolveError, setResolveError] = useState<{ key: string; message: string } | null>(null)
  const [above, setAbove] = useState(false)

  const untrusted = !item.trusted
  const problem = resolution?.problem ?? ''
  const key = `${String(choices.promptId)}:${choices.profile}`
  const resolved = fetched !== null && fetched.base === resolution && fetched.key === key ? fetched.value : null
  const base: Resolution | null = resolution === null || problem !== '' ? null : (resolved ?? resolution)
  const changed = hasChoices(choices)
  const agentName = resolution === null || resolution.agent.name === '' ? 'Claude' : resolution.agent.name

  // Re-resolve under the chosen prompt and profile, so every other setting's default
  // (a prompt's model, a profile's effort) is shown as it would really apply.
  useEffect(() => {
    if (resolution === null || problem !== '' || (choices.promptId === 0 && choices.profile === '')) {
      return undefined
    }

    let current = true

    resolveLaunch({ ...launchRequest(item.id, noChoices, false), promptId: choices.promptId, profile: choices.profile })
      .then((value) => {
        if (current) {
          setFetched({ base: resolution, key: `${String(choices.promptId)}:${choices.profile}`, value })
        }
      })
      .catch((err: unknown) => {
        if (current) {
          setResolveError({ key: `${String(choices.promptId)}:${choices.profile}`, message: errorMessage(err) })
        }
      })

    return () => {
      current = false
    }
  }, [item.id, resolution, problem, choices.promptId, choices.profile])

  // Open the menu upward when there is not enough room below the button.
  useLayoutEffect(() => {
    const trigger = triggerRef.current
    const panel = panelRef.current

    if (!open || trigger === null || panel === null) {
      return
    }

    const spaceBelow = window.innerHeight - trigger.getBoundingClientRect().bottom
    const spaceAbove = trigger.getBoundingClientRect().top

    setAbove(panel.offsetHeight + 12 > spaceBelow && spaceAbove > spaceBelow)
  }, [open, triggerRef, panelRef])

  // Focus the chosen prompt when the menu opens.
  useEffect(() => {
    if (open) {
      const panel = panelRef.current

      ;(
        panel?.querySelector<HTMLInputElement>('input:checked') ?? panel?.querySelector<HTMLInputElement>('input')
      )?.focus()
    }
  }, [open, panelRef])

  // run launches request. When the launch fails, it asks whether the repository has a
  // local clone, and offers to clone it when it has none.
  const run = (request: LaunchRequest) => {
    setState({ kind: 'launching' })

    launch(request)
      .then((result) => {
        setState({ kind: 'launched', result })
        setChoices(noChoices)
        setStatus(null)
      })
      .catch(async (err: unknown) => {
        const message = errorMessage(err)
        const target = await cloneTarget(item.id).catch(() => null)

        setState(target === null || target.cloned ? { kind: 'failed', message } : { kind: 'noClone', target, request })
      })
  }

  const start = () => {
    close()
    run(launchRequest(item.id, choices, untrusted))
  }

  // cloneThenLaunch clones the repository a launch found no clone of, then continues that
  // launch.
  const cloneThenLaunch = (target: CloneTarget, request: LaunchRequest) => {
    setConfirmingClone(false)
    setState({ kind: 'cloning', target })

    cloneRepo(item.id)
      .then(() => {
        run(request)
      })
      .catch((err: unknown) => {
        setState({ kind: 'failed', message: errorMessage(err) })
      })
  }

  const busy = state.kind === 'launching' || state.kind === 'cloning'

  const save = (scope: 'repo' | 'org') => {
    if (base === null || !savableChoices(choices)) {
      setStatus({ tone: 'warning', text: 'Choose something different first.' })

      return
    }

    const name = scope === 'org' ? '' : (base.repo.split('/').at(-1) ?? '')
    const stored = findDefaults(launchDefaults, base.owner, name)
    const input = withChoices(
      stored === undefined ? emptyDefaults(base.owner, name) : defaultsInput(stored),
      choices,
      item.kind,
    )
    const target = scope === 'org' ? `every ${base.owner} repository` : base.repo
    const report = saveStatus(target, base.prompt.nickname, base.repo, shadowedChoices(choices, base, scope))

    setSaving(true)
    saveLaunchDefaults(input)
      .then(async () => {
        await onDefaultsSaved()
        setChoices({ ...noChoices, override: choices.override })
        setStatus(report)
      })
      .catch((err: unknown) => {
        setStatus({ tone: 'error', text: errorMessage(err) })
      })
      .finally(() => {
        setSaving(false)
      })
  }

  const promptName =
    choices.promptId === 0
      ? (base?.prompt.nickname ?? '')
      : (prompts.find((p) => p.id === choices.promptId)?.nickname ?? '')
  const profileName = choices.profile === '' ? (base?.profile.value ?? '') : choices.profile
  const chip = problem !== '' ? 'No prompt' : base === null ? '…' : `${promptName} · ${profileName}`

  return (
    <>
      <div className="relative self-center">
        <div className="inline-flex items-stretch rounded-md border border-border bg-surface text-[13px]">
          <button
            type="button"
            disabled={busy || base === null}
            onClick={start}
            title={problem !== '' ? problem : `Open in ${agentName} with “${promptName}” as ${profileName}`}
            className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent"
          >
            <TerminalIcon className="size-3.5 text-accent" />
            {state.kind === 'launching' ? 'Opening…' : state.kind === 'cloning' ? 'Cloning…' : `Open in ${agentName}`}
          </button>
          <button
            ref={triggerRef}
            type="button"
            disabled={resolution === null}
            aria-label={`Launch options: ${chip}${changed ? ', changed for this launch' : ''}`}
            title="Prompt, profile, and options for this launch"
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls={open ? id : undefined}
            onClick={toggle}
            className="inline-flex max-w-52 items-center gap-1 rounded-r-md border-l border-border px-2 text-xs text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
          >
            {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
            {problem !== '' && <AlertIcon className="size-3 shrink-0 text-warning" />}
            <span className={`truncate ${changed ? 'font-medium text-text' : ''}`}>{chip}</span>
            <ChevronDownIcon className="size-3 shrink-0" />
          </button>
        </div>
        {open && (
          <div
            ref={panelRef}
            id={id}
            role="dialog"
            aria-label="Launch options"
            className={`absolute right-0 z-20 max-h-[calc(100vh-8rem)] w-[22rem] overflow-y-auto rounded-lg border border-border bg-surface shadow-xl shadow-black/25 motion-safe:animate-pop-in ${above ? 'bottom-full mb-1.5 origin-bottom-right' : 'top-full mt-1.5 origin-top-right'}`}
          >
            {base === null ? (
              <div className="flex flex-col gap-2 p-3.5 text-sm">
                <p className="flex items-start gap-2 text-warning">
                  <AlertIcon className="mt-0.5 size-4 shrink-0" />
                  {problem !== '' ? problem : 'Loading the launch settings…'}
                </p>
                {problem !== '' && (
                  <button
                    type="button"
                    onClick={() => {
                      close()
                      onManagePrompts()
                    }}
                    className="self-start text-sm font-medium text-accent hover:underline"
                  >
                    Open the Prompts page
                  </button>
                )}
              </div>
            ) : (
              <LaunchMenu
                item={item}
                resolved={base}
                choices={choices}
                onChoices={(next) => {
                  setChoices(next)
                  setStatus(null)
                }}
                profiles={profiles}
                prompts={prompts}
                agentName={agentName}
                onLaunch={start}
                onSave={save}
                saving={saving}
                status={status}
                resolveError={resolveError?.key === key ? resolveError.message : ''}
                onManagePrompts={() => {
                  close()
                  onManagePrompts()
                }}
              />
            )}
          </div>
        )}
      </div>
      {state.kind === 'launched' && (
        <p role="status" title={launchedText(state.result)} className="col-[2/-1] mt-1.5 truncate text-xs text-success">
          {launchedText(state.result)}
        </p>
      )}
      {state.kind === 'failed' && (
        <p role="alert" className="col-[2/-1] mt-1.5 text-xs text-danger">
          {state.message}
        </p>
      )}
      {state.kind === 'cloning' && (
        <p role="status" className="col-[2/-1] mt-1.5 truncate text-xs text-muted">
          {`Cloning ${state.target.repo} into ${state.target.dir}…`}
        </p>
      )}
      {state.kind === 'noClone' && (
        <div className="col-[2/-1] mt-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs">
          <p role="alert" className="text-warning">
            {state.target.taken
              ? `No local clone of ${state.target.repo}, and ${state.target.dir} already exists, so it can’t be cloned there.`
              : `No local clone of ${state.target.repo}.`}
          </p>
          {!state.target.taken && (
            <button
              type="button"
              onClick={() => {
                setConfirmingClone(true)
              }}
              className="font-medium text-accent hover:underline"
            >
              {`Clone into ${state.target.dir}`}
            </button>
          )}
        </div>
      )}
      {confirmingClone && state.kind === 'noClone' && (
        <ConfirmDialog
          title={`Clone ${state.target.repo}?`}
          confirmLabel="Clone and open"
          tone="primary"
          onCancel={() => {
            setConfirmingClone(false)
          }}
          onConfirm={() => {
            cloneThenLaunch(state.target, state.request)
          }}
        >
          <p>
            This runs{' '}
            <code className="font-mono text-text">{`gh repo clone ${state.target.repo} ${state.target.dir}`}</code>,
            then opens the session in the new clone.
          </p>
        </ConfirmDialog>
      )}
    </>
  )
}
