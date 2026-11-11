import { openExternal, type Account } from '../platform'
import { Avatar } from './Avatar'
import { ExternalIcon, PulseIcon, SlidersIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'

// githubStatusUrl is GitHub's own status page, which says when GitHub has an incident:
// the inbox, reviews, and checks can lag or fail while it does.
export const githubStatusUrl = 'https://www.githubstatus.com/'

export interface AccountMenuProps {
  // account is null before the inbox has loaded; its login is "" before the first refresh.
  account: Account | null
  onOpenSettings: () => void
}

// AccountCard heads the menu with who is signed in, or why no one is yet.
function AccountCard({ account }: { account: Account | null }) {
  const signedIn = account !== null && account.login !== ''
  const hasName = signedIn && account.name.trim() !== ''

  return (
    <div className="flex items-center gap-3 px-3.5 py-3">
      <Avatar account={account} size="md" />
      <div className="min-w-0">
        {signedIn ? (
          <>
            <p className="truncate text-sm font-semibold">{hasName ? account.name : account.login}</p>
            <p className="truncate text-xs text-muted">{hasName ? `@${account.login}` : 'Signed in to GitHub'}</p>
          </>
        ) : (
          <>
            <p className="text-sm font-semibold">Not signed in yet</p>
            <p className="text-xs text-muted">Your account appears after the first refresh.</p>
          </>
        )}
      </div>
    </div>
  )
}

// AccountMenu is the toolbar's profile button. It shows the signed-in account's picture
// and opens a menu with who is signed in, Settings, the account's GitHub profile, and
// GitHub's status page, signed in or not, which opens in the browser.
export function AccountMenu({ account, onOpenSettings }: AccountMenuProps) {
  const login = account?.login ?? ''
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

  items.push({
    label: 'GitHub status',
    icon: <PulseIcon className="size-4 text-muted" />,
    onSelect: () => {
      openExternal(githubStatusUrl)
    },
  })

  return (
    <MenuButton
      label={login === '' ? 'Account' : `Account: ${login}`}
      menuLabel="Account"
      title={login === '' ? 'Account' : login}
      items={items}
      header={<AccountCard account={account} />}
      panelClassName="w-64"
      className="flex size-8 items-center justify-center rounded-full transition-colors hover:bg-raised aria-expanded:bg-raised"
    >
      <Avatar account={account} size="sm" />
    </MenuButton>
  )
}
