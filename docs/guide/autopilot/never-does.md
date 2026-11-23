# What Autopilot never does

- It never changes a routine's mode on its own, and never runs while **Autopilot (beta)** is
  off.
- A local step never replaces what you set aside yourself, and has **Undo**. Dry run changes
  nothing.
- It changes on GitHub only the [five things](changes-on-github.md) the sheets
  already change, and merges or posts an agent's review only through the opt-in
  [steps that follow a run](merge-and-post-review.md), inside GitHub's own rules.
  It never closes anything or approves workflow runs, and a test fails the build if any of its
  code could; nor does it approve a pull request a routine pushed to, or anything at all but
  through Post the agent's review.
- It merges past branch protection only your own pull request whose every commit you signed,
  that lacks only an approval from someone else, through a Merge it with no agent step that
  allows it, in a routine you confirmed as it is, while **Let routines merge your own pull
  requests past the rules** is on; never anyone else's, never one an agent or a routine
  pushed to, and never a run you started by hand. No agent can merge at all.
- It never starts an agent on an outside contributor's item unless its org or repository
  allows it, and then read only; never runs a pull request you didn't open that changes agent
  configuration without your OK; and never pushes but once, to the pull request's own branch,
  never with `--force` ([fences](fences.md)).
- Nothing it reads or decides leaves your Mac: it reads and writes GitHub through the app's
  own API calls, as the inbox and the sheets do, and its agents send what they read only to
  their own model provider, as a session you open does. The one exception is
  [Describe it](describe-it.md), and only when you use it: Claude Code reads your words, how
  Autopilot works, your Autopilot setup, and the names in your inbox, never an item's title,
  text, comments, diff, or code, nor a prompt's text, and sends them only to Anthropic. A
  session in [your own Claude Code](your-own-claude-code.md) sees less (none of
  your inbox's names, and of your setup only its routines' names and sentences, its prompts'
  nicknames, and Autopilot's settings), and asks before each of the plugin's tool calls.
