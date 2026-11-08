import { brand } from '../../generated/brand'
import { openExternal, type Account } from '../../platform'
import { Avatar } from '../Avatar'
import { buttonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { Note } from './rows'

// AccountSection shows the GitHub account the inbox comes from, once the inbox has
// loaded (account is null until then), and how to sign in as another.
export function AccountSection({ account }: { account: Account | null }) {
  const signedIn = account !== null && account.login !== ''
  const hasName = signedIn && account.name.trim() !== ''

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-4 rounded-lg border border-border bg-surface p-4">
        <Avatar account={account} size="lg" />
        <div className="min-w-0 flex-1">
          {signedIn ? (
            <>
              <p className="truncate font-semibold">{hasName ? account.name : account.login}</p>
              <p className="truncate text-sm text-muted">@{account.login}</p>
            </>
          ) : (
            <>
              <p className="font-semibold">Not signed in yet</p>
              <p className="text-sm text-muted">Your account appears after the first refresh.</p>
            </>
          )}
        </div>
        {signedIn && (
          <button
            type="button"
            onClick={() => {
              openExternal(account.profileUrl)
            }}
            className={buttonClass}
          >
            View on GitHub
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        )}
      </div>
      <Note>
        {brand.name} signs in through the GitHub CLI: it borrows gh&apos;s token, keeps it in memory, and never stores
        it. To use another account, run <code className="font-mono">gh auth switch</code>, then refresh the inbox.
      </Note>
    </div>
  )
}
