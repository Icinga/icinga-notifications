ALTER TABLE browser_session
    DROP CONSTRAINT pk_browser_session;

DROP INDEX browser_session_authenticated_at_idx;

ALTER TABLE browser_session
    ALTER COLUMN user_agent TYPE varchar(4096) USING user_agent::varchar(4096);

ALTER TABLE browser_session
    ADD CONSTRAINT pk_browser_session PRIMARY KEY (php_session_id);

ALTER TABLE IF EXISTS browser_session
    ALTER COLUMN authenticated_at DROP DEFAULT;

CREATE INDEX idx_browser_session_authenticated_at ON browser_session (authenticated_at DESC);
CREATE INDEX idx_browser_session_username_agent ON browser_session (username, user_agent);
