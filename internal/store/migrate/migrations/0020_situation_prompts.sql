-- 0020_situation_prompts: an org's or a repository's prompt for each situation a pull
-- request can be in.
--
-- The conventions are 0001_init's. The launch defaults own every column here.

-- == launchdefaults ======================================================================

-- The inbox's cards offer an agent for the situation a pull request is in, and each
-- situation has a default prompt of its own, set and inherited like pr_prompt_id (the
-- repository's, else its org's, else the app's, else the pull request's own chain):
-- checks_prompt_id when its checks fail, conflicts_prompt_id when it conflicts with its
-- base, changes_prompt_id when a reviewer asked for changes, and rereview_prompt_id when
-- the user is asked to review it again. NULL inherits. A prompt that is a default cannot be
-- deleted: the foreign keys restrict it, as they do the two prompts before them.
ALTER TABLE launch_defaults ADD COLUMN checks_prompt_id INTEGER REFERENCES prompts (id) ON DELETE RESTRICT;
ALTER TABLE launch_defaults ADD COLUMN conflicts_prompt_id INTEGER REFERENCES prompts (id) ON DELETE RESTRICT;
ALTER TABLE launch_defaults ADD COLUMN changes_prompt_id INTEGER REFERENCES prompts (id) ON DELETE RESTRICT;
ALTER TABLE launch_defaults ADD COLUMN rereview_prompt_id INTEGER REFERENCES prompts (id) ON DELETE RESTRICT;

CREATE INDEX launch_defaults_checks_prompt_id ON launch_defaults (checks_prompt_id);
CREATE INDEX launch_defaults_conflicts_prompt_id ON launch_defaults (conflicts_prompt_id);
CREATE INDEX launch_defaults_changes_prompt_id ON launch_defaults (changes_prompt_id);
CREATE INDEX launch_defaults_rereview_prompt_id ON launch_defaults (rereview_prompt_id);
