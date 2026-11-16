import { useState, type ReactNode } from 'react'

import { matchingWords, modeLabel } from '../../lib/autopilot'
import { attempt } from '../../lib/call'
import { counted } from '../../lib/format'
import {
  routineMatching,
  setRoutineEnabled,
  setRoutineMode,
  type AutopilotCurrent,
  type AutopilotDraft,
  type AutopilotMatching,
  type AutopilotMode,
  type AutopilotRoutine,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

// RoutineChange is a change the user asked for from a routine's switch, its menu, or its
// mode: switching it on or off, or another mode.
export type RoutineChange = { kind: 'enable'; on: boolean } | { kind: 'mode'; mode: AutopilotMode }

// Question is the question asked before a change that can act on what the routine matches
// now: how many items it matches, and the change it waits to make.
interface Question {
  routine: AutopilotRoutine
  change: RoutineChange
  matching: AutopilotMatching
}

// draftOf is a routine as the question counts its matches: as it is saved.
function draftOf(r: AutopilotRoutine): AutopilotDraft {
  return { id: r.id, name: r.name, description: r.description, mode: r.mode, spec: r.spec, templateKey: r.templateKey }
}

// asks reports a change the question is asked about: switching a routine on, or taking one
// that is on out of Dry run.
function asks(r: AutopilotRoutine, change: RoutineChange): boolean {
  return change.kind === 'enable' ? change.on : r.enabled && r.mode === 'dryRun' && change.mode !== 'dryRun'
}

// questionBody says what each answer does, for the change asked about.
function questionBody({ routine, change, matching }: Question): string {
  const dry = change.kind === 'enable' && routine.mode === 'dryRun'
  const once =
    change.kind === 'enable' ? `Once “${routine.name}” is on` : `Once “${routine.name}” is in ${modeLabel(change.mode)}`
  const unknown =
    matching.unknown > 0
      ? ` ${counted(matching.unknown, 'more item', 'more items')} can’t be told yet, until what it checks is read.`
      : ''

  return dry
    ? `${once}, it records what it would do, in Dry run: for them too, or only for what changes from now on.${unknown}`
    : `${once}, it can act on them too, or only on what changes from now on.${unknown}`
}

export interface RoutineChanges {
  // request makes a change, asking first when it can act on what the routine matches now.
  request: (routine: AutopilotRoutine, change: RoutineChange) => void
  // busy is the routine a change is under way for, 0 for none.
  busy: number
  // dialog is the question, while it is asked.
  dialog: ReactNode
}

// useRoutineChange switches routines on and off and changes their modes. Switching one on,
// or taking one that is on out of Dry run, first counts what it matches now (Matching,
// which reads its searches unless a test just did); when that is any, it asks "3 pull
// requests match right now": Run on them too, or Only what changes from now on, and the
// change and the answer go together. Escape asks nothing more and changes nothing. onDone
// hears the routine as changed, and onError why a change couldn't be made, in the
// backend's words (as "Autopilot is running in another copy...").
export function useRoutineChange(
  onDone: (routine: AutopilotRoutine) => void,
  onError: (text: string) => void,
): RoutineChanges {
  const [question, setQuestion] = useState<Question | null>(null)
  const [busy, setBusy] = useState(0)

  const apply = async (routine: AutopilotRoutine, change: RoutineChange, current: AutopilotCurrent) => {
    setBusy(routine.id)

    const result = await attempt(
      change.kind === 'enable'
        ? setRoutineEnabled(routine.id, change.on, current)
        : setRoutineMode(routine.id, change.mode, current),
    )

    setBusy(0)
    setQuestion(null)

    if (result.ok) {
      onDone(result.data)
    } else {
      onError(result.error)
    }
  }

  const request = (routine: AutopilotRoutine, change: RoutineChange) => {
    if (!asks(routine, change)) {
      void apply(routine, change, 'run')

      return
    }

    setBusy(routine.id)
    void attempt(routineMatching(draftOf(routine))).then((counted) => {
      if (!counted.ok) {
        setBusy(0)
        onError(counted.error)

        return
      }

      if (counted.data.count === 0) {
        void apply(routine, change, 'run')

        return
      }

      setBusy(0)
      setQuestion({ routine, change, matching: counted.data })
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
          void apply(question.routine, question.change, 'run')
        }}
        onCancel={() => {
          void apply(question.routine, question.change, 'fromNow')
        }}
        onDismiss={() => {
          setQuestion(null)
        }}
      >
        {questionBody(question)}
      </ConfirmDialog>
    )

  return { request, busy, dialog }
}
