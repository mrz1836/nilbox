import { useEffect, useEffectEvent, useId, useState } from 'react'

import { modeHelp, modeLabels, modeOrder } from '../../../lib/autopilot'
import { counted } from '../../../lib/format'
import { isSaveKey } from '../../../lib/keys'
import { actsOn, canonical, costLine, dryRunWhy, sameDraft, testedKeys, unshown } from '../../../lib/routineBuilder'
import { anyModalOpen } from '../../../lib/useModalDialog'
import { useMutation } from '../../../lib/useMutation'
import { useDebouncedQuery } from '../../../lib/useQuery'
import {
  describeRoutine,
  testRoutine,
  type AutopilotCatalog,
  type AutopilotDraft,
  type AutopilotMode,
  type AutopilotStepKind,
  type AutopilotSuggestions,
  type AutopilotTestResult,
} from '../../../platform'
import { buttonClass, fieldClass, primaryButtonClass, Segmented } from '../../controls'
import type { AgentKit } from './AgentStepBlock'
import { IfBlock } from './IfBlock'
import { LimitsBlock } from './LimitsBlock'
import { Notes, type BuilderKit } from './parts'
import { TestPanel } from './TestPanel'
import { ThenBlock } from './ThenBlock'
import { WatchBlock } from './WatchBlock'
import { WhenBlock } from './WhenBlock'

export interface RoutineBuilderProps {
  // draft is the routine as edited, and baseline as it was opened or last saved.
  draft: AutopilotDraft
  baseline: AutopilotDraft
  // isNew reports a routine not saved yet, whose mode is the draft's to choose.
  isNew: boolean
  // sentence is the routine's sentence as saved, shown until the draft's is read.
  sentence: string
  // mode is the routine's mode as saved, or the draft's for a new one; heldKinds are the
  // kinds of step the saved routine has that keep it in Dry run.
  mode: AutopilotMode
  heldKinds: readonly AutopilotStepKind[]
  catalog: AutopilotCatalog
  suggestions: AutopilotSuggestions
  viewer: string
  agentKit: AgentKit
  saving: boolean
  // saveError is why the last save failed, or ''.
  saveError: string
  onChange: (draft: AutopilotDraft) => void
  onSave: () => void
  onDiscard: () => void
  // describeDelay is how long the draft rests, in milliseconds, before Describe, which asks
  // GitHub nothing, reads it again.
  describeDelay?: number
}

// RoutineBuilder is the routine itself, laid out top to bottom in the order it runs: its
// name and, for a new one, its mode; the sentence it reads as, which rewrites itself as it
// is edited; WATCH, WHEN, IF, THEN, and LIMITS, each edited in place, with what is wrong
// where it is; and Test below, which tries the draft on its items now, only when asked,
// since its searches cost points. Save (⌘S) stores it once nothing is wrong; warnings never
// keep it from being saved.
export function RoutineBuilder({
  draft,
  baseline,
  isNew,
  sentence,
  mode,
  heldKinds,
  catalog,
  suggestions,
  viewer,
  agentKit,
  saving,
  saveError,
  onChange,
  onSave,
  onDiscard,
  describeDelay = 300,
}: RoutineBuilderProps) {
  const id = useId()
  const spec = draft.spec
  const key = canonical({ spec, mode: draft.mode ?? mode })
  const described = useDebouncedQuery(
    () => describeRoutine({ ...draft, mode: draft.mode ?? mode }).then((d) => ({ key, d })),
    [key],
    describeDelay,
  )
  // The last reading stays until the next arrives, so an edit hides no problem and holds
  // no Save while the draft rests; Save checks the draft again whatever it showed.
  const current = described.data?.d
  const problems = current?.problems ?? []
  const warnings = current?.warnings ?? []
  // The last sentence that read stays while the draft has problems.
  const [lastSentence, setLastSentence] = useState(sentence)

  if (current !== undefined && current.sentence !== '' && current.sentence !== lastSentence) {
    setLastSentence(current.sentence)
  }

  const keys = testedKeys(draft.id ?? 0, spec)
  const ready = current !== undefined && problems.length === 0
  // A test runs only on asking, since its searches cost points; what it found stays, said to
  // be of an earlier draft once what it watches, fires on, checks, or fires once per changed
  // since, its warnings shown only while the steps are as tested, since they are by step.
  const tester = useMutation((d: AutopilotDraft) => testRoutine(d))
  const [tested, setTested] = useState<(ReturnType<typeof testedKeys> & { result: AutopilotTestResult }) | null>(null)
  const test = async () => {
    const asked = keys
    const result = await tester.run(draft)

    if (result.ok) {
      setTested({ ...asked, result: result.data })
    }
  }
  const lastTest = tested?.result
  const testCurrent = tested !== null && tested.key === keys.key
  const testWarnings = tested !== null && testCurrent && tested.steps === keys.steps ? tested.result.warnings : []
  const searchesTested = tested !== null && tested.watch === keys.watch
  const searchNotes = tested !== null && searchesTested ? tested.result.searches : []
  const kinds = actsOn(catalog, spec)
  const kit: BuilderKit = {
    catalog,
    suggestions,
    viewer,
    problems,
    warnings: [...warnings, ...testWarnings],
  }
  const dirty = !sameDraft(draft, baseline)
  const named = draft.name.trim() !== ''
  const canSave = (dirty || isNew) && named && ready && !saving
  const held = current?.dryRunOnly ?? heldKinds
  const describeError =
    !described.pending && described.error !== '' ? `Couldn’t read the routine: ${described.error}` : ''

  const set = (change: Partial<AutopilotDraft['spec']>) => {
    onChange({ ...draft, spec: { ...spec, ...change } })
  }

  // ⌘S saves while the tab shows: a hidden tab runs no effects, so it stops listening. A
  // dialog or a sheet open over the page, the question a save asks among them, keeps it.
  const onKey = useEffectEvent((e: globalThis.KeyboardEvent) => {
    if (isSaveKey(e)) {
      e.preventDefault()

      if (canSave && !anyModalOpen()) {
        onSave()
      }
    }
  })

  useEffect(() => {
    const listen = (e: globalThis.KeyboardEvent) => {
      onKey(e)
    }

    document.addEventListener('keydown', listen)

    return () => {
      document.removeEventListener('keydown', listen)
    }
  }, [])

  const status = (() => {
    if (saveError !== '') {
      return { tone: 'error' as const, text: saveError }
    }

    if (describeError !== '') {
      return { tone: 'error' as const, text: describeError }
    }

    if (saving) {
      return { tone: 'muted' as const, text: 'Saving…' }
    }

    if (dirty && !named) {
      return { tone: 'muted' as const, text: 'Give the routine a name to save it.' }
    }

    if (dirty && problems.length > 0) {
      return { tone: 'muted' as const, text: `Fix ${counted(problems.length, 'problem', 'problems')} to save.` }
    }

    return { tone: 'muted' as const, text: dirty ? 'Unsaved changes' : '' }
  })()

  const shownMode = held.length > 0 ? 'dryRun' : (draft.mode ?? mode)

  return (
    <div className="flex flex-col">
      <div className="flex flex-col gap-3 pb-3">
        <div className="flex flex-col gap-1">
          <label htmlFor={`${id}-name`} className="text-xs font-medium text-muted">
            Name
          </label>
          <input
            id={`${id}-name`}
            value={draft.name}
            maxLength={80}
            aria-invalid={dirty && !named}
            aria-describedby={dirty && !named ? `${id}-status` : undefined}
            onChange={(e) => {
              onChange({ ...draft, name: e.target.value })
            }}
            className={`${fieldClass} max-w-md`}
          />
        </div>
        {isNew && (
          <div className="flex flex-wrap items-center gap-3">
            <Segmented
              label="Mode"
              value={shownMode}
              disabled={held.length > 0}
              options={modeOrder.map((m) => ({ value: m, label: modeLabels[m], title: modeHelp[m] }))}
              describedBy={`${id}-mode`}
              onChange={(m) => {
                onChange({ ...draft, mode: m })
              }}
            />
            <p id={`${id}-mode`} className="text-xs text-muted">
              {held.length > 0
                ? dryRunWhy(catalog, held)
                : `${modeHelp[shownMode]} It starts off: switch it on once it’s saved.`}
            </p>
          </div>
        )}
        {!isNew && held.length > 0 && heldKinds.length === 0 && (
          <p className="text-xs text-muted">{`Once saved, it runs in Dry run. ${dryRunWhy(catalog, held)}`}</p>
        )}
        <p
          aria-live="polite"
          className={`text-sm leading-relaxed ${current !== undefined && current.sentence === '' ? 'text-muted' : 'text-text'}`}
        >
          {lastSentence === '' ? 'The sentence it reads as shows here once it has every part it needs.' : lastSentence}
        </p>
        <Notes problems={unshown(problems, spec)} warnings={unshown(warnings, spec)} />
      </div>
      <WatchBlock
        kit={kit}
        sources={spec.watch.sources}
        onChange={(sources) => {
          set({ watch: { sources } })
        }}
        cost={lastTest === undefined ? '' : costLine(lastTest.estimate, !searchesTested)}
        tested={searchNotes}
      />
      <WhenBlock
        kit={kit}
        spec={spec}
        onChange={(trigger) => {
          set({ trigger })
        }}
      />
      <IfBlock
        kit={kit}
        kinds={kinds}
        conditions={spec.conditions}
        onChange={(conditions) => {
          set({ conditions })
        }}
      />
      <ThenBlock
        kit={kit}
        agentKit={agentKit}
        spec={spec}
        kinds={kinds}
        onChange={(steps) => {
          set({ steps })
        }}
      />
      <LimitsBlock
        kit={kit}
        limits={spec.limits}
        oncePer={catalog.triggers.find((t) => t.event === spec.trigger.event)?.oncePer ?? 'occurrence'}
        onChange={(limits) => {
          set({ limits })
        }}
      />
      <TestPanel
        result={lastTest}
        stale={tested !== null && !testCurrent}
        testing={tester.pending}
        error={tester.error}
        ready={ready}
        kinds={kinds}
        mode={shownMode}
        draft={draft}
        onTest={() => {
          void test()
        }}
      />
      <footer className="sticky bottom-0 flex items-center justify-end gap-2 border-t border-border bg-background py-3">
        <p
          id={`${id}-status`}
          role={status.tone === 'error' ? 'alert' : 'status'}
          className={`mr-auto min-w-0 truncate text-xs ${status.tone === 'error' ? 'text-danger' : 'text-muted'}`}
          title={status.text}
        >
          {status.text}
        </p>
        {(dirty || isNew) && (
          <button type="button" disabled={saving} onClick={onDiscard} className={buttonClass}>
            {isNew ? 'Cancel' : 'Discard'}
          </button>
        )}
        <button type="button" disabled={!canSave} onClick={onSave} title="Save (⌘S)" className={primaryButtonClass}>
          {isNew ? 'Create routine' : 'Save'}
        </button>
      </footer>
    </div>
  )
}
