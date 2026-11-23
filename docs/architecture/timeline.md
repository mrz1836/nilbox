# internal/timeline

`internal/timeline/` is the conversation both sheets show, an issue's and a pull request's:
its entries (`Entry`: comments, reviews with their line comments, and events), a page of
them (`Page`), and the items, labels, and reactions they name. `Entries` converts what the
GitHub client read, the viewer's own marked; `HunkTail` keeps a line comment's last four
lines of its diff hunk; `Cut` shortens the description and every text of the entries, each
cut marked, for a sheet's cache, whose own cap and steps stay its own; `NonNil` fills the
lists an older build's cache lacks. Its JSON is what the issue sheet's cache already held,
so no name or tag changes. It is a leaf: it imports only `github`, `item`, `clock`, and
`textutil`, never a service, the store, or either sheet (the repoguard boundary test).
