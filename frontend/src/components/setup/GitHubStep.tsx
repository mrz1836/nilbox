import { useEffect, useEffectEvent, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { PicturesContext, usePictures } from '../../lib/pictures'
import { grantProjectsCommand } from '../../lib/projects'
import type { Query } from '../../lib/useQuery'
import { openExternal, type GitHubSignIn } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { CommandLine } from '../CommandLine'
import { buttonClass, chipClass } from '../controls'
import { AlertIcon, CircleCheckIcon, ExternalIcon, ProjectIcon, RefreshIcon } from '../icons'
import { Spinner } from '../Spinner'

// cliPage is where the GitHub CLI is downloaded.
const cliPage = 'https://cli.github.com'

export interface GitHubStepProps {
  // github is the check of the GitHub CLI's sign-in, which the setup shares with Finish.
  github: Query<GitHubSignIn>
}

// Problem is a sign-in that isn't ready: what is wrong, the command that fixes it, and
// what to do with it.
function Problem({ title, children, command }: { title: string; children: ReactNode; command: string }) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-warning/45 bg-warning/5 px-4 py-4">
      <p className="flex items-center gap-2 text-sm font-semibold">
        <AlertIcon className="size-4 shrink-0 text-warning" />
        {title}
      </p>
      <div className="text-sm text-muted">{children}</div>
      {command !== '' && <CommandLine command={command} className="max-w-md" />}
    </div>
  )
}

// SignedIn is a sign-in that is ready: the account, the scopes its token reports, whether
// it reads GitHub Projects, and a warning for a token that reports no scopes.
function SignedIn({ github }: { github: GitHubSignIn }) {
  const pictures = usePictures([github.login])

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-4 rounded-lg border border-success/45 bg-success/5 px-4 py-4">
        <PicturesContext value={pictures}>
          <PersonAvatar login={github.login} size="lg" />
        </PicturesContext>
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-1.5 text-sm font-semibold">
            <CircleCheckIcon className="size-4 text-success" />
            Signed in as @{github.login}
          </p>
          {github.name !== '' && <p className="truncate text-sm text-muted">{github.name}</p>}
          {github.scopes.length > 0 && (
            <ul aria-label="Scopes" className="mt-2 flex flex-wrap gap-1.5">
              {github.scopes.map((scope) => (
                <li key={scope} className={`${chipClass} font-mono`}>
                  {scope}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {github.unverified && (
        <p role="note" className="rounded-md border border-warning px-3 py-2 text-xs text-warning">
          GitHub didn’t report this token’s scopes, as for a fine-grained token, so {brand.name} can’t check it reads
          your private and organization repositories. Some items may be missing.
        </p>
      )}

      <div className="flex items-start gap-3 rounded-lg border border-border bg-surface px-4 py-3.5">
        <ProjectIcon className="mt-0.5 size-4 shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <p className="flex items-center justify-between gap-4 text-sm font-medium">
            Projects on issues
            {github.projects === 'granted' && <span className="text-xs font-medium text-success">On</span>}
            {github.projects !== 'granted' && <span className="text-xs font-medium text-muted">Optional</span>}
          </p>
          {github.projects === 'granted' ? (
            <p className="mt-0.5 text-xs text-muted">Issue cards show each GitHub project an issue is in.</p>
          ) : (
            <>
              <p className="mt-0.5 text-xs text-muted">
                To show the GitHub project each issue is in, give the GitHub CLI read-only access to your projects:
              </p>
              <CommandLine command={grantProjectsCommand} className="mt-2 max-w-sm" />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

// State shows where the sign-in stands, with the fix for each problem.
function State({ github }: { github: GitHubSignIn }) {
  switch (github.state) {
    case 'ready':
      return <SignedIn github={github} />
    case 'no_cli':
      return (
        <Problem title="The GitHub CLI isn’t installed" command={github.fix}>
          <p>
            Install it with Homebrew, or download it from{' '}
            <button
              type="button"
              onClick={() => {
                openExternal(cliPage)
              }}
              className="inline-flex items-center gap-0.5 font-medium text-accent hover:underline"
            >
              cli.github.com
              <ExternalIcon className="size-3" />
            </button>
            . {brand.name} looks for gh on your login shell’s PATH and in the usual folders, such as /opt/homebrew/bin.
          </p>
        </Problem>
      )
    case 'signed_out':
      return (
        <Problem title="The GitHub CLI isn’t signed in" command={github.fix}>
          <p>Run it in Terminal, then come back: this page checks again when you do.</p>
        </Problem>
      )
    case 'missing_scopes':
      return (
        <Problem title={`The token is missing ${github.missing.join(' and ')}`} command={github.fix}>
          <p>{github.message} Run this in Terminal, then come back.</p>
        </Problem>
      )
    case 'refused':
      return (
        <Problem title="GitHub refused the token" command={github.fix}>
          <p>{github.message}</p>
        </Problem>
      )
    case 'unreachable':
      return (
        <Problem title="Couldn’t reach GitHub" command="">
          <p>{github.message}</p>
        </Problem>
      )
  }
}

// GitHubStep connects GitHub: it checks the GitHub CLI's sign-in as the step opens, on
// Check again, and whenever the window comes back to the front, as from Terminal, until it
// is ready, and shows where it stands with the command that fixes each problem.
export function GitHubStep({ github }: GitHubStepProps) {
  const check = useEffectEvent(() => {
    if (github.data?.state !== 'ready' && !github.pending) {
      void github.refetch()
    }
  })

  // Entering the step, or coming back to the window, checks again until the sign-in is
  // ready.
  useEffect(() => {
    check()

    const onFocus = () => {
      check()
    }

    window.addEventListener('focus', onFocus)

    return () => {
      window.removeEventListener('focus', onFocus)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      {github.data === undefined ? (
        github.error === '' ? (
          <p role="status" className="flex items-center gap-2 text-sm text-muted">
            <Spinner />
            Checking your GitHub sign-in…
          </p>
        ) : (
          <Problem title="Couldn’t check the sign-in" command="">
            <p>{github.error}</p>
          </Problem>
        )
      ) : (
        <State github={github.data} />
      )}

      <div className="flex items-center justify-between gap-4">
        <p className="text-xs text-muted">
          {github.data !== undefined && github.data.cliVersion !== ''
            ? `GitHub CLI ${github.data.cliVersion}`
            : 'The GitHub CLI lends its token; nothing is stored.'}
        </p>
        <button
          type="button"
          disabled={github.pending}
          onClick={() => {
            void github.refetch()
          }}
          className={buttonClass}
        >
          <RefreshIcon className={`size-3.5 text-muted ${github.pending ? 'motion-safe:animate-spin' : ''}`} />
          {github.pending ? 'Checking…' : 'Check again'}
        </button>
      </div>
    </div>
  )
}
