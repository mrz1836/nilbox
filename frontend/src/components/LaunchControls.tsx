import { Suspense, useEffect, useId, useState, type MouseEvent, type ReactNode } from 'react'

import { attempt } from '../lib/call'
import { agentName as agentLabel, type LaunchOptions } from '../lib/launch'
import { sinceWhen, stateLabel, stateText } from '../lib/sessions'
import { formatAge } from '../lib/time'
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
  openExternal,
  removeLaunchDefaults,
  resolveLaunch,
  saveLaunchDefaults,
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
  type Resolution,
  type Situation,
} from '../platform'
import { useTakeOverTerminal, useWatch } from './autopilot/AutopilotChip'
import { ConfirmDialog } from './ConfirmDialog'
import { AlertIcon, AutopilotIcon, ChevronDownIcon, LockIcon, SparkIcon, TerminalIcon } from './icons'
import { LaunchDialog, LaunchHoldDialog, LaunchMenu, preloadDialogs, TakeOverDialog } from './launchDialogs'
import { RelativeTime } from './RelativeTime'
import { SessionDot } from './SessionDot'
import { SessionsButton } from './SessionTag'

// LaunchControlsProps are the launch options every launch control shares, and the item's
// own.
export interface LaunchControlsProps extends LaunchOptions {
  item: InboxItem
  // resolution is the item's default launch settings, or null while they load.
  resolution: ItemResolution | null
  // running is the item's newest session still running, if any: the button then brings
  // its terminal tab forward instead, and a launch from the menu asks first. In the card
  // variant it is the card's session (cardSession): the newest still running, else one
  // that ended in the last day.
  running?: AgentSession | null | undefined
  // sessionFresh reports, for the card variant, that a failed card session failed under a
  // day ago, as the card reads it from the clock. A failed session that isn't fresh is no
  // session to the slot, which offers a fresh launch again; fresh by default.
  sessionFresh?: boolean
  // initial are the choices every launch from here starts with, such as the worktree on
  // for a launch from the Worktrees view; none by default.
  initial?: Partial<Choices>
  // variant is how the launch shows: the split button (split, the default), a Signal
  // card's agent slot (card), which follows the card's session, or a Worktrees row's verb
  // (row), one bordered button that opens the launch options.
  variant?: 'split' | 'card' | 'row'
  // situation is the card's situation (nextStep): a pull request's, whose launch defaults'
  // prompt the card's agent button runs and is named for, and the launch options open on;
  // '' (the default) for the item's own prompt.
  situation?: Situation
  // boxClass places the split button, or the row's verb, in the grid it renders into: by
  // default centered in its row.
  boxClass?: string
  // outcomeClass places the launch's outcome in the grid it renders into: by default under
  // the row's content, to its right edge.
  outcomeClass?: string
  // sessions are the item's sessions, newest first, which the card variant's pills open in
  // the sessions panel.
  sessions?: readonly AgentSession[] | undefined
  // onOpen opens the item in its sheet, where the agent's verdict shows what it last said:
  // the card variant's Findings pill does. Without it, Findings opens the item on GitHub.
  onOpen?: (() => void) | undefined
  // column frames the card variant's agent slot in the card's action column (CardActions,
  // its primary before the slot); without it the slot stands alone.
  column?: ((agent: ReactNode) => ReactNode) | undefined
  // peek renders the card's peek (CardPeek), while it is open, after the action column,
  // handing it the agent's button that opens the launch options.
  peek?: ((launch: ReactNode) => ReactNode) | undefined
}

// LaunchPlace is where a row's launch control goes in the grid it renders into (boxClass and
// outcomeClass): its button's cell, and its outcome's, under the row.
export interface LaunchPlace {
  boxClass: string
  outcomeClass: string
}

// partRing draws a focus ring inside a part of the card's agent slot, so it follows that
// part's own shape within the slot's split control rather than crossing its neighbors.
const partRing = 'focus-visible:-outline-offset-2'

// joinedPart is a part of the agent slot's split control after its first: the top, right,
// and bottom of a border, its left edge the part before it.
const joinedPart = `border border-l-0 border-border ${partRing}`

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
// button opens the launch options on the item's resolved settings (the agent, prompt, and
// profile its org's and repository's defaults choose, the prompt and profile named on the
// arrow beside it, "Review · default"), so every launch is seen before it starts, and
// their own Open starts it. The arrow opens the same launch options, in a dialog in the
// middle of the window (LaunchDialog), where this launch can use another agent, prompt, profile,
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
// them, as a Worktrees row places its row variant's (LaunchPlace): one bordered button,
// the row's verb, that opens the same launch options.
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
  sessionFresh = true,
  variant = 'split',
  situation = '',
  boxClass = 'self-center',
  outcomeClass = 'col-[2/-1] mt-1.5',
  sessions,
  onOpen,
  column,
  peek,
}: LaunchControlsProps) {
  // start is the choices a launch starts from, and goes back to after it.
  const start: Choices = { ...noChoices, ...initial }
  const id = useId()
  // open says the launch options are showing.
  const [open, setOpen] = useState(false)
  const [choices, setChoices] = useState<Choices>(start)
  const [state, setState] = useState<LaunchState>({ kind: 'idle' })
  // watch opens an agent's background run's view, by its session: the button's Watch while
  // one works in the item's worktree, and the hold dialog's Watch it.
  const watch = useWatch()
  const takeOverIn = useTakeOverTerminal()
  // takingOver is the background run the hold dialog's Take over asks about, by its
  // session, with the worktree it works in.
  const [takingOver, setTakingOver] = useState<{ sessionId: string; where: string } | null>(null)
  const [confirmingClone, setConfirmingClone] = useState(false)
  const [status, setStatus] = useState<SaveStatus | null>(null)
  // saved is the last save, while Undo can still take it back.
  const [saved, setSaved] = useState<Saved | null>(null)

  // Once a launch control has painted, the dialogs it opens start loading.
  useEffect(() => {
    preloadDialogs()
  }, [])

  const untrusted = !item.trusted
  const problem = resolution?.problem ?? ''
  const chosen = choices.agent !== '' || choices.promptId !== 0 || choices.profile !== ''
  // situationPrompt is the prompt for the card's situation, as the item's resolution names
  // it, only to name the launch: the launch, and the launch options, resolve it again with
  // the settings it brings.
  const situationPrompt =
    situation === '' ? undefined : resolution?.situations.find((s) => s.situation === situation)?.prompt
  // asking: the choices differ from the item's settings, or the card names a situation
  // while the launch options show, whose prompt brings settings of its own. A card asks
  // nothing for its situation alone until its options open.
  const asking = chosen || (situation !== '' && open)
  const key = `${choices.agent}:${String(choices.promptId)}:${choices.profile}:${situation}`
  // Re-resolve under the situation and the chosen agent, prompt, and profile, so every other
  // setting's default (a prompt's model, a profile's effort) is shown as it would really
  // apply. The answer carries the resolution it was made over and the choices it was made
  // for, so one made for anything else never shows.
  const reresolved = useQuery(
    () =>
      resolveLaunch({
        ...launchRequest(item.id, noChoices, false, situation),
        agent: choices.agent,
        promptId: choices.promptId,
        profile: choices.profile,
      }).then((value) => ({ base: resolution, key, value })),
    [item.id, resolution, problem, key],
    { enabled: resolution !== null && problem === '' && asking },
  )
  // failed is a latest request, made for these choices, that was refused; the settings
  // under them are then unknown, whatever an earlier answer said.
  const failed = asking && !reresolved.pending && reresolved.error !== ''
  const answer = reresolved.data
  const resolved = asking && !failed && answer?.base === resolution && answer.key === key ? answer.value : null
  // resolveError says why the settings under the chosen prompt and profile could not be
  // worked out.
  const resolveError = failed ? reresolved.error : ''
  // shown is the item's resolution as the launch names it until resolved again: with the
  // situation's prompt in place of the item's own.
  const shown =
    situationPrompt === undefined || resolution === null ? resolution : { ...resolution, prompt: situationPrompt }
  const base: Resolution | null = shown === null || problem !== '' ? null : (resolved ?? shown)
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
    void run(launchRequest(item.id, choices, untrusted, situation))
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
      situation,
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
  // nickname names the card's launch by its prompt: the situation's, or an issue's (Work on
  // it); '' for a pull request's own, which the agent's name stands for.
  const nickname = situationPrompt !== undefined || item.kind === 'issue' ? promptName : ''
  const chip = problem !== '' ? 'No prompt' : base === null ? '…' : `${promptName} · ${profileName}`

  // opening is the launch button's words: what runs, else what it opens.
  const opening =
    state.kind === 'launching' || state.kind === 'linking'
      ? 'Opening…'
      : state.kind === 'cloning'
        ? 'Cloning…'
        : openLabel

  const launchTitle =
    problem !== '' ? problem : base === null ? openLabel : `${openLabel} with “${promptName}” as ${profileName}`
  // firstTitle is the launch button's tooltip: what it would start, and that the launch
  // options open first.
  const firstTitle =
    problem !== '' ? problem : `${launchTitle}. The launch options open first, to check or change them.`
  const optionsLabel = `Launch options: ${chip}${changed ? ', changed for this launch' : ''}`
  const optionsTitle = 'Prompt, profile, and options for this launch'

  // showOptions opens the launch options. The desktop's WebKit focuses no button on a
  // click. Focusing this one first makes it where the dialog gives focus back as it closes;
  // a pointer clicked it (a key finds it focused already), so it shows no focus ring.
  const showOptions = (e: MouseEvent<HTMLButtonElement>) => {
    e.currentTarget.focus({ focusVisible: false })
    setOpen(true)
  }

  // dialog is the launch options while they show. Its code loads on its own, the dialog and
  // the menu in it under one boundary, so the dialog opens whole, focus starting where it
  // should, with nothing in its place until then: it is drawn in the page's body, so the
  // card it is opened from holds no room for it.
  const dialog = open && (
    <Suspense fallback={null}>
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
            situation={situation}
            onManagePrompts={() => {
              close()
              onManagePrompts()
            }}
            onCancel={close}
          />
        )}
      </LaunchDialog>
    </Suspense>
  )

  // cardOptions is the card's ▾, the launch options: on its own, or the last part of the
  // agent slot's split control (joined), drawing the top, right, and bottom of its border,
  // its left edge the part before it. Its focus ring is drawn inside it, following its
  // rounded end within the border. It never narrows: the part before it gives way. It
  // carries the dot of a choice changed for this launch, and the alert of a prompt that
  // can't resolve, as the split button's arrow does.
  const cardOptions = (joined: boolean) => (
    <button
      type="button"
      // While a launch or a clone runs, the options wait with the button beside them, so
      // nothing starts a second launch.
      disabled={busy || resolution === null}
      aria-label={optionsLabel}
      title={optionsTitle}
      aria-haspopup="dialog"
      aria-expanded={open}
      aria-controls={open ? id : undefined}
      onClick={showOptions}
      className={`inline-flex shrink-0 items-center gap-1 px-1.75 text-muted transition-colors hover:bg-raised hover:text-text disabled:opacity-60 aria-expanded:bg-raised ${joined ? `rounded-r-lg ${joinedPart}` : 'h-7.5 rounded-lg border border-border'}`}
    >
      {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
      {problem !== '' && <AlertIcon className="size-3 shrink-0 text-warning" />}
      <ChevronDownIcon className="size-3 shrink-0" />
    </button>
  )

  // split is the agent slot's one control: its parts, the first narrowing and cutting its
  // words short when the action column is short, then the ▾, always last, so the ▾ sits at
  // the slot's right edge whatever the session's state.
  const split = (parts: ReactNode) => (
    <div className="inline-flex h-7.5 max-w-full min-w-0 items-stretch text-[0.8125rem]">
      {parts}
      {cardOptions(true)}
    </div>
  )

  // cardSlot is the card's agent slot, by the card's session, one split control closed by
  // the ▾: with none, the launch (its prompt's nickname for a situation or an issue, else
  // the agent's name), which opens the launch options rather than starting at once; while
  // one starts or works, a pill with its state and age that opens
  // the item's sessions, its border running slowly round, then Show in terminal, or Watch
  // for a background run; once its turn is done, a pill that shows its terminal; once it
  // ended, a pill that opens what it found, the item's sheet, or the run's view for a
  // background run. Waiting on the user or failed today, the card's primary is the way in,
  // and the slot has only the ▾; a session that failed longer ago leaves the card to the
  // item's own move, and the slot to a fresh launch.
  const cardSlot = () => {
    const session = running?.state === 'failed' && !sessionFresh ? null : running

    if (session === null) {
      // Until the agent is known, the launch names "the agent", so it never shares the
      // name Open with the card's primary beside it. It opens the launch options, as the ▾
      // does.
      return split(
        <button
          type="button"
          disabled={busy || resolution === null}
          onClick={showOptions}
          aria-label={
            agentName === '' ? 'Open in the agent' : nickname === '' ? openLabel : `${openLabel}: ${nickname}`
          }
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-controls={open ? id : undefined}
          title={firstTitle}
          className={`inline-flex min-w-0 items-center gap-1.5 rounded-l-lg border border-border pr-1.75 pl-2.25 font-semibold text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent ${partRing}`}
        >
          <SparkIcon className="size-3.5 shrink-0 text-accent" />
          <span className="min-w-0 truncate @max-3xl:sr-only">
            {state.kind === 'launching' || state.kind === 'linking'
              ? 'Opening…'
              : state.kind === 'cloning'
                ? 'Cloning…'
                : nickname !== ''
                  ? nickname
                  : agentName === ''
                    ? 'Agent'
                    : agentName}
          </span>
          {untrusted && (
            <span title="Launches read-only: an outside contributor’s" className="inline-flex shrink-0">
              <LockIcon className="size-3 shrink-0 text-warning" />
            </span>
          )}
        </button>,
      )
    }

    const name = agentLabel(agents, session.agent)
    const label = stateLabel(session.state)
    const since = sinceWhen(session)
    // A pill, the split's first part, narrows within the slot, its mark held and its words
    // cut short, so nothing of it ever runs past its border.
    const pill = `inline-flex min-w-0 items-center gap-1.75 rounded-l-lg px-2.5 font-semibold ${partRing}`
    const done = `${pill} border border-success/30 bg-success/11 text-success transition-colors hover:bg-success/18`
    // words are a pill's words, then how long the session has been as it is, after a dot
    // when dotted, as one line that truncates at its end, so the age is cut before the
    // words are; a list narrower than 48rem leaves the age out (and CardActions the words,
    // data-pill-label, but for screen readers).
    const words = (text: string, dotted: boolean) => (
      <span data-pill-label className="min-w-0 truncate">
        {text}
        {since !== '' && (
          <>
            {' '}
            <span className="@max-3xl:hidden">
              {dotted && '· '}
              <RelativeTime iso={since} show={formatAge} className="font-medium text-muted tabular-nums" />
            </span>
          </>
        )}
      </span>
    )
    // iconPart is a part between the pill and the ▾: an icon button, as wide as it is tall.
    const iconPart = `inline-flex w-7.5 shrink-0 items-center justify-center text-muted transition-colors hover:bg-raised hover:text-text disabled:opacity-60 disabled:hover:bg-transparent ${joinedPart}`

    switch (session.state) {
      case 'starting':
      case 'working':
      case 'unknown':
        return split(
          <>
            <SessionsButton
              sessions={sessions ?? [session]}
              agents={agents}
              align="right"
              shrinks
              label={`${name} session: ${label}. Show the item's sessions`}
              title={session.detail === '' ? `${name} ${label}` : `${name} ${label}: ${session.detail}`}
              className={`${pill} border-run ${stateText[session.state]} motion-safe:animate-border-run`}
            >
              <SessionDot state={session.state} />
              {words(`${name} ${label}`, false)}
            </SessionsButton>
            {session.runId !== 0 ? (
              <button
                type="button"
                aria-label="Watch"
                disabled={busy || watch === undefined}
                onClick={() => {
                  watch?.(session.id)
                }}
                title={`${name} ${label} in the background, for Autopilot: watch it, stop it, or take it over from its run.`}
                className={iconPart}
              >
                <AutopilotIcon className="size-3.5" />
              </button>
            ) : (
              <button
                type="button"
                aria-label="Show in terminal"
                disabled={busy || !session.canFocus}
                onClick={() => {
                  void showTerminal(session.id)
                }}
                title={
                  session.canFocus
                    ? `${name} ${label}: show its terminal tab. To start another session, use the launch options.`
                    : `${name} is starting; its terminal tab can be shown once it has started.`
                }
                className={iconPart}
              >
                <TerminalIcon className="size-3.5" />
              </button>
            )}
          </>,
        )
      case 'idle':
        return split(
          session.canFocus ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                void showTerminal(session.id)
              }}
              title={`${name}’s turn is done: show its terminal tab`}
              className={done}
            >
              <SessionDot state={session.state} />
              {words(`${name}’s done`, true)}
            </button>
          ) : (
            <SessionsButton
              sessions={sessions ?? [session]}
              agents={agents}
              align="right"
              shrinks
              title={`${name}’s turn is done`}
              className={done}
            >
              <SessionDot state={session.state} />
              {words(`${name}’s done`, true)}
            </SessionsButton>
          ),
        )
      case 'ended': {
        const background = session.runId !== 0 && watch !== undefined

        return split(
          <button
            type="button"
            onClick={() => {
              if (background) {
                watch(session.id)
              } else if (onOpen === undefined) {
                openExternal(item.url)
              } else {
                onOpen()
              }
            }}
            title={`${name}’s session ended: read what it found`}
            className={done}
          >
            <SparkIcon className="size-3.5 shrink-0" />
            {words('Findings', true)}
          </button>,
        )
      }
      case 'needs_approval':
      case 'needs_input':
      case 'failed':
        return cardOptions(false)
    }
  }

  // outcomes say how the last launch went, in the grid the controls render into
  // (outcomeClass), with the dialogs a launch can open.
  const outcomes = (
    <>
      {state.kind === 'launched' && state.result.notice !== '' && (
        <p role="status" className={`${outcomeClass} flex items-start gap-1.5 text-xs text-warning`}>
          <AlertIcon className="mt-0.5 size-3.5 shrink-0" />
          <span>{state.result.notice}</span>
        </p>
      )}
      {state.kind === 'failed' && (
        <p role="alert" className={`${outcomeClass} text-xs text-danger`}>
          {state.message}
        </p>
      )}
      {state.kind === 'cloning' && (
        <p role="status" className={`${outcomeClass} truncate text-xs text-muted`}>
          {`Cloning ${state.target.repo} into ${state.target.dir}…`}
        </p>
      )}
      {state.kind === 'noClone' && (
        <div className={`${outcomeClass} flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs`}>
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
        <p role="status" className={`${outcomeClass} truncate text-xs text-muted`}>
          {`Adding your clone of ${state.target.repo}…`}
        </p>
      )}
      {state.kind === 'addLocation' && (
        <AddLocation
          className={outcomeClass}
          linked={state.linked}
          onAdd={(folder) => {
            void addThenLaunch([folder], state.target, state.request)
          }}
          onCancel={() => {
            setState({ kind: 'noClone', target: state.target, request: state.request, error: '' })
          }}
        />
      )}
      {/* The hold and take-over dialogs load on their own, as the launch options do, each
          under its own boundary, with nothing in its place in the grid until it shows. */}
      {state.kind === 'held' && (
        <Suspense fallback={null}>
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
            onWatch={
              watch === undefined
                ? undefined
                : (sessionId) => {
                    setState({ kind: 'idle' })
                    watch(sessionId)
                  }
            }
            onTakeOver={(sessionId) => {
              setTakingOver({ sessionId, where: state.hold.worktree })
              setState({ kind: 'idle' })
            }}
          />
        </Suspense>
      )}
      {takingOver !== null && (
        <Suspense fallback={null}>
          <TakeOverDialog
            sessionId={takingOver.sessionId}
            live
            terminal={takeOverIn}
            where={takingOver.where}
            agentName={agentName}
            onCancel={() => {
              setTakingOver(null)
            }}
            onTaken={(result) => {
              setTakingOver(null)
              setState({ kind: 'launched', result })
            }}
          />
        </Suspense>
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

  if (variant === 'card') {
    // The slot's control narrows within the agent's part of the action column, its first
    // part giving way, and never past its border.
    const slot = (
      <span className="inline-flex max-w-full min-w-0 items-center">
        {cardSlot()}
        {dialog}
      </span>
    )

    // peekLaunch is the peek's way to the launch options: the agent's name, opening the
    // same dialog as the slot's arrow.
    const peekLaunch = (
      <button
        type="button"
        disabled={busy || resolution === null}
        title={optionsTitle}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={showOptions}
        className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 aria-expanded:bg-raised"
      >
        <SparkIcon className="size-3.5 shrink-0 text-accent" />
        {agentName === '' ? 'Launch' : agentName}…
      </button>
    )

    return (
      <>
        {column === undefined ? slot : column(slot)}
        {peek?.(peekLaunch)}
        {outcomes}
      </>
    )
  }

  if (variant === 'row') {
    // The row's verb opens the launch options, as the split button does, with no arrow: the
    // dot of a choice changed for this launch, which its name says too, and the alert of a
    // prompt that can't resolve, ride on it instead.
    return (
      <>
        <div className={boxClass}>
          <button
            type="button"
            disabled={busy || resolution === null}
            onClick={showOptions}
            aria-label={changed ? `${opening}, changed for this launch` : undefined}
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls={open ? id : undefined}
            title={firstTitle}
            className="inline-flex h-6 items-center gap-1.25 rounded-md border border-border bg-surface px-2.25 text-xs font-semibold whitespace-nowrap text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-surface aria-expanded:bg-raised"
          >
            {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
            {problem !== '' && <AlertIcon className="size-3 shrink-0 text-warning" />}
            {opening}
          </button>
          {dialog}
        </div>
        {outcomes}
      </>
    )
  }

  return (
    <>
      <div className={boxClass}>
        <div className="inline-flex items-stretch rounded-md border border-border bg-surface text-[0.8125rem]">
          {running === null ? (
            <button
              type="button"
              disabled={busy || resolution === null}
              onClick={showOptions}
              aria-haspopup="dialog"
              aria-expanded={open}
              aria-controls={open ? id : undefined}
              title={firstTitle}
              className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent"
            >
              <TerminalIcon className="size-3.5 text-accent" />
              {opening}
            </button>
          ) : running.runId !== 0 ? (
            <button
              type="button"
              disabled={busy || watch === undefined}
              onClick={() => {
                watch?.(running.id)
              }}
              title={`${agentLabel(agents, running.agent)} ${stateLabel(running.state)} in the background, for Autopilot: watch it, stop it, or take it over from its run.`}
              className="inline-flex items-center gap-1.5 rounded-l-md px-2.5 py-1 font-medium text-text transition-colors hover:bg-raised disabled:opacity-60 disabled:hover:bg-transparent"
            >
              <AutopilotIcon className="size-3.5 text-accent" />
              {state.kind === 'launching' ? 'Opening…' : 'Watch'}
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
            aria-label={optionsLabel}
            title={optionsTitle}
            aria-haspopup="dialog"
            aria-expanded={open}
            aria-controls={open ? id : undefined}
            onClick={showOptions}
            className="inline-flex max-w-52 items-center gap-1 rounded-r-md border-l border-border px-2 text-xs text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
          >
            {changed && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-accent" />}
            {problem !== '' && <AlertIcon className="size-3 shrink-0 text-warning" />}
            <span className={`truncate ${changed ? 'font-medium text-text' : ''}`}>{chip}</span>
            <ChevronDownIcon className="size-3 shrink-0" />
          </button>
        </div>
        {dialog}
      </div>
      {outcomes}
    </>
  )
}

interface AddLocationProps {
  // className places it in the grid the launch controls render into.
  className: string
  linked: LinkedClone
  // onAdd adds the folder as a repository location: the clone's own, or the one that
  // holds it.
  onAdd: (folder: string) => void
  onCancel: () => void
}

// AddLocation asks, for a clone found in a folder that holds other clones, whether to add
// that folder as a repository location, which brings its other clones in too, or only the
// clone.
function AddLocation({ className, linked, onAdd, onCancel }: AddLocationProps) {
  const { parent } = linked.location
  const others = `${String(parent.clones)} other ${parent.clones === 1 ? 'clone' : 'clones'}`

  return (
    <div className={`${className} flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs`}>
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
