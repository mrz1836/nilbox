import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Result } from '../lib/call'
import type { InboxItem, Nudged } from '../platform'
import { at } from '../test/clock'
import { details, item, replyList, snapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { NudgeDialog, useNudge, type NudgeOptions } from './NudgeDialog'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// waiting is someone else's pull request the user requested changes on, quiet since 09:00
// on June 13th, two days and three hours before the test clock.
const waiting: InboxItem = item({
  number: 434,
  repo: 'acme-org/example-repo',
  title: 'Add the widget cache',
  author: 'contributor-a',
  assigned: true,
  updatedAt: '2026-06-13T09:00:00Z',
  details: details({ viewerVerdict: 'changes' }),
})

// nudgeText and questionText are what the default Nudge reply and the Question reply fill
// in for the pull request.
const nudgeText = 'Hi @contributor-a, just checking in on the changes requested here.'
const questionText = 'A question for you, @contributor-a.'

// withReplies stands the library's comment replies in: Nudge (the default for nudges,
// unless nudge says otherwise) and Question.
function withReplies(nudge = 31) {
  platform.reviewReplies.mockResolvedValue(
    replyList({
      replies: [
        { id: 31, name: 'Nudge the author', nickname: 'Nudge', action: 'comment' },
        { id: 32, name: 'Ask a question', nickname: 'Question', action: 'comment' },
      ],
      default: 0,
      defaultBots: 0,
      nudge,
    }),
  )
  platform.fillReply.mockImplementation((_item, reply) => Promise.resolve(reply === 31 ? nudgeText : questionText))
}

// shown renders the dialog for waiting, its Post resolving with why it failed ('' for
// posted), and returns its handlers.
function shown(why = '', snoozeFirst = false) {
  const onPost = vi.fn<(body: string, snooze: boolean) => Promise<string>>(() => Promise.resolve(why))
  const onCancel = vi.fn()
  const onManageReplies = vi.fn()

  render(
    <NudgeDialog
      item={waiting}
      snoozeDays={3}
      snoozeFirst={snoozeFirst}
      onPost={onPost}
      onCancel={onCancel}
      onManageReplies={onManageReplies}
    />,
    { wrapper: at() },
  )

  return { onPost, onCancel, onManageReplies }
}

// text is the dialog's comment box.
const text = () => screen.getByRole('textbox', { name: 'Your comment' })

describe('NudgeDialog', () => {
  it('names the author and the pull request, says how long it has been quiet, and starts from the Nudge reply', async () => {
    withReplies()
    shown()

    const dialog = screen.getByRole('dialog', { name: 'Nudge @contributor-a' })
    expect(dialog).toHaveTextContent('acme-org/example-repo #434 · Add the widget cache')
    expect(dialog).toHaveTextContent('quiet for 2d')
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })
    expect(platform.fillReply).toHaveBeenCalledWith(waiting.id, 31)
    expect(screen.getByRole('checkbox', { name: /Then snooze for 3 days/ })).not.toBeChecked()
  })

  it('starts empty without a Nudge reply, and posts nothing until there is text', async () => {
    withReplies(0)
    const { onPost } = shown()

    await waitFor(() => {
      expect(platform.reviewReplies).toHaveBeenCalledWith('comment')
    })
    expect(text()).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Post comment' })).toBeDisabled()

    await userEvent.type(text(), 'Any news?')
    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(onPost).toHaveBeenCalledExactlyOnceWith('Any news?', false)
  })

  it('replaces the text it started with by a reply picked, and keeps what the user wrote', async () => {
    withReplies()
    shown()
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })

    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Question' }))
    await waitFor(() => {
      expect(text()).toHaveValue(questionText)
    })

    await userEvent.clear(text())
    await userEvent.type(text(), 'Mine.')
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /^Nudge/ }))
    await waitFor(() => {
      expect(text()).toHaveValue(`Mine.\n\n${nudgeText}`)
    })
  })

  it('posts the text, snoozing after when ticked', async () => {
    withReplies()
    const { onPost } = shown()
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })

    await userEvent.click(screen.getByRole('checkbox', { name: /Then snooze/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(onPost).toHaveBeenCalledExactlyOnceWith(nudgeText, true)
  })

  it('starts with Then snooze ticked when the setting says so', () => {
    withReplies()
    shown('', true)

    expect(screen.getByRole('checkbox', { name: /Then snooze/ })).toBeChecked()
  })

  it('posts on ⌘↩', async () => {
    withReplies()
    const { onPost } = shown()
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })

    await userEvent.click(text())
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')

    expect(onPost).toHaveBeenCalledExactlyOnceWith(nudgeText, false)
  })

  it('says why GitHub refused it, in the dialog, and lets the user try again', async () => {
    withReplies()
    const { onPost } = shown('GitHub refused the comment.')
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })

    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub refused the comment.')
    expect(screen.getByRole('button', { name: 'Post comment' })).toBeEnabled()
    expect(onPost).toHaveBeenCalledOnce()
  })

  it('says why the replies couldn’t load, while they load, and when there are none', async () => {
    platform.reviewReplies.mockRejectedValue(new Error('the library is locked'))
    const { unmount: unmountFirst } = render(
      <NudgeDialog item={waiting} snoozeDays={3} snoozeFirst={false} onPost={vi.fn()} onCancel={vi.fn()} />,
      { wrapper: at() },
    )

    await waitFor(() => {
      expect(platform.reviewReplies).toHaveBeenCalled()
    })
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    expect(await screen.findByRole('menuitem', { name: 'Couldn’t load the replies' })).toBeDisabled()
    expect(
      screen.queryByRole('menuitem', { name: 'Manage replies…' }),
      'nowhere to manage them',
    ).not.toBeInTheDocument()
    unmountFirst()

    platform.reviewReplies.mockReturnValue(new Promise(() => undefined))
    const { unmount: unmountSecond } = render(
      <NudgeDialog item={waiting} snoozeDays={3} snoozeFirst={false} onPost={vi.fn()} onCancel={vi.fn()} />,
      { wrapper: at() },
    )
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    expect(screen.getByRole('menuitem', { name: 'Loading replies…' })).toBeInTheDocument()
    unmountSecond()

    platform.reviewReplies.mockResolvedValue(replyList({ replies: [], nudge: 0 }))
    render(<NudgeDialog item={waiting} snoozeDays={3} snoozeFirst={false} onPost={vi.fn()} onCancel={vi.fn()} />, {
      wrapper: at(),
    })
    await waitFor(() => {
      expect(platform.reviewReplies).toHaveBeenCalledTimes(3)
    })
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    expect(await screen.findByRole('menuitem', { name: 'No comment replies yet' })).toBeInTheDocument()
  })

  it('cancels, and opens the replies from the menu’s foot', async () => {
    withReplies()
    const { onCancel, onManageReplies } = shown()

    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Manage replies…' }))
    expect(onManageReplies).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledOnce()
  })
})

// Host holds the Nudge dialog over options, with a button that opens it for waiting.
function Host(options: NudgeOptions) {
  const { onNudge, dialog } = useNudge(options)

  return (
    <>
      <button
        type="button"
        onClick={() => {
          onNudge(waiting)
        }}
      >
        Nudge it
      </button>
      {dialog}
    </>
  )
}

// hostOptions are Host's options, each a mock, its nudge answering with answer.
function hostOptions(answer: Result<Nudged>) {
  return {
    nudge: vi.fn(() => Promise.resolve(answer)),
    snoozeDays: 3,
    snoozeIt: vi.fn(() => Promise.resolve(true)),
    markRead: vi.fn(),
    showToast: vi.fn(),
    closeToast: vi.fn(),
  }
}

// hosted renders Host, its nudge answering with answer, and returns its options.
function hosted(answer: Result<Nudged>) {
  const options = hostOptions(answer)

  render(<Host {...options} />, { wrapper: at() })

  return options
}

// posted is what GitHub posting a nudge answers.
const posted: Result<Nudged> = {
  ok: true,
  data: { snapshot: snapshot(), url: 'https://github.com/acme-org/example-repo/pull/434#issuecomment-1' },
}

describe('useNudge', () => {
  it('posts the nudge, closes, marks the item read, and says so with the comment', async () => {
    withReplies()
    const options = hosted(posted)

    await userEvent.click(screen.getByRole('button', { name: 'Nudge it' }))
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })
    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(options.nudge).toHaveBeenCalledExactlyOnceWith(waiting.id, nudgeText)
    expect(options.markRead).toHaveBeenCalledWith(waiting)
    expect(options.snoozeIt).not.toHaveBeenCalled()
    expect(options.showToast).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        text: 'Nudged @contributor-a on example-repo #434',
        action: expect.objectContaining({ label: 'View comment' }) as unknown,
      }),
    )

    const [[toast]] = options.showToast.mock.calls as unknown as [[{ action: { onClick: () => void } }]]
    toast.action.onClick()
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/pull/434#issuecomment-1',
    )
  })

  it('snoozes after the nudge when ticked, its toast saying both', async () => {
    withReplies()
    const options = hosted(posted)

    await userEvent.click(screen.getByRole('button', { name: 'Nudge it' }))
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })
    await userEvent.click(screen.getByRole('checkbox', { name: /Then snooze/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(options.snoozeIt).toHaveBeenCalledExactlyOnceWith(waiting, 3, 'Nudged @contributor-a and snoozed')
    })
    expect(options.showToast).not.toHaveBeenCalled()
  })

  it('closes for the replies, from its menu’s foot', async () => {
    withReplies()
    const onManageReplies = vi.fn()
    render(<Host {...hostOptions(posted)} onManageReplies={onManageReplies} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'Nudge it' }))
    await userEvent.click(screen.getByRole('button', { name: 'Choose a reply' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Manage replies…' }))

    expect(onManageReplies).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('closes on Cancel, posting nothing', async () => {
    withReplies()
    const options = hosted(posted)

    await userEvent.click(screen.getByRole('button', { name: 'Nudge it' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(options.nudge).not.toHaveBeenCalled()
  })

  it('keeps the dialog open with why GitHub refused it', async () => {
    withReplies()
    const options = hosted({ ok: false, error: 'You can’t comment on this pull request.' })

    await userEvent.click(screen.getByRole('button', { name: 'Nudge it' }))
    await waitFor(() => {
      expect(text()).toHaveValue(nudgeText)
    })
    await userEvent.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('You can’t comment on this pull request.')
    expect(screen.getByRole('dialog', { name: 'Nudge @contributor-a' })).toBeInTheDocument()
    expect(options.markRead).not.toHaveBeenCalled()
  })
})
