import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { formatClock } from '../../lib/time'
import type { AutopilotRunEvents } from '../../platform'
import { deferred } from '../../test/async'
import { renderAt } from '../../test/clock'
import {
  agentMatch,
  agentRun,
  agentRunView,
  agents,
  githubStep,
  launchHold,
  launchResult,
  runEnding,
  runEvent,
  runEventsPage,
} from '../../test/fixtures'
import { emit, platform, send } from '../../test/platform'
import { RunView } from './RunView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// say is the agent saying "Step <i>", from the log's line at offset i*100.
function say(i: number, seq = 0) {
  return runEvent({ seq, offset: i * 100, text: `Step ${String(i)}` })
}

// renderView shows run 81, its timeline's first read answering events: a run still working
// by default, or, for null, what the test set up.
function renderView(
  props: Partial<Parameters<typeof RunView>[0]> = {},
  events: AutopilotRunEvents | null = runEventsPage(),
) {
  const onToast = vi.fn()
  const onOpenItem = vi.fn()

  if (events !== null) {
    platform.autopilotRunEvents.mockResolvedValue(events)
  }

  renderAt(
    <RunView
      runId={81}
      terminal="iterm2"
      agents={agents()}
      onOpenItem={onOpenItem}
      onToast={onToast}
      onClose={vi.fn()}
      {...props}
    />,
  )

  return { onToast, onOpenItem }
}

describe('RunView', () => {
  it('heads the run with its routine, its item, where it stands, and the routine’s steps', async () => {
    renderView(
      {},
      runEventsPage({
        events: [say(1, 1)],
        progress: {
          step: 'Moving esbuild',
          todos: [
            { text: 'Read the failing checks', status: 'completed' },
            { text: 'Move esbuild to a fix', status: 'in_progress' },
            { text: 'Commit and push', status: 'pending' },
          ],
          tokensIn: 12345,
          tokensOut: 640,
          cacheRead: 80000,
          events: 1,
          unknown: 2,
        },
      }),
    )

    const view = await screen.findByRole('region', { name: 'Fix failing checks' })

    expect(within(view).getByRole('status')).toHaveTextContent(
      'Working · 30 min · budget $3.00 · Claude · opus · high · pr-7 · Edit and test, no push',
    )
    expect(await within(view).findByText('12.3k in · 640 out · 80k from cache tokens')).toBeInTheDocument()
    expect(within(view).getByLabelText('The routine’s steps')).toHaveTextContent('1 Agent · 2 Wait for the checks')
    expect(within(view).getByText('1 Agent')).toHaveAttribute('aria-current', 'step')

    const plan = within(view).getByRole('region', { name: 'The agent’s plan' })
    expect(plan).toHaveTextContent('✓Read the failing checks')
    expect(plan).toHaveTextContent('◐Move esbuild to a fix')
    expect(plan).toHaveTextContent('○Commit and push')

    const timeline = within(view).getByRole('region', { name: 'Timeline' })
    await within(timeline).findByText('“Step 1”')
    expect(timeline).toHaveTextContent('2 messages from the agent weren’t shown.')

    for (const name of [
      'Take over in iTerm2',
      'Stop',
      'Diff',
      'Show the worktree',
      'View on GitHub',
      'Show the log file',
    ]) {
      expect(within(view).getByRole('button', { name })).toBeInTheDocument()
    }
  })

  it('follows the run as its stream moves on, asking only for what is new', async () => {
    platform.autopilotRunEvents.mockResolvedValueOnce(runEventsPage({ events: [say(1, 1), say(2, 2)] }))
    renderView({}, null)

    await screen.findByText('“Step 2”')

    platform.autopilotRunEvents.mockResolvedValueOnce(runEventsPage({ events: [say(3, 3)] }))
    send('autopilot:run', { runId: 81 })

    await screen.findByText('“Step 3”')
    expect(platform.autopilotRunEvents).toHaveBeenLastCalledWith(81, 2)

    emit('autopilot:run', { runId: 99 })
    expect(platform.autopilotRunEvents).toHaveBeenCalledTimes(2)
  })

  it('pages a long timeline back from its log, a page at a time, each event once', async () => {
    const user = userEvent.setup()

    platform.autopilotRunEventsBefore
      .mockResolvedValueOnce(runEventsPage({ events: [say(200), say(201)], earlier: true, before: 20000, done: true }))
      .mockResolvedValueOnce(runEventsPage({ events: [say(1), say(2)], earlier: false, before: 0, done: true }))
    renderView(
      {},
      runEventsPage({ events: [say(300), say(301)], earlier: true, before: 30000, done: true, ending: runEnding() }),
    )

    const timeline = await screen.findByRole('region', { name: 'Timeline' })
    await within(timeline).findByText('“Step 300”')

    await user.click(within(timeline).getByRole('button', { name: 'Show earlier events' }))
    await within(timeline).findByText('“Step 200”')
    expect(platform.autopilotRunEventsBefore).toHaveBeenLastCalledWith(81, 30000)

    await user.click(within(timeline).getByRole('button', { name: 'Show earlier events' }))
    await within(timeline).findByText('“Step 1”')
    expect(platform.autopilotRunEventsBefore).toHaveBeenLastCalledWith(81, 20000)

    expect(within(timeline).queryByRole('button', { name: 'Show earlier events' })).toBeNull()
    expect(
      within(timeline)
        .getAllByText(/^“Step \d+”$/)
        .map((el) => el.textContent),
    ).toEqual(['“Step 1”', '“Step 2”', '“Step 200”', '“Step 201”', '“Step 300”', '“Step 301”'])
  })

  it('says how a run that ended came out, in the agent’s words and GitHub’s', async () => {
    platform.autopilotRunView.mockResolvedValue(
      agentRunView({
        live: false,
        run: agentRun({
          status: 'ended',
          outcome: 'done',
          cost: 0.61,
          costEstimated: true,
          verdict: 'fixed',
        }),
        match: agentMatch({
          steps: [
            githubStep({ kind: 'agent', label: 'Agent', status: 'done', runId: 81, actions: [] }),
            githubStep({
              kind: 'waitForChecks',
              label: 'Wait for the checks',
              status: 'done',
              verdict: 'fixed',
              note: 'Fixed: checks pass on 4f2a9c1.',
              actions: [],
            }),
          ],
        }),
      }),
    )
    renderView({}, runEventsPage({ done: true, ending: runEnding() }))

    const ended = await screen.findByRole('region', { name: 'How it ended' })

    expect(ended).toHaveTextContent('Fixed: checks pass on 4f2a9c1.')
    expect(ended).toHaveTextContent('The agent reports:Moved esbuild to 0.25.1, which fixes GHSA-xxxx.')
    expect(ended).toHaveTextContent('12 turns · 6 min 12 s · on 4f2a9c1 · changed 2 files')
    expect(screen.getByRole('status')).toHaveTextContent('Finished: fixed. · 30 min · ≈ $0.61 of $3.00')
    expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull()
  })

  it('stops the run', async () => {
    const user = userEvent.setup()
    const { onToast } = renderView()

    await user.click(await screen.findByRole('button', { name: 'Stop' }))

    expect(platform.stopAutopilotRun).toHaveBeenCalledExactlyOnceWith(81)
    expect(onToast).toHaveBeenCalledWith('Stopping the agent’s run.')
  })

  it('shows the worktree’s changes since the run started, read-only', async () => {
    const user = userEvent.setup()

    platform.autopilotRunDiff.mockResolvedValue({
      from: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
      patch: 'diff --git a/package.json b/package.json\n-  "esbuild": "0.24.0"\n+  "esbuild": "0.25.1"\n',
      cut: false,
      untracked: ['notes.md'],
    })
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Diff' }))

    const diff = await screen.findByRole('region', { name: 'Diff' })
    expect(diff).toHaveTextContent('Changes since a1b2c3d, committed or not.')
    expect(diff).toHaveTextContent('+ "esbuild": "0.25.1"')
    expect(diff).toHaveTextContent('Not tracked by git yet:notes.md')
    expect(screen.getByRole('button', { name: 'Diff' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Diff' }))
    expect(screen.queryByRole('region', { name: 'Diff' })).toBeNull()
    expect(screen.getByRole('region', { name: 'Timeline' })).toBeInTheDocument()
  })

  it('takes the run over, keeping its fence unless the box is ticked', async () => {
    const user = userEvent.setup()
    const { onToast } = renderView()

    await user.click(await screen.findByRole('button', { name: 'Take over in iTerm2' }))

    const dialog = screen.getByRole('dialog', { name: 'Take over this run?' })
    expect(dialog).toHaveTextContent('stops the agent and opens its conversation in iTerm2, in pr-7')
    expect(dialog).toHaveTextContent('It’s no longer unattended once you take over.')

    const box = within(dialog).getByRole('checkbox', { name: /Use the profile’s permissions/ })
    expect(box).not.toBeChecked()

    await user.click(within(dialog).getByRole('button', { name: 'Take over' }))

    expect(platform.takeOverRun).toHaveBeenCalledExactlyOnceWith({
      sessionId: agentRun().sessionId,
      profile: false,
      alongside: '',
      confirmed: '',
    })
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(expect.stringContaining('its conversation carries on in iTerm2'))
    })

    await user.click(screen.getByRole('button', { name: 'Take over in iTerm2' }))
    await user.click(screen.getByRole('checkbox', { name: /Use the profile’s permissions/ }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))

    expect(platform.takeOverRun).toHaveBeenLastCalledWith(expect.objectContaining({ profile: true }))
  })

  it('asks about a hold the take over meets, and confirms exactly it', async () => {
    const user = userEvent.setup()
    const hold = launchHold()

    platform.takeOverRun.mockResolvedValueOnce(launchResult({ hold })).mockResolvedValueOnce(launchResult())
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Take over in iTerm2' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))
    await user.click(await screen.findByRole('button', { name: 'Open anyway' }))

    expect(platform.takeOverRun).toHaveBeenLastCalledWith({
      sessionId: agentRun().sessionId,
      profile: false,
      alongside: '',
      confirmed: hold.key,
    })
  })

  it('offers no Take over for a run whose agent can’t resume one', async () => {
    platform.autopilotRunView.mockResolvedValue(agentRunView({ canTakeOver: false }))
    renderView()

    await screen.findByRole('button', { name: 'Stop' })
    expect(screen.queryByRole('button', { name: /Take over/ })).toBeNull()
  })

  it('shows every kind of row, each command marked with how it came out', async () => {
    const tool = (offset: number, category: 'command' | 'edit', text: string) =>
      runEvent({ offset, kind: 'tool', tool: 'Bash', category, text })
    const done = (offset: number, error = false) => runEvent({ offset, kind: 'toolResult', text: 'out', error })

    renderView(
      {},
      runEventsPage({
        events: [
          runEvent({ offset: 0, kind: 'started', text: 'claude-opus-5-5', at: '' }),
          tool(10, 'command', '$ gh pr checks 7'),
          done(11),
          tool(20, 'command', '$ pnpm test'),
          done(21, true),
          tool(30, 'edit', 'package.json'),
          runEvent({ offset: 40, kind: 'denied', tool: 'Bash', text: 'git push isn’t allowed', error: true }),
          runEvent({ offset: 50, kind: 'retry', text: 'Retrying (1 of 10)' }),
          tool(60, 'command', '$ magex lint'),
        ],
      }),
    )

    const timeline = await screen.findByRole('region', { name: 'Timeline' })
    const rows = await within(timeline).findAllByRole('listitem')

    expect(rows.map((r) => r.textContent)).toEqual([
      'Started: claude-opus-5-5',
      `${formatClock(runEvent().at)}$ gh pr checks 7✓`,
      `${formatClock(runEvent().at)}$ pnpm test✗`,
      `${formatClock(runEvent().at)}Edited package.json…`,
      `${formatClock(runEvent().at)}Not allowed: Bash. git push isn’t allowed`,
      `${formatClock(runEvent().at)}Retrying (1 of 10)`,
      `${formatClock(runEvent().at)}$ magex lint…`,
    ])
    expect(within(timeline).getByLabelText('worked')).toBeInTheDocument()
    expect(within(timeline).getByLabelText('failed')).toBeInTheDocument()
  })

  it('says it is reading the run, and why it couldn’t', async () => {
    const onClose = vi.fn()

    platform.autopilotRunView.mockRejectedValueOnce(new Error('That run is no longer in Autopilot’s history.'))
    renderView({ onClose })

    expect(screen.getByText('Reading the run…')).toBeInTheDocument()
    expect(await screen.findByText('That run is no longer in Autopilot’s history.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close the run' }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('reads the run again as Autopilot changes, and says why its timeline couldn’t be read', async () => {
    platform.autopilotRunEvents
      .mockRejectedValueOnce(new Error('This run’s events couldn’t be read.'))
      .mockResolvedValue(runEventsPage({ events: [say(1, 1)] }))
    renderView({}, null)

    expect(await screen.findByText('This run’s events couldn’t be read.')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Timeline' })).getByText('Reading the run…')).toBeInTheDocument()

    send('autopilot:updated')

    expect(await screen.findByText('“Step 1”')).toBeInTheDocument()
    expect(screen.queryByText('This run’s events couldn’t be read.')).toBeNull()
    expect(platform.autopilotRunView).toHaveBeenCalledTimes(2)
  })

  it('reads one page at a time, the reads asked for meanwhile once it ends', async () => {
    const answer = deferred<AutopilotRunEvents>()

    platform.autopilotRunEvents
      .mockResolvedValueOnce(runEventsPage({ events: [say(1, 1)] }))
      .mockReturnValueOnce(answer.promise)
      .mockResolvedValueOnce(runEventsPage({ events: [say(3, 3)], done: true }))
    renderView({}, null)

    await screen.findByText('“Step 1”')

    act(() => {
      emit('autopilot:run', { runId: 81 })
      emit('autopilot:run', { runId: 81 })
      emit('autopilot:run', { runId: 81 })
    })
    expect(platform.autopilotRunEvents).toHaveBeenCalledTimes(2)

    act(() => {
      answer.resolve(runEventsPage({ events: [say(2, 2)] }))
    })

    await screen.findByText('“Step 3”')
    expect(platform.autopilotRunEvents).toHaveBeenCalledTimes(3)
    expect(platform.autopilotRunEvents).toHaveBeenLastCalledWith(81, 2)

    send('autopilot:run', { runId: 81 })
    expect(platform.autopilotRunEvents).toHaveBeenCalledTimes(3)
  })

  it('says why an earlier page couldn’t be read', async () => {
    platform.autopilotRunEventsBefore.mockRejectedValueOnce(new Error('This run’s earlier events couldn’t be read.'))
    renderView({}, runEventsPage({ events: [say(300)], earlier: true, before: 30000, done: true }))

    await userEvent.click(await screen.findByRole('button', { name: 'Show earlier events' }))

    expect(await screen.findByText('This run’s earlier events couldn’t be read.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show earlier events' })).toBeEnabled()
  })

  it('shows the worktree, the item on GitHub, the log file, and the item', async () => {
    const user = userEvent.setup()
    const { onOpenItem } = renderView()

    await user.click(await screen.findByRole('button', { name: 'Show the worktree' }))
    expect(platform.showSessionFolder).toHaveBeenCalledExactlyOnceWith(agentRun().sessionId)

    await user.click(screen.getByRole('button', { name: 'View on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(agentRunView().match.item.url)

    await user.click(screen.getByRole('button', { name: 'Show the log file' }))
    expect(platform.showScriptRunLog).toHaveBeenCalledExactlyOnceWith(44)

    await user.click(screen.getByRole('button', { name: 'example-repo #7 · Add the widget cache' }))
    expect(onOpenItem).toHaveBeenCalledExactlyOnceWith(agentRunView().match.item, 3)
  })

  it('says a diff is cut short, or holds no change, and why it couldn’t be read', async () => {
    const user = userEvent.setup()

    platform.autopilotRunDiff
      .mockResolvedValueOnce({ from: 'a1b2c3d4e5f6', patch: '+a\n\n+b\n', cut: true, untracked: [] })
      .mockResolvedValueOnce({ from: 'a1b2c3d4e5f6', patch: '', cut: false, untracked: [] })
      .mockRejectedValueOnce(new Error('This run’s changes couldn’t be read. Its worktree may be gone.'))
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Diff' }))
    expect(await screen.findByRole('region', { name: 'Diff' })).toHaveTextContent(
      'Changes since a1b2c3d, committed or not. The diff is long, so only its start shows.',
    )

    await user.click(screen.getByRole('button', { name: 'Diff' }))
    await user.click(screen.getByRole('button', { name: 'Diff' }))
    expect(await screen.findByText('No changes to tracked files.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Diff' }))
    await user.click(screen.getByRole('button', { name: 'Diff' }))
    expect(
      await screen.findByText('This run’s changes couldn’t be read. Its worktree may be gone.'),
    ).toBeInTheDocument()
  })

  it('says why a run that didn’t report needs a person', async () => {
    platform.autopilotRunView.mockResolvedValue(agentRunView({ live: false, run: agentRun({ status: 'ended' }) }))
    renderView(
      {},
      runEventsPage({
        done: true,
        ending: runEnding({
          reported: false,
          report: '',
          message: '',
          noReport: 'It was stopped before it reported.',
          needsPerson: true,
          reason: 'The fix needs a major version.',
          turns: 0,
          durationMs: 0,
          commit: '',
          changed: [],
          refused: 2,
        }),
      }),
    )

    const ended = await screen.findByRole('region', { name: 'How it ended' })
    expect(ended).toHaveTextContent('The agent didn’t report. It last said:It was stopped before it reported.')
    expect(ended).toHaveTextContent('It needs a person: The fix needs a major version.')
    expect(ended).toHaveTextContent('its fence refused 2 calls')
  })

  it('names your terminal before the settings load, and a run not started yet or a preset it doesn’t know', async () => {
    platform.autopilotRunView.mockResolvedValue(
      agentRunView({ run: agentRun({ startedAt: null, preset: 'custom', model: '', effort: '' }) }),
    )
    renderView({ terminal: null })

    expect(await screen.findByRole('button', { name: 'Take over in your terminal' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Working · budget $3.00 · Claude · pr-7 · custom')
  })

  it('says why Stop failed, and puts the take over away on Cancel', async () => {
    const user = userEvent.setup()

    platform.stopAutopilotRun.mockRejectedValueOnce(new Error('The run couldn’t be stopped.'))
    renderView()

    await user.click(await screen.findByRole('button', { name: 'Stop' }))
    expect(await screen.findByText('The run couldn’t be stopped.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Take over in iTerm2' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(platform.takeOverRun).not.toHaveBeenCalled()
  })

  it('says where a run in the terminal is followed, with no timeline or diff of its own', async () => {
    platform.autopilotRunView.mockResolvedValue(
      agentRunView({ canTakeOver: false, run: agentRun({ runner: 'terminal', scriptRunId: 0 }) }),
    )
    renderView()

    expect(await screen.findByText(/It runs in your terminal, not in the background/)).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Timeline' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Diff' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Show the log file' })).toBeNull()
  })

  it('marks the routine’s steps done before the one it is at', async () => {
    const view = agentRunView()
    const steps = view.match.steps.map((s, i) => (i === 0 ? { ...s, status: 'done' } : s))

    platform.autopilotRunView.mockResolvedValue({
      ...view,
      match: { ...view.match, steps, step: 1, state: 'verifying' },
    })
    renderView()

    expect(await screen.findByText('2 Wait for the checks')).toHaveAttribute('aria-current', 'step')
    expect(screen.getByText('1 Agent')).toHaveClass('text-text')
  })
})
