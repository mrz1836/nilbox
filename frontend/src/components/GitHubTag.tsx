import { tagText, tagTitle, type ThreadTag } from '../lib/githubThreads'

// GitHubTag marks a card whose item has unread notifications on GitHub: "2 unread on
// GitHub", with GitHub's reasons for notifying the user about it in its tooltip. It shows
// unread notifications alone, so an item with none unread shows nothing.
export function GitHubTag({ tag }: { tag: ThreadTag }) {
  if (tag.unread <= 0) {
    return null
  }

  return (
    <span title={tagTitle(tag)} className="shrink-0 text-[0.6875rem] leading-4 font-medium text-accent">
      {tagText(tag.unread)}
    </span>
  )
}
