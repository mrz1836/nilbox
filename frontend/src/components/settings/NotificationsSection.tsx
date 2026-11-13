import { useId, useState, type CSSProperties } from 'react'

import { brand } from '../../generated/brand'
import { bannerStatusText, inboxKinds, sessionBannerHint, sessionKinds } from '../../lib/notifications'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  notificationBannerStatus,
  sendTestNotification,
  setMenuBar,
  setNotificationBanner,
  setToastSeconds,
  type NotificationKind,
} from '../../platform'
import { buttonClass } from '../controls'
import { maxToastSeconds, minToastSeconds } from '../Toast'
import { ChoiceRow, ErrorText, Group, joinIds, Note, rowDescribedBy, SectionIntro, SettingRow, SwitchRow } from './rows'
import type { SectionProps } from './types'

// secondsText says a toast's length in words, for the slider's assistive text.
function secondsText(seconds: number): string {
  return `${String(seconds)} seconds`
}

interface ToastSecondsRowProps {
  // controlId is the slider's id; see SettingRow.
  controlId: string
  // seconds is the length stored (or being stored), and onChange stores another.
  seconds: number
  onChange: (seconds: number) => void
  // describedBy lists more ids that describe the slider, such as the section's error.
  describedBy: string | false
}

// ToastSecondsRow sets how long a toast stays, on a slider from minToastSeconds to
// maxToastSeconds a second at a time, with the length beside it. The length follows the
// knob as it moves, and is stored once, as the knob is let go or with each arrow key (the
// slider's change event, which React's onChange never sees: it follows input).
function ToastSecondsRow({ controlId, seconds, onChange, describedBy }: ToastSecondsRowProps) {
  // draft is the length the knob is at while it moves, or null once that is stored.
  const [draft, setDraft] = useState<number | null>(null)
  const shown = draft ?? seconds
  const fraction = (shown - minToastSeconds) / (maxToastSeconds - minToastSeconds)
  // The fill ends at the knob's middle, which runs from half a knob in from each end.
  const fill = { '--range-fill': `calc(0.5rem + (100% - 1rem) * ${String(fraction)})` } as CSSProperties

  const commit = (input: HTMLInputElement) => {
    const next = Number(input.value)

    setDraft(null)

    if (next !== seconds) {
      onChange(next)
    }
  }

  return (
    <SettingRow
      label="Toasts stay for"
      controlId={controlId}
      description="How long a message at the bottom of the window, such as Dismissed with its Undo, shows before it closes itself. It waits while the pointer rests on it."
    >
      <input
        id={controlId}
        type="range"
        min={minToastSeconds}
        max={maxToastSeconds}
        step={1}
        value={shown}
        aria-valuetext={secondsText(shown)}
        aria-describedby={rowDescribedBy(controlId, '', describedBy)}
        style={fill}
        className="range w-44"
        ref={(input) => {
          if (input === null) {
            return undefined
          }

          const committed = () => {
            commit(input)
          }

          input.addEventListener('change', committed)

          return () => {
            input.removeEventListener('change', committed)
          }
        }}
        onChange={(e) => {
          setDraft(Number(e.currentTarget.value))
        }}
      />
      <span aria-hidden="true" className="w-8 text-right text-sm text-muted tabular-nums">
        {shown} s
      </span>
    </SettingRow>
  )
}

// NotificationsSection holds the menu bar icon, how long a toast stays, and which kinds of
// notification show a banner, each saved as soon as it is set, with a test banner and a
// note when banners cannot show. The agent sessions' kinds stand apart, since they start
// off.
export function NotificationsSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const banners = useQuery(() => notificationBannerStatus(), [], {
    log: 'could not read whether notifications are allowed',
  })
  const tester = useMutation(() => sendTestNotification())
  // sent is whether the last test was sent; it is false again while the next one sends.
  const [sent, setSent] = useState(false)
  const status = banners.data ?? ''
  const off = settings.bannersOff
  // Every switch here points at why the last change was refused, while that shows.
  const errorId = error !== '' && `${id}-error`

  const switchBanner = (kind: NotificationKind, on: boolean) => {
    const bannersOff = on ? off.filter((k) => k !== kind) : [...off, kind]

    void save({ bannersOff }, () => setNotificationBanner(kind, on))
  }

  const sendTest = async () => {
    setSent(false)
    setSent((await tester.run()).ok)
    // Asking to show one may have changed whether they are allowed.
    void banners.refetch()
  }

  const statusText = bannerStatusText(status)

  return (
    <div className="flex flex-col gap-8">
      <Group>
        <SwitchRow
          label="Show in the menu bar"
          controlId={`${id}-menu-bar`}
          description={
            <>
              The icon shows how many items need you, and its menu lists them. While it is there, closing the window
              keeps {brand.name} running and refreshing; quit from its menu or with ⌘Q.
            </>
          }
          checked={settings.menuBar}
          describedBy={errorId}
          onChange={(menuBar) => {
            void save({ menuBar }, () => setMenuBar(menuBar))
          }}
        />
        <ToastSecondsRow
          controlId={`${id}-toast-seconds`}
          seconds={settings.toastSeconds}
          describedBy={errorId}
          onChange={(toastSeconds) => {
            void save({ toastSeconds }, () => setToastSeconds(toastSeconds))
          }}
        />
      </Group>
      <div className="flex flex-col gap-3">
        <SectionIntro title="Banners">
          After each refresh, {brand.name} can show a banner for what newly needs you, when a release you started
          reaches GitHub, and when a run from the Scripts tab fails. More than three at once arrive as one banner that
          sums them up. Clicking a banner shows its item here, a release on GitHub, or a failed run in Scripts › Runs.
        </SectionIntro>
        {statusText !== '' && (
          <p className="rounded-md border border-warning px-3 py-2 text-xs text-warning">{statusText}</p>
        )}
        <Group>
          {inboxKinds.map(({ kind, label, description }) => (
            <SwitchRow
              key={kind}
              label={label}
              controlId={`${id}-${kind}`}
              description={description}
              checked={!off.includes(kind)}
              describedBy={errorId}
              onChange={(on) => {
                switchBanner(kind, on)
              }}
            />
          ))}
          <ChoiceRow
            label="Test"
            description={
              <>
                <span id={`${id}-test-help`}>
                  {sent
                    ? 'Sent. If no banner showed, check Focus and System Settings › Notifications.'
                    : 'Show a sample banner, asking for permission first if you have not been asked.'}
                </span>
                {tester.error !== '' && (
                  <span id={`${id}-test-error`} role="alert" className="mt-1 block text-danger">
                    {tester.error}
                  </span>
                )}
              </>
            }
          >
            <button
              type="button"
              aria-describedby={joinIds(`${id}-test-help`, tester.error !== '' && `${id}-test-error`)}
              disabled={tester.pending || status === 'unavailable'}
              onClick={() => {
                void sendTest()
              }}
              className={buttonClass}
            >
              {tester.pending ? 'Sending…' : 'Send a test'}
            </button>
          </ChoiceRow>
        </Group>
      </div>
      <div className="flex flex-col gap-3">
        <SectionIntro title="Agent sessions">
          {brand.name} can also show a banner when a coding agent session you started from an item needs you. Clicking
          one shows its item here. {sessionBannerHint}
        </SectionIntro>
        <Group>
          {sessionKinds.map(({ kind, label, description }) => (
            <SwitchRow
              key={kind}
              label={label}
              controlId={`${id}-${kind}`}
              description={description}
              checked={!off.includes(kind)}
              describedBy={errorId}
              onChange={(on) => {
                switchBanner(kind, on)
              }}
            />
          ))}
        </Group>
      </div>
      <ErrorText id={`${id}-error`} error={error} className="-mt-5" />
      <Note>
        The bell in the toolbar keeps every notification, banner or not, so you can look back at what arrived.
        Notifications are made on this Mac from what the inbox already reads; nothing is sent anywhere else.
      </Note>
    </div>
  )
}
