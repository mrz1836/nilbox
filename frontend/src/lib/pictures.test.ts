import { describe, expect, it } from 'vitest'

import { issue, issueComment, issueEvent, issueTimeline } from '../test/fixtures'
import { pictureLogins } from './pictures'

describe('pictureLogins', () => {
  it('names who opened it, its people, and who wrote each comment, each once, sorted', () => {
    expect(pictureLogins(issue())).toEqual(['octo-other', 'octo-user'])

    expect(
      pictureLogins(
        issue({
          author: 'zed-user',
          assignees: [{ login: 'amy-user', mine: false }],
          participants: [],
          timeline: issueTimeline({
            items: [
              issueComment({ id: 'IC_1', actor: 'kim-user' }),
              issueComment({ id: 'IC_2', actor: 'kim-user' }),
              issueComment({ id: 'IC_3', actor: 'dependabot', actorIsBot: true }),
              issueComment({ id: 'IC_4', actor: 'hidden-user', minimized: true }),
              issueComment({ id: 'IC_5', actor: '' }),
              issueEvent({ id: 'LE_1', actor: 'event-user' }),
            ],
          }),
        }),
      ),
      'a bot, a hidden comment, a deleted account, and an event show no picture',
    ).toEqual(['amy-user', 'kim-user', 'zed-user'])
  })

  it('leaves out a bot that opened it', () => {
    expect(pictureLogins(issue({ author: 'dependabot', isBot: true, assignees: [], participants: [] }))).toEqual([
      'octo-other',
      'octo-user',
    ])
  })
})
