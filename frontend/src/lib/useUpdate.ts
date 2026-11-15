import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react'

import { brand } from '../generated/brand'
import {
  checkForUpdates,
  downloadUpdate,
  getUpdateStatus,
  hideUpdateBanner,
  listScriptRuns,
  on,
  restartToUpdateNow,
  showUpdateBanner,
  type Refused,
  type UpdateStatus,
} from '../platform'
import { attempt } from './call'
import { bannerOf, runsGoingIn, type Banner, type RestartStep } from './update'
import { useQuery } from './useQuery'

// UpdateQuery is the app's update as the window shows it.
export interface UpdateQuery {
  // status is null until the first answer.
  status: UpdateStatus | null
  // reload reads the status again now; it resolves once a read made since is answered.
  reload: () => Promise<void>
}

// useUpdate reads where the app's update stands, and reads it again on every
// "updates:updated", which follows each change, a download's progress included. A read
// that a newer one replaces is cancelled and its answer never shown (useQuery's rule), so
// the status shown is always the latest read's.
export function useUpdate(): UpdateQuery {
  const status = useQuery(() => getUpdateStatus(), [], { log: 'could not read the update’s status' })
  const readOnUpdate = useEffectEvent(() => {
    void status.refetch()
  })

  useEffect(
    () =>
      on('updates:updated', () => {
        readOnUpdate()
      }),
    [],
  )

  return { status: status.data ?? null, reload: status.refetch }
}

// downloadTries bounds how many downloads one restart makes: a check that finds a newer
// version while one downloads leaves that newer one to download next.
const downloadTries = 3

// waitFor is the longest a restart waits, quietly, after the last update said it is still
// finishing, before the banner offers the update again.
export const waitFor = 10_000

// RestartFlow is the window's one restart to update, which the banner, About, and the menu
// bar (through "updates:restart") all start.
export interface RestartFlow {
  step: RestartStep
  // start begins it, unless one is under way already.
  start: () => void
  // confirm and cancel answer the question it asks first.
  confirm: () => void
  cancel: () => void
  // dismiss puts a failure away.
  dismiss: () => void
}

// useRestartToUpdate runs the restart to update (restartToUpdate), the same however it was
// asked for:
//
//  1. With nothing found yet, as a Try again after a failed install may meet, it checks.
//  2. With the newer version not downloaded (Just tell me's Update, or one found since), it
//     downloads it first, which the banner shows as it goes.
//  3. It asks first when restarting would lose something: text a sheet holds that the
//     user hasn't sent (unsent counts the sheets), or runs going in the app, which Restart
//     names when it refuses to stop them unasked. Text unsent asks at once, naming the
//     runs going too, so one question covers both; otherwise Restart asks Go.
//  4. Restart then backs the data up and restarts; the app quits inside it, so it returns
//     only when something stopped the restart, whose words the banner shows.
//
// Two refusals are no failure to retry. While the last update may still be finishing, the
// download or the restart waits a moment (waiting), quietly, until the update next changes
// or waitFor passes. After a held quit, no restart works until the app is quit and opened
// again (reopen), which lasts the run.
//
// A start while one is under way, from a second click or the menu bar, does nothing. An
// app that can't update where it runs restarts nothing: the banner says why, shown again
// if Later hid it.
export function useRestartToUpdate(unsent: () => number): RestartFlow {
  const [step, setStep] = useState<RestartStep>({ step: 'idle' })
  // busy holds a restart from its start to its end, so nothing starts another meanwhile,
  // whichever render's handler asks.
  const busy = useRef(false)
  // counter is the latest unsent, read once the download is done, so text written while
  // the update downloaded counts too.
  const counter = useRef(unsent)

  useLayoutEffect(() => {
    counter.current = unsent
  })

  const end = (next: RestartStep) => {
    busy.current = false
    setStep(next)
  }

  const fail = (message: string) => {
    end({ step: 'failed', message })
  }

  // refuse ends on a refusal the window answers on its own: a moment's wait while the last
  // update finishes, or, after a held quit, what to do instead of trying again.
  const refuse = ({ refusal, message }: Refused) => {
    end(refusal === 'settling' ? { step: 'waiting', message } : { step: 'reopen', message })
  }

  const restart = async (confirmed: boolean) => {
    setStep({ step: 'restarting' })

    const answer = await attempt(restartToUpdateNow(confirmed))

    if (!answer.ok) {
      fail(answer.error)
    } else if (answer.data.restarted) {
      end({ step: 'idle' })
    } else if ('refusal' in answer.data) {
      refuse(answer.data)
    } else {
      const { runsGoing } = answer.data

      setStep({ step: 'asking', question: { runs: runsGoing.length === 0 ? null : runsGoing, sheets: 0 } })
    }
  }

  // ready makes sure there is an update downloaded to restart into, and returns its
  // status, or null once it has failed, or found the app can't update where it runs.
  const ready = async (): Promise<UpdateStatus | null> => {
    const read = await attempt(getUpdateStatus())

    if (!read.ok) {
      fail(read.error)

      return null
    }

    let status = read.data

    if (!status.enabled) {
      fail(status.off)

      return null
    }

    if (!status.location.ok) {
      const shown = await attempt(showUpdateBanner())

      if (!shown.ok) {
        console.error('could not show the update’s banner:', shown.error)
      }

      end({ step: 'idle' })

      return null
    }

    if (status.latest === '') {
      setStep({ step: 'checking' })

      const checked = await attempt(checkForUpdates())

      if (!checked.ok) {
        fail(checked.error)

        return null
      }

      status = checked.data

      if (status.latest === '') {
        fail(status.lastError === '' ? `No newer version of ${brand.name} is out.` : status.lastError)

        return null
      }
    }

    for (
      let tries = 0;
      tries < downloadTries && (status.state === 'available' || status.state === 'downloading');
      tries++
    ) {
      setStep({ step: 'downloading' })

      const downloaded = await attempt(downloadUpdate())

      if (!downloaded.ok) {
        fail(downloaded.error)

        return null
      }

      if (!downloaded.data.downloaded) {
        refuse(downloaded.data)

        return null
      }

      status = downloaded.data.status
    }

    return status
  }

  const run = async () => {
    setStep({ step: 'preparing' })

    if ((await ready()) === null) {
      return
    }

    const sheets = counter.current()

    if (sheets === 0) {
      await restart(false)

      return
    }

    const listed = await attempt(listScriptRuns({ kinds: [], repo: '' }))

    if (!listed.ok) {
      console.error('could not list the runs a restart would stop:', listed.error)
    }

    setStep({ step: 'asking', question: { runs: listed.ok ? runsGoingIn(listed.data) : [], sheets } })
  }

  const start = () => {
    if (busy.current) {
      return
    }

    busy.current = true
    void run()
  }

  // A wait while the last update finishes ends at the next change of the update, as the
  // start settling is, or after waitFor, whichever comes first: the banner then offers the
  // update again, and the same click goes ahead once it can.
  const settled = useEffectEvent(() => {
    setStep((was) => (was.step === 'waiting' ? { step: 'idle' } : was))
  })

  useEffect(
    () =>
      on('updates:updated', () => {
        settled()
      }),
    [],
  )

  useEffect(() => {
    if (step.step !== 'waiting') {
      return undefined
    }

    const timer = setTimeout(() => {
      settled()
    }, waitFor)

    return () => {
      clearTimeout(timer)
    }
  }, [step])

  return {
    step,
    start,
    confirm: () => {
      void restart(true)
    },
    cancel: () => {
      end({ step: 'idle' })
    },
    dismiss: () => {
      if (step.step === 'failed') {
        setStep({ step: 'idle' })
      }
    },
  }
}

// UpdateControl is the app's update as the banner and Settings → About use it: where it
// stands, the banner it shows, and the moves they make.
export interface UpdateControl {
  status: UpdateStatus | null
  flow: RestartStep
  banner: Banner
  // restart is restartToUpdate: Restart to update, Update, and Try again.
  restart: () => void
  // later hides the banner for a day (Later), at once.
  later: () => void
  // dismiss puts away the failure the banner shows, for this session.
  dismiss: () => void
  // reveal shows the banner again, lifting Later, as a click on the update's notification
  // asks; it resolves with whether an update is still on its way to show.
  reveal: () => Promise<boolean>
  reload: () => Promise<void>
}

// pendingStates are the states with a newer version on its way.
const pendingStates: readonly UpdateStatus['state'][] = ['available', 'downloading', 'ready', 'installing']

// useUpdateControl is the window's one view of the app's update: the status, the restart
// flow (unsent counts the sheets holding text not sent), and the banner, which Later hides
// at once and a failure put away leaves for this session. The menu bar's Restart to Update
// ("updates:restart") restarts as the banner does. After a held quit, when no restart can
// work in this run, asking for one shows the banner that says what to do instead, Later or
// not, and tries nothing.
export function useUpdateControl(flow: RestartFlow, update: UpdateQuery): UpdateControl {
  const { status, reload } = update
  // laterFor is the version Later hid the banner of in this window, before the status says
  // so; lastLaunchPut is the failed install the user put away.
  const [laterFor, setLaterFor] = useState('')
  const [lastLaunchPut, setLastLaunchPut] = useState('')
  const hidden = status !== null && status.latest !== '' && status.latest === laterFor
  const banner = bannerOf(hidden ? { ...status, bannerHidden: true } : status, flow.step, lastLaunchPut)

  const reveal = async () => {
    setLaterFor('')

    const shown = await attempt(showUpdateBanner())

    if (!shown.ok) {
      console.error('could not show the update’s banner:', shown.error)

      return true
    }

    void reload()

    return pendingStates.includes(shown.data.state)
  }

  const restart = () => {
    if (flow.step.step === 'reopen') {
      void reveal()
    } else {
      flow.start()
    }
  }

  const restartOnRequest = useEffectEvent(() => {
    restart()
  })

  useEffect(
    () =>
      on('updates:restart', () => {
        restartOnRequest()
      }),
    [],
  )

  return {
    status,
    flow: flow.step,
    banner,
    restart,
    later: () => {
      setLaterFor(status?.latest ?? '')
      void attempt(hideUpdateBanner()).then((result) => {
        if (!result.ok) {
          console.error('could not hide the update’s banner:', result.error)
        }

        void reload()
      })
    },
    dismiss: () => {
      if (flow.step.step === 'failed') {
        flow.dismiss()
      } else if (banner.kind === 'lastLaunch') {
        setLastLaunchPut(banner.version)
      }
    },
    reveal,
    reload,
  }
}
