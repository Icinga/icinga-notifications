CALL assert_correct_schema_version('v0.2.0-27');

-- The severities were reduced to ok, warning, error and critical. The obsolete severities debug, info and notice are
-- mapped to ok, alert and emerg to critical, while err and crit were renamed to error and critical respectively.
-- To migrate the existing values, the columns are temporarily extended to allow both the old and the new severities.
ALTER TABLE incident
    MODIFY COLUMN severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg', 'error', 'critical');
ALTER TABLE incident_history
    MODIFY COLUMN new_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg', 'error', 'critical'),
    MODIFY COLUMN old_severity enum('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg', 'error', 'critical');

UPDATE incident SET severity = 'ok' WHERE severity IN ('debug', 'info', 'notice');
UPDATE incident SET severity = 'error' WHERE severity = 'err';
UPDATE incident SET severity = 'critical' WHERE severity IN ('crit', 'alert', 'emerg');

UPDATE incident_history SET new_severity = 'ok' WHERE new_severity IN ('debug', 'info', 'notice');
UPDATE incident_history SET new_severity = 'error' WHERE new_severity = 'err';
UPDATE incident_history SET new_severity = 'critical' WHERE new_severity IN ('crit', 'alert', 'emerg');
UPDATE incident_history SET old_severity = 'ok' WHERE old_severity IN ('debug', 'info', 'notice');
UPDATE incident_history SET old_severity = 'error' WHERE old_severity = 'err';
UPDATE incident_history SET old_severity = 'critical' WHERE old_severity IN ('crit', 'alert', 'emerg');

ALTER TABLE incident
    MODIFY COLUMN severity enum('ok', 'warning', 'error', 'critical');
ALTER TABLE incident_history
    MODIFY COLUMN new_severity enum('ok', 'warning', 'error', 'critical'),
    MODIFY COLUMN old_severity enum('ok', 'warning', 'error', 'critical');

-- Escalation conditions may reference severities both in their JSON AST (e.g. "value":"crit") and in their query
-- string (e.g. "qs":"incident_severity>=crit"). Since the only other supported attributes are incident_age and
-- is_managed, these severity names cannot occur as any other value and can thus safely be replaced.
UPDATE rule_entry
    SET `condition` = REGEXP_REPLACE(
        REGEXP_REPLACE(`condition`, '"(debug|info|notice)"', '"ok"'),
        '(?<=[=<>])(debug|info|notice)(?=[&|)"]|$)', 'ok'
    )
    WHERE `condition` LIKE '%incident_severity%';

UPDATE rule_entry
    SET `condition` = REGEXP_REPLACE(
        REGEXP_REPLACE(`condition`, '"err"', '"error"'),
        '(?<=[=<>])err(?=[&|)"]|$)', 'error'
    )
    WHERE `condition` LIKE '%incident_severity%';

UPDATE rule_entry
    SET `condition` = REGEXP_REPLACE(
        REGEXP_REPLACE(`condition`, '"(crit|alert|emerg)"', '"critical"'),
        '(?<=[=<>])(crit|alert|emerg)(?=[&|)"]|$)', 'critical'
    )
    WHERE `condition` LIKE '%incident_severity%';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-28', UNIX_TIMESTAMP() * 1000);
