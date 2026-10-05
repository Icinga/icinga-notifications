CALL assert_correct_schema_version('v0.2.0-6');

ALTER TABLE incident ADD COLUMN message text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-7', EXTRACT(EPOCH from NOW()) * 1000);
