import { useEffect, useEffectEvent, useState, type ReactNode } from 'react'

import { brand } from '../generated/brand'
import { attempt } from '../lib/call'
import { counted } from '../lib/format'
import { runWhere } from '../lib/scriptRuns'
import { listScriptRuns, on, quitAnyway, type ScriptRun } from '../platform'
import { ConfirmDialog } from './ConfirmDialog'

// tagRisk is what stopping a release partway can leave behind.
const tagRisk = 'Stopping it now can leave a tag on this Mac but not on GitHub.'

// held reports a run whose kind holds Stop back for now, as a release does while magex
// switches branches or pulls, and a pull while git writes the clone's files.
function held(run: ScriptRun): boolean {
  return run.progress.stopRefused !== ''
}

// heldText says what quitting does to releases held back: it waits a few seconds for them,
// and one still held then goes on after the app closes, and is checked at the next start.
function heldText(one: boolean): string {
  return one
    ? `magex is switching branches or pulling, so stopping waits up to a few seconds for it to finish. If it doesn’t, the release goes on after ${brand.name} closes, and ${brand.name} checks how it ended when it opens again.`
    : `A release is switching branches or pulling, so stopping waits up to a few seconds for it. If it doesn’t finish, that release goes on after ${brand.name} closes, and ${brand.name} checks how it ended when it opens again.`
}

// heldOtherText says the same of any other run held back, as a pull is while git writes
// the clone's files.
function heldOtherText(one: boolean): string {
  return one
    ? `Stopping waits up to a few seconds for it to finish that. If it doesn’t, it goes on after ${brand.name} closes.`
    : `A run that can’t be stopped right now is waited for up to a few seconds. If it doesn’t finish, it goes on after ${brand.name} closes.`
}

// question asks whether to quit while runs go in the app: one run by its name and where
// it runs, with what stopping it can leave, or several listed. With none known, since
// they couldn't be listed, it asks about the runs as a whole.
function question(going: readonly ScriptRun[]): ReactNode {
  const [only] = going

  if (only === undefined) {
    return `Runs are going in ${brand.name}. Stopping them now ends them partway. Quit anyway?`
  }

  if (going.length === 1) {
    if (held(only) && only.kind === 'release') {
      return `A release is running in ${runWhere(only)}. ${heldText(true)} Quit anyway?`
    }

    if (held(only)) {
      return `${only.title} is running in ${runWhere(only)}. ${only.progress.stopRefused} ${heldOtherText(true)} Quit anyway?`
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
        {releases && ' Stopping a release now can leave a tag on this Mac but not on GitHub.'}
        {holding && ` ${heldText(false)}`}
        {holdingOther && ` ${heldOtherText(false)}`}
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

// QuitDialog asks, when the user quits while runs go in the app, whether to stop them and
// quit or keep running. The shell asks for it (app:quitRequested); by then a run may have
// ended, so it lists the runs again, and quits at once when none is going any more. It
// shows nothing until asked.
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

  const ask = useEffectEvent(async () => {
    const listed = await attempt(listScriptRuns({ kinds: [], repo: '' }))

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

  return (
    <ConfirmDialog
      title={`Quit ${brand.name}?`}
      confirmLabel="Stop and quit"
      cancelLabel="Keep running"
      size={going.length > 1 ? 'wide' : 'normal'}
      onConfirm={quit}
      onCancel={() => {
        setGoing(null)
      }}
    >
      {question(going)}
    </ConfirmDialog>
  )
}
