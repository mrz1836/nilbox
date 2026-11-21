import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { FileCheck } from '../platform'
import { deferred } from '../test/async'
import { at } from '../test/clock'
import { agentGroup, fileCheck, workflowsGroup } from '../test/fixtures'
import { platform } from '../test/platform'
import { expectWraps } from '../test/wrap'
import { SensitiveFiles } from './SensitiveFiles'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderChip(check: FileCheck = fileCheck(), onSettings?: () => void) {
  render(<SensitiveFiles itemId={7} check={check} onSettings={onSettings} />, { wrapper: at() })
}

async function openPanel(name: RegExp) {
  await userEvent.click(screen.getByRole('button', { name }))

  return within(screen.getByRole('dialog', { name: 'Sensitive files' }))
}

beforeEach(() => {
  platform.checkItemFiles.mockResolvedValue(fileCheck())
})

describe('SensitiveFiles', () => {
  it('names the categories on the chip, with the files in its tooltip', () => {
    renderChip()

    const chip = screen.getByRole('button', { name: /^Changes sensitive files/ })
    expect(chip).toHaveTextContent('Agent config · Workflows')
    expect(chip).toHaveAttribute('title', 'Changes sensitive files: Agent config (1 file), Workflows (1 file)')
    expect(chip).toHaveClass('text-danger')
    expect(chip).toHaveAttribute('aria-expanded', 'false')
  })

  it('lists the files by category, each opening its diff', async () => {
    renderChip()

    const panel = await openPanel(/^Changes sensitive files/)

    expect(panel.getByText('At commit', { exact: false })).toHaveTextContent('At commit a1b2c3d · checked 2m ago')
    expect(panel.getByText(/2 files here deserve a close look/)).toBeInTheDocument()

    const agent = within(panel.getByRole('region', { name: 'Agent configuration' }))
    expect(agent.getByText(/coding agents load/)).toBeInTheDocument()
    expect(agent.getByText('added')).toBeInTheDocument()

    const workflows = within(panel.getByRole('region', { name: 'Workflows and CI' }))
    expect(workflows.getByTitle('moved from .github/workflows/codeql.yml')).toHaveTextContent(
      'moved from .github/workflows/codeql.yml',
    )

    await userEvent.click(workflows.getByRole('button', { name: /docs\/old-codeql\.yml/ }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/pull/7/files#diff-bbbb',
    )

    expect(panel.getByText(/Opening a session on it asks first/)).toBeInTheDocument()
  })

  it('shows a file it cannot link as plain text', async () => {
    const file = { path: '.claude/settings.json', previousPath: '', change: 'added', pattern: '.claude/', url: '' }
    const group = { ...agentGroup(), files: [file] }
    renderChip(fileCheck({ groups: [group] }))

    const panel = await openPanel(/^Changes sensitive files/)

    const path = panel.getByTitle('.claude/settings.json')
    expect(path.tagName).toBe('SPAN')
    expect(path).toHaveTextContent('.claude/settings.json')
    expect(panel.queryByRole('button', { name: /\.claude\/settings\.json/ })).not.toBeInTheDocument()
  })

  it('wraps its sentences, even opened from a line that does not wrap', async () => {
    const workflows = workflowsGroup({
      description:
        'Run on CI with the repository’s token and secrets once merged, and some run on the pull request itself.',
    })
    render(
      <div className="whitespace-nowrap">
        <SensitiveFiles itemId={7} check={fileCheck({ groups: [agentGroup(), workflows], complete: false })} />
      </div>,
      { wrapper: at() },
    )

    const panel = await openPanel(/^Changes sensitive files/)
    const dialog = screen.getByRole('dialog', { name: 'Sensitive files' })

    expect(dialog).toHaveClass('whitespace-normal', 'break-words', 'w-[26rem]', 'max-w-[calc(100vw-2rem)]')
    for (const element of [
      panel.getByText('At commit', { exact: false }),
      panel.getByText(/deserve a close look before you review or open a session/),
      panel.getByText(/once merged, and some run on the pull request itself/),
      panel.getByText(/coding agents load/),
      panel.getByRole('heading', { name: /Workflows and CI/ }),
      panel.getByRole('note'),
      panel.getByText(/Opening a session on it asks first/),
      panel.getByText('Settings → Sensitive files'),
    ]) {
      expectWraps(element, dialog)
    }
  })

  it('keeps how a long path changed whole, its folders giving way first', async () => {
    const path = '.github/workflows/provenance-qualification-of-every-release-artifact.yml'
    const added = { path, previousPath: '', change: 'added', pattern: '.github/workflows/', url: 'https://x.test/a' }
    const moved = {
      path: 'docs/retired-workflows/provenance-qualification.yml',
      previousPath: '.github/workflows/provenance-qualification-of-every-nightly-build.yml',
      change: 'renamed',
      pattern: '.github/workflows/',
      url: '',
    }
    renderChip(fileCheck({ groups: [workflowsGroup({ files: [added, moved] })] }))

    const panel = await openPanel(/^Changes sensitive files/)

    const change = panel.getByTitle('added')
    const words = within(change).getByText('added')
    expect(change).toHaveClass('shrink-0')
    expect(words).toHaveClass('shrink-0', 'whitespace-nowrap')
    for (const name of ['truncate', 'text-ellipsis', 'min-w-0']) {
      expect(change).not.toHaveClass(name)
      expect(words).not.toHaveClass(name)
    }

    const link = panel.getByRole('button', { name: path })
    expect(link).toHaveAttribute('title', `Open the diff of ${path} on GitHub`)
    expect(within(link).getByText('.github/workflows/')).toHaveClass('min-w-0', 'truncate')
    expect(within(link).getByText('provenance-qualification-of-every-release-artifact.yml')).toHaveClass(
      'max-w-full',
      'shrink-0',
      'truncate',
    )

    expect(panel.getByTitle(moved.path)).toHaveTextContent(moved.path)
    const from = panel.getByTitle(`moved from ${moved.previousPath}`)
    expect(from).toHaveClass('shrink-0', 'max-w-1/2')
    expect(from).toHaveTextContent(`moved from ${moved.previousPath}`)
    expect(within(from).getByText('moved from')).toHaveClass('shrink-0', 'whitespace-nowrap')
    expect(within(from).getByText('.github/workflows/')).toHaveClass('min-w-0', 'truncate')
  })

  it('says when GitHub listed only some of the files', async () => {
    renderChip(fileCheck({ complete: false, checked: 3000, total: 4210 }))

    const panel = await openPanel(/^Changes sensitive files/)

    expect(panel.getByRole('note')).toHaveTextContent('GitHub listed 3,000 of 4,210 changed files')
  })

  it('checks the files again on request', async () => {
    const check = deferred<FileCheck>()
    platform.checkItemFiles.mockReturnValue(check.promise)
    renderChip(fileCheck({ status: 'failed', groups: [], error: 'GitHub answered 502' }))

    const panel = await openPanel(/^Couldn’t check the changed files/)
    expect(panel.getByText(/Couldn’t check this pull request’s files: GitHub answered 502\./)).toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Check now' }))

    expect(platform.checkItemFiles).toHaveBeenCalledWith(7)
    expect(panel.getByRole('button', { name: 'Checking…' })).toBeDisabled()

    check.resolve(fileCheck())
    expect(await panel.findByRole('button', { name: 'Check now' })).toBeEnabled()
  })

  it('shows why checking again failed', async () => {
    platform.checkItemFiles.mockRejectedValue(new Error('not logged in to GitHub'))
    renderChip(fileCheck({ status: 'pending', groups: [] }))

    const panel = await openPanel(/^Not checked since its last push/)
    expect(panel.getByText(/Not checked since its last push/)).toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Check now' }))

    expect(await panel.findByRole('alert')).toHaveTextContent('not logged in to GitHub')
  })

  it('shows a quiet shield for a pull request with nothing sensitive', async () => {
    renderChip(fileCheck({ status: 'clear', groups: [], checked: 12, total: 12 }))

    const chip = screen.getByRole('button', { name: 'No sensitive files among its 12 changed files' })
    expect(chip).toHaveTextContent('')
    expect(chip).toHaveClass('text-muted')

    const panel = await openPanel(/^No sensitive files/)
    expect(panel.getByText('None of its 12 checked files is sensitive.')).toBeInTheDocument()
    expect(panel.queryByText(/asks first/)).not.toBeInTheDocument()
  })

  it('opens the settings from the panel', async () => {
    const onSettings = vi.fn()
    renderChip(fileCheck(), onSettings)

    const panel = await openPanel(/^Changes sensitive files/)
    await userEvent.click(panel.getByRole('button', { name: 'What counts as sensitive' }))

    expect(onSettings).toHaveBeenCalledOnce()
  })

  it('names the settings page when it cannot open it', async () => {
    renderChip()

    const panel = await openPanel(/^Changes sensitive files/)

    expect(panel.getByText('Settings → Sensitive files')).toBeInTheDocument()
  })

  it('fits the panel inside the area it scrolls in', async () => {
    render(
      <div style={{ overflowY: 'auto' }}>
        <SensitiveFiles itemId={7} check={fileCheck()} />
      </div>,
      { wrapper: at() },
    )

    await userEvent.click(screen.getByRole('button', { name: /^Changes sensitive files/ }))

    const panel = screen.getByRole('dialog', { name: 'Sensitive files' })
    expect(panel.style.maxHeight).toBe('160px')
  })

  it('closes on Escape, back on the chip', async () => {
    renderChip()

    await openPanel(/^Changes sensitive files/)
    await userEvent.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /^Changes sensitive files/ })).toHaveFocus()
  })
})
