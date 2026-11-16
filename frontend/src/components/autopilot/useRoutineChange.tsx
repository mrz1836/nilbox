import { useState, type ReactNode } from 'react'

import { matchingWords, modeLabel } from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import { counted } from '../../lib/format'
import { watchChanged } from '../../lib/routineBuilder'
import {
  routineMatching,
  saveRoutine,
  setRoutineEnabled,
  setRoutineMode,
  type AutopilotCurrent,
  type AutopilotDraft,
  type AutopilotMatching,
  type AutopilotMode,
  type AutopilotRoutine,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

// RoutineChange is a change the user asked for from a routine's switch, its menu, its
// mode, or its builder: switching it on or off, another mode, or saving the builder's
// draft over it.
export type RoutineChange =
  { kind: 'enable'; on: boolean } | { kind: 'mode'; mode: AutopilotMode } | { kind: 'save'; draft: AutopilotDraft }

// Question is the question asked before a change that can act on what the routine matches
// now: how many items it matches, the change it waits to make, what hears the routine once
// it is made, and what hears why it couldn't be.
interface Question {
  routine: AutopilotRoutine
  change: RoutineChange
  matching: AutopilotMatching
  then: ((routine: AutopilotRoutine) => void) | undefined
  fail: ((text: string) => void) | undefined
}

// draftOf is a routine as the question counts its matches: as it is saved, or for a save,
// as the draft would save it.
function draftOf(r: AutopilotRoutine, change: RoutineChange): AutopilotDraft {
  if (change.kind === 'save') {
    return { ...change.draft, id: r.id }
  }

  return { id: r.id, name: r.name, description: r.description, mode: r.mode, spec: r.spec, templateKey: r.templateKey }
}

// asks reports a change the question is asked about: switching a routine on, taking one
// that is on out of Dry run, or saving a change to what one that is on watches, fires on,
// checks, or fires once per.
function asks(r: AutopilotRoutine, change: RoutineChange): boolean {
  switch (change.kind) {
    case 'enable':
      return change.on
    case 'mode':
      return r.enabled && r.mode === 'dryRun' && change.mode !== 'dryRun'
    case 'save':
      return r.enabled && watchChanged(r.spec, change.draft.spec)
  }
}

// questionBody says what each answer does, for the change asked about, with what it can't
// tell yet and what it couldn't read.
function questionBody({ routine, change, matching }: Question): string {
  const dry = change.kind !== 'mode' && routine.mode === 'dryRun'
  const once =
    change.kind === 'enable'
      ? `Once “${routine.name}” is on`
      : change.kind === 'save'
        ? `Once the change to “${routine.name}” is saved`
        : `Once “${routine.name}” is in ${modeLabel(change.mode)}`
  const them = matching.count > 0 || matching.unknown > 0 ? 'them' : 'whatever matches now'
  const unknown =
    matching.unknown > 0
      ? ` ${counted(matching.unknown, matching.count > 0 ? 'more item' : 'item', matching.count > 0 ? 'more items' : 'items')} can’t be told yet, until what it checks is read.`
      : ''
  const unread = matching.incomplete
    ? ' Not everything it watches could be read just now, so more may match than this says.'
    : ''

  return dry
    ? `${once}, it records what it would do, in Dry run: for ${them} too, or only for what changes from now on.${unknown}${unread}`
    : `${once}, it can act on ${them} too, or only on what changes from now on.${unknown}${unread}`
}

// asksAbout reports a count the question is asked about: any match, any item it can't tell
// yet, or a count that may be short, since the engine acts on those later all the same.
function asksAbout(m: AutopilotMatching): boolean {
  return m.count > 0 || m.unknown > 0 || m.incomplete
}

export interface RoutineChanges {
  // request makes a change, asking first when it can act on what the routine matches now;
  // then, when given, hears the routine as changed, and fail, in onError's place, why it
  // couldn't be made.
  request: (
    routine: AutopilotRoutine,
    change: RoutineChange,
    then?: (routine: AutopilotRoutine) => void,
    fail?: (text: string) => void,
  ) => void
  // busy is the routine a change is under way for, 0 for none, and asking reports the
  // question open, while a change waits for its answer.
  busy: number
  asking: boolean
  // dialog is the question, while it is asked.
  dialog: ReactNode
}

// useRoutineChange switches routines on and off, changes their modes, and saves the
// builder's drafts over them. Switching one on, taking one that is on out of Dry run, or
// saving a change to what one that is on watches, fires on, checks, or fires once per
// first counts what it matches now (Matching, which reads its searches unless a test just
// did); when that is any, or any it can't tell yet, or the count may be short, it asks "3
// pull requests match right now": Run on them too, or Only what changes from now on, and
// the change and the answer go together. Escape asks nothing more and changes nothing.
// onDone hears the routine as changed, and onError why a change couldn't be made, in the
// backend's words (as "Autopilot is running in another copy..."), unless the request says
// where.
export function useRoutineChange(
  onDone: (routine: AutopilotRoutine) => void,
  onError: (text: string) => void,
): RoutineChanges {
  const [question, setQuestion] = useState<Question | null>(null)
  const [busy, setBusy] = useState(0)

  const call = (routine: AutopilotRoutine, change: RoutineChange, current: AutopilotCurrent) => {
    switch (change.kind) {
      case 'enable':
        return setRoutineEnabled(routine.id, change.on, current)
      case 'mode':
        return setRoutineMode(routine.id, change.mode, current)
      case 'save':
        return saveRoutine({ ...change.draft, id: routine.id, current })
    }
  }

  const apply = async (
    routine: AutopilotRoutine,
    change: RoutineChange,
    current: AutopilotCurrent,
    then: ((routine: AutopilotRoutine) => void) | undefined,
    fail: ((text: string) => void) | undefined,
  ) => {
    setBusy(routine.id)

    const result = await attempt(call(routine, change, current))

    setBusy(0)
    setQuestion(null)

    if (result.ok) {
      onDone(result.data)
      then?.(result.data)
    } else {
      const report = fail ?? onError

      report(result.error)
    }
  }

  const request = (
    routine: AutopilotRoutine,
    change: RoutineChange,
    then?: (routine: AutopilotRoutine) => void,
    fail?: (text: string) => void,
  ) => {
    if (!asks(routine, change)) {
      void apply(routine, change, 'run', then, fail)

      return
    }

    setBusy(routine.id)
    void attempt(routineMatching(draftOf(routine, change))).then((counted) => {
      if (!counted.ok) {
        const report = fail ?? onError

        setBusy(0)
        report(counted.error)

        return
      }

      if (!asksAbout(counted.data)) {
        void apply(routine, change, 'run', then, fail)

        return
      }

      setBusy(0)
      setQuestion({ routine, change, matching: counted.data, then, fail })
    })
  }

  const dialog =
    question === null ? null : (
      <ConfirmDialog
        title={matchingWords(question.matching)}
        confirmLabel="Run on them too"
        cancelLabel="Only what changes from now on"
        tone="primary"
        busy={busy === question.routine.id}
        onConfirm={() => {
          void apply(question.routine, question.change, 'run', question.then, question.fail)
        }}
        onCancel={() => {
          void apply(question.routine, question.change, 'fromNow', question.then, question.fail)
        }}
        onDismiss={() => {
          setQuestion(null)
        }}
      >
        {questionBody(question)}
      </ConfirmDialog>
    )

  return { request, busy, asking: question !== null, dialog }
}
