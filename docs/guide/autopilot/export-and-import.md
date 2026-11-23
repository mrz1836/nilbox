# Export and import

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Easy mode: templates, from an item, why, export and import, graduating](../../architecture/autopilot/easy-mode.md), [The routines, kept](../../architecture/autopilot/routines-kept.md)

**Export…** on a routine's **⋯** menu, or **Export every routine…** under the routines, saves
them to a file you choose, in the export format ([backup files](../../backup-files.md)), with the text
of the prompts they run by nickname unless you untick **Include the text of the prompts they
run** (a starter prompt travels by its key, as every Mac has it). The file holds your org and
repository names, so it is yours to share or not; it never holds a routine's history or your
account, and Nilbox never sends it anywhere.

**Import routines…** reads such a file and shows what would arrive, changing nothing: each
routine, its sentence, and the name it takes here (another when you have a routine of the
name); each prompt it brings (*added to your prompts* when no prompt of yours has its nickname,
else *yours is the same* or *yours differs, and yours is kept*); and **What this Mac doesn’t
have**: a prompt, a profile, or an agent its routines name that this Mac lacks, each with a
choice of what to use instead or **Inherit** (the item's own default prompt, or the profile a
click would use). An agent whose program this Mac lacks inherits by itself. **Import** brings
the prompts in first, then every routine, switched off, at the end of your list; a file that
changed since it was read is read again rather than imported unseen, and a routine a newer
Nilbox made arrives as it was, unable to run until you update.
