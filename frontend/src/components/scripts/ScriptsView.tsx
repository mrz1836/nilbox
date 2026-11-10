import { useState, type ReactNode } from 'react'

import { counted, formatCount } from '../../lib/format'
import { isLive } from '../../lib/scriptRuns'
import { useIntegrations } from '../../lib/useIntegrations'
import { useScriptRuns } from '../../lib/useScriptRuns'
import type { IntegrationTool, TerminalID } from '../../platform'
import { AlertIcon, CheckIcon, ChevronRightIcon, ClockIcon, RingIcon, XIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { RunsPage } from './RunsPage'

// ScriptsPage is one of the tab's pages, in the rail's order. The rail lists those built:
// Runs so far, which Release, Hooks, and Repo scripts join as they are.
export type ScriptsPage = 'release' | 'hooks' | 'scripts' | 'runs'

// RailEntry is a page as the rail lists it: its name and icon, and what it counts, with
// what the count means.
interface RailEntry {
  id: ScriptsPage
  label: string
  icon: ReactNode
  count: number
  counts: (n: number) => string
}

const captionClass = 'px-2.5 pb-1 text-[0.6875rem] font-semibold tracking-wide text-muted uppercase'

// ToolLine is a tool's status at the rail's foot: its version when it can be used, with a
// newer release when there is one, or what keeps it from being used.
function ToolLine({ tool }: { tool: IntegrationTool }) {
  const named = `${tool.binary} ${tool.copy.version.raw}`
  let icon: ReactNode
  let text: string

  if (tool.state === 'missing') {
    icon = <XIcon className="mt-px size-3.5 shrink-0 text-muted" />
    text = `${tool.binary} isn’t on this Mac`
  } else if (!tool.on) {
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

export interface ScriptsViewProps {
  // terminal is the terminal chosen for launches, which a run in the terminal runs in, or
  // null until the settings have loaded.
  terminal?: TerminalID | null
  // onManageSettings opens Settings → Integrations.
  onManageSettings: () => void
}

// ScriptsView is the Scripts tab: a rail of its pages, each with what it counts, and at
// its foot the status of each tool and a way to manage them in Settings; beside it, the
// page chosen. Runs is its only page so far; Release, Hooks, and Repo scripts join it as
// they are built. It is a default export, for the window to load it lazily.
export default function ScriptsView({ terminal = null, onManageSettings }: ScriptsViewProps) {
  const [page, setPage] = useState<ScriptsPage>('runs')
  // The runs are listed once, here: the rail counts them, and Runs filters them.
  const listing = useScriptRuns()
  const { tools, error } = useIntegrations()
  const going = listing.runs?.filter((run) => isLive(run.status)).length ?? 0
  const pages: readonly RailEntry[] = [
    {
      id: 'runs',
      label: 'Runs',
      icon: <ClockIcon className="size-4" />,
      count: going,
      counts: (n) => counted(n, 'run in progress', 'runs in progress'),
    },
  ]
  // content is each built page.
  const content: Partial<Record<ScriptsPage, ReactNode>> = { runs: <RunsPage {...listing} terminal={terminal} /> }

  return (
    <div className="flex h-full min-h-0">
      <nav aria-label="Scripts" className="flex w-52 shrink-0 flex-col gap-0.5 border-r border-border p-3">
        <h2 className={captionClass}>Scripts</h2>
        {pages.map((entry) => (
          <button
            key={entry.id}
            type="button"
            aria-current={entry.id === page ? 'page' : undefined}
            title={entry.count > 0 ? entry.counts(entry.count) : undefined}
            onClick={() => {
              setPage(entry.id)
            }}
            className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm text-muted transition-colors hover:bg-raised hover:text-text aria-[current=page]:bg-raised aria-[current=page]:font-medium aria-[current=page]:text-text"
          >
            {entry.icon}
            <span className="min-w-0 flex-1 truncate">{entry.label}</span>
            {entry.count > 0 && (
              <>
                {' '}
                <span
                  aria-hidden="true"
                  className="rounded-full bg-accent/15 px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium text-text tabular-nums"
                >
                  {formatCount(entry.count)}
                </span>
                <span className="sr-only">({entry.counts(entry.count)})</span>
              </>
            )}
          </button>
        ))}
        <div className="mt-auto flex flex-col gap-1.5 border-t border-border pt-3">
          <h3 className={captionClass}>Tools</h3>
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
            onClick={onManageSettings}
            className="mt-1 inline-flex items-center gap-1 self-start rounded-md px-2.5 py-1 text-xs font-medium text-accent transition-colors hover:bg-raised"
          >
            Manage in Settings
            <ChevronRightIcon className="size-3" />
          </button>
        </div>
      </nav>
      <div className="min-w-0 flex-1">{content[page]}</div>
    </div>
  )
}
