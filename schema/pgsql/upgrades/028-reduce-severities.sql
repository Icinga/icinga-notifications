SELECT assert_correct_schema_version('v0.2.0-27');

-- The severities were reduced to ok, warning, error and critical. The obsolete severities debug, info and notice are
-- mapped to ok, alert and emerg to critical, while err and crit were renamed to error and critical respectively.
ALTER TYPE severity RENAME TO severity_old;
CREATE TYPE severity AS ENUM ('ok', 'warning', 'error', 'critical');

ALTER TABLE incident
    ALTER COLUMN severity TYPE severity USING (CASE
        WHEN severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN severity = 'err' THEN 'error'
        WHEN severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE severity::text
    END)::severity;

ALTER TABLE incident_history
    ALTER COLUMN new_severity TYPE severity USING (CASE
        WHEN new_severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN new_severity = 'err' THEN 'error'
        WHEN new_severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE new_severity::text
    END)::severity,
    ALTER COLUMN old_severity TYPE severity USING (CASE
        WHEN old_severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN old_severity = 'err' THEN 'error'
        WHEN old_severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE old_severity::text
    END)::severity;

DROP TYPE severity_old;

-- Escalation conditions may reference severities both in their JSON AST (e.g. "value":"crit") and in their query
-- string (e.g. "qs":"incident_severity>=crit"). Since the only other supported attributes are incident_age and
-- is_managed, these severity names cannot occur as any other value and can thus safely be replaced.
UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"(debug|info|notice)"', '"ok"', 'g'),
        '(?<=[=<>])(debug|info|notice)(?=[&|)"]|$)', 'ok', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"err"', '"error"', 'g'),
        '(?<=[=<>])err(?=[&|)"]|$)', 'error', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"(crit|alert|emerg)"', '"critical"', 'g'),
        '(?<=[=<>])(crit|alert|emerg)(?=[&|)"]|$)', 'critical', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-28', EXTRACT(EPOCH from NOW()) * 1000);
