CREATE TABLE event_queue (
    id binary(32) NOT NULL, -- SHA256 of JSON representation.

    json text NOT NULL COLLATE utf8mb4_unicode_ci,
    time bigint NOT NULL,
    -- No need for foreign keys, especially as the object might not exist at this point.
    source_id bigint NOT NULL,
    object_id binary(32) NOT NULL,

    version varchar(64) NOT NULL, -- From submitting client; allows migrations after upgrades.
    state smallint NOT NULL DEFAULT 0, -- pendig (0), processing (1), done (2), or error (64).

    CONSTRAINT pk_event_queue PRIMARY KEY (id)
);

CREATE INDEX idx_event_queue_time ON event_queue (time);
CREATE INDEX idx_event_queue_object_id ON event_queue (object_id);
CREATE INDEX idx_event_queue_state ON event_queue (state);
