import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { SettingsSection } from '../../lib/settingsIndex'
import { useHooks } from '../../lib/useHooks'
import type { HookInstallAllResult, IntegrationTool, ScriptRun } from '../../platform'
import { at } from '../../test/clock'
import { goPreCommitTool, hookInstallAll, hookRepo, hooksList, scriptRun, toolCopy } from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { HooksPage } from './HooksPage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface PageProps {
  tool: IntegrationTool | undefined
  runs?: readonly ScriptRun[] | null
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
}

// Page is the page as the tab shows it, reading the list itself.
function Page({ runs = [], ...props }: PageProps) {
  return <HooksPage hooks={useHooks(true, runs)} runs={runs} runsIn="app" {...props} />
}

function renderPage(props: Partial<Omit<PageProps, 'onOpenRun' | 'onOpenSettings'>> = {}) {
  const handlers = {
    onOpenRun: vi.fn<(id: number) => void>(),
    onOpenSettings: vi.fn<(section: SettingsSection) => void>(),
  }

  render(<Page tool={goPreCommitTool()} {...handlers} {...props} />, { wrapper: at() })

  return handlers
}

// group is a group's heading button, by its title.
function group(title: RegExp) {
  return screen.getByRole('button', { name: title })
}

// rows lists the names of a group's rows.
function rows(title: string): string[] {
  return within(screen.getByRole('list', { name: title }))
    .getAllByRole('listitem')
    .map((li) => li.querySelector('.font-medium')?.textContent ?? '')
}

describe('HooksPage', () => {
  it('lists the repositories needing attention and the ready ones open, and the rest collapsed', async () => {
    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('Scanning your clones…')
    await screen.findByRole('list', { name: 'Needs attention' })

    expect(rows('Needs attention')).toEqual(['acme-org/api', 'acme-org/cli', 'acme-org/ops', 'acme-org/sdk'])
    expect(rows('Ready')).toEqual(['acme-org/web'])
    expect(group(/^Needs attention · 4/)).toHaveAttribute('aria-expanded', 'true')
    expect(group(/^Ready · 1/)).toHaveAttribute('aria-expanded', 'true')
    expect(group(/^Turned off in the repository · 1/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^Not set up · 2\s*no \.github\/env\/ or \.github\/\.env\.base$/)).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    expect(screen.getByRole('heading', { name: 'Hooks', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('go-pre-commit 1.12.3')).toBeInTheDocument()
    expect(screen.getByText('1m ago')).toHaveAttribute('dateTime', '2026-06-15T11:59:00Z')
    expect(
      screen.getByText(
        'Agent sessions’ worktrees share each clone’s hooks, so an agent’s commits run these checks too.',
      ),
    ).toBeInTheDocument()
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })

  it('opens the collapsed groups, and links the clones without a config to go-pre-commit’s README', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Needs attention' })
    await userEvent.click(group(/^Turned off in the repository/))
    await userEvent.click(group(/^Not set up/))

    expect(rows('Turned off in the repository')).toEqual(['scratch'])
    expect(within(screen.getByRole('list', { name: 'Not set up' })).getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('~/projects/notes')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Set up go-pre-commit' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/go-pre-commit#readme')

    await userEvent.click(group(/^Ready/))
    expect(screen.queryByRole('list', { name: 'Ready' })).not.toBeInTheDocument()
  })

  it('searches every group, open while it searches', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Needs attention' })
    const search = screen.getByRole('searchbox', { name: 'Search repositories' })

    await userEvent.type(search, 'SCRATCH')
    expect(rows('Turned off in the repository')).toEqual(['scratch'])
    expect(screen.queryByRole('list', { name: 'Needs attention' })).not.toBeInTheDocument()
    expect(group(/^Turned off in the repository · 1/)).toBeDisabled()

    await userEvent.clear(search)
    await userEvent.type(search, 'notes')
    expect(within(screen.getByRole('list', { name: 'Not set up' })).getAllByRole('listitem')).toHaveLength(1)
    expect(group(/^Not set up · 1/)).toBeDisabled()

    await userEvent.clear(search)
    await userEvent.type(search, 'nothing-like-it')
    expect(screen.getByText('No repository matching “nothing-like-it”.')).toBeInTheDocument()
  })

  it('installs and updates every repository it can at once, a line for each, each opening its run', async () => {
    let finish: (result: HookInstallAllResult) => void = () => undefined
    platform.installAllHooks.mockReturnValue(
      new Promise<HookInstallAllResult>((resolve) => {
        finish = resolve
      }),
    )
    const { onOpenRun } = renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Install and update all (2)' }))

    expect(platform.installAllHooks).toHaveBeenCalledExactlyOnceWith(['~/projects/api', '~/projects/cli'])
    expect(screen.getByRole('button', { name: 'Install and update all (2)' })).toBeDisabled()

    const card = within(screen.getByRole('region', { name: 'Install and update all' }))

    expect(card.getByRole('status')).toHaveTextContent('Installing and updating the hooks, one clone at a time…')
    expect(card.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()

    act(() => {
      finish(
        hookInstallAll({
          lines: [
            ...hookInstallAll().lines,
            {
              name: 'acme-org/gone',
              dir: '~/projects/gone',
              action: '',
              runId: 0,
              outcome: 'skipped',
              message: 'It has no go-pre-commit config.',
            },
          ],
          message: '1 installed, 1 updated, 1 skipped.',
        }),
      )
    })

    expect(await card.findByText('1 installed, 1 updated, 1 skipped.')).toHaveAttribute('role', 'status')

    const lines = within(card.getByRole('list', { name: 'Each clone' })).getAllByRole('listitem')

    expect(lines.map((l) => l.textContent)).toEqual([
      'Done: acme-org/api Installed the pre-commit hook in acme-org/api.Open the run',
      'Done: acme-org/cli Updated the pre-commit hook in acme-org/cli.Open the run',
      'Skipped: acme-org/gone It has no go-pre-commit config.',
    ])

    await userEvent.click(card.getByRole('button', { name: 'Open the run in acme-org/cli' }))
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(12)

    await userEvent.click(card.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('region', { name: 'Install and update all' })).not.toBeInTheDocument()
  })

  it('says why Install and update all failed, until dismissed', async () => {
    platform.installAllHooks.mockRejectedValue(new Error('go-pre-commit isn’t installed.'))
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Install and update all (2)' }))

    const card = within(await screen.findByRole('region', { name: 'Install and update all' }))

    expect(await card.findByRole('alert')).toHaveTextContent(
      'Couldn’t install and update the hooks: go-pre-commit isn’t installed.',
    )

    await userEvent.click(card.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('region', { name: 'Install and update all' })).not.toBeInTheDocument()
  })

  it('says when nothing needs attention, with nothing to install', async () => {
    platform.listHooks.mockResolvedValue(hooksList({ repos: [hookRepo()] }))
    renderPage()

    expect(await screen.findByText('Nothing needs attention.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Install and update all/ })).not.toBeInTheDocument()
  })

  it('says when no clone has a config, linking to how to set one up', async () => {
    platform.listHooks.mockResolvedValue(hooksList({ repos: [] }))
    renderPage()

    expect(await screen.findByText('None of your clones has a go-pre-commit config.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Needs attention/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/Agent sessions’ worktrees/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Set up go-pre-commit' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/go-pre-commit#readme')
    expect(group(/^Not set up · 2/)).toBeInTheDocument()
  })

  it('warns that the hook can’t find go-pre-commit, with the line that fixes it', async () => {
    platform.listHooks.mockResolvedValue(hooksList({ pathWarning: { show: true, dir: '~/.local/bin' } }))
    renderPage()

    expect(
      await screen.findByText(/isn’t on your shell’s PATH\. Hooks can’t find go-pre-commit, so commits fail\./),
    ).toHaveTextContent('~/.local/bin isn’t on your shell’s PATH.')

    const fix = screen.getByRole('button', { name: 'How to fix' })

    expect(fix).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(fix)

    expect(fix).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText(/put this line in ~\/\.zprofile/)).toBeInTheDocument()
    expect(screen.getByText('export PATH="$HOME/.local/bin:$PATH"')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Copy the command' }))
    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('export PATH="$HOME/.local/bin:$PATH"')

    await userEvent.click(fix)
    expect(screen.queryByText(/put this line in ~\/\.zprofile/)).not.toBeInTheDocument()
  })

  it('says the checks install their own tools, unfolded until the first checks ran', async () => {
    platform.listHooks.mockResolvedValue(hooksList({ toolNote: { show: true, open: true } }))
    renderPage()

    expect(
      await screen.findByText(
        /The checks install gofumpt, golangci-lint, and gitleaks the first time they need them\./,
      ),
    ).toBeInTheDocument()

    const more = screen.getByRole('button', { name: 'More' })
    const details = new RegExp(
      `without checking them\\. ${brand.name} doesn’t run these installs; go-pre-commit does\\.`,
    )

    expect(more).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText(details)).toBeInTheDocument()

    await userEvent.click(more)
    expect(screen.queryByText(details)).not.toBeInTheDocument()
  })

  it('keeps the note on the tools under More once checks have run', async () => {
    platform.listHooks.mockResolvedValue(hooksList({ toolNote: { show: true, open: false } }))
    renderPage()

    const more = await screen.findByRole('button', { name: 'More' })

    expect(more).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText(/without checking them/)).not.toBeInTheDocument()

    await userEvent.click(more)
    expect(screen.getByText(/without checking them/)).toBeInTheDocument()
  })

  it('keeps the last scan when one fails, saying how old it is', async () => {
    platform.listHooks.mockResolvedValue(
      hooksList({ checkedAt: '2026-06-15T11:46:00Z', error: 'Couldn’t list the clones in ~/projects.' }),
    )
    renderPage()

    const note = await screen.findByRole('note')

    expect(note).toHaveTextContent('Couldn’t scan the clones · 14m old')
    expect(note).toHaveAttribute('title', 'Couldn’t list the clones in ~/projects.')
    expect(screen.getByRole('list', { name: 'Needs attention' })).toBeInTheDocument()
  })

  it('says why the clones couldn’t be listed at all', async () => {
    platform.listHooks.mockRejectedValue(new Error('the clone root is gone'))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t list the clones: the clone root is gone')
  })

  it('scans every clone again on request, and says why when that fails', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Needs attention' })
    platform.listHooks.mockResolvedValue(hooksList({ repos: [hookRepo()], checkedAt: '' }))
    await userEvent.click(screen.getByRole('button', { name: 'Scan again' }))

    expect(platform.listHooks).toHaveBeenLastCalledWith(true)
    expect(await screen.findByText('Nothing needs attention.')).toBeInTheDocument()
    expect(screen.queryByText(/Scanned/)).not.toBeInTheDocument()

    platform.listHooks.mockRejectedValue(new Error('the clone root is gone'))
    await userEvent.click(screen.getByRole('button', { name: 'Scan again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t scan the clones again: the clone root is gone')
  })

  it('reads the list again when the shell says a clone was scanned', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Needs attention' })
    platform.listHooks.mockResolvedValue(hooksList({ repos: [hookRepo()] }))
    act(() => {
      emit('hooks:updated')
    })

    expect(await screen.findByText('Nothing needs attention.')).toBeInTheDocument()
  })

  it('opens the run that holds a busy clone from Watch', async () => {
    platform.listHooks.mockResolvedValue(
      hooksList({ repos: [hookRepo({ busy: 'A release is using acme-org/web right now.' })] }),
    )
    const { onOpenRun } = renderPage({ runs: [scriptRun({ id: 9, status: 'running', dir: '~/projects/web' })] })

    await userEvent.click(await screen.findByRole('button', { name: 'Watch the run in acme-org/web' }))
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(9)
  })

  it('opens the run a row starts', async () => {
    const { onOpenRun } = renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Install the hook in acme-org/api' }))

    expect(platform.installHook).toHaveBeenCalledExactlyOnceWith({ dir: '~/projects/api', confirm: '', runsIn: 'app' })
    await vi.waitFor(() => {
      expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(11)
    })
  })

  it.each<[string, Partial<IntegrationTool> | undefined, string, string]>([
    ['missing', { state: 'missing' }, 'go-pre-commit isn’t on this Mac.', 'Install go-pre-commit in Settings'],
    [
      'too old',
      { state: 'tooOld', minVersion: '1.12.0', copy: toolCopy({ version: { raw: '1.10.0', commit: '', built: '' } }) },
      'go-pre-commit 1.10.0 is older than 1.12.0, which Hooks needs.',
      'Update go-pre-commit in Settings',
    ],
    [
      'won’t run',
      { state: 'wontRun', copy: toolCopy({ displayPath: '~/.local/bin/go-pre-commit', error: 'bad CPU type' }) },
      '~/.local/bin/go-pre-commit is there but won’t run: bad CPU type',
      'Open Settings',
    ],
    ['not listed yet', undefined, 'go-pre-commit can’t be used yet.', 'Open Settings'],
  ])('says why go-pre-commit can’t be used when it is %s, with the fix', async (_, change, text, fix) => {
    const { onOpenSettings } = renderPage({ tool: change === undefined ? undefined : goPreCommitTool(change) })

    expect(screen.getByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Needs attention' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Scan again' })).toBeDisabled()
    expect(screen.queryByText(/^go-pre-commit \d+\.\d+\.\d+$/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: fix }))
    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
  })
})
