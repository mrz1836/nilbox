import { useEffect, useEffectEvent, useId, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { emptyReason, otherPlace, shownSections, yoursSection } from '../../lib/repoScripts'
import type { SettingsSection } from '../../lib/settingsIndex'
import { terminalNames } from '../../lib/terminals'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import { useSections, type OnSection, type Sections, type StoredSections } from '../../lib/useSections'
import { newUserScript, placeName, placeOf } from '../../lib/userScripts'
import { useScriptRun, type ScriptAsk, type ScriptRunState } from '../../lib/useScriptRun'
import {
  listRepoScripts,
  listScriptRepos,
  type RepoScripts,
  type RunsIn,
  type ScriptClone,
  type ScriptsSection,
  type TerminalID,
  type UserScript,
} from '../../platform'
import { buttonClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, InfoIcon, PlusIcon, RefreshIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { BranchCheck } from './BranchCheck'
import { GroupHeading } from './GroupHeading'
import { MagexSection } from './MagexSection'
import { RunPanel } from './RunPanel'
import { ScriptEditor } from './ScriptEditor'
import { ScriptRow } from './ScriptRow'
import { TaskInputsForm } from './TaskInputsForm'
import { TrustDialog } from './TrustDialog'
import { AskFirst, YoursSection } from './YoursSection'

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
  // setting is where runs go, as set, and terminalName the terminal they go to there,
  // for the user's own scripts, which may say a place of their own.
  setting: RunsIn
  terminalName: string
  // onOpenRelease opens the Release page, or is null while it isn't on.
  onOpenRelease: (() => void) | null
}

interface ScriptsProps extends PlaceProps {
  page: RepoScripts
  run: ScriptRunState
  // folds keeps each source's section open or folded as the user left it.
  folds: Sections
  onOpenSettings: (section: SettingsSection) => void
  // onEdit opens the editor on one of the user's scripts; onChange makes a change to
  // them, after which the page reads its scripts again.
  onEdit: (script: UserScript) => void
  onChange: (change: () => PromiseLike<unknown>) => void
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

// Scripts are a clone's sources in the page's order: the user's own scripts that show
// there, then its t3.json scripts and its .vscode/tasks.json tasks, each showing the
// command it runs, then magex. A source that couldn't be read says why, and a clone with
// none of the repository's says why not. Beside them, the confirmation one of the user's
// scripts asks first when it says to, the form a task's inputs open first, and the
// question a repository command asks before it runs.
function Scripts({
  page,
  run,
  folds,
  onOpenSettings,
  onEdit,
  onChange,
  setting,
  terminalName,
  ...place
}: ScriptsProps) {
  const id = useId()
  const yours = yoursSection(page)
  const sections = shownSections(page)
  const { step } = run
  const { onOpenRelease } = place
  const onRun = (ask: ScriptAsk) => {
    run.begin(ask)
  }

  return (
    <>
      {yours !== null && (
        <YoursSection
          page={page}
          section={yours}
          headingId={`${id}-yours`}
          open={folds.isOpen('scripts.yours')}
          onToggle={() => {
            folds.toggle('scripts.yours')
          }}
          pending={run.pending}
          onRun={onRun}
          setting={setting}
          terminalName={terminalName}
          onOpenRelease={onOpenRelease}
          onEdit={onEdit}
          onChange={onChange}
        />
      )}
      {sections.length === 0 && <Empty page={page} onOpenSettings={onOpenSettings} />}
      {sections.map((section) => {
        const headingId = `${id}-${section.source}`
        const key: ScriptsSection = `scripts.${section.source}`
        const open = folds.isOpen(key)
        const toggle = () => {
          folds.toggle(key)
        }

        if (section.source === 'magex') {
          return (
            <MagexSection
              key={section.source}
              section={section}
              headingId={headingId}
              open={open}
              onToggle={toggle}
              pending={run.pending}
              onRun={onRun}
              {...place}
            />
          )
        }

        return (
          <section key={section.source} aria-labelledby={headingId} className="flex flex-col gap-1">
            <GroupHeading
              id={headingId}
              title={section.title}
              count={section.entries.length}
              open={open}
              disabled={false}
              onToggle={toggle}
            />
            {open && section.error !== '' && (
              <p className="flex items-start gap-1.5 px-1 text-xs text-warning">
                <AlertIcon className="mt-px size-3.5 shrink-0" />
                {section.error}
              </p>
            )}
            {open && section.entries.length > 0 && (
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
      {step.step === 'confirm' && (
        <AskFirst
          ask={step.ask}
          script={step.script}
          dir={page.dir}
          place={placeName(step.ask.runsIn === '' ? placeOf(step.script, setting) : step.ask.runsIn, terminalName)}
          onRun={() => {
            run.go(step.ask)
          }}
          onCancel={run.cancel}
        />
      )}
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
  // onStarted hears of a script's run that started, and onPull of a pull's.
  onStarted: (id: number) => void
  onPull: (id: number) => void
  // folds keeps each source's section open or folded as the user left it.
  folds: Sections
  // note says a run started that doesn't show beside the scripts, with a way to show it,
  // or is null.
  note: ReactNode
  onOpenSettings: (section: SettingsSection) => void
}

// ClonePage is the page for one clone: its repository, folder, and branch beside the
// picker, with whether origin has newer commits on it and Pull when it does, the note when
// it is on another branch than the default, New script and Read again, and its scripts,
// with the editor of the user's own. A pull shows as a run, and once it ends the scripts
// are read again, since the files they come from may have moved with the branch.
function ClonePage({ clone, picker, onStarted, onPull, folds, note, onOpenSettings, ...place }: ClonePageProps) {
  // read counts the reads of the clone's scripts after the first: Read again, which
  // lists magex's targets afresh (refresh), and each change to the user's own scripts,
  // which doesn't. The query shows only the newest read's answer, so one still under way
  // from before can't land over it.
  const [read, setRead] = useState({ n: 0, refresh: false })
  const query = useQuery(() => listRepoScripts(clone.dir, read.refresh), [clone.dir, read.n])
  const run = useScriptRun(clone.dir, onStarted)
  // editing is the script the editor is open on, a new one with ID 0, with the names of
  // the variables it runs with, as the page had them, or null.
  const [editing, setEditing] = useState<{ script: UserScript; variables: readonly string[] } | null>(null)
  const change = useMutation((act: () => PromiseLike<unknown>) => act())
  const page = query.data ?? null
  const reading = read.refresh && query.pending
  // readAgain reads the scripts again. It counts from the latest count, not the one this
  // render saw, so changes that end one after another each read again.
  const readAgain = (refresh: boolean) => {
    setRead((was) => ({ n: was.n + 1, refresh }))
  }

  const onChange = async (act: () => PromiseLike<unknown>) => {
    const result = await change.run(act)

    if (result.ok) {
      readAgain(false)
    }
  }

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
    body = (
      <Scripts
        page={page}
        run={run}
        folds={folds}
        onOpenSettings={onOpenSettings}
        onEdit={(script) => {
          setEditing({ script, variables: page.variables })
        }}
        onChange={(act) => {
          void onChange(act)
        }}
        {...place}
      />
    )
  }

  return (
    <Frame
      action={
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={reading}
            title="Read the repository’s files and magex’s targets again"
            onClick={() => {
              readAgain(true)
            }}
            className={quietButtonClass}
          >
            <RefreshIcon className={`size-3.5 ${reading ? 'motion-safe:animate-spin' : ''}`} />
            {reading ? 'Reading…' : 'Read again'}
          </button>
          {page !== null && !page.off && (
            <button
              type="button"
              onClick={() => {
                setEditing({ script: newUserScript(), variables: page.variables })
              }}
              className={buttonClass}
            >
              <PlusIcon className="size-3.5" />
              New script
            </button>
          )}
        </div>
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
                {!page.off && (
                  <>
                    <span aria-hidden="true">·</span>
                    <BranchCheck
                      dir={clone.dir}
                      onStarted={onPull}
                      onPulled={() => {
                        readAgain(false)
                      }}
                    />
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
          <ErrorText error={change.error === '' ? '' : `Couldn’t change your scripts: ${change.error}`} />
          {note}
        </>
      }
    >
      {body}
      {editing !== null && (
        <ScriptEditor
          script={editing.script}
          variables={editing.variables}
          onSaved={() => {
            setEditing(null)
            readAgain(false)
          }}
          onCancel={() => {
            setEditing(null)
          }}
        />
      )}
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
  // sections are the sections the user opened or folded, kept across starts, and
  // onSection stores another.
  sections?: StoredSections
  onSection?: OnSection
  // showRuns reports whether a run that starts shows beside the scripts at once, and
  // onShowRuns changes it.
  showRuns?: boolean
  onShowRuns?: (on: boolean) => void
}

// RepoScriptsPage is the Scripts tab's Repo scripts page: a picker of the clones under the
// clone root, the ones a script ran in most recently first, starting on the one chosen
// last; then the chosen clone's commands, the user's own first (with New script and each
// one's menu to edit, duplicate, move, or delete it), then its t3.json, its
// .vscode/tasks.json, and magex, each with Run and a menu that runs it in the other place.
// A script of the user's set to ask before running asks first, a task with inputs asks
// for them first, and a command from the repository's own files asks before its first
// run and whenever it changes. Each source's section opens and folds, and stays as the user
// left it across starts. A run that starts shows beside the scripts, which stay as they
// were, when showRuns says so; otherwise a script's run says it started, with a way to show
// it, and a pull shows only in the branch's status. Nothing here asks GitHub's API: only the chosen clone's branch check asks its
// origin, one ls-remote whose answer is kept five minutes.
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
  sections = {},
  onSection = () => undefined,
  showRuns = false,
  onShowRuns = () => undefined,
}: RepoScriptsPageProps) {
  const id = useId()
  const folds = useSections(sections, onSection)
  // started is the run a script started that doesn't show beside the scripts, while the
  // page says so, or null.
  const [started, setStarted] = useState<number | null>(null)
  const follow = (run: number) => {
    setStarted(null)
    onFollow(run)
  }
  const note =
    started === null || followed !== null ? null : (
      <p role="status" className="flex flex-wrap items-center gap-x-1.5 text-xs text-muted">
        <span>The run started.</span>
        <button
          type="button"
          onClick={() => {
            follow(started)
          }}
          className="font-medium text-accent hover:underline"
        >
          Show it
        </button>
        <span aria-hidden="true">·</span>
        <button
          type="button"
          onClick={() => {
            onShowRuns(true)
            follow(started)
          }}
          className="font-medium text-accent hover:underline"
        >
          Always show runs here
        </button>
      </p>
    )
  const repos = useQuery(() => listScriptRepos(), [])
  const clones = repos.data ?? null
  const chosen = clones?.find((c) => c.dir === dir) ?? clones?.[0]
  const shownDir = chosen?.dir ?? ''
  const other = otherPlace(runsIn)
  const terminalName = terminal === null ? 'your terminal' : terminalNames[terminal]
  const otherName = other === 'app' ? brand.name : terminalName
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
        setting={runsIn}
        terminalName={terminalName}
        onOpenRelease={onOpenRelease}
        onStarted={(run) => {
          if (showRuns) {
            follow(run)
          } else {
            setStarted(run)
          }
        }}
        onPull={(run) => {
          if (showRuns) {
            follow(run)
          }
        }}
        folds={folds}
        note={note}
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
