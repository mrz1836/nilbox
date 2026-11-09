import { useId } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setMergeMethod, type MergeMethodSetting } from '../../platform'
import { ErrorText, joinIds, Note, SectionIntro } from './rows'
import type { SectionProps } from './types'

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
// is chosen, and points at where the review replies are kept.
export function ReviewingSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)

  const choose = (mergeMethod: MergeMethodSetting) => {
    void save({ mergeMethod }, () => setMergeMethod(mergeMethod))
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
                    choose(choice.value)
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
      <SectionIntro title="Review replies">
        Reply templates for approvals, requests for changes, and comments live in{' '}
        <span className="text-text">Prompts → Review replies</span>.
      </SectionIntro>
    </div>
  )
}
