// What the sheets over the inbox share: where the card they show sits among the cards they
// step through, their keys, and the steps of an action under way.

import { useEffect, useEffectEvent, useState } from 'react'

import { on, type InboxItem } from '../platform'
import { isTyping } from './keys'
import { isTopmost } from './useModalDialog'

// Place is where a card sits among the cards a sheet steps through: the cards before and
// after it, if any, and its position, as in "3 of 8 in Assigned", or '' when the list
// doesn't hold it.
export interface Place {
  prev: InboxItem | undefined
  next: InboxItem | undefined
  position: string
}

// placeIn finds the card with id among siblings, the cards of the list titled listTitle in
// the order it shows them.
export function placeIn(siblings: readonly InboxItem[], id: number, listTitle: string): Place {
  const at = siblings.findIndex((s) => s.id === id)

  return {
    prev: at > 0 ? siblings[at - 1] : undefined,
    next: at >= 0 ? siblings[at + 1] : undefined,
    position: at < 0 ? '' : `${String(at + 1)} of ${String(siblings.length)} in ${listTitle}`,
  }
}

// SheetStep is a step to the card before or after the one a sheet shows: the title of the
// card it steps to, and the step.
export interface SheetStep {
  title: string
  onStep: () => void
}

// stepTo is the step to the card to, which onNavigate takes, or null when there is none.
export function stepTo(to: InboxItem | undefined, onNavigate: (id: number) => void): SheetStep | null {
  if (to === undefined) {
    return null
  }

  return {
    title: to.title,
    onStep: () => {
      onNavigate(to.id)
    },
  }
}

// SheetBack is the way back to the sheet a link was followed from: its words, as in
// "acme-org/api#12", the title of what it shows, and the step back.
export interface SheetBack {
  label: string
  title: string
  onBack: () => void
}

// SheetKeys is what a sheet's keys do.
export interface SheetKeys {
  // working holds every key back while an action runs, so its outcome is never lost.
  working: boolean
  // run runs the sheet's button, as ⌘↩ asks; it checks for itself whether the button's
  // action is held back.
  run: () => void
  // prev and next step to the card before and after, or are null at the ends.
  prev: SheetStep | null
  next: SheetStep | null
}

// useSheetKeys gives the sheet whose title has titleId its keys while it is the dialog the
// user is in: ⌘↩ runs its button, and ← and → step through its list while the user isn't
// typing, with no modifier held.
export function useSheetKeys(titleId: string, keys: SheetKeys): void {
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    const dialog = document.querySelector(`dialog[aria-labelledby="${titleId}"]`)

    if (!(dialog instanceof HTMLDialogElement) || !isTopmost(dialog) || keys.working) {
      return
    }

    if (e.key === 'Enter' && e.metaKey) {
      e.preventDefault()
      keys.run()

      return
    }

    if (isTyping(e) || e.metaKey || e.altKey || e.ctrlKey) {
      return
    }

    const step = e.key === 'ArrowLeft' ? keys.prev : e.key === 'ArrowRight' ? keys.next : null

    if (step !== null) {
      e.preventDefault()
      step.onStep()
    }
  })

  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      onKey(e)
    }

    document.addEventListener('keydown', listener)

    return () => {
      document.removeEventListener('keydown', listener)
    }
  }, [])
}

// Progress is an action under way: what it is, the item it acts on, and the steps the
// backend reported so far.
export interface Progress {
  title: string
  target: string
  steps: readonly string[]
}

// withStep adds a step the backend reported to progress, once: a step repeated, as the
// wait for GitHub to work out a merge can be, shows as one.
export function withStep(progress: Progress | null, step: string): Progress | null {
  if (progress === null || progress.steps.at(-1) === step) {
    return progress
  }

  return { ...progress, steps: [...progress.steps, step] }
}

// ProgressEvent names an event that reports the steps of a sheet's action.
export type ProgressEvent = 'review:progress' | 'issue:progress'

// useActionProgress follows an action on the item with id as the backend reports its
// steps on event, from the moment the sheet sets it under way until the sheet sets it to
// null. A step about another item, from before the sheet stepped to this one, is never
// shown.
export function useActionProgress(
  event: ProgressEvent,
  id: number,
): [Progress | null, (progress: Progress | null) => void] {
  const [progress, setProgress] = useState<Progress | null>(null)

  const heard = useEffectEvent((itemId: number, step: string) => {
    if (itemId === id) {
      setProgress((p) => withStep(p, step))
    }
  })

  useEffect(
    () =>
      on(event, (data) => {
        heard(data.itemId, data.step)
      }),
    [event],
  )

  return [progress, setProgress]
}
