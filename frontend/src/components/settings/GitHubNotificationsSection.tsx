import { useId } from 'react'

import { brand } from '../../generated/brand'
import { askAgainText, checkedText, refusal } from '../../lib/githubThreads'
import type { GitHubThreadsState } from '../../lib/useGitHubThreads'
import { useNowAs } from '../../lib/useNow'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  setGitHubNotificationAction,
  setGitHubNotificationSwitch,
  setGitHubNotificationTrigger,
  type ThreadAction,
  type ThreadSwitch,
  type ThreadTrigger,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { RefreshIcon } from '../icons'
import { rowMatchClass, useSetting } from './match'
import { ErrorText, Group, joinIds, Note, SectionIntro, SwitchRow } from './rows'
import type { SectionProps } from './types'

// triggers are the moments the work behind a notification is finished, each with its own
// switch, in the order the backend lists them. Clearing by hand has none.
const triggers: readonly { trigger: ThreadTrigger; label: string; description: string }[] = [
  { trigger: 'merged', label: 'A pull request is merged', description: 'Here or anywhere else.' },
  {
    trigger: 'pr_closed',
    label: 'A pull request is closed without merging',
    description: 'Here or anywhere else.',
  },
  {
    trigger: 'issue_closed',
    label: 'An issue is closed',
    description: 'As completed or as not planned, here or anywhere else.',
  },
  {
    trigger: 'reviewed',
    label: `You submit a review in ${brand.name}`,
    description: 'A review that approves the pull request or requests changes.',
  },
  {
    trigger: 'dismissed',
    label: `You dismiss an item in ${brand.name}`,
    description: 'From its card or its sheet.',
  },
  {
    trigger: 'unassigned',
    label: 'You unassign yourself or remove yourself as reviewer',
    description: 'With Unassign me or Remove me as reviewer, from its card or its sheet.',
  },
]

// actions are what clearing a notification can do, the default first.
const actions: readonly { value: ThreadAction; label: string }[] = [
  { value: 'done', label: 'Mark done' },
  { value: 'read', label: 'Mark read' },
]

interface StatusProps {
  threads: GitHubThreadsState
}

// Status says how reading the notifications goes: what the GitHub inbox holds and when it
// was last checked, why GitHub refuses them and when it is asked again, or why the last
// check failed; with Check again, which reads them from GitHub now, and a note when GitHub
// holds more than the app reads.
function Status({ threads }: StatusProps) {
  const { snapshot, checkNow, checking, checkError } = threads
  const status = snapshot?.status ?? null
  const read = status !== null && status.reading
  const refused = read && status.access === 'refused'
  const checked = useNowAs((now) => (read && status.lastPassAt !== '' ? checkedText(now, status) : ''))
  const askAgain = useNowAs((now) => (refused ? askAgainText(now, status) : ''))
  const problem = !read ? '' : refused ? refusal(status) : status.lastError

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-start justify-between gap-6">
        <div className="min-w-0 text-sm">
          {problem !== '' ? (
            <>
              <p className="text-warning">{problem}</p>
              {askAgain !== '' && <p className="mt-0.5 text-xs text-muted">{askAgain}</p>}
            </>
          ) : (
            status !== null && (
              <p className="text-muted tabular-nums">{checked === '' ? 'Not read from GitHub yet.' : checked}</p>
            )
          )}
        </div>
        <button
          type="button"
          disabled={checking}
          onClick={() => {
            void checkNow()
          }}
          className={`${quietButtonClass} shrink-0`}
        >
          <RefreshIcon className="size-3.5" />
          {checking ? 'Checking…' : 'Check again'}
        </button>
      </div>
      <ErrorText error={checkError === '' ? '' : `Could not check GitHub: ${checkError}`} />
      {read && status.truncated && (
        <Note>
          GitHub holds more than 1,000 notifications for you. {brand.name} reads the newest 1,000, and counts only
          those.
        </Note>
      )}
    </div>
  )
}

interface ActionGroupProps {
  action: ThreadAction
  onChoose: (action: ThreadAction) => void
  // describedBy lists more ids that describe the choice, such as the section's error.
  describedBy: string | false
}

// ActionGroup chooses what clearing a notification does: mark it done, which takes it out
// of the GitHub inbox, or read, which leaves it there.
function ActionGroup({ action, onChoose, describedBy }: ActionGroupProps) {
  const id = useId()
  const [marks] = useSetting('What clearing does')

  return (
    <Group>
      <div {...marks} className={`flex items-start gap-8 px-4 py-3.5 ${rowMatchClass}`}>
        <p id={`${id}-label`} className="w-36 shrink-0 text-sm font-medium">
          What clearing does
        </p>
        <div className="flex min-w-0 flex-col gap-2">
          <div
            role="radiogroup"
            aria-labelledby={`${id}-label`}
            aria-describedby={joinIds(`${id}-help`, describedBy)}
            className="flex flex-wrap gap-x-6 gap-y-2"
          >
            {actions.map((choice) => (
              <label key={choice.value} className="flex cursor-pointer items-center gap-2 text-sm">
                <input
                  type="radio"
                  name={`${id}-action`}
                  value={choice.value}
                  checked={action === choice.value}
                  onChange={() => {
                    onChoose(choice.value)
                  }}
                  className="radio"
                />
                {choice.label}
              </label>
            ))}
          </div>
          <p id={`${id}-help`} className="text-xs text-muted">
            Mark done takes a notification out of your GitHub inbox. Mark read leaves it there: the count on the
            toolbar’s GitHub button stays, and only its accent goes once none are unread.
          </p>
        </div>
      </div>
    </Group>
  )
}

export interface GitHubNotificationsSectionProps extends SectionProps {
  // threads is the window's one mirror of the notifications (useGitHubThreads, in App),
  // which the toolbar's button shows too; without it the section says
  // nothing of how reading them goes.
  threads: GitHubThreadsState | null
}

// GitHubNotificationsSection holds the user's rules for their GitHub notifications, each
// saved as soon as it is set: whether the app reads them, whether it clears them on its
// own, how reading them goes, the moments that clear one, what else counts, and what
// clearing does. A change to reading reads the window's mirror again, and turning reading
// or clearing on reads them from GitHub at once.
export function GitHubNotificationsSection({ settings: stored, onSaved, threads }: GitHubNotificationsSectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const rules = settings.githubNotifications
  // Every switch here points at why the last change was refused, while that shows.
  const errorId = error !== '' && `${id}-error`

  const switchRule = async (name: ThreadSwitch, on: boolean) => {
    const result = await save({ githubNotifications: { ...rules, [name]: on } }, () =>
      setGitHubNotificationSwitch(name, on),
    )

    if (!result.ok || threads === null) {
      return
    }

    // The mirror says whether they are read only once it is read again.
    if (name === 'read') {
      threads.reload()
    }

    // Reading or clearing just turned on reads them from GitHub at once.
    if (on && (name === 'read' || name === 'auto')) {
      void threads.checkNow()
    }
  }

  const switchTrigger = (trigger: ThreadTrigger, on: boolean) => {
    const triggersOff = on ? rules.triggersOff.filter((t) => t !== trigger) : [...rules.triggersOff, trigger]

    void save({ githubNotifications: { ...rules, triggersOff } }, () => setGitHubNotificationTrigger(trigger, on))
  }

  const chooseAction = (action: ThreadAction) => {
    void save({ githubNotifications: { ...rules, action } }, () => setGitHubNotificationAction(action))
  }

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-col gap-3">
        <Group>
          <SwitchRow
            label="Read my GitHub notifications"
            controlId={`${id}-read`}
            description={
              <>
                {brand.name} keeps a copy of your GitHub inbox on this Mac, read from GitHub as the inbox refreshes, and
                lists what’s unread under the toolbar’s GitHub button. Reading them changes nothing on GitHub.
              </>
            }
            checked={rules.read}
            describedBy={errorId}
            onChange={(on) => {
              void switchRule('read', on)
            }}
          />
          <SwitchRow
            label="Clear them automatically"
            controlId={`${id}-auto`}
            description={
              rules.read
                ? 'Once the work behind a notification is done, as chosen below, it is cleared on GitHub for you.'
                : 'Needs reading them: turn it on above.'
            }
            checked={rules.auto}
            disabled={!rules.read}
            describedBy={errorId}
            onChange={(on) => {
              void switchRule('auto', on)
            }}
          />
        </Group>
        {rules.read && threads !== null && <Status threads={threads} />}
      </div>
      <div className="flex flex-col gap-3">
        <SectionIntro title="When to clear">
          A notification is cleared at one of these moments, unless a person’s activity since is still unread on GitHub.
          Read it there, and it clears on the next check.
        </SectionIntro>
        <Group>
          {triggers.map(({ trigger, label, description }) => (
            <SwitchRow
              key={trigger}
              label={label}
              controlId={`${id}-${trigger}`}
              description={description}
              checked={!rules.triggersOff.includes(trigger)}
              describedBy={errorId}
              onChange={(on) => {
                switchTrigger(trigger, on)
              }}
            />
          ))}
        </Group>
        <Group>
          <SwitchRow
            label="Include repositories you only watch"
            controlId={`${id}-watching`}
            description={`Clear a notification you get only because you watch its repository once its item is merged or closed. What you do in ${brand.name} clears one either way.`}
            checked={rules.watching}
            describedBy={errorId}
            onChange={(on) => {
              void switchRule('watching', on)
            }}
          />
          <SwitchRow
            label="Count bot activity after it closes as seen"
            controlId={`${id}-bot-activity`}
            description="A bot’s activity after the item is done, such as a comment from CI, doesn’t keep its notification from being cleared."
            checked={rules.botActivity}
            describedBy={errorId}
            onChange={(on) => {
              void switchRule('botActivity', on)
            }}
          />
        </Group>
      </div>
      <ActionGroup action={rules.action} onChoose={chooseAction} describedBy={errorId} />
      <ErrorText id={`${id}-error`} error={error} className="-mt-5" />
      <Note>
        Each notification cleared spends 1 REST point, which the API budget in the toolbar counts as housekeeping.
        Nothing about your notifications is sent anywhere but GitHub.
      </Note>
    </div>
  )
}
