import { useState } from 'react'

import { runScript, type RunsIn, type ScriptEntry, type ScriptQuestion, type UserScript } from '../platform'
import { useMutation } from './useMutation'

// ScriptAsk is a run the user asked for: the script, where it runs ('' for the setting),
// whether a command that bumps a version runs anyway, and a magex target's arguments.
export interface ScriptAsk {
  entry: ScriptEntry
  runsIn: RunsIn | ''
  anyway: boolean
  args: string[]
}

// TrustStep is the question a repository command asks first, with the run it holds back
// and the values the form filled in.
export interface TrustStep {
  step: 'trust'
  ask: ScriptAsk
  inputs: Record<string, string>
  question: ScriptQuestion
}

// ScriptRunStep is where asking for a run stands: nothing open; the confirmation one of
// the user's own scripts set to ask before running asks; the form for a task's inputs; or
// the question.
export type ScriptRunStep =
  | { step: 'idle' }
  | { step: 'confirm'; ask: ScriptAsk; script: UserScript }
  | { step: 'inputs'; ask: ScriptAsk }
  | TrustStep

// ScriptRunState is a click on Run as it goes.
export interface ScriptRunState {
  step: ScriptRunStep
  // pending reports a run being asked for; failed names the script whose run didn't
  // start, and why, or is null.
  pending: boolean
  failed: { name: string; error: string } | null
  // begin starts a run the user asked for: the confirmation first, for one of their own
  // scripts set to ask before running, and the form, for a task with inputs.
  begin: (ask: ScriptAsk) => void
  // go runs the script the confirmation asked about.
  go: (ask: ScriptAsk) => void
  // fill runs the script the form asked for with the values it filled in.
  fill: (ask: ScriptAsk, inputs: Record<string, string>) => void
  // confirm runs the command the question showed, as it showed it, remembering it when
  // remember is set.
  confirm: (held: TrustStep, remember: boolean) => void
  // cancel closes the form or the question, running nothing.
  cancel: () => void
}

// changedAgain says why a run the user confirmed didn't start, when the backend gave no
// words: it changed again as it was starting.
const changedAgain = 'It changed again as it was starting. Run it again to look at it.'

// useScriptRun runs a script in the clone at dir as the user asks, one click at a time:
// the form for a task's inputs first, then the run; and for a command from the
// repository's files the user hasn't confirmed as it is now, the question the backend
// sends back, then the run again with the hash they confirmed. A click asks at most once:
// a confirmed run held back again says why rather than asking again. onStarted hears each
// run that started.
export function useScriptRun(dir: string, onStarted: (id: number) => void): ScriptRunState {
  const [step, setStep] = useState<ScriptRunStep>({ step: 'idle' })
  const [failed, setFailed] = useState<ScriptRunState['failed']>(null)
  const start = useMutation(runScript)

  // send asks for the run, with the hash the user confirmed, or '' when none was asked.
  const send = async (ask: ScriptAsk, inputs: Record<string, string>, confirmed: string, remember: boolean) => {
    setStep({ step: 'idle' })
    setFailed(null)

    const result = await start.run({
      dir,
      ref: ask.entry.ref,
      inputs,
      args: ask.args,
      runsIn: ask.runsIn,
      confirmed,
      remember,
      anyway: ask.anyway,
    })

    if (!result.ok) {
      setFailed({ name: ask.entry.name, error: result.error })
    } else if (result.data.started) {
      onStarted(result.data.id)
    } else if (confirmed === '') {
      setStep({ step: 'trust', ask, inputs, question: result.data.question })
    } else {
      setFailed({ name: ask.entry.name, error: result.data.message === '' ? changedAgain : result.data.message })
    }
  }

  // proceed asks for the run, or for a task's inputs first.
  const proceed = (ask: ScriptAsk) => {
    if (ask.entry.inputs.length > 0) {
      setStep({ step: 'inputs', ask })
    } else {
      void send(ask, {}, '', false)
    }
  }

  return {
    step,
    pending: start.pending,
    failed,
    begin: (ask) => {
      setFailed(null)

      const { mine } = ask.entry

      if (mine?.confirm === true) {
        setStep({ step: 'confirm', ask, script: mine })
      } else {
        proceed(ask)
      }
    },
    go: proceed,
    fill: (ask, inputs) => {
      void send(ask, inputs, '', false)
    },
    confirm: (held, remember) => {
      void send(held.ask, held.inputs, held.question.sha256, remember)
    },
    cancel: () => {
      setStep({ step: 'idle' })
    },
  }
}
