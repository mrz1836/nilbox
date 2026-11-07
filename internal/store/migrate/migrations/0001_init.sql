-- 0001_init: the local schema.
--
-- Conventions:
--   - Timestamps are DATETIME columns so the driver reads them back as times. The
--     application writes them in UTC, which keeps their text form sortable.
--   - Booleans are BOOLEAN columns, kept to 0 or 1 by a CHECK, so code generated from
--     this schema reads them as bools. A NULL in a boolean that allows one means
--     "inherit".
--   - GitHub logins and repository names are case-insensitive, so those columns use
--     NOCASE collation.
--   - Credentials are never stored here; the app borrows them at run time.

-- The signed-in GitHub account.
CREATE TABLE accounts (
    id         INTEGER  PRIMARY KEY,
    login      TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Repositories seen in the inbox, with per-repo preferences, the matched local clone, and
-- what GitHub says about each one.
--
-- Every refresh rewrites GitHub's side from the inbox query, which carries it on each item
-- at no extra cost. fetched_at is NULL until a refresh has described the repository.
-- visibility is PUBLIC, PRIVATE, or INTERNAL; archived_at and pushed_at are NULL when
-- GitHub has none; parent is the "owner/name" a fork was made from; viewer_permission is
-- the viewer's access (ADMIN, MAINTAIN, WRITE, TRIAGE, or READ) and viewer_merge_method the
-- merge method GitHub offers the viewer first; language_color is "#rrggbb" or ''. An
-- archived repository's items are never listed.
CREATE TABLE repos (
    id                     INTEGER  PRIMARY KEY,
    owner                  TEXT     NOT NULL COLLATE NOCASE,
    name                   TEXT     NOT NULL COLLATE NOCASE,
    muted                  BOOLEAN  NOT NULL DEFAULT 0 CHECK (muted IN (0, 1)),
    pinned                 BOOLEAN  NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    local_path             TEXT     NOT NULL DEFAULT '',
    created_at             DATETIME NOT NULL,
    updated_at             DATETIME NOT NULL,
    node_id                TEXT     NOT NULL DEFAULT '',
    description            TEXT     NOT NULL DEFAULT '',
    visibility             TEXT     NOT NULL DEFAULT '',
    is_archived            BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_archived IN (0, 1)),
    archived_at            DATETIME,
    is_disabled            BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_disabled IN (0, 1)),
    is_locked              BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_locked IN (0, 1)),
    lock_reason            TEXT     NOT NULL DEFAULT '',
    is_fork                BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_fork IN (0, 1)),
    parent                 TEXT     NOT NULL DEFAULT '',
    default_branch         TEXT     NOT NULL DEFAULT '',
    viewer_permission      TEXT     NOT NULL DEFAULT '',
    viewer_merge_method    TEXT     NOT NULL DEFAULT '',
    merge_commit_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (merge_commit_allowed IN (0, 1)),
    squash_merge_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (squash_merge_allowed IN (0, 1)),
    rebase_merge_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (rebase_merge_allowed IN (0, 1)),
    auto_merge_allowed     BOOLEAN  NOT NULL DEFAULT 0 CHECK (auto_merge_allowed IN (0, 1)),
    delete_branch_on_merge BOOLEAN  NOT NULL DEFAULT 0 CHECK (delete_branch_on_merge IN (0, 1)),
    allow_update_branch    BOOLEAN  NOT NULL DEFAULT 0 CHECK (allow_update_branch IN (0, 1)),
    language               TEXT     NOT NULL DEFAULT '',
    language_color         TEXT     NOT NULL DEFAULT '',
    pushed_at              DATETIME,
    fetched_at             DATETIME,
    UNIQUE (owner, name)
);

-- The unified pull request and issue cache. The reason_* flags record which inbox lists
-- returned the item on the latest refresh; author_association and is_cross_repository
-- feed the outside-contributor trust rule.
--
-- author_type is the GraphQL type GitHub reports for the item's author: 'User' for a
-- person, 'Bot' for a GitHub App or bot (Dependabot, Renovate, GitHub Actions), and
-- others such as 'Mannequin'. It is '' until a refresh has read it. The trust rule only
-- treats an author as a bot when GitHub says so here, so a person whose login resembles
-- a bot's can never borrow its trust.
CREATE TABLE items (
    id                      INTEGER  PRIMARY KEY,
    account_id              INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    repo_id                 INTEGER  NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    kind                    TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number                  INTEGER  NOT NULL CHECK (number > 0),
    node_id                 TEXT     NOT NULL DEFAULT '',
    title                   TEXT     NOT NULL DEFAULT '',
    url                     TEXT     NOT NULL DEFAULT '',
    author_login            TEXT     NOT NULL DEFAULT '',
    author_association      TEXT     NOT NULL DEFAULT '',
    is_cross_repository     BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_cross_repository IN (0, 1)),
    is_draft                BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_draft IN (0, 1)),
    head_ref                TEXT     NOT NULL DEFAULT '',
    item_created_at         DATETIME NOT NULL,
    item_updated_at         DATETIME NOT NULL,
    reason_review_requested BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_review_requested IN (0, 1)),
    reason_authored         BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_authored IN (0, 1)),
    reason_assigned         BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_assigned IN (0, 1)),
    fetched_at              DATETIME NOT NULL,
    author_type             TEXT     NOT NULL DEFAULT '',
    UNIQUE (account_id, repo_id, kind, number)
);

CREATE INDEX items_repo_id ON items (repo_id);
CREATE INDEX items_account_updated ON items (account_id, item_updated_at);

-- Local-only triage state for an item: snooze, dismiss, and pin. The state stays on this
-- machine; nothing is written to GitHub.
--
-- An item can be dismissed (hidden from every list until it is restored) or snoozed
-- (hidden until snoozed_until passes, then back on its own), never both. A row whose
-- snooze has passed is kept, so the inbox can say the item is back from a snooze;
-- restoring an item deletes its row, unless the item is pinned.
--
-- snooze_baseline is a JSON object of the item's activity when it was snoozed (its head,
-- commit and comment counts, and reviews), which each refresh compares with the item's
-- details under the user's wake rules (the snooze.* prefs). wake_reasons is a JSON object
-- of the new activity that ended a snooze early; it is NULL while a snooze runs and when
-- one ran its course.
--
-- A pinned item stays at the top of every list it is in, the most recently pinned first,
-- until it is unpinned. A pin is kept apart from a snooze or a dismissal: snoozing or
-- dismissing a pinned item sets it aside as usual, and it comes back still pinned, so
-- undoing either leaves the pin as it was.
CREATE TABLE item_local_state (
    item_id         INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    dismissed_at    DATETIME,
    updated_at      DATETIME NOT NULL,
    snoozed_until   DATETIME CHECK (snoozed_until IS NULL OR dismissed_at IS NULL),
    snooze_baseline TEXT     CHECK (snooze_baseline IS NULL OR (json_valid(snooze_baseline) AND json_type(snooze_baseline) = 'object')),
    wake_reasons    TEXT     CHECK (wake_reasons IS NULL OR (json_valid(wake_reasons) AND json_type(wake_reasons) = 'object')),
    pinned_at       DATETIME
);

-- User preferences as key/value pairs.
CREATE TABLE prefs (
    key        TEXT     NOT NULL PRIMARY KEY,
    value      TEXT     NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Coding-agent launch profiles. add_dirs and extra_args are JSON arrays of strings; env is
-- a JSON object of string values.
CREATE TABLE claude_profiles (
    id              INTEGER  PRIMARY KEY,
    name            TEXT     NOT NULL UNIQUE,
    config_dir      TEXT     NOT NULL DEFAULT '',
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    permission_mode TEXT     NOT NULL DEFAULT '',
    plan_mode       BOOLEAN  NOT NULL DEFAULT 0 CHECK (plan_mode IN (0, 1)),
    add_dirs        TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(add_dirs) AND json_type(add_dirs) = 'array'),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    env             TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- GitHub API rate-limit readings, one row per resource per refresh.
CREATE TABLE rate_snapshots (
    id          INTEGER  PRIMARY KEY,
    account_id  INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    resource    TEXT     NOT NULL,
    limit_total INTEGER  NOT NULL,
    remaining   INTEGER  NOT NULL,
    used        INTEGER  NOT NULL,
    cost        INTEGER  NOT NULL DEFAULT 0,
    reset_at    DATETIME NOT NULL,
    taken_at    DATETIME NOT NULL
);

CREATE INDEX rate_snapshots_resource_taken_at ON rate_snapshots (resource, taken_at);
CREATE INDEX rate_snapshots_account_id ON rate_snapshots (account_id);

-- The details an inbox card shows about a pull request or issue, replaced whole on every
-- refresh that reads them. labels is a JSON array of {"name", "color"} objects, color as
-- "#rrggbb" or "". The pull request columns stay 0 or '' for an issue. checks_state is
-- GitHub's overall check result ('' when the head commit has no checks); the checks_*
-- counts add up the check runs and commit statuses by outcome. reviewers is everyone
-- asked to review or who has, the author excluded; reviewed is how many of them have and
-- are not asked again. head_oid is a pull request's head commit SHA; a new one means it
-- was pushed, force-pushes included.
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
    fetched_at        DATETIME NOT NULL,
    head_oid          TEXT     NOT NULL DEFAULT ''
);

-- The prompt library.
--
-- A prompt is a Go text/template that a launch fills in for one pull request or issue.
-- name is its full title and nickname the short label the launch menu shows; nicknames
-- are unique, ignoring case. applies_to is 'pr', 'issue', or 'any'. starter_key names the
-- starter a row was seeded from ('' for a prompt the user wrote), so the app can restore
-- the original text; the app seeds each starter once, so a deleted starter stays deleted.
--
-- The rest are the prompt's launch settings, used whenever the prompt is chosen: model and
-- effort are '' to inherit, plan_mode and worktree are NULL to inherit, and extra_args is a
-- JSON array of arguments added after the profile's and the launch defaults'.
CREATE TABLE prompts (
    id          INTEGER  PRIMARY KEY,
    name        TEXT     NOT NULL,
    nickname    TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    applies_to  TEXT     NOT NULL CHECK (applies_to IN ('pr', 'issue', 'any')),
    body        TEXT     NOT NULL,
    model       TEXT     NOT NULL DEFAULT '',
    effort      TEXT     NOT NULL DEFAULT '',
    plan_mode   BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree    BOOLEAN  CHECK (worktree IN (0, 1)),
    extra_args  TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    starter_key TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

-- Launch defaults for an org or a repository.
--
-- A row with name '' holds an owner's defaults, which every repository of that owner
-- inherits; a row with a name holds one repository's, which win over its owner's. Rows are
-- keyed by name rather than by repos.id, so an org's defaults need no repository row and
-- outlive the inbox cache.
--
-- Every setting inherits when it is '' or NULL: agent is the coding tool ('' is the app's,
-- Claude), profile a profile name, pr_prompt_id and issue_prompt_id the default prompt for
-- pull requests and for issues, model and effort the agent flags, plan_mode and worktree
-- on or off. extra_args is a JSON array of arguments added after the profile's (an org's
-- first, then the repository's). A prompt that is a default cannot be deleted: the
-- foreign keys restrict it.
CREATE TABLE launch_defaults (
    id              INTEGER  PRIMARY KEY,
    owner           TEXT     NOT NULL COLLATE NOCASE,
    name            TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    agent           TEXT     NOT NULL DEFAULT '',
    profile         TEXT     NOT NULL DEFAULT '',
    pr_prompt_id    INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    issue_prompt_id INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    plan_mode       BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree        BOOLEAN  CHECK (worktree IN (0, 1)),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    UNIQUE (owner, name)
);

CREATE INDEX launch_defaults_pr_prompt_id ON launch_defaults (pr_prompt_id);
CREATE INDEX launch_defaults_issue_prompt_id ON launch_defaults (issue_prompt_id);

-- The worktrees the app started agent sessions in.
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

-- The worktrees the app cleaned up, newest kept.
--
-- Cleaning up removes a finished worktree, and its own branch, once its pull request merged
-- or its issue closed: by hand from the Worktrees view, or on its own when automatic
-- cleanup is on. Each one is recorded here so the view can show what was removed and when,
-- across restarts, since a toast is easy to miss. automatic tells the sweep from the user.
-- display_path is the worktree's path with ~ for home, as the view shows it; head is the
-- commit it had checked out, which was on GitHub. Only the newest rows are kept.
CREATE TABLE worktree_cleanups (
    id             INTEGER  PRIMARY KEY,
    cleaned_at     DATETIME NOT NULL,
    automatic      BOOLEAN  NOT NULL DEFAULT 0 CHECK (automatic IN (0, 1)),
    repo           TEXT     NOT NULL CHECK (repo <> ''),
    worktree       TEXT     NOT NULL CHECK (worktree <> ''),
    display_path   TEXT     NOT NULL DEFAULT '',
    deleted_branch TEXT     NOT NULL DEFAULT '',
    head           TEXT     NOT NULL DEFAULT '',
    item_kind      TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    item_owner     TEXT     NOT NULL DEFAULT '',
    item_name      TEXT     NOT NULL DEFAULT '',
    item_number    INTEGER  NOT NULL DEFAULT 0 CHECK (item_number >= 0),
    item_title     TEXT     NOT NULL DEFAULT '',
    item_url       TEXT     NOT NULL DEFAULT '',
    item_state     TEXT     NOT NULL DEFAULT '' CHECK (item_state IN ('', 'open', 'merged', 'closed'))
);

CREATE INDEX worktree_cleanups_cleaned_at ON worktree_cleanups (cleaned_at);

-- The files checked pull requests change.
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
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    error      TEXT     NOT NULL DEFAULT '',
    checked_at DATETIME NOT NULL
);

-- What the app told the user, newest kept.
--
-- A refresh that finds items newly needing the user records one row per item: a pull
-- request that newly asks for the user's review (review_requested), an issue or pull
-- request newly assigned to them (assigned), or a snoozed item new activity brought back
-- early (woke, with what woke it in detail, such as "approved, 2 new commits"). Each row
-- is kept whether or not a banner was shown, so the window's notification history lists
-- every one. The item's repository, number, title, and URL are copied in, so a row still
-- reads after its item leaves the inbox; item_id links back to it while it is stored.
-- read_at is set once the user has seen the history. Only the newest rows are kept.
CREATE TABLE notifications (
    id         INTEGER  PRIMARY KEY,
    kind       TEXT     NOT NULL CHECK (kind <> ''),
    created_at DATETIME NOT NULL,
    read_at    DATETIME,
    item_id    INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    item_kind  TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    repo_owner TEXT     NOT NULL DEFAULT '',
    repo_name  TEXT     NOT NULL DEFAULT '',
    number     INTEGER  NOT NULL DEFAULT 0 CHECK (number >= 0),
    title      TEXT     NOT NULL DEFAULT '',
    url        TEXT     NOT NULL DEFAULT '',
    author     TEXT     NOT NULL DEFAULT '',
    detail     TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX notifications_created_at ON notifications (created_at);
