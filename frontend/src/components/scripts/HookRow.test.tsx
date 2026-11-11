import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { HookRepo } from '../../platform'
import { at } from '../../test/clock'
import { hookRepo, hooksList } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { HookRow } from './HookRow'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// row is a fixture's repository by its name.
function row(name: string): HookRepo {
  const found = hooksList().repos.find((r) => r.name === name)

  if (found === undefined) {
    throw new Error(`no row ${name}`)
  }

  return found
}

interface RowOptions {
  version?: string
  watch?: number | null
}

// renderRow shows repo as the page lists it, with runs going in the app.
function renderRow(repo: HookRepo, { version = '1.12.3', watch = null }: RowOptions = {}) {
  const onOpenRun = vi.fn<(id: number) => void>()

  render(
    <ul>
      <HookRow repo={repo} version={version} watch={watch} runsIn="app" onOpenRun={onOpenRun} />
    </ul>,
    { wrapper: at() },
  )

  return { onOpenRun, item: within(screen.getByRole('listitem')) }
}

// choose opens the menu called label and chooses the item called name.
async function choose(label: string, name: string | RegExp) {
  await userEvent.click(screen.getByRole('button', { name: label }))
  await userEvent.click(screen.getByRole('menuitem', { name }))
}

describe('HookRow', () => {
  it('shows a ready repository with how its last checks went, which opens their run', async () => {
    const { onOpenRun, item } = renderRow(hookRepo())

    expect(item.getByText('acme-org/web')).toBeInTheDocument()
    expect(item.getByText('Installed')).toBeInTheDocument()
    expect(item.getByText('Done:')).toHaveClass('sr-only')

    const last = item.getByRole('button', { name: /checks passed 2h ago/ })

    expect(last).toHaveAttribute('title', 'All 5 checks passed on 41 files in 38 s.')
    await userEvent.click(last)
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(21)
    expect(item.queryByRole('button', { name: /the hook in/ })).not.toBeInTheDocument()
  })

  it('says checks are running, with no time while they run', () => {
    const { item } = renderRow(hookRepo({ lastChecks: { id: 4, status: 'running', summary: '', finishedAt: '' } }))

    expect(item.getByRole('button', { name: '· checks running' })).toBeInTheDocument()
  })

  it('runs the checks on every file from Run checks, and opens the run', async () => {
    const { onOpenRun } = renderRow(hookRepo())

    await choose('Run checks in acme-org/web', 'Run checks on all files')

    expect(platform.runChecks).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      scope: 'all',
      check: '',
      runsIn: 'app',
    })
    await waitFor(() => {
      expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(21)
    })
  })

  it('runs the checks on the staged files', async () => {
    renderRow(hookRepo())

    await choose('Run checks in acme-org/web', 'Run checks on staged files')

    expect(platform.runChecks).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      scope: 'staged',
      check: '',
      runsIn: 'app',
    })
  })

  it('runs one check, marking those off in the repository, under the note on which fix files', async () => {
    renderRow(hookRepo())

    await userEvent.click(screen.getByRole('button', { name: 'Run checks in acme-org/web' }))

    const one = screen.getByRole('menuitem', { name: 'Run one check' })

    expect(one).toHaveAccessibleDescription('eof, fumpt, and whitespace fix files. whitespace also stages its fixes.')
    await userEvent.click(one)

    const checks = within(screen.getByRole('menu', { name: 'Run one check' }))

    expect(checks.getAllByRole('menuitem').map((i) => i.textContent)).toEqual([
      'eof',
      'fumpt',
      'gitleaks (off)',
      'lint',
      'mod-tidy',
      'whitespace',
    ])

    await userEvent.click(checks.getByRole('menuitem', { name: 'gitleaks (off)' }))
    expect(platform.runChecks).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      scope: 'one',
      check: 'gitleaks',
      runsIn: 'app',
    })
  })

  it('installs at once where there is no hook, and opens the run', async () => {
    const { onOpenRun, item } = renderRow(row('acme-org/api'))

    expect(item.getByText('No hook yet')).toBeInTheDocument()
    expect(item.getByText('Problem:')).toBeInTheDocument()

    const install = item.getByRole('button', { name: 'Install the hook in acme-org/api' })

    expect(install).toHaveTextContent(/^Install$/)
    await userEvent.click(install)

    expect(platform.installHook).toHaveBeenCalledExactlyOnceWith({ dir: '~/projects/api', confirm: '', runsIn: 'app' })
    await waitFor(() => {
      expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(11)
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('updates an outdated hook at once', async () => {
    const { item } = renderRow(row('acme-org/cli'))

    expect(item.getByText('Outdated hook')).toBeInTheDocument()
    await userEvent.click(item.getByRole('button', { name: 'Update the hook in acme-org/cli' }))

    expect(platform.updateHook).toHaveBeenCalledExactlyOnceWith({ dir: '~/projects/cli', confirm: '', runsIn: 'app' })
  })

  it('reinstalls a hook that isn’t executable at once', async () => {
    const { item } = renderRow(hookRepo({ group: 'attention', state: 'notExecutable', action: 'reinstall' }))

    expect(item.getByText('The hook isn’t executable')).toBeInTheDocument()
    await userEvent.click(item.getByRole('button', { name: 'Reinstall the hook in acme-org/web' }))

    expect(platform.reinstallHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      confirm: '',
      runsIn: 'app',
    })
  })

  it('asks before replacing another tool’s hook, naming where it goes', async () => {
    const { onOpenRun, item } = renderRow(row('acme-org/sdk'))

    expect(item.getByText('Another tool’s hook is there (husky)')).toBeInTheDocument()
    await userEvent.click(item.getByRole('button', { name: 'Replace the hook in acme-org/sdk…' }))

    let dialog = screen.getByRole('dialog', { name: 'Replace husky’s hook?' })

    expect(dialog).toHaveAccessibleDescription(
      'Replace husky’s pre-commit hook in acme-org/sdk? go-pre-commit moves it to .git/hooks/pre-commit.backup.<number>, and puts it back if you uninstall.',
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.replaceHook).not.toHaveBeenCalled()

    await userEvent.click(item.getByRole('button', { name: 'Replace the hook in acme-org/sdk…' }))
    dialog = screen.getByRole('dialog', { name: 'Replace husky’s hook?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Replace husky’s hook' }))

    expect(platform.replaceHook).toHaveBeenCalledExactlyOnceWith({ dir: '~/projects/sdk', confirm: '', runsIn: 'app' })
    await waitFor(() => {
      expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(11)
    })
  })

  it('asks before installing into a shared hooks folder, and confirms that folder', async () => {
    const { item } = renderRow(row('acme-org/ops'))

    expect(item.getByText('Shared hooks folder ~/.githooks (6 repos)')).toBeInTheDocument()
    await userEvent.click(item.getByRole('button', { name: 'Install the hook in acme-org/ops…' }))

    const dialog = screen.getByRole('dialog', { name: 'Install the hook in a shared folder?' })

    expect(dialog).toHaveAccessibleDescription(
      '~/.githooks is shared by 6 of your repositories, and by any other repository that uses it. Installing here installs go-pre-commit’s hook for all of them.',
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Install for all of them' }))

    expect(platform.installHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/ops',
      confirm: '~/.githooks',
      runsIn: 'app',
    })
  })

  it('reinstalls go-pre-commit’s own hook from its menu', async () => {
    const { onOpenRun } = renderRow(hookRepo())

    await choose('More for acme-org/web', 'Reinstall the hook')

    expect(platform.reinstallHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      confirm: '',
      runsIn: 'app',
    })
    await waitFor(() => {
      expect(onOpenRun).toHaveBeenCalledWith(11)
    })
  })

  it('asks before uninstalling, saying what comes back', async () => {
    renderRow(hookRepo({ alsoInstalled: ['commit-msg'], backups: ['pre-commit.backup.64138'] }))

    expect(screen.getByText('Also installed: commit-msg')).toBeInTheDocument()
    await choose('More for acme-org/web', 'Uninstall the hook…')

    const dialog = screen.getByRole('dialog', { name: 'Uninstall the hook from acme-org/web?' })

    expect(dialog).toHaveAccessibleDescription(
      'go-pre-commit removes its pre-commit and commit-msg hooks from acme-org/web. It puts back the hook it set aside, pre-commit.backup.64138.',
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Uninstall the hook' }))

    expect(platform.uninstallHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      confirm: '',
      runsIn: 'app',
    })
  })

  it('asks before uninstalling from a shared hooks folder, and confirms that folder', async () => {
    renderRow(hookRepo({ shared: true, hooksDir: '~/.githooks', sharedWith: 3 }))

    await choose('More for acme-org/web', 'Uninstall the hook…')

    const dialog = screen.getByRole('dialog', { name: 'Uninstall the hook from acme-org/web?' })

    expect(dialog).toHaveAccessibleDescription(
      'go-pre-commit removes its pre-commit hook from acme-org/web. ~/.githooks is shared by 3 of your repositories, and by any other repository that uses it. Uninstalling here uninstalls go-pre-commit’s hooks for all of them.',
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Uninstall the hook' }))

    expect(platform.uninstallHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/web',
      confirm: '~/.githooks',
      runsIn: 'app',
    })
  })

  it('offers no Reinstall or Uninstall where the hook isn’t go-pre-commit’s', async () => {
    renderRow(row('acme-org/sdk'))

    await userEvent.click(screen.getByRole('button', { name: 'More for acme-org/sdk' }))

    expect(screen.getByRole('menuitem', { name: 'Reinstall the hook' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Uninstall the hook…' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Run checks on all files' })).toBeEnabled()
  })

  it('opens the config in Finder and the terminal in the clone, and says why when it can’t', async () => {
    const { item } = renderRow(hookRepo())

    await choose('More for acme-org/web', 'Open the config')
    expect(platform.showHooksConfig).toHaveBeenCalledExactlyOnceWith('~/projects/web')

    await choose('More for acme-org/web', 'Open in terminal')
    expect(platform.openHooksTerminal).toHaveBeenCalledExactlyOnceWith('~/projects/web')

    platform.openHooksTerminal.mockRejectedValue(new Error('Your terminal can’t be opened here.'))
    await choose('More for acme-org/web', 'Open in terminal')
    expect(await item.findByRole('alert')).toHaveTextContent('Your terminal can’t be opened here.')

    // A run that fails to start says so in its place.
    platform.runChecks.mockRejectedValue(new Error('A release is using acme-org/web right now.'))
    await choose('More for acme-org/web', 'Run checks on all files')
    expect(await item.findByRole('alert')).toHaveTextContent('A release is using acme-org/web right now.')
  })

  it('says why a run didn’t start, and opens nothing', async () => {
    platform.installHook.mockRejectedValue(new Error('go-pre-commit is turned off in Settings → Integrations.'))
    const { onOpenRun, item } = renderRow(row('acme-org/api'))

    await userEvent.click(item.getByRole('button', { name: 'Install the hook in acme-org/api' }))

    expect(await item.findByRole('alert')).toHaveTextContent('go-pre-commit is turned off in Settings → Integrations.')
    expect(onOpenRun).not.toHaveBeenCalled()
  })

  it('holds its buttons while a run it started is starting', async () => {
    platform.installHook.mockReturnValue(new Promise<number>(() => undefined))
    const { item } = renderRow(row('acme-org/api'))

    const install = item.getByRole('button', { name: 'Install the hook in acme-org/api' })

    await userEvent.click(install)
    expect(install).toBeDisabled()
  })

  it('says what holds a busy clone, with Watch for its run, and holds its buttons', async () => {
    const { onOpenRun, item } = renderRow(
      { ...row('acme-org/api'), busy: 'A release is using acme-org/api right now.' },
      { watch: 9 },
    )

    expect(item.getByText(/A release is using acme-org\/api right now\./)).toBeInTheDocument()
    expect(item.getByRole('button', { name: 'Install the hook in acme-org/api' })).toBeDisabled()

    await userEvent.click(item.getByRole('button', { name: 'Watch the run in acme-org/api' }))
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(9)

    await userEvent.click(screen.getByRole('button', { name: 'More for acme-org/api' }))
    expect(screen.getByRole('menuitem', { name: 'Run checks on all files' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Open in terminal' })).toBeEnabled()
  })

  it('offers no Watch when no run is listed for what holds the clone', () => {
    const { item } = renderRow({ ...hookRepo(), busy: 'A worktree is being made in acme-org/web.' })

    expect(item.getByText('A worktree is being made in acme-org/web.')).toBeInTheDocument()
    expect(item.queryByRole('button', { name: /Watch/ })).not.toBeInTheDocument()
  })

  it('notes the go-pre-commit the repository’s CI pins when it isn’t the one in use', () => {
    const { item } = renderRow(hookRepo({ ciVersion: '1.14.0' }))

    expect(
      item.getByText('This repository’s CI uses go-pre-commit v1.14.0 (GO_PRE_COMMIT_VERSION); you have 1.12.3.'),
    ).toBeInTheDocument()
  })

  it('says why the hooks couldn’t be read', () => {
    const { item } = renderRow(
      hookRepo({
        group: 'attention',
        state: 'unreadable',
        error: 'Couldn’t read where git runs this clone’s hooks.',
        lastChecks: { id: 0, status: 'succeeded', summary: '', finishedAt: '' },
      }),
    )

    expect(item.getByText('Couldn’t read the hooks')).toBeInTheDocument()
    expect(item.getByText('Couldn’t read where git runs this clone’s hooks.')).toBeInTheDocument()
    expect(item.queryByRole('button', { name: /checks/ })).not.toBeInTheDocument()
  })

  it('says a repository turned off is turned on and off in its config, with no button', () => {
    const { item } = renderRow(row('scratch'))

    expect(item.getByText('Turned on and off in .github/env/')).toBeInTheDocument()
    expect(item.queryByRole('button', { name: /the hook in/ })).not.toBeInTheDocument()
    expect(item.queryByRole('button', { name: /Run checks in/ })).not.toBeInTheDocument()
  })
})
