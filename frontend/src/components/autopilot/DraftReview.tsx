import { useId, useState } from 'react'

import { modeHelp, modeLabels, modeOrder } from '../../lib/autopilot'
import type { Result } from '../../lib/call'
import { canonical, testedKeys } from '../../lib/routineBuilder'
import { useMutation } from '../../lib/useMutation'
import { useDebouncedQuery, useQuery } from '../../lib/useQuery'
import {
  describeRoutine,
  draftReadiness,
  saveRoutine,
  testRoutine,
  type AutopilotDescribed,
  type AutopilotDraft,
  type AutopilotMode,
  type AutopilotRoutine,
  type AutopilotRoutineReady,
  type AutopilotTestResult,
  type ItemKind,
} from '../../platform'
import { captionClass, Segmented } from '../controls'
import { CheckIcon } from '../icons'
import { TestPanel } from './builder/TestPanel'

// The review of a routine not saved yet, as quick setup and a Describe it card's routine
// share it: Describe's reading of the draft as it is now, what this Mac has for it, a test
// on the user's items when asked, the mode it starts in, and its save. Each caller lays the
// parts out its own way around them.

// DraftTesting is the last test of a draft, and how it stands.
export interface DraftTesting {
  // result is the last test's, undefined before one answered; stale reports one of an
  // earlier draft, whose watch, trigger, conditions, or Once per changed since.
  result: AutopilotTestResult | undefined
  stale: boolean
  // testing reports a test under way, and error why the last one failed.
  testing: boolean
  error: string
  // run tests the draft as it is now.
  run: () => void
}

// DraftReview is a draft as its review holds it.
export interface DraftReview {
  // reading is Describe's latest reading of the draft, kept while a newer one is read, and
  // fresh reports it is of the draft as it is now.
  reading: AutopilotDescribed | undefined
  fresh: boolean
  // readiness is what this Mac has for it, undefined until it is read.
  readiness: AutopilotRoutineReady | undefined
  test: DraftTesting
  // saving reports a save under way; save stores the draft as it is now, with the user's
  // confirmation that its Merge it may go past the rules when accepted says so, resolving
  // with the routine saved or why it couldn't be.
  saving: boolean
  save: (accepted?: boolean) => Promise<Result<AutopilotRoutine>>
}

// Tested is a test's result, with the keys of the draft it tried (testedKeys).
type Tested = ReturnType<typeof testedKeys> & { result: AutopilotTestResult }

// useDraftReview reviews draft as it changes: Describe reads it once it has rested for
// describeDelay milliseconds, asking GitHub nothing, and again when also changes (what
// else the reading leans on, such as the prompts a card adds before its routine); this
// Mac's readiness is read again as its spec changes, never asking GitHub; Test reads its
// searches only when asked; and save stores it as it is.
export function useDraftReview(draft: AutopilotDraft, describeDelay = 300, also = ''): DraftReview {
  const key = `${also}\n${canonical(draft)}`
  const described = useDebouncedQuery(() => describeRoutine(draft).then((d) => ({ key, d })), [key], describeDelay)
  const ready = useQuery(() => draftReadiness(draft), [canonical(draft.spec)], {
    log: 'could not check what this Mac has for the routine',
  })
  const tester = useMutation((d: AutopilotDraft) => testRoutine(d))
  const saver = useMutation((d: AutopilotDraft) => saveRoutine(d))
  const [tested, setTested] = useState<Tested | null>(null)
  const keys = testedKeys(draft.id ?? 0, draft.spec)

  return {
    reading: described.data?.d,
    fresh: described.data?.key === key,
    readiness: ready.data,
    test: {
      result: tested?.result,
      stale: tested !== null && tested.key !== keys.key,
      testing: tester.pending,
      error: tester.error,
      run: () => {
        const asked = keys

        void tester.run(draft).then((result) => {
          if (result.ok) {
            setTested({ ...asked, result: result.data })
          }
        })
      },
    },
    saving: saver.pending,
    save: (accepted = false) => saver.run(accepted ? { ...draft, acceptPastRules: true } : draft),
  }
}

export interface ModeChoiceProps {
  mode: AutopilotMode
  // hint says what is suggested beside the mode, '' for nothing: then the line says what
  // the mode does, and that the routine starts off.
  hint: string
  onChange: (mode: AutopilotMode) => void
}

// ModeChoice is the mode a new routine starts in, with what it does or what is suggested
// instead beside it.
export function ModeChoice({ mode, hint, onChange }: ModeChoiceProps) {
  const id = useId()

  return (
    <div className="flex flex-wrap items-center gap-3">
      <Segmented
        label="Mode"
        value={mode}
        options={modeOrder.map((m) => ({ value: m, label: modeLabels[m], title: modeHelp[m] }))}
        describedBy={`${id}-mode`}
        onChange={onChange}
      />
      <p id={`${id}-mode`} className="min-w-0 flex-1 text-xs text-muted">
        {hint === '' ? `${modeHelp[mode]} It starts off: switch it on once it’s saved.` : hint}
      </p>
    </div>
  )
}

// ThisMac is what this Mac has for a draft: ready to run here, or what it lacks, each with
// its fix, as a command to run or in words.
export function ThisMac({ readiness }: { readiness: AutopilotRoutineReady | undefined }) {
  const id = useId()

  return (
    <section aria-labelledby={`${id}-ready`} className="flex flex-col gap-1">
      <h3 id={`${id}-ready`} className={captionClass}>
        This Mac
      </h3>
      {readiness === undefined ? (
        <p className="text-xs text-muted">Checking what this Mac has…</p>
      ) : readiness.problems.length === 0 ? (
        <p className="flex items-center gap-1.5 text-xs text-muted">
          <CheckIcon className="size-3.5 text-success" />
          Ready to run here.
        </p>
      ) : (
        <ul aria-label="What this Mac lacks for it" className="flex flex-col gap-0.5">
          {readiness.problems.map((c) => (
            <li key={c.id} className="text-xs text-warning">
              {`${c.label}: ${c.found}`}
              {c.fix !== '' && <span className="text-muted">{c.command ? ` · run ${c.fix}` : ` · ${c.fix}`}</span>}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export interface DraftTestPanelProps {
  test: DraftTesting
  // ready reports a draft that can be tried now: it reads with no problem.
  ready: boolean
  // kinds are the kinds of item it acts on, which the headline names.
  kinds: readonly ItemKind[]
  mode: AutopilotMode
  draft: AutopilotDraft
}

// DraftTestPanel is the builder's Test panel over a draft's review.
export function DraftTestPanel({ test, ready, kinds, mode, draft }: DraftTestPanelProps) {
  return (
    <TestPanel
      result={test.result}
      stale={test.stale}
      testing={test.testing}
      error={test.error}
      ready={ready}
      kinds={kinds}
      mode={mode}
      draft={draft}
      onTest={test.run}
    />
  )
}
