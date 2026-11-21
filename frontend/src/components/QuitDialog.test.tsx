import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { ScriptRun } from '../platform'
import { scriptRun } from '../test/fixtures'
import { listening, platform, send } from '../test/platform'
import { QuitDialog } from './QuitDialog'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// going is a run going in the app, which quitting would stop.
function going(change: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({ status: 'running', exitCode: null, finishedAt: '', canStop: true, ...change })
}

const release = going({ id: 3, kind: 'release', title: 'Release v1.4.3', repo: 'acme-org/sdk' })

// heldPull is a pull whose Stop is held back while git writes the clone's files.
const heldPull = going({
  id: 5,
  kind: 'gitPull',
  title: 'Pull main',
  repo: 'acme-org/sdk',
  canStop: false,
  progress: {
    step: '',
    working: '',
    stopRefused:
      'A pull can’t be stopped: stopping it partway could leave the clone’s files half updated. It ends on its own, within thirty minutes.',
    stopConfirm: '',
  },
})

// askToQuit renders the dialog with the runs listed, then has the shell ask to quit.
async function askToQuit(runs: ScriptRun[]) {
  platform.listScriptRuns.mockResolvedValue(runs)
  render(<QuitDialog />)

  send('app:quitRequested')

  await waitFor(() => {
    expect(platform.listScriptRuns).toHaveBeenCalledExactlyOnceWith({ kinds: [], repo: '', withAgents: true })
  })
}

// dialog is the question, once it shows.
async function dialog() {
  return within(await screen.findByRole('dialog', { name: `Quit ${brand.name}?` }))
}

describe('QuitDialog', () => {
  it('shows nothing until the shell asks, and listens until it goes', () => {
    const { unmount } = render(<QuitDialog />)

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(listening('app:quitRequested')).toBe(1)

    unmount()

    expect(listening('app:quitRequested')).toBe(0)
  })

  it('quits at once when no run is going in the app any more', async () => {
    await askToQuit([scriptRun(), going({ id: 2, runsIn: 'terminal', canStop: false })])

    await waitFor(() => {
      expect(platform.quitAnyway).toHaveBeenCalledOnce()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('warns that stopping a release can leave a tag only on this Mac', async () => {
    await askToQuit([release, scriptRun()])

    const asked = await dialog()

    expect(
      asked.getByText(
        'A release is running in acme-org/sdk. Stopping it now can leave a tag on this Mac but not on GitHub. Quit anyway?',
      ),
    ).toBeInTheDocument()
    expect(asked.getByRole('button', { name: 'Keep running' })).toHaveFocus()
  })

  it('asks about a release Stop holds back for now, saying quitting waits for it and may leave it going', async () => {
    const held = going({
      ...release,
      canStop: false,
      progress: {
        step: 'onBranch',
        working: 'updated',
        stopRefused: 'Stopping now could leave a rebase in progress.',
        stopConfirm: '',
      },
    })

    await askToQuit([held])

    const d = await dialog()

    expect(
      d.getByText(
        `A release is running in acme-org/sdk. magex is switching branches or pulling, so stopping waits up to a few seconds for it to finish. If it doesn’t, the release goes on after ${brand.name} closes, and ${brand.name} checks how it ended when it opens again. Quit anyway?`,
      ),
    ).toBeInTheDocument()

    await userEvent.click(d.getByRole('button', { name: 'Stop and quit' }))
    expect(platform.quitAnyway).toHaveBeenCalledOnce()
  })

  it('says so among several runs when one is held back', async () => {
    const held = going({
      ...release,
      canStop: false,
      progress: {
        step: '',
        working: 'onBranch',
        stopRefused: 'Stopping now could leave a rebase in progress.',
        stopConfirm: '',
      },
    })

    await askToQuit([held, going({ id: 2 })])

    const d = await dialog()

    expect(
      d.getByText(/A release is switching branches or pulling, so stopping waits up to a few seconds for it\./),
    ).toBeInTheDocument()
    expect(d.getByText(new RegExp(`that release goes on after ${brand.name} closes`))).toBeInTheDocument()
  })

  it('asks about a pull Stop holds back while git writes the clone, in its own words', async () => {
    await askToQuit([heldPull])

    expect(
      (await dialog()).getByText(
        `Pull main is running in acme-org/sdk. A pull can’t be stopped: stopping it partway could leave the clone’s files half updated. It ends on its own, within thirty minutes. Quitting waits up to a few seconds for it. If it hasn’t ended by then, it goes on after ${brand.name} closes. Quit anyway?`,
      ),
    ).toBeInTheDocument()
  })

  it('says so among several runs when a pull is held back, never as a release', async () => {
    await askToQuit([heldPull, going({ id: 2 })])

    const d = await dialog()
    expect(d.getByText(/A run that can’t be stopped right now is waited for up to a few seconds\./)).toBeInTheDocument()
    expect(d.queryByText(/A release is switching branches/)).not.toBeInTheDocument()
  })

  it('names another run and where it runs', async () => {
    await askToQuit([going({ repo: '', dir: '~/scratch' })])

    expect(
      (await dialog()).getByText('magex lint is running in ~/scratch. Stopping it now ends it partway. Quit anyway?'),
    ).toBeInTheDocument()
  })

  it('keeps running when the user says so', async () => {
    await askToQuit([release])

    await userEvent.click((await dialog()).getByRole('button', { name: 'Keep running' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.quitAnyway).not.toHaveBeenCalled()
    expect(platform.keepRunning).toHaveBeenCalledOnce()
  })

  it('keeps running on Escape too, and tells the shell even when that fails', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const gone = new Error('the shell is gone')
    platform.keepRunning.mockRejectedValue(gone)
    await askToQuit([release])
    await dialog()

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not keep running:', gone)
    })
    expect(platform.keepRunning).toHaveBeenCalledOnce()
    expect(platform.quitAnyway).not.toHaveBeenCalled()
  })

  it('lists several runs, says what stopping a release can leave, and stops them and quits', async () => {
    await askToQuit([release, going({ id: 2 })])

    const asked = await dialog()

    expect(
      asked.getByText(
        `2 runs are going in ${brand.name}. Stopping them now ends them partway. Stopping a release now can leave a tag on this Mac but not on GitHub.`,
      ),
    ).toBeInTheDocument()
    expect(asked.getAllByRole('listitem').map((li) => li.textContent)).toEqual([
      'Release v1.4.3 in acme-org/sdk',
      'magex lint in acme-org/example-repo',
    ])

    await userEvent.click(asked.getByRole('button', { name: 'Stop and quit' }))

    expect(platform.quitAnyway).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('leaves the release warning out when no release is going', async () => {
    await askToQuit([going({ id: 2 }), going({ id: 1, title: 'Checks · all files' })])

    const asked = await dialog()

    expect(
      asked.getByText(`2 runs are going in ${brand.name}. Stopping them now ends them partway.`),
    ).toBeInTheDocument()
  })

  it('asks about the runs as a whole when they couldn’t be listed', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    platform.listScriptRuns.mockRejectedValue(new Error('database is locked'))
    render(<QuitDialog />)
    send('app:quitRequested')

    expect(
      (await dialog()).getByText(`Runs are going in ${brand.name}. Stopping them now ends them partway. Quit anyway?`),
    ).toBeInTheDocument()
    expect(logged).toHaveBeenCalledWith('could not list the runs going as the app quits:', 'database is locked')
  })

  describe('with agents working', () => {
    // agent is one of Autopilot's agents in the background, which keeps to a budget of its
    // own, so it can outlive the quit.
    const agent = going({ id: 8, kind: 'agent', title: 'Fix checks · acme-org/sdk #12', outlivesQuit: true })
    const other = going({ id: 9, kind: 'agent', title: 'Review · acme-org/api #4', outlivesQuit: true })

    it('lets them finish: they carry on, and the next start picks them up', async () => {
      await askToQuit([agent, other])

      const asked = await dialog()

      expect(asked.getByText('2 agents are still working. Stopping them now ends them partway.')).toBeInTheDocument()
      expect(asked.getAllByRole('listitem').map((li) => li.textContent)).toEqual([agent.title, other.title])
      expect(asked.getByText(`They carry on, and ${brand.name} picks them up when it opens again.`)).toBeInTheDocument()

      await userEvent.click(asked.getByRole('button', { name: 'Let them finish' }))

      expect(platform.letThemFinish).toHaveBeenCalledOnce()
      expect(platform.quitAnyway).not.toHaveBeenCalled()
      expect(platform.keepRunning).not.toHaveBeenCalled()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('stops them and quits', async () => {
      await askToQuit([agent])

      const asked = await dialog()

      expect(asked.getByText(`${agent.title} is still working. Stopping it now ends it partway.`)).toBeInTheDocument()

      await userEvent.click(asked.getByRole('button', { name: 'Stop it and quit' }))

      expect(platform.quitAnyway).toHaveBeenCalledOnce()
      expect(platform.letThemFinish).not.toHaveBeenCalled()
    })

    it('keeps running', async () => {
      await askToQuit([agent, other])

      await userEvent.click((await dialog()).getByRole('button', { name: 'Keep running' }))

      expect(platform.keepRunning).toHaveBeenCalledOnce()
      expect(platform.quitAnyway).not.toHaveBeenCalled()
      expect(platform.letThemFinish).not.toHaveBeenCalled()
    })

    it('lets the agents finish beside script runs, which stop', async () => {
      await askToQuit([agent, release])

      const asked = await dialog()

      expect(asked.getByRole('button', { name: 'Stop them and quit' })).toBeInTheDocument()
      expect(
        asked.getByText(/It carries on, and .* picks it up when it opens again\. 1 other run stops\./),
      ).toBeInTheDocument()

      await userEvent.click(asked.getByRole('button', { name: 'Let it finish' }))
      expect(platform.letThemFinish).toHaveBeenCalledOnce()
    })

    it('names every agent that stops with the app', async () => {
      const one = going({ id: 11, kind: 'agent', title: 'Fix A', outlivesQuit: false })
      const two = going({ id: 12, kind: 'agent', title: 'Fix B', outlivesQuit: false })

      await askToQuit([one, two])

      expect(
        (await dialog()).getByText(
          `Fix A, Fix B stop with ${brand.name}: their agents keep to no budget of their own, so nothing would bound them while ${brand.name} is closed.`,
        ),
      ).toBeInTheDocument()
    })

    it('writes a failure to let them finish to the console', async () => {
      const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
      const gone = new Error('the shell is gone')

      platform.letThemFinish.mockRejectedValueOnce(gone)
      await askToQuit([agent])
      await userEvent.click((await dialog()).getByRole('button', { name: 'Let it finish' }))

      await waitFor(() => {
        expect(logged).toHaveBeenCalledWith('could not quit letting the agents finish:', gone)
      })
    })

    it('offers no Let them finish while an agent can’t outlive the quit, and says it stops', async () => {
      const unbounded = going({ id: 10, kind: 'agent', title: 'Fix lint · acme-org/sdk #13', outlivesQuit: false })

      await askToQuit([agent, unbounded])

      const asked = await dialog()

      expect(asked.queryByRole('button', { name: /Let (them|it) finish/ })).not.toBeInTheDocument()
      expect(
        asked.getByText(
          `${unbounded.title} stops with ${brand.name}: its agent keeps to no budget of its own, so nothing would bound it while ${brand.name} is closed.`,
        ),
      ).toBeInTheDocument()
    })
  })

  it('writes a failure to quit to the console', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const gone = new Error('the shell is gone')

    platform.quitAnyway.mockRejectedValue(gone)
    await askToQuit([release])
    await userEvent.click((await dialog()).getByRole('button', { name: 'Stop and quit' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not quit:', gone)
    })
  })
})
