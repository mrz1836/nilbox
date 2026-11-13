import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { RepoRules, Settings } from '../../platform'
import { agents, defaultPrompts, profile, prompts, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { RepositoriesSection } from './RepositoriesSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(repoRules: RepoRules = { pinned: [], muted: [] }) {
  const onSaved = vi.fn<(s: Settings) => void>()

  render(
    <RepositoriesSection
      settings={settings({ repoRules })}
      onSaved={onSaved}
      agents={agents()}
      profiles={[profile()]}
      prompts={prompts()}
      defaultPrompts={defaultPrompts()}
      launchDefaults={[]}
      onLaunchChanged={() => Promise.resolve()}
    />,
  )

  return { onSaved }
}

function rules() {
  return within(screen.getByRole('list', { name: 'Pinned and muted repositories' }))
}

function field() {
  return screen.getByRole('combobox', { name: 'Org or repository to pin or mute' })
}

describe('RepoRules', () => {
  it('says how to pin or mute when nothing is', () => {
    renderSection()

    expect(screen.getByRole('heading', { name: 'Pinned and muted' })).toBeInTheDocument()
    expect(
      screen.getByText(/None yet\. Each card's menu pins or mutes its repository or org too\./),
    ).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Pinned and muted repositories' })).not.toBeInTheDocument()
  })

  it('lists each org first, then its repositories, with its rule', () => {
    renderSection({ pinned: ['acme-org/web', 'solo/app'], muted: ['acme-org'] })

    const items = rules().getAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([
      'acme-orgAll repositoriesPinnedMuted',
      'acme-org/webRepositoryPinnedMuted',
      'solo/appRepositoryPinnedMuted',
    ])
    expect(items[1]).toHaveClass('pl-8')
    expect(items[2]).toHaveClass('pl-4')

    expect(within(items[0] ?? document.body).getByRole('radio', { name: 'Muted' })).toBeChecked()
    expect(within(items[1] ?? document.body).getByRole('radio', { name: 'Pinned' })).toBeChecked()
  })

  it('switches a target’s rule, and takes it off', async () => {
    const stored = settings({ repoRules: { pinned: ['acme-org'], muted: [] } })
    platform.setRepoRule.mockResolvedValue(stored)
    const { onSaved } = renderSection({ pinned: [], muted: ['acme-org'] })

    await userEvent.click(within(rules().getByRole('radiogroup', { name: 'Rule for acme-org' })).getByText('Pinned'))

    expect(platform.setRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', 'pinned')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(stored)
    })

    await userEvent.click(rules().getByRole('button', { name: 'Remove the rule for acme-org' }))
    expect(platform.setRepoRule).toHaveBeenLastCalledWith('acme-org', '')
  })

  it('pins or mutes what is typed, offering the inbox’s orgs and repositories', async () => {
    platform.listLaunchTargets.mockResolvedValue([
      { owner: 'acme-org', name: '', items: 3 },
      { owner: 'acme-org', name: 'web', items: 2 },
    ])
    platform.setRepoRule.mockResolvedValue(settings({ repoRules: { pinned: [], muted: ['acme-org/web'] } }))
    renderSection()

    await waitFor(() => {
      expect(document.querySelectorAll('datalist option')).toHaveLength(2)
    })
    expect(document.querySelector('datalist option[value="acme-org/web"]')).toHaveTextContent('2 in the inbox')

    const add = screen.getByRole('button', { name: 'Mute' })
    expect(add).toBeDisabled()

    await userEvent.type(field(), ' https://github.com/acme-org/web/ ')
    await userEvent.click(screen.getByRole('button', { name: 'Mute' }))

    expect(platform.setRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org/web', 'muted')
    await waitFor(() => {
      expect(field()).toHaveValue('')
    })

    await userEvent.type(field(), 'solo{Enter}')
    expect(platform.setRepoRule).toHaveBeenLastCalledWith('solo', 'pinned')
  })

  it('refuses what can’t be a GitHub org or repository, and says why a change failed', async () => {
    platform.setRepoRule.mockRejectedValue(new Error('too many repositories and orgs: keep each list to 500'))
    renderSection()

    await userEvent.type(field(), 'not a repo')
    await userEvent.click(screen.getByRole('button', { name: 'Pin' }))

    expect(platform.setRepoRule).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Type an org, such as acme-org, or a repository, such as acme-org/example-repo.',
    )
    expect(field()).toHaveAttribute('aria-invalid', 'true')

    await userEvent.clear(field())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await userEvent.type(field(), 'acme-org')
    await userEvent.click(screen.getByRole('button', { name: 'Pin' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('too many repositories and orgs: keep each list to 500')
    expect(field()).toHaveValue('acme-org')
  })
})
