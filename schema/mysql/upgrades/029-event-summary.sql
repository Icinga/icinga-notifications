CALL assert_correct_schema_version('v0.2.0-28');

ALTER TABLE notification_history ADD COLUMN event_summary longtext AFTER event_message;
UPDATE notification_history SET event_summary = COALESCE(NULLIF(LEFT(TRIM(SUBSTRING_INDEX(event_message, '\n', 1)), 1024), ''), 'no summary');
ALTER TABLE notification_history MODIFY COLUMN event_summary longtext NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-29', UNIX_TIMESTAMP() * 1000);
