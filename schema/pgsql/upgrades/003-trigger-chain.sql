SELECT assert_correct_schema_version('v1.0.0-2');

ALTER TABLE notification_history
    ADD COLUMN rule_id bigint,
    ADD COLUMN rule_entry_id bigint;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0-3', EXTRACT(EPOCH from NOW()) * 1000);
