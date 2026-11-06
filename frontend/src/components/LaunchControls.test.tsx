import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  item,
  itemResolution,
  launchDefaults,
  launchPreview,
  launchResult,
  profile,
  prompts,
  resolution,
} from '../test/fixtures'
import { LaunchControls, type LaunchControlsProps } from './LaunchControls'

const platform = vi.hoisted(() => ({
  launch: vi.fn(),
  resolveLaunch: vi.fn(),
  previewLaunch: vi.fn(),
  saveLaunchDefaults: vi.fn(),
}))

vi.mock('../platform', () => platform)

const profiles = [profile(), profile({ name: 'work', configDir: '~/.claude_work', folder: '~/.claude_work' })]

// defaultRequest is a launch with nothing chosen: the item's resolved settings.
const defaultRequest = {
  itemId: 1,
  profile: '',
  promptId: 0,
  model: null,
  effort: null,
  planMode: null,
  worktree: null,
  fullAccessOverride: false,
}

function props(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return {
    item: item(),
    resolution: itemResolution(),
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

beforeEach(() => {
  vi.clearAllMocks()
  platform.launch.mockResolvedValue(launchResult())
  platform.resolveLaunch.mockResolvedValue(resolution())
  platform.previewLaunch.mockResolvedValue(launchPreview())
  platform.saveLaunchDefaults.mockResolvedValue(launchDefaults())
})

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

  it('waits for the launch settings before it can launch', () => {
    render(<LaunchControls {...props({ resolution: null })} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeDisabled()
    expect(optionsButton()).toBeDisabled()
    expect(optionsButton()).toHaveTextContent('…')
  })

  it('launches with the resolved settings in one click', async () => {
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledWith(defaultRequest)
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Opened “Review” in iterm2: ~/projects/example-repo (worktree pr-7) · default · full access',
    )
  })

  it('shows why a launch failed', async () => {
    platform.launch.mockRejectedValue(new Error('no local clone of acme-org/example-repo'))
    render(<LaunchControls {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('no local clone of acme-org/example-repo')
  })

  it('offers the prompts for the item’s kind, the default marked and focused', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()
    const group = within(menu.getByRole('radiogroup', { name: 'Prompt' }))

    expect(group.getAllByRole('radio').map((r) => r.getAttribute('value'))).toEqual(['3', '2', '1', '5'])
    expect(group.getByRole('radio', { name: /Review/ })).toBeChecked()
    expect(group.getByRole('radio', { name: /Review/ })).toHaveFocus()
    expect(group.getByRole('radio', { name: /Review/ }).closest('label')).toHaveTextContent('app default')
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

    const group = within((await openMenu()).getByRole('radiogroup', { name: 'Prompt' }))

    expect(group.getAllByRole('radio').map((r) => r.getAttribute('value'))).toEqual(['4', '5'])
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

    await userEvent.click(menu.getByRole('radio', { name: /Quick look/ }))

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
      profile: 'work',
      prPromptId: null,
      issuePromptId: null,
      model: '',
      effort: '',
      planMode: null,
      worktree: null,
      extraFlags: '',
    })
    expect(await menu.findByText('Saved as the default for acme-org/example-repo.')).toBeInTheDocument()
    expect(onDefaultsSaved).toHaveBeenCalledOnce()
    expect(optionsButton()).toHaveAccessibleName(/^Launch options: Review · default$/)
  })

  it('saves the choices over the org’s stored defaults', async () => {
    const stored = launchDefaults({ effort: 'max', extraFlags: '--advisor fable', extraArgs: ['--advisor', 'fable'] })
    render(<LaunchControls {...props({ launchDefaults: [stored] })} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('radio', { name: /Quick look/ }))
    await userEvent.click(menu.getByRole('button', { name: 'all of acme-org' }))

    expect(platform.saveLaunchDefaults).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: 'acme-org',
        name: '',
        profile: 'work',
        prPromptId: 2,
        effort: 'max',
        extraFlags: '--advisor fable',
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

    await userEvent.click(menu.getByRole('radio', { name: /Quick look/ }))
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

  it('asks for a choice before saving defaults, and shows a failed save', async () => {
    platform.saveLaunchDefaults.mockRejectedValue(new Error('the launch defaults can’t be saved'))
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))
    expect(menu.getByText('Choose something different first.')).toBeInTheDocument()
    expect(platform.saveLaunchDefaults).not.toHaveBeenCalled()

    await userEvent.click(menu.getByRole('radio', { name: 'work' }))
    await userEvent.click(menu.getByRole('button', { name: 'example-repo' }))
    expect(await menu.findByRole('alert')).toHaveTextContent('the launch defaults can’t be saved')
    expect(menu.getByRole('alert')).toHaveClass('text-danger')
  })

  it('previews exactly what the launch runs', async () => {
    render(<LaunchControls {...props()} />)

    const menu = await openMenu()

    await userEvent.click(menu.getByRole('button', { name: 'Preview' }))

    expect(await menu.findByText(/Review pull request #7/)).toBeInTheDocument()
    expect(platform.previewLaunch).toHaveBeenCalledWith(defaultRequest)
    expect(menu.getByText('~/projects/example-repo')).toBeInTheDocument()
    expect(menu.getByText(/claude <prompt> --dangerously-skip-permissions/)).toBeInTheDocument()

    platform.previewLaunch.mockRejectedValue(new Error('no local clone of acme-org/example-repo under ~/projects'))
    await userEvent.click(menu.getByRole('radio', { name: 'work' }))

    expect(await menu.findByRole('alert')).toHaveTextContent('no local clone')
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

    await userEvent.click(menu.getByRole('button', { name: 'Open the Prompts page' }))
    expect(onManagePrompts).toHaveBeenCalledOnce()
  })
})
