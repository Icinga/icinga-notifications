CREATE TABLE channel_state (
    channel_id bigint NOT NULL,
    state_key varchar(255) NOT NULL,
    value text NOT NULL,
    changed_at bigint NOT NULL,

    CONSTRAINT pk_channel_state PRIMARY KEY (channel_id, state_key),
    CONSTRAINT fk_channel_state_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);
CREATE INDEX idx_channel_state_channel_id_changed_at ON channel_state(channel_id, changed_at);
