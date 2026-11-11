import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { ScriptRun, ScriptRunStatus, TerminalID } from '../../platform'
import { at, FIXED_NOW, fromNow } from '../../test/clock'
import { checksRun, hookActionResult, hooksRun, scriptRun, scriptRunChunk } from '../../test/fixtures'
import { emit, listening, platform } from '../../test/platform'
import { RunPanel } from './RunPanel'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

afterEach(() => {
  vi.useRealTimers()
})

// ago is the time ms milliseconds before the test clock's, as a run records it.
function ago(ms: number): string {
  return fromNow(-ms).toISOString()
}

// ended is a run that ran for 38 seconds, ending a minute ago, with changes made.
function ended(change: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({ startedAt: ago(98_000), finishedAt: ago(60_000), ...change })
}

// running is a run going in the app for five seconds so far.
function running(change: Partial<ScriptRun> = {}): ScriptRun {
  return scriptRun({
    status: 'running',
    summary: '',
    exitCode: null,
    startedAt: ago(5_000),
    finishedAt: '',
    canStop: true,
    seen: false,
    ...change,
  })
}

// renderPanel shows run 1 in the panel, as the backend reads it, with the clock at
// FIXED_NOW.
function renderPanel(run: ScriptRun = ended(), terminal: TerminalID | null = 'iterm2') {
  const onSelect = vi.fn<(id: number) => void>()
  const onClose = vi.fn<() => void>()

  platform.getScriptRun.mockResolvedValue(run)
  const { unmount } = render(<RunPanel id={1} terminal={terminal} onSelect={onSelect} onClose={onClose} />, {
    wrapper: at(),
  })

  return { onSelect, onClose, unmount }
}

// panel waits for the run to show, and returns its header's status line.
async function shownRun(title = 'magex lint') {
  await screen.findByRole('heading', { name: title })

  return screen.getByRole('status')
}

describe('RunPanel', () => {
  it('says what the run is, where it ran, how it ended, and how long it took', async () => {
    renderPanel(ended({ title: 'Lint', command: 'magex lint', summary: 'Finished.' }))

    const status = await shownRun('Lint')

    expect(status).toHaveTextContent('Done')
    expect(status).toHaveTextContent('Finished.')
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByText('acme-org/example-repo ·', { exact: false })).toHaveTextContent(
      'acme-org/example-repo · ~/projects/example-repo',
    )
    expect(screen.getByText(`In ${brand.name}`)).toBeInTheDocument()
    expect(screen.getByText('38 s')).toBeInTheDocument()
    expect(screen.getByText('$ magex lint')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Output' })).toBeInTheDocument()
    expect(platform.getScriptRun).toHaveBeenCalledExactlyOnceWith(1)
  })

  it('names a folder that isn’t a repository alone, and leaves out a command its title says', async () => {
    renderPanel(ended({ repo: '', dir: '~/scratch' }))

    await shownRun()

    expect(screen.getByText('~/scratch')).toBeInTheDocument()
    expect(screen.queryByText(/acme-org/)).not.toBeInTheDocument()
    expect(screen.queryByText('$ magex lint')).not.toBeInTheDocument()
  })

  it.each<[ScriptRunStatus, string]>([
    ['running', 'Running'],
    ['succeeded', 'Done'],
    ['failed', 'Failed'],
    ['stopped', 'Stopped'],
    ['interrupted', 'Interrupted'],
    ['orphaned', 'Orphaned'],
  ])('shows a %s run’s status in words, with its icon', async (status, words) => {
    renderPanel(scriptRun({ status, summary: '' }))

    const line = await shownRun()

    expect(line).toHaveTextContent(words)
    expect(line.querySelector('svg')).not.toBeNull()
  })

  it('shows the summary Go words, as for an interrupted run', async () => {
    renderPanel(ended({ status: 'interrupted', summary: `${brand.name} closed while this was running.` }))

    expect(await shownRun()).toHaveTextContent(`Interrupted${brand.name} closed while this was running.`)
  })

  it.each<[ScriptRun['runsIn'], TerminalID | null, string]>([
    ['terminal', 'iterm2', 'In iTerm2'],
    ['terminal', 'terminal', 'In Terminal'],
    ['terminal', null, 'In your terminal'],
  ])('says a run in the %s ran in %s', async (runsIn, terminal, words) => {
    renderPanel(ended({ runsIn }), terminal)

    await shownRun()

    expect(screen.getByText(words)).toBeInTheDocument()
  })

  it('says no time for an ended run whose end wasn’t recorded', async () => {
    renderPanel(ended({ status: 'interrupted', finishedAt: '' }))

    await shownRun()

    expect(screen.queryByText(/^\d+ s$/)).not.toBeInTheDocument()
  })

  it('counts a running run’s time every second', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'], now: FIXED_NOW })
    renderPanel(running())

    await shownRun()
    expect(await screen.findByText('5 s')).toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(await screen.findByText('6 s')).toBeInTheDocument()
  })

  it('reads the run again whenever the runs change, until it goes', async () => {
    const { unmount } = renderPanel(running())

    await shownRun()
    platform.getScriptRun.mockResolvedValue(ended({ status: 'failed', summary: 'It exited with status 2.' }))
    act(() => {
      emit('scriptruns:updated')
    })

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('FailedIt exited with status 2.')
    })
    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()

    expect(listening('scriptruns:updated')).toBe(1)
    unmount()
    expect(listening('scriptruns:updated')).toBe(0)
  })

  it('reads the run again as its own progress moves, and not for another run’s', async () => {
    const refused = 'Stopping now could leave a rebase in progress.'
    const { unmount } = renderPanel(
      running({ canStop: false, progress: { step: '', working: 'onBranch', stopRefused: refused, stopConfirm: '' } }),
    )

    await shownRun()
    expect(screen.getByRole('button', { name: 'Stop' })).toBeDisabled()

    const reads = platform.getScriptRun.mock.calls.length

    platform.getScriptRun.mockResolvedValue(running())
    act(() => {
      emit('scriptruns:progress', { id: 2 })
    })
    expect(platform.getScriptRun).toHaveBeenCalledTimes(reads)

    act(() => {
      emit('scriptruns:progress', { id: 1 })
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Stop' })).toBeEnabled()
    })

    expect(listening('scriptruns:progress')).toBe(1)
    unmount()
    expect(listening('scriptruns:progress')).toBe(0)
  })

  it('says the run is being read, then why it couldn’t be, and still closes', async () => {
    platform.getScriptRun.mockRejectedValue(new Error('the run is no longer recorded'))
    const onClose = vi.fn<() => void>()

    render(<RunPanel id={1} terminal="iterm2" onSelect={vi.fn()} onClose={onClose} />, { wrapper: at() })

    expect(screen.getByText('Reading the run…')).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the run: the run is no longer recorded')

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('keeps showing the run when reading it again fails, and says so', async () => {
    renderPanel()

    await shownRun()
    platform.getScriptRun.mockRejectedValue(new Error('database is locked'))
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the run again: database is locked')
    expect(screen.getByRole('heading', { name: 'magex lint' })).toBeInTheDocument()
  })

  it('closes', async () => {
    const { onClose } = renderPanel()

    await shownRun()
    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))

    expect(onClose).toHaveBeenCalledOnce()
  })

  describe('seen', () => {
    it('marks an ended run seen once it shows', async () => {
      renderPanel(ended({ seen: false }))

      await shownRun()

      await waitFor(() => {
        expect(platform.markScriptRunSeen).toHaveBeenCalledExactlyOnceWith(1)
      })
    })

    it('leaves a run seen already, or still going, as it is', async () => {
      renderPanel(running())

      await shownRun()
      platform.getScriptRun.mockResolvedValue(ended({ seen: true }))
      act(() => {
        emit('scriptruns:updated')
      })
      await waitFor(() => {
        expect(screen.getByRole('status')).toHaveTextContent('Done')
      })

      expect(platform.markScriptRunSeen).not.toHaveBeenCalled()
    })

    it('marks a run seen as it ends in view', async () => {
      renderPanel(running())

      await shownRun()
      platform.getScriptRun.mockResolvedValue(ended({ status: 'failed', seen: false }))
      act(() => {
        emit('scriptruns:updated')
      })

      await waitFor(() => {
        expect(platform.markScriptRunSeen).toHaveBeenCalledExactlyOnceWith(1)
      })
    })

    it('writes a failure to mark it seen to the console', async () => {
      const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)

      platform.markScriptRunSeen.mockRejectedValue(new Error('database is locked'))
      renderPanel(ended({ seen: false }))

      await shownRun()

      await waitFor(() => {
        expect(logged).toHaveBeenCalledWith('could not mark the run seen:', 'database is locked')
      })
    })
  })

  describe('its actions', () => {
    it('offers Stop only for a run going in the app, and stops it', async () => {
      renderPanel(running())

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Stop' }))

      expect(platform.stopScriptRun).toHaveBeenCalledExactlyOnceWith(1)
      expect(screen.getByRole('button', { name: 'Stopping…' })).toBeDisabled()
    })

    it('says why it couldn’t stop the run, and offers Stop again', async () => {
      platform.stopScriptRun.mockRejectedValue(new Error('A run in the terminal stops there.'))
      renderPanel(running())

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Stop' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('A run in the terminal stops there.')
      expect(screen.getByRole('button', { name: 'Stop' })).toBeEnabled()
    })

    it('turns Stop off while the run’s kind refuses it, saying why, and on again once it lets it', async () => {
      const refused = 'Stopping now could leave a rebase in progress.'

      renderPanel(
        running({
          canStop: false,
          progress: { step: 'onBranch', working: 'updated', stopRefused: refused, stopConfirm: '' },
        }),
      )

      await shownRun()

      const stop = screen.getByRole('button', { name: 'Stop' })

      expect(stop).toBeDisabled()
      expect(stop).toHaveAccessibleDescription(refused)

      platform.getScriptRun.mockResolvedValue(running())
      act(() => {
        emit('scriptruns:updated')
      })

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Stop' })).toBeEnabled()
      })
      expect(screen.queryByText(refused)).not.toBeInTheDocument()
    })

    it('asks first while stopping would leave something behind, and stops only when told to', async () => {
      const confirm = 'Stopping now can leave v1.4.3 on this Mac but not on GitHub, and your clone on master.'

      renderPanel(running({ progress: { step: 'tagCreated', working: '', stopRefused: '', stopConfirm: confirm } }))

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Stop' }))

      const ask = screen.getByRole('dialog', { name: 'Stop this run?' })

      expect(ask).toHaveTextContent(confirm)
      await userEvent.click(within(ask).getByRole('button', { name: 'Keep running' }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(platform.stopScriptRun).not.toHaveBeenCalled()

      await userEvent.click(screen.getByRole('button', { name: 'Stop' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Stop the run' }))

      expect(platform.stopScriptRun).toHaveBeenCalledExactlyOnceWith(1)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it.each<[TerminalID | null, string]>([
      ['iterm2', 'iTerm2'],
      [null, 'your terminal'],
    ])('offers no Stop for a run in the terminal, and says to press Control-C there (%s)', async (terminal, name) => {
      renderPanel(running({ runsIn: 'terminal', canStop: false }), terminal)

      await shownRun()

      expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
      expect(screen.getByText(`It runs in ${name}: press Control-C there to stop it.`)).toBeInTheDocument()
    })

    it('has no close button where the panel closes with what holds it', async () => {
      platform.getScriptRun.mockResolvedValue(ended())
      render(<RunPanel id={1} terminal="iterm2" onSelect={vi.fn()} />, { wrapper: at() })

      await shownRun()

      expect(screen.queryByRole('button', { name: 'Close the run' })).not.toBeInTheDocument()
    })

    it('offers no Stop for an ended run, nor Run again where the run can’t', async () => {
      renderPanel(ended({ canRunAgain: false }))

      await shownRun()

      expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Run again' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Open in terminal' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Copy log' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Show the log file' })).toBeInTheDocument()
    })

    it('runs it again and shows the new run', async () => {
      const { onSelect } = renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Run again' }))

      expect(platform.runScriptAgain).toHaveBeenCalledExactlyOnceWith(1)
      expect(onSelect).toHaveBeenCalledExactlyOnceWith(2)
    })

    it('says why it couldn’t run it again', async () => {
      platform.runScriptAgain.mockRejectedValue(new Error('A busy clone: a release is using it.'))
      const { onSelect } = renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Run again' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('A busy clone: a release is using it.')
      expect(onSelect).not.toHaveBeenCalled()
    })

    it('opens the terminal in the run’s folder, and shows the log file', async () => {
      renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Open in terminal' }))
      await userEvent.click(screen.getByRole('button', { name: 'Show the log file' }))

      expect(platform.openScriptRunInTerminal).toHaveBeenCalledExactlyOnceWith(1)
      expect(platform.showScriptRunLog).toHaveBeenCalledExactlyOnceWith(1)
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('shows only the failure of the button pressed last', async () => {
      platform.openScriptRunInTerminal.mockRejectedValue(new Error('~/projects/example-repo isn’t there anymore.'))
      platform.showScriptRunLog.mockRejectedValueOnce(new Error('This run’s log is gone.'))
      renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Open in terminal' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('~/projects/example-repo isn’t there anymore.')

      await userEvent.click(screen.getByRole('button', { name: 'Show the log file' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('This run’s log is gone.')

      await userEvent.click(screen.getByRole('button', { name: 'Show the log file' }))
      await waitFor(() => {
        expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      })
    })

    it('copies the whole log as plain text', async () => {
      platform.scriptRunOutput.mockResolvedValue(scriptRunChunk({ text: '\x1b[32mlint passed\x1b[0m\n' }))
      renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Copy log' }))

      expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('lint passed\n')
      expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()

      // Another button clears it.
      await userEvent.click(screen.getByRole('button', { name: 'Show the log file' }))
      expect(screen.getByRole('button', { name: 'Copy log' })).toBeInTheDocument()
    })

    it('leaves magex’s JSON result out of a release’s log, and keeps it in the copy', async () => {
      const result =
        '{"from":"v1.4.3","to":"v1.4.4","bump":"patch","tag":"v1.4.4","pushed":true,"branch":"master",' +
        '"warnings":[],"error":""}'

      platform.scriptRunOutput.mockResolvedValue(scriptRunChunk({ text: `✅ Tag pushed to remote\n${result}\n` }))
      renderPanel(ended({ kind: 'release', title: 'Release v1.4.4' }))

      await shownRun('Release v1.4.4')

      const log = screen.getByRole('region', { name: 'Output' })

      expect(await within(log).findByText('✅ Tag pushed to remote')).toBeInTheDocument()
      expect(log).not.toHaveTextContent('"pushed":true')

      await userEvent.click(screen.getByRole('button', { name: 'Copy log' }))
      expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(`✅ Tag pushed to remote\n${result}\n`)
    })

    it('shows a line like a JSON result in another run’s log', async () => {
      const line = '{"from":"a","to":"b","tag":"","pushed":false,"branch":"","warnings":[],"error":""}'

      platform.scriptRunOutput.mockResolvedValue(scriptRunChunk({ text: `${line}\n` }))
      renderPanel()

      await shownRun()
      expect(await within(screen.getByRole('region', { name: 'Output' })).findByText(line)).toBeInTheDocument()
    })

    it('says it is copying until it has, and why it couldn’t', async () => {
      let refuse: (err: Error) => void = () => undefined

      platform.copyText.mockReturnValue(
        new Promise((_, reject) => {
          refuse = reject
        }),
      )
      renderPanel()

      await shownRun()
      await userEvent.click(screen.getByRole('button', { name: 'Copy log' }))

      expect(await screen.findByRole('button', { name: 'Copying…' })).toBeDisabled()

      act(() => {
        refuse(new Error('The clipboard refused it.'))
      })

      expect(await screen.findByRole('alert')).toHaveTextContent('The clipboard refused it.')
      expect(screen.getByRole('button', { name: 'Copy log' })).toBeEnabled()
    })
  })

  describe('its result', () => {
    it('shows nothing for a run that ended well with nothing to point out', async () => {
      renderPanel()

      await shownRun()

      expect(screen.queryByRole('region', { name: 'Result' })).not.toBeInTheDocument()
    })

    it('shows the exit code when it isn’t 0, and the tool’s warnings', async () => {
      renderPanel(
        ended({
          status: 'failed',
          exitCode: 2,
          result: {
            ...scriptRun().result,
            warnings: ['\x1b[33mwarning:\x1b[0m go.sum is out of date', 'warning: 3 files skipped'],
          },
        }),
      )

      await shownRun()
      const result = within(screen.getByRole('region', { name: 'Result' }))

      expect(result.getByText('Exit code 2')).toBeInTheDocument()
      expect(
        within(result.getByRole('list', { name: 'Warnings' }))
          .getAllByRole('listitem')
          .map((li) => li.textContent),
      ).toEqual(['warning: go.sum is out of date', 'warning: 3 files skipped'])
    })

    it('shows no exit code a run didn’t leave', async () => {
      renderPanel(
        ended({
          status: 'interrupted',
          exitCode: null,
          result: { ...scriptRun().result, warnings: ['warning: slow'] },
        }),
      )

      await shownRun()

      expect(screen.getByRole('region', { name: 'Result' })).not.toHaveTextContent('Exit code')
    })

    it('counts the lines the reader didn’t recognize, and shows those it kept', async () => {
      renderPanel(
        ended({
          result: {
            ...scriptRun().result,
            unrecognized: ['odd line', '\x1b[1manother\x1b[0m'],
            unrecognizedCount: 3,
          },
        }),
      )

      await shownRun()
      const result = within(screen.getByRole('region', { name: 'Result' }))

      expect(result.getByText(`3 lines ${brand.name} didn’t recognize ·`, { exact: false })).toBeInTheDocument()
      expect(result.queryByText(/odd line/)).not.toBeInTheDocument()

      await userEvent.click(result.getByRole('button', { name: 'Show' }))

      expect(result.getByRole('button', { name: 'Hide' })).toHaveAttribute('aria-expanded', 'true')
      expect(result.getByText(/odd line/)).toHaveTextContent('odd line another')
      expect(result.getByText('The first 2 lines are kept.')).toBeInTheDocument()

      await userEvent.click(result.getByRole('button', { name: 'Hide' }))

      expect(result.queryByText(/odd line/)).not.toBeInTheDocument()
    })

    it('says one line when one wasn’t recognized, kept with the rest', async () => {
      renderPanel(ended({ result: { ...scriptRun().result, unrecognized: ['odd'], unrecognizedCount: 1 } }))

      await shownRun()
      const result = within(screen.getByRole('region', { name: 'Result' }))

      expect(result.getByText(`1 line ${brand.name} didn’t recognize ·`, { exact: false })).toBeInTheDocument()
      await userEvent.click(result.getByRole('button', { name: 'Show' }))
      expect(result.queryByText(/are kept/)).not.toBeInTheDocument()
    })
  })
  describe('the result of a hooks action or a checks run', () => {
    it('shows what a hooks action did, read from the hook files, once it ended', async () => {
      renderPanel(hooksRun({ startedAt: ago(98_000), finishedAt: ago(60_000) }))

      await shownRun('Install the hook')

      expect(
        await screen.findByRole('heading', { name: /Installed the pre-commit hook in acme-org\/api\./ }),
      ).toBeInTheDocument()
      expect(platform.getHookActionResult).toHaveBeenCalledExactlyOnceWith(11)
      expect(platform.getChecksResult).not.toHaveBeenCalled()
    })

    it('opens the run Replace… starts from a blocked install’s result', async () => {
      platform.getHookActionResult.mockResolvedValue(
        hookActionResult({
          outcome: 'nothingInstalled',
          message: 'Nothing installed: husky’s hook is in the way.',
          otherTool: 'husky',
        }),
      )
      platform.replaceHook.mockResolvedValue(12)
      const { onSelect } = renderPanel(hooksRun())

      await userEvent.click(await screen.findByRole('button', { name: 'Replace the hook in acme-org/api…' }))
      await userEvent.click(screen.getByRole('button', { name: 'Replace husky’s hook' }))

      await waitFor(() => {
        expect(onSelect).toHaveBeenCalledExactlyOnceWith(12)
      })
    })

    it('shows how a checks run came out, above the rest of its result, once it ended', async () => {
      renderPanel(checksRun())

      await shownRun('Checks · all files')

      expect(await screen.findByRole('heading', { name: 'Checks in acme-org/web · all files' })).toBeInTheDocument()
      expect(platform.getChecksResult).toHaveBeenCalledExactlyOnceWith(21)
      expect(screen.getByRole('region', { name: 'Result' })).toHaveTextContent('Exit code 1')
      expect(platform.getHookActionResult).not.toHaveBeenCalled()
    })

    it('reads no result while the run goes', async () => {
      renderPanel(running({ kind: 'checks', title: 'Checks · all files' }))

      await shownRun('Checks · all files')

      expect(platform.getChecksResult).not.toHaveBeenCalled()
      expect(platform.getHookActionResult).not.toHaveBeenCalled()
    })
  })
})
