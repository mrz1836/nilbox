-- 0006_item_author_type: what kind of account opened each item.
--
-- author_type is the GraphQL type GitHub reports for the item's author: 'User' for a
-- person, 'Bot' for a GitHub App or bot (Dependabot, Renovate, GitHub Actions), and
-- others such as 'Mannequin'. It is '' until a refresh has read it. The trust rule only
-- treats an author as a bot when GitHub says so here, so a person whose login resembles
-- a bot's can never borrow its trust.
ALTER TABLE items ADD COLUMN author_type TEXT NOT NULL DEFAULT '';
