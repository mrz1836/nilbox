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
  type RepoLaunchRequest,
  type WorktreePlan,
} from '../../platform'
import { buttonClass, ErrorText, fieldClass, iconButtonClass, linkClass, primaryButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, InfoIcon, TerminalIcon, XIcon } from '../icons'
import { LaunchHoldDialog } from '../LaunchHoldDialog'
import { LaunchFields } from '../LaunchMenu'
import { LaunchPreviewSection, type LaunchSubject } from '../LaunchPreviewSection'
import { Sheet } from '../Sheet'
import { Spinner } from '../Spinner'

export interface DepsUpgradeSheetProps {
  // clone is the clone to upgrade, as the Deps page lists it.
  clone: ManifestsClone
  // onOpenPrompts opens the Prompts tab, where the upgrade's prompts are edited.
  onOpenPrompts: () => void
  // onClose closes the sheet; an upgrade it started goes on in the terminal.
  onClose: () => void
}

// upgrades are the two upgrades the sheet offers, each the starter prompt for it, which the
// library holds under the starter's key until the user deletes it: what the sheet calls it,
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
// sheet stays open to say.
type StartState =
  | { kind: 'idle' }
  | { kind: 'starting' }
  | { kind: 'held'; hold: LaunchHold; request: RepoLaunchRequest }
  | { kind: 'started'; result: LaunchResult }
  | { kind: 'failed'; message: string }

// DepsUpgradeSheet launches an agent that upgrades one clone's dependencies in a new
// worktree made from origin's default branch, and opens a pull request with the result. As
// it opens, it reads the prompt library, the agents, and their profiles, and offers the
// upgrade's two starters, Minor and patch (chosen) and Majors too, each while the library
// keeps it, and any other prompt for repositories the user wrote; with none at all, it says
// so and sends the user to the Prompts page. It resolves the launch with the chosen prompt
// and plans it, again as the prompt, agent, or profile changes, as the launch options do an
// item's: the resolution shows each setting's default and what the sheet says of the
// repository first (its notice), and the plan names the worktree it makes. The agent, its
// profile, model, and effort, plan mode, and Fast mode are the launch options' own
// (LaunchFields), with a preview of exactly what it runs.
//
// A refusal, of the repository, its clone, the prompt, or the worktree, shows as the
// sheet's error, and Start waits until the choices resolve and plan again. A start held back
// for a session running where it would start asks first (LaunchHoldDialog), and its Start
// another starts beside it. A start says nothing once it starts, as a launch from a card
// says nothing: the sheet closes, and the Deps page's row follows the session. Only what the
// launch worked around keeps the sheet open, to say it.
export function DepsUpgradeSheet({ clone, onOpenPrompts, onClose }: DepsUpgradeSheetProps) {
  const id = useId()
  const [choices, setChoices] = useState<Choices>(noChoices)
  const [state, setState] = useState<StartState>({ kind: 'idle' })
  // showing says the sheet is still open, so a start that ends after it closed closes
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

  // asked is what the sheet resolves and plans as its choices change: the prompt, agent,
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

  // openPrompts closes the sheet for the Prompts page, where its prompts are edited.
  const openPrompts = () => {
    onClose()
    onOpenPrompts()
  }

  // run starts the upgrade request asks for. A start held back asks the user first; one
  // that starts closes the sheet, unless its launch worked something around.
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

  // body is the prompts to upgrade with, then the launch's own settings, its worktree, and
  // its preview, once the library has loaded and the launch has resolved.
  let body: ReactNode

  if (config.data === undefined) {
    body =
      config.error === '' ? (
        <p role="status" className="flex items-center gap-2 text-sm text-muted">
          <Spinner />
          Loading the launch settings…
        </p>
      ) : (
        <ErrorBanner>Couldn’t load the launch settings: {config.error}</ErrorBanner>
      )
  } else if (promptId === 0) {
    body = (
      <div className="flex flex-col gap-2 text-sm">
        <p className="flex items-start gap-2 text-warning">
          <AlertIcon className="mt-0.5 size-4 shrink-0" />
          No prompt in your library is for repositories. Write one on the Prompts page to upgrade with it.
        </p>
        <button type="button" onClick={openPrompts} className={`${linkClass} self-start`}>
          Open the Prompts page
        </button>
      </div>
    )
  } else {
    body = (
      <>
        {/* The prompts keep to a column a line can be read across, Edit near what it edits,
            however wide the sheet. */}
        <div className="flex max-w-3xl flex-col gap-2">
          {starters.length > 0 && (
            <div role="radiogroup" aria-label="Upgrade" className="flex flex-col gap-2">
              {starters.map((s) => (
                <div key={s.starter} className="flex items-baseline gap-3">
                  {/* The label holds the radio alone: a label holds no button, so Edit sits
                      beside it. */}
                  <label className="flex w-40 shrink-0 cursor-pointer items-center gap-2 text-sm font-medium select-none">
                    <input
                      type="radio"
                      name={`${id}-prompt`}
                      value={s.prompt.id}
                      checked={promptId === s.prompt.id}
                      aria-describedby={`${id}-${s.starter}`}
                      onChange={() => {
                        choose({ ...choices, promptId: s.prompt.id })
                      }}
                      className="radio"
                    />
                    {s.label}
                  </label>
                  <p id={`${id}-${s.starter}`} className="min-w-0 flex-1 text-sm text-muted">
                    {s.about}
                  </p>
                  <button
                    type="button"
                    onClick={openPrompts}
                    aria-label={`Edit “${s.prompt.nickname}”`}
                    title="Edit it on the Prompts page"
                    className={`${linkClass} shrink-0 text-xs`}
                  >
                    Edit
                  </button>
                </div>
              ))}
            </div>
          )}
          {others.length > 0 && (
            <div className="flex items-center gap-3">
              <label
                htmlFor={`${id}-other`}
                className={`w-40 shrink-0 text-sm font-medium ${starters.length > 0 ? 'pl-6' : ''}`}
              >
                {starters.length > 0 ? 'Another prompt' : 'Prompt'}
              </label>
              {/* While a starter is chosen, the select shows that none of these is, and a
                  pick in it chooses that prompt instead. */}
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
              {other !== undefined && (
                <button
                  type="button"
                  onClick={openPrompts}
                  aria-label={`Edit “${other.nickname}”`}
                  title="Edit it on the Prompts page"
                  className={`${linkClass} shrink-0 text-xs`}
                >
                  Edit
                </button>
              )}
            </div>
          )}
        </div>
        {resolution === undefined ? (
          resolved.pending && (
            <p role="status" className="flex items-center gap-2 text-sm text-muted">
              <Spinner />
              Working out the launch settings…
            </p>
          )
        ) : (
          // The launch options' own settings and preview, as in their dialog, in a card of
          // their own. The worktree a repository's launch always makes is said in place of
          // the choice an item's offers.
          <div className="flex max-w-xl flex-col rounded-lg border border-border text-sm">
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
            <LaunchPreviewSection subject={subject} choices={{ ...choices, promptId }} label="Preview the launch" />
          </div>
        )}
      </>
    )
  }

  return (
    <Sheet labelledBy={`${id}-title`} onClose={onClose}>
      <header className="flex items-start justify-between gap-3 border-b border-border px-5 py-3.5">
        <div className="min-w-0">
          <h2 id={`${id}-title`} className="truncate text-base font-semibold">
            Upgrade dependencies
          </h2>
          <p title={`${clone.repo} · ${clone.dir}`} className="mt-0.5 truncate text-xs text-muted">
            <span className="font-medium text-text">{clone.repo}</span> · {clone.dir}
          </p>
        </div>
        <button
          type="button"
          aria-label="Close the upgrade sheet"
          title="Close"
          onClick={onClose}
          className={`${iconButtonClass} -mr-1`}
        >
          <XIcon className="size-3.5" />
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-5 py-4">
        {state.kind === 'started' ? (
          <div role="status" className="flex flex-col gap-2 text-sm">
            <p className="text-text">{`The upgrade started in ${state.result.worktree}.`}</p>
            <p className="flex items-start gap-1.5 text-warning">
              <AlertIcon className="mt-0.5 size-3.5 shrink-0" />
              <span>{state.result.notice}</span>
            </p>
          </div>
        ) : (
          <>
            {groups.length > 0 && (
              <ul aria-label="Manifests" className="flex flex-wrap gap-x-1.5 gap-y-1 text-sm text-text">
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
            {resolution !== undefined && resolution.notice !== '' && (
              <p className="flex items-start gap-1.5 text-sm text-muted">
                <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
                {resolution.notice}
              </p>
            )}
            {body}
          </>
        )}
      </div>
      <footer className="flex flex-col gap-2 border-t border-border px-5 py-3">
        <ErrorText error={error} />
        <div className="flex flex-wrap items-center justify-end gap-2">
          {state.kind === 'started' ? (
            <button type="button" onClick={onClose} className={buttonClass}>
              Close
            </button>
          ) : (
            <>
              <button type="button" onClick={onClose} className={buttonClass}>
                Cancel
              </button>
              <button
                type="button"
                disabled={!canStart}
                onClick={() => {
                  void run(repoLaunchRequest(clone.repo, clone.dir, promptId, choices))
                }}
                className={primaryButtonClass}
              >
                <TerminalIcon className="size-4" />
                {starting ? 'Starting the upgrade…' : 'Start upgrade'}
              </button>
            </>
          )}
        </div>
      </footer>
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
    </Sheet>
  )
}
