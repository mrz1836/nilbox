# Runs

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/scriptrun`](../architecture/scriptrun.md)

Releases, hook changes, pre-commit checks, scripts, a clone's pull, and a tool updating itself
all go through one runner, which keeps a record of each run and its whole output.

- **In the app or in your terminal.** A run goes in the app by default; **Runs in** under Settings →
  Integrations sends runs to your terminal instead. In the app, its output is written as it comes
  into its own log in `script-runs/` in the data folder, readable only by you, and the window reads
  it from there. A run in your terminal (the one chosen in Settings → Launching) gets a small
  script in the launch folder, as a session does. The script runs the command under `script(1)`,
  so it has a real terminal (colors, a passphrase prompt) while its output is copied into the same
  log, and leaves the shell's process ID and the command's exit status beside it, which is how the
  app follows a run it didn't start itself.
- **One run per clone, and none queued.** A run holds its clone while it goes. A second run in that
  clone, or a launch that needs a new worktree there, fails at once and names the run, and the
  worktree cleanup skips the clone until its next pass. A tool updating itself holds the folder
  its copy is in the same way, as does the app installing a tool there, so a second update or an
  install there fails at once too. At most four runs go in the app at once, and a release never
  waits for one of those slots.
- **Limits and stopping.** In the app, a release stops after 5 minutes, a hooks action after 1,
  checks after 30, a pull after 30, and a tool's update after 10; a script, and any run in your
  terminal, has no limit. **Stop** sends the run's whole process group
  Control-C (SIGINT), then SIGTERM after 5 seconds, then SIGKILL after 10 more. A release's Stop is
  off while magex switches to the branch it releases and pulls (*Stopping now could leave main
  partly updated*, or, with a magex before 1.30.2, *Stopping now could leave a rebase in
  progress*). Once magex has made the tag, Stop asks first, since stopping can leave the tag on
  your Mac but not on GitHub, and so it does while magex has your clone on another branch
  (*Stopping now can leave your clone on main.*). A pull's Stop is off for the whole pull.
- **Quitting while runs are going.** Quitting (the app menu, ⌘Q, or the menu bar's Quit) while a
  run goes in the app brings the window forward and asks first: **Keep running** or **Stop and
  quit**. Stop and quit sends each run SIGTERM, then SIGKILL after 3 seconds, and records it as
  stopped before the app closes. A release whose magex is switching branches or pulling gets
  SIGTERM only once that ends, within the same 3 seconds; one still pulling then is left to
  finish, unsignaled, and the next start follows it. The question says so for such a release:
  stopping waits up to a few seconds, and the release may finish after the app closes. A
  release the app hasn't finished checking on GitHub by then stays recorded as going, and the
  next start checks it. Runs in your terminal are left running. Autopilot's agents working in
  the background are listed beside the runs (*2 agents are still working.*), and while every one
  of them keeps to a budget of its own (Claude Code's), a quiet **Let them finish** in the
  question quits and leaves them working: they stay recorded as going, the next start follows
  each again, and one found past its time limit then is stopped at once. Any other run stops as
  with **Stop and quit**, and an agent that keeps to no budget of its own (Codex) stops with the
  app, which the question says. While the app moves itself to
  Applications, a quit waits for the move instead, then goes ahead
  ([Moving to Applications](updates.md#moving-to-applications)). Closing the window quits the same way,
  and asks the same question in the window, which stays open for it, unless the app keeps
  running on a close ([Closing the window](menu-bar.md)): then the window only
  hides, and nothing is asked. A logout, a restart, or a shutdown asks the app to quit too,
  so while a run goes in the app, macOS cancels it and names the app, as Terminal does for a
  running command; answer the question, then try again.
- **After a crash or a restart.** Once the window paints, the app checks every run still recorded
  as going. A run in the app whose process group is still alive, and started when the record says,
  shows *Still running from before Nilbox restarted*: its log is followed again, and **Stop** works.
  One that's gone shows *Nilbox closed while this was running*. A run in your terminal is followed
  again if it's still going, finished with its exit status if it wrote one, or shows *The terminal
  closed while this was running*. A release among them is checked as one that just ended is: its
  log is read again, and GitHub is asked whether its tag is there. So is a hooks action, whose
  result is read from the hook files against the hooks it started from, and a checks run, whose
  result names the files the checks changed, from git's status of the clone against the one
  taken as they started.
- **The Runs page.** Scripts → **Runs** lists every run the history keeps, newest first, and lists
  them again as a run starts, changes, or ends. Each row shows the run's status (**Running**,
  **Done**, **Failed**, **Stopped**, **Interrupted**, or **Orphaned**), its title, and when it
  started (hover for the exact time); on the line below, its repository, or its folder when that
  isn't one, **In Nilbox** or in the terminal chosen in Settings → Launching, how long it took once
  it ended; under that, its result. Click a row to open the run in its panel beside the list. The
  buttons above the list pick the kind of run it shows: **All**, **Release**, **Hooks** (hooks
  actions and checks), **Scripts** (scripts and pulls), or **Updates** (a tool updating itself).
  **Search repositories…** keeps the runs whose `owner/name` holds what you type, in any case, so it
  never finds a run in a folder that isn't a repository. Both filter what the page has listed,
  asking nothing again. A routine's agents in the background are never listed here ([Agents in the
  background](autopilot/agents-in-the-background.md)).
- **Clear history**, beside the page's title, is off until a run has ended. It asks first,
  **Clear the run history?**: *This forgets every run that has ended, with its log. Runs still
  going stay.* Its **Clear history** forgets every ended run, whatever the filter shows, and
  closes the panel unless the run in it is still going. The agents' runs stay: **Clear
  history…** in [Settings → Autopilot](autopilot/settings.md) forgets those.
- **The run panel.** A run's panel shows its status, its result, and its log as it comes
  (colors kept, the newest 2,000 lines, earlier output on request). **Open in terminal** opens
  your terminal in the run's folder and runs nothing there; **Show the log file** shows the log
  in Finder; **Copy log** copies the log, and **Run again**, where the kind allows it, works the
  command out afresh and starts it. **Stop** shows while a run goes in the app: it is off, with
  the reason beside it, while the run's kind refuses it (a release while magex switches branches
  and pulls, a pull for its whole run), and asks
  first, in the kind's own words, while stopping would leave something behind. A run in your
  terminal has no **Stop**: the panel says to press Control-C there.
- **A run that fails tells you.** Once a run's end is recorded as failed, or as stopped at its
  time limit, the [notification history](menu-bar.md) gets one **Run failed**
  entry for it, with a banner while that kind is on: *Checks failed: Checks · all files* under
  its repository, then the run's result (*4 passed, 1 failed: whitespace.*) or *Timed out
  after 30 minutes.* A run in a folder that isn't a GitHub clone, as a tool's update is, names
  the folder in the title instead. It covers every kind of run, in the app or in your
  terminal, and one from before a restart that the app settles as failed once it starts again.
  A run that couldn't start (the window already says why), one you stopped, from the app or in
  your terminal (Control-C, or closing its window or tab), one a quit or a restart interrupted,
  and one that succeeded tell nothing, and no run tells twice. Failures that come within 2
  seconds of each other, as **Install and update all** or a restart can make, show their
  banners together, and more than three as one that sums them up; the history keeps each.
  Clicking the entry or its banner opens the run here, in Runs, while the history still holds
  it: once it doesn't, as after **Clear history**, a toast says so.
- **History.** The newest 200 runs are kept, with up to 512 MB of logs in all; older runs go with
  their logs, and a run still going is never dropped. A finished log over 16 MB keeps its first
  4 MB and its last 12 MB.
- **What's logged.** The app log gets one line per run as it ends: its kind, repository, status,
  exit code, and how many warnings and unrecognized lines it had. Never the output, and never an
  environment value.
