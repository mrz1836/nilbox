import { startTransition, useActionState, useId, useState } from 'react'

import { snoozeLength } from '../../lib/triage'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  setIgnoredWorkflows,
  setMergeMethod,
  setNudgeAfterHours,
  setNudgeSnooze,
  type MergeMethodSetting,
} from '../../platform'
import { Segmented, type SegmentOption } from '../controls'
import { ChoiceRow, ErrorText, Group, joinIds, Note, SectionIntro, SwitchRow } from './rows'
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

// nudgeHours are the quiet times Offer a nudge after offers, in hours: 0 offers none.
const nudgeHours: readonly number[] = [0, 12, 24, 48, 72, 168]

// nudgeWords name a quiet time, short for the segmented control ("1 day") or long for its
// title ("after a day of quiet").
function nudgeWords(hours: number): { label: string; title: string } {
  if (hours === 0) {
    return { label: 'Off', title: 'Never offer a nudge' }
  }

  const label =
    hours % 168 === 0
      ? hours === 168
        ? '1 week'
        : `${String(hours / 168)} weeks`
      : hours % 24 === 0
        ? hours === 24
          ? '1 day'
          : `${String(hours / 24)} days`
        : hours === 1
          ? '1 hour'
          : `${String(hours)} hours`

  return { label, title: `Offer a nudge once it has been quiet for ${label}` }
}

// nudgeOptions are the segmented control's choices: the usual quiet times, and the stored
// one when it is none of them, as a backup can bring.
function nudgeOptions(stored: number): SegmentOption<string>[] {
  const hours = nudgeHours.includes(stored) ? nudgeHours : [...nudgeHours, stored].sort((a, b) => a - b)

  return hours.map((n) => ({ value: String(n), ...nudgeWords(n) }))
}

export interface ReviewingSectionProps extends SectionProps {
  // onManageReplies opens the review replies in the Prompts tab, from the nudges' note;
  // without it the note names the place alone.
  onManageReplies?: (() => void) | undefined
}

// ReviewingSection holds how the review sheet merges a pull request, saved as soon as it
// is chosen, when a pull request waiting on its author offers a nudge and whether a nudge
// snoozes it, each saved as soon as it is chosen, the workflows whose held runs never count
// (Ignored workflows), each change stored as it is made, and points at where the review
// replies are kept.
export function ReviewingSection({ settings: stored, onSaved, onManageReplies }: ReviewingSectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  // error is why the last choice of a merge method was refused, and nudgeError and
  // snoozeError why the last change of Offer a nudge after and of Snooze after a nudge was;
  // a refused ignored workflow shows its own.
  const [error, setError] = useState('')
  const [nudgeError, setNudgeError] = useState('')
  const [snoozeError, setSnoozeError] = useState('')
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

  const chooseNudge = async (value: string) => {
    const nudgeAfterHours = Number(value)

    setNudgeError('')

    const result = await save({ nudgeAfterHours }, () => setNudgeAfterHours(nudgeAfterHours))

    setNudgeError(result.ok ? '' : result.error)
  }

  const chooseSnooze = async (nudgeSnooze: boolean) => {
    setSnoozeError('')

    const result = await save({ nudgeSnooze }, () => setNudgeSnooze(nudgeSnooze))

    setSnoozeError(result.ok ? '' : result.error)
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
      <section aria-labelledby={`${id}-nudges`} className="flex flex-col gap-3">
        <SectionIntro id={`${id}-nudges`} title="Nudges">
          When a pull request you requested changes on goes quiet, its card offers <em>Nudge</em>: a comment asking its
          author how it is going, which you read before it is posted.
        </SectionIntro>
        <Group>
          <ChoiceRow
            label="Offer a nudge after"
            description="How long it has to be quiet: any push, comment, or review starts the wait again, and so does your nudge. Never for a bot’s pull request."
            error={nudgeError}
            errorId={`${id}-nudge-error`}
          >
            <Segmented
              label="Offer a nudge after"
              value={String(settings.nudgeAfterHours)}
              options={nudgeOptions(settings.nudgeAfterHours)}
              describedBy={joinIds(nudgeError !== '' && `${id}-nudge-error`)}
              onChange={(value) => {
                void chooseNudge(value)
              }}
            />
          </ChoiceRow>
          <SwitchRow
            label="Snooze after a nudge"
            controlId={`${id}-nudge-snooze`}
            description={`Start each nudge with “Then snooze for ${snoozeLength(settings.snoozeDays)}” ticked. The pull request comes back early when its author pushes.`}
            error={snoozeError}
            checked={settings.nudgeSnooze}
            onChange={(on) => {
              void chooseSnooze(on)
            }}
          />
        </Group>
        <Note>
          The comment starts from your <strong className="font-semibold text-text">Nudge</strong> reply in{' '}
          {onManageReplies === undefined ? (
            <span className="text-text">Prompts › Review replies</span>
          ) : (
            <button type="button" onClick={onManageReplies} className="text-accent hover:underline">
              Prompts › Review replies
            </button>
          )}
          .
        </Note>
      </section>
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
        Reply templates for approvals, requests for changes, comments, and nudges live in{' '}
        <span className="text-text">Prompts → Review replies</span>.
      </SectionIntro>
    </div>
  )
}
