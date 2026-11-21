import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { noChoices } from '../lib/launchChoices'
import {
  agents,
  launchPreview,
  profile,
  prompt,
  prompts,
  repoLaunchPreview,
  repoResolution,
  resolution,
} from '../test/fixtures'
import { LaunchMenu, type LaunchMenuProps } from './LaunchMenu'
import type { LaunchSubject } from './LaunchPreviewSection'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// prSubject is the trusted pull request the menu launches on, as LaunchControls makes it
// the launch's subject.
const prSubject: LaunchSubject = {
  kind: 'pr',
  trusted: true,
  key: '[1,"",false]',
  preview: () => Promise.resolve(launchPreview()),
}

function props(overrides: Partial<LaunchMenuProps> = {}): LaunchMenuProps {
  return {
    subject: prSubject,
    resolved: resolution(),
    choices: noChoices,
    onChoices: vi.fn(),
    agents: agents(),
    profiles: [profile()],
    prompts: prompts(),
    agentName: 'Claude',
    onLaunch: vi.fn(),
    busy: false,
    onSave: vi.fn(),
    saving: false,
    status: null,
    onUndo: null,
    resolveError: '',
    onManagePrompts: vi.fn(),
    onCancel: vi.fn(),
    ...overrides,
  }
}

describe('LaunchMenu', () => {
  it('chooses the prompt from a select, with the chosen one’s description under it', async () => {
    const onChoices = vi.fn()
    const same = prompt({ id: 8, nickname: 'Fix merge conflicts', name: 'Fix merge conflicts' })
    const { rerender } = render(<LaunchMenu {...props({ onChoices, prompts: [...prompts(), same] })} />)

    const select = screen.getByRole('combobox', { name: 'Prompt' })
    expect(select).toHaveValue('1')
    expect(select).toHaveAccessibleDescription('Review a pull request')

    await userEvent.selectOptions(select, 'Quick look')
    expect(onChoices).toHaveBeenLastCalledWith({ ...noChoices, promptId: 2 })

    rerender(
      <LaunchMenu {...props({ onChoices, prompts: [...prompts(), same], choices: { ...noChoices, promptId: 8 } })} />,
    )
    expect(select).toHaveValue('8')
    expect(select).not.toHaveAccessibleDescription(/./)
  })

  it('waits for a choice before it saves defaults, then saves in one click with an undo', async () => {
    const onSave = vi.fn()
    const { rerender } = render(<LaunchMenu {...props({ onSave })} />)

    expect(screen.getByText('Change a choice above to save it as the default for')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'example-repo' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'all of acme-org' })).toBeDisabled()

    rerender(<LaunchMenu {...props({ onSave, choices: { ...noChoices, profile: 'work' } })} />)

    expect(screen.getByText('Save these choices as the default for')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'all of acme-org' }))
    expect(onSave).toHaveBeenCalledWith('org')

    const onUndo = vi.fn()
    rerender(
      <LaunchMenu
        {...props({
          onSave,
          onUndo,
          status: { tone: 'success', text: 'Saved as the default for every acme-org repository.' },
        })}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('Saved as the default for every acme-org repository.')
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(onUndo).toHaveBeenCalledOnce()

    rerender(<LaunchMenu {...props({ onUndo, saving: true, status: { tone: 'success', text: 'Saved.' } })} />)
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  })

  it('launches, or cancels without launching', async () => {
    const onLaunch = vi.fn()
    const onCancel = vi.fn()
    render(<LaunchMenu {...props({ onLaunch, onCancel })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onLaunch).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    expect(onLaunch).toHaveBeenCalledOnce()
  })

  it('holds Open back while a launch or a clone runs', async () => {
    const onLaunch = vi.fn()
    render(<LaunchMenu {...props({ onLaunch, busy: true })} />)

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open).toBeDisabled()

    await userEvent.click(open)
    expect(onLaunch).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled()
  })
})

describe('LaunchMenu for a repository', () => {
  // upgrade and majors are the prompts for repositories the Deps page's Upgrade starts.
  const upgrade = prompt({
    id: 21,
    name: 'Upgrade dependencies',
    nickname: 'Upgrade deps',
    appliesTo: 'repo',
    starterKey: 'deps-minor',
  })
  const majors = prompt({
    id: 22,
    name: 'Upgrade dependencies, majors too',
    nickname: 'Upgrade majors too',
    appliesTo: 'repo',
    starterKey: 'deps-major',
  })
  // preview plans acme-org/api-server's upgrade, as the Deps page's sheet does.
  const preview = vi.fn(() => Promise.resolve(repoLaunchPreview()))
  const repoSubject: LaunchSubject = { kind: '', trusted: true, key: 'acme-org/api-server', preview }

  it('offers only the prompts for repositories, shows the worktree always on, and saves nothing', async () => {
    const onChoices = vi.fn()
    const onLaunch = vi.fn()
    render(
      <LaunchMenu
        {...props({
          subject: repoSubject,
          resolved: repoResolution(),
          prompts: [...prompts(), upgrade, majors],
          onChoices,
          onLaunch,
        })}
      />,
    )

    const select = screen.getByRole('combobox', { name: 'Prompt' })
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Upgrade deps', 'Upgrade majors too'])
    expect(select).toHaveValue('21')
    expect(select).toHaveAccessibleDescription('Upgrade dependencies')

    // Every pick is the launch's own: a repository inherits no prompt.
    await userEvent.selectOptions(select, 'Upgrade majors too')
    expect(onChoices).toHaveBeenLastCalledWith({ ...noChoices, promptId: 22 })

    const worktree = screen.getByRole('checkbox', { name: /Start in a worktree/ })
    expect(worktree).toBeChecked()
    expect(worktree).toBeDisabled()
    expect(screen.getByText('always, for a repository')).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /Use profile permissions/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/as the default for/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'api-server' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Open in Claude' }))
    expect(onLaunch).toHaveBeenCalledOnce()
  })

  it('previews the launch through its subject', async () => {
    const choices = { ...noChoices, promptId: 21 }
    render(<LaunchMenu {...props({ subject: repoSubject, resolved: repoResolution(), choices })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))

    expect(await screen.findByText(/Upgrade the Go and JavaScript dependencies/)).toBeInTheDocument()
    expect(screen.getByText(/^Creates .*deps-20260615 from origin\/master$/)).toBeInTheDocument()
    expect(preview).toHaveBeenCalledExactlyOnceWith(choices)
  })
})
