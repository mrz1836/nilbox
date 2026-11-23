# Your shell's environment

An app started from the Finder, the Dock, or `open` gets only what launchd hands it: about
nine variables, a `PATH` of `/usr/bin:/bin:/usr/sbin:/sbin`, and launchd's own ssh agent. Your
terminal has much more: Homebrew, `~/.local/bin`, `~/go/bin`, the ssh agent or signing setup
your shell's startup files configure, and the rest. So, once the window has painted its first
frame (never before, so it can't slow the cold start), the app reads your login shell's
environment the way editors do: it runs `$SHELL -i -l -c` (or `/bin/zsh` when `$SHELL` is
unset or not listed in `/etc/shells`), in its own process group, with nothing on stdin and a
five-second limit, and keeps the variables it prints. On an M1 Max that took about
330 ms the first time and under 60 ms after.

- **What it's for.** It's the environment the app finds and runs your command-line tools with
  for Integrations (mage-x and go-pre-commit), so they're the same copies your terminal runs
  (see [Tools the app finds and installs](tools.md)). It matters for
  `git` too:
  Apple's `/usr/bin/git` and Homebrew's keep separate keychain entries, and Apple's can stop to
  ask for a keychain password in a window, which a run with no terminal can't answer.
- **When it can't be read** (the shell takes longer than five seconds, fails, or prints
  nothing usable), runs use the app's own environment, with Homebrew, `/usr/local/bin`,
  `~/.local/bin`, `~/bin`, and `~/go/bin` put before launchd's folders on `PATH`. The log says
  why the read failed.
- **What's kept, and where.** The variables stay in memory only. The app never writes them to
  a file or a script, and never logs one, not even its name (names can hold a project's
  name); the log records how long the read took and how many variables it found.
- **Your startup files** run as they do when you open a terminal. If they print a greeting,
  it's ignored. If one waits for input, the read gives up after five seconds.
