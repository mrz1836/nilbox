import { useId, useState } from 'react'

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
  type NotificationKind,
} from '../../platform'
import { buttonClass } from '../controls'
import { ChoiceRow, ErrorText, Group, joinIds, Note, SectionIntro, SwitchRow } from './rows'
import type { SectionProps } from './types'

// NotificationsSection holds the menu bar icon and which kinds of notification show a
// banner, each saved as soon as it is switched, with a test banner and a note when banners
// cannot show. The agent sessions' kinds stand apart, since they start off.
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
      </Group>
      <div className="flex flex-col gap-3">
        <SectionIntro title="Banners">
          After each refresh, {brand.name} can show a banner for what newly needs you. More than three at once arrive as
          one banner that sums them up. Clicking a banner shows its item here.
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
