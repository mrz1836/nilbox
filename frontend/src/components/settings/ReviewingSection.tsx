import { startTransition, useActionState, useId, useState } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setIgnoredWorkflows, setMergeMethod, type MergeMethodSetting } from '../../platform'
import { ErrorText, joinIds, Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'

// Refusal is why a change of the ignored workflows was refused, and the draft typed when it
// was: typing again clears it.
interface Refusal {
  draft: string
  message: string
}

const noRefusal: Refusal = { draft: '', message: '' }

// mergeChoices are the ways the review sheet can merge, as GitHub's merge button words
// them, each with what it does.
const mergeChoices: readonly { value: MergeMethodSetting; label: string; description: string }[] = [
  {
    value: 'repo',
    label: 'As each repository offers first',
    description: 'The method GitHub’s merge button offers you first in that repository.',
  },
  {
    value: 'squash',
    label: 'Squash and merge',
    description: 'Combines the pull request’s commits into one commit on the base branch.',
  },
  {
    value: 'merge_commit',
    label: 'Create a merge commit',
    description: 'Adds every commit to the base branch, joined by a merge commit.',
  },
  {
    value: 'rebase_merge',
    label: 'Rebase and merge',
    description: 'Adds each commit to the base branch on its own, with no merge commit.',
  },
]

// ReviewingSection holds how the review sheet merges a pull request, saved as soon as it
// is chosen, the workflows whose held runs never count (Ignored workflows), each change
// stored as it is made, and points at where the review replies are kept.
export function ReviewingSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  // error is why the last choice of a merge method was refused; a refused ignored workflow
  // shows its own.
  const [error, setError] = useState('')
  const [draft, setDraft] = useState('')
  // invalid is why the draft cannot be added, found before asking the backend.
  const [invalid, setInvalid] = useState<Refusal>(noRefusal)
  // Every change of the ignored workflows runs through one action: one at a time, with the
  // list's controls held while it runs.
  const [refused, change, busy] = useActionState(
    async (_: Refusal, next: { names: string[]; draft: string; then?: () => void }): Promise<Refusal> => {
      const result = await save({ ignoredWorkflows: next.names }, () => setIgnoredWorkflows(next.names))

      if (!result.ok) {
        return { draft: next.draft, message: result.error }
      }

      next.then?.()

      return noRefusal
    },
    noRefusal,
  )

  const names = settings.ignoredWorkflows
  const refusal = invalid.message !== '' ? invalid : busy ? noRefusal : refused
  const ignoreError = refusal.draft === draft ? refusal.message : ''

  const choose = async (mergeMethod: MergeMethodSetting) => {
    setError('')

    const result = await save({ mergeMethod }, () => setMergeMethod(mergeMethod))

    setError(result.ok ? '' : result.error)
  }

  // store replaces the ignored workflows.
  const store = (next: string[], then?: () => void) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ names: next, draft, ...(then === undefined ? {} : { then }) })
    })
  }

  const add = () => {
    // The button is disabled while the field is blank, so the name is never empty.
    const name = draft.trim()

    if (names.includes(name)) {
      setInvalid({ draft, message: `${name} is already ignored.` })

      return
    }

    store([...names, name], () => {
      setDraft('')
    })
  }

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-3">
        <SectionIntro id={`${id}-merge`} title="Merge method">
          How the review sheet merges a pull request, whether or not it approves it first.
        </SectionIntro>
        <div
          role="radiogroup"
          aria-labelledby={`${id}-merge`}
          aria-describedby={`${id}-note`}
          className="divide-y divide-border rounded-lg border border-border bg-surface"
        >
          {mergeChoices.map((choice) => {
            const controlId = `${id}-${choice.value}`

            return (
              <div key={choice.value} className="flex items-start gap-3 px-4 py-3.5">
                <input
                  id={controlId}
                  type="radio"
                  name={`${id}-method`}
                  value={choice.value}
                  checked={settings.mergeMethod === choice.value}
                  aria-describedby={joinIds(`${controlId}-help`, error !== '' && `${id}-error`)}
                  onChange={() => {
                    void choose(choice.value)
                  }}
                  className="radio mt-0.5"
                />
                <div className="min-w-0">
                  <label htmlFor={controlId} className="cursor-pointer text-sm font-medium">
                    {choice.label}
                  </label>
                  <p id={`${controlId}-help`} className="mt-0.5 text-xs text-muted">
                    {choice.description}
                  </p>
                </div>
              </div>
            )
          })}
        </div>
        <ErrorText id={`${id}-error`} error={error} />
        <Note id={`${id}-note`}>
          A repository that doesn’t allow the method you choose merges the way it offers first.
        </Note>
      </div>
      <section aria-labelledby={`${id}-ignored`} className="flex flex-col gap-3">
        <SectionIntro id={`${id}-ignored`} title="Ignored workflows">
          Workflows whose runs, held for approval, never count toward where a pull request stands, even when GitHub
          reports the merge blocked: its card reads its checks as they are, and the review sheet names the runs quietly
          under its checks.
        </SectionIntro>
        <StringListEditor
          label="Ignored workflows"
          items={names}
          empty="No workflow is ignored. A run GitHub approves only on its site doesn’t count unless GitHub reports the merge blocked; ignore a workflow from a held run in the review sheet, or here."
          removeLabel={(name) => `Count ${name} again`}
          onRemove={(name) => {
            store(names.filter((n) => n !== name))
          }}
          inputLabel="Workflow to ignore"
          placeholder="A workflow’s name, as GitHub shows it"
          submitLabel="Ignore workflow"
          draft={draft}
          onDraftChange={setDraft}
          onAdd={add}
          error={ignoreError}
          describedBy={`${id}-names`}
          busy={busy}
        />
        <Note id={`${id}-names`}>A name matches a workflow’s whole name, case and all.</Note>
      </section>
      <SectionIntro title="Review replies">
        Reply templates for approvals, requests for changes, and comments live in{' '}
        <span className="text-text">Prompts → Review replies</span>.
      </SectionIntro>
    </div>
  )
}
