import { useId, useState, type Ref } from 'react'

import { appliesToLabels, pinSummary, usagePlaces, usageSummary } from '../lib/launch'
import type { AgentInfo, Diagnostic, Prompt, PromptAppliesTo, PromptInput, PromptVariable } from '../platform'
import { chipClass, describedBy, Field, fieldClass, Segmented, type SegmentOption } from './controls'
import { AlertIcon, ChevronRightIcon, CircleCheckIcon, StarIcon } from './icons'
import { LaunchOverrideFields } from './LaunchOverrideFields'
import { TemplateEditor, type TemplateEditorHandle } from './TemplateEditor'

// The limits the backend enforces, mirrored for counters.
export const maxNameLength = 80
export const maxNicknameLength = 20

const appliesToOptions: readonly SegmentOption<PromptAppliesTo>[] = [
  { value: 'pr', label: appliesToLabels.pr },
  { value: 'issue', label: appliesToLabels.issue },
  { value: 'any', label: appliesToLabels.any, title: 'Pull requests and issues' },
]

export interface PromptEditorProps {
  // agents are the coding agents, whose models and flags a prompt can set.
  agents: readonly AgentInfo[]
  draft: PromptInput
  onChange: (draft: PromptInput) => void
  // saved is the stored prompt being edited, or null for a new one.
  saved: Prompt | null
  // fieldErrors maps an input field to its problem.
  fieldErrors: Readonly<Record<string, string | undefined>>
  diagnostics: readonly Diagnostic[]
  variables: readonly PromptVariable[]
  editorRef: Ref<TemplateEditorHandle>
  // onReveal points the editor at a diagnostic.
  onReveal: (d: Diagnostic) => void
  // checked reports whether the draft has been checked at least once.
  checked: boolean
}

// counter shows how much of a limit a value uses, in red once it is over.
function Counter({ value, limit }: { value: string; limit: number }) {
  const n = Array.from(value).length

  return (
    <span aria-hidden="true" className={`text-[11px] tabular-nums ${n > limit ? 'text-danger' : 'text-muted'}`}>
      {n}/{limit}
    </span>
  )
}

// whereText places a diagnostic, as in "Line 3, column 14".
function whereText(d: Diagnostic): string {
  if (d.line === 0) {
    return 'Prompt'
  }

  return d.column > 0 ? `Line ${String(d.line)}, column ${String(d.column)}` : `Line ${String(d.line)}`
}

// PromptEditor is the form for one prompt: its large title, its nickname for the launch
// menu, what it is for, its text in the template editor with the problems found in it,
// and the launch settings it sets.
export function PromptEditor({
  agents,
  draft,
  onChange,
  saved,
  fieldErrors,
  diagnostics,
  variables,
  editorRef,
  onReveal,
  checked,
}: PromptEditorProps) {
  const id = useId()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const set = (patch: Partial<PromptInput>) => {
    onChange({ ...draft, ...patch })
  }

  const pins = pinSummary(
    {
      ...draft,
      extraArgs: Object.fromEntries(
        Object.entries(draft.extraFlags).map(([k, v]) => [k, v.trim() === '' ? [] : v.trim().split(/\s+/)]),
      ),
    },
    agents,
  )
  const settingsError = Object.keys(fieldErrors).some(
    (f) => ['models', 'effort', 'extraFlags'].includes(f.split('.')[0] ?? '') && (fieldErrors[f] ?? '') !== '',
  )
  const errors = diagnostics.filter((d) => d.severity === 'error')
  const warnings = diagnostics.filter((d) => d.severity !== 'error')

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 pt-5 pb-4">
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-name`} className="sr-only">
          Title
        </label>
        <input
          id={`${id}-name`}
          value={draft.name}
          placeholder="Name this prompt, such as “Review a pull request”"
          aria-invalid={(fieldErrors.name ?? '') !== ''}
          aria-describedby={describedBy(`${id}-name`, fieldErrors.name, false)}
          maxLength={maxNameLength + 20}
          onChange={(e) => {
            set({ name: e.target.value })
          }}
          className="-mx-2 rounded-md border border-transparent bg-transparent px-2 py-1 text-xl font-semibold tracking-tight text-text transition-colors outline-none placeholder:font-normal placeholder:text-muted/60 hover:border-border focus:border-accent/60 aria-invalid:border-danger"
        />
        {(fieldErrors.name ?? '') !== '' && (
          <p id={`${id}-name-error`} role="alert" className="text-xs text-danger">
            {fieldErrors.name}
          </p>
        )}
      </div>

      <div className="flex flex-wrap items-start gap-x-6 gap-y-3">
        <div className="w-48">
          <Field
            label="Nickname"
            controlId={`${id}-nickname`}
            hint="Shown on the launch button."
            error={fieldErrors.nickname}
            aside={<Counter value={draft.nickname} limit={maxNicknameLength} />}
          >
            <input
              id={`${id}-nickname`}
              value={draft.nickname}
              placeholder="Review"
              aria-invalid={(fieldErrors.nickname ?? '') !== ''}
              aria-describedby={describedBy(`${id}-nickname`, fieldErrors.nickname, true)}
              onChange={(e) => {
                set({ nickname: e.target.value })
              }}
              className={`${fieldClass} w-full`}
            />
          </Field>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted">For</span>
          <Segmented
            label="What the prompt is for"
            value={draft.appliesTo}
            options={appliesToOptions}
            onChange={(appliesTo) => {
              set({ appliesTo })
            }}
          />
          {(fieldErrors.appliesTo ?? '') !== '' && (
            <p role="alert" className="max-w-sm text-xs text-danger">
              {fieldErrors.appliesTo}
            </p>
          )}
        </div>
        {saved !== null && (
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted">Used as a default</span>
            <p
              title={usagePlaces(saved.usage).join('\n') || undefined}
              className="flex items-center gap-1.5 py-1 text-[13px] text-text"
            >
              {saved.usage.defaults.length > 0 && <StarIcon className="size-3.5 text-accent" />}
              {usageSummary(saved.usage)}
            </p>
          </div>
        )}
      </div>

      <div className="flex min-h-[18rem] flex-1 flex-col gap-2">
        <div className="flex items-baseline justify-between">
          <span className="text-xs font-medium text-muted">Prompt</span>
          <span className="text-[11px] text-muted">
            Type <code className="font-mono">{'{{'}</code> for variables
          </span>
        </div>
        <TemplateEditor
          id={`${id}-body`}
          ref={editorRef}
          label="Prompt text"
          value={draft.body}
          onChange={(body) => {
            set({ body })
          }}
          variables={variables}
          diagnostics={diagnostics}
          describedBy={`${id}-problems`}
          placeholder={'Review pull request #{{.Number}} in {{.Repo}}: {{.Title}}'}
        />
        <div id={`${id}-problems`} aria-live="polite" className="flex flex-col gap-1">
          {checked && errors.length === 0 && warnings.length === 0 && (
            <p className="flex items-center gap-1.5 text-xs text-success">
              <CircleCheckIcon className="size-3.5" />
              No problems
            </p>
          )}
          {[...errors, ...warnings].map((d) => (
            <button
              key={`${String(d.line)}:${String(d.column)}:${d.message}`}
              type="button"
              disabled={d.line === 0}
              onClick={() => {
                onReveal(d)
              }}
              className={`flex items-start gap-1.5 rounded-md px-1 py-0.5 text-left text-xs transition-colors enabled:hover:bg-raised ${d.severity === 'error' ? 'text-danger' : 'text-warning'}`}
            >
              <AlertIcon className="mt-px size-3.5 shrink-0" />
              <span>
                <span className="font-medium">{whereText(d)}:</span> {d.message}
              </span>
            </button>
          ))}
        </div>
      </div>

      <section aria-labelledby={`${id}-settings`} className="rounded-lg border border-border">
        <button
          id={`${id}-settings`}
          type="button"
          aria-expanded={settingsOpen}
          onClick={() => {
            setSettingsOpen((was) => !was)
          }}
          className="flex w-full items-center gap-2 rounded-lg px-3.5 py-2.5 text-left transition-colors hover:bg-raised/60"
        >
          <ChevronRightIcon className={`size-3.5 text-muted transition-transform ${settingsOpen ? 'rotate-90' : ''}`} />
          <span className="text-sm font-medium">Launch settings</span>
          <span className="flex min-w-0 flex-1 flex-wrap justify-end gap-1.5">
            {settingsError && <span className="text-xs text-danger">Has a problem</span>}
            {pins.length === 0 ? (
              <span className="text-xs text-muted">Inherits everything</span>
            ) : (
              pins.map((pin) => (
                <span key={pin} className={chipClass}>
                  {pin}
                </span>
              ))
            )}
          </span>
        </button>
        {settingsOpen && (
          <div className="grid grid-cols-2 gap-x-5 gap-y-4 border-t border-border px-3.5 pt-3 pb-4">
            <p className="col-span-2 text-xs leading-relaxed text-muted">
              Used whenever this prompt is chosen, over the org and repo defaults. The profile (the account) always
              comes from the defaults or the launch menu.
            </p>
            <LaunchOverrideFields
              id={id}
              agents={agents}
              values={draft}
              onChange={set}
              errors={fieldErrors}
              modelHint="An alias or a full name."
              flagsHint={
                <>
                  Added after the profile&apos;s and the defaults&apos; flags, such as{' '}
                  <code className="font-mono">--advisor fable</code>. Quote values with spaces.
                </>
              }
            />
          </div>
        )}
      </section>
    </div>
  )
}
