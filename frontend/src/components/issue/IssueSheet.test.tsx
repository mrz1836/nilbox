import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useIssueDrafts } from '../../lib/issueDrafts'
import type { LaunchOptions } from '../../lib/launch'
import type { InboxItem, IssueResult } from '../../platform'
import { deferred, nextTask } from '../../test/async'
import { renderAt } from '../../test/clock'
import {
  agentSession,
  agents,
  closeIssueOffer,
  details,
  issue,
  issueComment,
  issueOffer,
  issueRef,
  issueResult,
  issueTimeline,
  item,
  profile,
  prompts,
  subIssue,
  worktree,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
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

// closedIssue is issue 12 as a read finds it once someone closed it.
function closedIssue() {
  return issue({
    state: 'CLOSED',
    stateReason: 'COMPLETED',
    offers: [],
    primary: '',
    standing: { state: 'closed', sentence: 'Closed as completed by octo-other', at: '', opened: false },
  })
}

// closedSince is what the sheet hands on of issue 12, closed since the inbox read it.
const closedSince = {
  id: 1,
  repo: 'acme-org/example-repo',
  number: 12,
  title: 'Parser drops the trailing newline',
  url: 'https://github.com/acme-org/example-repo/issues/12',
  state: 'closed',
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
    renderAt(<Harness />)

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

  it('shows each person’s picture beside their name, read once for everyone shown', async () => {
    platform.issueAvatars.mockResolvedValue({ 'octo-other': 'data:image/png;base64,b3RoZXI=' })
    const { container } = renderAt(<Harness />)

    const conversation = await screen.findByRole('region', { name: 'Conversation' })
    await waitFor(() => {
      expect(
        within(conversation).getByRole('article', { name: 'octo-other’s comment' }).querySelector('img'),
      ).toHaveAttribute('src', 'data:image/png;base64,b3RoZXI=')
    })
    expect(platform.issueAvatars).toHaveBeenCalledExactlyOnceWith(['octo-other', 'octo-user'])
    expect(
      within(conversation).getByRole('article', { name: 'Description' }).querySelector('img'),
      'who opened it',
    ).toHaveAttribute('src', 'data:image/png;base64,b3RoZXI=')
    const mine = within(conversation).getByRole('article', { name: 'octo-user’s comment' })
    expect(mine.querySelector('img'), 'no picture yet: the initial').toBeNull()
    expect(within(screen.getByRole('region', { name: 'Assignees' })).getByText('O')).toBeInTheDocument()
    expect(container.querySelectorAll('img[src^="https:"]'), 'nothing remote loads').toHaveLength(0)
  })

  it('shows placeholders until it has a copy, and the reading banner while it first reads', async () => {
    const answer = deferred<ReturnType<typeof issue>>()
    platform.openIssue.mockReturnValue(new Promise(() => undefined))
    platform.refreshIssue.mockReturnValue(answer.promise)
    renderAt(<Harness />)

    expect(await screen.findByRole('region', { name: 'Reading from GitHub' })).toHaveTextContent(
      'Reading acme-org/example-repo#12 from GitHub…',
    )
    expect(screen.queryByRole('region', { name: 'Conversation' })).not.toBeInTheDocument()

    act(() => {
      answer.resolve(issue())
    })

    expect(await screen.findByRole('region', { name: 'Conversation' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Reading from GitHub' })).not.toBeInTheDocument()
  })

  it('holds the description’s place while it first reads a card', async () => {
    platform.openIssue.mockResolvedValue(
      issue({ fetchedAt: '', body: '', stale: true, timeline: issueTimeline({ items: [], before: '' }) }),
    )
    platform.refreshIssue.mockReturnValue(new Promise(() => undefined))
    renderAt(<Harness />)

    expect(await screen.findByRole('region', { name: 'Reading from GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Description' })).not.toHaveTextContent('No description.')
    expect(screen.queryByText('Not read from GitHub yet.')).not.toBeInTheDocument()
  })

  it('reads the issue again from the header, and opens a description cut short on GitHub', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ bodyTrimmed: true }))
    renderAt(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Read it all on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')

    await userEvent.click(screen.getByRole('button', { name: 'Read it from GitHub again' }))
    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })
  })

  it('says an issue has no description once it read it', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ body: '' }))
    renderAt(<Harness />)

    expect(await screen.findByText('No description.')).toBeInTheDocument()
  })

  it('says a card it never read is not read yet, when the read fails', async () => {
    platform.openIssue.mockResolvedValue(issue({ fetchedAt: '', body: '', stale: true }))
    platform.refreshIssue.mockRejectedValue(new Error('GitHub is down'))
    renderAt(<Harness />)

    expect(await screen.findByText('GitHub is down')).toBeInTheDocument()
    expect(screen.getByText('Not read from GitHub yet.')).toBeInTheDocument()
  })

  it('comments, and reports what the comment came to', async () => {
    const onDone = vi.fn()
    renderAt(<Harness onDone={onDone} />)

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
    renderAt(<Harness onManageReplies={onManageReplies} />)

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
    renderAt(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Looking' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t fill in the reply: the reply is gone')
    expect(screen.getByRole('textbox', { name: 'Your comment' })).toHaveValue('')
  })

  it('says the library has no replies for issues', async () => {
    platform.issueReplies.mockResolvedValue([])
    renderAt(<Harness />)

    await waitFor(() => {
      expect(platform.issueReplies).toHaveBeenCalled()
    })
    await userEvent.click(await screen.findByRole('button', { name: 'Choose a reply' }))
    expect(screen.getByRole('menuitem', { name: 'No replies for issues yet' })).toBeDisabled()
  })

  it('edits the labels, then reads the issue again once GitHub made any of it', async () => {
    renderAt(<Harness />)

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
    // The read starts in the effects of the render that shows the alert, which can come after
    // the findBy above has returned.
    await waitFor(() => {
      expect(platform.readIssueAgain, 'what it made shows').toHaveBeenCalledWith(1)
    })

    // Nothing sent, so nothing to read again.
    await userEvent.click(partly.getByRole('button', { name: 'Cancel' }))
    platform.editIssue.mockRejectedValueOnce(new Error('Take yourself off the issue with Unassign me.'))
    platform.readIssueAgain.mockClear()
    const refused = await labels('p1')

    expect(await refused.findByRole('alert')).toHaveTextContent('Take yourself off the issue with Unassign me.')
    // A read would start in the effects of the render that shows the alert, so they run
    // before the check that none did.
    await nextTask()
    expect(platform.readIssueAgain).not.toHaveBeenCalled()
  })

  it('unassigns someone with ×, through the issue’s edit, then reads it again', async () => {
    const shared = issue({
      assignees: [
        { login: 'octo-user', mine: true },
        { login: 'octo-other', mine: false },
      ],
    })
    platform.openIssue.mockResolvedValue(shared)
    platform.refreshIssue.mockResolvedValue(shared)
    renderAt(<Harness />)

    const assignees = await screen.findByRole('region', { name: 'Assignees' })
    expect(
      within(assignees).queryByRole('button', { name: 'Unassign octo-user' }),
      'Unassign me does that',
    ).not.toBeInTheDocument()

    await userEvent.click(within(assignees).getByRole('button', { name: 'Unassign octo-other' }))

    expect(platform.editIssue).toHaveBeenCalledExactlyOnceWith({
      itemId: 1,
      addLabels: [],
      removeLabels: [],
      addAssignees: [],
      removeAssignees: ['octo-other'],
    })
    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })
  })

  it('runs the button with ⌘↩, and only once it can', async () => {
    renderAt(<Harness />)

    await screen.findByRole('region', { name: 'Conversation' })
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(platform.actOnIssue, 'a comment with nothing to say').not.toHaveBeenCalled()

    await write('On it.')
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    await waitFor(() => {
      expect(platform.actOnIssue).toHaveBeenCalledOnce()
    })
  })

  it('greys the menu’s arrow while the button is held back, and tints it once the action can run', async () => {
    renderAt(<Harness />)

    // With nothing to say, Comment is held back, and its arrow looks it but still opens.
    expect(await actionButton('Comment')).toBeDisabled()
    const arrow = screen.getByRole('button', { name: 'More issue actions' })
    expect(arrow).toBeEnabled()
    expect(arrow).not.toHaveClass('bg-accent/14')

    await choose('Close as not planned')
    expect(await actionButton('Close as not planned')).toBeEnabled()
    expect(screen.getByRole('button', { name: 'More issue actions' })).toHaveClass('bg-accent/14')

    await choose('Comment')
    expect(screen.getByRole('button', { name: 'More issue actions' })).not.toHaveClass('bg-accent/14')
    await write('On it.')
    expect(await actionButton('Comment')).toBeEnabled()
    expect(screen.getByRole('button', { name: 'More issue actions' })).toHaveClass('bg-accent/14')
  })

  it('closes as not planned once confirmed, posting the text first, and shows each step', async () => {
    const finish = deferred<IssueResult>()
    platform.actOnIssue.mockReturnValue(finish.promise)
    const onDone = vi.fn()
    const onClose = vi.fn()
    renderAt(<Harness onDone={onDone} onClose={onClose} />)

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
      finish.resolve(closed)
    })

    await waitFor(() => {
      expect(onDone).toHaveBeenCalledWith(closed, expect.objectContaining({ id: 1 }))
    })
    expect(platform.actOnIssue).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'close_not_planned', body: 'Works as intended.' }),
    )
  })

  it('asks nothing when the close is cancelled', async () => {
    renderAt(<Harness />)

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
    renderAt(<Harness onDone={onDone} />)

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
    // The read that follows finds it closed, which the sheet hands on only once its notice
    // is answered, so the text stays in reach.
    platform.readIssueAgain.mockResolvedValue(closedIssue())
    const onDone = vi.fn()
    const onEnded = vi.fn()
    renderAt(<Harness onDone={onDone} onEnded={onEnded} />)

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
    expect(await screen.findByText('Closed as completed by octo-other')).toBeInTheDocument()
    expect(onEnded).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() => {
      expect(onEnded).toHaveBeenCalledExactlyOnceWith(closedSince)
    })
  })

  it('hands on an issue a read finds closed, once, and shows an open one as before', async () => {
    platform.refreshIssue.mockResolvedValue(closedIssue())
    const onEnded = vi.fn()
    const { unmount } = renderAt(<Harness onEnded={onEnded} />)

    await waitFor(() => {
      expect(onEnded).toHaveBeenCalledExactlyOnceWith(closedSince)
    })
    unmount()

    platform.refreshIssue.mockResolvedValue(issue())
    renderAt(<Harness onEnded={onEnded} />)

    expect(await actionButton('Comment')).toBeInTheDocument()
    expect(onEnded).toHaveBeenCalledOnce()
  })

  it('says why an action failed, only on the issue it ran on', async () => {
    platform.actOnIssue.mockRejectedValue(new Error('GitHub refused the comment.'))
    renderAt(<Harness />)

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
    renderAt(<Harness onUnassign={onUnassign} />)

    await choose('Unassign me')

    expect(onUnassign).toHaveBeenCalledWith(card(1))
    expect(await screen.findByText('GitHub refused it.')).toBeInTheDocument()
  })

  it('takes the user off the issue from × beside their name in Assignees', async () => {
    const onUnassign = vi.fn<(it: InboxItem) => Promise<string>>().mockResolvedValue('')
    renderAt(<Harness onUnassign={onUnassign} />)

    const assignees = await screen.findByRole('region', { name: 'Assignees' })
    await userEvent.click(within(assignees).getByRole('button', { name: 'Unassign me' }))

    expect(onUnassign).toHaveBeenCalledExactlyOnceWith(card(1))
    expect(platform.editIssue, 'not through the edit').not.toHaveBeenCalled()
  })

  it('takes the user off the issue from the assignee picker, after the rest of the edit', async () => {
    platform.refreshIssue.mockResolvedValue(
      issue({
        assignees: [
          { login: 'octo-user', mine: true },
          { login: 'octo-other', mine: false },
        ],
      }),
    )
    const onUnassign = vi.fn<(it: InboxItem) => Promise<string>>().mockResolvedValue('')
    renderAt(<Harness onUnassign={onUnassign} />)

    const assignees = await screen.findByRole('region', { name: 'Assignees' })
    await waitFor(() => {
      expect(within(assignees).getByRole('button', { name: 'Edit assignees' })).toBeInTheDocument()
    })
    await userEvent.click(within(assignees).getByRole('button', { name: 'Edit assignees' }))
    const picker = within(await screen.findByRole('dialog', { name: 'Assignees' }))
    await userEvent.click(picker.getByRole('checkbox', { name: /^octo-user/ }))
    await userEvent.click(picker.getByRole('checkbox', { name: /^octo-other/ }))
    await userEvent.click(picker.getByRole('button', { name: 'Unassign 2' }))

    await waitFor(() => {
      expect(onUnassign).toHaveBeenCalledExactlyOnceWith(card(1))
    })
    expect(platform.editIssue).toHaveBeenCalledExactlyOnceWith({
      itemId: 1,
      addLabels: [],
      removeLabels: [],
      addAssignees: [],
      removeAssignees: ['octo-other'],
    })
  })

  it('offers no unassign for an issue the user isn’t assigned to', async () => {
    platform.refreshIssue.mockResolvedValue(issue({ assigned: false }))
    renderAt(<Harness onUnassign={vi.fn()} />)

    await userEvent.click(await screen.findByRole('button', { name: 'More issue actions' }))
    await waitFor(() => {
      expect(screen.queryByRole('menuitem', { name: 'Unassign me' })).not.toBeInTheDocument()
    })
    expect(
      within(screen.getByRole('region', { name: 'Assignees' })).queryByRole('button', { name: 'Unassign me' }),
    ).not.toBeInTheDocument()
  })

  it('quotes a comment into the reply', async () => {
    renderAt(<Harness />)

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
    renderAt(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Show 37 earlier entries' }))

    expect(await screen.findByText('The first comment.')).toBeInTheDocument()
    expect(platform.issueEarlier).toHaveBeenCalledWith(1, 'Y3Vyc29y')
    expect(screen.queryByRole('button', { name: /earlier entr/ })).not.toBeInTheDocument()
  })

  it('says why the earlier entries could not be read', async () => {
    platform.issueEarlier.mockRejectedValue(new Error('GitHub is down'))
    renderAt(<Harness />)

    await userEvent.click(await screen.findByRole('button', { name: 'Show 37 earlier entries' }))
    expect(await screen.findByText('GitHub is down')).toBeInTheDocument()
  })

  it('marks what is new since the last visit, and scrolls to it once', async () => {
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')
    const scrolledTop = vi.spyOn(Element.prototype, 'scrollTop', 'set').mockImplementation(() => undefined)
    platform.openIssue.mockResolvedValue(issue({ lastSeenAt: '2026-06-15T10:45:00Z' }))
    renderAt(<Harness />)

    const divider = await screen.findByRole('separator', { name: 'New since your last visit' })
    expect(divider.nextElementSibling).toHaveTextContent('Can repro on 1.4.')
    // The scroll runs in the commit that shows the divider, so it has happened by the time
    // the divider can be found, however busy the runner: a scroll left to a later effect
    // would let the window paint the conversation at its top first.
    expect(scrolled).toHaveBeenCalledOnce()
    expect(scrolled.mock.contexts[0]).toBe(divider)

    // Nothing scrolls the conversation back after, once the refresh has answered too.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Read it from GitHub again' })).toHaveAttribute(
        'aria-disabled',
        'false',
      )
    })
    expect(platform.refreshIssue).toHaveBeenCalled()
    expect(scrolledTop).not.toHaveBeenCalled()
    expect(scrolled).toHaveBeenCalledOnce()
  })

  it('says the issue changed since it was read, as its card says, and reads it again', async () => {
    renderAt(
      <Harness item={item({ ...card(1), updatedAt: '2026-06-15T11:30:00Z', details: details({ comments: 3 }) })} />,
    )

    expect(await screen.findByText('This issue changed since it was read.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
  })

  it('steps through its list, and from the header', async () => {
    renderAt(<Harness />)

    await screen.findByRole('region', { name: 'Conversation' })
    await userEvent.keyboard('{ArrowRight}')
    expect(await screen.findByText('2 of 3 in Assigned')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Previous/ }))
    expect(await screen.findByText('1 of 3 in Assigned')).toBeInTheDocument()
  })

  it('shows the work on the issue, and uses the agent’s words as the comment', async () => {
    platform.sessionLastWords.mockResolvedValue({ text: 'The **whole** verdict.', partial: false })
    renderAt(
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

    // The work line, under where it stands, opens the issue's sessions.
    expect(screen.getByText('worktree')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Claude session: needs approval/ }))
    expect(screen.getByRole('dialog', { name: 'Agent sessions' })).toBeInTheDocument()
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
    renderAt(<Harness />)

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
    renderAt(<Harness />)

    expect(await screen.findByRole('img', { name: 'Closed as not planned' })).toBeInTheDocument()
    expect(screen.getByText('Closed as not planned by octo-other')).toBeInTheDocument()
    expect(screen.getByText('Nothing to do here now.')).toBeInTheDocument()
  })

  describe('the tabs', () => {
    // tabOf is the tab whose name starts with name.
    function tabOf(name: string) {
      return screen.getByRole('tab', { name: new RegExp(`^${name}`) })
    }

    // withSubIssues is issue id with two sub-issues, one of them done, of four.
    function withSubIssues(id = 1) {
      return issue({
        id,
        subIssuesTotal: 4,
        subIssuesDone: 2,
        subIssues: [
          subIssue(),
          subIssue({
            issue: issueRef({
              number: 21,
              title: 'Handle a missing newline',
              state: 'CLOSED',
              stateReason: 'COMPLETED',
            }),
          }),
        ],
      })
    }

    // readAs answers every read of an issue with what of says of its ID.
    function readAs(of: (id: number) => ReturnType<typeof issue>) {
      platform.openIssue.mockImplementation((id: number) => Promise.resolve(of(id)))
      platform.refreshIssue.mockImplementation((id: number) => Promise.resolve(of(id)))
    }

    it('shows the conversation alone for an issue without sub-issues', async () => {
      renderAt(<Harness />)

      await screen.findByRole('region', { name: 'Conversation' })
      expect(screen.getAllByRole('tab')).toHaveLength(1)
      expect(tabOf('Conversation')).toHaveAttribute('aria-selected', 'true')
      expect(tabOf('Conversation')).toHaveAccessibleName('Conversation 2+')
    })

    it('shows the Sub-issues tab from the card before the read, then counts both tabs', async () => {
      const answer = deferred<ReturnType<typeof issue>>()
      platform.openIssue.mockReturnValue(new Promise(() => undefined))
      platform.refreshIssue.mockReturnValue(answer.promise)
      renderAt(<Harness item={item({ ...card(1), details: details({ subIssues: { total: 4, completed: 1 } }) })} />)

      await screen.findByRole('region', { name: 'Reading from GitHub' })
      expect(tabOf('Conversation')).toHaveAccessibleName('Conversation')
      expect(tabOf('Sub-issues')).toHaveAccessibleName('Sub-issues 1/4')
      expect(tabOf('Conversation')).toHaveAttribute('aria-selected', 'true')

      await userEvent.click(tabOf('Sub-issues'))
      expect(screen.getByRole('tabpanel').querySelector('.skeleton')).not.toBeNull()

      act(() => {
        answer.resolve(withSubIssues())
      })

      expect(await screen.findByRole('list', { name: 'Open sub-issues' })).toHaveTextContent('#20 Handle CRLF endings')
      expect(tabOf('Sub-issues')).toHaveAccessibleName('Sub-issues 2/4')
      expect(tabOf('Sub-issues')).toHaveAttribute('title', '2 of 4 sub-issues done (50%)')
      expect(tabOf('Conversation')).toHaveAccessibleName('Conversation 2+')
      expect(screen.getByRole('list', { name: 'Done sub-issues' })).toHaveTextContent('#21 Handle a missing newline')
    })

    it('shows the sub-issues from the details', async () => {
      readAs(() => withSubIssues())
      renderAt(<Harness />)

      const details = await screen.findByRole('region', { name: 'Sub-issues' })
      expect(details).toHaveTextContent('2 of 4 done')
      expect(tabOf('Conversation')).toHaveAttribute('aria-selected', 'true')

      await userEvent.click(within(details).getByRole('button', { name: 'Show' }))
      expect(tabOf('Sub-issues')).toHaveAttribute('aria-selected', 'true')
      expect(screen.getByRole('list', { name: 'Open sub-issues' })).toBeVisible()
    })

    it('keeps the Sub-issues tab chosen while stepping, showing the conversation of an issue without any', async () => {
      readAs((id) => (id === 2 ? issue({ id }) : withSubIssues(id)))
      renderAt(<Harness />)

      await screen.findByRole('list', { name: 'Open sub-issues', hidden: true })
      await userEvent.click(tabOf('Sub-issues'))
      expect(tabOf('Sub-issues')).toHaveAttribute('aria-selected', 'true')

      await userEvent.click(screen.getByRole('button', { name: 'Next' }))
      expect(await screen.findByText('2 of 3 in Assigned')).toBeInTheDocument()
      await waitFor(() => {
        expect(screen.getAllByRole('tab')).toHaveLength(1)
      })
      expect(tabOf('Conversation')).toHaveAttribute('aria-selected', 'true')
      expect(screen.getByRole('region', { name: 'Conversation' })).toBeVisible()

      await userEvent.click(screen.getByRole('button', { name: 'Next' }))
      expect(await screen.findByText('3 of 3 in Assigned')).toBeInTheDocument()
      await waitFor(() => {
        expect(tabOf('Sub-issues')).toHaveAttribute('aria-selected', 'true')
      })

      // Choosing the conversation is a choice too: the next issue opens on it.
      await userEvent.click(tabOf('Conversation'))
      await userEvent.click(screen.getByRole('button', { name: /Previous/ }))
      await userEvent.click(screen.getByRole('button', { name: /Previous/ }))
      expect(await screen.findByText('1 of 3 in Assigned')).toBeInTheDocument()
      await waitFor(() => {
        expect(tabOf('Sub-issues')).toHaveAttribute('aria-selected', 'false')
      })
    })

    it('puts a tab back where it was left, and starts the next issue’s tabs afresh', async () => {
      const tops = new WeakMap<Element, number>()
      const scrolls: number[] = []
      vi.spyOn(Element.prototype, 'scrollTop', 'get').mockImplementation(function (this: Element) {
        return tops.get(this) ?? 0
      })
      vi.spyOn(Element.prototype, 'scrollTop', 'set').mockImplementation(function (this: Element, value: number) {
        scrolls.push(value)
        tops.set(this, value)
      })
      readAs((id) => withSubIssues(id))
      renderAt(<Harness />)

      const conversation = await screen.findByRole('main')
      await screen.findByRole('list', { name: 'Open sub-issues', hidden: true })

      tops.set(conversation, 240)
      await userEvent.click(tabOf('Sub-issues'))
      await userEvent.click(tabOf('Conversation'))
      expect(scrolls.at(-1)).toBe(240)

      await userEvent.click(tabOf('Sub-issues'))
      await userEvent.click(screen.getByRole('button', { name: 'Next' }))
      await screen.findByText('2 of 3 in Assigned')
      scrolls.length = 0
      await userEvent.click(tabOf('Conversation'))

      expect(scrolls).toEqual([])
    })
  })
})
