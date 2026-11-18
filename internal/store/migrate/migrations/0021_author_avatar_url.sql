-- 0021_author_avatar_url: the picture of an item's author when the author is a bot.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == items ===============================================================================

-- The inbox's cards show each author's picture. A person's is downloaded by login, but the
-- avatar host answers a bot's login with a generated pattern rather than its app's picture,
-- so the inbox's search asks GitHub for a bot author's picture URL too, at no extra point.
--
-- author_avatar_url is that URL, on the avatar host, as GitHub gave it, and '' for a person,
-- for any author that is not a bot, and until a read has asked for it. The inbox's refresh
-- and an item's own read write it; a routine's watched read leaves it as stored.
ALTER TABLE items ADD COLUMN author_avatar_url TEXT NOT NULL DEFAULT '';
