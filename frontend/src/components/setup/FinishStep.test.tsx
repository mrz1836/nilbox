import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CelebrationsContext } from '../../lib/celebrations'
import { earnedBurst } from '../../lib/nil'
import type { Celebrations } from '../../platform'
import {
  agents,
  agentVersions,
  githubSignIn,
  galleryTemplate,
  goPreCommitTool,
  integrationTool,
  integrationTools,
  list,
  locationState,
  settings,
  snapshot,
  snoozeTemplate,
  terminalChoices,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { FinishStep, type FinishStepProps } from './FinishStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

afterEach(() => {
  vi.unstubAllGlobals()
})

// showFinish shows Finish over a setup that went well, as change adjusts its props.
function showFinish(change: Partial<FinishStepProps> = {}, celebrations: Celebrations = 'milestones') {
  const props: FinishStepProps = {
    headingId: 'setup-heading',
    headingRef: createRef(),
    settings: settings({ cloneRoot: '~/projects' }),
    github: githubSignIn({ scopes: ['repo', 'read:org'] }),
    root: locationState({ clones: 42 }),
    agents: agents(),
    versions: agentVersions(),
    terminals: terminalChoices(),
    banners: 'on',
    tools: null,
    inbox: {
      snapshot: snapshot({
        lastRefreshedAt: '2026-06-15T12:00:00Z',
        reviewRequested: list([], 5),
        yours: list([], 7),
      }),
      refreshing: false,
      refresh: vi.fn(),
    },
    elapsed: 102_000,
    onFix: vi.fn(),
    onDone: vi.fn(),
    ...change,
  }

  render(
    <CelebrationsContext value={celebrations}>
      <FinishStep {...props} />
    </CelebrationsContext>,
  )

  return props
}

// log is the setup's log.
function log() {
  return within(screen.getByRole('figure', { name: 'Setup log' }))
}

describe('FinishStep', () => {
  it('prints the setup as go test would, previews the inbox, and opens it', async () => {
    const props = showFinish()

    expect(screen.getByText('Achievement unlocked')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'You’re all set' })).toBeInTheDocument()
    expect(log().getByText('@octo-user · repo, read:org')).toBeInTheDocument()
    expect(log().getByText('42 in ~/projects')).toBeInTheDocument()
    expect(log().getByText('Claude 2.1.319 (default) · Codex 0.163.0')).toBeInTheDocument()
    expect(log().getByText('iTerm2 · worktrees on')).toBeInTheDocument()
    expect(log().getByText('banners on · menu bar on')).toBeInTheDocument()
    expect(log().queryByText(/tools/)).not.toBeInTheDocument()
    expect(log().getByText(/\/setup\s+1m42s/)).toBeInTheDocument()
    expect(log().queryByRole('button', { name: /Fix/ })).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('5 review requests · 7 of yours')
    expect(screen.getByText(/Open in Claude starts a session/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Open my inbox' }))
    expect(props.onDone).toHaveBeenCalledOnce()
  })

  it('shows how to read a card, the compact guide, before opening the inbox', async () => {
    const props = showFinish()

    const guide = screen.getByRole('region', { name: 'How to read a card' })

    expect(within(guide).getByRole('heading', { name: 'How to read a card' })).toBeInTheDocument()
    expect(guide.querySelector('[inert] li[data-item-id]')).not.toBeNull()
    expect(guide.querySelector('[data-marker]')).toBeNull()
    expect(within(guide).queryByRole('list', { name: 'The parts of a card' })).not.toBeInTheDocument()
    expect(within(guide).getByRole('list', { name: 'Whose move' })).toBeInTheDocument()
    expect(guide.compareDocumentPosition(screen.getByRole('button', { name: 'Open my inbox' }))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Open my inbox' }))
    expect(props.onDone).toHaveBeenCalledOnce()
  })

  it('names the tools the Tools step left, or that it was skipped', () => {
    showFinish({
      tools: integrationTools({ tools: [integrationTool(), goPreCommitTool({ state: 'missing' })] }),
    })

    expect(log().getByText('mage-x')).toBeInTheDocument()
  })

  it('offers a fix beside what needs attention', async () => {
    const props = showFinish({
      github: githubSignIn({ state: 'signed_out' }),
      root: locationState({ state: 'missing' }),
    })

    expect(log().getByText('not connected yet')).toBeInTheDocument()
    expect(log().getByText(/\(2 to fix\)/)).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Connect GitHub to fill it.')

    await userEvent.click(log().getByRole('button', { name: 'Fix github' }))
    expect(props.onFix).toHaveBeenCalledWith('github')

    await userEvent.click(log().getByRole('button', { name: 'Fix clones' }))
    expect(props.onFix).toHaveBeenCalledWith('clones')
  })

  it('says the inbox is filling while its first refresh runs', () => {
    showFinish({ inbox: { snapshot: null, refreshing: true, refresh: vi.fn() } })

    expect(screen.getByRole('status')).toHaveTextContent('Filling your inbox…')
  })

  it('reads well before the settings load', () => {
    showFinish({ settings: null, terminals: null, banners: '' })

    expect(log().getByText('banners not checked · menu bar off')).toBeInTheDocument()
    expect(screen.getByText(/Open in your agent starts a session/)).toBeInTheDocument()
  })

  it('throws confetti from the heading as it shows', () => {
    const headingRef = createRef<HTMLHeadingElement>()
    showFinish({ headingRef })

    expect(screen.getAllByTestId('confetti')).toHaveLength(earnedBurst)
  })

  it('throws nothing with reduced motion', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
    showFinish()

    expect(screen.queryAllByTestId('confetti')).toHaveLength(0)
  })

  it.each(['quiet', 'off'] as const)('throws nothing with celebrations %s, the achievement all the same', (mode) => {
    showFinish({}, mode)

    expect(screen.queryAllByTestId('confetti')).toHaveLength(0)
    expect(screen.getByText('Achievement unlocked')).toBeInTheDocument()
  })

  it('suggests a template from what this Mac knows, while Autopilot is on, and sets it up on asking', async () => {
    platform.listTemplates.mockResolvedValue([
      snoozeTemplate(),
      galleryTemplate({ suggested: '3 of your clones run go-fortress’s security checks.' }),
    ])
    const onTemplate = vi.fn()
    showFinish({ autopilot: true, onTemplate })

    const suggested = within(await screen.findByRole('region', { name: 'Suggested for you' }))

    expect(suggested.getByText('Suggested for you: Fix Dependabot security failures')).toBeInTheDocument()
    expect(suggested.getByText('3 of your clones run go-fortress’s security checks.')).toBeInTheDocument()

    await userEvent.click(suggested.getByRole('button', { name: 'Set it up' }))
    expect(onTemplate).toHaveBeenCalledExactlyOnceWith('fix-dependabot-security')
  })

  it('suggests nothing while Autopilot is off, or when nothing this Mac knows fits a template', async () => {
    showFinish({ autopilot: false, onTemplate: vi.fn() })
    expect(platform.listTemplates).not.toHaveBeenCalled()

    showFinish({ autopilot: true, onTemplate: vi.fn() })
    await vi.waitFor(() => {
      expect(platform.listTemplates).toHaveBeenCalledOnce()
    })
    expect(screen.queryByRole('region', { name: 'Suggested for you' })).not.toBeInTheDocument()
  })
})
