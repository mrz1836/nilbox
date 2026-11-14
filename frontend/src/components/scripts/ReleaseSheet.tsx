import { useEffect, useEffectEvent, useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { counted } from '../../lib/format'
import { shellQuoted } from '../../lib/git'
import { bumpHint, intentOf } from '../../lib/release'
import { isLive } from '../../lib/scriptRuns'
import { terminalNames } from '../../lib/terminals'
import { formatAgo } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import {
  getScriptRun,
  on,
  openExternal,
  prepareRelease,
  startRelease,
  type ReleaseBump,
  type ReleaseCandidate,
  type ReleaseCommit,
  type ReleaseOffer,
  type ReleaseStartRequest,
  type RunsIn,
  type TerminalID,
} from '../../platform'
import { buttonClass, dangerButtonClass, fieldClass, primaryButtonClass, Segmented } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { ChevronRightIcon, ExternalIcon, InfoIcon, XIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { Sheet } from '../Sheet'
import { Spinner } from '../Spinner'
import { BumpChoice } from './BumpChoice'
import { CopyCommand } from './CopyCommand'
import { MajorConfirm } from './MajorConfirm'
import { ReleaseChecks } from './ReleaseChecks'
import { ReleaseProgress } from './ReleaseProgress'
import { ReleaseResult } from './ReleaseResult'
import { RunPanel } from './RunPanel'

// shownCommits is how many of the commits since the last version show before "N more".
const shownCommits = 3

// ReleaseTarget is the repository a sheet releases: its "owner/name", its clone as the
// page lists it (with ~), and the branch to release, or '' for the default on GitHub.
export interface ReleaseTarget {
  repo: string
  dir: string
  branch: string
}

// Choice is what the user chose on the sheet so far.
interface Choice {
  branch: string
  bump: ReleaseBump
  typed: string
  allowModulePath: boolean
  runsIn: RunsIn
}

// BumpHint says, under the cards, which bump the commits' conventional-commit prefixes
// usually mean (bumpHint), or nothing. It never changes the choice.
function BumpHint({ commits }: { commits: readonly ReleaseCommit[] }) {
  const hint = bumpHint(commits.map((c) => c.headline))

  if (hint === '') {
    return null
  }

  return (
    <p className="-mt-2 flex items-start gap-1.5 text-xs text-muted">
      <InfoIcon className="mt-px size-3.5 shrink-0" />
      {hint}
    </p>
  )
}

// WhatsInIt lists the commits since the newest version, from GitHub, so they are right
// however far behind the clone is: the newest few, then the rest on request, each linked
// to its commit on GitHub.
function WhatsInIt({ candidate }: { candidate: ReleaseCandidate }) {
  const id = useId()
  const [all, setAll] = useState(false)
  const { commits, ahead, repo, version } = candidate
  const shown = all ? commits : commits.slice(0, shownCommits)
  const hidden = ahead - shown.length

  return (
    <section aria-labelledby={id} className="flex flex-col gap-1.5">
      <h3 id={id} className="text-xs font-semibold tracking-wide text-muted uppercase">
        What’s in it · {counted(ahead, 'commit', 'commits')} since {version}
      </h3>
      <ul className="flex flex-col gap-1">
        {shown.map((commit) => (
          <li key={commit.oid} className="flex items-baseline gap-3 text-sm">
            <button
              type="button"
              onClick={() => {
                openExternal(`https://github.com/${repo}/commit/${commit.oid}`)
              }}
              className="min-w-0 truncate text-left text-text hover:text-accent hover:underline"
            >
              {commit.headline}
            </button>
            <RelativeTime iso={commit.at} show={formatAgo} className="ml-auto shrink-0 text-xs text-muted" />
          </li>
        ))}
      </ul>
      {hidden > 0 &&
        (all || commits.length <= shownCommits ? (
          <button
            type="button"
            onClick={() => {
              openExternal(`https://github.com/${repo}/compare/${version}...${candidate.defaultBranch}`)
            }}
            className="inline-flex items-center gap-1 self-start text-xs font-medium text-accent hover:underline"
          >
            {counted(hidden, 'more commit', 'more commits')} on GitHub
            <ExternalIcon className="size-3" />
          </button>
        ) : (
          <button
            type="button"
            aria-expanded={false}
            onClick={() => {
              setAll(true)
            }}
            className="inline-flex items-center gap-1 self-start text-xs font-medium text-accent hover:underline"
          >
            {counted(hidden, 'more commit', 'more commits')}
            <ChevronRightIcon className="size-3" />
          </button>
        ))}
    </section>
  )
}

export interface ReleaseSheetProps {
  target: ReleaseTarget
  // candidate is the repository as the Release page lists it, for the commits since its
  // newest version, or undefined while the page hasn't listed it.
  candidate: ReleaseCandidate | undefined
  // runsIn is where runs go, as set; the sheet can send this one release elsewhere.
  runsIn: RunsIn
  // terminal is the terminal chosen for launches, or null until the settings have loaded.
  terminal: TerminalID | null
  // onOpenSettings opens Settings → Integrations.
  onOpenSettings: () => void
  // onWatch closes the sheet and opens a run, as a busy clone's Watch does.
  onWatch: (runId: number) => void
  onClose: () => void
}

// ReleaseSheet releases one repository. As it opens, it checks the clone, origin, and
// GitHub, and shows what is true right now, with each fix; then the commits since the
// newest version, the three versions to choose from (Patch, every time, to start; Major
// only with its typed confirmation), where it runs, the sentence that says exactly what
// will happen, the command, and the button that names the version. A choice decides the
// sheet again on the facts already read; Check again reads them afresh. Start reads them
// once more under the clone's lock: when that refuses, the sheet says why and checks
// again, showing the new version if it moved. Once started, the sheet follows the run's
// steps above its panel and log, and once the release ended, how it came out. Closing the
// sheet never stops a release; it goes on under Runs.
export function ReleaseSheet({
  target,
  candidate,
  runsIn: setRunsIn,
  terminal,
  onOpenSettings,
  onWatch,
  onClose,
}: ReleaseSheetProps) {
  const id = useId()
  const { repo, dir } = target
  const [choice, setChoice] = useState<Choice>({
    branch: target.branch,
    bump: 'patch',
    typed: '',
    allowModulePath: false,
    runsIn: setRunsIn,
  })
  // owed reports a fresh read of GitHub, origin, and the clone still owed: as the sheet
  // opens, and after each Check again and each refused start. It is paid only by a
  // Prepare that asked for it and was answered, so one cancelled for a newer choice (or
  // by StrictMode's second effect) leaves it owed, and the next Prepare reads afresh.
  // Every other Prepare decides on the facts the sheet was shown. checks counts the
  // times one was asked for, so each asks again.
  const [owed, setOwed] = useState(true)
  const [checks, setChecks] = useState(1)
  const [majorVersion, setMajorVersion] = useState('')
  const typed = choice.typed.trim()
  // The typed version goes to Prepare only once it matches, so typing asks nothing.
  const confirmed = choice.bump === 'major' && typed !== '' && typed === majorVersion ? typed : ''
  const prepared = useQuery(() => {
    const refresh = owed

    return prepareRelease({
      repo,
      dir,
      branch: choice.branch,
      bump: choice.bump,
      typed: confirmed,
      allowModulePath: choice.allowModulePath,
      runsIn: choice.runsIn,
      refresh,
    }).then((shown) => {
      if (refresh) {
        setOwed(false)
      }

      return shown
    })
  }, [repo, dir, choice.branch, choice.bump, confirmed, choice.allowModulePath, choice.runsIn, checks])
  const offer = prepared.data
  // rereading reports a fresh read of GitHub, origin, and the clone under way, which the
  // sheet says. A Prepare for a new choice decides on the facts shown, so the sheet shows
  // nothing for it: a line put in and taken out again would move everything below it.
  const rereading = prepared.pending && owed
  const start = useMutation((req: ReleaseStartRequest) => startRelease(req))
  const [runId, setRunId] = useState<number | null>(null)

  if (offer !== undefined && offer.major.version !== majorVersion) {
    setMajorVersion(offer.major.version)
  }

  // choose changes the choice, and drops why the last start was refused.
  const choose = (change: Partial<Choice>) => {
    start.reset()
    setChoice({ ...choice, ...change })
  }

  // readAgain asks for a fresh read of GitHub, origin, and the clone.
  const readAgain = () => {
    setOwed(true)
    setChecks((n) => n + 1)
  }

  // begin starts the release the sheet shows.
  const begin = async (shown: ReleaseOffer) => {
    const started = await start.run({
      repo,
      dir,
      branch: shown.branch,
      bump: shown.bump,
      version: shown.version,
      head: shown.head,
      original: shown.current,
      typed: confirmed,
      allowModulePath: choice.allowModulePath,
      runsIn: choice.runsIn,
    })

    if (started.ok) {
      setRunId(started.data)
    } else {
      // The clone, origin, or the version moved: the sheet reads them all again.
      readAgain()
    }
  }

  // tryAgain goes back to the sheet with fresh checks, and a major's field empty again.
  const tryAgain = () => {
    setRunId(null)
    start.reset()
    setChoice({ ...choice, typed: '', allowModulePath: false })
    readAgain()
  }

  const running = useQuery(() => getScriptRun(runId ?? 0), [runId], { enabled: runId !== null })
  const readRunAgain = useEffectEvent(() => {
    void running.refetch()
  })

  // The run is read again as it changes status or ends, and as its own progress moves.
  useEffect(() => {
    if (runId === null) {
      return undefined
    }

    const offUpdated = on('scriptruns:updated', () => {
      readRunAgain()
    })
    const offProgress = on('scriptruns:progress', ({ id: moved }) => {
      if (moved === runId) {
        readRunAgain()
      }
    })

    return () => {
      offUpdated()
      offProgress()
    }
  }, [runId])

  const run = runId === null || running.data?.id !== runId ? undefined : running.data
  const live = run !== undefined && isLive(run.status)

  const version = offer?.version ?? ''
  const title = live ? `Releasing ${repo} ${intentOf(run).version}`.trimEnd() : `Release ${repo}`
  const terminalName = terminal === null ? 'your terminal' : terminalNames[terminal]
  const placeOptions = [
    { value: 'app' as const, label: brand.name },
    { value: 'terminal' as const, label: terminalName },
  ]
  const typedOK = choice.bump !== 'major' || (confirmed !== '' && offer?.major.confirmed === true)
  const canStart = offer !== undefined && offer.ready && typedOK && !prepared.pending && !start.pending
  const busy = start.pending || runId !== null
  const buttonLabel = offer === undefined || offer.button === '' ? `Release ${repo}` : offer.button
  const footer = runId === null || live

  return (
    <Sheet labelledBy={`${id}-title`} onClose={onClose}>
      <header className="flex items-start justify-between gap-3 border-b border-border px-5 py-3.5">
        <h2 id={`${id}-title`} className="min-w-0 truncate text-base font-semibold">
          {title}
        </h2>
        <button
          type="button"
          aria-label="Close the release sheet"
          title="Close"
          onClick={onClose}
          className="-mr-1 flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <XIcon className="size-3.5" />
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-5 py-4">
        {runId !== null ? (
          <>
            {run === undefined ? (
              running.error === '' ? (
                <p role="status" className="flex items-center gap-2 text-sm text-muted">
                  <Spinner />
                  Starting the release…
                </p>
              ) : (
                <ErrorBanner>Couldn’t read the release’s run: {running.error}</ErrorBanner>
              )
            ) : live ? (
              <ReleaseProgress run={run} />
            ) : (
              <ReleaseResult run={run} repo={repo} onTryAgain={tryAgain} onDone={onClose} />
            )}
            <div className="flex min-h-[18rem] flex-1 flex-col rounded-lg border border-border">
              <RunPanel id={runId} terminal={terminal} onSelect={onWatch} />
            </div>
          </>
        ) : (
          <>
            {offer === undefined ? (
              prepared.error === '' ? (
                <p role="status" className="flex items-center gap-2 text-sm text-muted">
                  <Spinner />
                  Checking the clone, origin, and GitHub…
                </p>
              ) : (
                <div className="flex flex-col gap-2">
                  <ErrorBanner>Couldn’t check the release: {prepared.error}</ErrorBanner>
                  <button type="button" onClick={readAgain} className={`${buttonClass} self-start`}>
                    Check again
                  </button>
                </div>
              )
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                  <label htmlFor={`${id}-branch`} className="font-medium">
                    Branch
                  </label>
                  {offer.branches.length > 1 ? (
                    <select
                      id={`${id}-branch`}
                      value={offer.branch}
                      disabled={busy}
                      onChange={(e) => {
                        choose({ branch: e.target.value })
                      }}
                      className={fieldClass}
                    >
                      {offer.branches.map((b) => (
                        <option key={b.name} value={b.name}>
                          {b.name}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <output id={`${id}-branch`} className="font-mono">
                      {offer.branch === '' ? 'unknown' : offer.branch}
                    </output>
                  )}
                  <span className="text-muted">
                    {offer.branchNote === ''
                      ? offer.branches.some((b) => b.default && b.name === offer.branch) &&
                        'the default branch on GitHub'
                      : offer.branchNote}
                  </span>
                </div>
                <div className="flex flex-col gap-2">
                  {rereading && (
                    <p role="status" className="flex items-center gap-2 text-xs text-muted">
                      <Spinner className="size-3.5" />
                      Checking again…
                    </p>
                  )}
                  {prepared.error !== '' && <ErrorText error={`Couldn’t check again: ${prepared.error}`} />}
                  <ReleaseChecks
                    checks={offer.checks}
                    repo={repo}
                    dir={dir}
                    checking={rereading}
                    onCheckAgain={readAgain}
                    onOpenSettings={onOpenSettings}
                    onWatch={onWatch}
                  />
                </div>
                {candidate !== undefined && candidate.commits.length > 0 && <WhatsInIt candidate={candidate} />}
                {offer.bumps.length > 0 && (
                  <BumpChoice
                    bumps={offer.bumps}
                    value={choice.bump}
                    disabled={busy}
                    onChange={(bump) => {
                      choose({ bump, typed: '', allowModulePath: false })
                    }}
                    onUpdate={onOpenSettings}
                  />
                )}
                {offer.bumps.length > 0 && candidate !== undefined && <BumpHint commits={candidate.commits} />}
                {choice.bump === 'major' && offer.bumps.some((b) => b.bump === 'major' && b.offered) && (
                  <MajorConfirm
                    major={offer.major}
                    repo={repo}
                    typed={choice.typed}
                    disabled={busy}
                    onTyped={(next) => {
                      choose({ typed: next })
                    }}
                    allowModulePath={choice.allowModulePath}
                    onAllowModulePath={(allowModulePath) => {
                      choose({ allowModulePath })
                    }}
                  />
                )}
                <div className="flex flex-wrap items-center gap-3 text-sm">
                  <span className="font-medium">Runs in</span>
                  <Segmented
                    label="Where this release runs"
                    size="sm"
                    value={choice.runsIn}
                    options={placeOptions}
                    disabled={busy}
                    onChange={(runsIn) => {
                      choose({ runsIn })
                    }}
                  />
                  <span className="text-xs text-muted">
                    {choice.runsIn === setRunsIn
                      ? 'As set in Settings → Integrations.'
                      : 'For this release only. Settings → Integrations keeps where runs go.'}
                  </span>
                </div>
                {offer.sentence !== '' && <p className="text-sm text-text">{offer.sentence}</p>}
                {offer.argv.length > 0 && (
                  <details className="text-sm">
                    <summary className="cursor-pointer text-xs font-medium text-muted hover:text-text">Command</summary>
                    <div className="mt-1.5">
                      <CopyCommand command={offer.argv.map(shellQuoted).join(' ')} />
                    </div>
                  </details>
                )}
              </>
            )}
          </>
        )}
      </div>
      {footer && (
        <footer className="flex flex-col gap-2 border-t border-border px-5 py-3">
          <ErrorText error={start.error} />
          <div className="flex flex-wrap items-center justify-end gap-2">
            {runId === null ? (
              <>
                <button type="button" onClick={onClose} className={buttonClass}>
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={!canStart}
                  onClick={
                    offer === undefined
                      ? undefined
                      : () => {
                          void begin(offer)
                        }
                  }
                  className={choice.bump === 'major' ? dangerButtonClass : primaryButtonClass}
                >
                  {start.pending ? `Starting the release of ${version}…` : buttonLabel}
                </button>
              </>
            ) : (
              <>
                <p className="mr-auto text-xs text-muted">
                  Closing this doesn’t stop the release: it goes on under Runs.
                </p>
                <button type="button" onClick={onClose} className={buttonClass}>
                  Close
                </button>
              </>
            )}
          </div>
        </footer>
      )}
    </Sheet>
  )
}
