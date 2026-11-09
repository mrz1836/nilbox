// The worktrees the inbox's rows show: each item's worktree, from the Worktrees view's
// latest listing, which the window keeps. Nothing is listed for the inbox alone, since a
// listing runs git in every clone and asks GitHub about the worktrees' items.

import type { InboxItem, Worktree, WorktreesSnapshot } from '../platform'

// noWorktrees is the empty map, shared so it stays the same object.
export const noWorktrees: ReadonlyMap<string, Worktree> = new Map()

// itemKey identifies an item across the inbox and the Worktrees view: its repository,
// ignoring case, and its number.
export function itemKey(repo: string, number: number): string {
  return `${repo.toLowerCase()}#${String(number)}`
}

// itemWorktrees maps each item a listed worktree is linked to, by itemKey, to the
// worktree; the first listed wins when an item has two.
export function itemWorktrees(snapshot: WorktreesSnapshot | null): ReadonlyMap<string, Worktree> {
  if (snapshot === null) {
    return noWorktrees
  }

  const out = new Map<string, Worktree>()

  for (const repo of snapshot.repos) {
    for (const wt of repo.worktrees) {
      if (wt.item === null || wt.main || wt.prunable) {
        continue
      }

      const key = itemKey(`${wt.item.owner}/${wt.item.name}`, wt.item.number)

      if (!out.has(key)) {
        out.set(key, wt)
      }
    }
  }

  return out
}

// worktreeOf returns the item's worktree, if a listing found one.
export function worktreeOf(worktrees: ReadonlyMap<string, Worktree>, item: InboxItem): Worktree | undefined {
  return worktrees.get(itemKey(item.repo, item.number))
}
