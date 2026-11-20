import { useId, useState } from 'react'

import { attempt } from '../../lib/call'
import { actsOn, placeLabel, placeValue } from '../../lib/routineBuilder'
import {
  blankProblems,
  blankValues,
  freeName,
  missingStarters,
  modeHint,
  quickProblems,
  templateDraft,
} from '../../lib/templates'
import {
  addStarterPrompt,
  type AutopilotBlank,
  type AutopilotCatalog,
  type AutopilotDraft,
  type AutopilotMode,
  type AutopilotProblem,
  type AutopilotRoutine,
  type AutopilotSuggestions,
  type AutopilotTemplate,
} from '../../platform'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { AlertIcon } from '../icons'
import { TokenField } from './builder/TokenField'
import { DraftTestPanel, ModeChoice, ThisMac, useDraftReview } from './DraftReview'

export interface QuickSetupProps {
  template: AutopilotTemplate
  // routines are the user's routines, whose names the new one's must differ from.
  routines: readonly Pick<AutopilotRoutine, 'name'>[]
  // defaultMode is the mode new routines start in, as Settings says.
  defaultMode: AutopilotMode
  catalog: AutopilotCatalog
  suggestions: AutopilotSuggestions
  viewer: string
  // onSaved hears the routine made; onCustomize opens the full builder with the routine as
  // filled in; onCancel goes back to the gallery; onPromptsChanged reads the gallery again
  // once a starter prompt it lacked is added.
  onSaved: (routine: AutopilotRoutine) => void
  onCustomize: (draft: AutopilotDraft) => void
  onCancel: () => void
  onPromptsChanged: () => void
  describeDelay?: number
}

interface BlankFieldProps {
  blank: AutopilotBlank
  index: number
  values: readonly string[]
  problems: readonly AutopilotProblem[]
  suggestions: AutopilotSuggestions
  viewer: string
  onChange: (values: string[]) => void
}

// BlankField is one of the template's blanks: a where, as the builder's Watch takes it, or
// a list of check names, picked from those seen on the user's pull requests.
function BlankField({ blank, index, values, problems, suggestions, viewer, onChange }: BlankFieldProps) {
  const id = useId()
  const where = blank.kind === 'where'
  const options = where
    ? [
        { value: '@me', label: 'Your account' },
        ...suggestions.owners
          .filter((o) => o.toLowerCase() !== viewer.toLowerCase())
          .map((o) => ({ value: `org:${o}`, label: o })),
      ]
    : suggestions.checks.map((c) => ({ value: c, label: c }))

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={`${id}-${String(index)}`} className="text-xs font-medium text-muted">
        {blank.label}
      </label>
      <TokenField
        id={`${id}-${String(index)}`}
        label={blank.label}
        placeholder={where ? 'Your account, an org, or owner/repo' : 'A check’s name, as GitHub shows it'}
        tokens={values}
        onChange={onChange}
        tokenLabel={where ? placeLabel : (v) => v}
        suggestions={options}
        toValue={where ? (t) => placeValue(t, viewer) : (t) => t.trim()}
        invalid={problems.length > 0}
        describedBy={`${id}-help`}
      />
      <p id={`${id}-help`} className="text-xs text-muted">
        {problems.length > 0 ? problems.map((p) => p.message).join(' ') : blank.help}
      </p>
    </div>
  )
}

// QuickSetup sets a template up in a few clicks: only its blanks (for most, where to look),
// the mode it starts in (the one new routines start in, the template's suggestion shown as
// a hint), the sentence it reads as, what this build or this Mac lacks for it, and a live
// Test on the user's items. Save makes the routine, off, as any new routine is; Customize
// opens the full builder with everything filled in.
export function QuickSetup({
  template: t,
  routines,
  defaultMode,
  catalog,
  suggestions,
  viewer,
  onSaved,
  onCustomize,
  onCancel,
  onPromptsChanged,
  describeDelay = 300,
}: QuickSetupProps) {
  const id = useId()
  const [values, setValues] = useState<string[][]>(() => blankValues(t))
  const [mode, setMode] = useState<AutopilotMode>(defaultMode)
  const [name] = useState(() => freeName(routines, t.name))
  const [saveError, setSaveError] = useState('')
  const [adding, setAdding] = useState('')
  const draft = templateDraft(t, values, name, mode)
  const review = useDraftReview(draft, describeDelay)
  const current = review.reading
  const problems = quickProblems(t, current?.problems ?? [])
  const missing = missingStarters(t)
  const runs = t.needs.length === 0 && missing.length === 0
  const canTest = runs && current !== undefined && problems.length === 0 && review.fresh
  const canSave = canTest && !review.saving
  const sentence = current?.sentence !== undefined && current.sentence !== '' ? current.sentence : t.sentence
  const hint = modeHint(t, mode)

  const save = async () => {
    setSaveError('')

    const result = await review.save()

    if (result.ok) {
      onSaved(result.data)
    } else {
      setSaveError(result.error)
    }
  }

  const addBack = async (starter: string) => {
    setAdding(starter)

    const result = await attempt(addStarterPrompt(starter))

    setAdding('')

    if (result.ok) {
      onPromptsChanged()
    } else {
      setSaveError(result.error)
    }
  }

  return (
    <section aria-labelledby={`${id}-title`} className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 id={`${id}-title`} className="flex items-center gap-2 text-lg font-semibold">
          <span aria-hidden="true">{t.icon}</span>
          {t.name}
        </h2>
        <p className="text-sm text-muted">{t.summary}</p>
      </header>
      <p aria-live="polite" className="text-sm leading-relaxed text-text">
        {sentence === '' ? 'Fill in what it asks, and the sentence it reads as shows here.' : sentence}
      </p>
      {t.needsWords !== '' && (
        <p
          role="alert"
          className="flex items-start gap-2 rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-sm"
        >
          <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
          <span>{t.needsWords}</span>
        </p>
      )}
      {missing.map((p) => (
        <div
          key={p.starter}
          className="flex flex-wrap items-center gap-3 rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-sm"
        >
          <span className="min-w-0 flex-1">{`It runs “${p.name}”, which isn’t in your prompts any more.`}</span>
          <button
            type="button"
            disabled={adding !== ''}
            onClick={() => {
              void addBack(p.starter)
            }}
            className={buttonClass}
          >
            {adding === p.starter ? 'Adding…' : 'Add it again'}
          </button>
        </div>
      ))}
      {t.blanks.length > 0 && (
        <div className="flex flex-col gap-3">
          {t.blanks.map((b, i) => (
            <BlankField
              key={`${b.kind}:${String(b.source)}:${String(b.condition)}`}
              blank={b}
              index={i}
              values={values[i] ?? []}
              problems={blankProblems(b, current?.problems ?? [])}
              suggestions={suggestions}
              viewer={viewer}
              onChange={(next) => {
                setValues(values.map((v, j) => (j === i ? next : v)))
              }}
            />
          ))}
        </div>
      )}
      <ModeChoice mode={mode} hint={hint} onChange={setMode} />
      {problems.filter((p) => !t.blanks.some((b) => blankProblems(b, [p]).length > 0)).length > 0 && (
        <ul aria-label="What keeps it from being saved" className="flex flex-col gap-1">
          {problems
            .filter((p) => !t.blanks.some((b) => blankProblems(b, [p]).length > 0))
            .map((p) => (
              <li key={`${p.part}:${p.field}:${p.message}`} className="text-xs text-danger">
                {p.message}
              </li>
            ))}
        </ul>
      )}
      <ThisMac readiness={review.readiness} />
      <DraftTestPanel
        test={review.test}
        ready={canTest}
        kinds={actsOn(catalog, draft.spec)}
        mode={mode}
        draft={draft}
      />
      <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-border pt-3">
        <p
          role={saveError === '' ? 'status' : 'alert'}
          className={`mr-auto min-w-0 truncate text-xs ${saveError === '' ? 'text-muted' : 'text-danger'}`}
          title={saveError}
        >
          {saveError !== '' ? saveError : review.saving ? 'Saving…' : `It’s saved as “${name}”, switched off.`}
        </p>
        <button type="button" onClick={onCancel} className={quietButtonClass}>
          Back to templates
        </button>
        <button
          type="button"
          onClick={() => {
            onCustomize(draft)
          }}
          className={buttonClass}
        >
          Customize
        </button>
        <button
          type="button"
          disabled={!canSave}
          onClick={() => {
            void save()
          }}
          className={primaryButtonClass}
        >
          Save
        </button>
      </footer>
    </section>
  )
}
