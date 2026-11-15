import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { SetupControl } from '../../lib/useSetup'
import type { IntegrationTools, Settings, SetupStep } from '../../platform'
import {
  agents,
  githubSignIn,
  goPreCommitTool,
  integrationTool,
  integrationTools,
  profile,
  settings,
  setupSignals,
  snapshot,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { integrationsOf } from '../../test/query'
import SetupView from './SetupView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// noTools is a Mac with neither tool on it.
function noTools(): IntegrationTools {
  return integrationTools({ tools: [integrationTool({ state: 'missing' }), goPreCommitTool({ state: 'missing' })] })
}

interface HarnessProps {
  start?: SetupStep
  // forced, when set, is the step shown, as the stored step arriving after the first frame
  // moves it.
  forced?: SetupStep
  tools?: IntegrationTools | null
  stored?: Settings | null
  finish?: () => void
  refresh?: () => void
}

// Harness drives the setup as App does: a step of its own, moved by go, over the window's
// settings and tools.
function Harness({
  start = 'welcome',
  forced,
  tools = noTools(),
  stored = settings(),
  finish = vi.fn(),
  refresh = vi.fn(),
}: HarnessProps) {
  const [own, setStep] = useState<SetupStep>(start)
  const [shown, setShown] = useState(stored)
  const control: SetupControl = {
    showing: true,
    step: forced ?? own,
    rerun: false,
    go: setStep,
    finish,
    restart: () => Promise.resolve({ ok: true, data: undefined }),
  }

  return (
    <SetupView
      control={control}
      theme="system"
      onThemeChange={() => Promise.resolve({ ok: true, data: undefined })}
      settings={shown}
      onSaved={setShown}
      agents={agents()}
      profiles={[profile(), profile({ agent: 'codex', folder: '~/.codex' })]}
      onLaunchChanged={() => Promise.resolve()}
      inbox={{ snapshot: snapshot(), refreshing: false, refresh }}
      integrations={integrationsOf(tools)}
    />
  )
}

// setup is the setup's dialog.
function setup() {
  return within(screen.getByRole('dialog', { name: /./ }))
}

// progress is the top bar's words for where the user stands.
function progress() {
  return screen.getByTestId('setup-progress').textContent
}

describe('SetupView', () => {
  it('walks the steps, saying where the user stands at each', async () => {
    render(<Harness />)

    expect(await screen.findByText('4 quick steps · about 2 minutes')).toBeInTheDocument()
    expect(progress()).toBe('Getting started')

    await userEvent.click(screen.getByRole('button', { name: 'Let’s go' }))

    expect(progress()).toBe('Step 1 of 4 · 3 to go')
    expect(screen.getByRole('heading', { name: 'Connect GitHub' })).toHaveFocus()
    expect(screen.getByText('Step 1 of 4: Connect GitHub')).toBeInTheDocument()
    expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveAttribute('aria-valuenow', '0')

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(progress()).toBe('Step 2 of 4 · 2 to go')
    expect(screen.getByRole('heading', { name: 'Where do your clones live?' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(progress()).toBe('Step 1 of 4 · 3 to go')
  })

  it('continues on Enter from the heading, never from a control', async () => {
    render(<Harness start="clones" />)

    const heading = await screen.findByRole('heading', { name: 'Where do your clones live?' })
    await waitFor(() => {
      expect(heading).toHaveFocus()
    })

    await userEvent.keyboard('{Enter}')
    expect(progress()).toBe('Step 3 of 4 · 1 to go')

    const back = screen.getByRole('button', { name: 'Back' })
    back.focus()
    await userEvent.keyboard('{Enter}')
    expect(progress()).toBe('Step 2 of 4 · 2 to go')
  })

  it('continues on Enter with focus on nothing, and keeps every key from the window beneath', () => {
    const beneath = vi.fn()
    document.addEventListener('keydown', beneath)

    try {
      render(<Harness start="notifications" />)

      document.body.focus()
      fireEvent.keyDown(document.body, { key: 'Enter' })
      expect(progress()).toBe('All 4 steps done')

      fireEvent.keyDown(screen.getByRole('heading', { name: 'You’re all set' }), { key: 'f', metaKey: true })
      expect(beneath).toHaveBeenCalledTimes(1)
    } finally {
      document.removeEventListener('keydown', beneath)
    }
  })

  it('leaves Enter alone while it is held, as by an input method', () => {
    render(<Harness start="clones" />)

    fireEvent.keyDown(screen.getByRole('heading', { name: 'Where do your clones live?' }), {
      key: 'Enter',
      isComposing: true,
    })
    fireEvent.keyDown(screen.getByRole('heading', { name: 'Where do your clones live?' }), {
      key: 'Enter',
      metaKey: true,
    })

    expect(progress()).toBe('Step 2 of 4 · 2 to go')
  })

  it('skips the setup only once the user confirms', async () => {
    const finish = vi.fn()
    render(<Harness start="github" finish={finish} />)

    await userEvent.click(screen.getAllByRole('button', { name: 'Skip setup' })[0] as HTMLElement)
    const confirm = within(screen.getByRole('dialog', { name: 'Skip the setup?' }))
    expect(confirm.getByText(/run it again from Settings → About/)).toBeInTheDocument()

    await userEvent.click(confirm.getByRole('button', { name: 'Cancel' }))
    expect(finish).not.toHaveBeenCalled()

    await userEvent.click(screen.getAllByRole('button', { name: 'Skip setup' })[0] as HTMLElement)
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Skip the setup?' })).getByRole('button', { name: 'Skip setup' }),
    )
    expect(finish).toHaveBeenCalledOnce()
  })

  it('skips from Welcome too', async () => {
    const finish = vi.fn()
    render(<Harness finish={finish} />)

    const skips = screen.getAllByRole('button', { name: 'Skip setup' })
    await userEvent.click(skips[skips.length - 1] as HTMLElement)
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Skip the setup?' })).getByRole('button', { name: 'Skip setup' }),
    )

    expect(finish).toHaveBeenCalledOnce()
  })

  it('shows Tools when a tool is installed', async () => {
    render(<Harness tools={integrationTools()} />)

    expect(await screen.findByText('5 quick steps · about 3 minutes')).toBeInTheDocument()
  })

  it('adds Tools as the user leaves Clones, once the new clone root holds Go repositories', async () => {
    render(<Harness start="clones" />)

    await waitFor(() => {
      expect(progress()).toBe('Step 2 of 4 · 2 to go')
    })

    platform.setupSignals.mockResolvedValue(setupSignals({ clones: 3, goModules: 3 }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))

    await waitFor(() => {
      expect(progress()).toBe('Step 3 of 5 · 2 to go')
    })
    expect(screen.getByRole('navigation', { name: 'Setup steps' })).toHaveTextContent('Suggested')
  })

  it('resumes on the stored step once it arrives, with the steps before it reached', async () => {
    const { rerender } = render(<Harness forced="welcome" />)

    expect(progress()).toBe('Getting started')

    rerender(<Harness forced="notifications" />)

    expect(await screen.findByRole('heading', { name: 'Stay in the loop' })).toBeInTheDocument()
    expect(progress()).toBe('Step 4 of 4 · last one')
    expect(
      within(screen.getByRole('navigation', { name: 'Setup steps' })).getByRole('button', { name: /Find your clones/ }),
    ).toBeInTheDocument()
  })

  it('opens on the Tools step when the user left it there', async () => {
    render(<Harness start="tools" />)

    expect(progress()).toBe('Step 5 of 5 · last one')
    expect(await screen.findByRole('button', { name: 'Skip' })).toBeInTheDocument()
  })

  it('continues from the Tools step with a tool installed', async () => {
    render(<Harness start="tools" tools={integrationTools()} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Continue' }))

    expect(progress()).toBe('All 5 steps done')
  })

  it('fills the inbox once the sign-in is ready, and only once', async () => {
    const refresh = vi.fn()
    render(<Harness start="github" refresh={refresh} />)

    await waitFor(() => {
      expect(refresh).toHaveBeenCalledOnce()
    })
    expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(refresh).toHaveBeenCalledOnce()
  })

  it('logs a GitHub notifications read that failed', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.checkGitHubThreadsNow.mockRejectedValue(new Error('offline'))
    render(<Harness start="github" />)

    await waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the GitHub notifications', expect.any(Error))
    })
  })

  it('continues without GitHub, marking the step for attention', async () => {
    platform.checkGitHub.mockResolvedValue(githubSignIn({ state: 'signed_out', fix: 'gh auth login' }))
    render(<Harness start="github" />)

    await userEvent.click(await screen.findByRole('button', { name: 'Continue without GitHub' }))

    expect(
      within(screen.getByRole('navigation', { name: 'Setup steps' })).getByRole('button', {
        name: /Connect GitHub, needs attention/,
      }),
    ).toBeInTheDocument()
  })

  it('goes back to a step from the rail', async () => {
    render(<Harness start="notifications" />)

    await userEvent.click(
      within(screen.getByRole('navigation', { name: 'Setup steps' })).getByRole('button', { name: /Find your clones/ }),
    )

    expect(progress()).toBe('Step 2 of 4 · 2 to go')
  })

  it('ends on Finish, whose button opens the inbox', async () => {
    const finish = vi.fn()
    render(<Harness start="notifications" finish={finish} />)

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByRole('heading', { name: 'You’re all set' })).toBeInTheDocument()
    expect(screen.getByRole('figure', { name: 'Setup log' })).toHaveTextContent(`${brand.slug}/setup`)

    await userEvent.click(screen.getByRole('button', { name: 'Open my inbox' }))
    expect(finish).toHaveBeenCalledOnce()
  })

  it('finishes on Enter at Finish', async () => {
    const finish = vi.fn()
    render(<Harness start="finish" finish={finish} />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'You’re all set' })).toHaveFocus()
    })
    await userEvent.keyboard('{Enter}')

    expect(finish).toHaveBeenCalledOnce()
  })

  it('fixes from Finish what needs attention', async () => {
    platform.checkGitHub.mockResolvedValue(githubSignIn({ state: 'no_cli', fix: 'brew install gh' }))
    render(<Harness start="finish" />)

    await userEvent.click(await screen.findByRole('button', { name: 'Fix github' }))

    expect(progress()).toBe('Step 1 of 4 · 3 to go')
  })

  it('shows the steps that need the settings once they load', async () => {
    const { rerender } = render(<Harness start="agents" stored={null} />)

    expect(screen.queryByRole('region', { name: 'Claude' })).not.toBeInTheDocument()

    rerender(<Harness start="agents" stored={settings()} />)

    expect(await setup().findByRole('heading', { name: 'Pick your agent' })).toBeInTheDocument()
  })

  it('walks back from Welcome’s restore to Finish', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/Desktop/backup.json')
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Restore from a backup…' }))
    await userEvent.click(
      await within(await screen.findByRole('dialog', { name: /Restore/ })).findByRole('button', { name: /Import/ }),
    )
    await userEvent.click(await screen.findByRole('button', { name: 'Finish now' }))

    expect(screen.getByRole('heading', { name: 'You’re all set' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Open my inbox' }))
  })

  it('walks from Welcome’s restore to the steps', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/Desktop/backup.json')
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Restore from a backup…' }))
    await userEvent.click(
      await within(await screen.findByRole('dialog', { name: /Restore/ })).findByRole('button', { name: /Import/ }),
    )
    await userEvent.click(await screen.findByRole('button', { name: 'Review the steps' }))

    expect(progress()).toBe('Step 1 of 4 · 3 to go')
  })
})
