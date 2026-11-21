import { useId, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { bannerStatusText } from '../../lib/notifications'
import { useMutation } from '../../lib/useMutation'
import type { Query } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  sendTestNotification,
  setGitHubNotificationSwitch,
  setMenuBar,
  setNotificationBanner,
  type BannerStatus,
  type NotificationKind,
  type Settings,
  type ThreadSwitch,
} from '../../platform'
import { buttonClass, primaryButtonClass, Switch } from '../controls'
import { BellIcon, CircleCheckIcon } from '../icons'
import { Group } from '../settings/rows'

interface ToggleProps {
  label: string
  help: ReactNode
  checked: boolean
  disabled?: boolean
  onChange: (on: boolean) => void
}

// Toggle is one switch of the step, its label and a line of help beside it.
function Toggle({ label, help, checked, disabled = false, onChange }: ToggleProps) {
  const id = useId()

  return (
    <div className="flex items-start justify-between gap-6 px-4 py-3.5">
      <div className="min-w-0">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
        </label>
        <p id={`${id}-help`} className="text-xs text-muted">
          {help}
        </p>
      </div>
      <Switch id={id} checked={checked} disabled={disabled} describedBy={`${id}-help`} onChange={onChange} />
    </div>
  )
}

// sessionToggles are the agent sessions' banners the step offers, which start off.
const sessionToggles: readonly { kind: NotificationKind; label: string; help: string }[] = [
  {
    kind: 'session_needs_you',
    label: 'Tell me when an agent needs me',
    help: 'A banner when a Claude Code or Codex session waits for your approval or answer.',
  },
  {
    kind: 'session_finished',
    label: 'Tell me when an agent finishes its turn',
    help: 'A banner when a session is done working and waits for your next message.',
  },
]

export interface NotificationsStepProps {
  settings: Settings
  onSaved: (settings: Settings) => void
  // banners is whether this Mac shows the app's banners, which the setup shares with
  // Finish.
  banners: Query<BannerStatus>
}

// NotificationsStep chooses how the app tells the user that something needs them: the
// banners, turned on with a sample, which makes macOS ask once; the agent sessions'
// banners; the menu bar icon; and the mirror of their GitHub notifications, read and
// cleared automatically. Each is saved as it is set.
export function NotificationsStep({ settings: stored, onSaved, banners }: NotificationsStepProps) {
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const tester = useMutation(() => sendTestNotification())
  const [sent, setSent] = useState(false)
  const status = banners.data ?? ''
  const off = settings.bannersOff
  const rules = settings.githubNotifications

  const turnOn = async () => {
    setSent(false)
    setSent((await tester.run()).ok)
    // Asking to show one may have changed whether they are allowed.
    void banners.refetch()
  }

  const switchBanner = (kind: NotificationKind, on: boolean) => {
    const bannersOff = on ? off.filter((k) => k !== kind) : [...off, kind]

    void save({ bannersOff }, () => setNotificationBanner(kind, on))
  }

  const switchThreads = (name: ThreadSwitch, on: boolean) => {
    void save({ githubNotifications: { ...rules, [name]: on } }, () => setGitHubNotificationSwitch(name, on))
  }

  const statusText = bannerStatusText(status)

  return (
    <div className="flex flex-col gap-8">
      <section aria-label="Banners" className="flex flex-col gap-3">
        <div className="flex items-start gap-4 rounded-lg border border-border bg-surface px-4 py-4">
          <BellIcon className="mt-0.5 size-5 shrink-0 text-accent" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-semibold">Banners</p>
            <p className="mt-0.5 text-xs text-muted">
              After each refresh, a banner for what newly needs you, a release you started, and a run that failed.
            </p>
            {status === 'on' && (
              <p className="mt-2 flex items-center gap-1.5 text-xs font-medium text-success">
                <CircleCheckIcon className="size-3.5" />
                Banners are on.
              </p>
            )}
            {statusText !== '' && <p className="mt-2 text-xs text-warning">{statusText}</p>}
            {sent && (
              <p role="status" className="mt-2 text-xs text-muted">
                Sent. If no banner showed, check Focus and System Settings › Notifications.
              </p>
            )}
            {tester.error !== '' && (
              <p role="alert" className="mt-2 text-xs text-danger">
                {tester.error}
              </p>
            )}
          </div>
          <button
            type="button"
            disabled={tester.pending || status === 'unavailable' || status === ''}
            onClick={() => {
              void turnOn()
            }}
            className={status === 'on' ? buttonClass : primaryButtonClass}
          >
            {tester.pending ? 'Sending…' : status === 'on' ? 'Send a test' : 'Turn on banners'}
          </button>
        </div>

        <Group>
          {sessionToggles.map(({ kind, label, help }) => (
            <Toggle
              key={kind}
              label={label}
              help={help}
              checked={!off.includes(kind)}
              onChange={(on) => {
                switchBanner(kind, on)
              }}
            />
          ))}
          <Toggle
            label={`Keep ${brand.name} in the menu bar`}
            help={
              settings.menuBar
                ? `Closing the window keeps ${brand.name} running and refreshing; quit from its menu or with ⌘Q.`
                : 'Closing the window quits the app.'
            }
            checked={settings.menuBar}
            onChange={(menuBar) => {
              void save({ menuBar }, () => setMenuBar(menuBar))
            }}
          />
        </Group>
      </section>

      <section aria-label="GitHub notifications" className="flex flex-col gap-3">
        <h3 className="text-sm font-semibold">GitHub notifications</h3>
        <Group>
          <Toggle
            label="Mirror my GitHub notifications"
            help="Read them here, beside your inbox, as GitHub lists them."
            checked={rules.read}
            onChange={(on) => {
              switchThreads('read', on)
            }}
          />
          <Toggle
            label="Clear them automatically"
            help="Mark one done on GitHub once the work behind it is done here, such as a pull request you merged."
            checked={rules.auto}
            disabled={!rules.read}
            onChange={(on) => {
              switchThreads('auto', on)
            }}
          />
        </Group>
        <p className="text-xs text-muted">More choices wait in Settings → GitHub notifications.</p>
      </section>

      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}
