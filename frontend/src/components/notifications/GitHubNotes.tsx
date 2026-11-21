import { useState } from 'react'

import { brand } from '../../generated/brand'
import { askAgainText, refusal } from '../../lib/githubThreads'
import { useNowAs } from '../../lib/useNow'
import { useSettingsSave } from '../../lib/useSettingsSave'
import { setGitHubNotificationSwitch, type GitHubThreadsStatus, type Settings } from '../../platform'
import { ErrorText, quietButtonClass } from '../controls'

// noteClass is where a note sits in the toolbar's panel: a band across it. The page passes
// its own.
const noteClass = 'border-b border-border px-4 py-2.5'

export interface RefusedProps {
  status: GitHubThreadsStatus
  className?: string
}

// Refused says GitHub refuses the token the notifications, and when the app asks again; a
// refresh button beside it asks now.
export function Refused({ status, className = noteClass }: RefusedProps) {
  const askAgain = useNowAs((now) => askAgainText(now, status))

  return (
    <div className={className}>
      <p className="text-xs font-medium text-warning">{refusal(status)}</p>
      {askAgain !== '' && <p className="mt-0.5 text-xs text-muted">{askAgain}</p>}
    </div>
  )
}

export interface TurnOnClearingProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // onTurnedOn checks GitHub once clearing is on.
  onTurnedOn: () => void
  className?: string
}

// TurnOnClearing offers to clear notifications on their own, and turns that on through the
// window's settings queue (useSettingsSave), so what it stores never replaces a newer change
// made in Settings; then it checks GitHub.
export function TurnOnClearing({ settings: stored, onSaved, onTurnedOn, className = noteClass }: TurnOnClearingProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const [pending, setPending] = useState(false)

  const turnOn = async () => {
    setPending(true)

    const result = await save({ githubNotifications: { ...settings.githubNotifications, auto: true } }, () =>
      setGitHubNotificationSwitch('auto', true),
    )

    setPending(false)

    if (result.ok) {
      onTurnedOn()
    }
  }

  return (
    <div className={className}>
      <p className="text-xs text-muted">
        {brand.name} can clear a notification on GitHub once the work behind it is done, such as a pull request merged
        or your review submitted.
      </p>
      <ErrorText error={error} className="mt-1">
        Could not turn on automatic clearing:{' '}
      </ErrorText>
      <button
        type="button"
        disabled={pending}
        onClick={() => {
          void turnOn()
        }}
        className={`mt-1.5 ${quietButtonClass}`}
      >
        {pending ? 'Turning on…' : 'Turn on automatic clearing'}
      </button>
    </div>
  )
}
