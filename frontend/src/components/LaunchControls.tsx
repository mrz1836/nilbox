import { useId, useState } from 'react'

import { attempt } from '../lib/call'
import { agentName as agentLabel } from '../lib/launch'
import { stateLabel } from '../lib/sessions'
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
import { useQuery } from '../lib/useQuery'
import {
  addCloneLocations,
  chooseFolder,
  cloneRepo,
  cloneTarget,
  focusSession,
  launch,
  linkClone,
  removeLaunchDefaults,
  resolveLaunch,
  saveLaunchDefaults,
  type AgentInfo,
  type AgentSession,
  type CloneTarget,
  type InboxItem,
  type ItemResolution,
  type LaunchDefaults,
  type LaunchDefaultsInput,
  type LaunchHold,
  type LaunchRequest,
  type LaunchResult,
  type LinkedClone,
  type Profile,
  type Prompt,
  type Resolution,
  type Settings,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { AlertIcon, ChevronDownIcon, TerminalIcon } from './icons'
import { LaunchDialog } from './LaunchDialog'
import { LaunchHoldDialog } from './LaunchHoldDialog'
import { LaunchMenu } from './LaunchMenu'

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
  // onSettingsSaved takes the settings stored once a clone found elsewhere is added as a
  // repository location.
  onSettingsSaved: (settings: Settings) => void
  // running is the item's newest session still running, if any: the button then brings
  // its terminal tab forward instead, and a launch from the menu asks first.
  running?: AgentSession | null | undefined
  // initial are the choices every launch from here starts with, such as the worktree on
  // for a launch from the Worktrees view; none by default.
  initial?: Partial<Choices>
}

// LaunchState is where the row's launch stands. A launch that finds no local clone ends
// in noClone, which keeps the request so a confirmed clone, or one the user finds
// elsewhere, can continue it, with why the last clone found elsewhere was refused; one
// found in a folder that holds other clones ends in addLocation, which asks whether to add
// that folder or the clone alone. One the app held back for the user to confirm ends in
// held, which keeps the request to send again.
type LaunchState =
  | { kind: 'idle' }
  | { kind: 'launching' }
  | { kind: 'held'; hold: LaunchHold; request: LaunchRequest }
  | { kind: 'cloning'; target: CloneTarget }
  | { kind: 'linking'; target: CloneTarget }
  | { kind: 'launched'; result: LaunchResult }
  | { kind: 'failed'; message: string }
  | { kind: 'noClone'; target: CloneTarget; request: LaunchRequest; error: string }
  | { kind: 'addLocation'; target: CloneTarget; request: LaunchRequest; linked: LinkedClone }

// noCloneOf says a repository has no local clone, and where the launch looked: "No local
// clone of acme-org/x in ~/projects or your other repository locations".
function noCloneOf(target: CloneTarget): string {
  const looked = target.where === '' ? '' : ` in ${target.where}`

  return `No local clone of ${target.repo}${looked}`
}

// Saved is the last save of a launch's choices as defaults, which Undo takes back: the
// defaults it saved over (owner and name; name is '' for the owner's), what was stored
// there before, or undefined for nothing, and the choices it saved, which Undo brings back.
interface Saved {
  owner: string
  name: string
  previous: LaunchDefaults | undefined
  choices: Choices
}

// LaunchControls is one row's "Open in Claude" or "Open in Codex" split button. The
// button launches with the item's resolved settings: the agent, prompt, and profile its
// org's and repository's defaults choose, the prompt and profile named on the arrow beside
// it ("Review · default"). The arrow opens the launch options in a dialog in the middle of
// the window (LaunchDialog), where this launch can use another agent, prompt, profile,
// model, effort, worktree, or plan mode, preview exactly what it runs, or save those
// choices as the repository's or the org's defaults. The dialog is rendered only while it
// is open, so a list of rows holds none, and it closes when its page hides. Choices apply
// to the next launch only and outlast closing the dialog, and the arrow shows a dot while
// any differs from the defaults. While a launch or a clone runs, the button, the arrow,
// and the dialog's Open all wait. A launch that finds no local clone offers to clone the
// repository into the clone root and, once confirmed, continues with the same choices; or
// to pick the clone the user has elsewhere, which is added as a repository location (its
// folder, when the user chooses, if it holds other clones) before the launch continues.
//
// A launch that starts says nothing on the row: the card's session tag, and the button
// turned to Show in terminal, follow the session, and the tag's panel says what it started
// with. Only what a launch worked around shows, as a warning, since the user should know
// it whether or not the app follows sessions.
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
  onSettingsSaved,
  initial,
  running = null,
}: LaunchControlsProps) {
  // start is the choices a launch starts from, and goes back to after it.
  const start: Choices = { ...noChoices, ...initial }
  const id = useId()
  // open says the launch options are showing.
  const [open, setOpen] = useState(false)
  const [choices, setChoices] = useState<Choices>(start)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })
  const [confirmingClone, setConfirmingClone] = useState(false)
  const [status, setStatus] = useState<SaveStatus | null>(null)
  // saved is the last save, while Undo can still take it back.
  const [saved, setSaved] = useState<Saved | null>(null)

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
  // undoer puts back what a save replaced: the defaults stored before it, or none.
  const undoer = useMutation(async (last: Saved) => {
    await (last.previous === undefined
      ? removeLaunchDefaults(last.owner, last.name)
      : saveLaunchDefaults(defaultsInput(last.previous)))
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
          : { kind: 'noClone', target: target.data, request, error: '' },
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
    setSaved(null)
  }

  // close hides the launch options, keeping the choices for the next launch.
  const close = () => {
    setOpen(false)
  }

  const launchNow = () => {
    close()
    void run(launchRequest(item.id, choices, untrusted))
  }

  // showTerminal brings a running session's terminal tab forward, saying why when it
  // can't.
  const showTerminal = async (sessionId: string) => {
    const shown = await attempt(focusSession(sessionId))

    setState(shown.ok ? { kind: 'idle' } : { kind: 'failed', message: shown.error })
  }

  // confirmHold launches again with the hold the user confirmed: a running session's apart
  // from a pull request's files, so a launch held back for both is confirmed for each.
  const confirmHold = (hold: LaunchHold, request: LaunchRequest) => {
    void run(hold.reason === 'running' ? { ...request, alongside: hold.key } : { ...request, confirmed: hold.key })
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

  // addThenLaunch adds folders as repository locations, then continues the launch that
  // found no clone in them.
  const addThenLaunch = async (folders: string[], target: CloneTarget, request: LaunchRequest) => {
    setState({ kind: 'linking', target })

    const added = await attempt(addCloneLocations(folders))

    if (!added.ok) {
      setState({ kind: 'noClone', target, request, error: added.error })

      return
    }

    onSettingsSaved(added.data)
    await run(request)
  }

  // linkThenLaunch takes the clone the user has elsewhere, dir, or else asks for its
  // folder, checks it is a clone of the item's repository, and adds it as a repository
  // location, or asks first whether to add its folder, which holds other clones. One the
  // locations hold already continues the launch at once.
  const linkThenLaunch = async (target: CloneTarget, request: LaunchRequest, dir = '') => {
    let folder = dir

    if (folder === '') {
      const chosen = await attempt(chooseFolder({ title: `Choose your clone of ${target.repo}`, button: 'Use' }))

      if (!chosen.ok) {
        setState({ kind: 'noClone', target, request, error: chosen.error })

        return
      }

      folder = chosen.data
    }

    if (folder === '') {
      return
    }

    setState({ kind: 'linking', target })

    const linked = await attempt(linkClone(item.id, folder))

    if (!linked.ok) {
      setState({ kind: 'noClone', target, request, error: linked.error })
    } else if (linked.data.location.covered !== '') {
      await run(request)
    } else if (linked.data.location.parent.path !== '') {
      setState({ kind: 'addLocation', target, request, linked: linked.data })
    } else {
      await addThenLaunch([linked.data.dir], target, request)
    }
  }

  const busy = state.kind === 'launching' || state.kind === 'cloning' || state.kind === 'linking'

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
      setSaved({ owner: base.owner, name, previous: stored, choices })
    } else {
      setStatus({ tone: 'error', text: result.error })
      setSaved(null)
    }
  }

  // undo takes the last save back and brings its choices back, so the user can save them
  // somewhere else instead.
  const undo = async () => {
    if (saved === null) {
      return
    }

    const result = await undoer.run(saved)

    if (result.ok) {
      setChoices(saved.choices)
      setStatus({ tone: 'success', text: 'Undone: the defaults are as they were.' })
      setSaved(null)
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
      <div className="self-center">
        <div className="inline-flex items-stretch rounded-md border border-border bg-surface text-[0.8125rem]">
          {running === null ? (
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
              {state.kind === 'launching' || state.kind === 'linking'
                ? 'Opening…'
                : state.kind === 'cloning'
                  ? 'Cloning…'
                  : openLabel}
            </button>
          ) : (
            <button
              type="button"
              disabled={busy || !running.canFocus}
              onClick={() => {
                void showTerminal(running.id)
              }}
              title={
                running.canFocus
                  ? `${agentLabel(agents, running.agent)} ${stateLabel(running.state)}: show its terminal tab. To start another session, use the launch options.`
                  : `${agentLabel(agents, running.agent)} is starting; its terminal tab can be shown once it has started.`
              }
              className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent"
            >
              <TerminalIcon className="size-3.5 text-accent" />
              {state.kind === 'launching' ? 'Opening…' : 'Show in terminal'}
            </button>
          )}
          <button
            type="button"
            // While a launch or a clone runs, the options wait with the button beside them,
            // so nothing starts a second launch.
            disabled={busy || resolution === null}
            aria-label={`Launch options: ${chip}${changed ? ', changed for this launch' : ''}`}
            title="Prompt, profile, and options for this launch"
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls={open ? id : undefined}
            onClick={(e) => {
              // The desktop's WebKit focuses no button on a click. Focusing this one first
              // makes it where the dialog gives focus back as it closes.
              e.currentTarget.focus()
              setOpen(true)
            }}
            className="inline-flex max-w-52 items-center gap-1 rounded-r-md border-l border-border px-2 text-xs text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
          >
            {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
            {problem !== '' && <AlertIcon className="size-3 shrink-0 text-warning" />}
            <span className={`truncate ${changed ? 'font-medium text-text' : ''}`}>{chip}</span>
            <ChevronDownIcon className="size-3 shrink-0" />
          </button>
        </div>
        {open && (
          <LaunchDialog id={id} title={openLabel} item={item} onClose={close}>
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
                  setSaved(null)
                }}
                agents={agents}
                profiles={profiles}
                prompts={prompts}
                agentName={agentName}
                onLaunch={launchNow}
                busy={busy}
                onSave={(scope) => {
                  void save(scope)
                }}
                saving={saver.pending || undoer.pending}
                status={status}
                onUndo={
                  saved === null
                    ? null
                    : () => {
                        void undo()
                      }
                }
                resolveError={resolveError}
                onManagePrompts={() => {
                  close()
                  onManagePrompts()
                }}
                onCancel={close}
              />
            )}
          </LaunchDialog>
        )}
      </div>
      {state.kind === 'launched' && state.result.notice !== '' && (
        <p role="status" className="col-[2/-1] mt-1.5 flex items-start gap-1.5 text-xs text-warning">
          <AlertIcon className="mt-0.5 size-3.5 shrink-0" />
          <span>{state.result.notice}</span>
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
              ? `${noCloneOf(state.target)}, and ${state.target.dir} already exists, so it can’t be cloned there.`
              : `${noCloneOf(state.target)}.`}
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
          {state.target.elsewhere !== '' && (
            <button
              type="button"
              onClick={() => {
                void linkThenLaunch(state.target, state.request, state.target.elsewhere)
              }}
              className="font-medium text-accent hover:underline"
            >
              {`Use ${state.target.elsewhere}`}
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              void linkThenLaunch(state.target, state.request)
            }}
            className="font-medium text-accent hover:underline"
          >
            It’s cloned elsewhere…
          </button>
          {state.error !== '' && (
            <p role="alert" className="basis-full text-danger">
              {state.error}
            </p>
          )}
        </div>
      )}
      {state.kind === 'linking' && (
        <p role="status" className="col-[2/-1] mt-1.5 truncate text-xs text-muted">
          {`Adding your clone of ${state.target.repo}…`}
        </p>
      )}
      {state.kind === 'addLocation' && (
        <AddLocation
          linked={state.linked}
          onAdd={(folder) => {
            void addThenLaunch([folder], state.target, state.request)
          }}
          onCancel={() => {
            setState({ kind: 'noClone', target: state.target, request: state.request, error: '' })
          }}
        />
      )}
      {state.kind === 'held' && (
        <LaunchHoldDialog
          hold={state.hold}
          agentName={agentName}
          agents={agents}
          item={{ repo: item.repo, number: item.number }}
          onCancel={() => {
            setState({ kind: 'idle' })
          }}
          onConfirm={() => {
            confirmHold(state.hold, state.request)
          }}
          onShow={(sessionId) => {
            void showTerminal(sessionId)
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

interface AddLocationProps {
  linked: LinkedClone
  // onAdd adds the folder as a repository location: the clone's own, or the one that
  // holds it.
  onAdd: (folder: string) => void
  onCancel: () => void
}

// AddLocation asks, for a clone found in a folder that holds other clones, whether to add
// that folder as a repository location, which brings its other clones in too, or only the
// clone.
function AddLocation({ linked, onAdd, onCancel }: AddLocationProps) {
  const { parent } = linked.location
  const others = `${String(parent.clones)} other ${parent.clones === 1 ? 'clone' : 'clones'}`

  return (
    <div className="col-[2/-1] mt-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs">
      <p role="status" className="text-text">
        {`${linked.dir} is a clone. Its folder, ${parent.path}, holds ${others}.`}
      </p>
      <button
        type="button"
        onClick={() => {
          onAdd(parent.path)
        }}
        className="font-medium text-accent hover:underline"
      >
        {`Add ${parent.path}`}
      </button>
      <button
        type="button"
        onClick={() => {
          onAdd(linked.dir)
        }}
        className="font-medium text-accent hover:underline"
      >
        Add only this clone
      </button>
      <button type="button" onClick={onCancel} className="text-muted hover:underline">
        Cancel
      </button>
    </div>
  )
}
