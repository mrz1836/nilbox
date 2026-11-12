-- 0014_session_launch: what a session's launch reported, kept for the item's sessions panel.

-- permission summarizes the permissions the session started with, as the launch worded
-- them ("full access", "plan mode (outside contributor)"). worktree_action is what the
-- launch did to the item's worktree (resume, adopt, attach, or create), or '' for a
-- session in the clone. launch_notice says what the launch worked around, such as a
-- worktree started from the last fetched copy of the default branch, or '' for nothing.
-- Each is '' for a session recorded before this migration.
ALTER TABLE agent_sessions ADD COLUMN permission TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN worktree_action TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN launch_notice TEXT NOT NULL DEFAULT '';
