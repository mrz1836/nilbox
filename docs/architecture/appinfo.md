# internal/appinfo

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/appinfo`](../../internal/appinfo) · Guide: [The window](../guide/window.md)

Settings → About's **Report a bug** opens `.github/ISSUE_TEMPLATE/bug_report.yml` and fills in its
`environment` field: the versions, then the settings and integrations, which core words
(`internal/core/bugreport.go`, `appinfo.StateReader`) with nothing personal in them, a name or
path of the user's own told only as the default or the user's own, or counted; its test fails on
any. The settings are worded one line per page of Settings, under the page's name and in the
sidebar's order (`internal/core/bugreport_settings.go`), from Appearance to Labs, whose line,
whether Autopilot (beta) is on, is always there. The settings kept outside the settings service
are among them: the default prompts and replies (each a starter, the user's own, none, or a
missing prompt), the profiles and the launch defaults (counted), and, in Autopilot's section,
Describe it's agent. Autopilot's section shows while Autopilot is on or the user has a routine.
`TestABugReportWordsEverySetting` changes each field of `settings.Settings` in turn and fails
when the report doesn't change with it, so a new setting is worded there or listed in
`bugReportLeavesOut` with the reason it stays out (a catalog, what the window remembers, a
path, or a setting another section words); it fails on a key that names no field too. A full
report's link stays under 8,000 characters (`TestABugReportStaysShortEnoughToOpen`). The form is
synced, so if a sync renames that field, the button stops filling
it in. The `internal/appinfo` tests read the form and fail when that happens. The account
menu's **Report a bug** opens the same link, and its **Send feedback** opens GitHub's own blank
issue (`template=BLANK_ISSUE`, `appinfo.FeedbackURL`) with a `[Feedback]` title, a few prompts,
and the app's version filled in; a synced `config.yml` with `blank_issues_enabled: false` would
turn that link into the template chooser. The `magefiles`
tests likewise fail if the synced `10-mage-x.env` stops pinning golangci-lint, which `magex
app:lint` reads.
