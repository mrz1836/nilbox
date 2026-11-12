import { act, fireEvent, render as renderPlain, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Activity, useState, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useReviewDrafts } from '../../lib/reviewDrafts'
import { isTopmost } from '../../lib/useModalDialog'
import type { InboxItem, ReviewResult } from '../../platform'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  closeOffer,
  item,
  itemResolution,
  mergeOffer,
  offer,
  profile,
  prompts,
  replyList,
  review,
  reviewFile,
  reviewResult,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import type { LaunchOptions } from '../ItemRow'
import { ReviewSheet, selectedAction, withStep, type ReviewSheetProps } from './ReviewSheet'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const launch: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
}

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

interface HarnessProps extends Partial<ReviewSheetProps> {
  onDone?: (result: ReviewResult) => void
}

// Harness is the sheet over drafts kept as the window keeps them, stepping through three
// pull requests.
function Harness({ onDone = vi.fn(), onClose = vi.fn(), ...props }: HarnessProps) {
  const drafts = useReviewDrafts()
  const [id, setId] = useState(props.id ?? 1)
  const siblings: InboxItem[] = [item({ id: 1 }), item({ id: 2, number: 8 }), item({ id: 3, number: 9 })]

  return (
    <ReviewSheet
      item={item({ id })}
      siblings={siblings}
      listTitle="Assigned"
      drafts={drafts}
      launch={null}
      resolution={null}
      sessions={undefined}
      onNavigate={setId}
      onClose={onClose}
      {...props}
      id={id}
      onDone={onDone}
    />
  )
}

// sheet is the review sheet's dialog.
function sheet() {
  return screen.getByRole('dialog', { name: 'Add the widget cache' })
}

// actionButton is the sheet's main action button, labelled label.
async function actionButton(label: string) {
  return screen.findByRole('button', { name: new RegExp(`^${label}…?$`) })
}

// choose picks an action from the action button's menu.
async function choose(label: string) {
  await userEvent.click(await screen.findByRole('button', { name: 'More review actions' }))
  await userEvent.click(screen.getByRole('menuitem', { name: label }))
}

describe('ReviewSheet', () => {
  it('shows the pull request', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        body: 'Adds a **cache**.',
        reviews: [
          { author: 'octo-reviewer', state: 'APPROVED', body: 'Nice', url: '', submittedAt: '', outdated: true },
        ],
      }),
    )
    render(<Harness />)

    expect(await screen.findByText('cache')).toBeInTheDocument()
    expect(sheet()).toHaveAttribute('open')
    expect(screen.getByText('Ready to merge')).toBeInTheDocument()
    expect(screen.getByText('1 of 3 in Assigned')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'cache/cache.go' })).toBeInTheDocument()
    expect(screen.getByText(/approved an earlier commit/)).toBeInTheDocument()
    expect(await actionButton('Approve & squash-merge')).toBeEnabled()
  })

  it('says what it reads from GitHub while the pull request loads, and that it has no description', async () => {
    platform.openReview.mockReturnValue(new Promise(() => undefined))
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))
    const { unmount } = render(<Harness />)

    // Nothing to show yet: the banner names the pull request and what its read brings.
    const reading = screen.getByRole('region', { name: 'Reading from GitHub' })
    expect(reading).toHaveTextContent('Reading acme-org/example-repo#7 from GitHub…')
    expect(
      within(within(reading).getByRole('list', { name: 'Coming from GitHub' }))
        .getAllByRole('listitem')
        .map((part) => part.textContent),
    ).toEqual(['Description', 'Changed files', 'Checks', 'Reviews'])
    expect(screen.getByText('Checking GitHub…')).toBeInTheDocument()
    unmount()

    // The inbox's copy shows while the first read runs, with the banner, and placeholders
    // where the files, the description, and the summary come.
    platform.openReview.mockResolvedValue(review({ body: '', fetchedAt: '', files: [] }))
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))
    const { unmount: unmountStub } = render(<Harness />)
    expect(await screen.findByRole('region', { name: 'Reading from GitHub' })).toHaveTextContent(
      'Reading acme-org/example-repo#7 from GitHub…',
    )
    expect(screen.getByRole('region', { name: 'Description' })).not.toHaveTextContent(/./)
    expect(screen.queryByRole('navigation', { name: 'Files' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Summary' })).not.toBeInTheDocument()
    unmountStub()

    // Once GitHub answers, the banner goes, and an empty description says so.
    platform.openReview.mockResolvedValue(review({ body: '', fetchedAt: '' }))
    platform.refreshReview.mockResolvedValue(review({ body: '' }))
    render(<Harness />)
    expect(await screen.findByText('No description.')).toBeInTheDocument()
    expect(screen.queryByText(/from GitHub…/)).not.toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Files' })).toBeInTheDocument()
  })

  it('keeps one reading banner, counting on, as the inbox’s copy arrives mid-read', async () => {
    let opened: (r: ReturnType<typeof review>) => void = () => undefined
    platform.openReview.mockReturnValue(
      new Promise((resolve) => {
        opened = resolve
      }),
    )
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))
    render(<Harness />)

    const banner = screen.getByRole('region', { name: 'Reading from GitHub' })

    act(() => {
      opened(review({ body: '', fetchedAt: '', files: [] }))
    })

    expect(await screen.findByRole('region', { name: 'Description' })).toBeInTheDocument()
    expect(banner).toBeInTheDocument()
  })

  it('shows placeholders while a read runs, even with the last pull request’s error showing', async () => {
    platform.openReview.mockRejectedValue(new Error('the database is busy'))
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))
    render(<Harness />)

    expect(await screen.findByRole('alert')).toHaveTextContent('the database is busy')
    expect(screen.getByRole('region', { name: 'Reading from GitHub' })).toBeInTheDocument()
  })

  it('says a pull request GitHub never answered for has not been read', async () => {
    platform.openReview.mockResolvedValue(review({ body: '', fetchedAt: '' }))
    platform.refreshReview.mockRejectedValue(new Error('GitHub is down'))
    render(<Harness />)

    expect(await screen.findByText('Not read from GitHub yet.')).toBeInTheDocument()
    expect(screen.queryByText(/from GitHub…/)).not.toBeInTheDocument()
  })

  it('reads the pull request from GitHub again on request, turning while it reads', async () => {
    platform.refreshReview.mockResolvedValueOnce(review({ body: 'Adds a **cache**.' }))
    render(<Harness />)
    expect(await screen.findByText('cache')).toBeInTheDocument()

    let answer: (r: ReturnType<typeof review>) => void = () => undefined
    platform.readReviewAgain.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    const refresh = screen.getByRole('button', { name: 'Read it from GitHub again' })
    await userEvent.click(refresh)

    // The button's read reaches GitHub however recent the last one was. While it reads,
    // the button waits, keeping focus, and a second click asks nothing more.
    expect(platform.readReviewAgain).toHaveBeenCalledExactlyOnceWith(1)
    expect(platform.refreshReview).toHaveBeenCalledOnce()
    expect(refresh).toHaveAttribute('aria-disabled', 'true')
    expect(refresh).toHaveFocus()
    expect(screen.getByText('Checking GitHub…')).toBeInTheDocument()
    await userEvent.click(refresh)
    expect(platform.readReviewAgain).toHaveBeenCalledOnce()

    act(() => {
      answer(review({ body: 'Adds a **faster cache**.' }))
    })

    expect(await screen.findByText('faster cache')).toBeInTheDocument()
    expect(refresh).toHaveAttribute('aria-disabled', 'false')
  })

  it('approves with the default reply, and closes', async () => {
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await choose('Approve')
    await waitFor(() => {
      expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue(
        'Reviewed and approved. Thanks, @octo-other!',
      )
    })
    await userEvent.click(await actionButton('Approve'))

    await waitFor(() => {
      expect(onDone).toHaveBeenCalledWith(reviewResult(), expect.objectContaining({ id: 1 }))
    })
    expect(platform.actOnReview).toHaveBeenCalledWith({
      itemId: 1,
      action: 'approve',
      head: review().head,
      body: 'Reviewed and approved. Thanks, @octo-other!',
      comments: [],
      method: 'SQUASH',
      accept: false,
    })
  })

  it('shows each step while an action runs, and waits for it', async () => {
    let finish: (result: ReviewResult) => void = () => undefined
    platform.actOnReview.mockReturnValue(
      new Promise<ReviewResult>((resolve) => {
        finish = resolve
      }),
    )
    const onClose = vi.fn()
    const onDone = vi.fn()
    render(<Harness onClose={onClose} onDone={onDone} />)

    await choose('Approve')
    await userEvent.click(await actionButton('Approve'))

    const status = await screen.findByRole('status')
    const heading = within(status).getByRole('heading', { name: 'Approve acme-org/example-repo#7' })
    // The pull request is its own piece of the title, which wraps as a whole after the action.
    expect(within(heading).getByText('acme-org/example-repo#7')).toBeInTheDocument()
    expect(within(status).getByRole('listitem')).toHaveTextContent('Starting…')
    expect(within(status).getByText(/Waiting for GitHub/)).toBeInTheDocument()

    act(() => {
      emit('review:progress', { itemId: 1, step: 'Checking your account' })
      emit('review:progress', { itemId: 2, step: 'Another pull request' })
      emit('review:progress', { itemId: 1, step: 'Approving' })
      emit('review:progress', { itemId: 1, step: 'Approving' })
    })

    const steps = within(status).getAllByRole('listitem')
    expect(steps.map((step) => step.textContent)).toEqual(['Checking your account', 'Approving…'])
    expect(steps[1]).toHaveAttribute('aria-current', 'step')
    expect(steps[0]).not.toHaveAttribute('aria-current')

    // The sheet behind takes no input, and neither Escape nor the arrows leave it.
    expect(sheet().querySelector('[inert]')).toContainElement(
      screen.getByRole('button', { name: 'Close', hidden: true }),
    )
    await userEvent.keyboard('{Escape}')
    await userEvent.keyboard('{ArrowRight}')
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('heading', { name: 'Add the widget cache', hidden: true })).toBeInTheDocument()

    act(() => {
      finish(reviewResult())
    })

    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(sheet().querySelector('[inert]')).toBeNull()
  })

  it('asks before a merge, with the method and the warnings', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [
          offer({
            action: 'approve_merge',
            label: 'Approve & squash-merge',
            confirm: true,
            warnings: ['It changes sensitive files: Workflows.'],
          }),
          offer(),
        ],
      }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await userEvent.click(await actionButton('Approve & squash-merge'))

    const dialog = screen.getByRole('dialog', { name: 'Approve and merge #7 into main?' })
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()

    const confirm = within(dialog).getByRole('button', { name: 'Approve & squash-merge' })
    expect(confirm).toBeDisabled()

    await userEvent.click(within(dialog).getByRole('radio', { name: 'Create a merge commit' }))
    expect(within(dialog).getByRole('button', { name: 'Approve & merge' })).toBeDisabled()

    await userEvent.click(within(dialog).getByRole('checkbox', { name: 'I’ve looked, merge anyway' }))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Approve & merge' }))

    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    expect(platform.actOnReview).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'approve_merge', method: 'MERGE', accept: true }),
    )
  })

  it('sends the text with a merge, which posts it as a comment first, and says so', async () => {
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    const box = await screen.findByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
    // An approval carries the text itself.
    expect(await actionButton('Approve & squash-merge')).toBeEnabled()
    expect(screen.queryByText(/posted as a comment first/)).not.toBeInTheDocument()

    await choose('Squash-merge')
    expect(screen.getByText('Your text is posted as a comment first.')).toHaveClass('text-muted')

    await userEvent.click(await actionButton('Squash-merge'))
    const dialog = screen.getByRole('dialog', { name: 'Merge #7 into main?' })
    // The confirmation says it too.
    expect(within(dialog).getByText('Your text is posted as a comment first.')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Squash-merge' }))

    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    expect(platform.actOnReview).toHaveBeenCalledWith({
      itemId: 1,
      action: 'merge_now',
      head: review().head,
      body: 'Reviewed and approved. Thanks, @octo-other!',
      comments: [],
      method: 'SQUASH',
      accept: false,
    })
  })

  it('merges with no comment once the text is cleared', async () => {
    render(<Harness />)

    await choose('Squash-merge')

    const box = screen.getByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
    await userEvent.clear(box)

    expect(screen.queryByText(/posted as a comment first/)).not.toBeInTheDocument()
    expect(await actionButton('Squash-merge')).toBeEnabled()

    await userEvent.click(await actionButton('Squash-merge'))
    expect(
      within(screen.getByRole('dialog', { name: 'Merge #7 into main?' })).queryByText(/posted as a comment first/),
    ).not.toBeInTheDocument()
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Merge #7 into main?' })).getByRole('button', { name: 'Squash-merge' }),
    )

    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'merge_now', body: '', comments: [] }),
      )
    })
  })

  it('holds a merge back while GitHub can’t take its comment, until the text is cleared', async () => {
    const reason =
      'You have a review of this pull request started on GitHub. Submit or discard it there first. ' +
      'Or clear your text and line comments to merge without a comment.'
    platform.refreshReview.mockResolvedValue(
      review({
        mine: true,
        offers: [
          mergeOffer({ commentBlocked: reason }),
          offer({ action: 'comment', label: 'Comment', enabled: false }),
        ],
        primary: 'merge_now',
      }),
    )
    render(<Harness />)

    const box = await screen.findByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })

    const button = await actionButton('Squash-merge')
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('title', reason)
    expect(screen.getByText(reason)).toHaveClass('text-warning')

    // ⌘↩ holds it back too.
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(screen.queryByRole('dialog', { name: 'Merge #7 into main?' })).not.toBeInTheDocument()

    await userEvent.clear(box)

    expect(screen.queryByText(reason)).not.toBeInTheDocument()
    expect(await actionButton('Squash-merge')).toBeEnabled()

    await userEvent.click(await actionButton('Squash-merge'))
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Merge #7 into main?' })).getByRole('button', { name: 'Squash-merge' }),
    )

    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(expect.objectContaining({ action: 'merge_now', body: '' }))
    })
  })

  it('drops the text a merge posted before it found a push, so it never posts twice', async () => {
    platform.actOnReview.mockResolvedValue(
      reviewResult({
        outcome: 'pushed_since',
        message: 'Commented, but acme-org/example-repo#7 was pushed to since you opened it, so it wasn’t merged.',
        steps: ['Commented'],
        commented: true,
      }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await choose('Squash-merge')

    const box = screen.getByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
    await userEvent.click(await actionButton('Squash-merge'))
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Merge #7 into main?' })).getByRole('button', { name: 'Squash-merge' }),
    )

    expect(await screen.findByText(/Commented, but acme-org\/example-repo#7 was pushed to/)).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
    expect(box).toHaveValue('')
    expect(screen.queryByText(/posted as a comment first/)).not.toBeInTheDocument()
    expect(await actionButton('Squash-merge')).toBeEnabled()
  })

  it('closes with your comment, asking first, and Cancel and Escape close nothing', async () => {
    const closed = reviewResult({
      outcome: 'closed',
      message: 'Closed acme-org/example-repo#7 with your comment',
      steps: ['Commented', 'Closed'],
      commented: true,
      leftInbox: true,
    })
    platform.actOnReview.mockResolvedValue(closed)
    const onDone = vi.fn()
    const onClose = vi.fn()
    render(<Harness onDone={onDone} onClose={onClose} />)

    const box = await screen.findByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
    await userEvent.clear(box)
    await userEvent.type(box, 'Superseded by #9.')

    // Picking it from the menu asks at once; Cancel sends nothing.
    await choose('Close pull request')
    let dialog = screen.getByRole('dialog', { name: 'Close #7 without merging it?' })
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog', { name: 'Close #7 without merging it?' })).not.toBeInTheDocument()

    // Nor does Escape, which closes the confirmation alone.
    await choose('Close pull request')
    expect(screen.getByRole('dialog', { name: 'Close #7 without merging it?' })).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Close #7 without merging it?' })).not.toBeInTheDocument()
    expect(sheet()).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    expect(platform.actOnReview).not.toHaveBeenCalled()

    await choose('Close pull request')
    dialog = screen.getByRole('dialog', { name: 'Close #7 without merging it?' })
    expect(dialog).toHaveTextContent('acme-org/example-repo#7, “Add the widget cache”, closes unmerged.')
    expect(dialog).toHaveTextContent('You can reopen it on GitHub.')
    expect(within(dialog).getByText('Your text is posted as a comment first.')).toBeInTheDocument()
    expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close pull request' }))

    await waitFor(() => {
      expect(onDone).toHaveBeenCalledWith(closed, expect.objectContaining({ id: 1 }))
    })
    expect(platform.actOnReview).toHaveBeenCalledWith({
      itemId: 1,
      action: 'close',
      head: review().head,
      body: 'Superseded by #9.',
      comments: [],
      method: 'SQUASH',
      accept: false,
    })
  })

  it('closes with no comment while the text is the default reply the user never touched', async () => {
    render(<Harness />)

    const box = await screen.findByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })

    // The approval's default reply never goes with a close.
    await choose('Close pull request')
    const dialog = screen.getByRole('dialog', { name: 'Close #7 without merging it?' })
    expect(within(dialog).queryByText(/posted as a comment first/)).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close pull request' }))

    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'close', body: '', comments: [] }),
      )
    })
  })

  it('never makes Close the button, the chosen action, or what ⌘↩ runs', async () => {
    // Reopening keeps the drafts, as the window does, so the sheet opens again on what was
    // chosen before.
    function Reopening() {
      const kept = useReviewDrafts()
      const [open, setOpen] = useState(true)

      return open ? (
        <Harness
          drafts={kept}
          onClose={() => {
            setOpen(false)
          }}
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            setOpen(true)
          }}
        >
          Open again
        </button>
      )
    }

    render(<Reopening />)

    const button = await actionButton('Approve & squash-merge')
    expect(button).toHaveClass('rounded-r-none')

    await choose('Close pull request')
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Close #7 without merging it?' })).getByRole('button', {
        name: 'Cancel',
      }),
    )

    // The button is what it was, and ⌘↩ runs it, never the close.
    expect(await actionButton('Approve & squash-merge')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Close pull request/ })).not.toBeInTheDocument()
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(screen.getByRole('dialog', { name: 'Approve and merge #7 into main?' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: 'Close #7 without merging it?' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    // Opened again, the sheet's button is still its own.
    await userEvent.click(within(sheet()).getByRole('button', { name: 'Close' }))
    await userEvent.click(screen.getByRole('button', { name: 'Open again' }))
    expect(await actionButton('Approve & squash-merge')).toBeInTheDocument()
    expect(platform.actOnReview).not.toHaveBeenCalled()
  })

  it('says there is nothing to do when only a close could run, and keeps the close in its menu', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [
          offer({
            action: 'approve',
            label: 'Approve',
            enabled: false,
            reason: 'You have a review started on GitHub.',
          }),
          closeOffer(),
        ],
        primary: '',
      }),
    )
    render(<Harness />)

    expect(await screen.findByText('Nothing to do here now.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Choose an action' })).not.toBeInTheDocument()

    // ⌘↩ runs nothing.
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(screen.queryByRole('dialog', { name: 'Close #7 without merging it?' })).not.toBeInTheDocument()

    await choose('Close pull request')
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Close #7 without merging it?' })).getByRole('button', {
        name: 'Close pull request',
      }),
    )
    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(expect.objectContaining({ action: 'close' }))
    })
  })

  it('drops the text a close posted when GitHub didn’t close it, so it never posts twice', async () => {
    platform.actOnReview.mockResolvedValue(
      reviewResult({
        outcome: 'commented_not_closed',
        message: 'Commented on acme-org/example-repo#7, but it wasn’t closed: GitHub refused the change',
        reason: 'GitHub refused the change',
        steps: ['Commented'],
        commented: true,
      }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    const box = await screen.findByRole('textbox', { name: 'Your review' })
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
    await userEvent.clear(box)
    await userEvent.type(box, 'Superseded by #9.')
    await choose('Close pull request')
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Close #7 without merging it?' })).getByRole('button', {
        name: 'Close pull request',
      }),
    )

    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    // The draft is cleared: the sheet, still shown here, is back on its own button and reply.
    expect(await actionButton('Approve & squash-merge')).toBeInTheDocument()
    await waitFor(() => {
      expect(box).toHaveValue('Reviewed and approved. Thanks, @octo-other!')
    })
  })

  it('cancels a merge', async () => {
    render(<Harness />)

    await userEvent.click(await actionButton('Approve & squash-merge'))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog', { name: /Approve and merge/ })).not.toBeInTheDocument()
    expect(platform.actOnReview).not.toHaveBeenCalled()
  })

  it('holds a request for changes back until it says something', async () => {
    platform.reviewReplies.mockResolvedValue(replyList({ replies: [], default: 0, defaultBots: 0 }))
    render(<Harness />)

    await choose('Request changes')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Request changes' })).toBeDisabled()
    })

    await userEvent.type(screen.getByRole('textbox', { name: 'Your review' }), 'Please add a test.')
    await userEvent.click(screen.getByRole('button', { name: 'Request changes' }))

    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'request_changes', body: 'Please add a test.' }),
      )
    })
  })

  it('stays open, with the draft, when the pull request was pushed to', async () => {
    platform.actOnReview.mockResolvedValue(
      reviewResult({ outcome: 'pushed_since', message: 'acme-org/example-repo#7 was pushed to since you opened it.' }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    platform.reviewReplies.mockResolvedValue(replyList({ replies: [], default: 0, defaultBots: 0 }))
    await choose('Comment')
    await userEvent.type(screen.getByRole('textbox', { name: 'Your review' }), 'Why?')
    await userEvent.click(await actionButton('Comment'))

    expect(await screen.findByText('acme-org/example-repo#7 was pushed to since you opened it.')).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue('Why?')

    // The push read it again at once, reaching GitHub.
    await waitFor(() => {
      expect(platform.readReviewAgain).toHaveBeenCalledOnce()
    })

    // The header's refresh reads again but leaves the notice for its own Reload to answer.
    await userEvent.click(screen.getByRole('button', { name: 'Read it from GitHub again' }))
    await waitFor(() => {
      expect(platform.readReviewAgain).toHaveBeenCalledTimes(2)
    })
    expect(screen.getByText('acme-org/example-repo#7 was pushed to since you opened it.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() => {
      expect(platform.readReviewAgain).toHaveBeenCalledTimes(3)
    })
    expect(screen.queryByText('acme-org/example-repo#7 was pushed to since you opened it.')).not.toBeInTheDocument()
  })

  it('shows why an action failed', async () => {
    platform.actOnReview.mockRejectedValue(new Error('GitHub refused it.'))
    render(<Harness />)

    await choose('Approve')
    await userEvent.click(await actionButton('Approve'))

    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub refused it.')
  })

  it('steps through its list with the arrows, and runs the action with ⌘↩', async () => {
    platform.openReview.mockImplementation((id) => Promise.resolve(review({ id })))
    platform.refreshReview.mockImplementation((id) => Promise.resolve(review({ id })))
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)

    await actionButton('Approve & squash-merge')
    await userEvent.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenLastCalledWith(2)
    })
    expect(screen.getByText('2 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenLastCalledWith(1)
    })
    await userEvent.keyboard('{ArrowLeft}')
    expect(screen.getByText('1 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Next pull request' }))
    await userEvent.click(screen.getByRole('button', { name: 'Previous pull request' }))
    expect(screen.getByText('1 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Next pull request' }))
    await userEvent.click(screen.getByRole('button', { name: 'Next pull request' }))
    await userEvent.keyboard('{ArrowRight}')
    expect(screen.getByText('3 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.type(screen.getByRole('textbox', { name: 'Your review' }), '{ArrowLeft}')
    expect(screen.getByText('3 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(await screen.findByRole('dialog', { name: /Approve and merge/ })).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()

    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('opens the launch options over itself, which take its keys until Escape closes them alone', async () => {
    platform.openReview.mockImplementation((id) => Promise.resolve(review({ id })))
    platform.refreshReview.mockImplementation((id) => Promise.resolve(review({ id })))
    const onClose = vi.fn()
    render(<Harness onClose={onClose} launch={launch} resolution={itemResolution()} />)

    await actionButton('Approve & squash-merge')

    const arrow = screen.getByRole('button', { name: /^Launch options/ })
    await userEvent.click(arrow)

    const options = screen.getByRole<HTMLDialogElement>('dialog', { name: 'Launch options' })
    expect(isTopmost(options)).toBe(true)
    expect(within(options).getByRole('heading', { name: 'Open in Claude' })).toBeInTheDocument()
    expect(within(options).getByRole('radio', { name: /Review/ })).toHaveFocus()

    // Neither the arrows nor ⌘↩ reach the sheet behind the dialog.
    await userEvent.keyboard('{ArrowRight}')
    await userEvent.keyboard('{ArrowLeft}')
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(screen.getByText('1 of 3 in Assigned')).toBeInTheDocument()
    expect(platform.openReview).not.toHaveBeenCalledWith(2)
    expect(screen.queryByRole('dialog', { name: /Approve and merge/ })).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()

    // Escape closes the launch options, and the sheet stays.
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Launch options' })).not.toBeInTheDocument()
    expect(sheet()).toHaveAttribute('open')
    expect(onClose).not.toHaveBeenCalled()
    expect(arrow).toHaveFocus()

    // The sheet's keys are its own again.
    await userEvent.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenLastCalledWith(2)
    })
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('stays open when the launch options over it get a close request', async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} launch={launch} resolution={itemResolution()} />)

    await actionButton('Approve & squash-merge')
    await userEvent.click(screen.getByRole('button', { name: /^Launch options/ }))

    // React hands a dialog's cancel event on to the dialogs around it, as if it bubbled.
    fireEvent(screen.getByRole('dialog', { name: 'Launch options' }), new Event('cancel', { cancelable: true }))

    expect(screen.queryByRole('dialog', { name: 'Launch options' })).not.toBeInTheDocument()
    expect(sheet()).toHaveAttribute('open')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes the launch options when its page hides, so the sheet alone is on top when it shows', async () => {
    const onClose = vi.fn()
    const page = (mode: 'visible' | 'hidden') => (
      <Activity mode={mode}>
        <Harness onClose={onClose} launch={launch} resolution={itemResolution()} />
      </Activity>
    )
    const { rerender } = render(page('visible'))

    await actionButton('Approve & squash-merge')
    await userEvent.click(screen.getByRole('button', { name: /^Launch options/ }))
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()

    // Another view shows (⌘, opens Settings), then the inbox again.
    rerender(page('hidden'))
    // The launch options close a microtask after their page hides.
    await act(async () => {
      await Promise.resolve()
    })
    rerender(page('visible'))

    expect(document.querySelector('dialog[aria-label="Launch options"]')).toBeNull()
    expect(sheet()).toHaveAttribute('open')
    expect(isTopmost(sheet() as HTMLDialogElement)).toBe(true)

    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('starts each pull request’s launch from its own settings, not the last one’s choices', async () => {
    platform.openReview.mockImplementation((id) => Promise.resolve(review({ id })))
    platform.refreshReview.mockImplementation((id) => Promise.resolve(review({ id })))
    render(<Harness launch={launch} resolution={itemResolution()} />)

    await actionButton('Approve & squash-merge')
    await userEvent.click(screen.getByRole('button', { name: /^Launch options/ }))

    const options = within(screen.getByRole('dialog', { name: 'Launch options' }))
    await userEvent.click(options.getByRole('radio', { name: /Quick look/ }))
    await userEvent.click(options.getByRole('button', { name: 'Close' }))
    expect(screen.getByRole('button', { name: /^Launch options/ })).toHaveAccessibleName(
      'Launch options: Quick look · default, changed for this launch',
    )

    await userEvent.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenLastCalledWith(2)
    })

    expect(screen.getByRole('button', { name: /^Launch options/ })).toHaveAccessibleName(
      'Launch options: Review · default',
    )
  })

  it('writes line comments, with a suggested change, and sends them with the review', async () => {
    render(<Harness />)

    const user = userEvent.setup()
    const diff = await screen.findByRole('table', { name: 'Diff of cache/cache.go' })
    await user.click(within(diff).getByRole('button', { name: 'Comment on line 3' }))
    await user.keyboard('{Shift>}')
    await user.click(within(diff).getByRole('button', { name: 'Comment on line 2' }))
    await user.keyboard('{/Shift}')

    expect(screen.getByText('Comment on lines 2–3')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Suggest a change' }))
    expect(screen.getByRole('textbox', { name: 'Comment on lines 2–3' })).toHaveValue(
      '```suggestion\nvar size = 2\nvar ttl = 3\n```\n',
    )

    await userEvent.click(screen.getByRole('button', { name: 'Add to review' }))
    expect(screen.getByText('1 line comment go with the review.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Comment on lines 2–3' }), 'Try this.')
    await userEvent.click(screen.getByRole('button', { name: 'Save comment' }))

    await choose('Comment')
    await userEvent.click(await actionButton('Comment'))

    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(
        expect.objectContaining({
          action: 'comment',
          comments: [
            {
              path: 'cache/cache.go',
              line: 3,
              side: 'RIGHT',
              startLine: 2,
              startSide: 'RIGHT',
              body: '```suggestion\nvar size = 2\nvar ttl = 3\n```Try this.',
            },
          ],
        }),
      )
    })
  })

  it('deletes and discards line comments, and comments on a removed line', async () => {
    render(<Harness />)

    const diff = await screen.findByRole('table', { name: 'Diff of cache/cache.go' })
    await userEvent.click(within(diff).getByRole('button', { name: 'Comment on line 2' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('button', { name: 'Add to review' })).not.toBeInTheDocument()

    await userEvent.click(within(diff).getByRole('button', { name: 'Comment on old line 2' }))
    expect(screen.queryByRole('button', { name: 'Suggest a change' })).not.toBeInTheDocument()
    await userEvent.type(screen.getByRole('textbox', { name: 'Comment on old line 2' }), 'Why remove it?')
    await userEvent.click(screen.getByRole('button', { name: 'Add to review' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(screen.queryByText('Why remove it?')).not.toBeInTheDocument()

    await userEvent.click(within(diff).getByRole('button', { name: 'Comment on line 4' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Comment on line 4' }), 'Hm.')
    await userEvent.click(screen.getByRole('button', { name: 'Add to review' }))
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.queryByText('Hm.')).not.toBeInTheDocument()
  })

  it('marks files viewed, and reads a large diff on demand', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        files: [
          reviewFile(),
          reviewFile({ path: 'big.go', patch: '', patchState: 'omitted', level: 'danger', category: 'Workflows' }),
          reviewFile({ path: 'logo.png', patch: '', patchState: 'no_content' }),
          reviewFile({ path: 'huge.go', patch: '', patchState: 'cache_cap' }),
        ],
        totalFiles: 5,
        filesComplete: false,
      }),
    )
    render(<Harness />)

    expect(await screen.findByText('GitHub lists only the first 4 of 5 files.')).toBeInTheDocument()
    expect(screen.getByText('GitHub shows no diff for this file (binary or too large).')).toBeInTheDocument()
    expect(screen.getByText(/too large to keep here/)).toBeInTheDocument()

    await userEvent.click(
      within(screen.getByRole('region', { name: 'logo.png' })).getByRole('button', { name: 'Open on GitHub' }),
    )
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7/files')

    await userEvent.click(screen.getByRole('button', { name: 'Show the diff' }))
    await waitFor(() => {
      expect(platform.reviewPatches).toHaveBeenCalledWith(1, review().head, ['big.go'])
    })

    const file = screen.getByRole('region', { name: 'cache/cache.go' })
    await userEvent.click(within(file).getByRole('checkbox', { name: 'Viewed' }))
    expect(within(file).queryByRole('table')).not.toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: 'Files' })).getByLabelText('viewed')).toBeInTheDocument()

    await userEvent.click(within(file).getByRole('checkbox', { name: 'Viewed' }))
    await userEvent.click(within(file).getByRole('button', { name: /cache\/cache.go/ }))
    expect(within(file).getByRole('table')).toBeInTheDocument()

    await userEvent.click(
      within(screen.getByRole('navigation', { name: 'Files' })).getByRole('button', { name: /big.go/ }),
    )
  })

  it('shows the latest agent session, and uses its words as the review', async () => {
    platform.sessionLastWords.mockResolvedValue({ text: 'The **whole** verdict.', partial: true })
    render(<Harness sessions={[agentSession()]} launch={launch} />)

    expect(await screen.findByText('I need to run the test suite. May I?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Show all' }))
    expect(await screen.findByText('whole')).toBeInTheDocument()
    expect(screen.getByText('Only the start of the message could be read.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Use as review comment' }))
    expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue('The **whole** verdict.')
    expect(await actionButton('Comment')).toBeEnabled()
  })

  it('offers the review replies, and opens them in Prompts', async () => {
    const onManageReplies = vi.fn()
    platform.fillReply.mockImplementation((_item, reply) => Promise.resolve(reply === 22 ? 'Update OK.' : 'LGTM!'))
    render(<Harness onManageReplies={onManageReplies} />)

    await choose('Approve')
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Update OK' }))
    await waitFor(() => {
      expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue('Update OK.')
    })

    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'No reply' }))
    expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue('')

    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Manage replies…' }))
    expect(onManageReplies).toHaveBeenCalledOnce()
  })

  it('reloads a pull request the inbox saw pushed', async () => {
    platform.refreshReview.mockResolvedValue(review({ stale: true }))
    render(<Harness />)

    expect(await screen.findByText('This pull request changed since it was read.')).toBeInTheDocument()
  })

  it('runs an action that needs no review', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [offer({ action: 'update_branch', label: 'Update branch', reviews: false })],
        primary: 'update_branch',
      }),
    )
    platform.actOnReview.mockResolvedValue(reviewResult({ outcome: 'branch_updating', message: 'Updating' }))
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await userEvent.click(await actionButton('Update branch'))
    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    expect(platform.actOnReview).toHaveBeenCalledWith(expect.objectContaining({ body: '', comments: [] }))
  })

  it('asks Dependabot to rebase, and asks before updating its branch', async () => {
    const warning =
      'Dependabot stops rebasing a pull request once someone else changes its branch. Ask Dependabot to rebase instead to keep it managed.'
    platform.refreshReview.mockResolvedValue(
      review({
        author: 'dependabot',
        isBot: true,
        offers: [
          offer({ action: 'comment', label: 'Comment' }),
          offer({ action: 'rebase_bot', label: 'Ask Dependabot to rebase', reviews: false }),
          offer({
            action: 'update_branch',
            label: 'Update branch',
            reviews: false,
            confirm: true,
            warnings: [warning],
          }),
        ],
        primary: 'comment',
      }),
    )
    platform.actOnReview.mockResolvedValue(reviewResult({ outcome: 'branch_updating', message: 'Updating' }))
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    // Both wait in the menu while the pull request is neither behind nor in conflict.
    expect(await actionButton('Comment')).toBeInTheDocument()
    await choose('Update branch')
    await userEvent.click(await actionButton('Update branch'))

    // Cancel sends nothing.
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Update the branch of #7 with main?' })).getByRole('button', {
        name: 'Cancel',
      }),
    )
    expect(screen.queryByRole('dialog', { name: 'Update the branch of #7 with main?' })).not.toBeInTheDocument()
    expect(platform.actOnReview).not.toHaveBeenCalled()

    await userEvent.click(await actionButton('Update branch'))
    const dialog = screen.getByRole('dialog', { name: 'Update the branch of #7 with main?' })
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(within(dialog).getByText(warning)).toBeInTheDocument()
    expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument()

    const confirm = within(dialog).getByRole('button', { name: 'Update branch' })
    expect(confirm).toBeDisabled()
    await userEvent.click(within(dialog).getByRole('checkbox', { name: 'I’ve looked, update anyway' }))
    await userEvent.click(confirm)

    await waitFor(() => {
      expect(onDone).toHaveBeenCalled()
    })
    expect(platform.actOnReview).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'update_branch', accept: true, body: '', comments: [] }),
    )

    // Asking Dependabot is one click.
    platform.actOnReview.mockClear()
    await choose('Ask Dependabot to rebase')
    await userEvent.click(await actionButton('Ask Dependabot to rebase'))
    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'rebase_bot', accept: false }),
      )
    })
  })

  it('only opens the menu when the actions that can run are never the button on their own', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [
          offer({
            action: 'comment',
            label: 'Comment',
            enabled: false,
            reason: 'You have a review started on GitHub.',
          }),
          offer({ action: 'rebase_bot', label: 'Ask Dependabot to rebase', reviews: false }),
          offer({
            action: 'update_branch',
            label: 'Update branch',
            reviews: false,
            confirm: true,
            warnings: ['Careful.'],
          }),
        ],
        primary: '',
      }),
    )
    render(<Harness />)

    const choose = await screen.findByRole('button', { name: 'Choose an action' })
    expect(screen.queryByText('Nothing to do here now.')).not.toBeInTheDocument()

    // ⌘↩ runs nothing: there is no action to run until the user picks one.
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnReview).not.toHaveBeenCalled()

    await userEvent.click(choose)
    expect(screen.getByRole('menuitem', { name: 'Comment' })).toBeDisabled()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Ask Dependabot to rebase' }))

    // Picked, it is the button, and runs on a click.
    await userEvent.click(await actionButton('Ask Dependabot to rebase'))
    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(expect.objectContaining({ action: 'rebase_bot' }))
    })
  })

  it('asks before any action with warnings, never with merge methods unless it merges', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [
          offer({
            action: 'rebase_bot',
            label: 'Ask Dependabot to rebase',
            reviews: false,
            confirm: true,
            warnings: ['It drops your commits.'],
          }),
        ],
        primary: 'rebase_bot',
      }),
    )
    platform.actOnReview.mockResolvedValue(reviewResult({ outcome: 'asked_bot', message: 'Asked' }))
    render(<Harness />)

    await userEvent.click(await actionButton('Ask Dependabot to rebase'))

    const dialog = screen.getByRole('dialog', { name: 'Ask Dependabot to rebase on #7?' })
    expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('Merge method')).not.toBeInTheDocument()
    expect(within(dialog).getByText('It drops your commits.')).toBeInTheDocument()

    await userEvent.click(within(dialog).getByRole('checkbox', { name: 'I’ve looked, go ahead' }))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ask Dependabot to rebase' }))
    await waitFor(() => {
      expect(platform.actOnReview).toHaveBeenCalledWith(expect.objectContaining({ action: 'rebase_bot', accept: true }))
    })
  })

  it('says when there is nothing to do', async () => {
    platform.refreshReview.mockResolvedValue(review({ offers: [], primary: '' }))
    render(<Harness />)

    expect(await screen.findByText('Nothing to do here now.')).toBeInTheDocument()
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnReview).not.toHaveBeenCalled()
  })

  it('says there is nothing to do when no offer can run', async () => {
    platform.refreshReview.mockResolvedValue(
      review({
        offers: [
          offer({
            action: 'rebase_bot',
            label: 'Ask Dependabot to rebase',
            reviews: false,
            enabled: false,
            reason: 'Wait for it to load from GitHub.',
          }),
        ],
        primary: '',
      }),
    )
    render(<Harness />)

    expect(await screen.findByText('Nothing to do here now.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Choose an action' })).not.toBeInTheDocument()
  })

  it('runs nothing before the pull request loads', async () => {
    platform.openReview.mockReturnValue(new Promise(() => undefined))
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))
    render(<Harness />)

    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnReview).not.toHaveBeenCalled()
  })

  it('keeps the text when a reply can’t be filled in', async () => {
    render(<Harness />)

    await choose('Approve')
    await waitFor(() => {
      expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue(
        'Reviewed and approved. Thanks, @octo-other!',
      )
    })
    platform.fillReply.mockRejectedValue(new Error('The reply is gone'))
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Update OK' }))

    expect(screen.getByRole('textbox', { name: 'Your review' })).toHaveValue(
      'Reviewed and approved. Thanks, @octo-other!',
    )
  })
})

describe('withStep', () => {
  it('adds each new step once, and nothing when no action runs', () => {
    const started = { title: 'Approve', target: 'acme-org/example-repo#7', steps: [] }

    expect(withStep(null, 'Approving')).toBeNull()
    expect(withStep(started, 'Approving')).toEqual({
      title: 'Approve',
      target: 'acme-org/example-repo#7',
      steps: ['Approving'],
    })

    const once = { title: 'Approve', target: 'acme-org/example-repo#7', steps: ['Approving'] }

    expect(withStep(once, 'Approving')).toBe(once)
  })
})

describe('selectedAction', () => {
  it('keeps the chosen action while it is offered and enabled', () => {
    const r = review()

    expect(selectedAction(r, '')).toBe('approve_merge')
    expect(selectedAction(r, 'comment')).toBe('comment')
    expect(selectedAction(r, 'rebase_bot')).toBe('approve_merge')
    expect(selectedAction(review({ offers: [offer({ enabled: false })] }), 'approve')).toBe('approve_merge')
  })

  it('never keeps a close, which runs only from the menu, even one offered and enabled', () => {
    expect(selectedAction(review(), 'close')).toBe('approve_merge')
    expect(selectedAction(review({ offers: [closeOffer()], primary: 'close' }), 'close')).toBe('')
  })
})
