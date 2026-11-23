import { useId, useState } from 'react'

import { foldBot } from '../../lib/bots'
import { attempt } from '../../lib/call'
import { counted } from '../../lib/format'
import { snoozeLength } from '../../lib/triage'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave, type SettingsSave } from '../../lib/useSettingsSave'
import {
  forgetPeople,
  peopleSummary,
  setBypassOwn,
  setIgnoredWorkflows,
  setMergeBots,
  setMergeBotsOn,
  setMergeMethod,
  setNudgeAfterHours,
  setNudgeSnooze,
  type MergeMethodSetting,
  type PeopleSummary,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, ErrorText, linkClass, Segmented, type SegmentOption } from '../controls'
import { ChoiceRow, Group, groupClass, joinIds, Note, SectionIntro, SettingRow, SwitchRow } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'
import { useListChange } from './useListChange'

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

// learnedWords say how much the app learned of who works where, as in "12 people across 3
// repositories", or that it is still counting.
function learnedWords(summary: PeopleSummary | undefined): string {
  if (summary === undefined) {
    return 'Counting what the app learned…'
  }

  if (summary.people === 0) {
    return 'Nothing yet: the app learns as you open pull requests and issues.'
  }

  return `${counted(summary.people, 'person', 'people')} across ${counted(summary.repos, 'repository', 'repositories')}.`
}

// People says how much the app learned of who works on each repository and who can be
// assigned there, which the reviewer and assignee pickers suggest from, and forgets it all
// once the user confirms, after which the pickers learn again from what the app reads next.
function People() {
  const id = useId()
  const summary = useQuery(() => peopleSummary(), [], { log: 'Count the people the app learned' })
  const [confirming, setConfirming] = useState(false)
  const [forgetting, setForgetting] = useState(false)
  const [error, setError] = useState('')
  const controlId = `${id}-forget`

  const forget = async () => {
    setForgetting(true)
    setError('')

    const result = await attempt(forgetPeople())

    setForgetting(false)
    setConfirming(false)

    if (result.ok) {
      summary.set(() => result.data)
    } else {
      setError(result.error)
    }
  }

  return (
    <section aria-labelledby={`${id}-people`} className="flex flex-col gap-3">
      <SectionIntro id={`${id}-people`} title="People">
        Who works on each repository, and who can be assigned there, which the reviewer and assignee pickers suggest
        from without asking GitHub. The app learns it from the pull requests and issues you open, and keeps it on this
        Mac.
      </SectionIntro>
      <Group>
        <SettingRow label="Learned people" controlId={controlId} description={learnedWords(summary.data)} error={error}>
          <button
            id={controlId}
            type="button"
            aria-label="Forget the people the app learned"
            aria-describedby={joinIds(`${controlId}-help`, error !== '' && `${controlId}-error`)}
            disabled={summary.data === undefined || summary.data.people === 0 || forgetting}
            onClick={() => {
              setConfirming(true)
            }}
            className={buttonClass}
          >
            Forget…
          </button>
        </SettingRow>
      </Group>
      {confirming && (
        <ConfirmDialog
          title="Forget the people the app learned?"
          confirmLabel={forgetting ? 'Forgetting…' : 'Forget'}
          busy={forgetting}
          onCancel={() => {
            setConfirming(false)
          }}
          onConfirm={() => {
            void forget()
          }}
        >
          The pickers start again from what the app reads next, and read who can be assigned in each repository from
          GitHub again as you open them. Nothing on GitHub changes.
        </ConfirmDialog>
      )}
    </section>
  )
}

// MergeBots holds the auto-merge bots: the switch that has a pull request's card say when a
// bot on the list approved it, so it merges by itself once its checks pass, and the list,
// each change stored as it is made. The app ships no bot's name; the user lists their own.
function MergeBots({ settings, save }: Pick<SettingsSave, 'settings' | 'save'>) {
  const id = useId()
  const [onError, setOnError] = useState('')
  // Every change of the list runs through one action: one at a time, with the list's
  // controls held while it runs.
  const { draft, setDraft, refusal, refuse, store: change, busy } = useListChange(save)

  const bots = settings.mergeBots
  const listError = refusal.draft === draft ? refusal.message : ''

  const chooseOn = async (mergeBotsOn: boolean) => {
    setOnError('')

    const result = await save({ mergeBotsOn }, () => setMergeBotsOn(mergeBotsOn))

    setOnError(result.ok ? '' : result.error)
  }

  // store replaces the auto-merge bots, kept sorted as they are stored.
  const store = (next: string[], then?: () => void) => {
    const sorted = [...next].sort()

    change({ shows: { mergeBots: sorted }, call: () => setMergeBots(sorted), then })
  }

  const add = () => {
    const login = foldBot(draft)

    if (login === '') {
      refuse('Enter a bot’s login, such as acme-org-automerge.')

      return
    }

    if (bots.includes(login)) {
      refuse(`${login} is already listed.`)

      return
    }

    store([...bots, login], () => {
      setDraft('')
    })
  }

  return (
    <section aria-labelledby={`${id}-merge-bots`} className="flex flex-col gap-3">
      <SectionIntro id={`${id}-merge-bots`} title="Auto-merge bots">
        Bots that approve a pull request and merge it themselves once its checks pass, such as an org’s own GitHub App.
        When one on the list approved a pull request, its card says <em>Auto-merge</em>, so you can tell it apart from
        one that needs you.
      </SectionIntro>
      <Group>
        <SwitchRow
          label="Show when an auto-merge bot approved a pull request"
          controlId={`${id}-merge-bots-on`}
          description="The card, its compact row, and the review sheet name the bot. It costs no extra GitHub request: the refresh already reads each pull request’s latest reviews."
          error={onError}
          checked={settings.mergeBotsOn}
          onChange={(on) => {
            void chooseOn(on)
          }}
        />
      </Group>
      <StringListEditor
        label="Auto-merge bots"
        items={bots}
        empty="No bot is listed. Add the login of the bot that approves and merges your pull requests."
        removeLabel={(bot) => `Stop showing ${bot}’s approvals`}
        onRemove={(bot) => {
          store(bots.filter((b) => b !== bot))
        }}
        inputLabel="Auto-merge bot"
        placeholder="acme-org-automerge"
        submitLabel="Add bot"
        draft={draft}
        onDraftChange={setDraft}
        onAdd={add}
        error={listError}
        describedBy={`${id}-merge-bots-note`}
        busy={busy}
      />
      <Note id={`${id}-merge-bots-note`}>
        Use the login GitHub shows for the bot, with or without <code className="font-mono">[bot]</code>. Only the bot’s
        latest review counts, and only when GitHub reports its author as a bot, so a person with a similar login never
        does.
      </Note>
    </section>
  )
}

export interface ReviewingSectionProps extends SectionProps {
  // onManageReplies opens the review replies in the Prompts tab, from the nudges' note;
  // without it the note names the place alone.
  onManageReplies?: (() => void) | undefined
}

// ReviewingSection holds how the review sheet merges a pull request and whether its button
// merges the user's own pull request past the rules when an approval from someone else is
// all it lacks, when a pull request waiting on its author offers a nudge and whether a
// nudge snoozes it, each saved as soon as it is chosen, the workflows whose held runs never
// count (Ignored workflows), each change stored as it is made, the auto-merge bots whose
// approval a card names, the people the app learned, which it forgets on request, and
// points at where the review replies are kept.
export function ReviewingSection({ settings: stored, onSaved, onManageReplies }: ReviewingSectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  // error is why the last choice of a merge method was refused, bypassError why the last
  // change of Merge your own pull requests past the rules was, and nudgeError and
  // snoozeError why the last change of Offer a nudge after and of Snooze after a nudge was;
  // a refused ignored workflow shows its own.
  const [error, setError] = useState('')
  const [bypassError, setBypassError] = useState('')
  const [nudgeError, setNudgeError] = useState('')
  const [snoozeError, setSnoozeError] = useState('')
  // Every change of the ignored workflows runs through one action: one at a time, with the
  // list's controls held while it runs.
  const { draft, setDraft, refusal, refuse, store: change, busy } = useListChange(save)

  const names = settings.ignoredWorkflows
  const ignoreError = refusal.draft === draft ? refusal.message : ''

  const choose = async (mergeMethod: MergeMethodSetting) => {
    setError('')

    const result = await save({ mergeMethod }, () => setMergeMethod(mergeMethod))

    setError(result.ok ? '' : result.error)
  }

  const chooseBypass = async (bypassOwn: boolean) => {
    setBypassError('')

    const result = await save({ bypassOwn }, () => setBypassOwn(bypassOwn))

    setBypassError(result.ok ? '' : result.error)
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
    change({ shows: { ignoredWorkflows: next }, call: () => setIgnoredWorkflows(next), then })
  }

  const add = () => {
    // The button is disabled while the field is blank, so the name is never empty.
    const name = draft.trim()

    if (names.includes(name)) {
      refuse(`${name} is already ignored.`)

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
        <div role="radiogroup" aria-labelledby={`${id}-merge`} aria-describedby={`${id}-note`} className={groupClass}>
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
          A pull request with auto-merge on merges the way its auto-merge does, and a repository that doesn’t allow the
          method you choose merges the way it offers first.
        </Note>
        <Group>
          <SwitchRow
            label="Merge your own pull requests past the rules"
            controlId={`${id}-bypass-own`}
            description="When an approval from someone else is all your pull request lacks, and GitHub lets you merge past the branch’s rules, the review sheet’s button does that instead of Merge when ready. You still confirm each rule it skips."
            error={bypassError}
            checked={settings.bypassOwn}
            onChange={(on) => {
              void chooseBypass(on)
            }}
          />
        </Group>
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
            <button type="button" onClick={onManageReplies} className={linkClass}>
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
      <MergeBots settings={settings} save={save} />
      <People />
      <SectionIntro title="Review replies">
        Reply templates for approvals, requests for changes, comments, and nudges live in{' '}
        <span className="text-text">Prompts → Review replies</span>.
      </SectionIntro>
    </div>
  )
}
