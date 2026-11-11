import { useRef, useState, type ReactNode } from 'react'

import { counted, formatCount } from '../../lib/format'
import { attentionCount } from '../../lib/hooks'
import { unreleasedCount, type ReleaseRequest } from '../../lib/release'
import { isLive, type RunRequest } from '../../lib/scriptRuns'
import { scriptsOn, toolOf, usable } from '../../lib/scriptsTab'
import type { SettingsSection } from '../../lib/settingsIndex'
import { useHooks } from '../../lib/useHooks'
import type { IntegrationsState } from '../../lib/useIntegrations'
import { useReleaseCandidates } from '../../lib/useReleaseCandidates'
import type { ScriptRunsState } from '../../lib/useScriptRuns'
import type { IntegrationSwitch, IntegrationTool, RunsIn, ScriptsPage as StoredPage, TerminalID } from '../../platform'
import {
  AlertIcon,
  AnchorIcon,
  CheckIcon,
  ChevronRightIcon,
  ClockIcon,
  DotIcon,
  PlayIcon,
  RingIcon,
  TagIcon,
  XIcon,
} from '../icons'
import { captionClass } from '../controls'
import { ErrorText } from '../settings/rows'
import { HooksPage } from './HooksPage'
import { ReleasePage } from './ReleasePage'
import { ReleaseSheet, type ReleaseTarget } from './ReleaseSheet'
import { RepoScriptsPage } from './RepoScriptsPage'
import { RunsPage } from './RunsPage'

// ScriptsPage is one of the tab's pages, in the rail's order. The rail lists those whose
// integration is on, and Runs. It is a page the settings store (StoredPage), never none.
export type ScriptsPage = Exclude<StoredPage, ''>

// RailEntry is a page as the rail lists it: its name and icon, and what it counts, with
// what the count means, or null for a page that counts nothing; alert marks a count that
// needs the user with an amber dot.
interface RailEntry {
  id: ScriptsPage
  label: string
  icon: ReactNode
  count: number
  counts: ((n: number) => string) | null
  alert?: boolean
}

// railCaptionClass lines a caption up with the rail's entries.
const railCaptionClass = `${captionClass} px-2.5 pb-1`

// ToolLine is a tool's status at the rail's foot: its version when it can be used, with a
// newer release when there is one, or what keeps it from being used.
function ToolLine({ tool }: { tool: IntegrationTool }) {
  const named = `${tool.binary} ${tool.copy.version.raw}`
  let icon: ReactNode
  let text: string

  if (tool.state === 'missing') {
    icon = <XIcon className="mt-px size-3.5 shrink-0 text-muted" />
    text = `${tool.binary} isn’t on this Mac`
  } else if (tool.switch === 'off') {
    // Only the user's switch turns a tool off: left automatic, a copy too old to use, or one
    // that won't run, says so below.
    icon = <RingIcon className="mt-px size-3.5 shrink-0 text-muted" />
    text = `${tool.name} is off`
  } else if (tool.state === 'tooOld') {
    icon = <AlertIcon className="mt-px size-3.5 shrink-0 text-warning" />
    text = `${named} is too old`
  } else if (tool.state === 'wontRun') {
    icon = <XIcon className="mt-px size-3.5 shrink-0 text-danger" />
    text = `${tool.binary} won’t run`
  } else {
    icon = <CheckIcon className="mt-px size-3.5 shrink-0 text-success" />
    text = named
  }

  return (
    <li className="flex items-start gap-1.5 px-2.5 text-xs text-text">
      {icon}
      <span className="flex min-w-0 flex-col">
        <span className="break-words">{text}</span>
        {tool.on && tool.state === 'update' && <span className="text-muted">{tool.latest.version} available</span>}
      </span>
    </li>
  )
}

// Sheet is the release sheet open over the tab: its repository, and the request that
// opened it, so a new request opens it afresh.
interface OpenSheet {
  target: ReleaseTarget
  key: number
}

export interface ScriptsViewProps {
  // terminal is the terminal chosen for launches, which a run in the terminal runs in, or
  // null until the settings have loaded; runsIn is where runs go, as set.
  terminal: TerminalID | null
  runsIn: RunsIn
  // cloneRoot is the folder the clones are under, with ~.
  cloneRoot: string
  // integrations are the tools as the window lists them, and runs every run: the window
  // holds both, for the tab's badge. scriptsSwitch is how the user set the Scripts
  // integration, which shows Repo scripts.
  integrations: Pick<IntegrationsState, 'tools' | 'error'>
  scriptsSwitch: IntegrationSwitch
  runs: ScriptRunsState
  // request asks for a repository's release sheet, as a merge's toast does; each new one
  // opens Release and the sheet. runRequest asks for a run, as a failed run's notification
  // does; each new one opens Runs with the run in its panel.
  request: ReleaseRequest | null
  runRequest?: RunRequest | null
  // onOpenSettings opens the settings at a section: Integrations, or Repositories for the
  // clone root.
  onOpenSettings: (section: SettingsSection) => void
  // lastPage and lastRepo are the page and the Repo scripts clone the settings remember
  // from a past start, '' for none; onPage and onRepo store the ones the user picks.
  lastPage: StoredPage
  lastRepo: string
  onPage: (page: ScriptsPage) => void
  onRepo: (dir: string) => void
}

// ScriptsView is the Scripts tab: a rail of its pages, each with what it counts, and at
// its foot the status of each tool and a way to manage them in Settings; beside it, the
// page chosen. Release shows while mage-x is on, Hooks while go-pre-commit is, and Repo
// scripts while Scripts is, beside Runs. The tab opens on the page used last, and Repo
// scripts on the clone chosen last, kept across starts in the settings (a page that's off
// now opens the first page that's on). A run Release or Hooks starts opens in Runs; one
// Repo scripts starts shows beside its scripts, which stay as they were. It is a default
// export, for the window to load it lazily.
export default function ScriptsView({
  terminal,
  runsIn,
  cloneRoot,
  integrations,
  scriptsSwitch,
  runs,
  request,
  runRequest = null,
  onOpenSettings,
  lastPage,
  lastRepo,
  onPage,
  onRepo,
}: ScriptsViewProps) {
  const { tools, error } = integrations
  const mageX = toolOf(tools, 'mage-x')
  const goPreCommit = toolOf(tools, 'go-pre-commit')
  const releaseOn = mageX?.on === true
  const hooksOn = goPreCommit?.on === true
  const repoScriptsOn = scriptsOn(tools, scriptsSwitch)
  // page is the page picked while the window is open, or null before one is, when the one
  // remembered from a past start opens; the settings may load after the tab does.
  const [page, setPage] = useState<ScriptsPage | null>(null)
  const wanted = page ?? (lastPage === '' ? null : lastPage)
  const first: ScriptsPage = releaseOn ? 'release' : hooksOn ? 'hooks' : repoScriptsOn ? 'scripts' : 'runs'
  const shown: ScriptsPage =
    wanted === null ||
    (wanted === 'release' && !releaseOn) ||
    (wanted === 'hooks' && !hooksOn) ||
    (wanted === 'scripts' && !repoScriptsOn)
      ? first
      : wanted
  // pick shows a page the user chose and remembers it for the next start.
  const pick = (next: ScriptsPage) => {
    setPage(next)
    onPage(next)
  }
  const candidates = useReleaseCandidates(usable(mageX))
  const hooks = useHooks(usable(goPreCommit), runs.runs)
  const [selectedRun, setSelectedRun] = useState<number | null>(null)
  // repoDir is the clone Repo scripts showed last, or null before one is picked while the
  // window is open, when the one remembered from a past start shows; repoRun is the run it
  // shows beside its scripts, kept while the window is open.
  const [repoDir, setRepoDir] = useState<string | null>(null)
  // chooseRepo shows the clone Repo scripts picked, and remembers it for the next start
  // unless it is the one remembered already.
  const chooseRepo = (dir: string) => {
    setRepoDir(dir)

    if (dir !== lastRepo) {
      onRepo(dir)
    }
  }
  const [repoRun, setRepoRun] = useState<number | null>(null)
  const [sheet, setSheet] = useState<OpenSheet | null>(null)
  // handled and handledRun are the last release and run requests opened, so each opens
  // once; opens numbers the sheets the page's rows open, below every request's number, so
  // each opens afresh.
  const [handled, setHandled] = useState(0)
  const [handledRun, setHandledRun] = useState(0)
  const opens = useRef(0)

  if (request !== null && request.seq !== handled) {
    setHandled(request.seq)
    setPage('release')
    setSheet({ target: { repo: request.repo, dir: request.dir, branch: request.branch }, key: request.seq })
  }

  if (runRequest !== null && runRequest.seq !== handledRun) {
    setHandledRun(runRequest.seq)
    setSheet(null)
    setSelectedRun(runRequest.id)
    setPage('runs')
  }

  const going = runs.runs?.filter((run) => isLive(run.status)).length ?? 0
  const pages: readonly RailEntry[] = [
    ...(releaseOn
      ? [
          {
            id: 'release' as const,
            label: 'Release',
            icon: <TagIcon className="size-4" />,
            count: unreleasedCount(candidates.list?.repos),
            counts: (n: number) =>
              counted(n, 'repository with unreleased changes', 'repositories with unreleased changes'),
          },
        ]
      : []),
    ...(hooksOn
      ? [
          {
            id: 'hooks' as const,
            label: 'Hooks',
            icon: <AnchorIcon className="size-4" />,
            count: attentionCount(hooks.list?.repos),
            counts: (n: number) => counted(n, 'repository needs attention', 'repositories need attention'),
            alert: true,
          },
        ]
      : []),
    ...(repoScriptsOn
      ? [
          {
            id: 'scripts' as const,
            label: 'Repo scripts',
            icon: <PlayIcon className="size-4" />,
            count: 0,
            counts: null,
          },
        ]
      : []),
    {
      id: 'runs',
      label: 'Runs',
      icon: <ClockIcon className="size-4" />,
      count: going,
      counts: (n) => counted(n, 'run in progress', 'runs in progress'),
    },
  ]

  // openRun shows a run in the Runs page's panel.
  const openRun = (id: number) => {
    setSheet(null)
    setSelectedRun(id)
    setPage('runs')
  }

  const openIntegrations = () => {
    onOpenSettings('integrations')
  }

  const candidate =
    sheet === null
      ? undefined
      : candidates.list?.repos.find(
          (r) =>
            r.repo.toLowerCase() === sheet.target.repo.toLowerCase() &&
            (sheet.target.dir === '' || r.dir === sheet.target.dir),
        )

  return (
    <div className="flex h-full min-h-0">
      <nav aria-label="Scripts" className="flex w-52 shrink-0 flex-col gap-0.5 border-r border-border p-3">
        <h2 className={railCaptionClass}>Scripts</h2>
        {pages.map((entry) => {
          // words say what the count means, while there is one.
          const words = entry.counts === null || entry.count === 0 ? '' : entry.counts(entry.count)

          return (
            <button
              key={entry.id}
              type="button"
              aria-current={entry.id === shown ? 'page' : undefined}
              title={words === '' ? undefined : words}
              onClick={() => {
                pick(entry.id)
              }}
              className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:font-medium aria-[current=page]:text-text"
            >
              {entry.icon}
              <span className="min-w-0 flex-1 truncate">{entry.label}</span>
              {words !== '' && (
                <>
                  {' '}
                  <span
                    aria-hidden="true"
                    className="rounded-full bg-accent/15 px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium text-text tabular-nums"
                  >
                    {formatCount(entry.count)}
                  </span>
                  {entry.alert === true && <DotIcon className="-ml-1 size-3 shrink-0 text-warning" />}
                  <span className="sr-only">({words})</span>
                </>
              )}
            </button>
          )
        })}
        <div className="mt-auto flex flex-col gap-1.5 border-t border-border pt-3">
          <h3 className={railCaptionClass}>Tools</h3>
          {tools === null ? (
            error === '' && <p className="px-2.5 text-xs text-muted">Looking for the tools…</p>
          ) : (
            <ul aria-label="Tools" className="flex flex-col gap-1">
              {tools.tools.map((tool) => (
                <ToolLine key={tool.id} tool={tool} />
              ))}
            </ul>
          )}
          <ErrorText error={error === '' ? '' : `Couldn’t list the tools: ${error}`} className="px-2.5" />
          <button
            type="button"
            onClick={openIntegrations}
            className="mt-1 inline-flex items-center gap-1 self-start rounded-md px-2.5 py-1 text-xs font-medium text-accent transition-colors hover:bg-raised"
          >
            Manage in Settings
            <ChevronRightIcon className="size-3" />
          </button>
        </div>
      </nav>
      <div className="min-w-0 flex-1">
        {shown === 'release' ? (
          <ReleasePage
            candidates={candidates}
            tool={mageX}
            runs={runs.runs}
            cloneRoot={cloneRoot}
            onRelease={(c) => {
              opens.current -= 1
              setSheet({ target: { repo: c.repo, dir: c.dir, branch: '' }, key: opens.current })
            }}
            onOpenRun={openRun}
            onOpenSettings={onOpenSettings}
          />
        ) : shown === 'hooks' ? (
          <HooksPage
            hooks={hooks}
            tool={goPreCommit}
            runs={runs.runs}
            runsIn={runsIn}
            onOpenRun={openRun}
            onOpenSettings={onOpenSettings}
          />
        ) : shown === 'scripts' ? (
          <RepoScriptsPage
            dir={repoDir ?? lastRepo}
            onDir={chooseRepo}
            followed={repoRun}
            onFollow={setRepoRun}
            runsIn={runsIn}
            terminal={terminal}
            cloneRoot={cloneRoot}
            onOpenRelease={
              releaseOn
                ? () => {
                    pick('release')
                  }
                : null
            }
            onOpenSettings={onOpenSettings}
          />
        ) : (
          <RunsPage {...runs} terminal={terminal} selected={selectedRun} onSelect={setSelectedRun} />
        )}
      </div>
      {sheet !== null && (
        <ReleaseSheet
          key={sheet.key}
          target={sheet.target}
          candidate={candidate}
          runsIn={runsIn}
          terminal={terminal}
          onOpenSettings={openIntegrations}
          onWatch={openRun}
          onClose={() => {
            setSheet(null)
          }}
        />
      )}
    </div>
  )
}
