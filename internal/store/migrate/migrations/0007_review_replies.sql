-- 0007_review_replies: review replies, a second library in the prompts table.
--
-- purpose is 'agent' for a prompt a launch fills in for a coding agent, and 'reply' for a
-- review reply: text the review sheet fills in as the body of a review. reply_action names
-- the review a reply is written for ('approve', 'request_changes', or 'comment'), and is ''
-- for an agent prompt. A reply applies to pull requests and has no launch settings, and
-- agent prompts and replies share one nickname space. Every prompt stored so far is an
-- agent prompt.
ALTER TABLE prompts
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'agent' CHECK (purpose IN ('agent', 'reply'));

ALTER TABLE prompts
    ADD COLUMN reply_action TEXT NOT NULL DEFAULT ''
        CHECK (reply_action IN ('', 'approve', 'request_changes', 'comment')
            AND (reply_action = '') = (purpose = 'agent'));
