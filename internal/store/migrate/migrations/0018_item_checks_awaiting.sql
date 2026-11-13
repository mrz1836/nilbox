-- 0018_item_checks_awaiting: how many of a pull request's workflow runs wait for approval.
--
-- checks_awaiting counts the head commit's GitHub Actions runs held until a maintainer
-- approves them, as a fork's runs are: check suites that ended ACTION_REQUIRED with a
-- workflow run. GitHub's rollup leaves them out, so checks_state can read SUCCESS while
-- they wait. It stays 0 for an issue and for a pull request with none held. Like the other
-- details, it is replaced whole on every refresh that reads them.
ALTER TABLE item_details
    ADD COLUMN checks_awaiting INTEGER NOT NULL DEFAULT 0 CHECK (checks_awaiting >= 0);
