CALL assert_correct_schema_version('v0.2.0-15');

CREATE TABLE channel_state (
    channel_id bigint NOT NULL,
    state_key varchar(255) NOT NULL,
    value varchar(4096) NOT NULL,

    CONSTRAINT pk_channel_state PRIMARY KEY (channel_id, state_key),
    CONSTRAINT fk_channel_state_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);
CREATE INDEX idx_channel_state_channel_id ON channel_state(channel_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-16', EXTRACT(EPOCH from NOW()) * 1000);
