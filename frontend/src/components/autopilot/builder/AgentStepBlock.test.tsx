import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotAgentStep, AutopilotProblem, ItemKind, Prompt } from '../../../platform'
import { agents, autopilotCatalog, profile, prompt, prompts, routineSuggestions } from '../../../test/fixtures'
import { AgentStepBlock } from './AgentStepBlock'
import type { BuilderKit } from './parts'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// library is the prompt library: the small one's agent prompts, an approval's reply, which
// no agent step runs, and the two starters for repositories, which no routine runs.
function library(): Prompt[] {
  const usage = { defaults: [], slots: [], orgs: [], repos: [], slotPlaces: [], kinds: [], replies: [], routines: [] }

  return [
    ...prompts(),
    prompt({ id: 20, nickname: 'LGTM', purpose: 'reply', replyAction: 'approve', starterKey: 'reply-lgtm' }),
    prompt({
      id: 31,
      name: 'Upgrade dependencies',
      nickname: 'Upgrade deps',
      appliesTo: 'repo',
      starterKey: 'deps-minor',
      usage,
    }),
    prompt({
      id: 32,
      name: 'Upgrade dependencies, majors too',
      nickname: 'Upgrade majors too',
      appliesTo: 'repo',
      starterKey: 'deps-major',
      usage,
    }),
  ]
}

const changed = vi.fn<(step: AutopilotAgentStep) => void>()

interface HarnessProps {
  kinds?: ItemKind[]
  start?: AutopilotAgentStep
  problems?: AutopilotProblem[]
}

// Harness holds the agent step as the builder does, for a routine that acts on kinds.
function Harness({
  kinds = ['pr'],
  start = { agent: 'inherit', prompt: { itemDefault: true } },
  problems = [],
}: HarnessProps) {
  const [step, setStep] = useState(start)
  const kit: BuilderKit = {
    catalog: autopilotCatalog(),
    suggestions: routineSuggestions(),
    viewer: 'octo-user',
    problems,
    warnings: [],
  }

  return (
    <AgentStepBlock
      kit={kit}
      agentKit={{
        prompts: library(),
        agents: agents(),
        profiles: [profile()],
        defaultAgent: 'claude',
        defaultProfiles: { claude: 'default' },
        onEditPrompts: vi.fn(),
      }}
      path="[0]"
      kinds={kinds}
      step={step}
      onChange={(next) => {
        changed(next)
        setStep(next)
      }}
    />
  )
}

// offered lists what the prompt menu offers, by the names it shows.
function offered(): (string | null)[] {
  return within(screen.getByRole('combobox', { name: 'Prompt' }))
    .getAllByRole('option')
    .map((o) => o.textContent)
}

describe('AgentStepBlock', () => {
  it('offers the agent prompts that fit every kind the routine acts on, never one for repositories', () => {
    const { rerender } = render(<Harness kinds={['pr']} />)
    expect(offered()).toEqual(['The item’s own default prompt', 'Fix checks', 'Quick look', 'Review', 'Explain'])

    rerender(<Harness kinds={['issue']} />)
    expect(offered()).toEqual(['The item’s own default prompt', 'Work on it', 'Explain'])

    rerender(<Harness kinds={['pr', 'issue']} />)
    expect(offered()).toEqual(['The item’s own default prompt', 'Explain'])

    // A routine that acts on no kind yet still runs its prompts on items alone.
    rerender(<Harness kinds={[]} />)
    expect(offered()).toEqual([
      'The item’s own default prompt',
      'Fix checks',
      'Quick look',
      'Review',
      'Work on it',
      'Explain',
    ])
  })

  it('keeps a prompt for repositories a step names as itself, its problem where it is, until another is picked', async () => {
    const user = userEvent.setup()
    render(
      <Harness
        kinds={[]}
        start={{ agent: 'inherit', prompt: { starter: 'deps-minor' } }}
        problems={[
          {
            part: 'steps',
            field: '[0].agent.prompt',
            message:
              '"Upgrade deps" isn\'t written for pull requests or issues, and a routine runs its prompts on those alone: pick another prompt',
          },
        ]}
      />,
    )

    const menu = screen.getByRole('combobox', { name: 'Prompt' })
    expect(menu).toHaveValue('starter:deps-minor')
    expect(menu).toHaveDisplayValue('Upgrade deps')
    expect(menu).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText(/isn't written for pull requests or issues/)).toBeInTheDocument()
    expect(offered()).not.toContain('Upgrade majors too')

    await user.selectOptions(menu, 'starter:review-pr')

    expect(changed).toHaveBeenLastCalledWith(expect.objectContaining({ prompt: { starter: 'review-pr' } }))
    expect(offered()).not.toContain('Upgrade deps')
  })
})
