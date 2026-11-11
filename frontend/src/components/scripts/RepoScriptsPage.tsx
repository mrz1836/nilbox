import { useEffect, useEffectEvent, useId, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { emptyReason, otherPlace, shownSections } from '../../lib/repoScripts'
import type { SettingsSection } from '../../lib/settingsIndex'
import { terminalNames } from '../../lib/terminals'
import { useQuery } from '../../lib/useQuery'
import { useScriptRun, type ScriptAsk, type ScriptRunState } from '../../lib/useScriptRun'
import {
  listRepoScripts,
  listScriptRepos,
  type RepoScripts,
  type RunsIn,
  type ScriptClone,
  type TerminalID,
} from '../../platform'
import { buttonClass, captionClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, InfoIcon, RefreshIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { MagexSection } from './MagexSection'
import { RunPanel } from './RunPanel'
import { ScriptRow } from './ScriptRow'
import { TaskInputsForm } from './TaskInputsForm'
import { TrustDialog } from './TrustDialog'

interface FrameProps {
  // action sits at the end of the heading's line; header under it; children are the body.
  action?: ReactNode
  header?: ReactNode
  children: ReactNode
}

// Frame lays the page out: its heading, what the header says of the clone, and the body,
// which scrolls.
function Frame({ action, header, children }: FrameProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-2 px-4 pt-4 pb-2">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Repo scripts</h2>
          {action}
        </div>
        {header}
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-3 pb-4">{children}</div>
    </div>
  )
}

interface PlaceProps {
  // other is the place runs don't go, as set, and otherName its name, for each script's
  // way to run there instead.
  other: RunsIn
  otherName: string
  // onOpenRelease opens the Release page, or is null while it isn't on.
  onOpenRelease: (() => void) | null
}

interface ScriptsProps extends PlaceProps {
  page: RepoScripts
  run: ScriptRunState
  onOpenSettings: (section: SettingsSection) => void
}

// Empty says why a clone shows no scripts: a file that lists none, the files it doesn't
// have, and the sources turned off, with the way to Settings → Integrations.
function Empty({ page, onOpenSettings }: { page: RepoScripts; onOpenSettings: (section: SettingsSection) => void }) {
  const reason = emptyReason(page)

  return (
    <div className="flex flex-col items-start gap-2 px-1">
      {reason.lines.map((line) => (
        <p key={line} className="text-sm text-muted">
          {line}
        </p>
      ))}
      {reason.off && (
        <button
          type="button"
          onClick={() => {
            onOpenSettings('integrations')
          }}
          className={buttonClass}
        >
          Open Settings
        </button>
      )}
    </div>
  )
}

// Scripts are a clone's sources in the page's order: its t3.json scripts and its
// .vscode/tasks.json tasks, each showing the command it runs, then magex. A source that
// couldn't be read says why, and a clone with none says why not. Beside them, the form a
// task's inputs open first, and the question a repository command asks before it runs.
function Scripts({ page, run, onOpenSettings, ...place }: ScriptsProps) {
  const id = useId()
  const sections = shownSections(page)
  const { step } = run
  const onRun = (ask: ScriptAsk) => {
    run.begin(ask)
  }

  return (
    <>
      {sections.length === 0 && <Empty page={page} onOpenSettings={onOpenSettings} />}
      {sections.map((section) => {
        const headingId = `${id}-${section.source}`

        if (section.source === 'magex') {
          return (
            <MagexSection
              key={section.source}
              section={section}
              headingId={headingId}
              pending={run.pending}
              onRun={onRun}
              {...place}
            />
          )
        }

        return (
          <section key={section.source} aria-labelledby={headingId} className="flex flex-col gap-1">
            <h3 id={headingId} className={`${captionClass} px-1`}>
              {section.title}
            </h3>
            {section.error !== '' && (
              <p className="flex items-start gap-1.5 px-1 text-xs text-warning">
                <AlertIcon className="mt-px size-3.5 shrink-0" />
                {section.error}
              </p>
            )}
            {section.entries.length > 0 && (
              <ul aria-label={section.title} className="flex flex-col">
                {section.entries.map((entry) => (
                  <ScriptRow
                    key={entry.ref}
                    entry={entry}
                    args={[]}
                    pending={run.pending}
                    command
                    onRun={onRun}
                    {...place}
                  />
                ))}
              </ul>
            )}
          </section>
        )
      })}
      {step.step === 'inputs' && (
        <TaskInputsForm
          entry={step.ask.entry}
          onRun={(values) => {
            run.fill(step.ask, values)
          }}
          onCancel={run.cancel}
        />
      )}
      {step.step === 'trust' && (
        <TrustDialog
          question={step.question}
          repo={page.repo}
          name={page.name}
          dir={page.dir}
          fromBranch={page.fromBranch}
          onRun={(remember) => {
            run.confirm(step, remember)
          }}
          onCancel={run.cancel}
        />
      )}
    </>
  )
}

interface ClonePageProps extends PlaceProps {
  clone: ScriptClone
  // picker is the clone picker, which heads the clone's header.
  picker: ReactNode
  // onStarted shows a run that started.
  onStarted: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
}

// ClonePage is the page for one clone: its repository, folder, and branch beside the
// picker, the note when it is on another branch than the default, Read again, and its
// scripts.
function ClonePage({ clone, picker, onStarted, onOpenSettings, ...place }: ClonePageProps) {
  // reads counts the times Read again was pressed. Each is a new read of the clone's
  // scripts that lists magex's targets afresh, and the query shows only the newest read's
  // answer, so one still under way from before can't land over it.
  const [reads, setReads] = useState(0)
  const query = useQuery(() => listRepoScripts(clone.dir, reads > 0), [clone.dir, reads])
  const run = useScriptRun(clone.dir, onStarted)
  const page = query.data ?? null
  const reading = reads > 0 && query.pending

  let body: ReactNode

  if (page === null) {
    body =
      query.error === '' ? (
        <p role="status" className="flex items-center gap-2 px-1 text-sm text-muted">
          <Spinner />
          Reading the scripts in {clone.name}…
        </p>
      ) : (
        <ErrorBanner>
          Couldn’t read the scripts in {clone.name}: {query.error}
        </ErrorBanner>
      )
  } else if (page.off) {
    body = (
      <div className="flex items-start gap-3 rounded-lg border border-warning px-4 py-3">
        <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
        <p className="min-w-0 flex-1 text-sm text-text">{page.why}</p>
        <button
          type="button"
          onClick={() => {
            onOpenSettings('integrations')
          }}
          className={buttonClass}
        >
          Open Settings
        </button>
      </div>
    )
  } else {
    body = <Scripts page={page} run={run} onOpenSettings={onOpenSettings} {...place} />
  }

  return (
    <Frame
      action={
        <button
          type="button"
          disabled={reading}
          title="Read the repository’s files and magex’s targets again"
          onClick={() => {
            setReads(reads + 1)
          }}
          className={quietButtonClass}
        >
          <RefreshIcon className={`size-3.5 ${reading ? 'motion-safe:animate-spin' : ''}`} />
          {reading ? 'Reading…' : 'Read again'}
        </button>
      }
      header={
        <>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {picker}
            {page !== null && (
              <p className="flex min-w-0 flex-wrap items-center gap-x-1.5 text-xs text-muted">
                {page.repo !== '' && (
                  <>
                    <span className="text-text">{page.repo}</span>
                    <span aria-hidden="true">·</span>
                  </>
                )}
                <span className="font-mono break-all">{page.dir}</span>
                {page.branch !== '' && (
                  <>
                    <span aria-hidden="true">·</span>
                    <span>
                      <span className="sr-only">on </span>
                      <span className="font-mono">{page.branch}</span>
                    </span>
                  </>
                )}
              </p>
            )}
          </div>
          {page !== null && page.fromBranch !== '' && (
            <p className="flex items-start gap-1.5 text-xs text-muted">
              <InfoIcon className="mt-px size-3.5 shrink-0 text-info" />
              <span>
                From branch <span className="font-mono text-text">{page.fromBranch}</span>, not the default branch: a
                branch can change what the repository’s files say.
              </span>
            </p>
          )}
          <ErrorText
            error={page === null || query.error === '' ? '' : `Couldn’t read the scripts again: ${query.error}`}
          />
          <ErrorText error={run.failed === null ? '' : `Couldn’t run ${run.failed.name}: ${run.failed.error}`} />
        </>
      }
    >
      {body}
    </Frame>
  )
}

export interface RepoScriptsPageProps {
  // dir is the clone chosen last, with ~, or '' before any is; onDir remembers the clone
  // shown, the first the picker lists until one is chosen.
  dir: string
  onDir: (dir: string) => void
  // followed is the run shown beside the scripts, as one a script started, or null;
  // onFollow shows another, or, with null, closes it.
  followed: number | null
  onFollow: (id: number | null) => void
  // runsIn is where runs go, as set; terminal is the terminal chosen for launches, which a
  // run in the terminal runs in, or null until the settings have loaded.
  runsIn: RunsIn
  terminal: TerminalID | null
  // cloneRoot is the folder the clones are under, with ~.
  cloneRoot: string
  // onOpenRelease opens the Release page, or is null while it isn't on.
  onOpenRelease: (() => void) | null
  onOpenSettings: (section: SettingsSection) => void
}

// RepoScriptsPage is the Scripts tab's Repo scripts page: a picker of the clones under the
// clone root, the ones a script ran in most recently first, starting on the one chosen
// last; then the chosen clone's commands, from its t3.json, its .vscode/tasks.json, and
// magex, each with Run and a menu that runs it in the other place. A task with inputs
// asks for them first, and a command from the repository's own files asks before its
// first run and whenever it changes. A run that starts shows beside the scripts, which
// stay as they were. Nothing here asks GitHub.
export function RepoScriptsPage({
  dir,
  onDir,
  followed,
  onFollow,
  runsIn,
  terminal,
  cloneRoot,
  onOpenRelease,
  onOpenSettings,
}: RepoScriptsPageProps) {
  const id = useId()
  const repos = useQuery(() => listScriptRepos(), [])
  const clones = repos.data ?? null
  const chosen = clones?.find((c) => c.dir === dir) ?? clones?.[0]
  const shownDir = chosen?.dir ?? ''
  const other = otherPlace(runsIn)
  const otherName = other === 'app' ? brand.name : terminal === null ? 'your terminal' : terminalNames[terminal]
  const remember = useEffectEvent((shown: string) => {
    onDir(shown)
  })

  // The clone shown is remembered as chosen, the first one too, so the list ordered
  // anew, as after a run in another clone, never moves the page to another clone.
  useEffect(() => {
    if (shownDir !== '' && shownDir !== dir) {
      remember(shownDir)
    }
  }, [shownDir, dir])

  let main: ReactNode

  if (clones === null) {
    main = (
      <Frame>
        {repos.error === '' ? (
          <p role="status" className="flex items-center gap-2 px-1 text-sm text-muted">
            <Spinner />
            Listing your clones…
          </p>
        ) : (
          <ErrorBanner>Couldn’t list the clones: {repos.error}</ErrorBanner>
        )}
      </Frame>
    )
  } else if (chosen === undefined) {
    main = (
      <Frame>
        <div className="flex flex-col items-start gap-2 px-1">
          <p className="text-sm text-muted">
            No clones under {cloneRoot} yet. Repo scripts lists the repositories you have cloned there.
          </p>
          <button
            type="button"
            onClick={() => {
              onOpenSettings('repositories')
            }}
            className={buttonClass}
          >
            Change the clone root
          </button>
        </div>
      </Frame>
    )
  } else {
    main = (
      <ClonePage
        key={chosen.dir}
        clone={chosen}
        picker={
          <>
            <label htmlFor={`${id}-repo`} className="sr-only">
              Repository
            </label>
            <select
              id={`${id}-repo`}
              value={chosen.dir}
              onChange={(e) => {
                onDir(e.target.value)
              }}
              className="max-w-64 truncate rounded-md border border-border bg-surface px-2 py-1 text-sm font-medium"
            >
              {clones.map((clone) => (
                <option key={clone.dir} value={clone.dir}>
                  {clone.name}
                </option>
              ))}
            </select>
          </>
        }
        other={other}
        otherName={otherName}
        onOpenRelease={onOpenRelease}
        onStarted={onFollow}
        onOpenSettings={onOpenSettings}
      />
    )
  }

  // As on the Runs page, the scripts narrow to a column while a run shows beside them.
  return (
    <div className="flex h-full min-h-0">
      <div
        className={`flex min-h-0 flex-col ${followed === null ? 'flex-1' : 'w-[26rem] shrink-0 border-r border-border'}`}
      >
        {main}
      </div>
      {followed !== null && (
        <div className="min-w-0 flex-1">
          <RunPanel
            key={followed}
            id={followed}
            terminal={terminal}
            onSelect={onFollow}
            onClose={() => {
              onFollow(null)
            }}
          />
        </div>
      )}
    </div>
  )
}
