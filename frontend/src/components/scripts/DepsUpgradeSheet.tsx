import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

import { all, attempt } from '../../lib/call'
import { agentName as agentLabel, promptApplies } from '../../lib/launch'
import { noChoices, repoLaunchRequest, type Choices } from '../../lib/launchChoices'
import { kindNames } from '../../lib/manifests'
import { useQuery } from '../../lib/useQuery'
import {
  focusSession,
  launchRepo,
  listAgents,
  listProfiles,
  listPrompts,
  previewRepoLaunch,
  resolveRepoLaunch,
  type LaunchHold,
  type LaunchResult,
  type ManifestKind,
  type ManifestsClone,
  type ManifestTool,
  type Prompt,
  type RepoLaunchRequest,
  type WorktreePlan,
} from '../../platform'
import { buttonClass, ErrorText, fieldClass, linkClass, primaryButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, BoxIcon, InfoIcon, TerminalIcon } from '../icons'
import { LaunchFrame } from '../LaunchDialog'
import { LaunchHoldDialog } from '../LaunchHoldDialog'
import { LaunchFields, launchSectionTitle } from '../LaunchMenu'
import { LaunchPreviewSection, type LaunchSubject } from '../LaunchPreviewSection'
import { Spinner } from '../Spinner'

export interface DepsUpgradeSheetProps {
  // clone is the clone to upgrade, as the Deps page lists it.
  clone: ManifestsClone
  // onOpenPrompts opens the Prompts tab, where the upgrade's prompts are edited: on the
  // prompt promptId, when an Edit names one.
  onOpenPrompts: (promptId?: number) => void
  // onClose closes the dialog; an upgrade it started goes on in the terminal.
  onClose: () => void
}

// upgrades are the two upgrades the dialog offers, each the starter prompt for it, which the
// library holds under the starter's key until the user deletes it: what the dialog calls it,
// and what it does.
const upgrades = [
  {
    starter: 'deps-minor',
    label: 'Minor and patch',
    about: 'Newest versions within each major; the PR lists the majors found.',
  },
  {
    starter: 'deps-major',
    label: 'Majors too',
    about: 'Also moves to new majors, refactoring code where needed. Bigger, and it may break things.',
  },
] as const

// ManifestGroup is one kind of module a clone holds: the paths of its manifests, and the
// tools the clone is set up for that work on it.
interface ManifestGroup {
  kind: ManifestKind
  paths: string[]
  tools: ManifestTool[]
}

// manifestGroups groups a clone's manifests by kind, Go's first, each in the clone's order
// with the tools that work on it: magex on Go's, the package managers on JavaScript's.
function manifestGroups(clone: ManifestsClone): ManifestGroup[] {
  const paths = (kind: ManifestKind) => clone.modules.filter((m) => m.kind === kind).map((m) => m.path)
  const groups: ManifestGroup[] = [
    { kind: 'go', paths: paths('go'), tools: clone.tools.filter((t) => t === 'magex') },
    { kind: 'js', paths: paths('js'), tools: clone.tools.filter((t) => t !== 'magex') },
  ]

  return groups.filter((g) => g.paths.length > 0)
}

// worktreeSentence says where the upgrade runs: in a new worktree, by the name and from the
// base the plan gives it once there is one, never in the clone itself.
function worktreeSentence(plan: WorktreePlan | null): string {
  if (plan === null) {
    return 'Runs in a new worktree off the default branch. Your clone isn’t touched.'
  }

  return `Runs in a new worktree, ${plan.name}, off ${plan.base}. Your clone isn’t touched.`
}

// StartState is where the upgrade's start stands. One the app held back for a session
// running where it would start ends in held, which keeps the request to send again; one
// that started ends in started only when its launch worked something around, which the
// dialog stays open to say.
type StartState =
  | { kind: 'idle' }
  | { kind: 'starting' }
  | { kind: 'held'; hold: LaunchHold; request: RepoLaunchRequest }
  | { kind: 'started'; result: LaunchResult }
  | { kind: 'failed'; message: string }

// DepsUpgradeSheet launches an agent that upgrades one clone's dependencies in a new
// worktree made from origin's default branch, and opens a pull request with the result. It
// is a dialog the size of what it holds, in the launch options' own frame (LaunchFrame), as
// an item's launch options are. As it opens, it reads the prompt library, the agents, and
// their profiles, and offers the upgrade's two starters, Minor and patch (chosen) and Majors
// too, each while the library keeps it, each with an Edit that opens it on the Prompts page,
// and any other prompt for repositories the user wrote; with none at all, it says so and
// sends the user to the Prompts page. It resolves the launch with the chosen prompt and
// plans it, again as the prompt, agent, or profile changes, as the launch options do an
// item's: the resolution shows each setting's default and what the dialog says of the
// repository first (its notice), and the plan names the worktree it makes. The agent, its
// profile, model, and effort, plan mode, and Fast mode are the launch options' own
// (LaunchFields), with a preview of exactly what it runs.
//
// A refusal, of the repository, its clone, the prompt, or the worktree, shows as the
// dialog's error, and Start waits until the choices resolve and plan again. A start held
// back for a session running where it would start asks first (LaunchHoldDialog), and its
// Start another starts beside it. A start says nothing once it starts, as a launch from a
// card says nothing: the dialog closes, and the Deps page's row follows the session. Only
// what the launch worked around keeps the dialog open, to say it.
export function DepsUpgradeSheet({ clone, onOpenPrompts, onClose }: DepsUpgradeSheetProps) {
  const id = useId()
  const [choices, setChoices] = useState<Choices>(noChoices)
  const [state, setState] = useState<StartState>({ kind: 'idle' })
  // showing says the dialog is still open, so a start that ends after it closed closes
  // nothing: by then its host may show another.
  const showing = useRef(true)

  useEffect(() => {
    showing.current = true

    return () => {
      showing.current = false
    }
  }, [])

  const config = useQuery(() => all([listPrompts(), listAgents(), listProfiles()]), [])
  const library = config.data?.[0] ?? []
  const agents = config.data?.[1] ?? []
  const profiles = config.data?.[2] ?? []
  // The prompts for repositories: each upgrade's starter while the library keeps it, then
  // every other one, the user's own.
  const repoPrompts = library.filter((p) => promptApplies(p, ''))
  const starters = upgrades.flatMap((u) => {
    const prompt = repoPrompts.find((p) => p.starterKey === u.starter)

    return prompt === undefined ? [] : [{ ...u, prompt }]
  })
  const others = repoPrompts.filter((p) => !starters.some((s) => s.prompt.id === p.id))
  // promptId is the prompt chosen, else the first upgrade's starter, else another prompt
  // for repositories; 0 while the library loads, or when it holds none.
  const promptId = choices.promptId !== 0 ? choices.promptId : (starters[0]?.prompt.id ?? others[0]?.id ?? 0)
  const other = others.find((p) => p.id === promptId)

  // asked is what the dialog resolves and plans as its choices change: the prompt, agent,
  // and profile chosen, under which each other setting's default is worked out, as the
  // launch options re-resolve an item's. Each answer is the latest request's alone.
  const asked = repoLaunchRequest(clone.repo, clone.dir, promptId, {
    ...noChoices,
    agent: choices.agent,
    profile: choices.profile,
  })
  const askedKey = JSON.stringify(asked)
  const resolved = useQuery(() => resolveRepoLaunch(asked), [askedKey], { enabled: promptId !== 0 })
  // planned is the same launch planned, for the worktree it makes and anything that would
  // refuse it there, such as every name for today's worktree being taken.
  const planned = useQuery(() => previewRepoLaunch(asked), [askedKey], { enabled: promptId !== 0 })
  const resolution = resolved.data
  const resolveFailed = !resolved.pending && resolved.error !== ''
  const planFailed = !planned.pending && planned.error !== ''
  const starting = state.kind === 'starting'
  // Start waits for the plan too, so a refusal only the plan finds, as every name for
  // today's worktree being taken, shows before Start rather than after it.
  const canStart =
    resolution !== undefined && !resolved.pending && !planned.pending && !resolveFailed && !planFailed && !starting
  const error =
    state.kind === 'failed' ? state.message : resolveFailed ? resolved.error : planFailed ? planned.error : ''
  // agentName names the agent the upgrade starts: the one chosen, else the one resolved.
  const agentName = choices.agent !== '' ? agentLabel(agents, choices.agent) : (resolution?.agent.name ?? '')
  const groups = manifestGroups(clone)
  const notice = resolution?.notice ?? ''
  // subject is the repository as the launch options see it: trusted, always in a worktree
  // of its own, and planned for its clone with the prompt chosen.
  const subject: LaunchSubject = {
    kind: '',
    trusted: true,
    key: JSON.stringify([clone.repo, clone.dir]),
    preview: (c) => previewRepoLaunch(repoLaunchRequest(clone.repo, clone.dir, c.promptId, c)),
  }

  // choose takes the choices made, and drops why the last start failed, which was for
  // others.
  const choose = (next: Choices) => {
    setChoices(next)

    if (state.kind === 'failed') {
      setState({ kind: 'idle' })
    }
  }

  // openPrompts closes the dialog for the Prompts page, where its prompts are edited, open
  // on the prompt target when an Edit names one.
  const openPrompts = (target?: number) => {
    onClose()
    onOpenPrompts(target)
  }

  // run starts the upgrade request asks for. A start held back asks the user first; one
  // that starts closes the dialog, unless its launch worked something around.
  const run = async (request: RepoLaunchRequest) => {
    setState({ kind: 'starting' })

    const launched = await attempt(launchRepo(request))

    if (!launched.ok) {
      setState({ kind: 'failed', message: launched.error })

      return
    }

    const result = launched.data

    if (result.hold !== null) {
      setState({ kind: 'held', hold: result.hold, request })
    } else if (result.notice !== '') {
      setState({ kind: 'started', result })
    } else if (showing.current) {
      onClose()
    }
  }

  // show brings a running session's terminal tab forward instead of starting another,
  // saying why when it can't.
  const show = async (sessionId: string) => {
    const shown = await attempt(focusSession(sessionId))

    setState(shown.ok ? { kind: 'idle' } : { kind: 'failed', message: shown.error })
  }

  // edit is a prompt's Edit, which opens it on the Prompts page.
  const edit = (p: Pick<Prompt, 'id' | 'nickname'>) => (
    <button
      type="button"
      onClick={() => {
        openPrompts(p.id)
      }}
      aria-label={`Edit “${p.nickname}”`}
      title="Edit it on the Prompts page"
      className={`${linkClass} shrink-0 text-xs`}
    >
      Edit
    </button>
  )

  // choice is the upgrade to run, the dialog's first section, as the launch options head
  // theirs: each of the upgrade's starters the library keeps, with what it does, then the
  // user's other prompts for repositories in a select.
  const choice = (
    <div className="px-1.5 pt-2 pb-1">
      <p id={`${id}-upgrade`} className={launchSectionTitle}>
        Upgrade
      </p>
      <div className="flex flex-col gap-2 px-2 pb-1">
        {starters.length > 0 && (
          <div role="radiogroup" aria-labelledby={`${id}-upgrade`} className="flex flex-col gap-2">
            {starters.map((s) => (
              <div key={s.starter} className="flex items-start gap-2">
                <input
                  type="radio"
                  id={`${id}-${s.starter}-choice`}
                  name={`${id}-prompt`}
                  value={s.prompt.id}
                  checked={promptId === s.prompt.id}
                  aria-describedby={`${id}-${s.starter}`}
                  onChange={() => {
                    choose({ ...choices, promptId: s.prompt.id })
                  }}
                  className="radio mt-0.5"
                />
                <div className="min-w-0 flex-1">
                  {/* The label names the radio alone: a label holds no button, so Edit sits
                      beside it. */}
                  <div className="flex items-baseline justify-between gap-2">
                    <label htmlFor={`${id}-${s.starter}-choice`} className="cursor-pointer font-medium select-none">
                      {s.label}
                    </label>
                    {edit(s.prompt)}
                  </div>
                  <p id={`${id}-${s.starter}`} className="text-xs text-muted">
                    {s.about}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
        {others.length > 0 && (
          <div className="flex items-center gap-2">
            <label htmlFor={`${id}-other`} className="shrink-0 text-xs text-muted">
              {starters.length > 0 ? 'Another prompt' : 'Prompt'}
            </label>
            {/* While a starter is chosen, the select shows that none of these is, and a pick
                in it chooses that prompt instead. */}
            <select
              id={`${id}-other`}
              value={other?.id ?? 0}
              onChange={(e) => {
                choose({ ...choices, promptId: Number(e.target.value) })
              }}
              className={`${fieldClass} min-w-0 flex-1`}
            >
              {other === undefined && (
                <option value={0} disabled>
                  Choose a prompt
                </option>
              )}
              {others.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.nickname}
                </option>
              ))}
            </select>
            {other !== undefined && edit(other)}
          </div>
        )}
      </div>
    </div>
  )

  // body is the upgrade to run, then the launch's own settings, its worktree, and its
  // preview, once the library has loaded and the launch has resolved.
  let body: ReactNode

  if (config.data === undefined) {
    body = (
      <div className="px-3.5 py-3">
        {config.error === '' ? (
          <p role="status" className="flex items-center gap-2 text-muted">
            <Spinner />
            Loading the launch settings…
          </p>
        ) : (
          <ErrorBanner>Couldn’t load the launch settings: {config.error}</ErrorBanner>
        )}
      </div>
    )
  } else if (promptId === 0) {
    body = (
      <div className="flex flex-col gap-2 px-3.5 py-3">
        <p className="flex items-start gap-2 text-warning">
          <AlertIcon className="mt-0.5 size-4 shrink-0" />
          No prompt in your library is for repositories. Write one on the Prompts page to upgrade with it.
        </p>
        <button
          type="button"
          onClick={() => {
            openPrompts()
          }}
          className={`${linkClass} self-start`}
        >
          Open the Prompts page
        </button>
      </div>
    )
  } else {
    body = (
      <>
        {choice}
        {resolution === undefined ? (
          resolved.pending && (
            <p role="status" className="mt-1 flex items-center gap-2 border-t border-border px-3.5 py-3 text-muted">
              <Spinner />
              Working out the launch settings…
            </p>
          )
        ) : (
          // The launch options' own settings and preview, under the upgrade as under an
          // item's prompt. The worktree a repository's launch always makes is said in place
          // of the choice an item's offers.
          <>
            <div className="mt-1 border-t border-border">
              <LaunchFields
                subject={subject}
                resolved={resolution}
                choices={choices}
                onChoices={choose}
                agents={agents}
                profiles={profiles}
                resolveError=""
                worktree={<p className="text-text">{worktreeSentence(planned.data?.worktree ?? null)}</p>}
              />
            </div>
            <LaunchPreviewSection subject={subject} choices={{ ...choices, promptId }} label="Preview the launch" />
          </>
        )}
      </>
    )
  }

  return (
    <LaunchFrame
      id={id}
      label="Upgrade dependencies"
      title="Upgrade dependencies"
      about={
        <>
          <BoxIcon className="size-3.5 shrink-0" />
          <span className="truncate">
            <span className="font-medium text-text">{clone.repo}</span>
            {` · ${clone.dir}`}
          </span>
        </>
      }
      aboutTitle={`${clone.repo} · ${clone.dir}`}
      onClose={onClose}
    >
      {/* No column here takes flex-1, which the desktop's WebKit would draw empty (see
          LaunchFrame): the choices scroll, and the buttons stay in view under them. */}
      <div className="flex min-h-0 flex-col text-sm">
        <div className="min-h-0 overflow-y-auto overscroll-contain">
          {state.kind === 'started' ? (
            <div role="status" className="flex flex-col gap-2 px-3.5 py-3">
              <p className="text-text">{`The upgrade started in ${state.result.worktree}.`}</p>
              <p className="flex items-start gap-1.5 text-warning">
                <AlertIcon className="mt-0.5 size-3.5 shrink-0" />
                <span>{state.result.notice}</span>
              </p>
            </div>
          ) : (
            <>
              {(groups.length > 0 || notice !== '') && (
                <div className="flex flex-col gap-1.5 px-3.5 pt-3 pb-2">
                  {groups.length > 0 && (
                    <ul aria-label="Manifests" className="flex flex-wrap gap-x-1.5 gap-y-1 text-text">
                      {groups.map((g, i) => (
                        <li key={g.kind}>
                          {i > 0 && (
                            <span aria-hidden="true" className="mr-1.5 text-muted">
                              ·
                            </span>
                          )}
                          <span className="font-medium">{kindNames[g.kind]}:</span>{' '}
                          <span className="font-mono text-xs">{g.paths.join(', ')}</span>
                          {g.tools.length > 0 && <span className="text-muted">{` (${g.tools.join(', ')})`}</span>}
                        </li>
                      ))}
                    </ul>
                  )}
                  {notice !== '' && (
                    <p className="flex items-start gap-1.5 text-xs text-muted">
                      <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
                      {notice}
                    </p>
                  )}
                </div>
              )}
              <div className={groups.length > 0 || notice !== '' ? 'border-t border-border' : ''}>{body}</div>
            </>
          )}
        </div>
        <div className="flex shrink-0 flex-col gap-2 border-t border-border px-3 py-2.5">
          <ErrorText error={error} />
          <div className="flex items-center justify-end gap-2">
            {state.kind === 'started' ? (
              <button type="button" onClick={onClose} className={buttonClass}>
                Done
              </button>
            ) : (
              <>
                <button type="button" onClick={onClose} className={`${buttonClass} shrink-0`}>
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={!canStart}
                  onClick={() => {
                    void run(repoLaunchRequest(clone.repo, clone.dir, promptId, choices))
                  }}
                  className={`${primaryButtonClass} shrink-0`}
                >
                  <TerminalIcon className="size-4" />
                  {starting ? 'Starting the upgrade…' : 'Start upgrade'}
                </button>
              </>
            )}
          </div>
        </div>
      </div>
      {state.kind === 'held' && (
        <LaunchHoldDialog
          hold={state.hold}
          agentName={agentName}
          agents={agents}
          item={{ repo: clone.repo, number: 0 }}
          onCancel={() => {
            setState({ kind: 'idle' })
          }}
          onConfirm={() => {
            void run({ ...state.request, alongside: state.hold.key })
          }}
          onShow={(sessionId) => {
            void show(sessionId)
          }}
        />
      )}
    </LaunchFrame>
  )
}
