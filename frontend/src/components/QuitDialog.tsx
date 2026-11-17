import { useEffect, useEffectEvent, useState, type ReactNode } from 'react'

import { brand } from '../generated/brand'
import { attempt } from '../lib/call'
import { counted } from '../lib/format'
import { carryOnText, heldReleaseText, heldRunText, releaseTagRisk, tagRisk } from '../lib/runsGoing'
import { runWhere } from '../lib/scriptRuns'
import { keepRunning, letThemFinish, listScriptRuns, on, quitAnyway, type ScriptRun } from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { quietButtonClass } from './controls'

// held reports a run whose kind holds Stop back for now, as a release does while magex
// switches branches or pulls, and a pull while git writes the clone's files.
function held(run: ScriptRun): boolean {
  return run.progress.stopRefused !== ''
}

// isAgent reports an agent's run in the background.
function isAgent(run: ScriptRun): boolean {
  return run.kind === 'agent'
}

// agentRunsQuestion says, in Autopilot's words, that only agents are working: one by its
// name, several counted and listed.
function agentRunsQuestion(agents: readonly ScriptRun[]): ReactNode {
  const [only] = agents

  if (agents.length === 1 && only !== undefined) {
    return `${only.title} is still working. Stopping it now ends it partway.`
  }

  return (
    <>
      <p>{counted(agents.length, 'agent is', 'agents are')} still working. Stopping them now ends them partway.</p>
      <ul className="my-2 list-disc pl-5">
        {agents.map((run) => (
          <li key={run.id} className="text-text">
            {run.title}
          </li>
        ))}
      </ul>
    </>
  )
}

// question asks whether to quit while runs go in the app: one run by its name and where
// it runs, with what stopping it can leave, or several listed; the agents working alone,
// in Autopilot's words. With none known, since they couldn't be listed, it asks about the
// runs as a whole.
function question(going: readonly ScriptRun[]): ReactNode {
  const [only] = going

  if (only === undefined) {
    return `Runs are going in ${brand.name}. Stopping them now ends them partway. Quit anyway?`
  }

  if (going.every(isAgent)) {
    return agentRunsQuestion(going)
  }

  if (going.length === 1) {
    if (held(only) && only.kind === 'release') {
      return `A release is running in ${runWhere(only)}. ${heldReleaseText(true)} Quit anyway?`
    }

    if (held(only)) {
      return `${only.title} is running in ${runWhere(only)}. ${only.progress.stopRefused} ${heldRunText(true)} Quit anyway?`
    }

    return only.kind === 'release'
      ? `A release is running in ${runWhere(only)}. ${tagRisk} Quit anyway?`
      : `${only.title} is running in ${runWhere(only)}. Stopping it now ends it partway. Quit anyway?`
  }

  const releases = going.some((run) => run.kind === 'release')
  const holding = going.some((run) => held(run) && run.kind === 'release')
  const holdingOther = going.some((run) => held(run) && run.kind !== 'release')

  return (
    <>
      <p>
        {counted(going.length, 'run is', 'runs are')} going in {brand.name}. Stopping them now ends them partway.
        {releases && ` ${releaseTagRisk}`}
        {holding && ` ${heldReleaseText(false)}`}
        {holdingOther && ` ${heldRunText(false)}`}
      </p>
      <ul className="my-2 list-disc pl-5">
        {going.map((run) => (
          <li key={run.id}>
            <span className="text-text">{run.title}</span> in {runWhere(run)}
          </li>
        ))}
      </ul>
      <p>Quit anyway?</p>
    </>
  )
}

// LetFinish offers to quit and let the agents working finish on their own: offered while
// every one of them keeps to a budget of its own, which bounds it while no copy of the app
// follows it, so the next start picks each up again. Any other run still stops.
function LetFinish({ going, onLetFinish }: { going: readonly ScriptRun[]; onLetFinish: () => void }) {
  const agents = going.filter(isAgent)
  const others = going.length - agents.length

  return (
    <div className="mt-3 flex flex-col items-start gap-1">
      <button type="button" onClick={onLetFinish} className={quietButtonClass}>
        {agents.length === 1 ? 'Let it finish' : 'Let them finish'}
      </button>
      <p className="text-xs text-muted">
        {carryOnText(agents.length)}
        {others > 0 && ` ${counted(others, 'other run stops', 'other runs stop')}.`}
      </p>
    </div>
  )
}

// stopsWithTheApp names the agents' runs that stop with the app, whatever the user chooses,
// since their agent keeps to no budget of its own that would bound it while the app is
// closed.
function StopsWithTheApp({ runs }: { runs: readonly ScriptRun[] }) {
  if (runs.length === 0) {
    return null
  }

  const names = runs.map((run) => run.title).join(', ')
  const why =
    runs.length === 1
      ? `stops with ${brand.name}: its agent keeps to no budget of its own, so nothing would bound it while ${brand.name} is closed.`
      : `stop with ${brand.name}: their agents keep to no budget of their own, so nothing would bound them while ${brand.name} is closed.`

  return <p className="mt-2 text-xs text-muted">{`${names} ${why}`}</p>
}

// QuitDialog asks, when the user quits while runs go in the app, whether to stop them and
// quit or keep running; and, while every agent's run going can outlive the quit, whether
// to quit and let the agents finish, a quiet button in its body. The shell asks for it
// (app:quitRequested); by then a run may have ended, so it lists the runs again, and quits
// at once when none is going any more. Keep running tells the shell, which forgets what the
// quit was to do after, so the copy a move to Applications asked to open doesn't open at
// some later quit. It shows nothing until asked.
export function QuitDialog() {
  // going are the runs it asks about, or null while it asks nothing.
  const [going, setGoing] = useState<readonly ScriptRun[] | null>(null)

  const quit = () => {
    setGoing(null)
    void attempt(quitAnyway()).then((result) => {
      if (!result.ok) {
        console.error('could not quit:', result.error)
      }
    })
  }

  const letFinish = () => {
    setGoing(null)
    void attempt(letThemFinish()).then((result) => {
      if (!result.ok) {
        console.error('could not quit letting the agents finish:', result.error)
      }
    })
  }

  const ask = useEffectEvent(async () => {
    const listed = await attempt(listScriptRuns({ kinds: [], repo: '', withAgents: true }))

    if (!listed.ok) {
      console.error('could not list the runs going as the app quits:', listed.error)
      setGoing([])

      return
    }

    // A run held back from stopping for now still goes in the app, and quitting waits for
    // it, so it is asked about too.
    const stoppable = listed.data.filter((run) => run.canStop || held(run))

    if (stoppable.length === 0) {
      quit()
    } else {
      setGoing(stoppable)
    }
  })

  useEffect(
    () =>
      on('app:quitRequested', () => {
        void ask()
      }),
    [],
  )

  if (going === null) {
    return null
  }

  const agents = going.filter(isAgent)
  const outliving = agents.filter((run) => run.outlivesQuit)
  // Let them finish is offered while every agent's run going can outlive the quit.
  const canLetFinish = agents.length > 0 && outliving.length === agents.length
  const confirmLabel =
    agents.length === 0 ? 'Stop and quit' : going.length === 1 ? 'Stop it and quit' : 'Stop them and quit'

  return (
    <ConfirmDialog
      title={`Quit ${brand.name}?`}
      confirmLabel={confirmLabel}
      cancelLabel="Keep running"
      size={going.length > 1 ? 'wide' : 'normal'}
      onConfirm={quit}
      onCancel={() => {
        setGoing(null)
        void attempt(keepRunning()).then((result) => {
          if (!result.ok) {
            console.error('could not keep running:', result.error)
          }
        })
      }}
    >
      {question(going)}
      {canLetFinish ? (
        <LetFinish going={going} onLetFinish={letFinish} />
      ) : (
        <StopsWithTheApp runs={agents.filter((run) => !run.outlivesQuit)} />
      )}
    </ConfirmDialog>
  )
}
