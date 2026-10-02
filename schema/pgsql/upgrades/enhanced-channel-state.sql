TRUNCATE TABLE channel_state;
ALTER TABLE channel_state
    ALTER COLUMN state_key TYPE uuid USING state_key::uuid,
    ADD COLUMN incident_id bigint NOT NULL,
    ADD CONSTRAINT fk_channel_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    DROP CONSTRAINT pk_channel_state,
    ADD CONSTRAINT pk_channel_state PRIMARY KEY (state_key);
CREATE INDEX idx_channel_state_incident_id ON channel_state(incident_id);

-- The result of the notification delivery attempt (if any) as a JSON string.
ALTER TABLE notification_history ADD COLUMN delivery_result text;
