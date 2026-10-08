CALL assert_correct_schema_version('v0.2.0-21');

ALTER TABLE source ADD COLUMN dup text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-22', UNIX_TIMESTAMP() * 1000);
