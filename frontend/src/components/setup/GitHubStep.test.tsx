import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { grantProjectsCommand } from '../../lib/projects'
import type { GitHubSignIn } from '../../platform'
import { githubSignIn } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { queryOf } from '../../test/query'
import { GitHubStep } from './GitHubStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// command is the command line a problem offers to copy.
function command(): string {
  return screen.getByRole('button', { name: 'Copy the command' }).parentElement?.textContent ?? ''
}

describe('GitHubStep', () => {
  it('shows the account a ready sign-in belongs to, its scopes, and its projects', () => {
    platform.issueAvatars.mockResolvedValue({})
    render(<GitHubStep github={queryOf<GitHubSignIn>(githubSignIn())} />)

    expect(screen.getByText('Signed in as @octo-user')).toBeInTheDocument()
    expect(screen.getByText('Octo User')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Scopes' })).toHaveTextContent('reporead:orgread:project')
    expect(screen.getByText('On')).toBeInTheDocument()
    expect(screen.getByText('GitHub CLI 2.83.1')).toBeInTheDocument()
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })

  it('offers projects to a token without them, and warns of one that reports no scopes', () => {
    render(
      <GitHubStep github={queryOf(githubSignIn({ name: '', scopes: [], unverified: true, projects: 'unverified' }))} />,
    )

    expect(screen.getByRole('note')).toHaveTextContent('GitHub didn’t report this token’s scopes')
    expect(screen.getByText('Optional')).toBeInTheDocument()
    expect(command()).toContain(grantProjectsCommand)
    expect(screen.queryByRole('list', { name: 'Scopes' })).not.toBeInTheDocument()
  })

  it.each<[GitHubSignIn['state'], string, string]>([
    ['no_cli', 'The GitHub CLI isn’t installed', 'brew install gh'],
    ['signed_out', 'The GitHub CLI isn’t signed in', 'gh auth login'],
    ['refused', 'GitHub refused the token', 'gh auth login'],
  ])('offers the fix for %s', (state, title, fix) => {
    render(<GitHubStep github={queryOf(githubSignIn({ state, login: '', fix, message: 'The token was revoked.' }))} />)

    expect(screen.getByText(title)).toBeInTheDocument()
    expect(command()).toContain(fix)
  })

  it('names the scopes a token lacks', () => {
    render(
      <GitHubStep
        github={queryOf(
          githubSignIn({
            state: 'missing_scopes',
            missing: ['repo', 'read:org'],
            fix: 'gh auth refresh -s repo,read:org',
            message: 'The token is missing the repo and read:org scope.',
          }),
        )}
      />,
    )

    expect(screen.getByText('The token is missing repo and read:org')).toBeInTheDocument()
    expect(command()).toContain('gh auth refresh -s repo,read:org')
  })

  it('says GitHub couldn’t be reached, with no command', () => {
    render(<GitHubStep github={queryOf(githubSignIn({ state: 'unreachable', message: 'No route to host.' }))} />)

    expect(screen.getByText('Couldn’t reach GitHub')).toBeInTheDocument()
    expect(screen.getByText('No route to host.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Copy the command' })).not.toBeInTheDocument()
  })

  it('opens where the GitHub CLI is downloaded', async () => {
    render(<GitHubStep github={queryOf(githubSignIn({ state: 'no_cli', fix: 'brew install gh' }))} />)

    await userEvent.click(screen.getByRole('button', { name: 'cli.github.com' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://cli.github.com')
  })

  it('checks while the sign-in loads, and says why a check failed', () => {
    const { rerender } = render(<GitHubStep github={queryOf<GitHubSignIn>(undefined, { pending: true })} />)

    expect(screen.getByRole('status')).toHaveTextContent('Checking your GitHub sign-in…')
    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()

    rerender(<GitHubStep github={queryOf<GitHubSignIn>(undefined, { error: 'the backend is gone' })} />)

    expect(screen.getByText('Couldn’t check the sign-in')).toBeInTheDocument()
    expect(screen.getByText('the backend is gone')).toBeInTheDocument()
  })

  it('checks again on entry, on Check again, and when the window comes back, until it is ready', async () => {
    const github = queryOf(githubSignIn({ state: 'signed_out', fix: 'gh auth login' }))
    const { rerender } = render(<GitHubStep github={github} />)

    expect(github.refetch).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(github.refetch).toHaveBeenCalledTimes(2)

    act(() => {
      window.dispatchEvent(new Event('focus'))
    })
    expect(github.refetch).toHaveBeenCalledTimes(3)

    const ready = queryOf(githubSignIn())
    rerender(<GitHubStep github={ready} />)

    act(() => {
      window.dispatchEvent(new Event('focus'))
    })
    expect(ready.refetch).not.toHaveBeenCalled()
  })
})
