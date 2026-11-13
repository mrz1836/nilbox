import { act, render as renderPlain, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useIssueDrafts } from '../../lib/issueDrafts'
import type { InboxItem, IssueResult } from '../../platform'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  closeIssueOffer,
  details,
  issue,
  issueComment,
  issueOffer,
  issueResult,
  issueTimeline,
  item,
  profile,
  prompts,
  worktree,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import type { LaunchOptions } from '../ItemRow'
import { IssueSheet, type IssueSheetProps } from './IssueSheet'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const launch: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
  onSettingsSaved: () => undefined,
}

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

// card is issue 12's inbox card, or another issue's.
function card(id = 1): InboxItem {
  return item({
    id,
    kind: 'issue',
    number: 11 + id,
    title: id === 1 ? 'Parser drops the trailing newline' : `Issue ${String(id)}`,
    url: `https://github.com/acme-org/example-repo/issues/${String(11 + id)}`,
    updatedAt: '2026-06-15T11:00:00Z',
    assigned: true,
  })
}

interface HarnessProps extends Partial<IssueSheetProps> {
  onDone?: (result: IssueResult) => void
}

// Harness is the sheet over drafts kept as the window keeps them, stepping through three
// issues.
function Harness({ onDone = vi.fn(), onClose = vi.fn(), ...props }: HarnessProps) {
  const drafts = useIssueDrafts()
  const [id, setId] = useState(props.id ?? 1)

  return (
    <IssueSheet
      item={card(id)}
      siblings={[card(1), card(2), card(3)]}
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

// sheet is the issue sheet's dialog.
function sheet() {
  return screen.getByRole('dialog', { name: 'Parser drops the trailing newline' })
}

// actionButton is the sheet's main action button, labelled label.
async function actionButton(label: string) {
  return screen.findByRole('button', { name: new RegExp(`^${label}…?$`) })
}

// choose picks an action from the action button's menu.
async function choose(label: string) {
  await userEvent.click(await screen.findByRole('button', { name: 'More issue actions' }))
  await userEvent.click(screen.getByRole('menuitem', { name: label }))
}

// write types text into the comment box.
async function write(text: string) {
  await userEvent.type(await screen.findByRole('textbox', { name: 'Your comment' }), text)
}

describe('IssueSheet', () => {
  it('shows the issue: its conversation, where it stands, and its details', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ body: 'It drops the **newline**.' }))
    render(<Harness />)

    expect(await screen.findByText('newline')).toBeInTheDocument()
    expect(sheet()).toHaveAttribute('open')
    expect(screen.getByRole('img', { name: 'Open issue' })).toBeInTheDocument()
    expect(screen.getByText('1 of 3 in Assigned')).toBeInTheDocument()
    expect(screen.getByText('Assigned to you')).toBeInTheDocument()
    expect(screen.getByText('Bug')).toBeInTheDocument()

    const conversation = screen.getByRole('region', { name: 'Conversation' })
    expect(within(conversation).getByRole('article', { name: 'octo-other’s comment' })).toHaveTextContent(
      'Can repro on 1.4.',
    )
    expect(within(conversation).getByRole('article', { name: 'octo-user’s comment' })).toHaveTextContent('You')
    expect(within(conversation).getByText(/mentioned this in/)).toBeInTheDocument()

    expect(screen.getByRole('region', { name: 'Assignees' })).toHaveTextContent('octo-user (you)')
    expect(await actionButton('Comment')).toBeDisabled()
  })

  it('shows placeholders until it has a copy, and the reading banner while it first reads', async () => {
    let answer: (v: ReturnType<typeof issue>) => void = () => undefined
    platform.openIssue.mockReturnValue(new Promise(() => undefined))
    platform.refreshIssue.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    render(<Harness />)

    expect(await screen.findByRole('region', { name: 'Reading from GitHub' })).toHaveTextContent(
      'Reading acme-org/example-repo#12 from GitHub…',
    )
    expect(screen.queryByRole('region', { name: 'Conversation' })).not.toBeInTheDocument()

    act(() => {
      answer(issue())
    })

    expect(await screen.findByRole('region', { name: 'Conversation' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Reading from GitHub' })).not.toBeInTheDocument()
  })

  it('holds the description’s place while it first reads a card', async () => {
    platform.openIssue.mockResolvedValue(
      issue({ fetchedAt: '', body: '', stale: true, timeline: issueTimeline({ items: [], before: '' }) }),
    )
    platform.refreshIssue.mockReturnValue(new Promise(() => undefined))
    render(<Harness />)

    expect(await screen.findByRole('region', { name: 'Reading from GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Description' })).not.toHaveTextContent('No description.')
    expect(screen.queryByText('Not read from GitHub yet.')).not.toBeInTheDocument()
  })

  it('reads the issue again from the header, and opens a description cut short on GitHub', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ bodyTrimmed: true }))
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Read it all on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')

    await userEvent.click(screen.getByRole('button', { name: 'Read it from GitHub again' }))
    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })
  })

  it('says an issue has no description once it read it', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ body: '' }))
    render(<Harness />)

    expect(await screen.findByText('No description.')).toBeInTheDocument()
  })

  it('says a card it never read is not read yet, when the read fails', async () => {
    platform.openIssue.mockResolvedValue(issue({ fetchedAt: '', body: '', stale: true }))
    platform.refreshIssue.mockRejectedValue(new Error('GitHub is down'))
    render(<Harness />)

    expect(await screen.findByText('GitHub is down')).toBeInTheDocument()
    expect(screen.getByText('Not read from GitHub yet.')).toBeInTheDocument()
  })

  it('comments, and reports what the comment came to', async () => {
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await write('Fixed in #14.')
    await userEvent.click(await actionButton('Comment'))

    await waitFor(() => {
      expect(onDone).toHaveBeenCalledWith(issueResult(), expect.objectContaining({ id: 1 }))
    })
    expect(platform.actOnIssue).toHaveBeenCalledWith({
      itemId: 1,
      action: 'comment',
      seen: { comments: 2, latest: 'IC_2', latestAt: '2026-06-15T11:00:00Z' },
      body: 'Fixed in #14.',
    })
  })

  it('starts with no reply, and fills the comment from the one the user picks', async () => {
    const onManageReplies = vi.fn()
    platform.fillIssueReply.mockImplementation((_, replyId) =>
      Promise.resolve(replyId === 31 ? 'Looking into it, @octo-user.' : 'Thanks!'),
    )
    render(<Harness onManageReplies={onManageReplies} />)

    const text = await screen.findByRole('textbox', { name: 'Your comment' })
    expect(text, 'no reply is picked for the user').toHaveValue('')

    // pickReply picks the reply nicknamed name from the Replies menu.
    const pickReply = async (name: string) => {
      await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
      await userEvent.click(screen.getByRole('menuitem', { name }))
    }

    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    expect(screen.getAllByRole('menuitem').map((m) => m.textContent)).toEqual(['Looking', 'Thanks', 'Manage replies…'])
    await userEvent.click(screen.getByRole('menuitem', { name: 'Looking' }))

    expect(platform.fillIssueReply).toHaveBeenCalledWith(1, 31)
    await waitFor(() => {
      expect(text).toHaveValue('Looking into it, @octo-user.')
    })

    // Another pick takes the place of the last one's text, untouched since.
    await pickReply('Thanks')
    await waitFor(() => {
      expect(text).toHaveValue('Thanks!')
    })

    // Text the user wrote stays, with the reply after it.
    await userEvent.type(text, ' Fixed in #14.')
    await pickReply('Looking')
    await waitFor(() => {
      expect(text).toHaveValue('Thanks! Fixed in #14.\n\nLooking into it, @octo-user.')
    })

    await pickReply('Manage replies…')
    expect(onManageReplies).toHaveBeenCalledOnce()
  })

  it('says why a reply couldn’t be filled in', async () => {
    platform.fillIssueReply.mockRejectedValueOnce(new Error('the reply is gone'))
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Looking' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t fill in the reply: the reply is gone')
    expect(screen.getByRole('textbox', { name: 'Your comment' })).toHaveValue('')
  })

  it('says the library has no replies for issues', async () => {
    platform.issueReplies.mockResolvedValue([])
    render(<Harness />)

    await waitFor(() => {
      expect(platform.issueReplies).toHaveBeenCalled()
    })
    await userEvent.click(await screen.findByRole('button', { name: 'Choose a reply' }))
    expect(screen.getByRole('menuitem', { name: 'No replies for issues yet' })).toBeDisabled()
  })

  it('edits the labels, then reads the issue again once GitHub made any of it', async () => {
    render(<Harness />)

    // labels opens the label picker and applies the flip of the label named name.
    const labels = async (name: string) => {
      await userEvent.click(screen.getByRole('button', { name: 'Edit labels' }))
      const panel = within(await screen.findByRole('dialog', { name: 'Labels' }))
      await userEvent.click(await panel.findByRole('checkbox', { name }))
      await userEvent.click(panel.getByRole('button', { name: 'Apply' }))

      return panel
    }

    await screen.findByRole('button', { name: 'Edit labels' })
    await labels('p1')

    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })
    expect(platform.editIssue).toHaveBeenCalledWith({
      itemId: 1,
      addLabels: ['p1'],
      removeLabels: [],
      addAssignees: [],
      removeAssignees: [],
    })

    // A part GitHub made, and why the rest failed.
    platform.editIssue.mockResolvedValueOnce({ sent: 1, failed: 'GitHub kept “bug” on it.' })
    platform.readIssueAgain.mockClear()
    const partly = await labels('bug Something isn’t working')

    expect(await partly.findByRole('alert')).toHaveTextContent('GitHub kept “bug” on it.')
    expect(platform.readIssueAgain, 'what it made shows').toHaveBeenCalledWith(1)

    // Nothing sent, so nothing to read again.
    await userEvent.click(partly.getByRole('button', { name: 'Cancel' }))
    platform.editIssue.mockRejectedValueOnce(new Error('Take yourself off the issue with Unassign me.'))
    platform.readIssueAgain.mockClear()
    const refused = await labels('p1')

    expect(await refused.findByRole('alert')).toHaveTextContent('Take yourself off the issue with Unassign me.')
    expect(platform.readIssueAgain).not.toHaveBeenCalled()
  })

  it('runs the button with ⌘↩, and only once it can', async () => {
    render(<Harness />)

    await screen.findByRole('region', { name: 'Conversation' })
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnIssue, 'a comment with nothing to say').not.toHaveBeenCalled()

    await write('On it.')
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    await waitFor(() => {
      expect(platform.actOnIssue).toHaveBeenCalledOnce()
    })
  })

  it('closes as not planned once confirmed, posting the text first, and shows each step', async () => {
    let finish: (r: IssueResult) => void = () => undefined
    platform.actOnIssue.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    const onDone = vi.fn()
    const onClose = vi.fn()
    render(<Harness onDone={onDone} onClose={onClose} />)

    await write('Works as intended.')
    await choose('Close as not planned')
    expect(screen.getByText('Your text is posted as a comment first.')).toBeInTheDocument()
    await userEvent.click(await actionButton('Close as not planned'))

    const dialog = await screen.findByRole('dialog', { name: 'Close #12 as not planned?' })
    expect(dialog).toHaveTextContent('Nothing is sent if someone commented since you opened it.')
    expect(dialog).toHaveTextContent('Your text is posted as a comment first.')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close as not planned' }))

    const status = await screen.findByRole('status')
    act(() => {
      emit('issue:progress', { itemId: 1, step: 'Checking your account' })
      emit('issue:progress', { itemId: 2, step: 'Another issue' })
      emit('issue:progress', { itemId: 1, step: 'Closing as not planned' })
    })
    expect(
      within(status)
        .getAllByRole('listitem')
        .map((s) => s.textContent),
    ).toEqual(['Checking your account', 'Closing as not planned…'])
    expect(sheet().querySelector('[inert]')).not.toBeNull()
    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()

    const closed = issueResult({
      outcome: 'closed',
      message: 'Closed acme-org/example-repo#12 as not planned',
      leftInbox: true,
    })
    act(() => {
      finish(closed)
    })

    await waitFor(() => {
      expect(onDone).toHaveBeenCalledWith(closed, expect.objectContaining({ id: 1 }))
    })
    expect(platform.actOnIssue).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'close_not_planned', body: 'Works as intended.' }),
    )
  })

  it('asks nothing when the close is cancelled', async () => {
    render(<Harness />)

    await choose('Close as completed')
    await userEvent.click(await actionButton('Close as completed'))
    const dialog = await screen.findByRole('dialog', { name: 'Close #12 as completed?' })
    expect(dialog).not.toHaveTextContent('posted as a comment first')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(platform.actOnIssue).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: /Close #12/ })).not.toBeInTheDocument()
  })

  it('keeps the draft and reads again when someone commented since', async () => {
    platform.actOnIssue.mockResolvedValue(
      issueResult({
        outcome: 'new_activity',
        message: 'octo-third commented since you opened it, so acme-org/example-repo#12 wasn’t closed.',
        commented: false,
        steps: [],
      }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await write('Done.')
    await choose('Close as completed')
    await userEvent.click(await actionButton('Close as completed'))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: /Close #12/ })).getByRole('button', {
        name: 'Close as completed',
      }),
    )

    expect(await screen.findByText(/octo-third commented since you opened it/)).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: 'Your comment' })).toHaveValue('Done.')
    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })

    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(screen.queryByText(/octo-third commented/)).not.toBeInTheDocument()
  })

  it('keeps the text when someone closed the issue first, before it was posted', async () => {
    platform.actOnIssue.mockResolvedValue(
      issueResult({
        outcome: 'already_closed',
        message: 'acme-org/example-repo#12 was already closed as completed',
        commented: false,
        leftInbox: true,
        steps: [],
      }),
    )
    const onDone = vi.fn()
    render(<Harness onDone={onDone} />)

    await write('Fixed in #14.')
    await choose('Close as completed')
    await userEvent.click(await actionButton('Close as completed'))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: /Close #12/ })).getByRole('button', {
        name: 'Close as completed',
      }),
    )

    expect(
      await screen.findByText(
        'acme-org/example-repo#12 was already closed as completed. Your text wasn’t posted, and is still here.',
      ),
    ).toBeInTheDocument()
    expect(onDone).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: 'Your comment' })).toHaveValue('Fixed in #14.')
  })

  it('says why an action failed, only on the issue it ran on', async () => {
    platform.actOnIssue.mockRejectedValue(new Error('GitHub refused the comment.'))
    render(<Harness />)

    await write('Hi.')
    await userEvent.click(await actionButton('Comment'))

    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub refused the comment.')
    await userEvent.click(screen.getByRole('button', { name: /Next/ }))
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  it('takes the user off the issue, and says why it couldn’t', async () => {
    const onUnassign = vi.fn<(it: InboxItem) => Promise<string>>().mockResolvedValue('GitHub refused it.')
    render(<Harness onUnassign={onUnassign} />)

    await choose('Unassign me')

    expect(onUnassign).toHaveBeenCalledWith(card(1))
    expect(await screen.findByText('GitHub refused it.')).toBeInTheDocument()
  })

  it('offers no unassign for an issue the user isn’t assigned to', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ assigned: false }))
    render(<Harness onUnassign={vi.fn()} />)

    await userEvent.click(await screen.findByRole('button', { name: 'More issue actions' }))
    await waitFor(() => {
      expect(screen.queryByRole('menuitem', { name: 'Unassign me' })).not.toBeInTheDocument()
    })
  })

  it('quotes a comment into the reply', async () => {
    render(<Harness />)

    const comment = await screen.findByRole('article', { name: 'octo-other’s comment' })
    await userEvent.click(within(comment).getByRole('button', { name: 'Quote reply' }))

    const box = screen.getByRole('textbox', { name: 'Your comment' })
    expect(box).toHaveValue('> Can repro on 1.4.\n\n')
    expect(box).toHaveFocus()
  })

  it('reads the entries before the ones shown', async () => {
    platform.issueEarlier.mockResolvedValue(
      issueTimeline({ items: [issueComment({ id: 'IC_0', body: 'The first comment.' })], before: '', total: 40 }),
    )
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Show 37 earlier entries' }))

    expect(await screen.findByText('The first comment.')).toBeInTheDocument()
    expect(platform.issueEarlier).toHaveBeenCalledWith(1, 'Y3Vyc29y')
    expect(screen.queryByRole('button', { name: /earlier entr/ })).not.toBeInTheDocument()
  })

  it('says why the earlier entries could not be read', async () => {
    platform.issueEarlier.mockRejectedValue(new Error('GitHub is down'))
    render(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Show 37 earlier entries' }))
    expect(await screen.findByText('GitHub is down')).toBeInTheDocument()
  })

  it('marks what is new since the last visit, and scrolls to it once', async () => {
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.openIssue.mockResolvedValue(issue({ lastSeenAt: '2026-06-15T10:45:00Z' }))
    render(<Harness />)

    const divider = await screen.findByRole('separator', { name: 'New since your last visit' })
    expect(divider.nextElementSibling).toHaveTextContent('Can repro on 1.4.')
    expect(scrolled).toHaveBeenCalledOnce()
  })

  it('says the issue changed since it was read, as its card says, and reads it again', async () => {
    render(
      <Harness item={item({ ...card(1), updatedAt: '2026-06-15T11:30:00Z', details: details({ comments: 3 }) })} />,
    )

    expect(await screen.findByText('This issue changed since it was read.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
  })

  it('steps through its list, and from the header', async () => {
    render(<Harness />)

    await screen.findByRole('region', { name: 'Conversation' })
    await userEvent.keyboard('{ArrowRight}')
    expect(await screen.findByText('2 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Previous/ }))
    expect(await screen.findByText('1 of 3 in Assigned')).toBeInTheDocument()
  })

  it('shows the work on the issue, and uses the agent’s words as the comment', async () => {
    platform.sessionLastWords.mockResolvedValue({ text: 'The **whole** verdict.', partial: false })
    render(
      <Harness
        sessions={[agentSession({ kind: 'issue', number: 12, worktree: 'issue-12' })]}
        worktree={worktree({ branch: 'issue-12' })}
        launch={launch}
      />,
    )

    expect(await screen.findByText('I need to run the test suite. May I?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Show all' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Use as comment' }))
    expect(screen.getByRole('textbox', { name: 'Your comment' })).toHaveValue('The **whole** verdict.')
    expect(await actionButton('Comment')).toBeEnabled()
  })

  it('holds a close back while GitHub would refuse its comment', async () => {
    const blocked = 'The conversation is locked. Clear your text to close it without a comment.'
    platform.refreshIssue.mockResolvedValue(
      issue({
        locked: true,
        offers: [issueOffer({ enabled: false, reason: 'Locked.' }), closeIssueOffer({ commentBlocked: blocked })],
        primary: 'close_completed',
        standing: {
          state: 'locked',
          sentence: 'Locked · only people with write access can comment',
          at: '',
          opened: false,
        },
      }),
    )
    render(<Harness />)

    expect(await screen.findByText('Locked')).toBeInTheDocument()
    await write('Done.')
    expect(screen.getByText(blocked)).toBeInTheDocument()
    expect(await actionButton('Close as completed')).toBeDisabled()

    await userEvent.clear(screen.getByRole('textbox', { name: 'Your comment' }))
    expect(await actionButton('Close as completed')).toBeEnabled()
  })

  it('shows a closed issue, and offers nothing', async () => {
    platform.refreshIssue.mockResolvedValue(
      issue({
        state: 'CLOSED',
        stateReason: 'NOT_PLANNED',
        offers: [],
        primary: '',
        standing: {
          state: 'closed',
          sentence: 'Closed as not planned by octo-other',
          at: '2026-06-15T11:00:00Z',
          opened: false,
        },
      }),
    )
    render(<Harness />)

    expect(await screen.findByRole('img', { name: 'Closed as not planned' })).toBeInTheDocument()
    expect(screen.getByText('Closed as not planned by octo-other')).toBeInTheDocument()
    expect(screen.getByText('Nothing to do here now.')).toBeInTheDocument()
  })
})
