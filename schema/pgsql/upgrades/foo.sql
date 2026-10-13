SELECT assert_correct_schema_version('v0.2.0-23');

ALTER TABLE source ADD COLUMN foo text;
