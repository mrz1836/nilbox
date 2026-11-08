import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { SensitiveFilesSection } from './SensitiveFilesSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// withFiles returns the settings with the sensitive-file choices changed.
function withFiles(change: Partial<Settings['sensitiveFiles']>): Settings {
  const s = settings()

  return { ...s, sensitiveFiles: { ...s.sensitiveFiles, ...change } }
}

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<SensitiveFilesSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

function category(name: string) {
  return screen.getByRole('switch', { name })
}

beforeEach(() => {
  // Each change comes back with the settings as stored.
  platform.setSensitiveScope.mockResolvedValue(settings())
  platform.setSensitiveCategory.mockResolvedValue(settings())
  platform.setSensitivePatterns.mockResolvedValue(settings())
})

describe('SensitiveFilesSection', () => {
  it('chooses whose pull requests are checked, checking collaborators’ at once', async () => {
    const { onSaved } = renderSection(withFiles({ scope: 'outside' }))

    const scope = within(screen.getByRole('radiogroup', { name: 'Check pull requests from' }))
    expect(scope.getByRole('radio', { name: 'Outside contributors' })).toBeChecked()
    expect(screen.getByRole('radiogroup', { name: 'Check pull requests from' })).not.toHaveAttribute('aria-describedby')

    await userEvent.click(scope.getByRole('radio', { name: 'Also collaborators' }))

    expect(platform.setSensitiveScope).toHaveBeenCalledWith('collaborators')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings())
    })
    expect(platform.refreshInbox).toHaveBeenCalledOnce()
  })

  it('keeps the new scope when the refresh after it fails', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.refreshInbox.mockRejectedValue(new Error('offline'))
    const { onSaved } = renderSection(withFiles({ scope: 'outside' }))

    await userEvent.click(screen.getByRole('radio', { name: 'Also collaborators' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not refresh the inbox', expect.any(Error))
    })
    expect(onSaved).toHaveBeenCalledOnce()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    logged.mockRestore()
  })

  it('does not refresh when narrowing the scope', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(screen.getByRole('radio', { name: 'Outside contributors' }))

    expect(platform.setSensitiveScope).toHaveBeenCalledWith('outside')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.refreshInbox).not.toHaveBeenCalled()
  })

  it('lists the categories, the agent one always on', async () => {
    renderSection(withFiles({ off: ['dependencies'] }))

    expect(category('Agent configuration')).toBeChecked()
    expect(category('Agent configuration')).toBeDisabled()
    expect(screen.getByText('Always on')).toBeInTheDocument()
    expect(screen.getByText(/A session on a pull request that changes these asks you first/)).toBeInTheDocument()
    expect(category('Workflows and CI')).toBeChecked()
    expect(category('Dependencies')).not.toBeChecked()

    // Each category's patterns are a click away.
    await userEvent.click(screen.getByText('3 patterns'))
    expect(
      within(screen.getByRole('list', { name: 'Agent configuration patterns' })).getByText('.claude/'),
    ).toBeVisible()
  })

  it('turns a category on and off', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(category('Dependencies'))

    expect(platform.setSensitiveCategory).toHaveBeenCalledWith('dependencies', false)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
  })

  it('turns a category back on', async () => {
    renderSection(withFiles({ off: ['dependencies'] }))

    await userEvent.click(category('Dependencies'))

    expect(platform.setSensitiveCategory).toHaveBeenCalledWith('dependencies', true)
  })

  it('shows why a change was refused', async () => {
    platform.setSensitiveCategory.mockRejectedValue(new Error('this category is always on'))
    renderSection()

    await userEvent.click(category('Workflows and CI'))

    expect(await screen.findByRole('alert')).toHaveTextContent('this category is always on')
    // The settings above the patterns are read with why the last change was refused.
    expect(category('Workflows and CI')).toHaveAccessibleDescription(/ this category is always on$/)
    expect(screen.getByRole('radiogroup', { name: 'Check pull requests from' })).toHaveAccessibleDescription(
      'this category is always on',
    )
  })

  it('adds a pattern of the user’s own', async () => {
    const { onSaved } = renderSection(withFiles({ patterns: ['infra/'] }))

    expect(within(screen.getByRole('list', { name: 'Your patterns' })).getByText('infra/')).toBeInTheDocument()

    const input = screen.getByRole('textbox', { name: 'Pattern to add' })
    expect(input).toHaveAccessibleDescription(/^Patterns work like \.gitignore, ignoring case\./)
    await userEvent.type(input, ' *.tf ')
    await userEvent.click(screen.getByRole('button', { name: 'Add pattern' }))

    expect(platform.setSensitivePatterns).toHaveBeenCalledWith(['infra/', '*.tf'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(input).toHaveValue('')
  })

  it('refuses a repeated pattern, and shows the reason for an invalid one', async () => {
    renderSection(withFiles({ patterns: ['infra/'] }))

    const input = screen.getByRole('textbox', { name: 'Pattern to add' })
    await userEvent.type(input, 'INFRA/')
    await userEvent.click(screen.getByRole('button', { name: 'Add pattern' }))

    expect(screen.getByRole('alert')).toHaveTextContent('INFRA/ is already on the list.')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription(/^INFRA\/ is already on the list\. Patterns work like/)
    expect(platform.setSensitivePatterns).not.toHaveBeenCalled()

    platform.setSensitivePatterns.mockRejectedValue(new Error('invalid pattern "*": it matches every file'))
    await userEvent.clear(input)
    await userEvent.type(input, '*')
    await userEvent.click(screen.getByRole('button', { name: 'Add pattern' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('it matches every file')
    expect(input).toHaveValue('*')
  })

  it('removes a pattern', async () => {
    renderSection(withFiles({ patterns: ['infra/', '*.tf'] }))

    await userEvent.click(screen.getByRole('button', { name: 'Remove infra/' }))

    expect(platform.setSensitivePatterns).toHaveBeenCalledWith(['*.tf'])
  })

  it('says when the user has no patterns', () => {
    renderSection()

    expect(screen.getByText('No patterns of your own yet.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add pattern' })).toBeDisabled()
  })

  it('tries a path as it is typed', async () => {
    platform.matchSensitivePath.mockResolvedValue({
      sensitive: true,
      category: 'workflows',
      label: 'Workflows and CI',
      level: 'danger',
      pattern: '.github/workflows/',
    })
    renderSection()

    await userEvent.type(screen.getByRole('textbox', { name: 'Path to try' }), '.github/workflows/ci.yml')

    const status = screen.getByRole('status')
    await waitFor(() => {
      expect(status).toHaveTextContent('Sensitive: Workflows and CI, matched by .github/workflows/.')
    })
    expect(platform.matchSensitivePath).toHaveBeenLastCalledWith('.github/workflows/ci.yml')

    platform.matchSensitivePath.mockResolvedValue({ sensitive: false, category: '', label: '', level: '', pattern: '' })
    await userEvent.clear(screen.getByRole('textbox', { name: 'Path to try' }))
    expect(status).toBeEmptyDOMElement()

    await userEvent.type(screen.getByRole('textbox', { name: 'Path to try' }), 'README.md')
    await waitFor(() => {
      expect(status).toHaveTextContent('Not sensitive with these settings.')
    })
  })

  it('shows why a path could not be tried', async () => {
    platform.matchSensitivePath.mockRejectedValue(new Error('database is locked'))
    renderSection()

    await userEvent.type(screen.getByRole('textbox', { name: 'Path to try' }), 'go.mod')

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('database is locked')
    })
  })

  it('explains how the check works', () => {
    renderSection()

    expect(screen.getByRole('heading', { name: 'How it works' })).toBeInTheDocument()
    expect(screen.getByText(/never downloads their contents/)).toBeInTheDocument()
    expect(screen.getByText(/even in plan mode/)).toBeInTheDocument()
  })
})
