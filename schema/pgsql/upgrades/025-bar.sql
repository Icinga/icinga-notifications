SELECT assert_correct_schema_version('v0.2.0-23');

ALTER TABLE source ADD COLUMN bar text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-25', EXTRACT(EPOCH from NOW()) * 1000);
