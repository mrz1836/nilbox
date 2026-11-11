import { useEffect, useEffectEvent, useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { plainText } from '../../lib/ansi'
import { attempt } from '../../lib/call'
import { counted } from '../../lib/format'
import { isBumpResult } from '../../lib/release'
import { formatDuration, isLive, readLog, runDuration, runPlace } from '../../lib/scriptRuns'
import { useMutation } from '../../lib/useMutation'
import { useNow } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import {
  copyText,
  getScriptRun,
  markScriptRunSeen,
  on,
  openScriptRunInTerminal,
  runScriptAgain,
  showScriptRunLog,
  stopScriptRun,
  type ScriptRun,
  type ScriptRunResult,
  type TerminalID,
} from '../../platform'
import { terminalNames } from '../../lib/terminals'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, CheckIcon, CopyIcon, DocumentIcon, RefreshIcon, StopIcon, TerminalIcon, XIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { ChecksResult } from './ChecksResult'
import { HookResult } from './HookResult'
import { LogView } from './LogView'
import { RunStatus } from './RunStatus'

// tickMs is how often a running run's elapsed time moves on.
const tickMs = 1000

// Elapsed is how long a run took, or, while it runs, how long it has been running,
// counted every second. The window's clock (a minute's tick) stands in until the first
// second has passed.
function Elapsed({ run }: { run: ScriptRun }) {
  const live = isLive(run.status)
  const now = useNow()
  const [tick, setTick] = useState<number | null>(null)

  useEffect(() => {
    if (!live) {
      return undefined
    }

    const timer = setInterval(() => {
      setTick(Date.now())
    }, tickMs)

    return () => {
      clearInterval(timer)
    }
  }, [live])

  const text = live ? formatDuration((tick ?? now.getTime()) - Date.parse(run.startedAt)) : runDuration(run)

  return text === '' ? null : <span className="tabular-nums">{text}</span>
}

// RunResult is what a run left to look at beside its log: its exit code when it isn't 0,
// the tool's warnings, and the lines the reader didn't recognize, behind Show.
function RunResult({ exitCode, result }: { exitCode: number | null; result: ScriptRunResult }) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const kept = result.unrecognized.length

  return (
    <section
      aria-label="Result"
      className="flex max-h-56 shrink-0 flex-col gap-1.5 overflow-y-auto rounded-lg border border-border bg-surface px-3 py-2.5 text-xs"
    >
      {exitCode !== null && exitCode !== 0 && (
        <p className="flex items-center gap-1.5 text-text">
          <XIcon className="size-3.5 shrink-0 text-danger" />
          Exit code {exitCode}
        </p>
      )}
      {result.warnings.length > 0 && (
        <ul aria-label="Warnings" className="flex flex-col gap-1">
          {result.warnings.map((warning, i) => (
            <li key={i} className="flex items-start gap-1.5 text-warning">
              <AlertIcon className="mt-px size-3.5 shrink-0" />
              <span className="min-w-0 font-mono break-all whitespace-pre-wrap">{plainText(warning)}</span>
            </li>
          ))}
        </ul>
      )}
      {result.unrecognizedCount > 0 && (
        <div className="flex flex-col gap-1">
          <p className="text-muted">
            {counted(result.unrecognizedCount, 'line', 'lines')} {brand.name} didn’t recognize ·{' '}
            <button
              type="button"
              aria-expanded={open}
              aria-controls={`${id}-lines`}
              onClick={() => {
                setOpen(!open)
              }}
              className="font-medium text-accent hover:underline"
            >
              {open ? 'Hide' : 'Show'}
            </button>
          </p>
          {open && (
            <div id={`${id}-lines`} className="flex flex-col gap-1">
              <pre className="rounded-md bg-background px-2 py-1.5 font-mono leading-[1.35rem] break-all whitespace-pre-wrap text-text">
                {result.unrecognized.map((line) => plainText(line)).join('\n')}
              </pre>
              {kept < result.unrecognizedCount && (
                <p className="text-muted">The first {counted(kept, 'line is', 'lines are')} kept.</p>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  )
}

// Action is one of the panel's buttons.
type Action = 'stop' | 'again' | 'terminal' | 'copy' | 'reveal'

export interface RunPanelProps {
  // id is the run shown.
  id: number
  // terminal is the terminal chosen for launches, which a run in the terminal runs in, or
  // null until the settings have loaded.
  terminal: TerminalID | null
  // onSelect shows another run, as Run again starts one.
  onSelect: (id: number) => void
  // onClose closes the panel; without it there is no close button, as in the release
  // sheet, which closes itself.
  onClose?: () => void
}

// StopNote says why Stop is off, or how to stop a run in the terminal, which only the
// terminal can.
function StopNote({ id, run, terminal }: { id: string; run: ScriptRun; terminal: TerminalID | null }) {
  if (!isLive(run.status)) {
    return null
  }

  if (run.runsIn === 'terminal') {
    const where = terminal === null ? 'your terminal' : terminalNames[terminal]

    return (
      <p id={id} className="text-xs text-muted">
        It runs in {where}: press Control-C there to stop it.
      </p>
    )
  }

  return run.progress.stopRefused === '' ? null : (
    <p id={id} className="flex items-center gap-1.5 text-xs text-muted">
      <AlertIcon className="size-3.5 shrink-0 text-warning" />
      {run.progress.stopRefused}
    </p>
  )
}

// RunPanel is one run, live or ended, in the app or in the terminal: what it is and where
// it ran, its status (announced as it changes, and only it), how long it took, what can
// be done with it, what it left to look at (a hooks action's or a checks run's result,
// typed, once it ended), and its log. It reads the run again whenever the shell says the
// runs changed or this run's progress moved, and marks an ended run seen once it shows,
// which clears the tab's red dot.
export function RunPanel({ id, terminal, onSelect, onClose }: RunPanelProps) {
  const headingId = useId()
  const query = useQuery(() => getScriptRun(id), [id])
  const reloadOnUpdate = useEffectEvent(() => {
    void query.refetch()
  })
  const stop = useMutation(() => stopScriptRun(id))
  const again = useMutation(() => runScriptAgain(id))
  const openTerminal = useMutation(() => openScriptRunInTerminal(id))
  const copy = useMutation(async () => {
    await copyText(await readLog(id))
  })
  const reveal = useMutation(() => showScriptRunLog(id))
  const actions = { stop, again, terminal: openTerminal, copy, reveal }
  // last is the button pressed last: only its failure shows, so one that works clears
  // what an earlier one said went wrong.
  const [last, setLast] = useState<Action | null>(null)
  // stopping is set once Stop was asked for, until the run ends; confirming holds the
  // question Stop asks first while stopping now would leave something behind.
  const [stopping, setStopping] = useState(false)
  const [confirming, setConfirming] = useState('')
  const [copied, setCopied] = useState(false)
  const run = query.data
  const unseen = run !== undefined && !run.seen && !isLive(run.status)
  const error = last === null ? '' : actions[last].error

  // The run is read again as it changes status or ends, and as its own progress moves:
  // a step reached, or Stop turned on or off.
  useEffect(() => {
    const offUpdated = on('scriptruns:updated', () => {
      reloadOnUpdate()
    })
    const offProgress = on('scriptruns:progress', ({ id: moved }) => {
      if (moved === id) {
        reloadOnUpdate()
      }
    })

    return () => {
      offUpdated()
      offProgress()
    }
  }, [id])

  const markSeen = useEffectEvent(() => {
    void attempt(markScriptRunSeen(id)).then((result) => {
      if (!result.ok) {
        console.error('could not mark the run seen:', result.error)
      }
    })
  })

  useEffect(() => {
    if (unseen) {
      markSeen()
    }
  }, [unseen])

  // press starts an action: it becomes the one whose outcome shows.
  const press = (action: Action) => {
    setLast(action)
    setCopied(false)
  }

  const stopRun = async () => {
    setConfirming('')
    press('stop')
    setStopping((await stop.run()).ok)
  }

  // askToStop stops the run, asking first when its kind says stopping now leaves
  // something behind.
  const askToStop = (confirm: string) => {
    if (confirm === '') {
      void stopRun()
    } else {
      setConfirming(confirm)
    }
  }

  const runAgain = async () => {
    press('again')

    const started = await again.run()

    if (started.ok) {
      onSelect(started.data)
    }
  }

  const copyLog = async () => {
    press('copy')
    setCopied((await copy.run()).ok)
  }

  const close = onClose !== undefined && (
    <button
      type="button"
      aria-label="Close the run"
      title="Close"
      onClick={onClose}
      className="-mt-1 -mr-1 flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
    >
      <XIcon className="size-3.5" />
    </button>
  )

  if (run === undefined) {
    return (
      <section aria-label="Run" className="flex flex-col gap-3 p-4">
        <div className="flex items-start justify-between gap-3">
          {query.error === '' ? (
            <p className="text-sm text-muted">Reading the run…</p>
          ) : (
            <ErrorBanner className="min-w-0 flex-1">Couldn’t read the run: {query.error}</ErrorBanner>
          )}
          {close}
        </div>
      </section>
    )
  }

  const { result } = run
  // Stop shows while the run goes in the app: on while its kind lets it stop now, and off,
  // saying why, while it doesn't.
  const refused = run.progress.stopRefused
  const offersStop = run.canStop || (isLive(run.status) && run.runsIn === 'app' && refused !== '')
  const hasResult =
    (run.exitCode !== null && run.exitCode !== 0) || result.warnings.length > 0 || result.unrecognizedCount > 0
  // ended reports a run that has ended, whose typed result a hooks action or a checks run
  // shows above the rest.
  const ended = !isLive(run.status)

  return (
    <section aria-labelledby={headingId} className="flex h-full min-h-0 flex-col gap-3 p-4">
      <header className="flex flex-col gap-1.5">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 id={headingId} className="truncate text-base font-semibold">
              {run.title}
            </h2>
            <p className="truncate text-xs text-muted" title={run.dir}>
              {run.repo !== '' && `${run.repo} · `}
              <span className="font-mono">{run.dir}</span>
            </p>
          </div>
          {close}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
          <div role="status" aria-live="polite" className="flex min-w-0 items-center gap-2">
            <RunStatus status={run.status} chip />
            {run.summary !== '' && <span className="text-text">{run.summary}</span>}
          </div>
          <span aria-hidden="true">·</span>
          <span>{runPlace(run.runsIn, terminal)}</span>
          <Elapsed run={run} />
        </div>
        {run.command !== '' && run.command !== run.title && (
          <p className="truncate font-mono text-xs text-muted" title={run.command}>
            $ {run.command}
          </p>
        )}
      </header>
      <div className="flex flex-wrap items-center gap-1">
        {offersStop && (
          <button
            type="button"
            disabled={stopping || stop.pending || !run.canStop}
            aria-describedby={refused === '' ? undefined : `${headingId}-stop`}
            onClick={() => {
              askToStop(run.progress.stopConfirm)
            }}
            className={quietButtonClass}
          >
            <StopIcon className="size-3.5 text-danger" />
            {stopping || stop.pending ? 'Stopping…' : 'Stop'}
          </button>
        )}
        {run.canRunAgain && (
          <button
            type="button"
            disabled={again.pending}
            onClick={() => {
              void runAgain()
            }}
            className={quietButtonClass}
          >
            <RefreshIcon className="size-3.5" />
            Run again
          </button>
        )}
        <button
          type="button"
          disabled={openTerminal.pending}
          onClick={() => {
            press('terminal')
            void openTerminal.run()
          }}
          className={quietButtonClass}
        >
          <TerminalIcon className="size-3.5" />
          Open in terminal
        </button>
        <button
          type="button"
          disabled={copy.pending}
          onClick={() => {
            void copyLog()
          }}
          className={quietButtonClass}
        >
          {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
          {copied ? 'Copied' : copy.pending ? 'Copying…' : 'Copy log'}
        </button>
        <button
          type="button"
          disabled={reveal.pending}
          onClick={() => {
            press('reveal')
            void reveal.run()
          }}
          className={quietButtonClass}
        >
          <DocumentIcon className="size-3.5" />
          Show the log file
        </button>
      </div>
      <StopNote id={`${headingId}-stop`} run={run} terminal={terminal} />
      <ErrorText error={error} />
      {query.error !== '' && <ErrorBanner>Couldn’t read the run again: {query.error}</ErrorBanner>}
      {/* Each child keyed by the run needs a key of its own: siblings that share one, as
          these did with the log's, can be shown twice when the panel renders again. */}
      {ended && run.kind === 'hooks' && <HookResult key={`hook-${String(run.id)}`} runId={run.id} onSelect={onSelect} />}
      {ended && run.kind === 'checks' && <ChecksResult key={`checks-${String(run.id)}`} runId={run.id} />}
      {hasResult && <RunResult exitCode={run.exitCode} result={result} />}
      <LogView
        key={run.id}
        runId={run.id}
        live={isLive(run.status)}
        // A release's result shows what magex's JSON line says.
        hide={run.kind === 'release' ? isBumpResult : undefined}
      />
      {confirming !== '' && (
        <ConfirmDialog
          title="Stop this run?"
          confirmLabel="Stop the run"
          cancelLabel="Keep running"
          onConfirm={() => {
            void stopRun()
          }}
          onCancel={() => {
            setConfirming('')
          }}
        >
          {confirming}
        </ConfirmDialog>
      )}
    </section>
  )
}
