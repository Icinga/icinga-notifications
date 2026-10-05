SELECT assert_correct_schema_version('v0.2.0-20');

ALTER TABLE channel
  ADD COLUMN validation_result text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-21', EXTRACT(EPOCH from NOW()) * 1000);
