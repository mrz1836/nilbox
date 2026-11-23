# Rehearsing an update

`magex app:rehearse` builds two versions of the app to try an update end to end on one Mac,
in place or from the Applications folder, without a release:

```bash
CODESIGN_IDENTITY=<identity> magex app:rehearse case=normal
```

- **A** (`0.0.1`) is `bin/rehearsal/A/Nilbox.app`, and **B** (`0.0.2`) is a release's zip and
  signed manifest in `bin/rehearsal/feed/`, the folder A reads as its releases. Both are built
  for this Mac's processor (the manifest still names `darwin-universal`, the platform the app
  accepts) and trust a key pair made for that run alone, whose private half is never written
  anywhere. Both run on the rehearsal's own data directory, never on yours.
- `CODESIGN_IDENTITY` signs both (`-` signs ad hoc), and `CODESIGN_KEYCHAIN`, when set, goes on
  the keychain search list while the target runs, as it does for `app:package`.
- `case=normal` installs. `case=tampered` changes one byte of B's zip after its manifest is
  signed: A must refuse the download and stay. `case=broken` changes a file B's signature
  seals after B is signed: the helper must find B failing its signature check once swapped in,
  put A back, and A must say the update didn't install.

The target prints the steps, the simplest first. A rehearsal build has the app's bundle
identifier, so quit your own Nilbox first. Then open `bin/rehearsal/A/Nilbox.app` where it is
and, in the menu bar menu, choose **Check for Updates…** and **Restart to Update (0.0.2)**: a
rehearsal build, and only a rehearsal build, also updates itself in the folder it was built in
(core's `rehearsalFolder`, the folder that holds its feed). To rehearse from the Applications
folder instead, as a release updates, the steps move your own copy aside first, if there is
one (`ditto` merges into a bundle that is there), and print the commands that put it back:

```bash
[ ! -e /Applications/Nilbox.app ] || mv /Applications/Nilbox.app ~/Nilbox-before-rehearsal.app
ditto bin/rehearsal/A/Nilbox.app /Applications/Nilbox.app
open /Applications/Nilbox.app
# ... Check for Updates…, Restart to Update (0.0.2), then quit it, and put your copy back:
rm -rf /Applications/Nilbox.app
[ ! -e ~/Nilbox-before-rehearsal.app ] || mv ~/Nilbox-before-rehearsal.app /Applications/Nilbox.app
```

To clean up, delete `bin/rehearsal` and the rehearsal's data directory,
`~/Library/Application Support/Nilbox Rehearsal`.
