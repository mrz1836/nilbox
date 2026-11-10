import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { ReleaseResult as Result, ScriptRun } from '../../platform'
import { at } from '../../test/clock'
import { releaseResult, releaseRun } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ReleaseResult } from './ReleaseResult'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderResult(result: Partial<Result> | Error = {}, run: ScriptRun = releaseRun()) {
  if (result instanceof Error) {
    platform.getReleaseResult.mockRejectedValue(result)
  } else {
    platform.getReleaseResult.mockResolvedValue(releaseResult(result))
  }

  const handlers = { onTryAgain: vi.fn<() => void>(), onDone: vi.fn<() => void>() }

  render(
    <dialog open>
      <ReleaseResult run={run} repo="acme-org/sdk" {...handlers} />
    </dialog>,
    { wrapper: at() },
  )

  return handlers
}

// card is the result, once read.
async function card() {
  return within(await screen.findByRole('status', { name: /./ }))
}

// lines are the result's lines, with the words that say each one's state.
function lines(c: Awaited<ReturnType<typeof card>>): string[] {
  return c.queryAllByRole('listitem').map((li) => li.textContent)
}

describe('ReleaseResult', () => {
  it('says a release is on GitHub, with its links and its tag to copy, and throws confetti', async () => {
    const { onDone, onTryAgain } = renderResult()

    expect(screen.getByRole('status')).toHaveTextContent('Reading how the release came out…')

    const c = await card()

    expect(c.getByRole('heading', { name: 'acme-org/sdk v1.4.3 is on GitHub' })).toBeInTheDocument()
    expect(lines(c)).toEqual([
      'Done: Tagged a1b2c3d on master and pushed the tag to origin',
      'Done: Your clone is back on feature-x',
    ])
    expect(platform.getReleaseResult).toHaveBeenCalledExactlyOnceWith(7)
    // The confetti flies over the sheet, in its dialog.
    await waitFor(() => {
      expect(document.querySelector('dialog [data-testid="confetti"]')).not.toBeNull()
    })

    await userEvent.click(c.getByRole('button', { name: 'View v1.4.3 on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/sdk/releases/tag/v1.4.3')

    await userEvent.click(c.getByRole('button', { name: 'Watch the release workflow' }))
    expect(platform.openExternal).toHaveBeenLastCalledWith(
      'https://github.com/acme-org/sdk/actions?query=branch%3Av1.4.3',
    )

    await userEvent.click(c.getByRole('button', { name: 'Copy v1.4.3' }))
    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('v1.4.3')
    expect(await c.findByRole('button', { name: 'Copied' })).toBeInTheDocument()

    platform.copyText.mockRejectedValue(new Error('The clipboard refused it.'))
    await userEvent.click(c.getByRole('button', { name: 'Copied' }))
    expect(await c.findByRole('alert')).toHaveTextContent('The clipboard refused it.')

    expect(c.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
    await userEvent.click(c.getByRole('button', { name: 'Done' }))
    expect(onDone).toHaveBeenCalledOnce()
    expect(onTryAgain).not.toHaveBeenCalled()
  })

  it('throws no confetti when the system asks for less motion', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
    renderResult()

    await card()
    // Let the effect that would throw it run.
    await act(async () => {
      await new Promise((resolve) => {
        setTimeout(resolve, 0)
      })
    })

    expect(document.querySelector('[data-testid="confetti"]')).toBeNull()
    vi.unstubAllGlobals()
  })

  it('says when the version moved, or a newer commit was tagged, than the sheet showed', async () => {
    renderResult({ tag: 'v1.4.4', moved: true, newer: true, head: '9f8e7d6c5b4a', commit: 'b2c3d4e5f6a7' })

    const c = await card()

    expect(c.getByRole('heading', { name: 'acme-org/sdk v1.4.4 is on GitHub' })).toBeInTheDocument()
    expect(lines(c)).toEqual([
      'Done: Tagged b2c3d4e on master and pushed the tag to origin',
      'Note: Released v1.4.4. v1.4.3 appeared on GitHub meanwhile.',
      'Note: v1.4.4 tags b2c3d4e, which is newer than the 9f8e7d6 you reviewed.',
      'Done: Your clone is back on feature-x',
    ])
  })

  it('says a tag isn’t on GitHub, with the commands to push or undo it, and opens the terminal', async () => {
    const { onTryAgain } = renderResult({
      kind: 'notPushed',
      tagUrl: '',
      workflowUrl: '',
      pushWarning: 'Failed to push tag to remote: could not read Username',
      commands: ['git push origin v1.4.3', 'git tag -d v1.4.3'],
      back: false,
      branch: 'master',
      failure: { kind: 'hook', heading: 'Your pre-push hook stopped the push: lint failed', quote: '', bug: false },
    })

    const c = await card()

    expect(c.getByRole('heading', { name: 'v1.4.3 is tagged on this Mac but isn’t on GitHub' })).toBeInTheDocument()
    expect(c.getByText('Your pre-push hook stopped the push: lint failed')).toBeInTheDocument()
    expect(lines(c)).toEqual([
      'Warning: magex said: Failed to push tag to remote: could not read Username',
      'Warning: Your clone is on master. It was on feature-x.',
    ])
    expect(c.getByText('To push it:')).toBeInTheDocument()
    expect(c.getByText('To undo it:')).toBeInTheDocument()
    expect(c.getByText(`${brand.name} runs neither. Open your terminal in the clone to run one.`)).toBeInTheDocument()
    expect(c.queryByRole('button', { name: /on GitHub/ })).not.toBeInTheDocument()
    expect(c.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()

    await userEvent.click(c.getByRole('button', { name: 'Copy git tag -d v1.4.3' }))
    expect(platform.copyText).toHaveBeenCalledWith('git tag -d v1.4.3')

    await userEvent.click(c.getByRole('button', { name: 'Open in terminal' }))
    expect(platform.openScriptRunInTerminal).toHaveBeenCalledExactlyOnceWith(7)
    expect(onTryAgain).not.toHaveBeenCalled()
  })

  it('names a failure in words, quotes magex, says where the clone is, and offers to try again', async () => {
    const { onTryAgain } = renderResult({
      kind: 'failed',
      tagUrl: '',
      workflowUrl: '',
      tagged: true,
      taken: true,
      back: false,
      detached: true,
      branch: '',
      inProgress: 'rebasing',
      commands: ['git tag -d v1.4.3'],
      failure: {
        kind: 'unconfirmed',
        heading: 'magex wanted a confirmation the app didn’t send.',
        quote: 'major version bump requires explicit confirmation',
        bug: true,
      },
    })

    const c = await card()

    expect(c.getByRole('heading', { name: 'magex wanted a confirmation the app didn’t send.' })).toBeInTheDocument()
    expect(c.getByText('major version bump requires explicit confirmation')).toBeInTheDocument()
    expect(c.getByText(/please report it from Settings → About/)).toBeInTheDocument()
    expect(lines(c)).toEqual([
      'Warning: v1.4.3 is tagged on this Mac at a commit GitHub doesn’t have.',
      'Warning: Your clone is on a detached commit. It was on feature-x.',
      'Problem: A rebase is in progress in your clone. Finish or abort it in your terminal.',
    ])

    await userEvent.click(c.getByRole('button', { name: 'Try again' }))
    expect(onTryAgain).toHaveBeenCalledOnce()
  })

  it('says when GitHub couldn’t be asked whether the release is there', async () => {
    renderResult({
      kind: 'unverified',
      tagUrl: '',
      workflowUrl: '',
      error: 'origin timed out',
      back: false,
      branch: '',
      original: '',
    })

    const c = await card()

    expect(c.getByRole('heading', { name: 'Couldn’t check whether v1.4.3 is on GitHub' })).toBeInTheDocument()
    expect(lines(c)).toEqual([`Warning: It may be on GitHub: ${brand.name} couldn’t ask origin. origin timed out`])
    expect(c.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('says a clone left on another branch, and names a failure without a tag as failed', async () => {
    renderResult({
      kind: 'failed',
      tag: '',
      tagged: false,
      back: false,
      branch: 'master',
      original: '',
      tagUrl: '',
      workflowUrl: '',
    })

    const c = await card()

    expect(c.getByRole('heading', { name: 'The release of v1.4.3 failed' })).toBeInTheDocument()
    expect(lines(c)).toEqual(['Warning: Your clone is on master.'])
  })

  it('says why the result couldn’t be read, with Try again and Done', async () => {
    const { onTryAgain, onDone } = renderResult(new Error('This release ended without a result to show.'))

    expect(await screen.findByRole('alert')).toHaveTextContent('This release ended without a result to show.')
    expect(screen.getByRole('heading', { name: 'v1.4.3 is on GitHub.' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await userEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(onTryAgain).toHaveBeenCalledOnce()
    expect(onDone).toHaveBeenCalledOnce()
  })

  it('leaves out what it can’t say: the branch the run didn’t keep, and a warning magex didn’t print', async () => {
    renderResult(
      { kind: 'notPushed', tagUrl: '', workflowUrl: '', commands: [] },
      releaseRun({ result: { ...releaseRun().result, intent: null } }),
    )

    const c = await card()

    expect(lines(c)).toEqual(['Done: Your clone is back on feature-x'])
    expect(c.queryByRole('button', { name: 'Open in terminal' })).not.toBeInTheDocument()
  })

  it('says a release on GitHub without naming a branch the run didn’t keep', async () => {
    renderResult({}, releaseRun({ result: { ...releaseRun().result, intent: null } }))

    const c = await card()

    expect(lines(c)[0]).toBe('Done: Tagged a1b2c3d and pushed the tag to origin')
  })

  it('heads an unreadable result plainly when the run said nothing', async () => {
    renderResult(new Error('This release ended without a result to show.'), releaseRun({ summary: '' }))

    expect(await screen.findByRole('heading', { name: 'The release ended without a result' })).toBeInTheDocument()
  })
})
