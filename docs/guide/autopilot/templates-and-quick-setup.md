# Templates and quick setup

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Easy mode: templates, from an item, why, export and import, graduating](../../architecture/autopilot/easy-mode.md)

This is Autopilot's easy mode: a routine from a template takes a few blanks and a click, and
opens in the full builder whenever you want more. **Templates** lists routines made for you to
start from, each with its icon, one line on what it does, and the mode it suggests. Those that
fit what Nilbox already knows come first, under **Suggested for you**, each with why: *3 of your
clones run go-fortress’s security checks.* (from the clones the first-run setup looks at),
*Dependabot opened 9 pull requests in your inbox this week.*, *Bots opened 4 pull requests this
week that ask for your review or are assigned to you.* (from the stored items). Nothing is read
for them that isn't on this Mac already.

| Template | What it does | Suggests |
|---|---|---|
| 🛡️ **Fix Dependabot security failures** | When a Dependabot update fails a security scan (a failed check's name holds security, codeql, govulncheck, gitleaks, nancy, or osv), runs **Fix dep security** in the background, pushes once to the pull request's branch, waits for the checks, and tells you how it went | Ask first |
| 🩹 **Heal Dependabot PRs, then merge them** | When a patch or minor Dependabot update fails its checks, runs **Heal Dependabot PR**, waits for the checks, merges inside GitHub's rules if they pass, and tells you if they still fail | Ask first |
| 👥 **Review members' PRs** | When a member opens a pull request under 400 lines with no sensitive file, runs **Review** read only, and posts its verdict | Ask first |
| ⏾ **Snooze passing bot PRs** | A trusted bot's patch or minor update whose checks pass, asking for your review or assigned to you, is snoozed for 3 days | Autopilot |
| 🔔 **Tell me about security failures** | A notification when a Dependabot update fails a security scan | Autopilot |
| 💬 **Address requested changes** | When a reviewer asks for changes on your pull request, runs **Address review** in your terminal | Ask first |
| 🔀 **Resolve merge conflicts on my PRs** | When your pull request conflicts with its base, runs **Resolve conflicts**, which merges the base in (never a rebase) | Ask first |
| 🔁 **Re-run flaky checks once** | When a check you name as flaky fails, runs **Re-run failed jobs** (GitHub tasks only), once a commit | Autopilot |
| ↻ **Ask Dependabot to rebase what conflicts** | Comments *@dependabot rebase* on a conflicting Dependabot update, once a commit | Autopilot |
| 🏷️ **Ask for a repro on quiet bugs** | When a bug assigned to you goes quiet for 14 days with no needs-repro label, comments your **Needs repro** reply and adds the label | Ask first |
| ⬆️ **Update my PRs when they fall behind** | Updates the branch of your pull request that fell behind, where the repository allows it | Ask first |
| 🔧 **Fix failing checks on my PRs** | When your pull request's checks fail, runs **Fix checks** in the background, waits for the checks, and tells you how it went | Ask first |

The fixed tools presets keep a background agent out of agent, workflow, and tooling files,
`package.json` among them ([fences](fences.md)), so a fix that
needs one (a JavaScript dependency's, say) pushes nothing and says what a person must change;
the templates' descriptions say so, and the fence is never widened for them.

## Quick setup

Picking a template opens a short form, not the whole builder: only the template's blanks (for most,
**Where**: your account, and the orgs, users, or repositories to watch too; for Re-run flaky checks
once, the checks' names, picked from those seen on your pull requests), the mode it starts in (the
one new routines start in, Ask first unless you changed it; the template's own suggestion shows
beside it as a hint, never as a change), the sentence it reads as, and **This Mac**: what this Mac
lacks for it (its agent, its profile's sign-in, gh), checked from what the checks last found, never
asking GitHub. **Test** tries it on your items, as the builder's does. **Save** makes the routine,
switched off, named after the template ("<name> 2" when you have one of that name), and
**Customize** opens the full builder with everything filled in. A template whose starter prompt you
deleted says so, with **Add it again**, which puts the starter back in your prompts as it ships.

**Make a routine from this…** on an item's **⋯** menu (**Autopilot ▸**) opens the builder filled
in from the item, saving nothing: its kind; its author, or its bot; its repository's owner (your
account for your own); **Checks fail** when its checks fail, with the failed checks' names as
one *contains any of* condition, else **Merge conflicts** when it conflicts, else **Changes
requested** when that is the review decision, else **Appears**; and the prompt a click would
launch on it, run in the background, in the mode new routines start in. Its labels and its
dependency update's type wait as **Suggested by the item:** buttons under **If**, one click
each, and **Test** shows the item at the top, with whether the routine matches it now, until a
test reads more.

The first-run setup's last step suggests one template, the first that fits what this Mac knows,
while Autopilot (beta) is on: *Suggested for you: Fix Dependabot security failures*, with why and
**Set it up**, which ends the setup and opens its quick setup.
