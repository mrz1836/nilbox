import type { ReactNode } from 'react'

import { intentOf, releaseSteps, type StepState } from '../../lib/release'
import { isLive } from '../../lib/scriptRuns'
import type { ScriptRun } from '../../platform'
import { CheckIcon, RingIcon } from '../icons'
import { Spinner } from '../Spinner'

// stepIcons mark each step's state beside its words: done, under way (a steady dot with
// reduced motion, as the spinner holds still), and still to come.
const stepIcons: Readonly<Record<StepState, ReactNode>> = {
  done: <CheckIcon className="size-4 shrink-0 text-success" />,
  working: <Spinner className="size-4" />,
  pending: <RingIcon className="size-4 shrink-0 text-muted" />,
}

// stepStates say each state in words, for assistive technology.
const stepStates: Readonly<Record<StepState, string>> = { done: 'Done:', working: 'Now:', pending: 'Next:' }

export interface ReleaseProgressProps {
  run: ScriptRun
}

// ReleaseProgress is how far a release has got, step by step, as the run's progress reads
// magex's output: switched to the branch, pulled, tagged, pushed, and then the app's own
// check that GitHub has the tag. Each step is announced as it changes, politely.
export function ReleaseProgress({ run }: ReleaseProgressProps) {
  const steps = releaseSteps(run.progress, intentOf(run), isLive(run.status))

  return (
    <ol aria-label="Steps" aria-live="polite" className="flex flex-col gap-2 text-sm">
      {steps.map((step) => (
        <li
          key={step.id}
          aria-current={step.state === 'working' ? 'step' : undefined}
          className="flex items-center gap-2.5"
        >
          {stepIcons[step.state]}
          <span className={step.state === 'pending' ? 'text-muted' : 'font-medium text-text'}>
            <span className="sr-only">{stepStates[step.state]} </span>
            {step.text}
            {step.state === 'working' && '…'}
          </span>
        </li>
      ))}
    </ol>
  )
}
