import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { BranchState, BranchStatus } from '../../platform'
import { branchStatus, scriptRun } from '../../test/fixtures'
import { emit, listening, platform } from '../../test/platform'
import { BranchCheck } from './BranchCheck'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderCheck shows the check for acme-org/sdk's clone, with origin answering status.
function renderCheck(status: Partial<BranchStatus> = {}) {
  const onStarted = vi.fn<(id: number) => void>()
  const onPulled = vi.fn<() => void>()

  platform.getBranchStatus.mockResolvedValue(branchStatus(status))
  render(<BranchCheck dir="~/projects/sdk" onStarted={onStarted} onPulled={onPulled} />)

  return { onStarted, onPulled }
}

// behind is the clone's master with newer commits on origin.
const behind: Partial<BranchStatus> = {
  state: 'behind',
  origin: '5d6e7f8',
  note: 'origin/master has newer commits. Pull fast-forwards master to them.',
}

describe('BranchCheck', () => {
  it('shows a spinner while it checks, then that the branch is up to date', async () => {
    renderCheck()

    expect(screen.getByText('Checking origin…')).toBeInTheDocument()

    const label = await screen.findByText('Up to date')
    expect(label).toHaveAttribute('title', expect.stringContaining('master is up to date with origin/master. Checked '))
    expect(platform.getBranchStatus).toHaveBeenCalledExactlyOnceWith('~/projects/sdk', false)
    expect(screen.queryByRole('button', { name: /^Pull/ })).not.toBeInTheDocument()
  })

  it('offers a pull when origin has newer commits', async () => {
    renderCheck(behind)

    expect(await screen.findByText('Update available')).toHaveAttribute(
      'title',
      expect.stringContaining('origin/master has newer commits.'),
    )
    expect(screen.getByRole('button', { name: 'Pull master' })).toBeEnabled()
  })

  it.each<[BranchState, string]>([
    ['ahead', 'Ahead of origin'],
    ['diverged', 'Diverged from origin'],
    ['detached', 'No branch checked out'],
    ['noUpstream', 'Not tracking origin'],
    ['inProgress', 'git is in the middle of something'],
  ])('says %s in a few words, the note in its tooltip, and offers no pull', async (state, words) => {
    renderCheck({ state, note: `The note for ${state}.`, checkedAt: '' })

    expect(await screen.findByText(words)).toHaveAttribute('title', `The note for ${state}.`)
    expect(screen.queryByRole('button', { name: /^Pull/ })).not.toBeInTheDocument()
  })

  it('says in full why origin couldn’t be checked', async () => {
    renderCheck({
      state: 'unknown',
      local: '',
      origin: '',
      checkedAt: '',
      note: 'Couldn’t ask origin about master: Permission denied (publickey).',
    })

    expect(await screen.findByText('Couldn’t ask origin about master: Permission denied (publickey).')).toBeVisible()
    expect(screen.queryByRole('button', { name: /^Pull/ })).not.toBeInTheDocument()
  })

  it('says why the branch couldn’t be read at all', async () => {
    platform.getBranchStatus.mockRejectedValue(new Error('~/projects/gone isn’t a clone in ~/projects.'))
    render(<BranchCheck dir="~/projects/gone" onStarted={vi.fn()} onPulled={vi.fn()} />)

    expect(
      await screen.findByText('Couldn’t check the branch: ~/projects/gone isn’t a clone in ~/projects.'),
    ).toBeVisible()
  })

  it('asks origin again on refresh', async () => {
    const user = userEvent.setup()
    renderCheck()

    await screen.findByText('Up to date')
    platform.getBranchStatus.mockResolvedValue(branchStatus(behind))
    await user.click(screen.getByRole('button', { name: 'Check origin again' }))

    expect(await screen.findByText('Update available')).toBeVisible()
    expect(platform.getBranchStatus).toHaveBeenLastCalledWith('~/projects/sdk', true)
  })

  it('pulls, shows the run, and checks again once it ends', async () => {
    const user = userEvent.setup()
    const { onStarted, onPulled } = renderCheck(behind)
    platform.getScriptRun.mockResolvedValue(scriptRun({ id: 51, kind: 'gitPull', status: 'running' }))

    await user.click(await screen.findByRole('button', { name: 'Pull master' }))

    expect(platform.pullBranch).toHaveBeenCalledExactlyOnceWith('~/projects/sdk')
    expect(onStarted).toHaveBeenCalledExactlyOnceWith(51)
    expect(await screen.findByRole('button', { name: 'Pulling master' })).toBeDisabled()
    await waitFor(() => {
      expect(listening('scriptruns:updated')).toBe(1)
    })
    expect(onPulled).not.toHaveBeenCalled()

    platform.getBranchStatus.mockResolvedValue(branchStatus())
    platform.getScriptRun.mockResolvedValue(scriptRun({ id: 51, kind: 'gitPull', status: 'succeeded' }))
    act(() => {
      emit('scriptruns:updated')
    })

    expect(await screen.findByText('Up to date')).toBeVisible()
    expect(onPulled).toHaveBeenCalledOnce()
    expect(platform.getBranchStatus).toHaveBeenLastCalledWith('~/projects/sdk', false)
    expect(platform.getScriptRun).toHaveBeenCalledWith(51)
    expect(listening('scriptruns:updated')).toBe(0)
  })

  it('says why a pull didn’t start', async () => {
    const user = userEvent.setup()
    const { onStarted } = renderCheck(behind)
    platform.pullBranch.mockRejectedValue(new Error('A script is using acme-org/sdk right now.'))

    await user.click(await screen.findByRole('button', { name: 'Pull master' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t pull master: A script is using acme-org/sdk right now.',
    )
    expect(onStarted).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Pull master' })).toBeEnabled()
  })
})
