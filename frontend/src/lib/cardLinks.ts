// Links between inbox cards: an issue and the pull requests that close it once they merge,
// GitHub's closing references (a pull request's description says it closes the issue, or
// someone linked the two by hand). Only an issue's details read them, at no extra point,
// since a pull request's would cost one more a batch, so a pull request's card finds the
// issues it closes among the listed issues' links. Nothing here asks GitHub for anything.

import { createContext } from 'react'

import type { InboxItem, InboxSnapshot, ItemKind } from '../platform'
import { itemKey } from './itemWorktrees'
import { inboxLists } from './triage'

// CardLink is an item a card links to: on an issue's card, a pull request that closes it
// once it merges; on a pull request's, a listed issue it closes.
export interface CardLink {
  kind: ItemKind
  repo: string
  number: number
  title: string
  url: string
  // id is the linked item's card in the inbox, 0 when the inbox doesn't hold it.
  id: number
  // merged and draft are a pull request's state; both false for an issue.
  merged: boolean
  draft: boolean
  // trusted is whether the issue's card takes the pull request's word that it closes the
  // issue (ClosingPull.trusted); always true for an issue.
  trusted: boolean
}

// LinkIndex finds the links of one inbox's cards: each listed item's card by itemKey, and,
// by a pull request's itemKey, the listed issues that name it among their closing pull
// requests, by number.
export interface LinkIndex {
  ids: ReadonlyMap<string, number>
  closes: ReadonlyMap<string, readonly InboxItem[]>
}

// linkIndex indexes the snapshot's lists for cardLinks. An item in both lists counts once.
export function linkIndex(snapshot: InboxSnapshot): LinkIndex {
  const ids = new Map<string, number>()
  const closes = new Map<string, InboxItem[]>()

  for (const list of inboxLists) {
    for (const item of snapshot[list].items) {
      const key = itemKey(item.repo, item.number)

      if (ids.has(key)) {
        continue
      }

      ids.set(key, item.id)

      for (const pull of item.kind === 'issue' ? (item.details?.closingPulls ?? []) : []) {
        const pullKey = itemKey(pull.repo, pull.number)
        closes.set(pullKey, [...(closes.get(pullKey) ?? []), item])
      }
    }
  }

  for (const issues of closes.values()) {
    issues.sort((a, b) => a.number - b.number)
  }

  return { ids, closes }
}

// cardLinks lists the items an item's card links to: an issue's closing pull requests, in
// GitHub's order, or the listed issues a pull request closes. index tells which of them the
// inbox holds; without one, none is, and a pull request links to nothing.
export function cardLinks(item: InboxItem, index: LinkIndex | null): CardLink[] {
  if (item.kind === 'issue') {
    return (item.details?.closingPulls ?? []).map((pull) => ({
      kind: 'pr',
      repo: pull.repo,
      number: pull.number,
      title: pull.title,
      url: pull.url,
      id: index?.ids.get(itemKey(pull.repo, pull.number)) ?? 0,
      merged: pull.merged,
      draft: pull.isDraft,
      trusted: pull.trusted,
    }))
  }

  return (index?.closes.get(itemKey(item.repo, item.number)) ?? []).map((issue) => ({
    kind: 'issue',
    repo: issue.repo,
    number: issue.number,
    title: issue.title,
    url: issue.url,
    id: issue.id,
    merged: false,
    draft: false,
    trusted: true,
  }))
}

// linkName names a link as the card it is on reads it: "#14" in the card's own repository,
// "other-repo#14" in another of the same owner, and "owner/name#14" elsewhere.
export function linkName(link: { repo: string; number: number }, from: string): string {
  const number = `#${String(link.number)}`
  const [owner = '', name = ''] = link.repo.split('/')
  const [fromOwner = ''] = from.split('/')

  if (link.repo.toLowerCase() === from.toLowerCase()) {
    return number
  }

  return owner.toLowerCase() === fromOwner.toLowerCase() ? `${name}${number}` : `${link.repo}${number}`
}

// linkWords say what a link is to the card it is on, for its tooltip and screen readers:
// "Fix in progress: #14 Title" or "Fix merged: #14 Title" on an issue's card, with
// "(draft)" for a draft and "from outside the project" for one the card doesn't trust, and
// "Closes #9 Title once merged" on a pull request's.
export function linkWords(link: CardLink, from: string): string {
  const named = `${linkName(link, from)} ${link.title}`

  if (link.kind === 'issue') {
    return `Closes ${named} once merged`
  }

  const words = `${link.merged ? 'Fix merged' : 'Fix in progress'}: ${named}${link.draft ? ' (draft)' : ''}`

  return link.trusted ? words : `${words}, from outside the project`
}

// CardLinks are the inbox's links around its cards: the index of its lists, and reveal,
// which shows a linked card in the inbox. Without them, as outside the inbox, an issue's
// card links its closing pull requests to GitHub, and a pull request's links nothing.
export interface CardLinks {
  index: LinkIndex
  reveal: (id: number) => void
}

export const CardLinksContext = createContext<CardLinks | null>(null)
