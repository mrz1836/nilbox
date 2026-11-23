# Use Nilbox in your own Claude Code

Describe it also works from a Claude Code session of your own, in a terminal or an editor.
**Settings → Autopilot → Use Nilbox in your own Claude Code** lists each of Claude Code's
profiles with the plugin's state there (*Installed · 1.0.0*, *Not installed*) and **Install** or
**Remove**:

- **Install** writes the plugin into Nilbox's data folder (`describe/plugin`, a local
  marketplace of its own holding the plugin's manifest, its skill, and its MCP server, and no
  settings, hooks, or permission rules), points the program it runs (`describe/bin/nilbox-mcp`)
  at the app, and runs Claude Code's own `claude plugin install` for the profile, which needs
  no sign-in, so a profile not signed in yet can have it too. Claude Code
  loads the plugin in place from that folder, which each version of Nilbox rewrites as it starts
  (or once Autopilot is turned on) when its words differ, so an update reaches your next session
  with nothing to install again.
  Only Nilbox in a place that lasts, such as Applications, can point the plugin at itself: a
  build from source, or a copy macOS moved aside, says so and installs nothing, unless a release
  already did.
- **Remove** runs `claude plugin uninstall`, then removes the plugin's marketplace from the
  profile.

In a session of that profile, ask for what you want Autopilot to do. The plugin's skill
(`autopilot`, of the plugin `nilbox`) takes the request up, and its tools read how Autopilot
works and your setup, check a proposal, and propose it. Each of their calls asks you first,
since the plugin brings no permission rule; allow them in your own settings if you like. The
proposal waits at the top of Describe it, under **From your Claude Code**, as a card like any
other, with where and when it came from (*A session in example-repo, 5m ago*) and **Discard**
for every part left, and the count beside **Describe it** says how many wait. The session hears
what you did with its proposals through the plugin's `autopilot_proposal_status`. A card shows
for a day after you dealt with every part, and Nilbox removes its file as it starts once 30 days
have passed; at most 50 wait at once, and past that a session is told to ask you to deal with
some first.

A session there sees less than Describe it in the app, since it may run in a repository you
don't trust, holding tools that could send what it reads anywhere: of your setup, only your
routines' names, sentences, switches, and modes, your prompts' nicknames and what they are for,
and Autopilot's settings; never your routines' definitions, your scripts, profiles, or rules,
nor any name in your inbox. Its proposals are still checked against your whole setup, as their
cards are. Claude Code starts the plugin's server, Nilbox's own binary in its tool mode, with
every session of the profile; it reads nothing but the snapshot Nilbox writes as your setup
changes and the proposals' folder, and writes nothing but proposals. The snapshot follows your
routines, Autopilot's settings, your pinned and muted repositories, and your inbox as they
change; a change to your prompts, your scripts, the trusted bots, or the ignored workflows
reaches it with the next refresh of your inbox.
