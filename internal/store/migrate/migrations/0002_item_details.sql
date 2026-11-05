-- 0002_item_details: what a refresh learns about each item beyond the inbox lists.

-- The details an inbox card shows about a pull request or issue, replaced whole on every
-- refresh that reads them. labels is a JSON array of {"name", "color"} objects, color as
-- "#rrggbb" or "". The pull request columns stay 0 or '' for an issue. checks_state is
-- GitHub's overall check result ('' when the head commit has no checks); the checks_*
-- counts add up the check runs and commit statuses by outcome. reviewers is everyone
-- asked to review or who has, the author excluded; reviewed is how many of them have and
-- are not asked again.
CREATE TABLE item_details (
    item_id           INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    labels            TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(labels) AND json_type(labels) = 'array'),
    comments          INTEGER  NOT NULL DEFAULT 0 CHECK (comments >= 0),
    additions         INTEGER  NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions         INTEGER  NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    changed_files     INTEGER  NOT NULL DEFAULT 0 CHECK (changed_files >= 0),
    commits           INTEGER  NOT NULL DEFAULT 0 CHECK (commits >= 0),
    mergeable         TEXT     NOT NULL DEFAULT '',
    checks_state      TEXT     NOT NULL DEFAULT '',
    checks_passed     INTEGER  NOT NULL DEFAULT 0 CHECK (checks_passed >= 0),
    checks_failed     INTEGER  NOT NULL DEFAULT 0 CHECK (checks_failed >= 0),
    checks_pending    INTEGER  NOT NULL DEFAULT 0 CHECK (checks_pending >= 0),
    checks_skipped    INTEGER  NOT NULL DEFAULT 0 CHECK (checks_skipped >= 0),
    review_decision   TEXT     NOT NULL DEFAULT '',
    reviewers         INTEGER  NOT NULL DEFAULT 0 CHECK (reviewers >= 0),
    reviewed          INTEGER  NOT NULL DEFAULT 0 CHECK (reviewed >= 0 AND reviewed <= reviewers),
    approvals         INTEGER  NOT NULL DEFAULT 0 CHECK (approvals >= 0),
    changes_requested INTEGER  NOT NULL DEFAULT 0 CHECK (changes_requested >= 0),
    fetched_at        DATETIME NOT NULL
);
