import { useId } from 'react'

import {
  commentThresholds,
  commitThresholds,
  reviewWakes,
  snoozeChoices,
  snoozeLength,
  snoozeShort,
} from '../../lib/triage'
import { useSettingsSave } from '../../lib/useSettingsSave'
import { setSnoozeDays, setSnoozeWake, type SnoozeRules } from '../../platform'
import { Segmented } from '../controls'
import { ChoiceRow, ErrorText, Group, joinIds, Note, SectionIntro } from './rows'
import type { SectionProps } from './types'

// thresholdOption is one choice of a wake rule's count: 0 is never, 1 any.
function thresholdOption(n: number, any: string, many: (n: number) => string) {
  if (n === 0) {
    return { value: '0', label: 'Never', title: 'Never wakes a snooze' }
  }

  return n === 1
    ? { value: '1', label: 'Any', title: any }
    : { value: String(n), label: `${String(n)}+`, title: many(n) }
}

const commitOptions = commitThresholds.map((n) =>
  thresholdOption(n, 'Any push, a force-push included', (k) => `${String(k)} or more new commits`),
)

const commentOptions = commentThresholds.map((n) =>
  thresholdOption(n, 'Any new comment', (k) => `${String(k)} or more new comments`),
)

const reviewOptions: readonly { value: (typeof reviewWakes)[number]; label: string; title: string }[] = [
  { value: 'never', label: 'Never', title: 'Reviews never wake a snooze' },
  { value: 'decisions', label: 'Decisions', title: 'An approval or a request for changes' },
  { value: 'any', label: 'Any', title: 'Any review, including one that only comments' },
]

// SnoozingSection holds how long the Snooze button sets an item aside and which new
// activity ends a snooze early, each saved as soon as it is chosen.
export function SnoozingSection({ settings: stored, onSaved }: SectionProps) {
  const errorId = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const wake = settings.snoozeWake
  // Every choice here points at why the last one was refused, while that shows.
  const describedBy = joinIds(error !== '' && errorId)

  const chooseDays = (value: string) => {
    const snoozeDays = Number(value)

    void save({ snoozeDays }, () => setSnoozeDays(snoozeDays))
  }

  const chooseWake = (change: Partial<SnoozeRules>) => {
    const snoozeWake = { ...wake, ...change }

    void save({ snoozeWake }, () => setSnoozeWake(snoozeWake))
  }

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <ChoiceRow
          label="Snooze for"
          description="How long a row’s Snooze button sets an item aside. Its arrow offers the other lengths for one snooze."
        >
          <Segmented
            label="Snooze for"
            value={String(settings.snoozeDays)}
            options={snoozeChoices.map((days) => ({
              value: String(days),
              label: snoozeShort(days),
              title: snoozeLength(days),
            }))}
            describedBy={describedBy}
            onChange={chooseDays}
          />
        </ChoiceRow>
      </Group>
      <div className="flex flex-col gap-3">
        <SectionIntro title="Wake early">
          A snoozed item comes back before its time when new activity since you snoozed it reaches any of these. Its
          card then says what was new, such as <em>Back early: approved, 2 new commits</em>.
        </SectionIntro>
        <Group>
          <ChoiceRow
            label="New commits"
            description="Any counts a force-push too, except on a bot’s pull request, where a force-push is usually a rebase."
          >
            <Segmented
              label="Wake on new commits"
              value={String(wake.commits)}
              options={commitOptions}
              describedBy={describedBy}
              onChange={(v) => {
                chooseWake({ commits: Number(v) })
              }}
            />
          </ChoiceRow>
          <ChoiceRow
            label="Reviews"
            description="Decisions are approvals and requests for changes. Any adds reviews that only comment."
          >
            <Segmented
              label="Wake on reviews"
              value={wake.reviews}
              options={reviewOptions}
              describedBy={describedBy}
              onChange={(reviews) => {
                chooseWake({ reviews })
              }}
            />
          </ChoiceRow>
          <ChoiceRow
            label="New comments"
            description="Conversation and review comments, bots’ included: a higher count rides out their noise."
          >
            <Segmented
              label="Wake on new comments"
              value={String(wake.comments)}
              options={commentOptions}
              describedBy={describedBy}
              onChange={(v) => {
                chooseWake({ comments: Number(v) })
              }}
            />
          </ChoiceRow>
        </Group>
      </div>
      <ErrorText id={errorId} error={error} className="-mt-5" />
      <Note>
        A snoozed item waits in its list&apos;s <strong className="font-medium">Snoozed</strong> tab, and an item back
        on time is marked <em>Back from snooze</em>. A dismissed item never comes back on its own: it stays in the{' '}
        <strong className="font-medium">Dismissed</strong> tab until you restore it. Both stay on this Mac, and GitHub
        never hears about them.
      </Note>
    </div>
  )
}
