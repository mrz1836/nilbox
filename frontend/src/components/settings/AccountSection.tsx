import { useId } from 'react'

import { brand } from '../../generated/brand'
import { grantProjectsCommand } from '../../lib/projects'
import { openExternal, type Account, type ProjectAccess } from '../../platform'
import { Avatar } from '../Avatar'
import { CommandLine } from '../CommandLine'
import { buttonClass } from '../controls'
import { CircleCheckIcon, ExternalIcon, ProjectIcon } from '../icons'
import { cardMatchClass, useSetting } from './match'
import { Note } from './rows'

// ProjectsAccess says whether issue cards show the GitHub projects each issue is in, and,
// while the GitHub CLI's token can't read them, the command that lets it.
function ProjectsAccess({ access }: { access: ProjectAccess }) {
  const headingId = useId()
  const [marks] = useSetting('Projects on issues')

  return (
    <section
      {...marks}
      aria-labelledby={headingId}
      className={`flex items-start gap-3 rounded-lg border border-border bg-surface px-4 py-3.5 ${cardMatchClass}`}
    >
      <ProjectIcon className="mt-0.5 size-4 shrink-0 text-muted" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-4">
          <h3 id={headingId} className="text-sm font-medium">
            Projects on issues
          </h3>
          {access === 'on' && (
            <span className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-success">
              <CircleCheckIcon className="size-3.5" />
              On
            </span>
          )}
          {access === 'needs_scope' && <span className="shrink-0 text-xs font-medium text-muted">Not set up</span>}
        </div>
        {access === 'on' && (
          <p className="mt-0.5 text-xs text-muted">
            Issue cards show each GitHub project an issue is in, with its status there. {brand.name} reads them with
            your token&apos;s read-only <code className="font-mono">read:project</code> scope.
          </p>
        )}
        {access === 'needs_scope' && (
          <>
            <p className="mt-0.5 text-xs text-muted">
              To show the GitHub project each issue is in and its status there, give the GitHub CLI read-only access to
              your projects. Run this in a terminal, then refresh the inbox:
            </p>
            <CommandLine command={grantProjectsCommand} className="mt-2 max-w-sm" />
          </>
        )}
        {access === '' && (
          <p className="mt-0.5 text-xs text-muted">
            The first refresh checks whether {brand.name} can read the GitHub projects your issues are in.
          </p>
        )}
      </div>
    </section>
  )
}

export interface AccountSectionProps {
  // account is null until the inbox has loaded.
  account: Account | null
  // projectAccess is whether the last refresh could read issue projects, '' before one has.
  projectAccess: ProjectAccess
}

// AccountSection shows the GitHub account the inbox comes from, once the inbox has
// loaded, whether issue cards show their projects, and how to sign in as another.
export function AccountSection({ account, projectAccess }: AccountSectionProps) {
  const signedIn = account !== null && account.login !== ''
  const hasName = signedIn && account.name.trim() !== ''
  const [marks] = useSetting('GitHub account')

  return (
    <div className="flex flex-col gap-3">
      <div
        {...marks}
        className={`flex items-center gap-4 rounded-lg border border-border bg-surface p-4 ${cardMatchClass}`}
      >
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
      <ProjectsAccess access={projectAccess} />
      <Note>
        {brand.name} signs in through the GitHub CLI: it borrows gh&apos;s token, keeps it in memory, and never stores
        it. To use another account, run <code className="font-mono">gh auth switch</code>, then refresh the inbox.
      </Note>
    </div>
  )
}
