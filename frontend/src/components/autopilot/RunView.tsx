import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { itemText, runVerdictWords } from '../../lib/autopilot'
import { counted } from '../../lib/format'
import { agentName } from '../../lib/launch'
import {
  agentReport,
  costText,
  diffLines,
  followedText,
  presetLabels,
  shortCommit,
  statusText,
  stepMarks,
  timelineRows,
  todoMark,
  usageText,
  type DiffLine,
  type TimelineRow,
} from '../../lib/runView'
import { formatDuration } from '../../lib/scriptRuns'
import { terminalLabel } from '../../lib/terminals'
import { formatClock, formatFull } from '../../lib/time'
import { useOnAutopilot } from '../../lib/useAutopilot'
import { useMutation } from '../../lib/useMutation'
import { useNow } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import { useRunTimeline, type RunTimeline } from '../../lib/useRunTimeline'
import {
  autopilotRunDiff,
  autopilotRunView,
  openExternal,
  showScriptRunLog,
  showSessionFolder,
  stopAutopilotRun,
  type AgentID,
  type AgentInfo,
  type AutopilotItemRef,
  type AutopilotRunDiff,
  type AutopilotRunEnding,
  type AutopilotRunProgress,
  type AutopilotRunView,
  type TerminalID,
} from '../../platform'
import { ErrorText, iconButtonClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AutopilotIcon, DocumentIcon, ExternalIcon, FolderIcon, StopIcon, TerminalIcon, XIcon } from '../icons'
import { Spinner } from '../Spinner'
import { TakeOverDialog } from './TakeOverDialog'

export interface RunViewProps {
  // runId is the ledger's run shown.
  runId: number
  // terminal is the terminal chosen for launches, which Take over opens the conversation
  // in, or null until the settings load.
  terminal: TerminalID | null
  agents: readonly AgentInfo[]
  // onOpenItem shows the run's item, as Activity's Open does.
  onOpenItem: (item: AutopilotItemRef, routineId: number) => void
  // onToast says what a button did.
  onToast: (text: string, tone?: 'error') => void
  onClose: () => void
}

// stateClasses color a row's mark: ✓ for a command that worked, ✗ for one that failed, …
// for one still running.
const stateMarks: Readonly<Record<TimelineRow['state'], { mark: string; className: string; label: string }>> = {
  ok: { mark: '✓', className: 'text-success', label: 'worked' },
  failed: { mark: '✗', className: 'text-danger', label: 'failed' },
  pending: { mark: '…', className: 'text-muted', label: 'running' },
  '': { mark: '', className: '', label: '' },
}

// rowClass is a timeline row's text style by its kind.
function rowClass(row: TimelineRow): string {
  if (row.error) {
    return 'text-danger'
  }

  switch (row.kind) {
    case 'command':
      return 'font-mono text-text'
    case 'say':
      return 'text-text italic'
    case 'result':
      return 'font-medium text-text'
    default:
      return 'text-muted'
  }
}

// Row is one row of the timeline: when, what, and how a command came out.
function Row({ row }: { row: TimelineRow }) {
  const state = stateMarks[row.state]

  return (
    <li className="grid grid-cols-[4.5rem_1fr_1.25rem] items-baseline gap-2 py-0.5 text-xs">
      <span className="font-mono text-muted tabular-nums" title={row.at === '' ? undefined : formatFull(row.at)}>
        {row.at === '' ? '' : formatClock(row.at)}
      </span>
      <span className={`min-w-0 break-words whitespace-pre-wrap ${rowClass(row)}`}>
        {row.kind === 'say' ? `“${row.text}”` : row.text}
      </span>
      <span className={`text-right ${state.className}`} aria-label={state.label === '' ? undefined : state.label}>
        {state.mark}
      </span>
    </li>
  )
}

// Timeline is the run's timeline, a page at a time: Show earlier events reads the page of
// its log before the first shown.
function Timeline({ timeline, live }: { timeline: RunTimeline; live: boolean }) {
  const rows = timelineRows(timeline.events, live)

  return (
    <section aria-label="Timeline" className="flex min-h-0 flex-col gap-1">
      {timeline.earlier && (
        <button
          type="button"
          disabled={timeline.loadingEarlier}
          onClick={timeline.loadEarlier}
          className={`${quietButtonClass} self-start`}
        >
          {timeline.loadingEarlier ? 'Reading earlier events…' : 'Show earlier events'}
        </button>
      )}
      {!timeline.loaded ? (
        <p role="status" className="flex items-center gap-2 text-xs text-muted">
          <Spinner />
          Reading the run…
        </p>
      ) : rows.length === 0 ? (
        <p className="text-xs text-muted">{live ? 'Nothing yet.' : 'This run left nothing to show.'}</p>
      ) : (
        <ol className="flex flex-col">
          {rows.map((row) => (
            <Row key={row.key} row={row} />
          ))}
        </ol>
      )}
      {timeline.progress.unknown > 0 && (
        <p className="text-xs text-muted">
          {counted(timeline.progress.unknown, 'message', 'messages')} from the agent weren’t shown.
        </p>
      )}
      <ErrorText error={timeline.error} />
    </section>
  )
}

// Plan is the agent's todo list, as a live checklist.
function Plan({ progress }: { progress: AutopilotRunProgress }) {
  return (
    <section aria-label="The agent’s plan" className="flex flex-col gap-1.5">
      <h3 className="text-xs font-semibold text-muted">The agent’s plan</h3>
      {progress.todos.length === 0 ? (
        <p className="text-xs text-muted">It hasn’t written one.</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {progress.todos.map((todo, i) => (
            <li key={i} className="flex items-baseline gap-1.5 text-xs text-text">
              <span aria-hidden="true" className={todo.status === 'completed' ? 'text-success' : 'text-muted'}>
                {todoMark(todo.status)}
              </span>
              <span className={todo.status === 'completed' ? 'text-muted line-through' : ''}>{todo.text}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

// diffClasses color a patch's lines.
const diffClasses: Readonly<Record<DiffLine['tone'], string>> = {
  add: 'text-success',
  remove: 'text-danger',
  hunk: 'text-info',
  file: 'font-semibold text-text',
  plain: 'text-muted',
}

// Diff is the worktree's changes since the run started, committed or not, read-only.
function Diff({ diff }: { diff: AutopilotRunDiff }) {
  const lines = diffLines(diff.patch)

  return (
    <section aria-label="Diff" className="flex min-h-0 flex-col gap-2">
      <p className="text-xs text-muted">
        Changes since {shortCommit(diff.from)}, committed or not
        {diff.cut && '. The diff is long, so only its start shows'}.
      </p>
      {lines.length === 0 ? (
        <p className="text-xs text-muted">No changes to tracked files.</p>
      ) : (
        <pre className="overflow-x-auto rounded-md bg-background px-2 py-1.5 font-mono text-xs leading-[1.35rem]">
          {lines.map((line, i) => (
            <div key={i} className={diffClasses[line.tone]}>
              {line.text === '' ? ' ' : line.text}
            </div>
          ))}
        </pre>
      )}
      {diff.untracked.length > 0 && (
        <div className="text-xs">
          <p className="text-muted">Not tracked by git yet:</p>
          <ul className="font-mono text-text">
            {diff.untracked.map((path) => (
              <li key={path}>{path}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

// Ending is what a run that ended came to: the agent's own words, as plain text, the app's
// verdict from GitHub, and what it cost.
function Ending({ view, ending }: { view: AutopilotRunView; ending: AutopilotRunEnding }) {
  const { run } = view
  const verdict = runVerdictWords(view.match, run)
  const report = agentReport(ending)
  const facts = [
    ending.turns > 0 ? counted(ending.turns, 'turn', 'turns') : '',
    ending.durationMs > 0 ? formatDuration(ending.durationMs) : '',
    ending.commit === '' ? '' : `on ${shortCommit(ending.commit)}`,
    ending.changed.length > 0 ? `changed ${counted(ending.changed.length, 'file', 'files')}` : '',
    ending.refused > 0 ? `its fence refused ${counted(ending.refused, 'call', 'calls')}` : '',
  ].filter((f) => f !== '')

  return (
    <section
      aria-label="How it ended"
      className="flex flex-col gap-1.5 rounded-lg border border-border px-3 py-2.5 text-xs"
    >
      {verdict !== '' && <p className="font-medium text-text">{verdict}</p>}
      {report !== '' && (
        <div>
          <p className="text-muted">
            {ending.reported ? 'The agent reports:' : 'The agent didn’t report. It last said:'}
          </p>
          <p className="whitespace-pre-wrap text-text">{report}</p>
        </div>
      )}
      {ending.needsPerson && ending.reason !== '' && <p className="text-warning">It needs a person: {ending.reason}</p>}
      {facts.length > 0 && <p className="text-muted">{facts.join(' · ')}</p>}
    </section>
  )
}

// Action is one of the view's buttons.
type Action = 'stop' | 'folder' | 'log' | 'diff'

// RunView is one of Autopilot's agent runs, beside Activity, as the Runs page's run panel
// sits beside its list: what it is and where it runs, how it is doing (its time, its cost
// against its budget, its tokens, and the routine's steps with its own marked), what can be
// done with it (Take over in the terminal, Stop, Diff, Show the worktree, View on GitHub,
// Show the log file), the agent's plan as a live checklist, and its timeline, which pages
// back a page at a time. Once it ends, it says how it came out.
export function RunView({ runId, terminal, agents, onOpenItem, onToast, onClose }: RunViewProps) {
  const headingId = useId()
  const now = useNow()
  const query = useQuery(() => autopilotRunView(runId), [runId])
  const timeline = useRunTimeline(runId)
  const stop = useMutation(() => stopAutopilotRun(runId))
  const diff = useMutation(() => autopilotRunDiff(runId))
  const folder = useMutation((sessionId: string) => showSessionFolder(sessionId))
  const log = useMutation((scriptRunId: number) => showScriptRunLog(scriptRunId))
  const actions = { stop, folder, log, diff }
  const [last, setLast] = useState<Action | null>(null)
  const [shown, setShown] = useState<AutopilotRunDiff | null>(null)
  const [takingOver, setTakingOver] = useState(false)

  // The run is read again as Autopilot changes, which its end does.
  useOnAutopilot(() => {
    void query.refetch()
  })

  const view = query.data
  const close = (
    <button
      type="button"
      aria-label="Close the run"
      title="Close"
      onClick={onClose}
      className={`${iconButtonClass} -mt-1 -mr-1`}
    >
      <XIcon className="size-3.5" />
    </button>
  )

  if (view === undefined) {
    return (
      <section aria-label="Run" className="flex flex-col gap-3 p-4">
        <div className="flex items-start justify-between gap-3">
          {query.error === '' ? (
            <p className="text-sm text-muted">Reading the run…</p>
          ) : (
            <ErrorBanner className="min-w-0 flex-1">{query.error}</ErrorBanner>
          )}
          {close}
        </div>
      </section>
    )
  }

  const { run, match } = view
  const agent = agentName(agents, run.agent as AgentID)
  // background reports an agent's run in the background, whose timeline and worktree the
  // view reads; any other is followed where it runs.
  const background = run.runner === 'background'
  const live = view.live && !timeline.done
  const error = last === null ? '' : actions[last].error
  const where = terminalLabel(terminal, 'your terminal')
  const progress = timeline.progress
  const tokens = progress.tokensIn + progress.tokensOut
  const started = run.startedAt === null ? Number.NaN : Date.parse(run.startedAt)
  const finished = run.finishedAt === null ? Number.NaN : Date.parse(run.finishedAt)
  const elapsed = Number.isNaN(started)
    ? ''
    : formatDuration((Number.isNaN(finished) ? now.getTime() : finished) - started)
  const details = [
    elapsed,
    costText(run, tokens),
    agent,
    run.model,
    run.effort,
    run.worktree,
    presetLabels[run.preset] ?? run.preset,
  ].filter((d) => d !== '')
  const usage = usageText(progress)
  const marks = stepMarks(match)

  const press = (action: Action) => {
    setLast(action)
  }

  const toggleDiff = async () => {
    if (shown !== null) {
      setShown(null)

      return
    }

    press('diff')

    const got = await diff.run()

    if (got.ok) {
      setShown(got.data)
    }
  }

  return (
    <section aria-labelledby={headingId} className="flex h-full min-h-0 flex-col gap-3 p-4">
      <header className="flex flex-col gap-1.5">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 id={headingId} className="flex min-w-0 items-center gap-1.5 text-base font-semibold">
              <AutopilotIcon className="size-4 shrink-0 text-accent" />
              <span className="truncate">{match.routine}</span>
            </h2>
            <button
              type="button"
              onClick={() => {
                onOpenItem(match.item, match.routineId)
              }}
              className="max-w-full truncate text-left text-xs text-muted hover:underline"
              title={match.item.title}
            >
              {itemText(match.item)} · {match.item.title}
            </button>
          </div>
          {close}
        </div>
        <p role="status" aria-live="polite" className="text-xs">
          <span className="font-medium text-text">{statusText(run, live, timeline.ending)}</span>
          {details.length > 0 && <span className="text-muted"> · {details.join(' · ')}</span>}
        </p>
        {usage !== '' && <p className="text-xs text-muted">{usage}</p>}
        {marks.length > 1 && (
          <p aria-label="The routine’s steps" className="text-xs text-muted">
            {marks.map((m, i) => (
              <span key={m.label}>
                {i > 0 && ' · '}
                <span
                  aria-current={m.current ? 'step' : undefined}
                  className={m.current ? 'font-semibold text-text' : m.done ? 'text-text' : ''}
                >
                  {m.label}
                </span>
              </span>
            ))}
          </p>
        )}
      </header>
      <div className="flex flex-wrap items-center gap-1">
        {view.canTakeOver && (
          <button
            type="button"
            onClick={() => {
              setTakingOver(true)
            }}
            className={quietButtonClass}
          >
            <TerminalIcon className="size-3.5" />
            Take over in {where}
          </button>
        )}
        {live && (
          <button
            type="button"
            disabled={stop.pending || run.status === 'stopping'}
            onClick={() => {
              press('stop')
              void stop.run().then((result) => {
                if (result.ok) {
                  onToast('Stopping the agent’s run.')
                  void query.refetch()
                }
              })
            }}
            className={quietButtonClass}
          >
            <StopIcon className="size-3.5 text-danger" />
            {stop.pending || run.status === 'stopping' ? 'Stopping…' : 'Stop'}
          </button>
        )}
        {background && run.headBefore !== '' && (
          <button
            type="button"
            aria-pressed={shown !== null}
            disabled={diff.pending}
            onClick={() => {
              void toggleDiff()
            }}
            className={`${quietButtonClass} aria-pressed:bg-raised`}
          >
            {diff.pending ? 'Reading the diff…' : 'Diff'}
          </button>
        )}
        {run.sessionId !== '' && (
          <button
            type="button"
            disabled={folder.pending}
            onClick={() => {
              press('folder')
              void folder.run(run.sessionId)
            }}
            className={quietButtonClass}
          >
            <FolderIcon className="size-3.5" />
            Show the worktree
          </button>
        )}
        {match.item.url !== '' && (
          <button
            type="button"
            onClick={() => {
              openExternal(match.item.url)
            }}
            className={quietButtonClass}
          >
            <ExternalIcon className="size-3.5" />
            View on GitHub
          </button>
        )}
        {run.scriptRunId > 0 && !run.logCleared && (
          <button
            type="button"
            disabled={log.pending}
            onClick={() => {
              press('log')
              void log.run(run.scriptRunId)
            }}
            className={quietButtonClass}
          >
            <DocumentIcon className="size-3.5" />
            Show the log file
          </button>
        )}
      </div>
      <ErrorText error={error} />
      {timeline.ending !== null && <Ending view={view} ending={timeline.ending} />}
      <div className="grid min-h-0 flex-1 grid-cols-[minmax(10rem,14rem)_1fr] gap-4 overflow-hidden">
        <div className="min-h-0 overflow-y-auto">
          <Plan progress={progress} />
        </div>
        <div className="min-h-0 overflow-y-auto pr-1">
          {!background ? (
            <p className="text-xs text-muted">{followedText(run.runner)}</p>
          ) : shown === null ? (
            <Timeline timeline={timeline} live={live} />
          ) : (
            <Diff diff={shown} />
          )}
        </div>
      </div>
      {takingOver && (
        <TakeOverDialog
          sessionId={run.sessionId}
          live={live}
          terminal={where}
          where={run.worktree}
          agentName={agent}
          onCancel={() => {
            setTakingOver(false)
          }}
          onTaken={() => {
            setTakingOver(false)
            onToast(`Took it over: its conversation carries on in ${where}. ${brand.name} follows that session now.`)
            void query.refetch()
          }}
        />
      )}
    </section>
  )
}
