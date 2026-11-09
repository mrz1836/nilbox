import { useId, useState } from 'react'

import { attempt } from '../lib/call'
import { agentName as agentLabel } from '../lib/launch'
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
import { useMutation } from '../lib/useMutation'
import { usePopover } from '../lib/usePopover'
import { useQuery } from '../lib/useQuery'
import {
  cloneRepo,
  cloneTarget,
  launch,
  resolveLaunch,
  saveLaunchDefaults,
  type AgentInfo,
  type CloneTarget,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type LaunchDefaultsInput,
  type LaunchHold,
  type LaunchRequest,
  type LaunchResult,
  type Profile,
  type Prompt,
  type Resolution,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { AlertIcon, ChevronDownIcon, TerminalIcon } from './icons'
import { LaunchHoldDialog } from './LaunchHoldDialog'
import { LaunchMenu } from './LaunchMenu'
import { PopoverPanel } from './PopoverPanel'

export interface LaunchControlsProps {
  item: InboxItem
  // resolution is the item's default launch settings, or null while they load.
  resolution: ItemResolution | null
  // agents are the coding agents a launch can start, each with whether it is installed.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  // launchDefaults are every saved org and repo default, for saving this launch's choices.
  launchDefaults: readonly LaunchDefaults[]
  // onDefaultsSaved reloads the launch defaults and every row's settings.
  onDefaultsSaved: () => Promise<void>
  // onManagePrompts opens the Prompts page.
  onManagePrompts: () => void
  // initial are the choices every launch from here starts with, such as the worktree on
  // for a launch from the Worktrees view; none by default.
  initial?: Partial<Choices>
  // menuAlign lines the launch menu up with the button's right edge, for a button at the
  // right of a card (the default), or its left edge, for one at the left of a row, so the
  // menu opens over the content rather than off the side of the view.
  menuAlign?: 'left' | 'right'
}

// LaunchState is where the row's launch stands. A launch that finds no local clone ends
// in noClone, which keeps the request so a confirmed clone can continue it; one the app
// held back for the user to confirm ends in held, which keeps the request to send again.
type LaunchState =
  | { kind: 'idle' }
  | { kind: 'launching' }
  | { kind: 'held'; hold: LaunchHold; request: LaunchRequest }
  | { kind: 'cloning'; target: CloneTarget }
  | { kind: 'launched'; result: LaunchResult }
  | { kind: 'failed'; message: string }
  | { kind: 'noClone'; target: CloneTarget; request: LaunchRequest }

// worktreeActions word what a launch did to its item's worktree.
const worktreeActions: Readonly<Record<string, string>> = {
  create: 'made',
  resume: 'resumed',
  adopt: 'taken over',
  attach: 'made again',
}

// launchedText summarizes a started launch for the row.
function launchedText(result: LaunchResult, agent: string): string {
  const where =
    result.worktree === ''
      ? result.clonePath
      : `${result.dir} (worktree ${worktreeActions[result.worktreeAction] ?? result.worktreeAction})`
  const notice = result.notice === '' ? '' : ` · ${result.notice}`

  return `Opened “${result.prompt}” in ${agent} in ${result.terminal}: ${where} · ${result.profile} · ${result.permission}${notice}`
}

// LaunchControls is one row's "Open in Claude" or "Open in Codex" split button. The
// button launches with the item's resolved settings: the agent, prompt, and profile its
// org's and repository's defaults choose, the prompt and profile named on the arrow beside
// it ("Review · default"). The arrow opens the launch menu, where this launch can use
// another agent, prompt, profile, model, effort, worktree, or plan mode, preview exactly
// what it runs, or save those choices as the repository's or the org's defaults. Choices
// apply to the next launch only, and the arrow shows a dot while any differs from the
// defaults. A launch that finds no local clone offers to clone the repository into the
// clone root and, once confirmed, continues with the same choices.
//
// It renders two grid children: the button, and below the row's content, the launch's
// outcome, which runs to the card's right edge. The inbox row is the grid that places
// them.
export function LaunchControls({
  item,
  resolution,
  agents,
  profiles,
  prompts,
  launchDefaults,
  onDefaultsSaved,
  onManagePrompts,
  initial,
  menuAlign = 'right',
}: LaunchControlsProps) {
  // start is the choices a launch starts from, and goes back to after it.
  const start: Choices = { ...noChoices, ...initial }
  const id = useId()
  // The menu opens upward when there is not enough room below the button, and focus goes
  // to the chosen prompt.
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({
    placement: 'flip',
    focus: (panel) =>
      panel.querySelector<HTMLInputElement>('[data-initial-focus] input:checked') ??
      panel.querySelector<HTMLInputElement>('input:checked') ??
      panel.querySelector('input'),
  })
  const { open, toggle, close, triggerRef } = popover
  const [choices, setChoices] = useState<Choices>(start)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })
  const [confirmingClone, setConfirmingClone] = useState(false)
  const [status, setStatus] = useState<SaveStatus | null>(null)

  const untrusted = !item.trusted
  const problem = resolution?.problem ?? ''
  const chosen = choices.agent !== '' || choices.promptId !== 0 || choices.profile !== ''
  const key = `${choices.agent}:${String(choices.promptId)}:${choices.profile}`
  // Re-resolve under the chosen agent, prompt, and profile, so every other setting's
  // default (a prompt's model, a profile's effort) is shown as it would really apply. The
  // answer carries the resolution it was made over and the choices it was made for, so one
  // made for anything else never shows.
  const reresolved = useQuery(
    () =>
      resolveLaunch({
        ...launchRequest(item.id, noChoices, false),
        agent: choices.agent,
        promptId: choices.promptId,
        profile: choices.profile,
      }).then((value) => ({ base: resolution, key, value })),
    [item.id, resolution, problem, key],
    { enabled: resolution !== null && problem === '' && chosen },
  )
  // failed is a latest request, made for these choices, that was refused; the settings
  // under them are then unknown, whatever an earlier answer said.
  const failed = chosen && !reresolved.pending && reresolved.error !== ''
  const answer = reresolved.data
  const resolved = chosen && !failed && answer?.base === resolution && answer.key === key ? answer.value : null
  // resolveError says why the settings under the chosen prompt and profile could not be
  // worked out.
  const resolveError = failed ? reresolved.error : ''
  const base: Resolution | null = resolution === null || problem !== '' ? null : (resolved ?? resolution)
  // changed reports a choice that differs from where launches from here start.
  const changed = hasChoices(choices) && JSON.stringify(choices) !== JSON.stringify(start)
  // agentName names the agent the button starts: the one chosen for this launch, else the
  // one the item's settings resolve to.
  const agentName =
    choices.agent !== ''
      ? (agents.find((a) => a.id === choices.agent)?.name ?? choices.agent)
      : (base?.agent.name ?? resolution?.agent.name ?? '')
  // openLabel is the button's label, plain "Open" until the agent is known.
  const openLabel = agentName === '' ? 'Open' : `Open in ${agentName}`
  // saver stores this launch's choices as launch defaults, then reloads every row's.
  const saver = useMutation(async (input: LaunchDefaultsInput) => {
    await saveLaunchDefaults(input)
    await onDefaultsSaved()
  })

  // run launches request. A launch the app holds back asks the user to confirm; when the
  // launch fails, it asks whether the repository has a local clone, and offers to clone
  // it when it has none.
  const run = async (request: LaunchRequest) => {
    setState({ kind: 'launching' })

    const launched = await attempt(launch(request))

    if (!launched.ok) {
      const target = await attempt(cloneTarget(item.id))

      setState(
        !target.ok || target.data.cloned
          ? { kind: 'failed', message: launched.error }
          : { kind: 'noClone', target: target.data, request },
      )

      return
    }

    const result = launched.data

    if (result.hold !== null) {
      setState({ kind: 'held', hold: result.hold, request })

      return
    }

    setState({ kind: 'launched', result })
    setChoices(start)
    setStatus(null)
  }

  const launchNow = () => {
    close()
    void run(launchRequest(item.id, choices, untrusted))
  }

  // cloneThenLaunch clones the repository a launch found no clone of, then continues that
  // launch.
  const cloneThenLaunch = async (target: CloneTarget, request: LaunchRequest) => {
    setConfirmingClone(false)
    setState({ kind: 'cloning', target })

    const cloned = await attempt(cloneRepo(item.id))

    if (cloned.ok) {
      await run(request)
    } else {
      setState({ kind: 'failed', message: cloned.error })
    }
  }

  const busy = state.kind === 'launching' || state.kind === 'cloning'

  const save = async (scope: 'repo' | 'org') => {
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
      base.agent.id,
    )
    const target = scope === 'org' ? `every ${base.owner} repository` : base.repo
    const report = saveStatus(target, base.prompt.nickname, base.repo, shadowedChoices(choices, base, scope))

    const result = await saver.run(input)

    if (result.ok) {
      setChoices({ ...start, override: choices.override })
      setStatus(report)
    } else {
      setStatus({ tone: 'error', text: result.error })
    }
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
        <div className="inline-flex items-stretch rounded-md border border-border bg-surface text-[0.8125rem]">
          <button
            type="button"
            disabled={busy || base === null}
            onClick={launchNow}
            title={
              problem !== ''
                ? problem
                : base === null
                  ? openLabel
                  : `${openLabel} with “${promptName}” as ${profileName}`
            }
            className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent"
          >
            <TerminalIcon className="size-3.5 text-accent" />
            {state.kind === 'launching' ? 'Opening…' : state.kind === 'cloning' ? 'Cloning…' : openLabel}
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
        <PopoverPanel
          state={popover}
          align={menuAlign}
          id={id}
          role="dialog"
          aria-label="Launch options"
          className="z-20 max-h-[calc(100vh-8rem)] w-[22rem] overflow-y-auto shadow-black/25"
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
              agents={agents}
              profiles={profiles}
              prompts={prompts}
              agentName={agentName}
              onLaunch={launchNow}
              onSave={(scope) => {
                void save(scope)
              }}
              saving={saver.pending}
              status={status}
              resolveError={resolveError}
              onManagePrompts={() => {
                close()
                onManagePrompts()
              }}
            />
          )}
        </PopoverPanel>
      </div>
      {state.kind === 'launched' && (
        <p
          role="status"
          title={launchedText(state.result, agentLabel(agents, state.result.agent))}
          className="col-[2/-1] mt-1.5 truncate text-xs text-success"
        >
          {launchedText(state.result, agentLabel(agents, state.result.agent))}
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
      {state.kind === 'held' && (
        <LaunchHoldDialog
          hold={state.hold}
          agentName={agentName}
          onCancel={() => {
            setState({ kind: 'idle' })
          }}
          onConfirm={() => {
            void run({ ...state.request, confirmed: state.hold.key })
          }}
        />
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
            void cloneThenLaunch(state.target, state.request)
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
