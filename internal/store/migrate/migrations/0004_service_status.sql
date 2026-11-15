-- 0004_service_status: the status check follows several services' status pages.
--
-- The conventions are 0001_init's.

-- == prefs ===============================================================================

-- The check of GitHub's status page became the service status check, which reads GitHub's,
-- Claude's, and OpenAI's pages, so its interval moves to the new key, as the user set it.
-- A database that somehow has both keeps the new one.
UPDATE prefs SET key = 'refresh.serviceStatus'
WHERE key = 'refresh.githubStatus'
  AND NOT EXISTS (SELECT 1 FROM prefs WHERE key = 'refresh.serviceStatus');

DELETE FROM prefs WHERE key = 'refresh.githubStatus';
