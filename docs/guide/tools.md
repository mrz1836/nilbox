# Tools the app finds and installs

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/integrations`](../architecture/integrations.md)

**Settings → Integrations** shows the command-line tools the app drives: mage-x's `magex`, which
releases your repositories, and `go-pre-commit`, which runs your pre-commit checks. For each one
it says which copy it uses, its version, and where it came from (a GitHub release, Homebrew,
`go install`, a development build, or the app itself).

- **Finding them.** After the first paint, the app looks on your shell's `PATH`, then in
  Homebrew, `/usr/local/bin`, `~/.local/bin`, `~/bin`, and go install's folder, and runs each
  copy's `--version`. The copy your terminal runs is the one it uses, unless you choose another
  under **Details**. It checks once a day, while you're signed in to GitHub, for a newer release.
  The app drives magex 1.29.5 or later and go-pre-commit 1.12.0 or later; an older copy's card
  says so (*magex 1.28.0 is older than 1.29.5, which Nilbox needs*), and the pages that run it
  ask for a newer one.
- **Updating.** A copy that can update itself does: **Update** runs the tool's own updater,
  `magex update:install` or `go-pre-commit update`, which downloads the latest release for your
  Mac, checks it against the release's SHA-256 checksums, and swaps it in for the copy with a
  rename. That's any copy from a GitHub release (or one the app installed) that isn't a link,
  isn't in a go install folder, sits in a folder you can write, and is new enough to have the
  updater (magex 1.26.0, go-pre-commit 1.10.0). The go install folders are `$GOBIN`, each
  `$GOPATH` folder's `bin`, and `~/go/bin`, from your shell's environment and, when `go` is on
  your `PATH`, from `go env` too (so `go env -w` settings count); if `go env` can't be read, no
  copy updates itself. It runs in the app, in the copy's folder, with your shell's environment;
  the card shows the run as it goes, and it's in **Runs** too. Once it ends, the app looks for
  the tools again, and only a newer version reads as *Updated magex to 1.30.0*; an updater that
  says the copy is already current reads as *magex is already up to date (1.30.0)*, which isn't
  an error. A copy the app installed stays *installed by Nilbox*. Since the tool replaces its
  own copy, the app doesn't ask whether to replace a copy it didn't install. An update still
  running from before the app restarted shows on the card too, and nothing installs the tool
  until it ends.
- **Installing.** **Install**, **Reinstall**, and an update of any other copy download the
  release archive for your Mac from the tool's GitHub release, check it against the release's
  SHA-256 checksums file and GitHub's own digest, try the program, and put it in
  `~/.local/bin` (or replace the copy you have in place). The download goes to a private folder
  in the app's data folder, and nothing else on your Mac changes until the program has run
  once. The program is swapped in with a rename, so your terminal never finds it missing, and
  if the new copy won't run, the old one is put back. While a tool updates itself, an install of
  it, or any install in that folder, fails at once and says why; try again once the update ends.
- **What the app never does.** It never runs `go install` or `sudo`, never edits your shell's
  startup files, and never replaces a Homebrew copy (it shows the `brew upgrade` command) or a
  link (such as a development build you linked into `~/.local/bin`). A copy it didn't install is
  replaced only after you say so, unless the tool updates itself. A copy in go install's folder
  stays where it is, and never runs its own updater, which would run `go install`: the new one goes
  in `~/.local/bin`, and if your terminal still finds the old one first, the card says so and
  offers to move it to the Trash, through Finder, so **Put Back** works. The first time, macOS
  asks whether the app may control Finder; if you say no, it offers **Reveal in Finder** instead.
  Under **Details**, a copy the app installed has **Reinstall** and **Move to the Trash**, and
  every other copy the card lists under **Other copies** (one the tool doesn't use, or one that
  won't run) has **Move to the Trash** too, after you confirm, except a link, which Finder would
  follow to what it points to, and a Homebrew copy, which Homebrew manages. While a run uses the
  tool, neither kind of update starts.
- **`~/.local/bin` and your `PATH`.** The app finds a tool it installed either way. If your shell's
  `PATH` doesn't include `~/.local/bin`, your terminal won't, and the card says so with the line to
  add to `~/.zprofile`: `export PATH="$HOME/.local/bin:$PATH"`. For go-pre-commit this matters
  most: its Git hook runs `go-pre-commit` from your `PATH`, so without that line, commits fail.
- **What the tools fetch themselves.** Until go-pre-commit 1.14.0, the first time its checks need
  gofumpt, golangci-lint, or gitleaks, go-pre-commit installs them itself with `go install` and
  `curl`; from 1.14.0 it checks those downloads against their checksums. The app doesn't install
  them, and can't check them for an older go-pre-commit.
