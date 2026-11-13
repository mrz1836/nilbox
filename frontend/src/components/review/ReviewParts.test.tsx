import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { emptyDraft } from '../../lib/reviewDrafts'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  closeOffer,
  fileCheck,
  item,
  itemResolution,
  mergeOffer,
  offer,
  profile,
  prompts,
  review,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ActionButton, menuOnly } from './ActionButton'
import { AgentVerdict } from './AgentVerdict'
import { DiffView } from './DiffView'
import { LineComment } from './LineComment'
import { MergeConfirmDialog, merges, mergeVerb } from './MergeConfirmDialog'
import { commentFirst, heldBack, needsText, ReviewComposer, replyActionOf, sendsText, sentText } from './ReviewComposer'
import { ReviewHeader } from './ReviewHeader'
import { areasText, checksSummary, Readiness, ReviewSummary } from './ReviewSummary'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

describe('ReviewSummary', () => {
  it('says what a dependency update bumps, and how far', () => {
    const bump = {
      name: 'golang.org/x/net',
      from: '0.30.0',
      to: '1.0.0',
      group: '',
      dir: '',
      count: 1,
      level: 'major' as const,
      security: true,
    }
    const { rerender } = render(<ReviewSummary review={review({ bump })} item={undefined} />)

    expect(screen.getByText('golang.org/x/net 0.30.0 → 1.0.0 · major · security')).toHaveClass('text-warning')

    rerender(
      <ReviewSummary
        review={review({ bump: { ...bump, from: '', to: 'v9', level: '', security: false } })}
        item={undefined}
      />,
    )
    expect(screen.getByText('golang.org/x/net v9')).toBeInTheDocument()

    rerender(
      <ReviewSummary
        review={review({ bump: { ...bump, name: '', group: 'go-deps', count: 3, level: '', security: false } })}
        item={undefined}
      />,
    )
    expect(screen.getByText('go-deps group · 3 updates')).toBeInTheDocument()

    rerender(
      <ReviewSummary
        review={review({ bump: { ...bump, name: '', group: '', count: 0, level: '', security: false } })}
        item={undefined}
      />,
    )
    expect(screen.getByText('dependencies')).toBeInTheDocument()
  })

  it('lists the checks, the failing first, and the reviews', async () => {
    render(
      <ReviewSummary
        review={review({
          checks: [
            { name: 'lint', workflow: '', state: 'SUCCESS', outcome: 'passed', url: '', required: false },
            {
              name: 'e2e',
              workflow: '',
              state: 'FAILURE',
              outcome: 'failed',
              url: 'https://example.test/run/1',
              required: true,
            },
            { name: 'build', workflow: '', state: 'IN_PROGRESS', outcome: 'pending', url: '', required: false },
            { name: 'docs', workflow: '', state: 'SKIPPED', outcome: 'skipped', url: '', required: false },
            { name: 'other', workflow: '', state: '?', outcome: 'odd', url: '', required: false },
          ],
          reviews: [
            { author: 'a', state: 'CHANGES_REQUESTED', body: '', url: '', submittedAt: '', outdated: false },
            { author: 'b', state: 'DISMISSED', body: '', url: '', submittedAt: '', outdated: false },
            { author: 'c', state: 'COMMENTED', body: '', url: '', submittedAt: '', outdated: false },
          ],
          change: { kind: 'code', label: 'Code', areas: [] },
        })}
        item={item({ files: fileCheck() })}
      />,
    )

    expect(screen.getByText('5 checks · 1 failing · 1 running').closest('details')).toHaveAttribute('open')
    const [ran] = screen.getAllByRole('list')
    expect(ran).toBeDefined()
    expect(
      within(ran as HTMLElement)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['e2erequired', 'build', 'lint'])

    // The checks skipped, and any GitHub reports otherwise, fold away below the ones that ran.
    const skipped = screen.getByText('2 skipped').closest('details')
    expect(skipped).not.toHaveAttribute('open')
    expect(skipped).toHaveTextContent('docs')
    expect(skipped).toHaveTextContent('other')
    await userEvent.click(screen.getByRole('button', { name: 'e2e' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://example.test/run/1')
    expect(screen.getByText('requested changes')).toBeInTheDocument()
    expect(screen.getByText('dismissed')).toBeInTheDocument()
    expect(screen.getByText('commented')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Agent config/ })).toBeInTheDocument()
  })

  it('names the workflow runs GitHub counts as not passing that ran no check', () => {
    const { rerender } = render(
      <ReviewSummary
        review={review({
          unrun: [
            { name: 'PR Management', approval: true },
            { name: 'Auto-merge Trigger', approval: true },
            { name: 'Auto-merge Trigger', approval: true },
            { name: 'Auto-merge Trigger', approval: false },
          ],
        })}
        item={undefined}
      />,
    )

    const runs = screen.getByRole('list', { name: 'Workflow runs with no check' })
    expect(
      within(runs)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual([
      '3 workflow runs awaiting approval: PR Management, Auto-merge Trigger',
      '1 workflow run that didn’t start: Auto-merge Trigger',
    ])

    rerender(<ReviewSummary review={review({ checks: [], unrun: [{ name: '', approval: false }] })} item={undefined} />)
    expect(screen.getByText('No checks on this commit.')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Workflow runs with no check' })).toHaveTextContent(
      '1 workflow run that didn’t start: an app',
    )

    rerender(<ReviewSummary review={review()} item={undefined} />)
    expect(screen.queryByRole('list', { name: 'Workflow runs with no check' })).not.toBeInTheDocument()
  })

  it('says when a commit has no checks', () => {
    render(
      <ReviewSummary
        review={review({ checks: [], readiness: { state: 'closed', sentence: 'No longer open', needs: [] } })}
        item={undefined}
      />,
    )

    expect(screen.getByText('No checks on this commit.')).toBeInTheDocument()
    expect(screen.queryByText('No longer open')).not.toBeInTheDocument()
    expect(checksSummary([])).toEqual({ counts: '0 checks', running: '' })
  })

  it('marks each check running, queued or in progress, with a ring, and their count too', () => {
    const check = (name: string, state: string, outcome: string) => ({
      name,
      workflow: '',
      state,
      outcome,
      url: '',
      required: false,
    })
    const ring = (el: Element | null) => el?.querySelector('.motion-safe\\:animate-ping') ?? null
    const { rerender } = render(
      <ReviewSummary
        review={review({
          checks: [
            check('lint', 'SUCCESS', 'passed'),
            check('build', 'IN_PROGRESS', 'pending'),
            check('e2e', 'FAILURE', 'failed'),
            check('deploy', 'QUEUED', 'pending'),
            check('docs', 'SKIPPED', 'skipped'),
          ],
        })}
        item={undefined}
      />,
    )

    // The failing first, then those running, then those passed.
    const [ran] = screen.getAllByRole('list')
    const rows = within(ran as HTMLElement).getAllByRole('listitem')
    expect(rows.map((li) => [li.textContent, ring(li) !== null])).toEqual([
      ['e2e', false],
      ['build', true],
      ['deploy', true],
      ['lint', false],
    ])
    const skipped = screen.getByText('1 skipped').closest('details')
    expect(skipped).toHaveTextContent('docs')
    expect(ring(skipped)).toBeNull()

    // The count, which stays in view with the list folded, has the ring too.
    const summary = screen.getByText('5 checks · 1 failing · 2 running')
    expect(summary.tagName).toBe('SUMMARY')
    expect(ring(summary)).not.toBeNull()

    rerender(
      <ReviewSummary
        review={review({ checks: [check('lint', 'SUCCESS', 'passed'), check('e2e', 'FAILURE', 'failed')] })}
        item={undefined}
      />,
    )
    // With nothing running, nothing moves.
    const finished = screen.getByText('2 checks · 1 failing')
    expect(finished.tagName).toBe('SUMMARY')
    expect(ring(finished.closest('details'))).toBeNull()
  })

  it('says how close it is to merging, in a line of its own', () => {
    render(<Readiness review={review({ readiness: { state: 'closed', sentence: 'No longer open', needs: [] } })} />)

    expect(screen.getByText('No longer open')).toHaveClass('text-muted')
  })

  it('names the folders a change touches, and its top level', () => {
    expect(areasText([])).toBe('')
    expect(areasText(['.github'])).toBe('.github/')
    expect(areasText(['wallet', '.'])).toBe('wallet/ and the top level')
    expect(areasText(['cmd', 'internal', '.'])).toBe('cmd/, internal/ and the top level')
  })
})

describe('MergeConfirmDialog', () => {
  it('confirms only the actions that merge', () => {
    for (const a of ['approve_merge', 'approve_auto_merge', 'merge_now', 'auto_merge', 'merge_bypass'] as const) {
      expect(merges(a)).toBe(true)
    }

    for (const a of ['approve', 'comment', 'request_changes', 'update_branch', 'rebase_bot', 'close', ''] as const) {
      expect(merges(a)).toBe(false)
    }
  })

  it('words each kind of merge', () => {
    const cases = [
      { action: 'merge_now' as const, title: 'Merge #7 into main?', label: 'Squash-merge' },
      {
        action: 'approve_auto_merge' as const,
        title: 'Approve, and merge #7 into main once its requirements are met?',
        label: 'Approve & auto-merge',
      },
      {
        action: 'auto_merge' as const,
        title: 'Merge #7 into main automatically once its requirements are met?',
        label: 'Merge when ready',
      },
    ]

    for (const c of cases) {
      const { unmount } = render(
        <MergeConfirmDialog
          review={review({ mergeMethods: ['SQUASH', 'REBASE', ''] })}
          offer={offer({ action: c.action, confirm: true })}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      )

      expect(screen.getByRole('dialog', { name: c.title })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: c.label })).toBeEnabled()
      expect(screen.getByRole('radio', { name: 'Rebase and merge' })).toBeInTheDocument()
      unmount()
    }

    expect(mergeVerb('REBASE')).toBe('rebase-merge')
    expect(mergeVerb('MERGE')).toBe('merge')
  })

  it('confirms with the method chosen', async () => {
    const onConfirm = vi.fn()
    render(
      <MergeConfirmDialog
        review={review()}
        offer={offer({ action: 'merge_now', confirm: true })}
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Squash-merge' }))
    expect(onConfirm).toHaveBeenCalledWith('SQUASH')
  })

  it('names each rule a merge past them skips, and holds it until accepted', async () => {
    const onConfirm = vi.fn()
    render(
      <MergeConfirmDialog
        review={review()}
        offer={offer({
          action: 'merge_bypass',
          label: 'Squash-merge past the rules',
          confirm: true,
          warnings: ['It needs an approval from someone else.'],
        })}
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      />,
    )

    const dialog = screen.getByRole('dialog', { name: 'Merge #7 into main past the repository’s rules?' })
    expect(dialog).toHaveTextContent('GitHub would hold this merge back:')
    expect(screen.getByRole('listitem')).toHaveTextContent('It needs an approval from someone else.')
    expect(dialog).toHaveTextContent(/Nothing else here ever does, and no agent can/)

    const confirm = screen.getByRole('button', { name: 'Merge past the rules' })
    expect(confirm).toBeDisabled()
    await userEvent.click(screen.getByRole('checkbox', { name: 'I’m merging past the rules' }))
    await userEvent.click(confirm)
    expect(onConfirm).toHaveBeenCalledWith('SQUASH')
  })
})

describe('ReviewHeader', () => {
  it('falls back to the card, and opens GitHub', async () => {
    const onClose = vi.fn()
    const { rerender } = render(
      <ReviewHeader
        titleId="t"
        review={null}
        item={item()}
        position=""
        onPrev={null}
        onNext={null}
        onClose={onClose}
        onRefresh={vi.fn()}
        checking={false}
      />,
    )

    expect(screen.getByRole('heading', { name: 'Add the widget cache' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledOnce()

    rerender(
      <ReviewHeader
        titleId="t"
        review={null}
        item={undefined}
        position=""
        onPrev={null}
        onNext={null}
        onClose={onClose}
        onRefresh={vi.fn()}
        checking={false}
      />,
    )
    expect(screen.getByRole('button', { name: 'Open on GitHub' })).toBeDisabled()
    expect(screen.getByText('#0 ·', { exact: false })).toBeInTheDocument()

    rerender(
      <ReviewHeader
        titleId="t"
        review={review({ headRef: '' })}
        item={undefined}
        position=""
        onPrev={null}
        onNext={null}
        onClose={onClose}
        onRefresh={vi.fn()}
        checking={false}
      />,
    )
    expect(screen.queryByText(/→ main/)).not.toBeInTheDocument()
  })
})

describe('AgentVerdict', () => {
  it('shows nothing without a session, and says when it has said nothing', () => {
    const { container, rerender } = render(<AgentVerdict sessions={undefined} agents={agents()} onUse={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()

    rerender(<AgentVerdict sessions={[agentSession({ lastMessage: '' })]} agents={agents()} onUse={vi.fn()} />)
    expect(screen.getByText('It hasn’t said anything yet.')).toBeInTheDocument()
  })

  it('says when the whole message could not be read', async () => {
    platform.sessionLastWords.mockRejectedValue(new Error('No such session'))
    render(<AgentVerdict sessions={[agentSession()]} agents={agents()} onUse={vi.fn()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Show all' }))
    expect(await screen.findByText('No such session')).toBeInTheDocument()
  })

  it('puts what it said into the sheet’s text, as its button names', async () => {
    const onUse = vi.fn()
    render(
      <AgentVerdict
        sessions={[agentSession({ lastMessage: 'Fixed in the parser.' })]}
        agents={agents()}
        onUse={onUse}
        useLabel="Use as comment"
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Use as comment' }))
    expect(onUse).toHaveBeenCalledWith('Fixed in the parser.')
  })
})

describe('LineComment', () => {
  it('suggests a change after what was written', async () => {
    render(<LineComment where="line 3" side="RIGHT" lines={['x := 1']} initial="" onAdd={vi.fn()} onCancel={vi.fn()} />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Comment on line 3' }), 'Try')
    await userEvent.click(screen.getByRole('button', { name: 'Suggest a change' }))

    expect(screen.getByRole('textbox', { name: 'Comment on line 3' })).toHaveValue('Try\n```suggestion\nx := 1\n```\n')
  })
})

describe('ActionButton', () => {
  it('lists every offer, with why one is off, and runs the one chosen', async () => {
    const onSelect = vi.fn()
    const onRun = vi.fn()
    render(
      <ActionButton
        offers={[offer(), offer({ action: 'merge_now', label: 'Merge', enabled: false, reason: 'It’s behind main.' })]}
        selected="merge_now"
        onSelect={onSelect}
        onRun={onRun}
        busy={false}
        blocked={() => ''}
      />,
    )

    expect(screen.getByRole('button', { name: 'Merge' })).toHaveAttribute('title', 'It’s behind main.')

    await userEvent.click(screen.getByRole('button', { name: 'More review actions' }))
    expect(screen.getByRole('menuitem', { name: 'Merge' })).toBeDisabled()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Approve' }))
    expect(onSelect).toHaveBeenCalledWith('approve')
  })

  it('says it is working', () => {
    render(
      <ActionButton offers={[offer()]} selected="approve" onSelect={vi.fn()} onRun={vi.fn()} busy blocked={() => ''} />,
    )

    expect(screen.getByRole('button', { name: 'Working…' })).toBeDisabled()
  })

  it('runs a close when it is picked, and never makes it the button or the choice', async () => {
    const onSelect = vi.fn()
    const onRun = vi.fn()
    const { rerender } = render(
      <ActionButton
        offers={[offer(), closeOffer()]}
        selected="close"
        onSelect={onSelect}
        onRun={onRun}
        busy={false}
        blocked={() => ''}
        menuOnly={menuOnly}
      />,
    )

    // A close asked for as the choice is no button: the menu holds it.
    expect(screen.queryByRole('button', { name: /^Close pull request/ })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Choose an action' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Close pull request' }))
    expect(onRun).toHaveBeenCalledWith('close')
    expect(onSelect).not.toHaveBeenCalled()

    // While GitHub can't take its comment, the menu holds it back and says why.
    rerender(
      <ActionButton
        offers={[offer(), closeOffer()]}
        selected="approve"
        onSelect={onSelect}
        onRun={onRun}
        busy={false}
        blocked={(a) => (a === 'close' ? 'You have a review started on GitHub.' : '')}
        menuOnly={menuOnly}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: 'More review actions' }))
    expect(screen.getByRole('menuitem', { name: 'Close pull request' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Close pull request' })).toHaveAttribute(
      'title',
      'You have a review started on GitHub.',
    )
  })

  it('counts a close as nothing to do, keeping it in a menu beside the words', async () => {
    const onRun = vi.fn()
    const { rerender } = render(
      <ActionButton
        offers={[offer({ enabled: false, reason: 'Not now.' }), closeOffer()]}
        selected=""
        onSelect={vi.fn()}
        onRun={onRun}
        busy={false}
        blocked={() => ''}
        menuOnly={menuOnly}
      />,
    )

    expect(screen.getByText('Nothing to do here now.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'More review actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Close pull request' }))
    expect(onRun).toHaveBeenCalledWith('close')

    // With no close to run either, there is no menu at all.
    rerender(
      <ActionButton
        offers={[offer({ enabled: false, reason: 'Not now.' }), closeOffer({ enabled: false, reason: 'Wait.' })]}
        selected=""
        onSelect={vi.fn()}
        onRun={onRun}
        busy={false}
        blocked={() => ''}
        menuOnly={menuOnly}
      />,
    )
    expect(screen.getByText('Nothing to do here now.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More review actions' })).not.toBeInTheDocument()
  })

  it('takes a sheet’s own menu, with any action as the button and items set apart at its foot', async () => {
    const onSelect = vi.fn()
    const onRun = vi.fn()
    const unassign = vi.fn()
    render(
      <ActionButton
        offers={[
          { action: 'comment', label: 'Comment', enabled: true, reason: '', confirm: false },
          { action: 'close', label: 'Close as completed', enabled: true, reason: '', confirm: true },
        ]}
        selected="close"
        onSelect={onSelect}
        onRun={onRun}
        busy={false}
        blocked={() => ''}
        menuLabel="More issue actions"
        extra={[{ label: 'Unassign me', onSelect: unassign }]}
      />,
    )

    // Nothing runs only from this menu, so even a close can be the button, and asks first.
    await userEvent.click(screen.getByRole('button', { name: 'Close as completed…' }))
    expect(onRun).toHaveBeenCalledWith('close')

    await userEvent.click(screen.getByRole('button', { name: 'More issue actions' }))
    const menu = screen.getByRole('menu')
    expect(
      within(menu)
        .getAllByRole('menuitem')
        .map((m) => m.textContent),
    ).toEqual(['Comment', 'Close as completed', 'Unassign me'])
    // Only the items set apart get a line above them.
    expect(within(menu).getAllByRole('separator')).toHaveLength(1)
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unassign me' }))
    expect(unassign).toHaveBeenCalledOnce()
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('keeps the items set apart in a menu beside the words when nothing else can run', async () => {
    const unassign = vi.fn()
    render(
      <ActionButton
        offers={[offer({ enabled: false, reason: 'Not now.' })]}
        selected=""
        onSelect={vi.fn()}
        onRun={vi.fn()}
        busy={false}
        blocked={() => ''}
        menuLabel="More issue actions"
        extra={[{ label: 'Unassign me', onSelect: unassign }]}
      />,
    )

    expect(screen.getByText('Nothing to do here now.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'More issue actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unassign me' }))
    expect(unassign).toHaveBeenCalledOnce()
  })

  it('holds the items set apart back while an action runs, with no line above them when they stand alone', async () => {
    const { rerender } = render(
      <ActionButton
        offers={[offer({ action: 'comment', label: 'Comment' })]}
        selected="comment"
        onSelect={vi.fn()}
        onRun={vi.fn()}
        busy
        blocked={() => ''}
        menuLabel="More issue actions"
        extra={[{ label: 'Unassign me', onSelect: vi.fn() }]}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'More issue actions' }))
    expect(screen.getByRole('menuitem', { name: 'Unassign me' })).toBeDisabled()
    await userEvent.keyboard('{Escape}')

    rerender(
      <ActionButton
        offers={[]}
        selected=""
        onSelect={vi.fn()}
        onRun={vi.fn()}
        busy={false}
        blocked={() => ''}
        menuLabel="More issue actions"
        extra={[{ label: 'Unassign me', onSelect: vi.fn() }]}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: 'More issue actions' }))
    expect(screen.getByRole('menuitem', { name: 'Unassign me' })).toBeEnabled()
    expect(within(screen.getByRole('menu')).queryByRole('separator')).not.toBeInTheDocument()
  })
})

describe('DiffView', () => {
  it('says why there are no files', () => {
    const { rerender } = render(
      <DiffView
        review={review({ files: [], stale: true })}
        draft={emptyDraft}
        onDraft={vi.fn()}
        loading={new Set()}
        onLoad={vi.fn()}
      />,
    )
    expect(screen.getByText('Reading the files from GitHub…')).toBeInTheDocument()

    rerender(
      <DiffView
        review={review({ files: [] })}
        draft={emptyDraft}
        onDraft={vi.fn()}
        loading={new Set()}
        onLoad={vi.fn()}
      />,
    )
    expect(screen.getByText('No files changed.')).toBeInTheDocument()

    // Never read from GitHub (its first read failed): it says so, rather than reading.
    rerender(
      <DiffView
        review={review({ files: [], stale: true, fetchedAt: '' })}
        draft={emptyDraft}
        onDraft={vi.fn()}
        loading={new Set()}
        onLoad={vi.fn()}
      />,
    )
    expect(screen.getByText('Not read from GitHub yet.')).toBeInTheDocument()
  })
})

describe('ReviewComposer', () => {
  it('starts each pull request’s launch from its own settings as the sheet steps to the next', async () => {
    const launch = {
      agents: agents(),
      profiles: [profile()],
      prompts: prompts(),
      launchDefaults: [],
      onDefaultsSaved: () => Promise.resolve(),
      onManagePrompts: () => undefined,
      onSettingsSaved: () => undefined,
    }
    // composer is the sheet's composer for pull request id, which the sheet steps through
    // in place.
    const composer = (id: number) => (
      <ReviewComposer
        review={review({ id })}
        item={item({ id, number: id + 6 })}
        draft={emptyDraft}
        onDraft={vi.fn()}
        reply={{ list: undefined, fallback: 0, body: '' }}
        selected="approve"
        onRun={vi.fn()}
        busy={false}
        error=""
        launch={launch}
        resolution={itemResolution()}
        sessions={undefined}
      />
    )
    const { rerender } = render(composer(1))

    await userEvent.click(screen.getByRole('button', { name: /^Launch options/ }))
    await userEvent.selectOptions(
      within(screen.getByRole('dialog', { name: 'Launch options' })).getByRole('combobox', { name: 'Prompt' }),
      'Quick look',
    )
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('button', { name: /^Launch options/ })).toHaveAccessibleName(
      'Launch options: Quick look · default, changed for this launch',
    )

    rerender(composer(2))

    expect(screen.getByRole('button', { name: /^Launch options/ })).toHaveAccessibleName(
      'Launch options: Review · default',
    )
  })
})

describe('ReviewComposer helpers', () => {
  it('knows which reply goes with an action, and when one needs text', () => {
    expect(replyActionOf('request_changes')).toBe('request_changes')
    expect(replyActionOf('comment')).toBe('comment')
    expect(replyActionOf('merge_now')).toBe('approve')
    // A close sends only what the user wrote or picked, never a default reply left as it was.
    const untouched = { ...emptyDraft, edited: false }
    const written = { ...emptyDraft, edited: true }
    expect(sentText('close', untouched, 'Reviewed and approved.')).toBe('')
    expect(sentText('close', written, 'Superseded.')).toBe('Superseded.')
    expect(sentText('merge_now', untouched, 'Reviewed and approved.')).toBe('Reviewed and approved.')
    expect(needsText('close', '', 0)).toBe('')
    expect(needsText('comment', ' ', 0)).not.toBe('')
    expect(needsText('comment', '', 1)).toBe('')
    expect(needsText('approve', '', 0)).toBe('')
    // A merge never waits for text: it posts what there is as a comment first.
    expect(needsText('merge_now', '', 0)).toBe('')
    expect(needsText('auto_merge', ' ', 0)).toBe('')
  })

  it('says what a merge with no review of its own posts as a comment first, as its offer says', () => {
    const merge = mergeOffer()

    expect(commentFirst(merge, 'Thanks!', 0)).toBe('Your text is posted as a comment first.')
    expect(commentFirst(merge, 'Thanks!', 2)).toBe('Your text is posted as a comment first.')
    expect(commentFirst(merge, ' \n', 1)).toBe('Your line comments are posted as a comment first.')
    expect(commentFirst(merge, ' \n', 0)).toBe('')
    // The backend trims the same way: a byte order mark is no text, and U+0085 is some.
    expect(commentFirst(merge, '\uFEFF', 0)).toBe('')
    expect(commentFirst(merge, '\u0085', 0)).toBe('Your text is posted as a comment first.')
    expect(commentFirst(undefined, 'Thanks!', 1)).toBe('')
    expect(commentFirst(offer({ action: 'approve_merge' }), 'Thanks!', 1)).toBe('')
  })

  it('sends the text with an offer that reviews or comments first, and with nothing else', () => {
    expect(sendsText(offer())).toBe(true)
    expect(sendsText(mergeOffer({ action: 'auto_merge' }))).toBe(true)
    expect(sendsText(offer({ action: 'update_branch', reviews: false }))).toBe(false)
    expect(sendsText(undefined)).toBe(false)
  })

  it('holds back a merge whose comment GitHub can’t take, only while it has one to post', () => {
    const blocked = mergeOffer({ commentBlocked: 'You have a review started on GitHub. Or clear your text.' })

    expect(heldBack(blocked, 'Thanks!', 0)).toBe('You have a review started on GitHub. Or clear your text.')
    expect(commentFirst(blocked, '', 1)).toBe('You have a review started on GitHub. Or clear your text.')
    expect(heldBack(blocked, ' ', 0)).toBe('')
    expect(heldBack(mergeOffer(), 'Thanks!', 0)).toBe('')
    // A close goes by the same marks.
    const closing = closeOffer({ commentBlocked: 'You have a review started on GitHub. Or clear your text.' })
    expect(heldBack(closing, 'Superseded.', 0)).toBe('You have a review started on GitHub. Or clear your text.')
    expect(heldBack(closing, '', 0)).toBe('')
    expect(commentFirst(closeOffer(), 'Superseded.', 0)).toBe('Your text is posted as a comment first.')
    expect(sendsText(closeOffer())).toBe(true)
    expect(heldBack(offer({ action: 'comment' }), '\uFEFF', 0)).toBe('Write something, or add a line comment, first.')
    expect(heldBack(undefined, '', 0)).toBe('')
  })
})
