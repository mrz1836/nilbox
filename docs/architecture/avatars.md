# internal/avatars

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/avatars`](../../internal/avatars) · Guide: [Inbox cards](../guide/inbox-cards.md), [Reviewing a pull request](../guide/reviewing-a-pull-request.md)

`internal/avatars/` keeps the profile pictures the sheets and the inbox's cards show. The
sheets show people's (who opened the issue or pull request, an issue's assignees and
participants, a pull request's reviewers, and who wrote each comment and review shown in full,
never a bot), which the window asks for in one batch through the issue sheet's bound
`Avatars` (`Pictures`), for both sheets, once a sheet renders, so the conversation never waits
on one. The cards show their authors', people's and bots' alike, which the window asks for
through the service's own binding, `PicturesFor(people, bots)`: core builds the one service,
`Core.Avatars`, hands it to the issue sheet, and binds it, so both share its kept pictures and
its back-off. A person's is downloaded by login (`FetchLoginAvatar`); a bot's (`Bot{Login,
URL}`) by the URL the inbox stored with its item (`items.author_avatar_url`) through
`FetchAvatar`, which refuses a URL off the avatar host, since the host answers a bot's login
with a generated pattern rather than its app's picture. A bot is kept and answered under its
login with `[bot]` after it, which no person's login can hold, so it never takes the picture of
a person named after its app; a bot with no URL is skipped. It owns `avatars`, one row per
lower-cased key, at most `MaxLogins` (100) people and bots together a call, downloaded at most
four at a time, each once until it is a week old and shown again, a failed one (a refused URL
included) not tried again for 30 minutes, and bounded (30 days unused, 2,000 rows, 16 MiB),
pruned in the transaction that writes. The window gets them as data: URLs (`PersonAvatar` and
`card/CardAvatar`, `frontend/src/lib/pictures.ts`), so it loads nothing remote itself.
