-- 0015_autopilot_graduate: when the user last answered Not now to putting a routine on
-- Autopilot.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC.

-- == autopilot ===========================================================================

-- A routine in Ask first whose last five approved runs finished cleanly offers to go on
-- Autopilot; nothing ever changes its mode by itself. graduate_declined_at is when the user
-- last said Not now, NULL while they never did: only approvals after it count toward the
-- next offer.
ALTER TABLE autopilot_routines ADD COLUMN graduate_declined_at DATETIME;
