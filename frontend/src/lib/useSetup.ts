import { useEffect, useState } from 'react'

import { finishSetup, getSetup, restartSetup, setSetupStep, type SetupStep } from '../platform'
import { attempt, logFailure, type Result } from './call'
import { setupFromURL, urlWithoutSetup } from './setup'
import { useQuery } from './useQuery'

// SetupControl is the first-run setup as the window shows it.
export interface SetupControl {
  // showing reports that the setup shows over the window: from the page's URL for the
  // first frame, then as its stored state says, then as the user finishes or runs it again.
  showing: boolean
  // step is the step it shows.
  step: SetupStep
  // rerun reports a setup the user ran again from Settings, which welcomes them back.
  rerun: boolean
  // go shows step, and remembers it, so a quit part way through opens there again.
  go: (step: SetupStep) => void
  // finish marks the setup done, as the user finished or skipped it, and shows the window
  // beneath it. A store that fails is logged: the next start shows the setup again.
  finish: () => void
  // restart opens the setup again from Welcome, as Settings → About asks, resolving with
  // how storing that went.
  restart: () => Promise<Result<void>>
}

// stripSetupParam takes the setup parameter out of the page's URL, so a reload opens the
// inbox: the text size keeps every other parameter as it writes its own.
function stripSetupParam(win: Window): void {
  const next = urlWithoutSetup(win.location.href)

  if (next !== null) {
    win.history.replaceState(win.history.state, '', next)
  }
}

// useSetup follows the first-run setup. Its first frame trusts the page's URL, which the
// shell writes from the stored state, so the window never flashes the inbox before the
// setup; the stored state, once read, is what counts, and a state that can't be read never
// shows the setup. Each step change is remembered; finishing hides the setup at once and
// stores it.
export function useSetup(win: Window = window): SetupControl {
  const [fromURL] = useState(() => setupFromURL(win.location.search))
  const stored = useQuery(() => getSetup(), [], { log: 'could not read the setup' })
  // decided is whether the setup shows once this run finished it or ran it again; null
  // until then.
  const [decided, setDecided] = useState<boolean | null>(null)
  const [chosen, setChosen] = useState<SetupStep | null>(null)
  const [rerun, setRerun] = useState(false)

  const read = stored.data
  const showing = decided ?? (read === undefined ? fromURL && stored.error === '' : !read.done)
  const step = chosen ?? read?.step ?? 'welcome'

  // A setup that doesn't show leaves no parameter in the URL behind it.
  useEffect(() => {
    if (!showing && read !== undefined) {
      stripSetupParam(win)
    }
  }, [showing, read, win])

  const go = (next: SetupStep) => {
    setChosen(next)
    // A store that fails is logged: the step shows already.
    logFailure(setSetupStep(next), 'could not remember the setup’s step:')
  }

  const finish = () => {
    setDecided(false)
    stripSetupParam(win)
    logFailure(finishSetup(), 'could not store that the setup is done:')
  }

  const restart = async (): Promise<Result<void>> => {
    const result = await attempt(restartSetup())

    if (!result.ok) {
      return result
    }

    setChosen('welcome')
    setRerun(true)
    setDecided(true)

    return { ok: true, data: undefined }
  }

  return { showing, step, rerun, go, finish, restart }
}
