# First-run setup

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/setup`](../architecture/setup.md)

On a fresh database, as on a new install, after a [reset](starting-over.md), or in a
[sandbox](../development/build-targets.md#a-sandbox-on-fresh-data), the app opens on its setup, over the whole window: a few
steps that take you from the first launch to a filled inbox and a working agent launch. An
install from before the setup, whose inbox ever signed in, is adopted as done once, at
startup, and never sees it. The top bar says where you are (**Step 2 of 5 · 3 to go**), the
rail beside the step lists every step, and each one you have reached is a way back to it.
Every choice is saved as you make it, through the same setters Settings uses, and so is the
step you are on: quitting part way through opens the setup there again. Enter continues
while focus isn't in a field or on a control; Escape never skips.

| Step | What it does |
|---|---|
| **Welcome** | Picks the theme, and offers to restore a [backup](backing-up.md), as on a new Mac. After a reset it says what came back; run again from Settings, it welcomes you back |
| **Connect GitHub** | Checks the GitHub CLI's sign-in, one GraphQL point, as the step opens, on **Check again**, and whenever the window comes back to the front: gh missing (`brew install gh`), signed out (`gh auth login`), missing scopes (`gh auth refresh -s repo,read:org`), a refused token, or GitHub out of reach, each with its command to copy; once ready, the account, its scopes, and whether it reads projects. The first time it reads ready, the inbox and your GitHub notifications start filling, so they are there by the end |
| **Find your clones** | The clone root as it reads now (`~/projects` until the user picks another), and every other folder in the home folder that holds clones, whatever its name, one level down too (`~/src/<owner>/<repo>`), with GOPATH's and ghq's layouts. Each folder can be added beside the clone root, or, while the root is missing or holds no clones, used in its place. The look never reads a hidden folder, a link, Desktop, Documents, Downloads, Library, or a folder another disk is mounted on (a share, a cloud drive, a virtual machine's files), so it never makes macOS ask or waits on a network. **Look in ~/Documents/GitHub**, GitHub Desktop's folder, looks there only when the user clicks it, since macOS asks first, even to find that the folder isn't there; a refusal says where to allow it. With no clones found, **Start fresh** makes the clone root. Any other folder can be chosen, and the other repository locations are folded away below |
| **Pick your agent** | Each coding agent with its version, or where to get it; its profiles, and the command that signs one in; the default, the profile a launch runs with, the terminal (iTerm2, Warp, or Terminal, installed ones only: a terminal that isn't installed moves to the first one that is, in that order, unless you chose it here; choosing Warp says to finish its welcome first), and whether each session gets its own worktree |
| **Stay in the loop** | Turns banners on with a sample, which makes macOS ask once; the agent sessions' banners; the menu bar icon; and the mirror of your GitHub notifications, and clearing them |
| **Add your tools** | Only when it fits: mage-x or go-pre-commit is installed, or a clone uses one of them, runs go-fortress, or is a Go module (a few fixed paths in each clone, looked at with `lstat` alone). It says why it is suggested, shows each tool's card as Settings → Integrations does, and is always skippable |
| **Finish** | Throws confetti (unless [Celebrations](inbox-nil.md) is quiet or off), prints a `go test`-style log of the setup with a **Fix** beside whatever needs attention, previews the inbox, shows [how to read a card](inbox-cards.md) (a sample card and the key to its colors and symbols), and opens it |

macOS asks to allow banners only once the setup ends, or from its Notifications step, never
the moment the window opens. **Skip setup** ends it at any step. **Settings → About → Run
setup again** opens it from Welcome, every step showing what is set already, so it doubles as
a check of your setup.
