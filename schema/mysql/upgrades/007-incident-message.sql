CALL assert_correct_schema_version('v0.2.0-6');

ALTER TABLE incident ADD COLUMN message longtext;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-7', UNIX_TIMESTAMP() * 1000);
