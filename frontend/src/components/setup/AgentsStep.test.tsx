import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentInfo, Profile, Settings, TerminalChoice } from '../../platform'
import { nextTask } from '../../test/async'
import { agents, agentVersions, profile, settings, terminalChoices } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { AgentsStep } from './AgentsStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface RigProps {
  stored?: Settings
  list?: AgentInfo[]
  profiles?: Profile[]
  terminals?: TerminalChoice[] | null
  onRediscovered?: () => Promise<void>
}

// Rig holds the settings as the window does, taking each one a setter returns.
function Rig({
  stored = settings(),
  list = agents(),
  profiles = [profile(), profile({ agent: 'codex', folder: '~/.codex' })],
  terminals = terminalChoices(),
  onRediscovered = () => Promise.resolve(),
}: RigProps) {
  const [shown, setShown] = useState(stored)

  return (
    <AgentsStep
      settings={shown}
      onSaved={setShown}
      agents={list}
      profiles={profiles}
      versions={agentVersions()}
      terminals={terminals}
      onRediscovered={onRediscovered}
    />
  )
}

// card is an agent's card.
function card(name: string) {
  return within(screen.getByRole('region', { name }))
}

describe('AgentsStep', () => {
  it('shows each agent with its version and profiles, the default chosen', () => {
    render(<Rig />)

    expect(card('Claude').getByText('claude 2.1.319')).toBeInTheDocument()
    expect(card('Claude').getByRole('radio', { name: 'Use by default' })).toBeChecked()
    expect(card('Codex').getByRole('radio', { name: 'Use by default' })).not.toBeChecked()
    expect(card('Claude').getByRole('list', { name: 'Claude profiles' })).toHaveTextContent('default')
  })

  it('makes another agent the default', async () => {
    render(<Rig />)

    await userEvent.click(card('Codex').getByRole('radio', { name: 'Use by default' }))

    expect(platform.setDefaultAgent).toHaveBeenCalledWith('codex')
    expect(card('Codex').getByRole('radio', { name: 'Use by default' })).toBeChecked()
  })

  it('points to where an agent that isn’t installed comes from', async () => {
    const list = agents().map((a) => (a.id === 'codex' ? { ...a, installed: false } : a))
    render(<Rig list={list} />)

    expect(card('Codex').getByText('Not found')).toBeInTheDocument()
    expect(card('Codex').queryByRole('radio')).not.toBeInTheDocument()

    await userEvent.click(card('Codex').getByRole('button', { name: 'Get Codex' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/openai/codex')
  })

  it('moves a default agent that isn’t installed to one that is, saying so', async () => {
    const list = agents().map((a) => (a.id === 'claude' ? { ...a, installed: false } : a))
    render(<Rig list={list} />)

    expect(await screen.findByText('Claude isn’t installed, so launches start Codex instead.')).toBeInTheDocument()
    expect(platform.setDefaultAgent).toHaveBeenCalledExactlyOnceWith('codex')
  })

  it('offers iTerm2, Warp, then Terminal, and keeps iTerm2 when it’s installed', async () => {
    render(<Rig />)
    await nextTask()

    expect(
      within(screen.getByRole('radiogroup', { name: 'Terminal' }))
        .getAllByRole('radio')
        .map((r) => r.getAttribute('value')),
    ).toEqual(['iterm2', 'warp', 'terminal'])
    expect(screen.getByRole('radio', { name: /iTerm2/ })).toBeChecked()
    expect(platform.setTerminal).not.toHaveBeenCalled()
    expect(screen.queryByText(/isn’t installed, so launches open in/)).not.toBeInTheDocument()
  })

  it('moves launches to Warp when only Warp is installed, saying so', async () => {
    render(<Rig terminals={terminalChoices({ iterm: false })} />)

    expect(await screen.findByText('iTerm2 isn’t installed, so launches open in Warp.')).toBeInTheDocument()
    expect(platform.setTerminal).toHaveBeenCalledExactlyOnceWith('warp')
    expect(screen.getByRole('radio', { name: /iTerm2/ })).toBeDisabled()
    expect(screen.getByRole('radio', { name: /Warp/ })).toBeChecked()
    expect(screen.getByText('Open Warp once and finish its welcome before your first launch.')).toBeInTheDocument()
  })

  it('moves launches to Terminal when neither iTerm2 nor Warp is installed, saying so', async () => {
    render(<Rig terminals={terminalChoices({ iterm: false, warp: false })} />)

    expect(await screen.findByText('iTerm2 isn’t installed, so launches open in Terminal.')).toBeInTheDocument()
    expect(platform.setTerminal).toHaveBeenCalledExactlyOnceWith('terminal')
    expect(screen.getByRole('radio', { name: /iTerm2/ })).toBeDisabled()
    expect(screen.getByRole('radio', { name: /Warp/ })).toBeDisabled()
    expect(screen.getByRole('radio', { name: /Terminal/ })).toBeChecked()
  })

  it('moves Warp, gone since it was chosen, to the first terminal installed', async () => {
    render(<Rig stored={settings({ terminal: 'warp' })} terminals={terminalChoices({ warp: false })} />)

    expect(await screen.findByText('Warp isn’t installed, so launches open in iTerm2.')).toBeInTheDocument()
    expect(platform.setTerminal).toHaveBeenCalledExactlyOnceWith('iterm2')
  })

  it('never moves a terminal that is installed, even one later in the order', async () => {
    render(<Rig stored={settings({ terminal: 'terminal' })} />)
    await nextTask()

    expect(screen.getByRole('radio', { name: /Terminal/ })).toBeChecked()
    expect(platform.setTerminal).not.toHaveBeenCalled()
  })

  it('never moves the terminal the user chose here', async () => {
    const { rerender } = render(<Rig />)

    await userEvent.click(screen.getByRole('radio', { name: /Warp/ }))
    expect(platform.setTerminal).toHaveBeenCalledExactlyOnceWith('warp')

    rerender(<Rig terminals={terminalChoices({ warp: false })} />)
    await nextTask()

    expect(platform.setTerminal).toHaveBeenCalledOnce()
    expect(screen.getByRole('radio', { name: /Warp/ })).toBeChecked()
    expect(screen.queryByText(/isn’t installed, so launches open in/)).not.toBeInTheDocument()
  })

  it('says Warp’s welcome comes first when Warp is chosen', async () => {
    render(<Rig />)

    expect(screen.queryByText(/finish its welcome/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: /Warp/ }))

    expect(platform.setTerminal).toHaveBeenCalledWith('warp')
    expect(
      await screen.findByText('Open Warp once and finish its welcome before your first launch.'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/Choose Allow/)).not.toBeInTheDocument()
  })

  it('chooses the terminal, and says iTerm2 asks to be controlled', async () => {
    render(<Rig stored={settings({ terminal: 'terminal' })} />)

    expect(screen.queryByText(/Choose Allow/)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: /iTerm2/ }))

    expect(platform.setTerminal).toHaveBeenCalledWith('iterm2')
    expect(
      await screen.findByText(/Your first launch asks to let .* control iTerm2. Choose Allow./),
    ).toBeInTheDocument()
  })

  it('waits for the terminals', () => {
    render(<Rig terminals={null} />)

    expect(screen.getByText('Looking for terminals…')).toBeInTheDocument()
  })

  it('offers the profile launches run with, and the command that signs one in', async () => {
    render(
      <Rig
        profiles={[
          profile(),
          profile({
            name: 'work',
            configDir: '/Users/example/.claude_work',
            folder: '~/.claude_work',
            signedIn: false,
          }),
        ]}
      />,
    )

    expect(card('Claude').getByRole('list', { name: 'Claude profiles' })).toHaveTextContent('work · not signed in')
    expect(card('Claude').getByText('CLAUDE_CONFIG_DIR=~/.claude_work claude login')).toBeInTheDocument()

    await userEvent.selectOptions(card('Claude').getByRole('combobox', { name: 'Profile launches run with' }), 'work')

    expect(platform.setDefaultProfile).toHaveBeenCalledWith('claude', 'work')
  })

  it('says an agent has no profile yet', () => {
    render(<Rig profiles={[profile()]} />)

    expect(card('Codex').getByText(/No profile yet/)).toBeInTheDocument()
  })

  it('looks for profiles again, then reads them again', async () => {
    const onRediscovered = vi.fn(() => Promise.resolve())
    render(<Rig onRediscovered={onRediscovered} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(platform.rediscoverProfiles).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(onRediscovered).toHaveBeenCalledOnce()
    })
  })

  it('says why profiles couldn’t be looked for', async () => {
    platform.rediscoverProfiles.mockRejectedValue(new Error('home is gone'))
    const onRediscovered = vi.fn(() => Promise.resolve())
    render(<Rig onRediscovered={onRediscovered} />)

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t look for profiles: home is gone')
    expect(onRediscovered).not.toHaveBeenCalled()
  })

  it('starts each session in its own worktree, or not', async () => {
    render(<Rig />)

    const worktrees = screen.getByRole('switch', { name: 'Start each session in its own worktree' })
    expect(worktrees).toBeChecked()

    await userEvent.click(worktrees)

    expect(platform.setWorktreeDefault).toHaveBeenCalledWith(false)
  })

  it('says why a change was refused', async () => {
    platform.setWorktreeDefault.mockRejectedValue(new Error('the settings are locked'))
    render(<Rig />)

    await userEvent.click(screen.getByRole('switch', { name: 'Start each session in its own worktree' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the settings are locked')
  })
})
