TRUNCATE TABLE channel_state;
ALTER TABLE channel_state
    DROP PRIMARY KEY,
    DROP FOREIGN KEY fk_channel_state_channel,
    DROP INDEX idx_channel_state_channel_id; -- Let InnoDB generate a new idx for the constraint instead.
ALTER TABLE channel_state
    MODIFY COLUMN state_key binary(16) NOT NULL FIRST,
    ADD COLUMN incident_id bigint NOT NULL AFTER channel_id,
    ADD CONSTRAINT fk_channel_state_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
    ADD CONSTRAINT fk_channel_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    ADD CONSTRAINT pk_channel_state PRIMARY KEY (state_key);

-- The result of the notification delivery attempt (if any) as a JSON string.
ALTER TABLE notification_history ADD COLUMN delivery_result mediumtext;
