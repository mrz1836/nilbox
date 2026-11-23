# internal/devtools

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/devtools`](../../internal/devtools) · See also: [The inspector](../development/inspector.md)

The inspector a build from source carries, which the `magex inspect:*` targets talk to
(`magefiles/inspect.go`; the user's side is in
[the inspector](../development/inspector.md)). It is plain Go, tested on Linux; the desktop side
is `cmd/nilbox/devtools_desktop.go` and, for the picture, `devtools_darwin.go`.

## Only builds from source

Every file that imports it builds only with the `devtools` tag, and magex passes that tag to a build
from source alone: `buildTags` in `magefiles/bundle.go` gives it to `app:dev` and to the bundle
`app:build` makes (no flavor), never to a release or a rehearsal, which the magefiles' tests pin,
and `TestOnlyBuildsFromSourceCarryTheInspector` (`internal/repoguard/devtools_test.go`) fails on a
file that imports it and builds without the tag. A release's `devKit`
(`cmd/nilbox/nodevtools_desktop.go`) does nothing, so the bootstrap reads the same either way.
`app:lint` and `app:test` lint and test the desktop code both ways.

## The socket

`Listen` listens on `devtools.sock` in the data folder, mode 0600, after removing a socket a run
left behind; one another running copy answers on is left alone (`ErrInUse`), and the inspector stays
off, which the log says. A connection carries one request, one JSON line (`Request`: a command and
string arguments, as magex passes them), and gets JSON lines back (`Line`), ending with `end` or
`error`. Each line is written within ten seconds, so a client that stops reading never holds the app
up, and a client closing its end ends a command that follows. It speaks no HTTP: only
`internal/github` imports `net/http`.

## What it keeps

`Feed` keeps the last entries of one kind and hands each new one to its followers, never waiting on
a slow one. The log is kept by `LogHandler`, which the bootstrap adds beside the app's handlers,
from Debug up whatever level they write at; the events by `Tee`, which wraps the relay the services
emit into; the page's console and the user's trail by the agent. `stream` sends a feed's last
entries, or several feeds' in time order (`watch`), then follows: it starts following before it
reads what is kept, so nothing between the two is missed.

## The page

`agent.js`, embedded, is the agent. The bootstrap runs it in the main window, the first the app
opens (`app.Window.OnCreate`), on every page load (`events.Common.WindowRuntimeReady`, then
`ExecJS`), and `Page.Eval` runs it again ahead of each evaluation, so a page that lost it gets it
back; it installs once. The agent posts to the app through the web view's message handler, each
message prefixed `devtools:` (`Prefix`); the app's `RawMessageHandler`, set only in a build with the
inspector, hands each message from the main frame to `Page.Receive`. An evaluation's code goes to
the agent as a JSON string with an ID; the agent compiles it as an expression, or else as
statements, runs it with its helpers as `nb`, and posts back the value, made safe for JSON (an
element as its description, a cycle cut short, depth and size capped), or what it threw. The trail
names a control by its label or text, anything else by its label alone or its text when it holds no
other element, and keystrokes in a field only as typing in it, once per pause, never the keys. A
card's description reads whether the web view draws it and what is inside it (`checkVisibility`),
which is what `content-visibility` once got wrong (index.css says why no inbox or notification row
uses it). The agent runs outside the frontend's sources, so
`frontend/src/test/devtoolsAgent.test.ts` runs it in jsdom.

## Pointing

⌥⌘P in the window, the menu bar's Point at an Element (`Page.StartPointing`), or `nb.point`
(`inspect:point`) turns pointing on. The agent's listeners are on the window in the capture phase,
so they hear every key and press before the page does, a sheet's `useModalDialog` (which listens on
the document) included: ⌥⌘P works over a sheet, and Escape, swallowed there, never closes one. While
pointing, the agent outlines the element under the pointer, or, with ↑ and ↓, one around it or back
in (kept while the pointer stays inside it), and swallows every press and click, so the app never
acts; a click opens a box for a note on what is outlined, and Enter posts a `note`. Everything it
draws is one element, the layer, appended inside the open dialog the pointer is over (else the last
`dialog:modal`, else the body) and shown as a `popover="manual"`: a modal dialog leaves the rest of
the page inert, and the top layer draws the popover over it. Every event from inside the layer, and
focus moving into it or back out, stops at the window, so the app hears neither the note's keys nor
focus leaving it. The note carries the trail's target and where, the element as `describe` has it,
its computed styles that are set, the text around it, the view (the `nav` items marked pressed or
current), and, when the fiber React keeps on the element has a `_debugOwner` (the development build
of `app:dev`), the component chain from the fibers' `return`s and the first `/src/` frame of
`_debugStack`, its line the compiled module's and so approximate. The agent hides the layer before
it reads the page and posts. `Page.Receive` numbers the note and puts it on the trail (kind `note`),
then, on a goroutine of its own, since the picture needs the main thread the message came on, takes
a picture with the shot `AttachShot` gave it, tells the agent to show itself again (`noted`), boxes
the element on it (`Outline`, scaled by the picture's width over the viewport's), and keeps the note
after the one before it.

## Notes and the off switch

`Notes` keeps the last 50 notes in `InspectorDir`'s `notes/`, each as `<n>.json` and `<n>.png`,
written through a file beside it and renamed, and reads them back as the app starts (`Page.Open`,
from `devKit.start`), so numbering goes on from the last one. A note is open until `inspect:notes
done=` marks it; `inspect:notes` shows the open ones, and `follow=true` streams each new one, which
is how an agent hears of a note as it is left. `settings.json` beside them keeps the one setting,
whether pointing may start. `Page.sync` tells the agent the state (`InspectorState`) as the page
loads and after each change, and `OnChange` tells the desktop side, which redraws the menu bar's
menu when the setting changes. While it is off, the agent leaves ⌥⌘P to the app and `inspect:point`
refuses (`ErrPointingOff`). The window's Settings reach the agent, not Go: `platform.inspector()`
returns `window.__appDevtools.inspector`, whose choices the agent posts (`inspector` messages), and
`InspectorCard` in About renders only when it is there. A release has no agent, so the card renders
nothing and adds no listener, its `devKit.menu` and `redrawMenu` do nothing, and it writes no
`devtools/`. "Find a setting" leaves the card out, since its index ships in a release.

## The picture

`devKit.shot` has the web view take a picture of what it draws (`WKWebView
takeSnapshotWithConfiguration`), as PNG, on the main thread, and waits for it on a thread of its
own; one that takes longer than the request allows is left to finish.

## The database

`store.ReadOnlyQuery` and `store.Tables` read the live database over a connection of their own,
`mode=ro` and `query_only`, and run one statement that reads (SELECT, WITH, EXPLAIN, VALUES, or a
PRAGMA that sets nothing): the one way a package other than a table's owner reads a table, for a
build from source alone, and never to write.

## Trust

The socket is the user's alone, and anything the user runs could already read the data folder. But a
script the inspector runs can do whatever the window can, writes to GitHub through the bound
services among them, which is why only a build from source carries it.
