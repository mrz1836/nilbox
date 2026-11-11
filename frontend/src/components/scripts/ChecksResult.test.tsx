import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { hookChecksResult } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ChecksResult } from './ChecksResult'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// shown renders run 21's result and waits for it.
async function shown() {
  render(<ChecksResult runId={21} />)

  return within(await screen.findByRole('region'))
}

describe('ChecksResult', () => {
  it('says which checks passed, which failed and why, and which files they changed', async () => {
    const card = await shown()

    expect(platform.getChecksResult).toHaveBeenCalledExactlyOnceWith(21)
    expect(card.getByRole('heading', { name: 'Checks in acme-org/web · all files' })).toBeInTheDocument()
    expect(card.getByText('1 min 12 s')).toBeInTheDocument()
    expect(card.getByText('4 passed, 1 failed: whitespace.')).toBeInTheDocument()

    const checks = within(card.getByRole('list', { name: 'Checks' })).getAllByRole('listitem')

    expect(checks.map((c) => c.textContent)).toEqual([
      'Done: eof',
      'Done: fumpt',
      'Done: lint',
      'Done: mod-tidy',
      'Problem: whitespace',
      'Skipped: gitleaks (off)',
    ])

    expect(card.getByText('whitespace:').parentElement).toHaveTextContent('whitespace: whitespace issues found')
    expect(card.getByText('To fix: Run go-pre-commit run whitespace and commit the fixes.')).toBeInTheDocument()
    expect(card.getByLabelText('whitespace’s output')).toHaveTextContent('README.md:3: trailing whitespace')

    expect(card.getByText('Changed 2 files:')).toBeInTheDocument()
    expect(
      within(card.getByRole('list', { name: 'Changed files' }))
        .getAllByRole('listitem')
        .map((f) => f.textContent),
    ).toEqual([
      'README.md (fixed and staged by whitespace)',
      'docs/setup.md (fixed and staged by whitespace; already had uncommitted changes)',
    ])
  })

  it('names one check’s run, and a check that only showed its output or was skipped', async () => {
    const check = { durationMs: 0, error: '', suggestion: '', detail: [], changedFiles: [] }
    platform.getChecksResult.mockResolvedValue(
      hookChecksResult({
        scope: 'one',
        check: 'lint',
        message: '1 failed: lint.',
        checks: [
          { ...check, name: 'lint', state: 'failed', detail: ['\u001b[31mmain.go:1: unused\u001b[0m'] },
          { ...check, name: 'gitleaks', state: 'skipped', suggestion: 'Install gitleaks.' },
          { ...check, name: 'eof', state: 'notRun' },
        ],
        durationMs: 350,
        changed: [{ path: 'main.go', staged: false, wasDirty: false, by: [] }],
      }),
    )
    const card = await shown()

    expect(card.getByRole('heading', { name: 'lint in acme-org/web · all files' })).toBeInTheDocument()
    expect(card.getByText('350 ms')).toBeInTheDocument()
    expect(card.getByLabelText('lint’s output')).toHaveTextContent(/^main\.go:1: unused$/)
    expect(card.queryByText('lint:')).not.toBeInTheDocument()
    expect(card.getByText('To fix: Install gitleaks.')).toBeInTheDocument()
    expect(card.getByText('(skipped)')).toBeInTheDocument()
    expect(card.getByText('(not run)')).toBeInTheDocument()
    expect(within(card.getByRole('list', { name: 'Changed files' })).getByRole('listitem')).toHaveTextContent(
      /^main\.go$/,
    )
  })

  it('says nothing of the files when none changed, nor the time when go-pre-commit gave none', async () => {
    platform.getChecksResult.mockResolvedValue(
      hookChecksResult({
        scope: 'staged',
        message: 'All 5 checks passed on 3 files.',
        failed: 0,
        passed: 5,
        durationMs: 0,
        checks: hookChecksResult().checks.map((c) => ({ ...c, state: c.state === 'off' ? 'off' : 'passed' })),
        changed: [],
      }),
    )
    const card = await shown()

    expect(card.getByRole('heading', { name: 'Checks in acme-org/web · staged files' })).toBeInTheDocument()
    expect(card.getByText('All 5 checks passed on 3 files.')).toHaveTextContent(/^Done: /)
    expect(card.queryByText(/Changed/)).not.toBeInTheDocument()
    expect(card.queryByText(/ s$/)).not.toBeInTheDocument()
  })

  it('says when it couldn’t tell which files changed, and why', async () => {
    platform.getChecksResult.mockResolvedValue(
      hookChecksResult({ changedKnown: false, changed: [], changedError: 'Couldn’t read git’s status.' }),
    )
    const card = await shown()

    expect(
      card.getByText('Couldn’t tell which files the checks changed: Couldn’t read git’s status.'),
    ).toBeInTheDocument()
  })

  it('says when it couldn’t tell which files changed, with no reason', async () => {
    platform.getChecksResult.mockResolvedValue(hookChecksResult({ changedKnown: false, changed: [] }))
    const card = await shown()

    expect(card.getByText('Couldn’t tell which files the checks changed.')).toBeInTheDocument()
  })

  it('lists the tools go-pre-commit installed, and its own errors', async () => {
    platform.getChecksResult.mockResolvedValue(
      hookChecksResult({
        installs: ['gofumpt@v0.12.0', 'golangci-lint@v2.6.0'],
        errors: ['no go.mod found', 'lint: timed out'],
      }),
    )
    const card = await shown()

    expect(
      card.getByText('go-pre-commit installed gofumpt@v0.12.0, golangci-lint@v2.6.0 for these checks.'),
    ).toBeInTheDocument()
    expect(
      within(card.getByRole('list', { name: 'go-pre-commit’s errors' }))
        .getAllByRole('listitem')
        .map((e) => e.textContent),
    ).toEqual(['no go.mod found', 'lint: timed out'])
  })

  it('shows nothing for a run that ended without a result', async () => {
    platform.getChecksResult.mockRejectedValue(new Error('This run ended without a result to show.'))
    const { container } = render(<ChecksResult runId={21} />)

    await vi.waitFor(() => {
      expect(platform.getChecksResult).toHaveBeenCalled()
    })
    await Promise.resolve()
    expect(container).toBeEmptyDOMElement()
  })
})
