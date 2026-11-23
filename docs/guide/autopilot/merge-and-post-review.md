# Merge it and Post the agent's review

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Merge it and Post the agent's review](../../architecture/autopilot/merge-and-review.md)

Two steps act on what a run came to, and only if you add them. Both are pull requests' alone,
go through the review sheet (read from GitHub right before, one point, and sent only as the
sheet offers it then, at the [pace](changes-on-github.md) of the GitHub
actions, counting toward **GitHub actions a day**), and are pinned to a commit: a push since
sends nothing. In Ask first, each asks again once it is reached, showing what the run came to,
whatever your OK to start the routine.

**Merge it** comes after a **Wait for the checks** (*only if the checks passed*, as **+ Add a
step** sets it), and merges only the commit the checks passed on. Or, in a routine with no
agent step that fires when **Checks pass**, it merges the commit the checks passed on, after
any steps before it that don't change the branch (a reply or labels, say, never **Ask
Dependabot to rebase**, **Update branch**, or a script). Such a routine acts once per head
commit, and **+ Add a step** offers Merge it there with no *only if*. Right before it merges,
the routine is decided again on what the item's read found: a push since closes the match as
moved on, a condition that no longer holds (a label taken off) as no longer matching, and
checks running again wait. A run you start by hand (**Run anyway**) never merges in such a
routine: there was no pass to merge.

- **How:** *auto-merge if it was on, else when GitHub allows* (where it starts) turns GitHub's
  auto-merge back on when it was on as the routine matched, as a push to the branch turns it
  off; else, and with *when GitHub allows*, it merges at once, but only when GitHub says it
  would merge now. **Method:** the repository's own, a merge commit, squash, or rebase.
- It merges past branch protection in one case alone, below; otherwise never, nor as an admin.
  When GitHub holds the merge for an approving review, it waits in [Needs you](needs-you.md)
  (*GitHub wants an approving review before it merges this: approve it, then let the merge
  go.*) with **Approve and merge…**, which opens the review sheet for you to approve, and
  **Merge**, which tries again. Your own pull request, which GitHub never lets you approve,
  closes instead, as no longer offered.
- It never merges where the agent's commits changed workflow, agent, or tooling files, or
  where not every file they changed could be checked, *Its agent's commits changed workflow
  files, so it isn't merged: a person reviews those.*, nor an outside contributor's pull
  request.

**Merge past the rules when an approval is all it lacks** is a tick on a Merge it with no agent
step, in a routine that fires when **Checks pass**, merging *when GitHub allows*. It is for your
own pull requests that nobody else reviews, such as a sync a tool opens in your name: on a
repository whose rules want an approving review, the merge waits for one that never comes, and
the review sheet's own [Merge past the rules](../reviewing-a-pull-request.md) is what you'd click by
hand. With the tick, the routine merges it so with no click, once three things are yours:

1. **Let routines merge your own pull requests past the rules** is on in **Settings →
   Autopilot** (off where it starts). It is never in a backup, Describe it can't propose it,
   and turning it off stops every such merge at once. While it is off, the builder and
   Describe it's card warn on the step, it merges only what GitHub allows, and the routine's
   line says so in red, its pane with **Open Autopilot settings**. A pull request that needs
   to go past the rules waits in **Needs you** meanwhile, at the merge; turn the switch on and
   it merges by itself, without sending again what the steps before the merge sent.
2. You confirm the routine when you save it, in the builder or from Describe it's card: Save
   lists exactly what it may do, a box to tick for each tick, and saves only once they are.
   That confirms the routine as it is: any change to what it does asks again (a new name
   doesn't), and so does a copy, an import, a restored routine, or one a backup brings back.
3. **Also when it changes workflow, agent, or tooling files**, a second tick, for a pull
   request that changes [sensitive files](../sensitive-files.md) too; without it, such a pull request
   isn't merged past the rules.

Each time, right before it merges, it reads the pull request again (one GraphQL point, with the
merge state) and goes only if every one of these holds:

- It is your own pull request, at the commit the routine's checks passed on, and no routine's
  run pushed to it. Every one of its commits, at most 100, is signed, with a signature GitHub
  verified, by you: anyone can write your name on a commit, but not sign it with your key.
- GitHub holds it back (BLOCKED) only for an approving review: no request for changes, not
  behind its base, no conflicts, and GitHub lets you merge past the rules, as it lets a
  repository admin.
- Every check on the head passed, each check its branch requires among them, by name, and no
  review thread is left unresolved.
- Each rule on its base branch is one a routine can account for: the pull request rule it goes
  past, required checks and signatures (checked above), no force pushes, no deletion, the
  rules about files that pushes keep to, Copilot's automatic review, and a linear history for
  squash or rebase. Any other (a merge queue, required deployments, a locked branch, one it
  can't read) stops it.
- The merge warns of nothing but the approval and, with the second tick, the sensitive files.

When all hold it squash-merges (or merges with the step's method) at the head, and Activity
says *Squash-merged #12 past the rules: it lacked only an approval from someone else.* When
GitHub would merge it anyway, it merges as any Merge it does. While GitHub still works out the
merge state, or the checks still report, it tries again on a later cycle. While the switch is
off it waits for it, as above. Anything else closes the match as no longer offered, saying why (*Its commit 4f2a9c1 wasn't signed by you.*):
nothing ever waits for your click. In Ask first, the step asks before it merges, as Merge it
always does. A GitHub admin merge has the same moment between its read and its merge as the
review sheet's own: a push in that second is refused by GitHub, which merges only the head the
routine named.

**Post the agent's review** comes after an agent step in **Read only**, and posts what the
agent wrote in its report (its verdict, its text, and its line comments) at the commit the
agent read.

- **As a comment** posts it as a review that only comments, whatever its verdict. **As its
  verdict** posts a request for changes as one, and an approval only with **Allow approving**
  ticked (off where it starts).
- A routine approves only a member's or a trusted bot's pull request, only when every file was
  checked and none is [sensitive](../sensitive-files.md), and never one a routine pushed to: an
  approval held back posts as a comment, saying why (*Posted as a comment: a routine never
  approves a pull request a routine pushed to.*).
- Each line comment is checked against the diff, as the sheet's are: one on a line the diff
  doesn't show is left out, and counted. An agent that wrote no review skips the step.
- An outside contributor's pull request waits for your OK in every mode, as a GitHub action
  does.

The ⚡ chip then says what went (**⚡ Merged**, **⚡ Auto-merge on**, **⚡ Approved**,
**⚡ Changes requested**, **⚡ Reviewed**). Nothing lets an agent merge, approve, or close
anything itself: these steps are the app's own writes, with the guards above in Go whatever
the routine says.
