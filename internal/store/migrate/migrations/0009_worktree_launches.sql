-- 0009_worktree_launches: the worktrees the app started agent sessions in.
--
-- A launch with "Start in a worktree" asks the agent for a worktree named after the item
-- (pr-<n> or issue-<n>) under <clone>/.claude/worktrees. Recording which pull request or
-- issue each one is for lets the Worktrees view link it exactly, and lets the automatic
-- cleanup find the worktrees to check without listing every clone. clone_path is the
-- clone's directory as the launcher resolved it; worktree is the directory's name. A
-- launch on the same item again replaces its row, and a row is removed with its worktree.
CREATE TABLE worktree_launches (
    id          INTEGER  PRIMARY KEY,
    clone_path  TEXT     NOT NULL CHECK (clone_path <> ''),
    worktree    TEXT     NOT NULL CHECK (worktree <> ''),
    owner       TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name        TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind        TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number      INTEGER  NOT NULL CHECK (number > 0),
    launched_at DATETIME NOT NULL,
    UNIQUE (clone_path, worktree)
);
