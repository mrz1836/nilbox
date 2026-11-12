import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { SettingsSection } from '../../lib/settingsIndex'
import type { OnSection, StoredSections } from '../../lib/useSections'
import type { RepoScripts, RunsIn, ScriptSection, ScriptStart, TerminalID } from '../../platform'
import {
  branchStatus,
  bumpInput,
  magexTarget,
  repoScripts,
  scriptClone,
  scriptEntry,
  scriptQuestion,
  scriptRun,
  scriptSection,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { RepoScriptsPage } from './RepoScriptsPage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface PageProps {
  runsIn?: RunsIn
  terminal?: TerminalID | null
  release?: boolean
  // showRuns opens a run beside the scripts as it starts; the tests of the run panel turn
  // it on, as the user can in Settings.
  showRuns?: boolean
  sections?: StoredSections
}

// renderPage renders the page as the tab shows it, remembering the clone chosen, the run
// followed, the sections opened or folded, and whether runs show beside the scripts, with
// Release on unless release is false.
function renderPage({
  runsIn = 'app',
  terminal = 'iterm2',
  release = true,
  showRuns: showAtFirst = true,
  sections: storedAtFirst = {},
}: PageProps = {}) {
  const handlers = {
    onDir: vi.fn<(dir: string) => void>(),
    onFollow: vi.fn<(id: number | null) => void>(),
    onOpenRelease: vi.fn<() => void>(),
    onOpenSettings: vi.fn<(section: SettingsSection) => void>(),
    onSection: vi.fn<OnSection>(),
    onShowRuns: vi.fn<(on: boolean) => void>(),
  }

  function Page() {
    const [dir, setDir] = useState('')
    const [followed, setFollowed] = useState<number | null>(null)
    const [showRuns, setShowRuns] = useState(showAtFirst)

    return (
      <RepoScriptsPage
        dir={dir}
        onDir={(chosen) => {
          handlers.onDir(chosen)
          setDir(chosen)
        }}
        followed={followed}
        onFollow={(id) => {
          handlers.onFollow(id)
          setFollowed(id)
        }}
        runsIn={runsIn}
        terminal={terminal}
        where="under ~/projects"
        onOpenRelease={release ? handlers.onOpenRelease : null}
        onOpenSettings={handlers.onOpenSettings}
        sections={storedAtFirst}
        onSection={handlers.onSection}
        showRuns={showRuns}
        onShowRuns={(on) => {
          handlers.onShowRuns(on)
          setShowRuns(on)
        }}
      />
    )
  }

  render(<Page />)

  return handlers
}

// list is a section's list of scripts, by its title, once it shows.
async function list(title: string) {
  return within(await screen.findByRole('list', { name: title }))
}

// row is a script's row, by its name, in a section's list.
async function row(title: string, name: string) {
  const item = (await list(title)).getByText(name, { exact: true }).closest('li')

  if (item === null) {
    throw new Error(`no row ${name}`)
  }

  return within(item)
}

// names lists the names of a section's scripts.
async function names(title: string): Promise<string[]> {
  return (await list(title)).getAllByRole('listitem').map((li) => li.querySelector('.font-medium')?.textContent ?? '')
}

// asks makes the next run held back by question, as the backend refuses a repository
// command the user hasn't confirmed.
function asks(question = scriptQuestion(), message = 'Confirm “Test” before it runs.') {
  platform.runScript.mockResolvedValueOnce({ started: false, question, message })
}

// withPage lists page for every clone.
function withPage(page: Partial<RepoScripts>) {
  platform.listRepoScripts.mockResolvedValue(repoScripts(page))
}

describe('RepoScriptsPage', () => {
  it('lists the clone’s sources in order, with its repository, folder, and branch', async () => {
    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('Listing your clones…')

    expect(await names('From t3.json')).toEqual(['Fix all', 'Test', 'Version: bump patch'])
    expect(await names('From .vscode/tasks.json')).toEqual([
      'magex: lint',
      'go: test file',
      'Release: bump',
      'Bench',
      'docs: watch',
    ])
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Yours · 0',
      'From t3.json · 3',
      'From .vscode/tasks.json · 5',
      'magex · 6',
    ])
    expect(screen.getByText('Save a command you run often with New script, and it shows here.')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Repository' })).toHaveValue('~/projects/sdk')
    expect(screen.getByText('acme-org/sdk')).toBeInTheDocument()
    expect(screen.getByText('~/projects/sdk')).toBeInTheDocument()
    expect(screen.getByText('master')).toBeInTheDocument()
    expect(screen.queryByText(/From branch/)).not.toBeInTheDocument()
    expect(platform.listRepoScripts).toHaveBeenCalledExactlyOnceWith('~/projects/sdk', false)

    const fix = await row('From t3.json', 'Fix all')
    expect(fix.getByText('magex format:fix && magex lint && magex test:race')).toBeInTheDocument()
    const lint = await row('From .vscode/tasks.json', 'magex: lint')
    expect(lint.getByText('Run comprehensive linting with golangci-lint')).toBeInTheDocument()
    expect(lint.getByText('Changed since you last ran it')).toBeInTheDocument()
    const watch = await row('From .vscode/tasks.json', 'docs: watch')
    expect(watch.getByText('Runs until you stop it.')).toBeInTheDocument()
    const bench = await row('From .vscode/tasks.json', 'Bench')
    expect(
      bench.getByText('Runs its dependencies one after another, where VS Code runs them side by side.'),
    ).toBeInTheDocument()
  })

  it('shows a clone on another branch, and one whose origin isn’t on GitHub', async () => {
    withPage({ repo: '', name: 'notes', dir: '~/projects/sdk', branch: '', fromBranch: '' })
    renderPage()

    await list('From t3.json')
    expect(screen.queryByText('acme-org/sdk')).not.toBeInTheDocument()
    expect(screen.queryByText('master')).not.toBeInTheDocument()

    withPage({ branch: 'feature-x', fromBranch: 'feature-x' })
    await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

    expect(await screen.findByText(/, not the default branch: a branch can change/)).toHaveTextContent(
      'From branch feature-x, not the default branch: a branch can change what the repository’s files say.',
    )
  })

  it('greys a script it can’t run, saying why, with nothing to run', async () => {
    renderPage()

    const greyed = await row('From .vscode/tasks.json', 'go: test file')

    expect(greyed.getByText(`Uses \${file}, which ${brand.name} can’t fill in.`)).toBeInTheDocument()
    expect(greyed.getByText('Can’t run:')).toBeInTheDocument()
    expect(greyed.queryByRole('button')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /^All magex commands/ }))
    const updater = await row('All magex commands', 'update:install')
    expect(updater.getByText(/This updates magex itself/)).toBeInTheDocument()
    expect(updater.queryByRole('button')).not.toBeInTheDocument()
  })

  it('points a version bump to Release, and runs it anyway from its menu', async () => {
    const { onOpenRelease } = renderPage()

    const bump = await row('From t3.json', 'Version: bump patch')

    expect(bump.getByText(/Release does this, with checks/)).toBeInTheDocument()
    expect(bump.queryByRole('button', { name: 'Run Version: bump patch' })).not.toBeInTheDocument()

    await userEvent.click(bump.getByRole('button', { name: 'Open Release' }))
    expect(onOpenRelease).toHaveBeenCalledOnce()

    asks(scriptQuestion({ ref: 't3:Version: bump patch', name: 'Version: bump patch', sha256: 'c0ffee02' }))
    await userEvent.click(bump.getByRole('button', { name: 'More for Version: bump patch' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway' }))

    expect(platform.runScript).toHaveBeenLastCalledWith({
      dir: '~/projects/sdk',
      ref: 't3:Version: bump patch',
      inputs: {},
      args: [],
      runsIn: '',
      confirmed: '',
      remember: false,
      anyway: true,
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Run' }))
    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ confirmed: 'c0ffee02', remember: true, anyway: true }),
    )

    await userEvent.click(bump.getByRole('button', { name: 'More for Version: bump patch' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway in iTerm2' }))
    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ runsIn: 'terminal', anyway: true, confirmed: '' }),
    )
  })

  it('asks a task’s inputs first, showing each choice with the value it fills in', async () => {
    const { onFollow } = renderPage()

    const bench = await row('From .vscode/tasks.json', 'Bench')
    await userEvent.click(bench.getByRole('button', { name: 'Run Bench' }))

    const form = within(screen.getByRole('dialog', { name: 'Run “Bench”' }))

    expect(form.getByRole('textbox', { name: 'Which benchmarks to run' })).toHaveValue('.')
    expect(form.getByRole('radio', { name: 'Once (1)' })).toBeChecked()
    expect(form.getByRole('radio', { name: 'Five times (5)' })).not.toBeChecked()
    expect(platform.runScript).not.toHaveBeenCalled()

    await userEvent.clear(form.getByRole('textbox', { name: 'Which benchmarks to run' }))
    await userEvent.type(form.getByRole('textbox', { name: 'Which benchmarks to run' }), 'Parse')
    await userEvent.click(form.getByRole('radio', { name: 'Five times (5)' }))
    asks(
      scriptQuestion({
        ref: 'tasks:Bench',
        name: 'Bench',
        source: 'tasks',
        command: 'go test -bench=Parse -count=5 ./...',
      }),
    )
    await userEvent.click(form.getByRole('button', { name: 'Run' }))

    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ ref: 'tasks:Bench', inputs: { pattern: 'Parse', count: '5' }, confirmed: '' }),
    )

    const question = within(
      await screen.findByRole('dialog', { name: 'Run “Bench” from acme-org/sdk’s .vscode/tasks.json?' }),
    )
    expect(question.getByText('go test -bench=Parse -count=5 ./...')).toBeInTheDocument()

    await userEvent.click(question.getByRole('button', { name: 'Run' }))
    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ ref: 'tasks:Bench', inputs: { pattern: 'Parse', count: '5' }, confirmed: 'c0ffee01' }),
    )
    expect(onFollow).toHaveBeenCalledExactlyOnceWith(31)
  })

  it('runs nothing when the form is cancelled', async () => {
    renderPage()

    const bump = await row('From .vscode/tasks.json', 'Release: bump')
    await userEvent.click(bump.getByRole('button', { name: 'More for Release: bump' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway' }))

    const form = within(screen.getByRole('dialog', { name: 'Run “Release: bump”' }))
    expect(form.getByRole('radio', { name: 'Patch (patch)' })).toBeChecked()
    expect(form.getByRole('radio', { name: 'major' })).toBeInTheDocument()

    await userEvent.click(form.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.runScript).not.toHaveBeenCalled()
  })

  it('asks before a repository command runs, then runs it with the hash confirmed, remembering it', async () => {
    const { onFollow } = renderPage()

    asks()
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))

    expect(platform.runScript).toHaveBeenLastCalledWith({
      dir: '~/projects/sdk',
      ref: 't3:Test',
      inputs: {},
      args: [],
      runsIn: '',
      confirmed: '',
      remember: false,
      anyway: false,
    })

    const dialog = within(await screen.findByRole('dialog', { name: 'Run “Test” from acme-org/sdk’s t3.json?' }))

    expect(dialog.getByText('This command comes from the repository, not from you:')).toBeInTheDocument()
    expect(dialog.getByText('magex test')).toBeInTheDocument()
    expect(
      dialog.getByText(
        (_, el) =>
          el?.textContent === 'It runs in ~/projects/sdk with your permissions, as if you typed it in a terminal.',
      ),
    ).toBeInTheDocument()
    expect(dialog.getByRole('checkbox', { name: 'Don’t ask again for this command in acme-org/sdk' })).toBeChecked()
    expect(dialog.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(onFollow).not.toHaveBeenCalled()

    await userEvent.click(dialog.getByRole('button', { name: 'Run' }))

    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ ref: 't3:Test', confirmed: 'c0ffee01', remember: true }),
    )
    expect(platform.runScript).toHaveBeenCalledTimes(2)
    expect(onFollow).toHaveBeenCalledExactlyOnceWith(31)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('runs it once without remembering when Don’t ask again is cleared, and nothing when cancelled', async () => {
    renderPage()

    asks()
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))
    let dialog = within(await screen.findByRole('dialog'))
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.runScript).toHaveBeenCalledOnce()

    asks()
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))
    dialog = within(await screen.findByRole('dialog'))
    await userEvent.click(dialog.getByRole('checkbox', { name: /Don’t ask again/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Run' }))

    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ confirmed: 'c0ffee01', remember: false }),
    )
  })

  it('shows a changed command’s old and new commands, under its heading', async () => {
    renderPage()

    asks(
      scriptQuestion({
        ref: 'tasks:magex: lint',
        name: 'magex: lint',
        source: 'tasks',
        command: 'magex lint',
        trust: 'changed',
        trusted: 'magex lint:fix',
        sha256: 'c0ffee10',
      }),
    )
    await userEvent.click(
      (await row('From .vscode/tasks.json', 'magex: lint')).getByRole('button', { name: 'Run magex: lint' }),
    )

    const dialog = within(
      await screen.findByRole('dialog', { name: 'Run “magex: lint” from acme-org/sdk’s .vscode/tasks.json?' }),
    )

    expect(dialog.getByRole('heading', { name: 'Changed since you last ran it' })).toBeInTheDocument()
    expect(dialog.getAllByRole('term').map((t) => t.textContent)).toEqual(['You ran', 'It now runs'])
    expect(dialog.getAllByRole('definition').map((d) => d.textContent)).toEqual(['magex lint:fix', 'magex lint'])
    expect(dialog.queryByText('This command comes from the repository, not from you:')).not.toBeInTheDocument()
  })

  it('asks for a custom magex target, saying it runs the repository’s own magefiles code', async () => {
    withPage({ fromBranch: 'feature-x', branch: 'feature-x' })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: /^This repository’s targets \(1\)/ }))
    asks(
      scriptQuestion({
        ref: 'magex:deploy:docs',
        name: 'deploy:docs',
        source: 'magex',
        command: 'magex deploy:docs',
        trustNote: 'This runs acme-org/sdk’s own magefiles code.',
        sha256: 'c0ffee20',
      }),
    )
    await userEvent.type(screen.getByRole('textbox', { name: 'Arguments' }), 'env=prod')
    await userEvent.click(
      (await row('This repository’s targets', 'deploy:docs')).getByRole('button', { name: 'Run deploy:docs' }),
    )

    // The repository's own targets take no arguments.
    expect(platform.runScript).toHaveBeenLastCalledWith(expect.objectContaining({ ref: 'magex:deploy:docs', args: [] }))

    const dialog = within(
      await screen.findByRole('dialog', { name: 'Run “deploy:docs” from acme-org/sdk’s magefiles?' }),
    )
    expect(dialog.getByText('This runs acme-org/sdk’s own magefiles code.')).toBeInTheDocument()
    expect(
      dialog.getByText((_, el) => el?.tagName === 'SPAN' && el.textContent.startsWith('From branch feature-x,')),
    ).toBeInTheDocument()
  })

  it('asks every time in a clone whose origin isn’t on GitHub', async () => {
    withPage({ repo: '', name: 'notes' })
    renderPage()

    asks()
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Run “Test” from notes’s t3.json?' }))

    expect(dialog.queryByRole('checkbox')).not.toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Run' }))
    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ confirmed: 'c0ffee01', remember: false }),
    )
  })

  it('asks once per click: a confirmed command held back again says why, without asking again', async () => {
    const { onFollow } = renderPage()

    asks()
    asks(
      scriptQuestion({ command: 'magex test -v', sha256: 'c0ffee99' }),
      '“Test” changed since you confirmed it. Look at it again before it runs.',
    )
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Run' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t run Test: “Test” changed since you confirmed it. Look at it again before it runs.',
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.runScript).toHaveBeenCalledTimes(2)
    expect(onFollow).not.toHaveBeenCalled()

    asks()
    asks(scriptQuestion(), '')
    await userEvent.click((await row('From t3.json', 'Test')).getByRole('button', { name: 'Run Test' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Run' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t run Test: It changed again as it was starting. Run it again to look at it.',
    )
  })

  it('says why a run didn’t start', async () => {
    platform.runScript.mockRejectedValueOnce(
      new Error('busy', { cause: { message: 'A release is using acme-org/sdk right now.' } }),
    )
    renderPage()

    await userEvent.click((await row('From t3.json', 'Fix all')).getByRole('button', { name: 'Run Fix all' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t run Fix all: A release is using acme-org/sdk right now.',
    )
  })

  it('holds the buttons while a run is being asked for', async () => {
    let finish: (start: ScriptStart) => void = () => undefined
    platform.runScript.mockReturnValueOnce(
      new Promise<ScriptStart>((resolve) => {
        finish = resolve
      }),
    )
    const { onFollow } = renderPage()

    const fix = await row('From t3.json', 'Fix all')
    await userEvent.click(fix.getByRole('button', { name: 'Run Fix all' }))

    expect(fix.getByRole('button', { name: 'Run Fix all' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Run magex lint' })).toBeDisabled()

    act(() => {
      finish({ started: true, id: 41 })
    })
    await waitFor(() => {
      expect(onFollow).toHaveBeenCalledExactlyOnceWith(41)
    })
  })

  it('runs a script in the other place from its menu', async () => {
    renderPage()

    const fix = await row('From t3.json', 'Fix all')
    await userEvent.click(fix.getByRole('button', { name: 'More for Fix all' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run in iTerm2' }))

    expect(platform.runScript).toHaveBeenLastCalledWith(
      expect.objectContaining({ ref: 't3:Fix all', runsIn: 'terminal' }),
    )
  })

  it('names the other place as runs go to the terminal, or before the settings load', async () => {
    renderPage({ runsIn: 'terminal' })

    await userEvent.click((await row('From t3.json', 'Fix all')).getByRole('button', { name: 'More for Fix all' }))
    await userEvent.click(screen.getByRole('menuitem', { name: `Run in ${brand.name}` }))

    expect(platform.runScript).toHaveBeenLastCalledWith(expect.objectContaining({ runsIn: 'app' }))
  })

  it('names the terminal as yours until the settings load', async () => {
    renderPage({ terminal: null })

    await userEvent.click((await row('From t3.json', 'Fix all')).getByRole('button', { name: 'More for Fix all' }))

    expect(screen.getByRole('menuitem', { name: 'Run in your terminal' })).toBeInTheDocument()
  })

  it('points to Release without a way there while Release is off', async () => {
    renderPage({ release: false })

    const bump = await row('From t3.json', 'Version: bump patch')

    expect(bump.getByText('Release does this, with checks')).toBeInTheDocument()
    expect(bump.queryByRole('button', { name: 'Open Release' })).not.toBeInTheDocument()
  })

  describe('magex', () => {
    it('runs a common target from its chip, with the arguments given, and says what magex targets run', async () => {
      renderPage()

      const common = await list('Common magex targets')

      expect(common.getAllByRole('button').map((b) => b.textContent)).toEqual(['format:fix', 'lint', 'test'])
      expect(
        screen.getByText(
          'magex targets run the repository’s own code and its .github/env settings, as any build does.',
        ),
      ).toBeInTheDocument()

      await userEvent.type(screen.getByRole('textbox', { name: 'Arguments' }), '  bump=minor   push ')
      await userEvent.click(common.getByRole('button', { name: 'Run magex lint' }))

      expect(platform.runScript).toHaveBeenLastCalledWith({
        dir: '~/projects/sdk',
        ref: 'magex:lint',
        inputs: {},
        args: ['bump=minor', 'push'],
        runsIn: '',
        confirmed: '',
        remember: false,
        anyway: false,
      })
    })

    it('points a chip that bumps a version to Release, and runs it anyway from its menu', async () => {
      withPage({
        sections: [
          scriptSection({
            source: 'magex',
            title: 'magex',
            common: ['magex:lint', 'magex:version:bump'],
            entries: [
              magexTarget('lint', 'Run the linters'),
              magexTarget('version:bump', 'Bump the version and tag it', { release: true }),
            ],
          }),
        ],
      })
      const { onOpenRelease } = renderPage()

      const chips = await list('Common magex targets')
      const bump = chips.getByRole('button', { name: 'magex version:bump: Release does this, with checks' })

      expect(bump).toHaveAttribute('title', 'Release does this, with checks. Open Release, or run it anyway.')
      expect(chips.queryByRole('button', { name: 'Run magex version:bump' })).not.toBeInTheDocument()

      await userEvent.click(bump)
      const menu = within(screen.getByRole('menu'))
      expect(screen.getByText('Release does this, with checks')).toBeInTheDocument()
      expect(menu.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
        'Open Release',
        'Run it anyway',
        'Run it anyway in iTerm2',
      ])
      await userEvent.click(menu.getByRole('menuitem', { name: 'Open Release' }))
      expect(onOpenRelease).toHaveBeenCalledOnce()
      expect(platform.runScript).not.toHaveBeenCalled()

      await userEvent.type(screen.getByRole('textbox', { name: 'Arguments' }), 'bump=minor')
      await userEvent.click(bump)
      await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway' }))

      expect(platform.runScript).toHaveBeenLastCalledWith(
        expect.objectContaining({ ref: 'magex:version:bump', args: ['bump=minor'], runsIn: '', anyway: true }),
      )
    })

    it('leaves Open Release out of a version bump chip’s menu while Release is off', async () => {
      withPage({
        sections: [
          scriptSection({
            source: 'magex',
            title: 'magex',
            common: ['magex:version:bump'],
            entries: [magexTarget('version:bump', 'Bump the version and tag it', { release: true })],
          }),
        ],
      })
      renderPage({ release: false })

      await userEvent.click(
        (await list('Common magex targets')).getByRole('button', {
          name: 'magex version:bump: Release does this, with checks',
        }),
      )
      expect(
        within(screen.getByRole('menu'))
          .getAllByRole('menuitem')
          .map((item) => item.textContent),
      ).toEqual(['Run it anyway', 'Run it anyway in iTerm2'])

      await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway in iTerm2' }))
      expect(platform.runScript).toHaveBeenLastCalledWith(
        expect.objectContaining({ ref: 'magex:version:bump', runsIn: 'terminal', anyway: true }),
      )
    })

    it('greys a chip it can’t run, saying why', async () => {
      withPage({
        sections: [
          scriptSection({
            source: 'magex',
            title: 'magex',
            common: ['magex:lint', 'magex:gone'],
            entries: [magexTarget('lint', 'Run the linters', { runnable: false, why: 'Not today.' })],
          }),
        ],
      })
      renderPage()

      const chips = await list('Common magex targets')

      expect(chips.getAllByRole('button')).toHaveLength(1)
      expect(chips.getByRole('button', { name: 'Run magex lint' })).toBeDisabled()
      expect(chips.getByRole('button', { name: 'Run magex lint' })).toHaveAttribute('title', 'Not today.')
    })

    it('folds the repository’s own targets and the built-ins, and searches both', async () => {
      renderPage()

      const own = await screen.findByRole('button', { name: 'This repository’s targets (1)' })
      const all = screen.getByRole('button', { name: 'All magex commands (5)' })

      expect(own).toHaveAttribute('aria-expanded', 'false')
      expect(screen.queryByRole('list', { name: 'All magex commands' })).not.toBeInTheDocument()
      // A folded list isn't there, so nothing points at it.
      expect(all).not.toHaveAttribute('aria-controls')

      await userEvent.click(all)
      expect(all).toHaveAttribute('aria-controls', screen.getByRole('list', { name: 'All magex commands' }).id)
      expect(await names('All magex commands')).toEqual([
        'format:fix',
        'lint',
        'test',
        'version:bump',
        'update:install',
      ])
      const bump = await row('All magex commands', 'version:bump')
      expect(bump.getByText(/^Release does this, with checks/)).toBeInTheDocument()

      await userEvent.click(all)
      expect(screen.queryByRole('list', { name: 'All magex commands' })).not.toBeInTheDocument()

      const search = screen.getByRole('searchbox', { name: 'Search magex targets' })
      await userEvent.type(search, 'DOCS')
      expect(await names('This repository’s targets')).toEqual(['deploy:docs'])
      expect(screen.getByRole('button', { name: 'This repository’s targets (1)' })).toBeDisabled()
      expect(screen.queryByRole('button', { name: /^All magex commands/ })).not.toBeInTheDocument()

      await userEvent.clear(search)
      await userEvent.type(search, 'linters')
      expect(await names('All magex commands')).toEqual(['lint'])

      await userEvent.clear(search)
      await userEvent.type(search, 'nothing-like-it')
      expect(screen.getByText('No magex target matching “nothing-like-it”.')).toBeInTheDocument()
    })

    it('runs a built-in from its row, with the arguments given', async () => {
      renderPage()

      await userEvent.type(await screen.findByRole('textbox', { name: 'Arguments' }), 'verbose')
      await userEvent.click(screen.getByRole('button', { name: /^All magex commands/ }))
      await userEvent.click((await row('All magex commands', 'test')).getByRole('button', { name: 'Run test' }))

      expect(platform.runScript).toHaveBeenLastCalledWith(
        expect.objectContaining({ ref: 'magex:test', args: ['verbose'] }),
      )
    })

    it('says why magex can’t list its targets', async () => {
      withPage({
        sections: [
          scriptSection({ source: 't3', found: false }),
          scriptSection({
            source: 'magex',
            title: 'magex',
            found: false,
            note: 'magex isn’t installed. Install it in Settings → Integrations.',
          }),
        ],
      })
      renderPage()

      expect(
        await screen.findByText('magex isn’t installed. Install it in Settings → Integrations.'),
      ).toBeInTheDocument()
      expect(screen.queryByRole('textbox', { name: 'Arguments' })).not.toBeInTheDocument()
      expect(screen.queryByRole('list', { name: 'Common magex targets' })).not.toBeInTheDocument()
      expect(screen.queryByRole('heading', { name: 'From t3.json' })).not.toBeInTheDocument()
    })

    it('says why the repository’s own targets couldn’t be read', async () => {
      withPage({
        sections: [
          scriptSection({
            source: 'magex',
            title: 'magex',
            error: 'Couldn’t read the repository’s magefiles: permission denied.',
            entries: [magexTarget('lint', 'Run the linters')],
          }),
        ],
      })
      renderPage()

      expect(
        await screen.findByText('Couldn’t read the repository’s magefiles: permission denied.'),
      ).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^This repository’s targets/ })).not.toBeInTheDocument()
    })
  })

  it('marks each script by its t3.json icon', async () => {
    withPage({
      sections: [
        scriptSection({
          entries: ['play', 'test', 'lint', 'build', 'configure', 'other'].map((icon) =>
            scriptEntry({ ref: `t3:${icon}`, name: icon, icon }),
          ),
        }),
      ],
    })
    renderPage()

    expect(await names('From t3.json')).toEqual(['play', 'test', 'lint', 'build', 'configure', 'other'])
  })

  it('says why a source couldn’t be read, and hides one turned off', async () => {
    withPage({
      sections: [
        scriptSection({ error: 't3.json isn’t valid JSON: unexpected end of input.' }),
        scriptSection({ source: 'tasks', title: 'From .vscode/tasks.json', off: true, entries: [scriptEntry()] }),
      ],
    })
    renderPage()

    expect(await screen.findByText('t3.json isn’t valid JSON: unexpected end of input.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'From .vscode/tasks.json' })).not.toBeInTheDocument()
  })

  describe('a clone with nothing to show', () => {
    const tasks = { source: 'tasks' as const, title: 'From .vscode/tasks.json' }
    const magexOff = scriptSection({ source: 'magex', title: 'magex', off: true })

    // empty shows a page with sections, and returns what opens the settings.
    async function empty(sections: ScriptSection[]) {
      withPage({ sections })
      const { onOpenSettings } = renderPage()

      await screen.findByText('acme-org/sdk')

      return { onOpenSettings }
    }

    it('says its files list nothing, and that magex is turned off, with the way to Settings', async () => {
      const { onOpenSettings } = await empty([scriptSection(), scriptSection(tasks), magexOff])

      expect(screen.getByText('acme-org/sdk’s t3.json and .vscode/tasks.json list no commands.')).toBeInTheDocument()
      expect(screen.getByText('magex targets are turned off in Settings → Integrations.')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Open Settings' }))
      expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
    })

    it('says its sources are turned off', async () => {
      await empty([scriptSection({ off: true }), scriptSection({ ...tasks, off: true }), magexOff])

      expect(
        screen.getByText(
          'Commands from t3.json and VS Code tasks and magex targets are turned off in Settings → Integrations.',
        ),
      ).toBeInTheDocument()
      expect(screen.queryByText(/no commands|has no/)).not.toBeInTheDocument()
    })

    it('says it has neither file, with nothing turned off', async () => {
      await empty([scriptSection({ found: false }), scriptSection({ ...tasks, found: false })])

      expect(screen.getByText('acme-org/sdk has no t3.json or .vscode/tasks.json.')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Open Settings' })).not.toBeInTheDocument()
    })

    it('says it has nothing to show when it lists no source', async () => {
      await empty([])

      expect(screen.getByText('acme-org/sdk has no scripts to show.')).toBeInTheDocument()
    })
  })

  it('reads another clone when one is chosen', async () => {
    platform.listRepoScripts.mockImplementation((dir) =>
      Promise.resolve(repoScripts({ dir, repo: 'acme-org/web', name: 'acme-org/web', sections: [] })),
    )
    renderPage()

    await screen.findByText('acme-org/web')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'web')

    expect(await screen.findByText('~/projects/web')).toBeInTheDocument()
    expect(platform.listRepoScripts).toHaveBeenLastCalledWith('~/projects/web', false)
  })

  it('reads the clone’s scripts again, listing magex’s targets afresh, and never every clone', async () => {
    renderPage()

    await list('From t3.json')
    expect(platform.listScriptRepos).toHaveBeenCalledOnce()

    let finish: (page: RepoScripts) => void = () => undefined
    platform.listRepoScripts.mockReturnValueOnce(
      new Promise<RepoScripts>((resolve) => {
        finish = resolve
      }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

    expect(screen.getByRole('button', { name: 'Reading…' })).toBeDisabled()
    expect(platform.listRepoScripts).toHaveBeenLastCalledWith('~/projects/sdk', true)
    expect(platform.listScriptRepos).toHaveBeenCalledOnce()

    act(() => {
      finish(
        repoScripts({
          sections: [scriptSection({ entries: [scriptEntry({ name: 'Lint only', ref: 't3:Lint only' })] })],
        }),
      )
    })

    expect(await names('From t3.json')).toEqual(['Lint only'])
    expect(screen.getByRole('button', { name: 'Read again' })).toBeEnabled()
  })

  it('never lets an answer to an earlier read land over Read again’s', async () => {
    let first: (page: RepoScripts) => void = () => undefined
    platform.listRepoScripts.mockReturnValueOnce(
      new Promise<RepoScripts>((resolve) => {
        first = resolve
      }),
    )
    platform.listRepoScripts.mockResolvedValueOnce(
      repoScripts({ sections: [scriptSection({ entries: [scriptEntry({ name: 'Fresh', ref: 't3:Fresh' })] })] }),
    )
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Read again' }))
    expect(await names('From t3.json')).toEqual(['Fresh'])
    expect(platform.listRepoScripts.mock.calls).toEqual([
      ['~/projects/sdk', false],
      ['~/projects/sdk', true],
    ])

    act(() => {
      first(repoScripts({ sections: [scriptSection({ entries: [scriptEntry({ name: 'Stale', ref: 't3:Stale' })] })] }))
    })
    await act(async () => {
      await Promise.resolve()
    })

    expect(await names('From t3.json')).toEqual(['Fresh'])
  })

  it('says why the scripts couldn’t be read again', async () => {
    renderPage()

    await list('From t3.json')
    platform.listRepoScripts.mockRejectedValueOnce(new Error('magex timed out'))
    await userEvent.click(screen.getByRole('button', { name: 'Read again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the scripts again: magex timed out')
    expect(await names('From t3.json')).toEqual(['Fix all', 'Test', 'Version: bump patch'])
  })

  it('says why the clones or the scripts couldn’t be listed', async () => {
    platform.listScriptRepos.mockRejectedValueOnce(new Error('the clone root is gone'))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t list the clones: the clone root is gone')
  })

  it('says it is reading the clone’s scripts until they come', async () => {
    platform.listRepoScripts.mockReturnValue(new Promise<RepoScripts>(() => undefined))
    renderPage()

    expect(await screen.findByText('Reading the scripts in sdk…')).toBeInTheDocument()
  })

  it('says why a clone’s scripts couldn’t be read', async () => {
    platform.listRepoScripts.mockRejectedValue(new Error('~/projects/sdk isn’t a clone in ~/projects.'))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t read the scripts in sdk: ~/projects/sdk isn’t a clone in ~/projects.',
    )
  })

  it('offers a way to change the clone root when there are no clones', async () => {
    platform.listScriptRepos.mockResolvedValue([])
    const { onOpenSettings } = renderPage()

    expect(
      await screen.findByText(
        'No clones under ~/projects yet. Repo scripts lists the repositories you have cloned there.',
      ),
    ).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Change the repository locations' }))

    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('repositories')
    expect(platform.listRepoScripts).not.toHaveBeenCalled()
  })

  it('says when Scripts is off, with the way to Settings', async () => {
    withPage({ off: true, why: 'Scripts are turned off in Settings → Integrations.', sections: [] })
    const { onOpenSettings } = renderPage()

    expect(await screen.findByText('Scripts are turned off in Settings → Integrations.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Open Settings' }))

    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
    expect(platform.getBranchStatus).not.toHaveBeenCalled()
  })

  it('starts on the clone chosen last while it is still listed', async () => {
    platform.listScriptRepos.mockResolvedValue([scriptClone({ name: 'web', dir: '~/projects/web' }), scriptClone()])
    render(
      <RepoScriptsPage
        dir="~/projects/sdk"
        onDir={vi.fn()}
        runsIn="app"
        terminal="iterm2"
        where="under ~/projects"
        followed={null}
        onFollow={vi.fn()}
        onOpenRelease={null}
        onOpenSettings={vi.fn()}
      />,
    )

    expect(await screen.findByRole('combobox', { name: 'Repository' })).toHaveValue('~/projects/sdk')
    expect(platform.listRepoScripts).toHaveBeenCalledExactlyOnceWith('~/projects/sdk', false)
  })

  it('remembers the clone it starts on, so a list ordered anew never moves the page', async () => {
    const { onDir } = renderPage()

    expect(await screen.findByRole('combobox', { name: 'Repository' })).toHaveValue('~/projects/sdk')
    await waitFor(() => {
      expect(onDir).toHaveBeenCalledExactlyOnceWith('~/projects/sdk')
    })
  })

  it('shows a run a script starts beside the scripts, which stay as they were', async () => {
    const { onFollow } = renderPage()

    await userEvent.type(await screen.findByRole('textbox', { name: 'Arguments' }), 'verbose')
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search magex targets' }), 'tests')
    await userEvent.click(screen.getByRole('button', { name: 'Run magex test' }))

    expect(onFollow).toHaveBeenCalledExactlyOnceWith(31)
    expect(await screen.findByRole('region', { name: 'magex lint' })).toBeInTheDocument()
    expect(platform.getScriptRun).toHaveBeenCalledWith(31)
    expect(screen.getByRole('textbox', { name: 'Arguments' })).toHaveValue('verbose')
    expect(screen.getByRole('searchbox', { name: 'Search magex targets' })).toHaveValue('tests')
    expect(await names('All magex commands')).toEqual(['test'])
    expect(platform.listRepoScripts).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))
    expect(onFollow).toHaveBeenLastCalledWith(null)
    expect(screen.queryByRole('region', { name: 'magex lint' })).not.toBeInTheDocument()
  })

  it('says whether origin has newer commits on the branch, beside it', async () => {
    renderPage()

    expect(await screen.findByText('Up to date')).toBeInTheDocument()
    expect(platform.getBranchStatus).toHaveBeenCalledExactlyOnceWith('~/projects/sdk', false)
    expect(screen.queryByRole('button', { name: 'Pull master' })).not.toBeInTheDocument()
  })

  it('pulls a branch behind origin, shows the pull as a run, and reads the scripts again once it ends', async () => {
    const pull = { id: 51, kind: 'gitPull' as const, title: 'Pull master', repo: 'acme-org/sdk', dir: '~/projects/sdk' }
    platform.getBranchStatus.mockResolvedValue(
      branchStatus({ state: 'behind', origin: '5d6e7f8', note: 'origin/master has newer commits.' }),
    )
    platform.getScriptRun.mockResolvedValue(scriptRun({ ...pull, status: 'running', summary: '', finishedAt: '' }))
    const { onFollow } = renderPage()

    expect(await screen.findByText('Update available')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Pull master' }))

    expect(platform.pullBranch).toHaveBeenCalledExactlyOnceWith('~/projects/sdk')
    expect(onFollow).toHaveBeenCalledExactlyOnceWith(51)
    expect(await screen.findByRole('region', { name: 'Pull master' })).toBeInTheDocument()
    expect(platform.listRepoScripts).toHaveBeenCalledOnce()

    platform.getBranchStatus.mockResolvedValue(branchStatus())
    platform.getScriptRun.mockResolvedValue(
      scriptRun({ ...pull, summary: 'Updated master from 1a2b3c4 to 5d6e7f8, 2 new commits.' }),
    )
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await screen.findByText('Up to date')).toBeInTheDocument()
    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(2)
    })
    expect(platform.listRepoScripts).toHaveBeenLastCalledWith('~/projects/sdk', false)
    expect(platform.getBranchStatus).toHaveBeenLastCalledWith('~/projects/sdk', false)
  })

  it('folds a source’s section, remembering it, and opens on the sections as the user left them', async () => {
    const { onSection } = renderPage({ sections: { 'scripts.magex': false } })

    expect(await names('From t3.json')).toEqual(['Fix all', 'Test', 'Version: bump patch'])
    expect(screen.getByRole('button', { name: 'magex · 6' })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('textbox', { name: 'Arguments' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'From t3.json · 3' }))

    expect(onSection).toHaveBeenCalledExactlyOnceWith('scripts.t3', false)
    expect(screen.getByRole('button', { name: 'From t3.json · 3' })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('list', { name: 'From t3.json' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'magex · 6' }))

    expect(onSection).toHaveBeenLastCalledWith('scripts.magex', true)
    expect(screen.getByRole('textbox', { name: 'Arguments' })).toBeInTheDocument()
  })

  it('says a run started, with a way to show it, without opening it beside the scripts by default', async () => {
    const { onFollow, onShowRuns } = renderPage({ showRuns: false })

    await userEvent.click(await screen.findByRole('button', { name: 'Run magex test' }))

    const note = await screen.findByRole('status')
    expect(note).toHaveTextContent('The run started.')
    expect(onFollow).not.toHaveBeenCalled()
    expect(screen.queryByRole('region', { name: 'magex lint' })).not.toBeInTheDocument()

    await userEvent.click(within(note).getByRole('button', { name: 'Show it' }))

    expect(onFollow).toHaveBeenCalledExactlyOnceWith(31)
    expect(await screen.findByRole('region', { name: 'magex lint' })).toBeInTheDocument()
    expect(onShowRuns).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))
    await userEvent.click(screen.getByRole('button', { name: 'Run magex test' }))
    await userEvent.click(
      within(await screen.findByRole('status')).getByRole('button', { name: 'Always show runs here' }),
    )

    expect(onShowRuns).toHaveBeenCalledExactlyOnceWith(true)
    expect(onFollow).toHaveBeenLastCalledWith(31)

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))
    await userEvent.click(screen.getByRole('button', { name: 'Run magex test' }))

    expect(onFollow).toHaveBeenLastCalledWith(31)
    expect(screen.queryByText('The run started.')).not.toBeInTheDocument()
  })

  it('shows a pull only in the branch’s status by default', async () => {
    const pull = { id: 51, kind: 'gitPull' as const, title: 'Pull master', repo: 'acme-org/sdk', dir: '~/projects/sdk' }
    platform.getBranchStatus.mockResolvedValue(
      branchStatus({ state: 'behind', origin: '5d6e7f8', note: 'origin/master has newer commits.' }),
    )
    platform.getScriptRun.mockResolvedValue(scriptRun({ ...pull, status: 'running', summary: '', finishedAt: '' }))
    const { onFollow } = renderPage({ showRuns: false })

    await userEvent.click(await screen.findByRole('button', { name: 'Pull master' }))

    expect(platform.pullBranch).toHaveBeenCalledExactlyOnceWith('~/projects/sdk')
    expect(onFollow).not.toHaveBeenCalled()
    expect(screen.queryByText('The run started.')).not.toBeInTheDocument()

    platform.getBranchStatus.mockResolvedValue(branchStatus())
    platform.getScriptRun.mockResolvedValue(scriptRun({ ...pull, summary: 'Updated master.' }))
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await screen.findByText('Up to date')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Pull master' })).not.toBeInTheDocument()
  })

  it('fills in a release task’s choice with its first value when its default isn’t one', async () => {
    withPage({
      sections: [
        scriptSection({
          source: 'tasks',
          title: 'From .vscode/tasks.json',
          entries: [scriptEntry({ ref: 'tasks:Pick', name: 'Pick', inputs: [bumpInput({ default: 'nope' })] })],
        }),
      ],
    })
    renderPage()

    await userEvent.click((await row('From .vscode/tasks.json', 'Pick')).getByRole('button', { name: 'Run Pick' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Run' }))

    expect(platform.runScript).toHaveBeenLastCalledWith(expect.objectContaining({ inputs: { bump: 'patch' } }))
  })
})
