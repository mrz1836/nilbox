# internal/hooks

`internal/hooks/` is the Hooks page's service. `Repos` scans, four at a time, every clone
in the repository locations with a go-pre-commit config inside it (a `.github/env/*.env` file, or
`.github/.env.base`): where git runs its hooks (gitread's `HookPaths`), its `pre-commit` hook
(missing, go-pre-commit's by its marker, or another tool's, named husky, pre-commit, or
lefthook from its text), and `ENABLE_GO_PRE_COMMIT` and the checks' switches from the config
files themselves. Only a regular file, or a link to one, is read, a config file and a hook
alike, and each is opened without waiting and checked again once open (`paths.OpenRegular`),
so a named pipe swapped in never hangs a scan. A hooks folder outside both the clone's git folder and its work tree is
shared, and the scan counts the clones that use it. Only go-pre-commit's `status` says whether
its own hook is current, a byte comparison, so it runs in a scratch repository of the app's own
that holds the hook's bytes, never in a clone, whose `.env` files go-pre-commit would load into
its environment, `PATH` included, before it runs git; its answer is kept per copy and per
hook's SHA-256, so status runs once for bytes no one judged. A scan runs apart from the
window's call that started it, within `ScanTimeout`, and `Scans`, a desktop worker, holds
every `Repos` call, its scan and its reads of the database included, to the app's lifetime:
the quit ends every one under way and refuses later ones, and the worker returns once they
have, so the shell waits for them before it closes the database. The scan is kept
five minutes, a clone an action or a check ran in is scanned again on the next read, and
`event.HooksUpdated` tells the window once the run has let go of the clone (the spec's
`Released`), so the reread finds it free and the run ended. `Install`, `Update`, `Reinstall`, `Replace`,
`Uninstall`, and `RunChecks` hand `scriptrun` a `KindHooks` or `KindChecks` run whose `Before`,
under the clone's lock, reads the clone again and refuses a shared hooks folder the request
doesn't confirm. An action's `Finish` reads what it did from the hook files, never from the
exit status alone (1.13 exits 0 when another tool's hook stops it); a checks run's compares
git's porcelain v2 status and each dirty file's SHA-256, taken before and after, so a fix to a
file that was already dirty shows. The `Before` keeps what it read beside the run's record
(`scriptrun`'s `Spec.Keep`, never its intent: an action's hooks and the copy of go-pre-commit
that ran, with its version's features; a checks run's config switches and its snapshot,
hashed or, past `maxHashed` dirty files, by size and time), so the `Verifier`, which core
registers for both kinds, judges a run left from before a restart as its `Finish` would,
once the shell's environment is read (`shellenv.Await`): the hook an action left unchanged is
judged only while the copy that ran is still the one in use, and a run that kept nothing
says it can't tell. A `Finish` whose step (git's status, go-pre-commit's) the quit cuts short
leaves the run `Unsettled`, for the next start; one whose steps all ended stands.
`InstallAll` installs or updates one clone at a time, each its own run in the app, and
skips another tool's hook, a shared folder, and a busy clone. The readers know 1.13.1's
text, recorded in `testdata/go-pre-commit-1.13.1`, and read `status --json` and `run
--json` once `Definition.Features` says the copy has them. The page carries the PATH
warning (the hook finds go-pre-commit only on the PATH or in `$(go env GOPATH)/bin`) and,
until `FeatureVerifiedTools`, the note that the checks install their own tools, folded
once a checks run ended (`hooks.checksRan`, which hooks alone writes). The bound
`ActionResult` and `ChecksResult` read a run's outcome back typed, and its `Rerunner`, which
core registers for both kinds, works Run again out afresh from the run's intent. A row's
**Open in terminal** (`OpenTerminal`) opens the terminal the runs open in at the clone and
runs nothing there, and its **Open the config** (`ShowConfig`) shows `.github/env/` (or
`.github/.env.base`) in Finder, each only for a clone the repository locations hold.
