import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { FileCheck } from '../platform'
import { agentGroup, fileCheck } from '../test/fixtures'
import { SensitiveFiles } from './SensitiveFiles'

const platform = vi.hoisted(() => ({
  checkItemFiles: vi.fn(),
  openExternal: vi.fn(),
}))

vi.mock('../platform', () => platform)

const now = new Date('2026-06-15T12:00:00Z')

function renderChip(check: FileCheck = fileCheck(), onSettings?: () => void) {
  render(<SensitiveFiles itemId={7} check={check} now={now} onSettings={onSettings} />)
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
    expect(workflows.getByText('moved from .github/workflows/codeql.yml')).toBeInTheDocument()

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

    expect(panel.getByText('.claude/settings.json').tagName).toBe('SPAN')
    expect(panel.queryByRole('button', { name: /\.claude\/settings\.json/ })).not.toBeInTheDocument()
  })

  it('says when GitHub listed only some of the files', async () => {
    renderChip(fileCheck({ complete: false, checked: 3000, total: 4210 }))

    const panel = await openPanel(/^Changes sensitive files/)

    expect(panel.getByRole('note')).toHaveTextContent('GitHub listed 3,000 of 4,210 changed files')
  })

  it('checks the files again on request', async () => {
    let finish: (check: FileCheck) => void = () => undefined
    platform.checkItemFiles.mockReturnValue(
      new Promise<FileCheck>((resolve) => {
        finish = resolve
      }),
    )
    renderChip(fileCheck({ status: 'failed', groups: [], error: 'GitHub answered 502' }))

    const panel = await openPanel(/^Couldn’t check the changed files/)
    expect(panel.getByText(/Couldn’t check this pull request’s files: GitHub answered 502\./)).toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Check now' }))

    expect(platform.checkItemFiles).toHaveBeenCalledWith(7)
    expect(panel.getByRole('button', { name: 'Checking…' })).toBeDisabled()

    finish(fileCheck())
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
        <SensitiveFiles itemId={7} check={fileCheck()} now={now} />
      </div>,
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
