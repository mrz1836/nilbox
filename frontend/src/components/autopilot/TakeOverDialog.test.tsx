import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { LaunchResult } from '../../platform'
import { deferred } from '../../test/async'
import { launchResult, runningHold } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { TakeOverDialog, type TakeOverDialogProps } from './TakeOverDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderDialog asks to take session s over, a run still working in pr-7 unless props say
// otherwise.
function renderDialog(props: Partial<TakeOverDialogProps> = {}) {
  const onTaken = vi.fn<(result: LaunchResult) => void>()
  const onCancel = vi.fn()

  render(
    <TakeOverDialog
      sessionId="s"
      live
      terminal="iTerm2"
      where="pr-7"
      agentName="Claude"
      onTaken={onTaken}
      onCancel={onCancel}
      {...props}
    />,
  )

  return { onTaken, onCancel }
}

describe('TakeOverDialog', () => {
  it('opens an ended run’s conversation without stopping anything, saying it opens while it does', async () => {
    const answer = deferred<LaunchResult>()

    platform.takeOverRun.mockReturnValueOnce(answer.promise)
    const { onTaken } = renderDialog({ live: false, where: '' })

    const dialog = within(screen.getByRole('dialog', { name: 'Take over this conversation?' }))
    expect(
      dialog.getByText(`${brand.name} opens its conversation in iTerm2, where you can carry on.`),
    ).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Take over' }))
    expect(dialog.getByRole('button', { name: /Opening…/ })).toBeDisabled()

    const result = launchResult()
    answer.resolve(result)

    await vi.waitFor(() => {
      expect(onTaken).toHaveBeenCalledExactlyOnceWith(result)
    })
  })

  it('says it stops the agent while it does', async () => {
    platform.takeOverRun.mockReturnValueOnce(new Promise(() => undefined))
    renderDialog()

    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))
    expect(screen.getByRole('button', { name: /Stopping the agent…/ })).toBeDisabled()
  })

  it('says why it couldn’t take the run over, and can try again', async () => {
    platform.takeOverRun.mockRejectedValueOnce(
      new Error('The run hasn’t stopped yet. Try Take over again in a moment.'),
    )
    const { onTaken } = renderDialog()

    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The run hasn’t stopped yet.')
    expect(onTaken).not.toHaveBeenCalled()

    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))
    expect(platform.takeOverRun).toHaveBeenCalledTimes(2)
  })

  it('confirms a session running in the worktree apart, as a launch does', async () => {
    const hold = runningHold()

    platform.takeOverRun.mockResolvedValueOnce(launchResult({ hold })).mockResolvedValueOnce(launchResult())
    const { onTaken } = renderDialog()

    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Take over' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Start another' }))

    expect(platform.takeOverRun).toHaveBeenLastCalledWith({
      sessionId: 's',
      profile: false,
      alongside: hold.key,
      confirmed: '',
    })
    await vi.waitFor(() => {
      expect(onTaken).toHaveBeenCalledOnce()
    })
  })
})
