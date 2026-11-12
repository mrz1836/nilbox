import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
  type BoundFunctions,
  type queries,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Activity, StrictMode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { LaunchPreview } from '../platform'
import { at } from '../test/clock'
import {
  agentSession,
  agents,
  codexAgent,
  cloneTarget,
  item,
  itemResolution,
  launchDefaults,
  launchHold,
  launchPreview,
  launchResult,
  profile,
  prompts,
  resolution,
  runningHold,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { LaunchControls, type LaunchControlsProps } from './LaunchControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const profiles = [
  profile(),
  profile({ name: 'work', configDir: '~/.claude_work', folder: '~/.claude_work' }),
  profile({ agent: 'codex', folder: '~/.codex' }),
  profile({ agent: 'codex', name: 'alt', configDir: '~/.codex_alt', folder: '~/.codex_alt', signedIn: false }),
]

// defaultRequest is a launch with nothing chosen: the item's resolved settings.
const defaultRequest = {
  itemId: 1,
  agent: '',
  profile: '',
  promptId: 0,
  model: null,
  effort: null,
  planMode: null,
  worktree: null,
  fast: null,
  fullAccessOverride: false,
  confirmed: '',
  alongside: '',
}

function props(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return {
    item: item(),
    resolution: itemResolution(),
    agents: agents(),
    profiles,
    prompts: prompts(),
    launchDefaults: [],
    onDefaultsSaved: vi.fn(() => Promise.resolve()),
    onManagePrompts: vi.fn(),
    ...overrides,
  }
}

// optionsButton is the arrow beside "Open in Claude".
function optionsButton(): HTMLElement {
  return screen.getByRole('button', { name: /^Launch options/ })
}

// openMenu opens the launch menu and returns queries scoped to it.
async function openMenu() {
  await userEvent.click(optionsButton())

  return within(screen.getByRole('dialog', { name: 'Launch options' }))
}

// choosePrompt picks a prompt, by its nickname, in the launch options' Prompt select.
async function choosePrompt(menu: BoundFunctions<typeof queries>, nickname: string) {
  const select = menu.getByRole('combobox', { name: 'Prompt' })

  await userEvent.selectOptions(select, within(select).getByRole('option', { name: new RegExp(`^${nickname}`) }))
}

// missingClone is where a repository with no local clone would be cloned.
const missingClone = cloneTarget({ cloned: false })

// noCloneError is how a launch with no local clone fails.
const noCloneError = new Error('no local clone of acme-org/example-repo under ~/projects')

describe('LaunchControls', () => {
  it('names the resolved prompt and profile beside the button', () => {
    render(<LaunchControls {...props()} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toHaveAttribute(
      'title',
      'Open in Claude with “Review” as default',
    )
    expect(optionsButton()).toHaveAccessibleName('Launch options: Review · default')
    expect(optionsButton()).toHaveAttribute('aria-expanded', 'false')
  })

  it('opens the options as a modal dialog named for the launch and the item, focus on the chosen prompt', async () => {
    render(<LaunchControls {...props()} />)

    // Only an open dialog is rendered, so a list of rows holds none.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    const menu = await openMenu()
    const dialog = screen.getByRole('dialog', { name: 'Launch options' })

    expect(dialog).toHaveAttribute('open')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAccessibleDescription('acme-org/example-repo#7 · Add the widget cache')
    expect(menu.getByRole('heading', { name: 'Open in Claude' })).toBeInTheDocument()
    expect(optionsButton()).toHaveAttribute('aria-haspopup', 'dialog')
    expect(optionsButton()).toHaveAttribute('aria-expanded', 'true')
    expect(optionsButton()).toHaveAttribute('aria-controls', dialog.id)
    expect(menu.getByRole('combobox', { name: 'Prompt' })).toHaveFocus()
  })

  it('names an issue under the title', async () => {
    const issue = item({ id: 9, kind: 'issue', number: 12, title: 'The cache leaks' })
    const res = itemResolution({
      itemId: 9,
      kind: 'issue',
      prompt: { ...resolution().prompt, id: 4, nickname: 'Work on it', inheritedId: 4 },
    })
    render(<LaunchControls {...props({ item: issue, resolution: res })} />)

    await openMenu()

    expect(screen.getByRole('dialog', { name: 'Launch options' })).toHaveAccessibleDescription(
      'acme-org/example-repo#12 · The cache leaks',
    )
  })

  it('stays open while choosing, and keeps the choices once closed', async () => {
    const codex = resolution({ agent: codexAgent(), agentSource: 'launch' })
    platform.resolveLaunch.mockImplementation((request) =>
      Promise.resolve(request.agent === 'codex' ? codex : resolution()),
    )
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')
    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()

    await userEvent.click(menu.getByRole('radio', { name: 'Codex' }))
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()
    expect(await menu.findByRole('heading', { name: 'Open in Codex' })).toBeInTheDocument()

    await userEvent.click(menu.getByRole('button', { name: 'Close' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(optionsButton()).toHaveAccessibleName(/changed for this launch$/)
    expect(platform.launch).not.toHaveBeenCalled()

    const again = await openMenu()

    expect(again.getByRole('radio', { name: 'Codex' })).toBeChecked()
    expect(again.getByRole('combobox', { name: 'Prompt' })).toHaveValue('2')
    expect(again.getByRole('combobox', { name: 'Prompt' })).toHaveFocus()

    await userEvent.click(again.getByRole('button', { name: 'Open in Codex' }))

    expect(platform.launch).toHaveBeenCalledWith({ ...defaultRequest, agent: 'codex', promptId: 2 })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(optionsButton()).toHaveFocus()
  })

  it('closes without launching on Escape, the close button, Cancel, or a click on the backdrop, focus back on the arrow', async () => {
    render(<LaunchControls {...props()} />)

    const closers: ((dialog: HTMLElement) => Promise<unknown>)[] = [
      () => userEvent.keyboard('{Escape}'),
      (dialog) => userEvent.click(within(dialog).getByRole('button', { name: 'Close' })),
      (dialog) => userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' })),
      (dialog) => userEvent.click(dialog),
    ]

    for (const closeWith of closers) {
      await openMenu()
      await closeWith(screen.getByRole('dialog', { name: 'Launch options' }))

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(optionsButton()).toHaveFocus()
      expect(optionsButton()).toHaveAttribute('aria-expanded', 'false')
      expect(optionsButton()).not.toHaveAttribute('aria-controls')
    }

    expect(platform.launch).not.toHaveBeenCalled()
  })

  it('stays open after a press inside it that ends on the backdrop', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()
    const dialog = screen.getByRole('dialog', { name: 'Launch options' })

    // A drag that selects text from the heading out past the dialog's edge ends on the
    // backdrop, where the click lands.
    fireEvent.mouseDown(menu.getByRole('heading', { name: 'Open in Claude' }))
    fireEvent.click(dialog)

    expect(dialog).toBeInTheDocument()
  })

  it('draws the dialog in the page’s body, outside the row that opens it', async () => {
    render(
      <ul>
        <li data-testid="row" className="group hover:bg-raised/40">
          <LaunchControls {...props()} />
        </li>
      </ul>,
    )

    await openMenu()

    // The row's :hover and :focus-within never take in the pointer or focus in the
    // dialog, so the row doesn't light up behind the backdrop.
    const dialog = screen.getByRole('dialog', { name: 'Launch options' })
    expect(screen.getByTestId('row')).not.toContainElement(dialog)
    expect(dialog.parentElement).toBe(document.body)
    expect(screen.getByTestId('row')).toContainElement(optionsButton())
  })

  it('sizes every column in the dialog to what it holds, so the desktop’s WebKit never collapses it', async () => {
    render(<LaunchControls {...props()} />)

    await openMenu()

    // The dialog is as tall as its content, under a cap. WebKit counts a column item
    // with flex-1 (a 0% basis) there as empty, and the dialog shrinks to its border.
    const dialog = screen.getByRole('dialog', { name: 'Launch options' })
    const grown = Array.from(dialog.querySelectorAll('.flex-1'))
      .filter((el) => el.parentElement?.classList.contains('flex-col'))
      .map((el) => el.className)

    expect(dialog).not.toHaveClass('flex-1')
    expect(grown).toEqual([])
    expect(within(dialog).getByRole('heading', { name: 'Open in Claude' })).toBeInTheDocument()
  })

  it('closes the options when its page hides, keeping the choices, and leaves them closed when it shows', async () => {
    const page = (mode: 'visible' | 'hidden') => (
      <Activity mode={mode}>
        <LaunchControls {...props()} />
      </Activity>
    )
    const { rerender } = render(page('visible'))

    const menu = await openMenu()
    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    rerender(page('hidden'))
    // The dialog closes a microtask after its page hides.
    await act(async () => {
      await Promise.resolve()
    })
    rerender(page('visible'))

    expect(document.querySelector('dialog')).toBeNull()
    expect(optionsButton()).toHaveAttribute('aria-expanded', 'false')
    expect(optionsButton()).toHaveAccessibleName(/changed for this launch$/)
    expect((await openMenu()).getByRole('radio', { name: 'work' })).toBeChecked()
  })

  it('stays open under StrictMode, which runs its effects twice', async () => {
    render(
      <StrictMode>
        <LaunchControls {...props()} />
      </StrictMode>,
    )

    await openMenu()
    await act(async () => {
      await Promise.resolve()
    })

    expect(screen.getByRole('dialog', { name: 'Launch options' })).toHaveAttribute('open')
    expect(optionsButton()).toHaveAttribute('aria-expanded', 'true')
  })

  it('holds the options back while a launch runs, so nothing launches twice', async () => {
    platform.launch.mockReturnValue(new Promise(() => undefined))
    render(<LaunchControls {...props()} />)

    await userEvent.click((await openMenu()).getByRole('button', { name: 'Open in Claude' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Opening…' })).toBeDisabled()
    expect(optionsButton()).toBeDisabled()

    await userEvent.click(optionsButton())

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.launch).toHaveBeenCalledOnce()
  })

  it('waits for the launch settings before it can launch', () => {
    render(<LaunchControls {...props({ resolution: null })} />)

    // Until the settings resolve, the agent the button starts is not known.
    expect(screen.getByRole('button', { name: 'Open' })).toBeDisabled()
    expect(optionsButton()).toBeDisabled()
    expect(optionsButton()).toHaveTextContent('…')
  })

  it('launches with the resolved settings in one click', async () => {
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(defaultRequest)
    // The card's session tag follows the session it started, so the row says nothing more.
    expect(await screen.findByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('says what a launch worked around, as a warning', async () => {
    const notice = 'Couldn’t fetch origin/main, so the worktree starts from the last fetched copy: timed out'
    platform.launch.mockResolvedValue(launchResult({ notice }))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent(notice)
    expect(status).toHaveClass('text-warning')
    expect(status).not.toHaveTextContent('Opened')
  })

  it('shows why a launch failed', async () => {
    platform.launch.mockRejectedValue(new Error('find the agent: executable not found'))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('find the agent: executable not found')
    expect(platform.cloneTarget).toHaveBeenCalledWith(1)
    expect(screen.queryByRole('button', { name: /^Clone into/ })).not.toBeInTheDocument()
  })

  it('shows the launch’s own failure when the clone check fails too', async () => {
    platform.launch.mockRejectedValue(noCloneError)
    platform.cloneTarget.mockRejectedValue(new Error('read clone root: permission denied'))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(noCloneError.message)
    expect(screen.queryByRole('button', { name: /^Clone into/ })).not.toBeInTheDocument()
  })

  it('offers to clone a repository with no local clone, then continues the launch', async () => {
    platform.launch.mockRejectedValueOnce(noCloneError).mockResolvedValueOnce(launchResult())
    platform.cloneTarget.mockResolvedValue(missingClone)
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('No local clone of acme-org/example-repo.')
    expect(platform.cloneRepo).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Clone into ~/projects/example-repo' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Clone acme-org/example-repo?' }))

    expect(dialog.getByText('gh repo clone acme-org/example-repo ~/projects/example-repo')).toBeInTheDocument()
    expect(platform.cloneRepo).not.toHaveBeenCalled()

    await userEvent.click(dialog.getByRole('button', { name: 'Clone and open' }))

    expect(platform.cloneRepo).toHaveBeenCalledWith(1)
    expect(await screen.findByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(platform.launch).toHaveBeenCalledTimes(2)
    expect(platform.launch).toHaveBeenNthCalledWith(1, { ...defaultRequest, profile: 'work' })
    expect(platform.launch).toHaveBeenNthCalledWith(2, { ...defaultRequest, profile: 'work' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the clone running', async () => {
    platform.launch.mockRejectedValueOnce(noCloneError)
    platform.cloneTarget.mockResolvedValue(missingClone)
    platform.cloneRepo.mockReturnValue(new Promise(() => undefined))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Clone into ~/projects/example-repo' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clone and open' }))

    expect(screen.getByRole('button', { name: 'Cloning…' })).toBeDisabled()
    expect(optionsButton()).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Cloning acme-org/example-repo into ~/projects/example-repo…')
    expect(platform.launch).toHaveBeenCalledTimes(1)
  })

  it('clones nothing when the clone is cancelled', async () => {
    platform.launch.mockRejectedValue(noCloneError)
    platform.cloneTarget.mockResolvedValue(missingClone)
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Clone into ~/projects/example-repo' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.cloneRepo).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Clone into ~/projects/example-repo' })).toBeInTheDocument()
  })

  it('offers no clone where something already is', async () => {
    platform.launch.mockRejectedValue(noCloneError)
    platform.cloneTarget.mockResolvedValue({ ...missingClone, taken: true })
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'No local clone of acme-org/example-repo, and ~/projects/example-repo already exists, so it can’t be cloned there.',
    )
    expect(screen.queryByRole('button', { name: /^Clone into/ })).not.toBeInTheDocument()
  })

  it('shows a failed clone and launches nothing', async () => {
    platform.launch.mockRejectedValue(noCloneError)
    platform.cloneTarget.mockResolvedValue(missingClone)
    platform.cloneRepo.mockRejectedValue(new Error('clone acme-org/example-repo: GraphQL: Could not resolve'))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Clone into ~/projects/example-repo' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clone and open' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'clone acme-org/example-repo: GraphQL: Could not resolve',
    )
    expect(platform.launch).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
  })

  it('offers the prompts for the item’s kind, the default marked and focused', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()
    const select = menu.getByRole('combobox', { name: 'Prompt' })
    const options = within(select).getAllByRole('option')

    expect(options.map((o) => o.getAttribute('value'))).toEqual(['3', '2', '1', '5'])
    expect(within(select).getByRole('option', { name: 'Review · app default' })).toHaveProperty('selected', true)
    expect(select).toHaveFocus()
    expect(select).toHaveAccessibleDescription('Review a pull request')
    expect(menu.queryByText('Work on it')).not.toBeInTheDocument()
  })

  it('offers issue prompts on an issue', async () => {
    const issue = item({ id: 9, kind: 'issue', number: 12 })
    const res = itemResolution({
      itemId: 9,
      kind: 'issue',
      prompt: { ...resolution().prompt, id: 4, nickname: 'Work on it', inheritedId: 4 },
    })
    render(<LaunchControls {...props({ item: issue, resolution: res })} />)

    expect(optionsButton()).toHaveAccessibleName('Launch options: Work on it · default')

    const select = (await openMenu()).getByRole('combobox', { name: 'Prompt' })

    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.getAttribute('value')),
    ).toEqual(['4', '5'])
  })

  it('re-resolves under another prompt and launches with the choices for this launch only', async () => {
    platform.resolveLaunch.mockResolvedValue(
      resolution({
        prompt: { ...resolution().prompt, id: 2, nickname: 'Quick look', source: 'launch' },
        model: { value: 'sonnet', source: 'prompt', inherited: 'sonnet', inheritedSource: 'prompt' },
      }),
    )
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')

    expect(platform.resolveLaunch).toHaveBeenCalledWith({ ...defaultRequest, promptId: 2 })
    expect(optionsButton()).toHaveAccessibleName('Launch options: Quick look · default, changed for this launch')
    expect(await menu.findByRole('option', { name: 'Default · Sonnet (prompt)' })).toBeInTheDocument()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.selectOptions(menu.getByRole('combobox', { name: 'Effort' }), 'max')
    await userEvent.click(menu.getByRole('checkbox', { name: /Start in a worktree/ }))
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({
      ...defaultRequest,
      promptId: 2,
      profile: 'work',
      effort: 'max',
      worktree: false,
    })

    await waitFor(() => {
      expect(optionsButton()).toHaveAccessibleName('Launch options: Review · default')
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('treats picking the default again as no choice', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.selectOptions(menu.getByRole('combobox', { name: 'Effort' }), 'low')
    expect(optionsButton()).toHaveAccessibleName(/changed for this launch/)

    await userEvent.selectOptions(menu.getByRole('combobox', { name: 'Effort' }), 'high')
    expect(optionsButton()).toHaveAccessibleName('Launch options: Review · default')

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('radio', { name: 'default' }))
    expect(optionsButton()).toHaveAccessibleName('Launch options: Review · default')
  })

  it('shows where each default comes from', async () => {
    render(
      <LaunchControls
        {...props({
          resolution: itemResolution({
            model: { value: 'opus', source: 'org', inherited: 'opus', inheritedSource: 'org' },
            warnings: ['The repo default profile "gone" no longer exists, so it was skipped.'],
          }),
        })}
      />,
    )

    const menu = await openMenu()

    expect(menu.getByRole('option', { name: 'Default · Opus (org default)' })).toBeInTheDocument()
    expect(menu.getByRole('option', { name: 'Default · High (profile)' })).toBeInTheDocument()
    expect(menu.getByRole('checkbox', { name: /Start in a worktree/ })).toBeChecked()
    expect(menu.getByRole('checkbox', { name: /Start in a worktree/ }).closest('label')).toHaveTextContent(
      'app default',
    )
    expect(menu.getByRole('checkbox', { name: /Start in plan mode/ }).closest('label')).toHaveTextContent('profile')
    expect(menu.getByText(/no longer exists/)).toBeInTheDocument()
  })

  it('keeps an outside contributor’s item in plan mode until the override', async () => {
    render(
      <LaunchControls {...props({ item: item({ trusted: false }), resolution: itemResolution({ trusted: false }) })} />,
    )

    const menu = await openMenu()
    const plan = menu.getByRole('checkbox', { name: /Start in plan mode/ })

    expect(plan).toBeChecked()
    expect(plan).toBeDisabled()
    expect(plan.closest('label')).toHaveTextContent('outside contributor')

    await userEvent.click(menu.getByRole('checkbox', { name: 'Use profile permissions for this item' }))

    expect(plan).toBeEnabled()
    expect(plan).not.toBeChecked()

    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith({ ...defaultRequest, fullAccessOverride: true })
  })

  it('saves the choices as the repository’s defaults', async () => {
    const onDefaultsSaved = vi.fn(() => Promise.resolve())
    render(<LaunchControls {...props({ onDefaultsSaved })} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith({
      owner: 'acme-org',
      name: 'example-repo',
      agent: '',
      profiles: { claude: 'work' },
      prPromptId: null,
      issuePromptId: null,
      models: {},
      effort: '',
      planMode: null,
      worktree: null,
      extraFlags: {},
    })
    expect(await menu.findByText('Saved as the default for acme-org/example-repo.')).toBeInTheDocument()
    expect(onDefaultsSaved).toHaveBeenCalledOnce()
    expect(optionsButton()).toHaveAccessibleName(/^Launch options: Review · default$/)
  })

  it('saves the choices over the org’s stored defaults', async () => {
    const stored = launchDefaults({
      effort: 'max',
      extraFlags: { claude: '--advisor fable' },
      extraArgs: { claude: ['--advisor', 'fable'] },
    })
    render(<LaunchControls {...props({ launchDefaults: [stored] })} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')
    await userEvent.click(menu.getByRole('button', { name: 'all of acme-org' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: 'acme-org',
        name: '',
        profiles: { claude: 'work' },
        prPromptId: 2,
        effort: 'max',
        extraFlags: { claude: '--advisor fable' },
      }),
    )
  })

  it('explains a saved choice the chosen prompt overrides', async () => {
    platform.resolveLaunch.mockResolvedValue(
      resolution({
        prompt: { ...resolution().prompt, id: 2, nickname: 'Quick look', source: 'launch' },
        model: { value: 'sonnet', source: 'prompt', inherited: 'sonnet', inheritedSource: 'prompt' },
      }),
    )
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')
    expect(await menu.findByRole('option', { name: 'Default · Sonnet (prompt)' })).toBeInTheDocument()

    await userEvent.selectOptions(menu.getByRole('combobox', { name: 'Model' }), 'opus')
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))

    const status = await menu.findByText(/Saved as the default for acme-org\/example-repo\./)
    expect(status).toHaveTextContent('“Quick look” sets its own model, so it applies only with other prompts.')
    expect(status).toHaveClass('text-warning')
  })

  it('shows settings that could not be worked out for a choice', async () => {
    platform.resolveLaunch.mockRejectedValue(new Error('profile not found: "work"'))
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    expect(
      await menu.findByText(/Couldn’t work out the settings for this choice: profile not found/),
    ).toBeInTheDocument()
  })

  it('shows no earlier choice’s settings once a later choice cannot be worked out', async () => {
    platform.resolveLaunch
      .mockResolvedValueOnce(
        resolution({
          prompt: { ...resolution().prompt, id: 2, nickname: 'Quick look', source: 'launch' },
          model: { value: 'sonnet', source: 'prompt', inherited: 'sonnet', inheritedSource: 'prompt' },
        }),
      )
      .mockRejectedValue(new Error('profile not found: "work"'))
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')
    expect(await menu.findByRole('option', { name: 'Default · Sonnet (prompt)' })).toBeInTheDocument()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    expect(
      await menu.findByText(/Couldn’t work out the settings for this choice: profile not found/),
    ).toBeInTheDocument()
    // The menu falls back to the item's own settings, not Quick look's.
    expect(menu.queryByRole('option', { name: 'Default · Sonnet (prompt)' })).not.toBeInTheDocument()
  })

  it('asks for a choice before saving defaults, and shows a failed save', async () => {
    platform.saveLaunchDefaults.mockRejectedValue(new Error('the launch defaults can’t be saved'))
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    expect(menu.getByRole('button', { name: 'example-repo' })).toBeDisabled()
    expect(platform.saveLaunchDefaults).not.toHaveBeenCalled()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))
    expect(await menu.findByRole('alert')).toHaveTextContent('the launch defaults can’t be saved')
    expect(menu.getByRole('alert')).toHaveClass('text-danger')
    expect(menu.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('undoes a save where nothing was stored by removing it, and brings the choices back', async () => {
    const onDefaultsSaved = vi.fn(() => Promise.resolve())
    render(<LaunchControls {...props({ onDefaultsSaved })} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))
    expect(await menu.findByText('Saved as the default for acme-org/example-repo.')).toBeInTheDocument()

    await userEvent.click(menu.getByRole('button', { name: 'Undo' }))

    expect(platform.removeLaunchDefaults).toHaveBeenCalledWith('acme-org', 'example-repo')
    expect(await menu.findByRole('status')).toHaveTextContent('Undone: the defaults are as they were.')
    expect(onDefaultsSaved).toHaveBeenCalledTimes(2)
    expect(menu.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    // The choice comes back, so it can be saved for the owner instead.
    expect(menu.getByRole('radio', { name: 'work' })).toBeChecked()
    expect(menu.getByRole('button', { name: 'all of acme-org' })).toBeEnabled()
  })

  it('undoes a save over stored defaults by saving them back', async () => {
    const stored = launchDefaults({ effort: 'max' })
    render(<LaunchControls {...props({ launchDefaults: [stored] })} />)

    const menu = await openMenu()

    await choosePrompt(menu, 'Quick look')
    await userEvent.click(menu.getByRole('button', { name: 'all of acme-org' }))
    await userEvent.click(await menu.findByRole('button', { name: 'Undo' }))

    expect(platform.saveLaunchDefaults).toHaveBeenLastCalledWith(
      expect.objectContaining({ owner: 'acme-org', name: '', effort: 'max', prPromptId: stored.prPromptId }),
    )
    expect(platform.removeLaunchDefaults).not.toHaveBeenCalled()
  })

  it('shows a failed undo, and still offers it', async () => {
    platform.removeLaunchDefaults.mockRejectedValueOnce(new Error('database is locked'))
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))
    await userEvent.click(await menu.findByRole('button', { name: 'Undo' }))

    expect(await menu.findByRole('alert')).toHaveTextContent('database is locked')
    expect(menu.getByRole('button', { name: 'Undo' })).toBeEnabled()
  })

  it('previews exactly what the launch runs', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('button', { name: 'Preview' }))

    expect(await menu.findByText(/Review pull request #7/)).toBeInTheDocument()
    expect(platform.previewLaunch).toHaveBeenCalledWith(defaultRequest)
    expect(
      menu.getByText(`Creates ~/projects/.${brand.slug}/worktrees/example-repo/pr-7 from the pull request’s head`),
    ).toBeInTheDocument()
    expect(menu.getByText(`~/projects/.${brand.slug}/worktrees/example-repo/pr-7`)).toBeInTheDocument()
    expect(menu.getByText('unset CLAUDE_CONFIG_DIR')).toBeInTheDocument()
    expect(menu.getByText(/claude <prompt> --dangerously-skip-permissions/)).toBeInTheDocument()

    platform.previewLaunch.mockRejectedValue(new Error('no local clone of acme-org/example-repo under ~/projects'))
    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    expect(await menu.findByRole('alert')).toHaveTextContent('no local clone')
  })

  it('says it is planning while a changed choice is planned, rather than show the last plan', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('button', { name: 'Preview' }))
    expect(await menu.findByText(/Review pull request #7/)).toBeInTheDocument()

    let finish: (preview: LaunchPreview) => void = () => undefined
    platform.previewLaunch.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    expect(await menu.findByText('Planning the launch…')).toBeInTheDocument()
    expect(menu.queryByText(/Review pull request #7/)).not.toBeInTheDocument()

    await act(async () => {
      finish(launchPreview({ prompt: 'Review it as work\n' }))
      await Promise.resolve()
    })

    expect(await menu.findByText(/Review it as work/)).toBeInTheDocument()
  })

  it('opens the Prompts page from the menu', async () => {
    const onManagePrompts = vi.fn()
    render(<LaunchControls {...props({ onManagePrompts })} />)

    await userEvent.click((await openMenu()).getByRole('button', { name: 'Manage' }))

    expect(onManagePrompts).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('explains an item no prompt applies to', async () => {
    const onManagePrompts = vi.fn()
    const res = itemResolution({
      problem: 'No prompt applies to this kind of item; add one on the Prompts tab (issues).',
    })
    render(<LaunchControls {...props({ resolution: res, onManagePrompts })} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeDisabled()
    expect(optionsButton()).toHaveAccessibleName('Launch options: No prompt')

    const menu = await openMenu()

    expect(menu.getByText(/No prompt applies/)).toBeInTheDocument()
    // With no options to choose, focus starts in the dialog itself.
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toContainElement(
      document.activeElement as HTMLElement,
    )

    await userEvent.click(menu.getByRole('button', { name: 'Open the Prompts page' }))
    expect(onManagePrompts).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows the running session’s terminal rather than starting another', async () => {
    render(<LaunchControls {...props({ running: agentSession() })} />, { wrapper: at() })

    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    const show = screen.getByRole('button', { name: 'Show in terminal' })
    expect(show).toHaveAttribute('title', expect.stringContaining('Claude needs approval: show its terminal tab'))

    await userEvent.click(show)

    expect(platform.focusSession).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')
    expect(platform.launch).not.toHaveBeenCalled()

    platform.focusSession.mockRejectedValueOnce(new Error('Couldn’t find the session’s tab.'))
    await userEvent.click(show)

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t find the session’s tab.')
  })

  it('waits for a starting session’s terminal before it can show it', () => {
    render(<LaunchControls {...props({ running: agentSession({ state: 'starting', canFocus: false }) })} />, {
      wrapper: at(),
    })

    const show = screen.getByRole('button', { name: 'Show in terminal' })
    expect(show).toBeDisabled()
    expect(show).toHaveAttribute('title', expect.stringContaining('is starting'))
  })

  it('asks before starting a second session where one is running, and can go to it instead', async () => {
    const hold = runningHold()
    platform.launch
      .mockResolvedValueOnce(launchResult({ hold }))
      .mockResolvedValueOnce(launchResult({ hold }))
      .mockResolvedValueOnce(launchResult())
    render(<LaunchControls {...props({ running: agentSession() })} />, { wrapper: at() })

    const menu = await openMenu()
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'A session is already running here' }))
    expect(dialog.getByRole('list', { name: 'Sessions running here' })).toHaveTextContent(
      'Claude needs approval · 2m ago',
    )
    expect(dialog.getByText(/working in worktree pr-7 at once can undo each other’s changes/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()

    await userEvent.click(dialog.getByRole('button', { name: 'Show in terminal' }))

    expect(platform.focusSession).toHaveBeenCalledWith(hold.running[0]?.sessionId)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.launch).toHaveBeenCalledOnce()

    await userEvent.click((await openMenu()).getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Start another' }))

    expect(platform.launch).toHaveBeenLastCalledWith({ ...defaultRequest, alongside: hold.key })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Show in terminal' })).toBeEnabled()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('names the item a session running in the same clone was started on', async () => {
    const base = runningHold()
    const running = base.running.map((r) => ({ ...r, number: 12, worktree: '' }))
    platform.launch.mockResolvedValueOnce(launchResult({ hold: { ...base, worktree: '', running } }))
    render(<LaunchControls {...props()} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'A session is already running here' }))
    expect(dialog.getByRole('list', { name: 'Sessions running here' })).toHaveTextContent(
      'Claude needs approval on acme-org/example-repo#12',
    )
  })

  it('confirms a running session and a pull request’s agent files apart', async () => {
    const running = runningHold()
    const files = launchHold()
    platform.launch
      .mockResolvedValueOnce(launchResult({ hold: running }))
      .mockResolvedValueOnce(launchResult({ hold: files }))
      .mockResolvedValueOnce(launchResult())
    render(<LaunchControls {...props()} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Start another' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Open anyway' }))

    expect(platform.launch).toHaveBeenNthCalledWith(2, { ...defaultRequest, alongside: running.key })
    expect(platform.launch).toHaveBeenNthCalledWith(3, {
      ...defaultRequest,
      alongside: running.key,
      confirmed: files.key,
    })
  })

  it('asks before opening a pull request that changes agent configuration', async () => {
    const hold = launchHold()
    platform.launch.mockResolvedValueOnce(launchResult({ hold })).mockResolvedValueOnce(launchResult())
    render(<LaunchControls {...props({ item: item({ trusted: false, trust: 'outside' }) })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'This pull request changes agent configuration' }))
    expect(dialog.getByText(/starts in worktree pr-7, at the pull request’s head/)).toBeInTheDocument()
    expect(dialog.getByText(/run commands on your Mac, even in plan or read-only mode/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    // Each file opens its diff on GitHub.
    const files = within(dialog.getByRole('list', { name: 'Agent configuration it changes' }))
    expect(files.getByText('added')).toBeInTheDocument()
    await userEvent.click(files.getByRole('button', { name: /\.claude\/settings\.json/ }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/pull/7/files#diff-aaaa',
    )

    await userEvent.click(dialog.getByRole('button', { name: 'Open anyway' }))

    expect(platform.launch).toHaveBeenNthCalledWith(1, { ...defaultRequest, fullAccessOverride: false })
    expect(platform.launch).toHaveBeenNthCalledWith(2, { ...defaultRequest, confirmed: hold.key })
    expect(await screen.findByRole('button', { name: 'Open in Claude' })).toBeEnabled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens nothing when a held launch is cancelled', async () => {
    platform.launch.mockResolvedValueOnce(launchResult({ hold: launchHold({ atHead: false, worktree: '' }) }))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByText(/If the session checks the pull request out, Claude loads these files/)).toBeInTheDocument()
    expect(dialog.getByText(/every later session in ~\/projects\/example-repo/)).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.launch).toHaveBeenCalledOnce()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeEnabled()
  })

  it('asks again when the pull request moved after the first question', async () => {
    const first = launchHold()
    const second = launchHold({ key: 'agentFiles:bbbb', head: 'bbbb' })
    platform.launch
      .mockResolvedValueOnce(launchResult({ hold: first }))
      .mockResolvedValueOnce(launchResult({ hold: second }))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Open anyway' }))

    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(platform.launch).toHaveBeenLastCalledWith({ ...defaultRequest, confirmed: first.key })
  })

  it('explains a pull request whose files could not all be checked', async () => {
    platform.launch.mockResolvedValueOnce(
      launchResult({ hold: launchHold({ reason: 'unchecked', files: [], checked: 3000, total: 4210 }) }),
    )
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Its files couldn’t all be checked' }))
    expect(dialog.getByText(/GitHub listed only 3,000 of its 4,210 changed files/)).toBeInTheDocument()
    expect(dialog.queryByRole('list')).not.toBeInTheDocument()
  })

  it('names why a check failed', async () => {
    platform.launch.mockResolvedValueOnce(
      launchResult({ hold: launchHold({ reason: 'unchecked', files: [], error: 'GitHub answered 502' }) }),
    )
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByText(/GitHub didn’t list its files: GitHub answered 502\./)).toBeInTheDocument()
  })
})

describe('LaunchControls with more than one agent', () => {
  // codexResolution is the item's settings resolved for Codex, as the launch menu's switch
  // asks for them.
  const codexResolution = resolution({
    agent: codexAgent(),
    agentSource: 'launch',
    profile: { value: 'default', source: 'app', inherited: 'default', inheritedSource: 'app' },
  })

  it('switches the agent for one launch, with that agent’s own profiles and models', async () => {
    platform.resolveLaunch.mockResolvedValue(codexResolution)
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    expect(menu.getByRole('radio', { name: 'Claude' })).toBeChecked()
    expect(menu.queryByRole('radio', { name: /^alt/ })).not.toBeInTheDocument()

    await userEvent.click(menu.getByRole('radio', { name: 'Codex' }))

    expect(await menu.findByRole('radio', { name: /^alt.*not signed in$/ })).toBeInTheDocument()
    expect(menu.queryByRole('radio', { name: 'work' })).not.toBeInTheDocument()
    expect(menu.getByRole('option', { name: 'GPT-6-Sol' })).toBeInTheDocument()
    expect(menu.getByRole('option', { name: 'Ultra' })).toBeInTheDocument()
    expect(menu.getByRole('checkbox', { name: /Start read-only/ })).toBeInTheDocument()
    // The row's button and the menu's own both follow the agent chosen.
    expect(screen.getByTitle('Open in Codex with “Review” as default')).toBeEnabled()

    await userEvent.click(menu.getByRole('button', { name: 'Open in Codex' }))

    expect(platform.launch).toHaveBeenCalledWith({ ...defaultRequest, agent: 'codex' })
    expect(platform.resolveLaunch).toHaveBeenCalledWith(expect.objectContaining({ agent: 'codex' }))
  })

  it('launches with the item’s own agent while the agents are still loading', async () => {
    render(<LaunchControls {...props({ agents: [] })} />)

    const menu = await openMenu()

    // With no agents to choose from, the menu offers the resolved agent's models alone.
    expect(menu.queryByRole('radiogroup', { name: 'Agent' })).not.toBeInTheDocument()
    expect(menu.getByRole('option', { name: 'Opus' })).toBeInTheDocument()

    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(defaultRequest)
  })

  it('chooses a model for one launch, and none again on picking the default', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()
    const model = menu.getByRole('combobox', { name: 'Model' })

    await userEvent.selectOptions(model, 'opus')
    expect(model).toHaveValue('opus')

    await userEvent.selectOptions(model, '')
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenLastCalledWith(defaultRequest)
  })

  it('starts in the agent’s own planning mode, and back to the default when unchecked', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()
    const plan = menu.getByRole('checkbox', { name: /Start in plan mode/ })

    await userEvent.click(plan)
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenLastCalledWith({ ...defaultRequest, planMode: true })

    const again = await openMenu()
    await userEvent.click(again.getByRole('checkbox', { name: /Start in plan mode/ }))
    await userEvent.click(again.getByRole('checkbox', { name: /Start in plan mode/ }))
    await userEvent.click(again.getByRole('button', { name: 'Open in Claude' }))

    // Unchecked again, it is the resolved setting, so nothing is chosen.
    expect(platform.launch).toHaveBeenLastCalledWith(defaultRequest)
  })

  it('goes back to the item’s own agent, and clears the other agent’s profile', async () => {
    platform.resolveLaunch.mockResolvedValue(codexResolution)
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('radio', { name: 'Codex' }))
    await userEvent.click(menu.getByRole('radio', { name: 'Claude' }))
    await userEvent.click(menu.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(defaultRequest)
  })

  it('offers an agent that is not installed only with the reason', async () => {
    const all = agents().map((a) => (a.id === 'codex' ? { ...a, installed: false, path: '' } : a))
    render(<LaunchControls {...props({ agents: all })} />)

    const menu = await openMenu()
    const option = menu.getByRole('radio', { name: 'Codex' })

    expect(option).toBeDisabled()
    expect(option.closest('label')).toHaveAttribute('title', 'Codex isn’t installed (codex wasn’t found)')
  })

  it('previews a Codex launch: its own home, a resumed worktree, and the prompt last', async () => {
    platform.resolveLaunch.mockResolvedValue(codexResolution)
    platform.previewLaunch.mockResolvedValue(
      launchPreview({
        resolution: codexResolution,
        worktree: {
          name: 'pr-7',
          path: `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
          branch: 'worktree-pr-7',
          action: 'resume',
          base: '',
          dirty: 2,
        },
        env: ['export CODEX_HOME=~/.codex_alt'],
        command: 'codex --sandbox read-only --ask-for-approval on-request -- <prompt>',
        permission: 'read-only',
      }),
    )
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: 'Codex' }))
    await userEvent.click(menu.getByRole('button', { name: 'Preview' }))

    expect(
      await menu.findByText(`Resumes ~/projects/.${brand.slug}/worktrees/example-repo/pr-7 · 2 uncommitted files`),
    ).toBeInTheDocument()
    expect(menu.getByText('export CODEX_HOME=~/.codex_alt')).toBeInTheDocument()
    expect(menu.getByText('codex --sandbox read-only --ask-for-approval on-request -- <prompt>')).toBeInTheDocument()
    expect(platform.previewLaunch).toHaveBeenLastCalledWith({ ...defaultRequest, agent: 'codex' })
  })

  it('greys Codex’s effort out unless the settings pass it, and offers Fast mode for one launch', async () => {
    platform.resolveLaunch.mockResolvedValue(
      resolution({
        ...codexResolution,
        effort: { value: '', source: 'app', inherited: '', inheritedSource: 'app' },
        effortOff: true,
        fast: { value: false, source: 'app', inherited: false, inheritedSource: 'app' },
      }),
    )
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    expect(menu.getByRole('combobox', { name: 'Effort' })).toBeEnabled()
    expect(menu.queryByRole('checkbox', { name: /Fast mode/ })).not.toBeInTheDocument()

    await userEvent.click(menu.getByRole('radio', { name: 'Codex' }))

    const effort = await menu.findByRole('combobox', { name: 'Effort' })
    expect(effort).toBeDisabled()
    expect(effort).toHaveDisplayValue('Codex’s own')
    expect(effort).toHaveAccessibleDescription(/change it in the session with \/model/)

    const fast = menu.getByRole('checkbox', { name: /Fast mode/ })
    expect(fast).not.toBeChecked()

    await userEvent.click(fast)

    expect(fast).toBeChecked()
    expect(fast).toHaveAccessibleDescription(/can’t show when it waits for you/)
    expect(menu.getByText('this launch')).toBeInTheDocument()

    await userEvent.click(menu.getByRole('button', { name: 'Open in Codex' }))

    expect(platform.launch).toHaveBeenLastCalledWith({ ...defaultRequest, agent: 'codex', fast: true })
  })
})
