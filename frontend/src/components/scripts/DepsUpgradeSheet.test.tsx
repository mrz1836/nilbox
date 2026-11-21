import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type {
  LaunchHold,
  LaunchResult,
  ManifestsClone,
  Prompt,
  RepoLaunchRequest,
  RepoResolution,
} from '../../platform'
import { deferred } from '../../test/async'
import { renderAt } from '../../test/clock'
import {
  agents,
  codexAgent,
  launchResult,
  manifestsClone,
  manifestsModule,
  profile,
  prompt,
  prompts,
  repoResolution,
  runningHold,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { DepsUpgradeSheet } from './DepsUpgradeSheet'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// noUsage is where a prompt for repositories is a default: nowhere, as no default takes one.
const noUsage = { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] }

// repoPrompt is a prompt for repositories as the library lists it.
function repoPrompt(overrides: Partial<Prompt>): Prompt {
  return prompt({ appliesTo: 'repo', body: 'Upgrade {{.Repo}}\n', starterKey: '', usage: noUsage, ...overrides })
}

// minor and major are the upgrade's two starters; goOnly and security are prompts for
// repositories the user wrote.
const minor = repoPrompt({ id: 21, name: 'Upgrade dependencies', nickname: 'Upgrade deps', starterKey: 'deps-minor' })
const major = repoPrompt({
  id: 22,
  name: 'Upgrade dependencies, majors too',
  nickname: 'Upgrade majors too',
  starterKey: 'deps-major',
})
const goOnly = repoPrompt({ id: 23, name: 'Upgrade the Go modules alone', nickname: 'Go only' })
const security = repoPrompt({ id: 24, name: 'Take the security fixes alone', nickname: 'Security fixes' })

// asked is the request the sheet resolves and plans as it opens: acme-org/api-server's
// clone with the Minor and patch starter, nothing else chosen.
const asked: RepoLaunchRequest = {
  repo: 'acme-org/api-server',
  dir: '~/projects/api-server',
  promptId: 21,
  agent: '',
  profile: '',
  model: null,
  effort: null,
  planMode: null,
  fast: null,
  alongside: '',
}

// codexResolution is the repository's settings resolved for Codex, as the agent's switch
// asks for them.
const codexResolution = repoResolution({
  agent: codexAgent(),
  agentSource: 'launch',
  profile: { value: 'default', source: 'app', inherited: 'default', inheritedSource: 'app' },
})

// deps is where the upgrade's worktree is made, under the app's folder of worktrees.
const deps = `~/projects/.${brand.slug}/worktrees/api-server/deps-20260615`

// heldFor is a start held back for a Claude session running in the upgrade's new folder,
// one started on number of acme-org/api-server: an item's, or 0 for another upgrade's.
function heldFor(number: number): LaunchHold {
  const base = runningHold()

  return {
    ...base,
    dir: deps,
    worktree: 'deps-20260615',
    running: base.running.map((r) => ({
      ...r,
      dir: deps,
      worktree: 'deps-20260615',
      repo: 'acme-org/api-server',
      number,
    })),
  }
}

interface Options {
  clone?: ManifestsClone
  // library is the prompt library as it lists: the item prompts and both starters unless
  // given.
  library?: Prompt[]
}

function renderSheet({ clone = manifestsClone(), library = [...prompts(), minor, major] }: Options = {}) {
  platform.listPrompts.mockResolvedValue(library)

  const handlers = { onOpenPrompts: vi.fn<() => void>(), onClose: vi.fn<() => void>() }
  const view = renderAt(<DepsUpgradeSheet clone={clone} {...handlers} />)

  return { ...handlers, unmount: view.unmount }
}

// dialog is the open sheet, whatever it shows.
function dialog() {
  return within(screen.getByRole('dialog', { name: 'Upgrade dependencies' }))
}

// sheet is the open sheet, once the launch has resolved and its settings show.
async function sheet() {
  const s = dialog()

  await s.findByRole('radiogroup', { name: 'Profile' })

  return s
}

describe('DepsUpgradeSheet', () => {
  it('offers the two upgrades, Minor and patch chosen, and resolves and plans the launch as it opens', async () => {
    renderSheet()

    expect(dialog().getByRole('status')).toHaveTextContent('Loading the launch settings…')

    const s = await sheet()

    expect(s.getByTitle('acme-org/api-server · ~/projects/api-server')).toHaveTextContent(
      'acme-org/api-server · ~/projects/api-server',
    )
    const manifests = within(s.getByRole('list', { name: 'Manifests' })).getAllByRole('listitem')
    expect(manifests).toHaveLength(2)
    expect(manifests[0]).toHaveTextContent('Go: go.mod (magex)')
    expect(manifests[1]).toHaveTextContent('JS: web/package.json (pnpm)')

    const minorChoice = s.getByRole('radio', { name: 'Minor and patch' })
    expect(minorChoice).toBeChecked()
    expect(minorChoice).toHaveAccessibleDescription('Newest versions within each major; the PR lists the majors found.')
    const majorChoice = s.getByRole('radio', { name: 'Majors too' })
    expect(majorChoice).not.toBeChecked()
    expect(majorChoice).toHaveAccessibleDescription(
      'Also moves to new majors, refactoring code where needed. Bigger, and it may break things.',
    )
    // Neither an item's prompt nor a choice among other prompts is offered.
    expect(s.queryByRole('combobox', { name: 'Another prompt' })).not.toBeInTheDocument()
    expect(s.queryByText('Review')).not.toBeInTheDocument()

    expect(platform.resolveRepoLaunch).toHaveBeenCalledExactlyOnceWith(asked)
    expect(platform.previewRepoLaunch).toHaveBeenCalledExactlyOnceWith(asked)
    expect(
      await s.findByText('Runs in a new worktree, deps-20260615, off origin/master. Your clone isn’t touched.'),
    ).toBeInTheDocument()
    // A repository's launch always makes a worktree, and is trusted: neither is a choice.
    expect(s.queryByRole('checkbox', { name: /Start in a worktree/ })).not.toBeInTheDocument()
    expect(s.queryByRole('checkbox', { name: /Use profile permissions/ })).not.toBeInTheDocument()
    expect(s.queryByText(/GitHub hasn’t said/)).not.toBeInTheDocument()
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()
  })

  it('says it is working out the settings, and waits to start, until the launch resolves', async () => {
    const resolving = deferred<RepoResolution>()
    platform.resolveRepoLaunch.mockReturnValue(resolving.promise)
    renderSheet()

    expect(await dialog().findByText('Working out the launch settings…')).toBeInTheDocument()
    expect(dialog().getByRole('button', { name: 'Start upgrade' })).toBeDisabled()

    await act(async () => {
      resolving.resolve(repoResolution())
      await Promise.resolve()
    })

    const s = await sheet()
    expect(s.queryByText('Working out the launch settings…')).not.toBeInTheDocument()
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()
  })

  it('resolves and plans again for Majors too, and starts with it', async () => {
    const { onClose } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('radio', { name: 'Majors too' }))

    expect(s.getByRole('radio', { name: 'Minor and patch' })).not.toBeChecked()
    await waitFor(() => {
      expect(platform.resolveRepoLaunch).toHaveBeenLastCalledWith({ ...asked, promptId: 22 })
    })
    expect(platform.previewRepoLaunch).toHaveBeenLastCalledWith({ ...asked, promptId: 22 })
    await waitFor(() => {
      expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()
    })

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    expect(platform.launchRepo).toHaveBeenCalledExactlyOnceWith({ ...asked, promptId: 22 })
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce()
    })
  })

  it('offers the user’s own prompts for repositories in a select, a pick there choosing one', async () => {
    const { onClose, onOpenPrompts } = renderSheet({ library: [...prompts(), minor, major, goOnly, security] })
    const s = await sheet()

    const select = s.getByRole('combobox', { name: 'Another prompt' })
    expect(select).toHaveDisplayValue('Choose a prompt')
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Choose a prompt', 'Go only', 'Security fixes'])
    expect(s.queryByRole('button', { name: 'Edit “Go only”' })).not.toBeInTheDocument()

    await userEvent.selectOptions(select, 'Security fixes')

    expect(select).toHaveDisplayValue('Security fixes')
    expect(within(select).queryByRole('option', { name: 'Choose a prompt' })).not.toBeInTheDocument()
    expect(s.getByRole('radio', { name: 'Minor and patch' })).not.toBeChecked()
    expect(s.getByRole('radio', { name: 'Majors too' })).not.toBeChecked()
    await waitFor(() => {
      expect(platform.resolveRepoLaunch).toHaveBeenLastCalledWith({ ...asked, promptId: 24 })
    })

    // A starter chosen again puts the select back to none of them.
    await userEvent.click(s.getByRole('radio', { name: 'Minor and patch' }))
    expect(select).toHaveDisplayValue('Choose a prompt')

    await userEvent.selectOptions(select, 'Go only')
    await userEvent.click(s.getByRole('button', { name: 'Edit “Go only”' }))

    expect(onClose).toHaveBeenCalledOnce()
    expect(onOpenPrompts).toHaveBeenCalledOnce()
  })

  it('leaves out a starter the user deleted, and starts with the one left', async () => {
    renderSheet({ library: [...prompts(), major, goOnly] })
    const s = await sheet()

    expect(s.queryByRole('radio', { name: 'Minor and patch' })).not.toBeInTheDocument()
    expect(s.getByRole('radio', { name: 'Majors too' })).toBeChecked()
    expect(s.getByRole('combobox', { name: 'Another prompt' })).toHaveDisplayValue('Choose a prompt')
    expect(platform.resolveRepoLaunch).toHaveBeenCalledExactlyOnceWith({ ...asked, promptId: 22 })
  })

  it('chooses among the user’s own prompts alone once both starters are gone', async () => {
    renderSheet({ library: [...prompts(), goOnly, security] })
    const s = await sheet()

    expect(s.queryByRole('radiogroup', { name: 'Upgrade' })).not.toBeInTheDocument()
    expect(s.getByRole('combobox', { name: 'Prompt' })).toHaveDisplayValue('Go only')
    expect(s.getByRole('button', { name: 'Edit “Go only”' })).toBeInTheDocument()
    expect(platform.resolveRepoLaunch).toHaveBeenCalledExactlyOnceWith({ ...asked, promptId: 23 })
  })

  it('says when no prompt is for repositories, sends the user to the Prompts page, and starts nothing', async () => {
    const { onClose, onOpenPrompts } = renderSheet({ library: prompts() })

    expect(
      await dialog().findByText(
        'No prompt in your library is for repositories. Write one on the Prompts page to upgrade with it.',
      ),
    ).toBeInTheDocument()
    expect(dialog().getByRole('button', { name: 'Start upgrade' })).toBeDisabled()
    expect(platform.resolveRepoLaunch).not.toHaveBeenCalled()
    expect(platform.previewRepoLaunch).not.toHaveBeenCalled()

    await userEvent.click(dialog().getByRole('button', { name: 'Open the Prompts page' }))

    expect(onClose).toHaveBeenCalledOnce()
    expect(onOpenPrompts).toHaveBeenCalledOnce()
  })

  it('opens the Prompts page from a starter’s Edit, closing the sheet', async () => {
    const { onClose, onOpenPrompts } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Edit “Upgrade majors too”' }))

    expect(onClose).toHaveBeenCalledOnce()
    expect(onOpenPrompts).toHaveBeenCalledOnce()
  })

  it('says why the launch settings couldn’t load', async () => {
    platform.listPrompts.mockRejectedValue(new Error('database is locked'))
    renderAt(<DepsUpgradeSheet clone={manifestsClone()} onOpenPrompts={vi.fn()} onClose={vi.fn()} />)

    expect(await dialog().findByRole('alert')).toHaveTextContent(
      'Couldn’t load the launch settings: database is locked',
    )
    expect(dialog().getByRole('button', { name: 'Start upgrade' })).toBeDisabled()
  })

  it('lists the clone’s manifests by kind, each with the tools set up for it', async () => {
    const web = manifestsModule({ kind: 'js', path: 'web/package.json', packageManager: 'npm' })
    const shown = async (clone: ManifestsClone) => {
      const { unmount } = renderSheet({ clone })
      const s = await sheet()
      const list = s.queryByRole('list', { name: 'Manifests' })
      const items =
        list === null
          ? []
          : within(list)
              .getAllByRole('listitem')
              .map((li) => li.textContent)

      unmount()

      return items
    }

    expect(
      await shown(manifestsClone({ modules: [manifestsModule(), manifestsModule({ path: 'tools/go.mod' })] })),
    ).toEqual(['Go: go.mod, tools/go.mod (magex)'])
    expect(await shown(manifestsClone({ modules: [web], tools: ['npm'] }))).toEqual(['JS: web/package.json (npm)'])
    expect(await shown(manifestsClone({ modules: [manifestsModule(), web], tools: [] }))).toEqual([
      'Go: go.mod',
      '·JS: web/package.json',
    ])
    expect(await shown(manifestsClone({ modules: [], error: 'git couldn’t read the clone' }))).toEqual([])
  })

  it('shows what the sheet says of the repository first, as it is', async () => {
    const notice =
      'GitHub hasn’t said whether you can push to acme-org/api-server. If you can’t, the agent stops at the push and says so.'
    platform.resolveRepoLaunch.mockResolvedValue(repoResolution({ notice }))
    renderSheet()
    const s = await sheet()

    expect(s.getByText(notice)).toBeInTheDocument()
  })

  it('shows a refusal of the repository as the sheet’s error, and starts nothing', async () => {
    const refusal = 'GitHub says acme-org/api-server is archived, so nothing can be pushed to it.'
    platform.resolveRepoLaunch.mockRejectedValue(new Error(refusal))
    renderSheet()

    expect(await dialog().findByRole('alert')).toHaveTextContent(refusal)
    expect(dialog().getByRole('button', { name: 'Start upgrade' })).toBeDisabled()
    expect(dialog().queryByRole('radiogroup', { name: 'Profile' })).not.toBeInTheDocument()
    expect(dialog().queryByText('Working out the launch settings…')).not.toBeInTheDocument()
  })

  it('shows a refusal of the worktree as the sheet’s error, and starts nothing', async () => {
    const refusal =
      'Every name for today’s worktree of ~/projects/api-server, deps-20260615 to deps-20260615-99, is taken. ' +
      'Remove some of them on the Worktrees tab, then launch again.'
    platform.previewRepoLaunch.mockRejectedValue(new Error(refusal))
    renderSheet()
    const s = await sheet()

    expect(await s.findByRole('alert')).toHaveTextContent(refusal)
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeDisabled()
    expect(s.getByText('Runs in a new worktree off the default branch. Your clone isn’t touched.')).toBeInTheDocument()
  })

  it('keeps the last settings worked out while a choice can’t be, so it can be taken back', async () => {
    platform.resolveRepoLaunch.mockImplementation((req) =>
      req.agent === 'codex'
        ? Promise.reject(new Error('No Codex profile was found: run codex once.'))
        : Promise.resolve(repoResolution()),
    )
    renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('radio', { name: 'Codex' }))

    expect(await s.findByRole('alert')).toHaveTextContent('No Codex profile was found: run codex once.')
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeDisabled()

    await userEvent.click(s.getByRole('radio', { name: 'Claude' }))

    await waitFor(() => {
      expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()
    })
    expect(s.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('starts the upgrade with the choices made, and closes, as a launch that starts says nothing', async () => {
    platform.listProfiles.mockResolvedValue([
      profile(),
      profile({ name: 'work', configDir: '~/.claude_work', folder: '~/.claude_work' }),
      profile({ agent: 'codex', folder: '~/.codex' }),
    ])
    platform.resolveRepoLaunch.mockImplementation((req) =>
      Promise.resolve(req.agent === 'codex' ? codexResolution : repoResolution()),
    )
    const { onClose } = renderSheet()
    const s = await sheet()

    expect(s.getByRole('radio', { name: 'work' })).toBeInTheDocument()

    await userEvent.click(s.getByRole('radio', { name: 'Codex' }))

    const fast = await s.findByRole('checkbox', { name: /Fast mode/ })
    expect(s.queryByRole('radio', { name: 'work' })).not.toBeInTheDocument()
    expect(platform.resolveRepoLaunch).toHaveBeenLastCalledWith({ ...asked, agent: 'codex' })

    await userEvent.selectOptions(s.getByRole('combobox', { name: 'Model' }), 'GPT-6-Luna')
    await userEvent.click(s.getByRole('checkbox', { name: /Start read-only/ }))
    await userEvent.click(fast)

    // The model, plan mode, and Fast mode only change this launch's own settings, which
    // resolve nothing again.
    expect(platform.resolveRepoLaunch).toHaveBeenCalledTimes(2)

    const starting = deferred<LaunchResult>()
    platform.launchRepo.mockReturnValue(starting.promise)
    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    expect(platform.launchRepo).toHaveBeenCalledExactlyOnceWith({
      ...asked,
      agent: 'codex',
      model: 'gpt-6-luna',
      planMode: true,
      fast: true,
    })
    expect(s.getByRole('button', { name: 'Starting the upgrade…' })).toBeDisabled()
    expect(onClose).not.toHaveBeenCalled()

    await act(async () => {
      starting.resolve(launchResult({ worktree: 'deps-20260615', dir: deps, agent: 'codex' }))
      await Promise.resolve()
    })

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('stays open to say what a start worked around', async () => {
    const notice =
      'Couldn’t fetch origin/master, so the worktree starts from the last fetched copy: Network is unreachable'
    platform.launchRepo.mockResolvedValue(launchResult({ worktree: 'deps-20260615', dir: deps, notice }))
    const { onClose } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    const started = await s.findByRole('status')
    expect(started).toHaveTextContent('The upgrade started in deps-20260615.')
    expect(started).toHaveTextContent(notice)
    expect(s.queryByRole('button', { name: 'Start upgrade' })).not.toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()

    await userEvent.click(s.getByRole('button', { name: 'Close' }))

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows why a start failed, until a choice changes', async () => {
    platform.launchRepo.mockRejectedValue(new Error('Couldn’t fetch origin/master.'))
    renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    expect(await s.findByRole('alert')).toHaveTextContent('Couldn’t fetch origin/master.')
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()

    await userEvent.click(s.getByRole('radio', { name: 'Majors too' }))

    expect(s.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('asks before starting beside a session running where it would start, and starts beside it once confirmed', async () => {
    const hold = heldFor(12)
    platform.launchRepo.mockResolvedValueOnce(launchResult({ hold })).mockResolvedValueOnce(launchResult())
    const { onClose } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    const held = within(await screen.findByRole('dialog', { name: 'A session is already running here' }))
    // A session started on an item of the repository is named by it.
    expect(held.getByRole('list', { name: 'Sessions running here' })).toHaveTextContent(
      'Claude needs approval on acme-org/api-server#12 · 2m ago',
    )
    expect(held.getByText(/working in worktree deps-20260615 at once/)).toBeInTheDocument()

    await userEvent.click(held.getByRole('button', { name: 'Start another' }))

    expect(platform.launchRepo).toHaveBeenLastCalledWith({ ...asked, alongside: hold.key })
    await waitFor(() => {
      expect(onClose).toHaveBeenCalledOnce()
    })
  })

  it('goes back to the sheet when a held start is cancelled, or shows the running session instead', async () => {
    const hold = heldFor(0)
    platform.launchRepo.mockResolvedValue(launchResult({ hold }))
    const { onClose } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))

    // A session another upgrade of the repository started needs no name.
    const running = await screen.findByRole('list', { name: 'Sessions running here' })
    expect(running).toHaveTextContent('Claude needs approval · 2m ago')
    expect(running).not.toHaveTextContent(/ on /)

    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'A session is already running here' })).getByRole('button', {
        name: 'Cancel',
      }),
    )

    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).not.toBeInTheDocument()
    expect(s.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show in terminal' }))

    expect(platform.focusSession).toHaveBeenCalledWith(hold.running[0]?.sessionId)
    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).not.toBeInTheDocument()

    platform.focusSession.mockRejectedValue(new Error('Its terminal tab is gone.'))
    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show in terminal' }))

    expect(await s.findByRole('alert')).toHaveTextContent('Its terminal tab is gone.')
    expect(platform.launchRepo).toHaveBeenCalledTimes(3)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes nothing when a start ends after the sheet closed', async () => {
    const starting = deferred<LaunchResult>()
    platform.launchRepo.mockReturnValue(starting.promise)
    const { onClose, unmount } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Start upgrade' }))
    unmount()

    await act(async () => {
      starting.resolve(launchResult())
      await Promise.resolve()
    })

    expect(onClose).not.toHaveBeenCalled()
  })

  it('previews exactly what the upgrade runs, again as a choice changes', async () => {
    renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Preview the launch' }))

    expect(await s.findByText(/Upgrade the Go and JavaScript dependencies of acme-org\/api-server/)).toBeInTheDocument()
    expect(s.getByText(`Creates ${deps} from origin/master`)).toBeInTheDocument()
    expect(platform.previewRepoLaunch).toHaveBeenLastCalledWith(asked)

    await userEvent.click(s.getByRole('radio', { name: 'Majors too' }))

    await waitFor(() => {
      expect(platform.previewRepoLaunch).toHaveBeenLastCalledWith({ ...asked, promptId: 22 })
    })
  })

  it('offers no choice of agent with one alone', async () => {
    platform.listAgents.mockResolvedValue(agents().slice(0, 1))
    renderSheet()
    const s = await sheet()

    expect(s.queryByRole('radiogroup', { name: 'Agent' })).not.toBeInTheDocument()
    expect(s.getByRole('radio', { name: 'default' })).toBeChecked()
  })

  it('closes on Cancel and on its close button', async () => {
    const { onClose } = renderSheet()
    const s = await sheet()

    await userEvent.click(s.getByRole('button', { name: 'Cancel' }))
    await userEvent.click(s.getByRole('button', { name: 'Close the upgrade sheet' }))

    expect(onClose).toHaveBeenCalledTimes(2)
    expect(platform.launchRepo).not.toHaveBeenCalled()
  })
})
