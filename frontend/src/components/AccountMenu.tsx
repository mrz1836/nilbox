import { brief, toneFill, toneOf, toneText } from '../lib/githubStatus'
import { bugReportURL, feedbackURL, openExternal, type Account, type GitHubStatus } from '../platform'
import { Avatar } from './Avatar'
import { BugIcon, CommentIcon, ExternalIcon, PulseIcon, SlidersIcon } from './icons'
import { MenuButton, type MenuItem } from './Menu'

// githubStatusUrl is GitHub's own status page, which says when GitHub has an incident:
// the inbox, reviews, and checks can lag or fail while it does.
export const githubStatusUrl = 'https://www.githubstatus.com/'

export interface AccountMenuProps {
  // account is null before the inbox has loaded; its login is "" before the first refresh.
  account: Account | null
  // githubStatus is the last reading of GitHub's status page, which the GitHub status item
  // names while GitHub reports a problem; null or absent, it names nothing.
  githubStatus?: GitHubStatus | null
  onOpenSettings: () => void
}

// openMade opens a link Go makes now, such as a bug report with the versions as they are,
// in the browser.
function openMade(make: () => Promise<string>, what: string) {
  make().then(openExternal, (err: unknown) => {
    console.error(`could not make the ${what} link`, err)
  })
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
// GitHub's status page, which names what the page reports while GitHub has a problem, then
// a way to report a bug or send feedback, signed in or not. Everything but Settings opens
// in the browser: the bug report and the feedback as a new issue on the project's
// repository, filled in for the user to read before sending.
export function AccountMenu({ account, githubStatus = null, onOpenSettings }: AccountMenuProps) {
  const login = account?.login ?? ''
  const tone = githubStatus === null ? null : toneOf(githubStatus.state)
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
      label: 'GitHub status',
      icon: <PulseIcon className="size-4 text-muted" />,
      hint:
        githubStatus === null || tone === null ? undefined : (
          <span className={`inline-flex items-center gap-1.5 ${toneText[tone]}`}>
            <span aria-hidden="true" className={`size-1.5 rounded-full ${toneFill[tone]}`} />
            {brief(githubStatus.state)}
          </span>
        ),
      onSelect: () => {
        openExternal(githubStatusUrl)
      },
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
