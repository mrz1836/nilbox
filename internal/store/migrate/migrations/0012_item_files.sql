-- 0012_item_files: the files checked pull requests change.
--
-- A pull request from outside the repository (and a collaborator's, unless the user says
-- otherwise) has its changed files listed whenever its head commit moves, so the inbox can
-- point out the sensitive ones: workflows, agent configuration, build scripts, and the
-- like. Only paths are kept, never content. files is a JSON array of {"path", "previous",
-- "change"} objects: the path after the change, a renamed or copied file's path before it
-- ('' otherwise), and GitHub's word for the change. head_oid is the head commit the list
-- belongs to; total is how many files GitHub says the pull request changes, which can be
-- more than it lists (3,000 at most), so complete says whether files holds them all. A
-- check that failed keeps its message in error, with no files, and is tried again on the
-- next refresh. The row is replaced on each check and removed when the item leaves the
-- inbox.
CREATE TABLE item_files (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    total      INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    files      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(files) AND json_type(files) = 'array'),
    complete   INTEGER  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    error      TEXT     NOT NULL DEFAULT '',
    checked_at DATETIME NOT NULL
);
