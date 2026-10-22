SELECT assert_correct_schema_version('v0.2.0-28');

ALTER TABLE notification_history ADD COLUMN event_summary text;
UPDATE notification_history SET event_summary = COALESCE(NULLIF(LEFT(BTRIM(SPLIT_PART(event_message, E'\n', 1)), 1024), ''), 'no summary');
ALTER TABLE notification_history ALTER COLUMN event_summary SET NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-29', EXTRACT(EPOCH from NOW()) * 1000);
