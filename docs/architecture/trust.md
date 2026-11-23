# internal/trust

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/trust`](../../internal/trust) · Guide: [Outside contributors](../guide/outside-contributors.md)

`internal/trust/` decides whether a pull request or an issue comes from someone trusted with its
repository. A launch on an untrusted item starts planning only, because the item's content (code,
descriptions, comments) was written by someone outside the project. The package also keeps the
user's auto-merge bots (`mergebots.go`). It reads no setting itself: callers hand it the stored
values, and it imports only `prefval`. What the user sees is in [Outside
contributors](../guide/outside-contributors.md).

## The trust classes

An item falls in one of three classes (`Class`):

- **`member`** (`ClassMember`): its author's association with the repository is `OWNER`,
  `MEMBER`, or `COLLABORATOR` (`AssociationOwner`, `AssociationMember`,
  `AssociationCollaborator`), and a pull request's branch is in the repository itself.
- **`bot`** (`ClassBot`): GitHub reports its author as a bot (a GitHub App), the bot is on the
  user's trusted list, and a pull request's branch is in the repository itself.
- **`outside`** (`ClassOutside`): everyone and everything else: every other association
  (`CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, `FIRST_TIMER`, `NONE`, `MANNEQUIN`, one GitHub adds
  later, and none at all), bots not on the list, and every pull request from a fork
  (`TestClassifyPeople`).

`Policy.Classify(author, isPR, crossRepo)` decides as follows. A pull request from a fork
(`crossRepo`) is `outside` whoever opened it, a trusted bot included. Then the association, trimmed
and upper-cased, makes a `member`, a bot that is also a collaborator included. Then a bot on the
list is a `bot`, and anything else is `outside`. `crossRepo` counts only for a pull request; the
rest of the rule applies to issues as well, since an agent reads an issue's body as it reads a pull
request's. `Policy.Trusted` is any class but `outside`.

`Author` is who opened an item: `Login`, `Type` (its GraphQL type, `User`, `Bot`, or another
actor's; `""` before a refresh has read it), and `Association`. `Author.IsBot` reports `Type ==
TypeBot` (`"Bot"`). A bot is matched by both its type and its login, so a person whose login
resembles a bot's never borrows its trust, and a bot whose type isn't read yet isn't trusted as
one (`TestClassifyBots`). GitHub reports a bot's login as its app's slug, such as `dependabot`,
without the `[bot]` suffix its REST login carries.

## The trusted bots (`trust.bots`)

`PrefBots` (`trust.bots`) lists the trusted bots as a JSON array of logins, which settings alone
writes (`SetTrustedBots`, `ResetTrustedBots`, and `Portable`'s `trustedBots` on import).
`DefaultBots` is `dependabot`, `github-actions`, and `renovate`. `DecodeBots(raw, stored)` reads
the defaults while the key is unset or unreadable, else the stored list, which may be empty (a
stored `null` reads as empty too); `ResetTrustedBots` deletes the key, so the list is the defaults
again. `EncodeBots` writes a list through `prefval.EncodeList`, a nil list as `[]`.
`NewPolicy(bots)` matches logins through `foldBot`: trimmed, lower-cased, and without an `app/`
prefix or a `[bot]` suffix.

`NormalizeBots` cleans a list as the user entered it: each login folded so, empty entries and
repeats dropped, the list sorted and never nil. A login GitHub couldn't have issued is an
`ErrInvalidBot` naming it: once folded, `botPattern` wants 1 to 39 lower-case letters, digits, and
hyphens, the first a letter or a digit. A list of more than `maxBots` (100) is an
`ErrTooManyBots`. `FuzzNormalizeBots` checks that a list it keeps normalizes to itself and makes a
policy that trusts every bot the user listed.

## Auto-merge bots (`mergebots.go`)

Many orgs run a GitHub App of their own (such as `acme-org-automerge`) that approves a pull request
and merges it once its checks pass. `MergeBots` is the user's list of them and whether a card says
so: `On`, from `PrefMergeBotsOn` (`trust.mergeBotsOn`, a switch as `prefval` writes one, off until
the user turns it on), and `Bots`, from `PrefMergeBots` (`trust.mergeBots`, a JSON array of logins,
none until the user adds one; no build ships a name). Settings alone writes both (`SetMergeBotsOn`,
`SetMergeBots`, and `Portable`'s `mergeBotsOn` and `mergeBots`). `DecodeMergeBots` reads them, off
when the switch is unset or unreadable and none when the list is, never a nil list.
`NormalizeMergeBots` cleans a list as `NormalizeBots` does, but a list of more than 100 is an
`ErrTooManyMergeBots`, so the error says which list is too long.

`MergeBots.Match(approving)` takes the logins of the bots whose latest review approves a pull
request and returns the first bot of the list, in its stored order, found among them, both sides
folded as `foldBot` folds a login, and a blank listed login matching nothing; it returns `""` while
the switch is off and when no listed bot approved (`TestMergeBotsMatch`). Listing a bot trusts
nothing: the match only names it on the card. The inbox matches as each snapshot is built, against
`github.ItemDetails.ApprovingBots`, which holds bots alone, by type and login, and names the match
`Details.MergeBot`; the read, the storage, and the card are in [Auto-merge
bots](budget-and-github.md#auto-merge-bots).

## Who reads it

- **The inbox.** Each snapshot item carries `Trust` (its class), `Trusted`, and `AuthorIsBot`, and
  each of an issue's closing pull requests `Trusted`, the signed-in user's own always
  (`closingPulls`). The policy comes from the stored `trust.bots` (`newFileScope`, kept until the
  settings change in `scopeCache`). `trust.bots`, `trust.mergeBotsOn`, and `trust.mergeBots` are in
  the snapshot's `settingsKey`, so a change builds the next snapshot from what is stored and asks
  GitHub nothing. The class decides whose changed files are checked (`sensitive.Checked`; see
  [`internal/sensitive`](sensitive.md)). `Author.IsBot` alone tells a bot from a person for the
  snooze wake rules (`snooze.Wake`) and the routines' author suggestions. The card shows an
  untrusted item's chip (`frontend/src/components/card/RiskChip.tsx`, `trustTitle`).
- **The launcher.** Its `world.policy` is built from `Settings.TrustedBots`, and an item's class
  (`launchItemRow.class`) decides `Item.Trusted` and the resolution's `subject.trusted`. Below the
  trust rule, `resolvePermission` starts an untrusted item without the user's
  `LaunchRequest.FullAccessOverride` planning only, and a fenced run on one Read only; a routine's
  launch never carries the override (`personsChoices`). `promptData` sets
  `prompts.Data.OutsideContributor` for an `outside` item and `IsBot` from the author's type, and
  `internal/prompts`' `Notice` ends the prompt with `trustNotice`'s line (the outside-contributor
  line, or, for a bot's item that isn't outside, the line about text quoted from other projects),
  followed by `sensitiveNotice`'s line when the pull request changes sensitive files. See
  [`internal/launcher`](launcher.md).
- **Autopilot.** A routine's facts read the class as `FieldTrust` (`builder.trust`, the policy from
  the inbox's `FactsEnv`): a fork's or a bot's class is known at once, while one whose association
  isn't read yet, or an `outside` one whose author's type isn't, is unknown; `FieldAuthorIsBot` is
  the author's type alone. `outsideRule` runs an agent on an outside contributor's item only where
  its org or repository allows it and otherwise closes the match as skipped (`ReasonOutside`,
  `outside_contributor`), and any step on one that isn't local (a GitHub action, a Repo script)
  waits for the user's OK (`BlockOutside`). Merge it never merges one (`outsideContributor`), and
  Post the agent's review approves only a `member`'s or a `bot`'s pull request (`guardOf`). See
  [Merge it and Post the agent's review](autopilot/merge-and-review.md).
- **Describe it.** A proposal's `rules.trust` and `rules.untrust` change the trusted list, each
  login normalized by `NormalizeBots` (`kit`'s `rulesCheck.bots`), and the kit tells Claude Code
  what the classes mean and which bots are trusted by default (`rulesTopic`). See
  [`internal/autopilot/kit`](autopilot/kit.md).
