CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE browser_session
(
    php_session_id   VARCHAR(256) NOT NULL,
    username         CITEXT       NOT NULL,
    user_agent       TEXT         NOT NULL,
    authenticated_at bigint       NOT NULL DEFAULT (EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) * 1000),

    CONSTRAINT pk_session PRIMARY KEY (php_session_id, username, user_agent)
);

CREATE INDEX browser_session_authenticated_at_idx
    ON browser_session (
        authenticated_at
    )
;
