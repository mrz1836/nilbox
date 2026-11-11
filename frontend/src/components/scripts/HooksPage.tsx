import { useId, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { busyRun, hookGroups, installable, matchClones, matchHooks, outcomeTones } from '../../lib/hooks'
import { usable } from '../../lib/scriptsTab'
import type { SettingsSection } from '../../lib/settingsIndex'
import { exportLine } from '../../lib/shell'
import { formatAge, formatAgo } from '../../lib/time'
import type { HooksState } from '../../lib/useHooks'
import { useMutation } from '../../lib/useMutation'
import {
  installAllHooks,
  openExternal,
  type HookGroup,
  type HookInstallAllResult,
  type IntegrationTool,
  type RunsIn,
  type ScriptRun,
} from '../../platform'
import { CommandLine } from '../CommandLine'
import { buttonClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, ChevronRightIcon, ExternalIcon, InfoIcon, RefreshIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { GroupHeading } from './GroupHeading'
import { HookRow } from './HookRow'
import { RepoSearch } from './RepoSearch'
import { ToneIcon, toneWords } from './ToneIcon'
import { ToolProblem } from './ToolProblem'

// Section is one of the page's groups: a group of clones with a config, or the clones
// without one.
type Section = HookGroup | 'notSetUp'

// sectionsOpen is which sections start open, as hookGroups says; the clones without a
// config start collapsed.
function sectionsOpen(): Readonly<Partial<Record<Section, boolean>>> {
  return Object.fromEntries(hookGroups.map((g) => [g.id, g.open]))
}

// oldAs says how old a scan is: "14m old".
function oldAs(now: Date, iso: string): string {
  return `${formatAge(now, iso)} old`
}

// Fold is whether a note's details show: null until the user folds or unfolds them, when
// they follow what the page says.
type Fold = boolean | null

// PathWarning says the hook can't find go-pre-commit, so commits fail, and how to fix it:
// the line that puts the folder on the PATH, to copy, as Settings shows it.
function PathWarning({ dir }: { dir: string }) {
  const id = useId()
  const [open, setOpen] = useState(false)

  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-warning px-3 py-2 text-sm">
      <p className="flex items-start gap-1.5 text-text">
        <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
        <span>
          <span className="font-mono break-all">{dir}</span> isn’t on your shell’s PATH. Hooks can’t find go-pre-commit,
          so commits fail.{' '}
          <button
            type="button"
            aria-expanded={open}
            aria-controls={`${id}-fix`}
            onClick={() => {
              setOpen(!open)
            }}
            className="inline-flex items-center gap-0.5 font-medium text-accent hover:underline"
          >
            How to fix
            <ChevronRightIcon
              className={`size-3.5 transition-transform motion-reduce:transition-none ${open ? 'rotate-90' : ''}`}
            />
          </button>
        </span>
      </p>
      {open && (
        <div id={`${id}-fix`} className="flex flex-col gap-1.5 pl-5.5">
          <p className="text-xs text-muted">
            Add <span className="font-mono break-all">{dir}</span> to your PATH: put this line in ~/.zprofile, then open
            a new terminal. The warning goes once a scan finds it there.
          </p>
          <CommandLine command={exportLine(dir)} prompt={false} className="w-full max-w-md" />
        </div>
      )}
    </div>
  )
}

// ToolNote says the checks install their own tools, unchecked, until go-pre-commit
// verifies them: unfolded until the first checks run ends in the app, then under More.
function ToolNote({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  const id = useId()

  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border px-3 py-2 text-sm">
      <p className="flex items-start gap-1.5 text-text">
        <InfoIcon className="mt-0.5 size-4 shrink-0 text-info" />
        <span>
          The checks install gofumpt, golangci-lint, and gitleaks the first time they need them.{' '}
          <button
            type="button"
            aria-expanded={open}
            aria-controls={`${id}-more`}
            onClick={onToggle}
            className="inline-flex items-center gap-0.5 font-medium text-accent hover:underline"
          >
            More
            <ChevronRightIcon
              className={`size-3.5 transition-transform motion-reduce:transition-none ${open ? 'rotate-90' : ''}`}
            />
          </button>
        </span>
      </p>
      {open && (
        <p id={`${id}-more`} className="pl-5.5 text-xs text-muted">
          The first time a check needs gofumpt, golangci-lint, or gitleaks, go-pre-commit installs it: gofumpt with go
          install, golangci-lint and gitleaks with curl, without checking them. {brand.name} doesn’t run these installs;
          go-pre-commit does.
        </p>
      )}
    </div>
  )
}

// InstallAll is Install and update all as it goes, then how it went: a line for each
// clone, each opening its run, until dismissed.
function InstallAll({
  pending,
  result,
  error,
  onOpenRun,
  onDismiss,
}: {
  pending: boolean
  result: HookInstallAllResult | null
  error: string
  onOpenRun: (id: number) => void
  onDismiss: () => void
}) {
  return (
    <section
      aria-label="Install and update all"
      className="flex flex-col gap-1.5 rounded-lg border border-border bg-surface px-3 py-2.5 text-sm"
    >
      {pending && (
        <p role="status" className="flex items-center gap-2 text-muted">
          <Spinner />
          Installing and updating the hooks, one clone at a time…
        </p>
      )}
      {result !== null && (
        <>
          <p role="status" className="font-medium">
            {result.message}
          </p>
          <ul aria-label="Each clone" className="flex flex-col gap-1">
            {result.lines.map((line) => (
              <li key={line.dir} className="flex items-start gap-1.5 text-xs">
                <ToneIcon tone={outcomeTones[line.outcome]} className="mt-px size-3.5 shrink-0" />
                <span className="min-w-0 flex-1">
                  <span className="sr-only">{toneWords[outcomeTones[line.outcome]]} </span>
                  <span className="font-medium">{line.name}</span> {line.message}
                </span>
                {line.runId > 0 && (
                  <button
                    type="button"
                    aria-label={`Open the run in ${line.name}`}
                    onClick={() => {
                      onOpenRun(line.runId)
                    }}
                    className={quietButtonClass}
                  >
                    Open the run
                  </button>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
      <ErrorText error={error === '' ? '' : `Couldn’t install and update the hooks: ${error}`} />
      {!pending && (
        <button type="button" onClick={onDismiss} className={`${quietButtonClass} self-end`}>
          Dismiss
        </button>
      )}
    </section>
  )
}

export interface HooksPageProps {
  hooks: HooksState
  // tool is go-pre-commit as the integrations list it, or undefined until listed.
  tool: IntegrationTool | undefined
  // runs are every run, newest first, for the run that holds a busy clone; null until
  // listed.
  runs: readonly ScriptRun[] | null
  // runsIn is where runs go, as set.
  runsIn: RunsIn
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
}

// HooksPage is the Scripts tab's Hooks page: every clone under the clone root with a
// go-pre-commit config, in its group (needs attention, with Install and update all; ready;
// turned off in the repository, collapsed), then the clones without one, collapsed, with
// the way to set it up; a search over every group; a way to scan every clone again, and
// how old the scan is when the last one failed; the warning that the hook can't find
// go-pre-commit, with the fix; and the note that the checks install their own tools.
// While go-pre-commit can't be used, the page says why in place of its list.
export function HooksPage({ hooks, tool, runs, runsIn, onOpenRun, onOpenSettings }: HooksPageProps) {
  const id = useId()
  const [search, setSearch] = useState('')
  const [open, setOpen] = useState(sectionsOpen)
  const [more, setMore] = useState<Fold>(null)
  const [installed, setInstalled] = useState<HookInstallAllResult | null>(null)
  const installAll = useMutation((dirs: string[]) => installAllHooks(dirs))
  const { list, error, refreshing, refreshError, refresh } = hooks
  const searching = search.trim() !== ''
  const matched = list === null ? [] : matchHooks(list.repos, search)
  const others = list === null ? [] : matchClones(list.notSetUp, search)
  const openIntegrations = () => {
    onOpenSettings('integrations')
  }

  const toggle = (section: Section) => {
    setOpen({ ...open, [section]: open[section] !== true })
  }

  const installEvery = async (dirs: string[]) => {
    setInstalled(null)

    const done = await installAll.run(dirs)

    if (done.ok) {
      setInstalled(done.data)
    }
  }

  let body: ReactNode

  if (tool === undefined || !usable(tool)) {
    body = (
      <ToolProblem
        tool={tool}
        binary="go-pre-commit"
        name="go-pre-commit"
        page="Hooks"
        onOpenSettings={openIntegrations}
      />
    )
  } else if (list === null) {
    body =
      error === '' ? (
        <p role="status" className="flex items-center gap-2 px-1 text-sm text-muted">
          <Spinner />
          Scanning your clones…
        </p>
      ) : (
        <ErrorBanner>Couldn’t list the clones: {error}</ErrorBanner>
      )
  } else {
    const readme = `https://github.com/${tool.repo}#readme`
    const setUp = (
      <button
        type="button"
        onClick={() => {
          openExternal(readme)
        }}
        className={`${buttonClass} self-start`}
      >
        Set up go-pre-commit
        <ExternalIcon className="size-3.5 text-muted" />
      </button>
    )
    const notSetUpOpen = searching || open.notSetUp === true
    const toolNoteOpen = more ?? list.toolNote.open

    body = (
      <>
        {list.pathWarning.show && <PathWarning dir={list.pathWarning.dir} />}
        {list.toolNote.show && (
          <ToolNote
            open={toolNoteOpen}
            onToggle={() => {
              setMore(!toolNoteOpen)
            }}
          />
        )}
        {(installAll.pending || installed !== null || installAll.error !== '') && (
          <InstallAll
            pending={installAll.pending}
            result={installed}
            error={installAll.error}
            onOpenRun={onOpenRun}
            onDismiss={() => {
              setInstalled(null)
              installAll.reset()
            }}
          />
        )}
        {list.repos.length === 0 && !searching && (
          <div className="flex flex-col items-start gap-2 px-1">
            <p className="text-sm text-muted">None of your clones has a go-pre-commit config.</p>
            {setUp}
          </div>
        )}
        {searching && matched.length === 0 && others.length === 0 && (
          <p className="px-1 text-sm text-muted">No repository matching “{search.trim()}”.</p>
        )}
        {list.repos.length > 0 &&
          hookGroups.map((group) => {
            const rows = matched.filter((r) => r.group === group.id)
            const shown = searching || open[group.id] === true
            const dirs = rows.filter(installable).map((r) => r.dir)

            if (rows.length === 0 && (searching || group.id !== 'attention')) {
              return null
            }

            return (
              <section key={group.id} aria-labelledby={`${id}-${group.id}`} className="flex flex-col gap-1">
                <GroupHeading
                  id={`${id}-${group.id}`}
                  title={group.title}
                  count={rows.length}
                  open={shown}
                  disabled={searching}
                  onToggle={() => {
                    toggle(group.id)
                  }}
                >
                  {dirs.length > 0 && (
                    <button
                      type="button"
                      disabled={installAll.pending}
                      onClick={() => {
                        void installEvery(dirs)
                      }}
                      className={buttonClass}
                    >
                      Install and update all ({dirs.length})
                    </button>
                  )}
                </GroupHeading>
                {shown &&
                  (rows.length === 0 ? (
                    <p className="px-3 text-sm text-muted">Nothing needs attention.</p>
                  ) : (
                    <ul aria-label={group.title} className="flex flex-col">
                      {rows.map((repo) => (
                        <HookRow
                          key={repo.dir}
                          repo={repo}
                          version={tool.copy.version.raw}
                          watch={busyRun(runs, repo.dir)}
                          runsIn={runsIn}
                          onOpenRun={onOpenRun}
                        />
                      ))}
                    </ul>
                  ))}
              </section>
            )
          })}
        {others.length > 0 && (
          <section aria-labelledby={`${id}-notSetUp`} className="flex flex-col gap-1">
            <GroupHeading
              id={`${id}-notSetUp`}
              title="Not set up"
              count={others.length}
              open={notSetUpOpen}
              disabled={searching}
              note="no .github/env/ or .github/.env.base"
              onToggle={() => {
                toggle('notSetUp')
              }}
            />
            {notSetUpOpen && (
              <div className="flex flex-col gap-2">
                <ul aria-label="Not set up" className="flex flex-col">
                  {others.map((clone) => (
                    <li key={clone.dir} className="flex items-baseline gap-3 px-3 py-1 text-sm">
                      <span className="font-medium">{clone.name}</span>
                      <span className="truncate font-mono text-xs text-muted">{clone.dir}</span>
                    </li>
                  ))}
                </ul>
                <div className="px-3">{setUp}</div>
              </div>
            )}
          </section>
        )}
        {list.repos.length > 0 && (
          <p className="px-1 text-xs text-muted">
            Agent sessions’ worktrees share each clone’s hooks, so an agent’s commits run these checks too.
          </p>
        )}
      </>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-2 px-4 pt-4 pb-2">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Hooks</h2>
          <div className="flex items-center gap-2 text-xs text-muted">
            {tool !== undefined && usable(tool) && (
              <span>
                {tool.binary} {tool.copy.version.raw}
              </span>
            )}
            {list !== null && list.checkedAt !== '' && (
              <span>
                · Scanned <RelativeTime iso={list.checkedAt} show={formatAgo} />
              </span>
            )}
            <button
              type="button"
              disabled={refreshing || !usable(tool)}
              onClick={() => {
                void refresh()
              }}
              className={quietButtonClass}
            >
              <RefreshIcon className={`size-3.5 ${refreshing ? 'motion-safe:animate-spin' : ''}`} />
              {refreshing ? 'Scanning…' : 'Scan again'}
            </button>
          </div>
        </div>
        <ErrorText error={refreshError === '' ? '' : `Couldn’t scan the clones again: ${refreshError}`} />
        {list !== null && list.error !== '' && (
          <p role="note" className="flex items-center gap-1.5 text-xs text-warning" title={list.error}>
            <AlertIcon className="size-3.5 shrink-0" />
            Couldn’t scan the clones · <RelativeTime iso={list.checkedAt} show={oldAs} />
            <span className="sr-only">: {list.error}</span>
          </p>
        )}
        <RepoSearch value={search} onChange={setSearch} />
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-3 pb-4">{body}</div>
    </div>
  )
}
