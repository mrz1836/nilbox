import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotGitHubAction, AutopilotProblem, ItemKind, Prompt } from '../../../platform'
import { autopilotCatalog, prompt, prompts, routineSuggestions } from '../../../test/fixtures'
import { GitHubStepBlock, isCommentReply } from './GitHubStepBlock'
import { fitsKinds, type BuilderKit } from './parts'

vi.mock('../../../platform', () => import('../../../test/platform').then((fake) => fake.platform))

// library is the prompt library with its replies: an approval's, a request for changes',
// and two comments', one for pull requests alone.
function library() {
  return [
    ...prompts(),
    prompt({ id: 20, nickname: 'LGTM', purpose: 'reply', replyAction: 'approve', starterKey: 'reply-lgtm' }),
    prompt({ id: 21, nickname: 'Needs changes', purpose: 'reply', replyAction: 'request_changes', starterKey: '' }),
    prompt({
      id: 22,
      nickname: 'Question',
      purpose: 'reply',
      replyAction: 'comment',
      starterKey: 'reply-question',
      appliesTo: 'pr',
    }),
    prompt({ id: 23, nickname: 'Thanks', purpose: 'reply', replyAction: 'comment', starterKey: '', appliesTo: 'any' }),
  ]
}

const changed = vi.fn<(actions: AutopilotGitHubAction[]) => void>()

interface HarnessProps {
  start?: AutopilotGitHubAction[]
  kinds?: ItemKind[]
  problems?: AutopilotProblem[]
  prompts?: Prompt[]
}

// Harness holds the step's actions as the builder does, over library unless prompts says.
function Harness({ start = [], kinds = ['pr'], problems = [], prompts: held = library() }: HarnessProps) {
  const [actions, setActions] = useState(start)
  const kit: BuilderKit = {
    catalog: autopilotCatalog(),
    suggestions: routineSuggestions(),
    viewer: 'octo-user',
    problems,
    warnings: [],
  }

  return (
    <GitHubStepBlock
      kit={kit}
      prompts={held}
      path="[0]"
      kinds={kinds}
      actions={actions}
      onChange={(next) => {
        changed(next)
        setActions(next)
      }}
    />
  )
}

// last is the actions as the block last changed them.
function last(): AutopilotGitHubAction[] {
  return changed.mock.lastCall?.[0] ?? []
}

describe('GitHubStepBlock', () => {
  it('adds each action that fits once, in the order they are sent', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByRole('button', { name: 'Add an action' }))
    await user.click(screen.getByRole('menuitem', { name: 'Ask Dependabot to rebase' }))
    expect(last()).toEqual([{ rebase: {} }])

    await user.click(screen.getByRole('button', { name: 'Add an action' }))
    expect(screen.queryByRole('menuitem', { name: 'Ask Dependabot to rebase' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: 'Add or remove labels' }))
    expect(last()).toEqual([{ rebase: {} }, { labels: { add: [] } }])

    await user.click(screen.getByRole('button', { name: 'Move action 2 up' }))
    expect(last()).toEqual([{ labels: { add: [] } }, { rebase: {} }])

    await user.click(screen.getByRole('button', { name: 'Remove action 1' }))
    expect(last()).toEqual([{ rebase: {} }])
  })

  it('offers no branch action on a routine that acts on issues', async () => {
    const user = userEvent.setup()
    render(<Harness kinds={['issue']} />)

    await user.click(screen.getByRole('button', { name: 'Add an action' }))

    expect(screen.queryByRole('menuitem', { name: 'Ask Dependabot to rebase' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Update branch' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Comment one of my replies' })).toBeInTheDocument()
  })

  it('comments a comment’s reply alone, never an approval’s or a request for changes’', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByRole('button', { name: 'Add an action' }))
    await user.click(screen.getByRole('menuitem', { name: 'Comment one of my replies' }))
    expect(last()).toEqual([{ reply: { prompt: { starter: 'reply-question' } } }])

    const picker = screen.getByRole('combobox', { name: 'Action 1: the reply it comments' })
    const offered = within(picker)
      .getAllByRole('option')
      .map((o) => o.textContent)

    expect(offered).toEqual(['Question', 'Thanks'])

    await user.selectOptions(picker, 'nickname:Thanks')
    expect(last()).toEqual([{ reply: { prompt: { nickname: 'Thanks' } } }])

    expect(isCommentReply({ purpose: 'reply', replyAction: 'approve' })).toBe(false)
    expect(isCommentReply({ purpose: 'reply', replyAction: 'comment' })).toBe(true)
  })

  it('offers every comment reply to a routine that acts on no kind yet, but never one for repositories', async () => {
    const user = userEvent.setup()
    render(
      <Harness
        kinds={[]}
        prompts={[
          ...library(),
          // No reply is for repositories, but a library is checked whatever it holds.
          prompt({
            id: 24,
            nickname: 'Deps',
            purpose: 'reply',
            replyAction: 'comment',
            starterKey: '',
            appliesTo: 'repo',
          }),
        ]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Add an action' }))
    await user.click(screen.getByRole('menuitem', { name: 'Comment one of my replies' }))

    expect(
      within(screen.getByRole('combobox', { name: 'Action 1: the reply it comments' }))
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Question', 'Thanks'])

    expect(fitsKinds({ appliesTo: 'repo' }, [])).toBe(false)
    expect(fitsKinds({ appliesTo: 'repo' }, ['pr'])).toBe(false)
    expect(fitsKinds({ appliesTo: 'issue' }, [])).toBe(true)
    expect(fitsKinds({ appliesTo: 'any' }, ['pr', 'issue'])).toBe(true)
    expect(fitsKinds({ appliesTo: 'pr' }, ['pr', 'issue'])).toBe(false)
  })

  it('keeps a reply that no longer fits as itself, its problem where it is', () => {
    render(
      <Harness
        start={[{ reply: { prompt: { starter: 'reply-lgtm' } } }]}
        problems={[
          {
            part: 'steps',
            field: '[0].github[0].reply.prompt',
            message: '"LGTM" is a reply that approves: a GitHub step only comments',
          },
        ]}
      />,
    )

    const picker = screen.getByRole('combobox', { name: 'Action 1: the reply it comments' })

    expect(picker).toHaveValue('starter:reply-lgtm')
    expect(picker).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText(/is a reply that approves: a GitHub step only comments/)).toBeInTheDocument()
  })

  it('edits the labels it adds and takes off', async () => {
    const user = userEvent.setup()
    render(<Harness start={[{ labels: { add: [] } }]} />)

    await user.type(screen.getByRole('combobox', { name: 'Action 1: labels to add' }), 'needs-repro{Enter}')
    expect(last()).toEqual([{ labels: { add: ['needs-repro'] } }])

    await user.type(screen.getByRole('combobox', { name: 'Action 1: labels to take off' }), 'triage{Enter}')
    expect(last()).toEqual([{ labels: { add: ['needs-repro'], remove: ['triage'] } }])
  })
})
