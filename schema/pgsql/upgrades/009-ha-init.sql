CREATE TABLE event_queue (
    id bytea NOT NULL, -- SHA256 of JSON representation.

    json text NOT NULL,
    time bigint NOT NULL,
    object_id bytea NOT NULL, -- No foreign key, object might not exist at this point.

    user_agent varchar(255) NOT NULL, -- From submitting client; allows migrations after upgrades.
    state smallint NOT NULL DEFAULT 0, -- pending (0), processing (1), done (2), or error (64).

    CONSTRAINT pk_event_queue PRIMARY KEY (id)
);

CREATE INDEX idx_event_queue_time ON event_queue (time);
CREATE INDEX idx_event_queue_time_state ON event_queue (time, state);
CREATE INDEX idx_event_queue_state_object_id ON event_queue (state, object_id);
