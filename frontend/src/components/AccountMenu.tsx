import type { ReactNode } from 'react'

import { logFailure } from '../lib/call'
import { stateLabel, toneOf, troubled, watched, worst } from '../lib/serviceStatus'
import { formatAgo } from '../lib/time'
import { toneFill, toneText } from '../lib/tone'
import { useNowAs } from '../lib/useNow'
import type { ServiceStatusState } from '../lib/useServiceStatus'
import { bugReportURL, feedbackURL, openExternal, type Account, type ServiceState } from '../platform'
import { Avatar } from './Avatar'
import { BugIcon, CommentIcon, ExternalIcon, PulseIcon, SlidersIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'

export interface AccountMenuProps {
  // account is null before the inbox has loaded; its login is "" before the first refresh.
  account: Account | null
  // service is where the services the user watches stand, which the Service status item
  // names while a part watched has a problem, and lists, a provider each, with Check
  // again; absent, or before the first reading, it names and lists nothing.
  service?: ServiceStatusState | undefined
  onOpenSettings: () => void
  // onChooseWatch opens Settings → Service status; it defaults to onOpenSettings.
  onChooseWatch?: () => void
}

// stateHint is a state as a menu item's hint: its words, toned with a dot while it is in
// trouble, and quiet otherwise.
function stateHint(state: ServiceState): ReactNode {
  const tone = toneOf(state)

  if (tone === null) {
    return stateLabel(state)
  }

  return (
    <span className={`inline-flex items-center gap-1.5 ${toneText[tone]}`}>
      <span aria-hidden="true" className={`size-1.5 rounded-full ${toneFill[tone]}`} />
      {stateLabel(state)}
    </span>
  )
}

// serviceItems are the Service status submenu: each provider watched, its state as a hint,
// opening its status page; Check again, which reads their pages now and keeps the menu
// open, so their states change in place, with when they were checked as its hint; then a
// way to choose what to watch. With nothing watched, or the check off, there is nothing to
// check again.
function serviceItems(service: ServiceStatusState | undefined, checked: string, chooseWatch: () => void): MenuItem[] {
  const choose: MenuItem = { label: 'Choose what to watch…', onSelect: chooseWatch }
  const providers = watched(service?.status ?? null)

  if (service === undefined || providers.length === 0) {
    return [choose]
  }

  const { checking, check } = service
  const items: MenuItem[] = providers.map((provider) => ({
    label: provider.name,
    hint: stateHint(provider.state),
    onSelect: () => {
      openExternal(provider.url)
    },
  }))

  items.push(
    {
      label: 'Check again',
      hint: checking ? 'Checking…' : checked === '' ? undefined : `Checked ${checked}`,
      divided: true,
      keepOpen: true,
      onSelect: () => {
        if (!checking) {
          check()
        }
      },
    },
    choose,
  )

  return items
}

// openMade opens a link Go makes now, such as a bug report with the versions as they are,
// in the browser.
function openMade(make: () => Promise<string>, what: string) {
  logFailure(make(), `could not make the ${what} link`, openExternal)
}

// AccountCard heads the menu with who is signed in, or why no one is yet. Signed in, it
// opens the account's GitHub profile when clicked, as the menu's item does, and closes
// the menu.
function AccountCard({ account, close }: { account: Account | null; close: () => void }) {
  if (account === null || account.login === '') {
    return (
      <div className="flex items-center gap-3 px-3.5 py-3">
        <Avatar account={account} size="md" />
        <div className="min-w-0">
          <p className="text-sm font-semibold">Not signed in yet</p>
          <p className="text-xs text-muted">Your account appears after the first refresh.</p>
        </div>
      </div>
    )
  }

  const hasName = account.name.trim() !== ''

  // The menu's own View GitHub profile item is the keyboard's way there, so the card
  // stays out of the tab order.
  return (
    <div className="p-1.5">
      <button
        type="button"
        tabIndex={-1}
        title="View GitHub profile"
        onClick={() => {
          close()
          openExternal(account.profileUrl)
        }}
        className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-raised focus-visible:outline-none"
      >
        <Avatar account={account} size="md" />
        <span className="min-w-0">
          <span className="block truncate text-sm font-semibold">{hasName ? account.name : account.login}</span>
          <span className="block truncate text-xs text-muted">
            {hasName ? `@${account.login}` : 'Signed in to GitHub'}
          </span>
        </span>
      </button>
    </div>
  )
}

// AccountMenu is the toolbar's profile button. It shows the signed-in account's picture
// and opens a menu with who is signed in, Settings, the account's GitHub profile, and
// Service status, which names the worst problem a service the user watches reports and
// opens a submenu of each one's status page and Check again, then a way to report a bug or
// send feedback, signed in or not. Everything but Settings, Check again, and choosing what
// to watch opens in the browser: the bug report and the feedback as a new issue on the
// project's repository, filled in for the user to read before sending.
export function AccountMenu({ account, service, onOpenSettings, onChooseWatch = onOpenSettings }: AccountMenuProps) {
  const login = account?.login ?? ''
  const trouble = troubled(service?.status ?? null)
  const checkedAt = service?.status?.checkedAt ?? ''
  const checked = useNowAs((now) => (checkedAt === '' ? '' : formatAgo(now, checkedAt)))
  const items: MenuItem[] = [
    {
      label: 'Settings',
      icon: <SlidersIcon className="size-4 text-muted" />,
      shortcut: '⌘,',
      onSelect: onOpenSettings,
    },
  ]

  if (account !== null && login !== '') {
    items.push({
      label: 'View GitHub profile',
      icon: <ExternalIcon className="size-4 text-muted" />,
      onSelect: () => {
        openExternal(account.profileUrl)
      },
    })
  }

  items.push(
    {
      label: 'Service status',
      icon: <PulseIcon className="size-4 text-muted" />,
      hint: trouble.length === 0 ? undefined : stateHint(worst(trouble)),
      items: () => serviceItems(service, checked, onChooseWatch),
    },
    {
      label: 'Report a bug',
      icon: <BugIcon className="size-4 text-muted" />,
      divided: true,
      onSelect: () => {
        openMade(bugReportURL, 'bug report')
      },
    },
    {
      label: 'Send feedback',
      icon: <CommentIcon className="size-4 text-muted" />,
      onSelect: () => {
        openMade(feedbackURL, 'feedback')
      },
    },
  )

  return (
    <MenuButton
      label={login === '' ? 'Account' : `Account: ${login}`}
      menuLabel="Account"
      title={login === '' ? 'Account' : login}
      items={items}
      header={(close) => <AccountCard account={account} close={close} />}
      panelClassName="max-w-80 min-w-64"
      className="flex size-8 items-center justify-center rounded-full transition-colors hover:bg-raised aria-expanded:bg-raised"
    >
      <Avatar account={account} size="sm" />
    </MenuButton>
  )
}
