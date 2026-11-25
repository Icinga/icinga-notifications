CALL assert_correct_schema_version('v1.0.0-2');

ALTER TABLE notification_history
    ADD COLUMN rule_id bigint AFTER incident_id,
    ADD COLUMN rule_entry_id bigint AFTER rule_id;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0-3', UNIX_TIMESTAMP() * 1000);
