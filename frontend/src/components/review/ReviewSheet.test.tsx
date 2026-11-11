import { act, render as renderPlain, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useReviewDrafts } from '../../lib/reviewDrafts'
import type { InboxItem, ReviewResult } from '../../platform'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  item,
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
    expect(within(status).getByRole('heading', { name: 'Approve acme-org/example-repo#7' })).toBeInTheDocument()
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

    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() => {
      expect(platform.refreshReview).toHaveBeenCalledTimes(3)
    })
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
      review({ offers: [offer({ action: 'update_branch', label: 'Update branch' })], primary: 'update_branch' }),
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

  it('says when there is nothing to do', async () => {
    platform.refreshReview.mockResolvedValue(review({ offers: [], primary: '' }))
    render(<Harness />)

    expect(await screen.findByText('Nothing to do here now.')).toBeInTheDocument()
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnReview).not.toHaveBeenCalled()
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
    const started = { title: 'Approve', steps: [] }

    expect(withStep(null, 'Approving')).toBeNull()
    expect(withStep(started, 'Approving')).toEqual({ title: 'Approve', steps: ['Approving'] })

    const once = { title: 'Approve', steps: ['Approving'] }

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
})
