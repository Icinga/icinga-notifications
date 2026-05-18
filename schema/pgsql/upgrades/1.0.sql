-- This procedure can be used in upgrade scripts to assert that the schema version in the database matches the
-- expected version before applying the upgrade. This is important to prevent users from accidentally skipping
-- intermediate upgrade scripts, which could lead to an inconsistent database state. For instance, since every
-- upgrade script knows its predecessor's version, we can just do "CALL assert_correct_schema_version('v1.0')"
-- at the beginning of the 1.x upgrade scripts to ensure that the 1.0 script has been applied before.
CREATE OR REPLACE PROCEDURE assert_correct_schema_version(expected_version text)
    LANGUAGE plpgsql
AS $$
DECLARE
    actual_version text;
BEGIN
    SELECT version INTO actual_version FROM notifications_schema ORDER BY timestamp DESC LIMIT 1;
    IF actual_version IS NULL THEN
        RAISE 'Schema version not found in notifications_schema table.';
    ELSIF actual_version != expected_version THEN
        RAISE 'Schema version mismatch: expected %, got %. Please apply all previous upgrade scripts in order before applying this one.', expected_version, actual_version;
    END IF;
END;
$$;
COMMENT ON PROCEDURE assert_correct_schema_version IS 'Asserts that the schema version in the database matches the expected version and raises an error if not.';

CREATE TABLE notifications_schema (
  id serial,
  version varchar(64) NOT NULL,
  timestamp bigint NOT NULL,

  CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
  CONSTRAINT idx_notifications_schema_version UNIQUE (version)
);

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0', EXTRACT(EPOCH from NOW()) * 1000);
