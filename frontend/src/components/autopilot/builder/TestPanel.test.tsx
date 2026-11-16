import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotDraft } from '../../../platform'
import { routineTest } from '../../../test/fixtures'
import { platform } from '../../../test/platform'
import { TestPanel } from './TestPanel'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// rebase is a draft that asks Dependabot to rebase, then labels what it asked.
const rebase: AutopilotDraft = {
  id: 0,
  name: 'Rebase',
  spec: {
    version: 1,
    watch: { sources: [{ search: { kind: 'pr', authors: ['app/dependabot'], where: ['@me'] } }] },
    trigger: { event: 'merge_conflicts' },
    conditions: { match: 'all', of: [] },
    steps: [{ github: [{ rebase: {} }, { labels: { add: ['deps'] } }] }],
    limits: {},
  },
}

describe('TestPanel’s Preview', () => {
  it('shows a match exactly what the draft’s GitHub actions would send it, built and not sent', async () => {
    const user = userEvent.setup()
    platform.previewAutopilotActions.mockResolvedValue([
      {
        step: 0,
        kind: 'github',
        action: 'rebase',
        label: 'Ask Dependabot to rebase',
        sends: '@dependabot rebase',
        problem: '',
      },
      { step: 0, kind: 'github', action: 'labels', label: 'Add or remove labels', sends: 'Adds “deps”.', problem: '' },
    ])

    render(
      <TestPanel
        result={routineTest()}
        stale={false}
        testing={false}
        error=""
        ready
        kinds={['pr']}
        mode="ask"
        draft={rebase}
        onTest={vi.fn()}
        preview={rebase}
      />,
    )

    expect(screen.getAllByRole('button', { name: /^Preview, for / })).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Preview, for acme-org/example-repo #7' }))

    const sends = await screen.findByRole('list', { name: 'What it would send acme-org/example-repo #7' })

    expect(within(sends).getByText('@dependabot rebase')).toBeInTheDocument()
    expect(within(sends).getByText('Adds “deps”.')).toBeInTheDocument()
    expect(platform.previewAutopilotActions).toHaveBeenCalledExactlyOnceWith(rebase, 1)
  })

  it('offers no Preview for a draft that sends GitHub nothing', () => {
    render(
      <TestPanel
        result={routineTest()}
        stale={false}
        testing={false}
        error=""
        ready
        kinds={['pr']}
        mode="ask"
        draft={rebase}
        onTest={vi.fn()}
      />,
    )

    expect(screen.queryByRole('button', { name: /^Preview, for / })).not.toBeInTheDocument()
  })
})
