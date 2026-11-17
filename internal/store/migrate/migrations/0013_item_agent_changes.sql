-- 0013_item_agent_changes: what an agent run's commits changed on an item.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == item_agent_changes ==================================================================

-- One row per item and head commit an agent run's commits changed files at: the head the
-- run started from, whose worktree now holds its commits, and the run's own last commit,
-- which a push makes the pull request's head. files lists the paths git named, up to the
-- fence's cap (complete is 0 past it), as a JSON array of strings. A launch on the item at
-- either head reads them as part of its file check, whoever opened the item, so an agent
-- file a run wrote holds the next click there, as an outside contributor's would. Two runs
-- at one head add up their paths. run_id is the run that noted them last, 0 for none. The
-- rows go with their item.
CREATE TABLE item_agent_changes (
    item_id  INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    head_oid TEXT     NOT NULL CHECK (head_oid <> '' AND head_oid = lower(head_oid)),
    files    TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(files) AND json_type(files) = 'array'),
    total    INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    complete BOOLEAN  NOT NULL DEFAULT 1 CHECK (complete IN (0, 1)),
    run_id   INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    noted_at DATETIME NOT NULL,
    PRIMARY KEY (item_id, head_oid)
);
