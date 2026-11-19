import { describe, expect, it } from 'vitest'

import { closingPull, details, item, list, snapshot } from '../test/fixtures'
import { cardLinks, linkIndex, linkName, linkWords, type CardLink } from './cardLinks'

// The issue #5 is closed by the listed pull request #8 and by #30 in another repository,
// which the inbox doesn't hold; #6 is closed by #8 too.
const pull = item({ id: 1, number: 8, title: 'Fix the flaky retry test' })
const issue = item({
  id: 2,
  kind: 'issue',
  number: 5,
  title: 'The retry test is flaky',
  details: details({
    closingPulls: [
      closingPull({ repo: 'ACME-org/Example-repo' }),
      closingPull({ repo: 'acme-org/other-repo', number: 30, merged: true, trusted: false }),
    ],
  }),
})
const other = item({ id: 3, kind: 'issue', number: 6, details: details({ closingPulls: [closingPull()] }) })

describe('cardLinks', () => {
  const index = linkIndex(snapshot({ reviewRequested: list([other]), yours: list([pull, issue, other]) }))

  it('links an issue to its closing pull requests, in GitHub’s order, held or not', () => {
    expect(cardLinks(issue, index)).toEqual<CardLink[]>([
      {
        kind: 'pr',
        repo: 'ACME-org/Example-repo',
        number: 8,
        title: 'Fix the flaky retry test',
        url: 'https://github.com/acme-org/example-repo/pull/8',
        id: 1,
        merged: false,
        draft: false,
        trusted: true,
      },
      {
        kind: 'pr',
        repo: 'acme-org/other-repo',
        number: 30,
        title: 'Fix the flaky retry test',
        url: 'https://github.com/acme-org/example-repo/pull/8',
        id: 0,
        merged: true,
        draft: false,
        trusted: false,
      },
    ])
  })

  it('links a pull request to the listed issues it closes, by number, each once', () => {
    expect(cardLinks(pull, index).map((l) => [l.kind, l.number, l.id])).toEqual([
      ['issue', 5, 2],
      ['issue', 6, 3],
    ])
  })

  it('links a pull request no listed issue names to nothing', () => {
    expect(cardLinks(item({ id: 9, number: 99 }), index)).toEqual([])
  })

  it('without the inbox, links an issue to GitHub alone and a pull request to nothing', () => {
    expect(cardLinks(issue, null).map((l) => l.id)).toEqual([0, 0])
    expect(cardLinks(pull, null)).toEqual([])
  })

  it('links nothing for an issue whose details aren’t read yet', () => {
    expect(cardLinks(item({ kind: 'issue', details: null }), index)).toEqual([])
  })
})

describe('linkName', () => {
  it.each([
    ['acme-org/example-repo', '#8'],
    ['Acme-Org/Example-Repo', '#8'],
    ['acme-org/other-repo', 'other-repo#8'],
    ['octo-org/tool', 'octo-org/tool#8'],
  ])('names a link in %s from acme-org/example-repo as %s', (repo, want) => {
    expect(linkName({ repo, number: 8 }, 'acme-org/example-repo')).toBe(want)
  })
})

describe('linkWords', () => {
  const [fix, foreign] = cardLinks(issue, null)

  it.each<[string, CardLink | undefined, string]>([
    ['an open fix', fix, 'Fix in progress: #8 Fix the flaky retry test'],
    [
      'an untrusted merged fix elsewhere',
      foreign,
      'Fix merged: other-repo#30 Fix the flaky retry test, from outside the project',
    ],
    [
      'a draft',
      fix === undefined ? undefined : { ...fix, draft: true },
      'Fix in progress: #8 Fix the flaky retry test (draft)',
    ],
    [
      'an issue a pull request closes',
      cardLinks(pull, linkIndex(snapshot({ yours: list([pull, issue]) })))[0],
      'Closes #5 The retry test is flaky once merged',
    ],
  ])('says what %s is', (_name, link, want) => {
    expect(link === undefined ? '' : linkWords(link, 'acme-org/example-repo')).toBe(want)
  })
})
