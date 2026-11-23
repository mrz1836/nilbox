# The inspector

[Docs](../README.md) › [Development](../README.md#development) · See also: [`internal/devtools`](../architecture/devtools.md), [Build targets](build-targets.md)

A build from source carries an inspector, so you, or an agent helping you, can look inside the
app while it runs, from the terminal: Nilbox Dev (`magex app:build`, `app:run`, `app:fresh`)
and `magex app:dev` alike. A release never does, nor a rehearsal: magex compiles it in only with
the `devtools` build tag, which it passes to a build from source alone, and a guard test fails
on any file that would take it into a release (`internal/devtools`).

From the moment the app starts, the inspector keeps the log, every record from Debug up whatever
the log file keeps, and the events the services emit. Once the window opens, a small agent it
runs in the page on every load keeps the page's console and its uncaught errors, and the trail
of what you do there: each click, key, field typed in (never what you type), and choice
changed, with the card, dialog, or list it happened in. So when something goes wrong, there's
no need to retrace the steps: `magex inspect:trail` already has them.

When something in the window looks wrong, you can point at it instead of describing it. Press
⌥⌘P, or choose **Point at an Element** in the menu bar's menu (or run `magex inspect:point`),
and the window outlines whatever the pointer is over and names it. ↑ outlines the element
around it, and ↓ goes back in, so a whole row or section is as easy to point at as a button.
Click it (the app doesn't act on the click), type what's wrong in the box that opens, and press
Enter; click another element for another note. Escape drops the note you're writing, then stops
pointing, and leaves an open sheet or dialog open. Each note keeps your words, what you clicked
and where (the card, dialog, and list), the view, its styles that are set, the text around it,
and a picture of the window with the element boxed; under `magex app:dev` it also keeps the
React components that render the element and the source file that wrote it. `magex
inspect:notes` reads the open ones back, and each note shows on the trail too, in order with
your clicks.

Notes are kept, with their pictures, in `devtools/` in the data folder, so they outlive a
restart, until they are cleared: with **Clear notes** in Settings → About → Inspector, or
`magex inspect:notes clear=true`. Whoever fixes one marks it done (`inspect:notes done=3`), and
the badge at the bottom of the window counts the notes still open while you point. The
**Point at elements** switch beside **Clear notes** turns pointing off: ⌥⌘P is then the app's
again, and the menu bar's item goes. Only a build from source has any of this: a release runs
no inspector, so About shows no Inspector there.

It answers on a socket in the data folder (`devtools.sock`), which only your account can open,
and sends nothing anywhere. It reads the database read-only: a statement that would write is
refused, and the connection can't write besides.

| Target | What it shows |
|---|---|
| `magex inspect:status` | The build, the process and its Go runtime, the data's paths, the window, the page, and how much each feed keeps |
| `magex inspect:logs [n=] [level=] [grep=] [follow=true]` | The log, the last `n` (100) records at `level` and up holding `grep`; `follow=true` keeps printing until interrupted |
| `magex inspect:events`, `inspect:console`, `inspect:trail` | The services' events, the page's console, and your trail, read the same way |
| `magex inspect:watch [level=] [grep=]` | The log, the events, the console, and the trail together, live, until interrupted |
| `magex inspect:point [on=false]` | Turns pointing on in the window, as ⌥⌘P does (not while it is turned off in Settings); `on=false` turns it off |
| `magex inspect:notes [n=] [all=true] [follow=true] [wait=true]` | The open notes you left by pointing, each picture saved as `bin/inspect/note-<n>.png` with the element boxed; `all=true` shows the done ones too, `follow=true` keeps printing each new note until interrupted, and `wait=true` waits for the next note, prints it, and returns |
| `magex inspect:notes done=2,3` or `done=all`, `clear=true` | Marks notes done, or empties the list |
| `magex inspect:sql q="SELECT …" [max=]` | One statement that reads, against the live database, at most `max` (200) rows; a long value is cut short (`json=true` prints it whole) |
| `magex inspect:tables` | The database's tables and their row counts |
| `magex inspect:js code="…"` or `file=probe.js` | Runs JavaScript in the window's page and prints what it returns, once a promise settles: an expression, or statements that `return`. `nb` holds helpers: `nb.$`, `nb.$$`, `nb.find`, `nb.describe`, `nb.dom`, `nb.text`, `nb.cards`, `nb.click`, `nb.type`, `nb.press`, `nb.scroll`, `nb.wait`, and `nb.waitFor`, each taking a selector or the name a control shows |
| `magex inspect:dom sel="…" [styles=true] [html=true]` | The elements a selector matches as the web view lays them out: where each is, whether it is on screen and drawn, and its computed styles and HTML when asked |
| `magex inspect:cards` | The inbox's cards: each one's list, rule, and place, and whether the web view draws it and what is inside it |
| `magex inspect:click target="…"` | Clicks a control, by selector or by the name it shows |
| `magex inspect:shot [out=]` | Saves a picture of the window as its web view draws it, to `out` or `bin/inspect/shot-<time>.png` |
| `magex inspect:stacks [debug=2]` | Every goroutine's stack |
| `magex inspect:reload` | Reloads the page |
| `magex inspect:help` | Lists what the inspector does |

Each target finds the app by your own data folder; `data=<folder>` names a sandbox's instead,
as `app:fresh` and `app:dev fresh=true` make. `json=true` prints the answer's lines as JSON.
A script that holds `$`, or runs long, reads better from a file with `file=`.
