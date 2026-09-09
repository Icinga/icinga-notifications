CREATE OR REPLACE FUNCTION assert_correct_schema_version(expected_version text)
    RETURNS void
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL RESTRICTED
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
