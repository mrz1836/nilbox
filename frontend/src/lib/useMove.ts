import { useEffect, useEffectEvent, useRef, useState } from 'react'

import {
  moveToApplications,
  on,
  openInApplications,
  showAppInFinder,
  type MoveResult,
  type UpdateLocation,
} from '../platform'
import { attempt } from './call'
import type { Offer } from './update'

// MoveVariant is which question the move dialog asks: move asks to move the app into
// Applications; exists says a copy is there already, and offers to open it.
export type MoveVariant = 'move' | 'exists'

// MoveWork is what the move dialog is doing: moving the app, or opening the copy in
// Applications, or nothing; leaving says that work went ahead, and the app is quitting so
// the copy opens.
export type MoveWork = 'moving' | 'opening' | null

// MoveDialogState is what the move dialog shows while it is open: its question, the work
// under way, whether the app is quitting for the copy, and the last failure's words, or ''.
export interface MoveDialogState {
  variant: MoveVariant
  work: MoveWork
  leaving: boolean
  error: string
}

// MoveControl is the app's move into Applications as the window offers it: in a dialog on
// every launch from where the app can't update itself, until it is moved, and from the
// update banner and Settings → About, wherever the update's offer says (Offer.move).
export interface MoveControl {
  // location is where the app runs, for the dialog's words, or null until the status loads.
  location: UpdateLocation | null
  // dialog is what the move dialog shows, or null while it is closed.
  dialog: MoveDialogState | null
  // move moves the app at once, as the banner's and About's Move to Applications do,
  // showing the dialog as it works.
  move: () => void
  // confirm is the dialog's main button: Move to Applications, or Open it.
  confirm: () => void
  // dismiss is Not now: it closes the dialog for this launch, unless work is under way.
  dismiss: () => void
  // reveal shows the copy the user opened in Finder, which a move leaves where it is.
  reveal: () => void
}

// useMoveToApplications runs the move into Applications, the same however it is asked for,
// from where the app runs (location, null until the status loads). Wherever the update's
// offer (offerOf, which the banner and About offer from too) offers the move, the dialog
// opens as the status first loads, on every launch until the app is moved; Not now closes
// it for this launch, and the banner and About still offer the move. A move copies the app into
// Applications and checks it (MoveToApplications), then the app quits the normal way and
// the copy opens; a copy there already is never replaced, and the dialog offers to open it
// (OpenInApplications) instead. A second click while one is under way does nothing, and a
// failure shows its words in the dialog, with nothing changed.
//
// The quit asks first while runs go in the app, in its own dialog: the move dialog closes
// then, and if the user keeps the app running, the copy waits in Applications, and opens
// at the next quit. That question can come before the move's own answer, so a quit asked
// while the work is under way closes the dialog as soon as the work has gone ahead.
export function useMoveToApplications(offer: Offer, location: UpdateLocation | null): MoveControl {
  // shown is whether the dialog shows: on launch, as soon as the move is offered; open once
  // the banner or About asks for it; closed once put away.
  const [shown, setShown] = useState<'launch' | 'open' | 'closed'>('launch')
  const [variant, setVariant] = useState<MoveVariant>('move')
  const [work, setWork] = useState<MoveWork>(null)
  const [leaving, setLeaving] = useState(false)
  const [error, setError] = useState('')
  // stage is where the work stands, whichever render's handler reads it: idle; working,
  // from its start until it fails or goes ahead; or leaving, once it went ahead and the app
  // quits. A click starts nothing but while idle. quitAsked records that the quit asked
  // first while the work was under way.
  const stage = useRef<'idle' | 'working' | 'leaving'>('idle')
  const quitAsked = useRef(false)

  // giveWay closes the dialog for the quit's own question, ready to move again.
  const giveWay = () => {
    stage.current = 'idle'
    quitAsked.current = false
    setShown('closed')
    setVariant('move')
    setWork(null)
    setLeaving(false)
  }

  const run = async (call: () => PromiseLike<MoveResult>, step: Exclude<MoveWork, null>) => {
    if (stage.current !== 'idle') {
      return
    }

    stage.current = 'working'
    setError('')
    setWork(step)

    const answer = await attempt(call())

    if (answer.ok && answer.data.outcome !== 'exists') {
      // The app quits now, and the copy opens once it has. A quit the user is asked about
      // already has its own dialog.
      if (quitAsked.current) {
        giveWay()

        return
      }

      stage.current = 'leaving'
      setLeaving(true)

      return
    }

    stage.current = 'idle'
    quitAsked.current = false
    setWork(null)

    if (answer.ok) {
      setVariant('exists')
    } else {
      setError(answer.error)
    }
  }

  const confirm = () => {
    if (variant === 'exists') {
      void run(openInApplications, 'opening')
    } else {
      void run(moveToApplications, 'moving')
    }
  }

  // A quit held while runs go asks in its own dialog: this one gives way, and the copy opens
  // at whichever quit goes through.
  const onQuitAsked = useEffectEvent(() => {
    if (stage.current === 'leaving') {
      giveWay()
    } else if (stage.current === 'working') {
      quitAsked.current = true
    }
  })

  useEffect(
    () =>
      on('app:quitRequested', () => {
        onQuitAsked()
      }),
    [],
  )

  const open = offer.move && shown !== 'closed'

  return {
    location,
    dialog: open ? { variant, work, leaving, error } : null,
    move: () => {
      if (stage.current !== 'idle') {
        return
      }

      setShown('open')
      setVariant('move')
      void run(moveToApplications, 'moving')
    },
    confirm,
    dismiss: () => {
      if (stage.current !== 'idle') {
        return
      }

      setShown('closed')
      setVariant('move')
      setError('')
    },
    reveal: () => {
      void attempt(showAppInFinder()).then((shownInFinder) => {
        if (!shownInFinder.ok) {
          setError(shownInFinder.error)
        }
      })
    },
  }
}
