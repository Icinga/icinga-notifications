-- Redefine assert_correct_schema_version() as it determined the latest schema version by timestamp, which isn't
-- unique if upgrades are applied in quick succession.
DROP ROUTINE IF EXISTS assert_correct_schema_version(text);
CREATE FUNCTION assert_correct_schema_version(expected_version text)
    RETURNS void
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL RESTRICTED
AS $$
DECLARE
    actual_version text;
BEGIN
    SELECT version INTO actual_version FROM notifications_schema ORDER BY id DESC LIMIT 1;

    IF actual_version IS NULL THEN
        RAISE 'Schema version not found in notifications_schema table.';
    ELSIF actual_version != expected_version THEN
        RAISE 'Schema version mismatch: expected %, got %. Please apply all previous upgrade scripts in order before applying this one.', expected_version, actual_version;
    END IF;
END;
$$;

-- Until the upgrade scripts stamped their own schema version, 003-schema-version-table.sql stamped v1.0. Databases
-- upgraded by then are assumed to have applied all upgrades preceding this one, so their version is corrected first.
UPDATE notifications_schema SET version = 'v0.2.0-22' WHERE version = 'v1.0';

SELECT assert_correct_schema_version('v0.2.0-22');

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-23', EXTRACT(EPOCH from NOW()) * 1000);
