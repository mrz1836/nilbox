import { useId, useState, type ReactNode, type Ref } from 'react'

import { appliesToLabels, pinSummary, routinePlaces, routinesUsing, usagePlaces, usageSummary } from '../lib/launch'
import { replyActionLabels, replyGroups, replyUsageSummary } from '../lib/replies'
import type {
  AgentInfo,
  Diagnostic,
  Prompt,
  PromptAppliesTo,
  PromptInput,
  PromptReplyAction,
  PromptVariable,
} from '../platform'
import { chipClass, describedBy, ErrorText, Field, fieldClass, Segmented, type SegmentOption } from './controls'
import { AlertIcon, ChevronRightIcon, CircleCheckIcon, EyeIcon, PencilIcon, StarIcon } from './icons'
import { LaunchOverrideFields } from './LaunchOverrideFields'
import { TemplateEditor, type TemplateEditorHandle } from './TemplateEditor'

// The limits the backend enforces, mirrored for counters.
const maxNameLength = 80
const maxNicknameLength = 20

// replyAppliesToOptions are what a comment reply can be for: the text of a comment on an
// item, never a repository's.
const replyAppliesToOptions: readonly SegmentOption<PromptAppliesTo>[] = [
  { value: 'pr', label: appliesToLabels.pr },
  { value: 'issue', label: appliesToLabels.issue },
  { value: 'any', label: appliesToLabels.any, title: 'Pull requests and issues' },
]

// agentAppliesToOptions are what an agent prompt can be for: an item's launch, or a
// repository's, as Upgrade… on the Deps page starts one.
const agentAppliesToOptions: readonly SegmentOption<PromptAppliesTo>[] = [
  ...replyAppliesToOptions,
  { value: 'repo', label: appliesToLabels.repo, title: 'A repository’s own launch, such as Upgrade… on the Deps page' },
]

const actionOptions: readonly SegmentOption<PromptReplyAction>[] = replyGroups.map(({ action }) => ({
  value: action,
  label: replyActionLabels[action],
}))

// PromptView is what the prompt's frame shows: its text to edit, or the text filled in.
export type PromptView = 'edit' | 'preview'

const viewOptions: readonly SegmentOption<PromptView>[] = [
  { value: 'edit', label: 'Edit', icon: <PencilIcon className="size-3.5" />, title: 'Edit (⇧⌘P)' },
  { value: 'preview', label: 'Preview', icon: <EyeIcon className="size-3.5" />, title: 'Preview (⇧⌘P)' },
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
  // view is what the prompt's frame shows, and onViewChange switches it.
  view: PromptView
  onViewChange: (view: PromptView) => void
  // preview is the prompt filled in, shown in the text's place while previewing.
  preview: ReactNode
  // actions sit at the end of the title's row, such as the stored prompt's menu.
  actions?: ReactNode
}

// counter shows how much of a limit a value uses, in red once it is over.
function Counter({ value, limit }: { value: string; limit: number }) {
  const n = Array.from(value).length

  return (
    <span aria-hidden="true" className={`text-caption tabular-nums ${n > limit ? 'text-danger' : 'text-muted'}`}>
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

// PromptEditor is the form for one prompt: its large title, which stays in view with the
// actions beside it while the rest scrolls, its nickname for the launch menu, what it is
// for, its text in the template editor with the problems found in it, and the launch
// settings it sets. Edit | Preview swaps the text for the prompt filled in, in the same
// frame; the editor stays mounted meanwhile, so its undo history, caret, and scroll are
// where they were on the way back. An agent prompt can be for repositories rather than
// items, as the Deps page's upgrades are, whose launch always makes a new worktree. A
// review reply has the review it is for in place of what it applies to, never
// repositories, and no launch settings, since it never launches anything.
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
  view,
  onViewChange,
  preview,
  actions,
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
  const reply = draft.purpose === 'reply'

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-1 px-6 pt-4">
        <div className="flex items-center gap-3">
          <label htmlFor={`${id}-name`} className="sr-only">
            Title
          </label>
          <input
            id={`${id}-name`}
            value={draft.name}
            placeholder={
              reply
                ? 'Name this reply, such as “Approve with thanks”'
                : 'Name this prompt, such as “Review a pull request”'
            }
            aria-invalid={(fieldErrors.name ?? '') !== ''}
            aria-describedby={describedBy(`${id}-name`, fieldErrors.name, false)}
            maxLength={maxNameLength + 20}
            onChange={(e) => {
              set({ name: e.target.value })
            }}
            className="-ml-2 min-w-0 flex-1 rounded-md border border-transparent bg-transparent px-2 py-1 text-xl font-semibold tracking-tight text-text transition-colors outline-none placeholder:font-normal placeholder:text-muted/60 hover:border-border focus:border-accent/60 aria-invalid:border-danger"
          />
          {actions !== undefined && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
        <ErrorText id={`${id}-name-error`} error={fieldErrors.name ?? ''} />
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 pt-4 pb-4">
        <div className="flex flex-wrap items-start gap-x-6 gap-y-3">
          <div className="w-48">
            <Field
              label="Nickname"
              controlId={`${id}-nickname`}
              hint={reply ? 'Shown when you pick a reply.' : 'Shown on the launch button.'}
              error={fieldErrors.nickname}
              aside={<Counter value={draft.nickname} limit={maxNicknameLength} />}
            >
              <input
                id={`${id}-nickname`}
                value={draft.nickname}
                placeholder={reply ? 'LGTM' : 'Review'}
                aria-invalid={(fieldErrors.nickname ?? '') !== ''}
                aria-describedby={describedBy(`${id}-nickname`, fieldErrors.nickname, true)}
                onChange={(e) => {
                  set({ nickname: e.target.value })
                }}
                className={`${fieldClass} w-full`}
              />
            </Field>
          </div>
          {reply && (
            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium text-muted">Action</span>
              <Segmented
                label="Action"
                value={draft.replyAction}
                options={actionOptions}
                // Only a comment reply can be for issues: a review is of a pull request.
                onChange={(replyAction) => {
                  set(replyAction === 'comment' ? { replyAction } : { replyAction, appliesTo: 'pr' })
                }}
              />
              <ErrorText error={fieldErrors.replyAction ?? ''} className="max-w-sm" />
            </div>
          )}
          {/* A comment reply can be for issues too; an approval and a request for changes
            are reviews of a pull request. */}
          {(!reply || draft.replyAction === 'comment') && (
            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium text-muted">For</span>
              <Segmented
                label={reply ? 'What the reply is for' : 'What the prompt is for'}
                value={draft.appliesTo}
                options={reply ? replyAppliesToOptions : agentAppliesToOptions}
                onChange={(appliesTo) => {
                  set({ appliesTo })
                }}
              />
              <ErrorText error={fieldErrors.appliesTo ?? ''} className="max-w-sm" />
            </div>
          )}
          {saved !== null && (
            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium text-muted">Used as a default</span>
              {reply ? (
                <p className="flex items-center gap-1.5 py-1 text-body text-text">
                  {saved.usage.replies.length > 0 && <StarIcon className="size-3.5 text-accent" />}
                  {replyUsageSummary(saved.usage.replies)}
                </p>
              ) : (
                <p
                  title={usagePlaces(saved.usage).join('\n') || undefined}
                  className="flex items-center gap-1.5 py-1 text-body text-text"
                >
                  {(saved.usage.defaults.length > 0 || saved.usage.slots.length > 0) && (
                    <StarIcon className="size-3.5 text-accent" />
                  )}
                  {usageSummary(saved.usage)}
                </p>
              )}
              {saved.usage.routines.length > 0 && (
                <p title={routinePlaces(saved.usage).join('\n')} className="text-body text-text">
                  {routinesUsing(saved.usage)}
                </p>
              )}
            </div>
          )}
        </div>

        <div className="flex min-h-[18rem] flex-1 flex-col gap-2">
          <div className="flex items-center gap-3">
            <span className="text-xs font-medium text-muted">{reply ? 'Reply' : 'Prompt'}</span>
            <Segmented
              size="sm"
              label={reply ? 'Show the reply as' : 'Show the prompt as'}
              value={view}
              options={viewOptions}
              onChange={onViewChange}
            />
            <span className="ml-auto text-caption text-muted">
              {view === 'edit' ? (
                <>
                  Type <code className="font-mono">{'{{'}</code> for variables
                </>
              ) : reply ? (
                'As the review sheet fills it in'
              ) : (
                'As a launch sends it'
              )}
            </span>
          </div>
          <div className="flex min-h-0 flex-1 flex-col" hidden={view === 'preview'}>
            <TemplateEditor
              id={`${id}-body`}
              ref={editorRef}
              label={reply ? 'Reply text' : 'Prompt text'}
              value={draft.body}
              onChange={(body) => {
                set({ body })
              }}
              variables={variables}
              diagnostics={diagnostics}
              describedBy={`${id}-problems`}
              placeholder={
                reply ? 'Thanks, @{{.Author}}!' : 'Review pull request #{{.Number}} in {{.Repo}}: {{.Title}}'
              }
            />
          </div>
          {view === 'preview' && preview}
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

        {!reply && (
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
              <ChevronRightIcon
                className={`size-3.5 text-muted transition-transform ${settingsOpen ? 'rotate-90' : ''}`}
              />
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
                  {draft.appliesTo === 'repo' &&
                    ' A launch for a repository always starts in a new worktree of its own.'}
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
        )}
      </div>
    </div>
  )
}
