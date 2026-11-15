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
  type RunGoing,
  type UpdateStatus,
} from '../platform'
import { attempt } from './call'
import {
  bannerOf,
  covers,
  finishingWords,
  offerOf,
  runsGoingIn,
  settlingWords,
  type Banner,
  type Offer,
  type RestartQuestion,
  type RestartStep,
} from './update'
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
// finishing, before the banner offers the update again: the start settling, which tells
// the window, usually ends the wait well before.
export const waitFor = 60_000

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
//     While a restart is under way already, or one whose quit was held sees the app out,
//     it stops there, asking nothing: the banner shows that restart. A banner Later hid
//     shows again, so how this restart ends shows there.
//  2. With the newer version not downloaded (Just tell me's Update, or one found since), it
//     downloads it first, which the banner shows as it goes.
//  3. It asks first when restarting would lose something: text a sheet holds that the
//     user hasn't sent (unsent counts the sheets), or runs going in the app, which Restart
//     names when it refuses to stop them unasked. Text unsent asks at once, naming the
//     runs going too, so one question covers both; otherwise Restart(false) asks Go, and
//     a refusal asks, counting the sheets again.
//  4. Confirmed, it asks Go again with Restart(false), which refuses before it backs
//     anything up while runs go: only when the question named every run Go names then
//     (covers) does it restart with Restart(true), which stops them, so a run that started
//     while it asked, or one going when the question named none, is asked about again.
//  5. Restart then backs the data up and restarts; the app quits inside it, so it returns
//     only when something stopped the restart, whose words the banner shows.
//
// Some refusals are no failure to retry. While the last update may still be finishing, or
// a restart whose quit was held sees the app out, the download or the restart waits a
// moment (waiting), quietly, until the update next changes or waitFor passes. An update
// that failed to install for good fails (final) with nothing to try again: its release
// page is the way to it.
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

  // refuse ends on a refusal the window answers on its own, in its own words for a wait: a
  // wait while the last update finishes or a held quit's restart sees the app out, or an
  // update that can't be tried again, in the backend's words.
  const refuse = ({ refusal, message }: Refused) => {
    switch (refusal) {
      case 'settling':
        end({ step: 'waiting', message: settlingWords })
        break
      case 'finishing':
        end({ step: 'waiting', message: finishingWords })
        break
      case 'uninstallable':
        end({ step: 'failed', message, final: true })
        break
    }
  }

  // ask asks first, naming runs, the sheets counted as they are now.
  const ask = (runs: readonly RunGoing[] | null) => {
    setStep({ step: 'asking', question: { runs, sheets: counter.current() } })
  }

  // restart calls Restart(confirmed), asked being the question the user confirmed, or null
  // when nothing was asked. Unconfirmed, a refusal for the runs going restarts confirmed
  // only when asked named them all, and asks otherwise.
  const restart = async (asked: RestartQuestion | null, confirmed = false): Promise<void> => {
    setStep({ step: 'restarting' })

    const answer = await attempt(restartToUpdateNow(confirmed))

    if (!answer.ok) {
      fail(answer.error)
    } else if (answer.data.restarted) {
      end({ step: 'idle' })
    } else if ('refusal' in answer.data) {
      refuse(answer.data)
    } else if (!confirmed && asked !== null && covers(asked, answer.data.runsGoing)) {
      await restart(asked, true)
    } else {
      // Restart may have waited for a check or a download first: the sheets are counted
      // as they are now, text written meanwhile included.
      const { runsGoing } = answer.data

      ask(runsGoing.length === 0 ? null : runsGoing)
    }
  }

  // ready makes sure there is an update downloaded to restart into, and returns its
  // status, or null once it has failed, found the app can't update where it runs, or found
  // a restart under way already.
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

    // A restart under way, or one whose quit was held, quits the app on its own: nothing
    // is asked, since nothing the user answers would keep their text.
    if (status.state === 'finishing') {
      end({ step: 'waiting', message: finishingWords })

      return null
    }

    if (status.state === 'installing') {
      end({ step: 'idle' })

      return null
    }

    if (status.bannerHidden || !status.location.ok) {
      const shown = await attempt(showUpdateBanner())

      if (!shown.ok) {
        console.error('could not show the update’s banner:', shown.error)
      }
    }

    if (!status.location.ok) {
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
      await restart(null)

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
      if (step.step === 'asking') {
        void restart(step.question)
      }
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
// stands, the banner it shows, what both offer, and the moves they make.
export interface UpdateControl {
  status: UpdateStatus | null
  flow: RestartStep
  banner: Banner
  // offer is what the banner and About offer (offerOf), so the two never disagree.
  offer: Offer
  // restart is restartToUpdate: Restart to update, Update, and Try again.
  restart: () => void
  // later hides the banner for a day (Later), at once; on a download's banner, it hides
  // that banner while the download goes on.
  later: () => void
  // dismiss puts away the failure the banner shows, for this session.
  dismiss: () => void
  // reveal shows the banner again, lifting Later, as a click on the update's notification
  // asks; it resolves with whether the banner shows something.
  reveal: () => Promise<boolean>
  reload: () => Promise<void>
}

// noBanner is the banner showing nothing.
const noBanner: Banner = { kind: 'none' }

// useUpdateControl is the window's one view of the app's update: the status, the restart
// flow (unsent counts the sheets holding text not sent), the banner, which Later hides at
// once and a failure put away leaves for this session, and what both the banner and About
// offer. The menu bar's Restart to Update, Update to X…, and Try Updating to X Again
// ("updates:restart") restart as the banner does.
export function useUpdateControl(flow: RestartFlow, update: UpdateQuery): UpdateControl {
  const { status, reload } = update
  // laterFor is the version Later hid the banner of in this window, until the status says
  // the update hides it, for its day; it lasts the session only when the update couldn't
  // store that (HideBanner failed). lastLaunchPut is the failed install the user put away,
  // and downloadHidden a download's banner Later hid while it downloads.
  const [laterFor, setLaterFor] = useState('')
  const [lastLaunchPut, setLastLaunchPut] = useState('')
  const [downloadHidden, setDownloadHidden] = useState(false)

  if (laterFor !== '' && status !== null && status.bannerHidden && status.latest === laterFor) {
    setLaterFor('')
  }

  const hidden = status !== null && status.latest !== '' && status.latest === laterFor
  const shown = bannerOf(hidden ? { ...status, bannerHidden: true } : status, flow.step, lastLaunchPut)
  const downloading = shown.kind === 'working' && shown.progress !== null

  if (downloadHidden && !downloading) {
    setDownloadHidden(false)
  }

  const banner = downloadHidden && downloading ? noBanner : shown

  const reveal = async () => {
    setLaterFor('')
    setDownloadHidden(false)

    const lifted = await attempt(showUpdateBanner())

    if (!lifted.ok) {
      console.error('could not show the update’s banner:', lifted.error)

      return bannerOf(status, flow.step, lastLaunchPut).kind !== 'none'
    }

    void reload()

    return bannerOf(lifted.data, flow.step, lastLaunchPut).kind !== 'none'
  }

  // A restart shows the banner again, so how it ends shows there: Later held here goes at
  // once, and the restart lifts Later the update stored before it goes on.
  const restart = () => {
    setLaterFor('')
    setDownloadHidden(false)
    flow.start()
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
    offer: offerOf(status, flow.step),
    restart,
    later: () => {
      if (downloading) {
        setDownloadHidden(true)

        return
      }

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
