DROP PROCEDURE IF EXISTS assert_correct_schema_version;
DELIMITER //
-- This procedure can be used in upgrade scripts to assert that the schema version in the database matches the
-- expected version before applying the upgrade. This is important to prevent users from accidentally skipping
-- intermediate upgrade scripts, which could lead to an inconsistent database state. For instance, since every
-- upgrade script knows its predecessor's version, we can just do "CALL assert_correct_schema_version('v1.0')"
-- at the beginning of the 1.x upgrade scripts to ensure that the 1.0 script has been applied before.
CREATE PROCEDURE assert_correct_schema_version(expected_version text)
    READS SQL DATA
    COMMENT 'Asserts that the schema version in the database matches the expected version and raises an error if not.'
BEGIN
    DECLARE actual_version text;
    DECLARE error_message text;
    SELECT version INTO actual_version FROM notifications_schema ORDER BY id DESC LIMIT 1;
    IF actual_version IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Schema version not found in notifications_schema table.';
    ELSEIF actual_version != expected_version THEN
        -- MySQL/MariaDB doesn't seem to allow to directly use CONCAT in the SIGNAL statement[^1],
        -- so we need to set it to a variable first.
        -- [^1]: https://bugs.mysql.com/bug.php?id=114001
        SET error_message = CONCAT('Schema version mismatch: expected ', expected_version, ', got ', actual_version, '. Apply all previous upgrade scripts in order first.');
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = error_message;
    END IF;
END //
DELIMITER ;

DROP TABLE object_extra_tag;

DROP TABLE incident_event;
ALTER TABLE incident_history DROP FOREIGN KEY fk_incident_history_event;
ALTER TABLE incident_history DROP INDEX fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;

CREATE TABLE object_source (
    object_id binary(32) NOT NULL,
    source_id bigint NOT NULL, -- The specific source that the object referenced by object_id is associated with.

    CONSTRAINT pk_object_source PRIMARY KEY (object_id, source_id),
    CONSTRAINT fk_object_source_object FOREIGN KEY (object_id) REFERENCES object(id),
    CONSTRAINT fk_object_source_source FOREIGN KEY (source_id) REFERENCES source(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- The object ID is going to be re-generated, so all objects and their referencing rows have to be removed.
-- This also clears all incidents and their related tables, so they don't require any data migrations below.
DELETE FROM incident_history;
DELETE FROM incident_rule_escalation_state;
DELETE FROM incident_rule;
DELETE FROM incident_contact;
DELETE FROM incident;
DELETE FROM object_id_tag;
DELETE FROM object;
ALTER TABLE object
  DROP CONSTRAINT fk_object_source,
  DROP COLUMN source_id,
  DROP COLUMN mute_reason;

ALTER TABLE rule ADD COLUMN source_type text AFTER object_filter;
UPDATE rule
  INNER JOIN source ON source.id = rule.source_id
  SET rule.source_type = source.type;

ALTER TABLE rule
  DROP CONSTRAINT fk_rule_source,
  DROP COLUMN source_id,
  MODIFY COLUMN source_type text NOT NULL;

ALTER TABLE source ADD COLUMN locked enum('n', 'y') NOT NULL DEFAULT 'n';

ALTER TABLE source ADD COLUMN client_certificate_subject varchar(768) DEFAULT NULL AFTER listener_password_hash;
ALTER TABLE source ADD CONSTRAINT uk_source_client_certificate_subject UNIQUE (client_certificate_subject);
ALTER TABLE source DROP CONSTRAINT ck_source_listener_username_or_deleted;
ALTER TABLE source ADD CONSTRAINT ck_source_listener_identity_or_deleted
    CHECK (deleted = 'y' OR listener_username IS NOT NULL OR client_certificate_subject IS NOT NULL);
ALTER TABLE source ADD CONSTRAINT ck_source_listener_cert_xor_credentials
    CHECK (listener_username IS NULL OR client_certificate_subject IS NULL);

ALTER TABLE incident ADD COLUMN mute_reason mediumtext DEFAULT NULL AFTER severity;
ALTER TABLE incident ADD COLUMN message longtext;
ALTER TABLE incident ADD COLUMN next_escalation_check_at bigint;
ALTER TABLE incident ADD COLUMN summary longtext NOT NULL AFTER mute_reason;
ALTER TABLE incident ADD COLUMN source_id bigint NOT NULL AFTER object_id;
ALTER TABLE incident ADD CONSTRAINT fk_incident_source FOREIGN KEY (source_id) REFERENCES source(id);
CREATE INDEX idx_incident_recovered_at ON incident(recovered_at);
CREATE INDEX idx_incident_object_id_recovered_at ON incident(object_id, recovered_at);
CREATE INDEX idx_incident_next_escalation_check_at ON incident(next_escalation_check_at);
CREATE INDEX idx_incident_recovered_at_next_escalation_check_at ON incident(recovered_at, next_escalation_check_at);

ALTER TABLE incident_contact ADD COLUMN changed_at bigint NOT NULL;
ALTER TABLE incident_contact
  ADD COLUMN id bigint NOT NULL AUTO_INCREMENT FIRST,
  ADD CONSTRAINT pk_incident_contact PRIMARY KEY (id);
ALTER TABLE incident_contact ADD COLUMN event_types text AFTER role;

ALTER TABLE incident_history ADD COLUMN event_id binary(16) AFTER incident_id; -- used for external references
-- This will be used by the query in the [Incident.RetriggerEscalations] method in icinga-notifications.
CREATE INDEX idx_incident_history_event_id_incident_id ON incident_history(event_id, incident_id);

-- The external UUIDs are stored as binary(16) and are no longer required for deleted rows.
ALTER TABLE channel
    DROP INDEX uk_channel_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE channel SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE deleted = 'n';
ALTER TABLE channel
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_channel_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_channel_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);

ALTER TABLE contact
    DROP INDEX uk_contact_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE contact SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE deleted = 'n';
ALTER TABLE contact
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_contact_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_contact_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);

ALTER TABLE contactgroup
    DROP INDEX uk_contactgroup_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE contactgroup SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE deleted = 'n';
ALTER TABLE contactgroup
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_contactgroup_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_contactgroup_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);

ALTER TABLE channel
  ADD COLUMN validation_result text;

-- This script is used to upgrade the escalation condition column in the rule_escalation table from a raw filter
-- string to the new JSON object format. The raw filter string is a simple string that represents a logical condition,
-- while the new JSON object format is a structured representation of the same condition. For filter chains, this
-- script assumes that the logical operator is always &, so if somehow a different logical op is used, the resulting
-- JSON object will be invalid.
DROP PROCEDURE IF EXISTS upgrade_escalation_condition_to_json_object;
DELIMITER //
CREATE PROCEDURE upgrade_escalation_condition_to_json_object()
    LANGUAGE SQL
    MODIFIES SQL DATA
BEGIN
    DECLARE escalation_id int;
    DECLARE escalation_cond text;
    DECLARE tmp_escalation_cond text;
    DECLARE attribute text;
    DECLARE operator text;
    DECLARE value text;
    DECLARE json_rules json;

    DECLARE done int;
    DECLARE cur CURSOR FOR SELECT id, `condition` FROM rule_escalation WHERE `condition` IS NOT NULL;
    -- Set up a handler to exit the loop when there are no more records to process.
    -- See https://dev.mysql.com/doc/refman/8.4/en/fetch.html
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET done = 1;

    SET escalation_id = 0;
    SET done = 0;

    OPEN cur;
    records_loop: LOOP
        FETCH cur INTO escalation_id, escalation_cond;
        IF done THEN
            LEAVE records_loop;
        END IF;

        -- Reset the state variables for each iteration.
        SET json_rules = JSON_ARRAY();
        SET attribute = NULL;
        SET operator = NULL;
        SET value = NULL;

        IF INSTR(escalation_cond, '&') = 0 THEN
            SET attribute = REGEXP_SUBSTR(escalation_cond, '[^!=><]+');
            SET operator = REGEXP_SUBSTR(escalation_cond, '[!=><]+');
            IF attribute IS NULL OR operator IS NULL THEN
                ITERATE records_loop; -- We have an invalid condition, so skip this record.
            END IF;
            SET value = SUBSTR(escalation_cond, LENGTH(attribute) + LENGTH(operator) + 1);
            UPDATE rule_escalation SET `condition` = JSON_OBJECT(
                'version', 2,
                'qs', escalation_cond,
                'ast', JSON_OBJECT('op', operator, 'attributes', JSON_ARRAY(attribute), 'value', value)
            ) WHERE id = escalation_id;
        ELSE
            -- Otherwise, we have a & filter chain, so we need to split the raw filter string into individual
            -- conditions and convert each one to a JSON literal like this:
            -- {"ast":{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"1h"},...]}}
            SET tmp_escalation_cond = escalation_cond;
            WHILE tmp_escalation_cond IS NOT NULL AND tmp_escalation_cond != '' DO
                SET attribute = REGEXP_SUBSTR(tmp_escalation_cond, '[^!=><]+');
                SET operator = REGEXP_SUBSTR(tmp_escalation_cond, '[!=><]+');
                -- The value is located after the operator and before the next & or the end of the string. This is
                -- not same as the single condition case, because we need to get the value after the operator but
                -- before the next & or end of the string. So, we use a Perl-style lookbehind assertion[^1][^2][^3]
                -- to find the value after the operator and before the next & or end of the string.
                --
                -- [^1]: https://perldoc.perl.org/perlre#Lookaround-Assertions
                -- [^2]: https://mariadb.com/docs/server/reference/sql-functions/string-functions/regular-expressions-functions/pcre
                -- [^3]: MySQL 8.0 uses the ICU library for its REGEXP, which supports all PCRE features, including
                -- lookbehind assertions. https://unicode-org.github.io/icu/userguide/strings/regexp.html#regular-expression-operators
                SET value = REGEXP_SUBSTR(tmp_escalation_cond, CONCAT('(?<=', operator, ')[^&]+'));
                IF attribute IS NULL OR operator IS NULL OR value IS NULL THEN
                    ITERATE records_loop; -- We have an invalid condition, so skip this record.
                END IF;

                -- We need to use JSON_EXTRACT as a workaround to avoid double encoding the resulting JSON object
                -- when appending it to the json_rules array. Without this, the resulting JSON object would be double
                -- encoded causing the final filter chain construction to be invalid.
                SET json_rules = JSON_ARRAY_APPEND(json_rules, '$', JSON_EXTRACT(JSON_OBJECT('op', operator, 'attributes', JSON_ARRAY(attribute), 'value', value), '$'));
                -- Remove the processed condition from the tmp_escalation_cond string including the leading & operator.
                SET tmp_escalation_cond = TRIM(LEADING '&' FROM SUBSTRING(tmp_escalation_cond, LENGTH(attribute) + LENGTH(operator) + LENGTH(value) + 1));
            END WHILE;

            -- Finally, construct the final JSON object with the logical operator & and the array of converted rules,
            -- and update the corresponding tuple in the table with the resulting JSON object.
            UPDATE rule_escalation SET `condition` = JSON_OBJECT(
                'version', 2,
                'qs', escalation_cond,
                -- For the same reason as above, we need to extract the entire JSON object ($ is the root selector).
                'ast', JSON_EXTRACT(JSON_OBJECT('op', '&', 'rules', JSON_EXTRACT(json_rules, '$')), '$')
            ) WHERE id = escalation_id;
        END IF;
    END LOOP;
    CLOSE cur;
END //
DELIMITER ;

-- Call the procedure to perform the upgrade and then drop it immediately as it's no longer needed after it's completed.
CALL upgrade_escalation_condition_to_json_object();
DROP PROCEDURE upgrade_escalation_condition_to_json_object;

-- The web UI does not allow the escalation at position 0 to have a condition. Hence, all existing
-- escalations are moved up by one position so that none of them occupies position 0. Since the unique constraint
-- on (rule_id, position) is checked for each row immediately, the rows are updated in descending order of position.
UPDATE rule_escalation SET position = position + 1 WHERE position IS NOT NULL ORDER BY position DESC;

ALTER TABLE rule ADD COLUMN type enum('notification', 'escalation') AFTER source_type; -- NOT NULL is enforced via CHECK not to default to 'notification'
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ADD CONSTRAINT ck_rule_type_notnull CHECK (type IS NOT NULL);

-- Rename rule_escalation to rule_entry.
RENAME TABLE rule_escalation TO rule_entry;
ALTER TABLE rule_entry
    RENAME INDEX uk_rule_escalation_rule_id_position TO uk_rule_entry_rule_id_position,
    RENAME INDEX idx_rule_escalation_changed_at TO idx_rule_entry_changed_at,
    DROP FOREIGN KEY fk_rule_escalation_rule,
    DROP FOREIGN KEY fk_rule_escalation_rule_escalation,
    DROP CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for,
    DROP CONSTRAINT ck_rule_escalation_non_deleted_needs_position,
    ADD CONSTRAINT ck_rule_entry_not_both_condition_and_fallback_for CHECK (NOT (`condition` IS NOT NULL AND fallback_for IS NOT NULL)),
    ADD CONSTRAINT ck_rule_entry_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),
    ADD CONSTRAINT fk_rule_entry_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    ADD CONSTRAINT fk_rule_entry_rule_entry FOREIGN KEY (fallback_for) REFERENCES rule_entry(id);

-- Rename rule_escalation_recipient to rule_entry_recipient.
RENAME TABLE rule_escalation_recipient TO rule_entry_recipient;
ALTER TABLE rule_entry_recipient
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    RENAME INDEX idx_rule_escalation_recipient_changed_at TO idx_rule_entry_recipient_changed_at,
    DROP FOREIGN KEY fk_rule_escalation_recipient_rule_escalation,
    DROP FOREIGN KEY fk_rule_escalation_recipient_contact,
    DROP FOREIGN KEY fk_rule_escalation_recipient_contactgroup,
    DROP FOREIGN KEY fk_rule_escalation_recipient_schedule,
    DROP FOREIGN KEY fk_rule_escalation_recipient_channel,
    DROP CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient,
    ADD CONSTRAINT ck_rule_entry_recipient_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
    ADD CONSTRAINT fk_rule_entry_recipient_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT fk_rule_entry_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    ADD CONSTRAINT fk_rule_entry_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    ADD CONSTRAINT fk_rule_entry_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    ADD CONSTRAINT fk_rule_entry_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id);

ALTER TABLE incident_history
    DROP FOREIGN KEY fk_incident_history_incident_rule_escalation_state;

ALTER TABLE incident_rule_escalation_state
    DROP FOREIGN KEY fk_incident_rule_escalation_state_rule_escalation,
    DROP PRIMARY KEY,
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    ADD CONSTRAINT fk_incident_rule_escalation_state_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_entry_id);

-- Rename incident_history.rule_escalation_id to rule_entry_id.
ALTER TABLE incident_history
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    DROP FOREIGN KEY fk_incident_history_rule_escalation,
    ADD CONSTRAINT fk_incident_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_entry_id) REFERENCES incident_rule_escalation_state(incident_id, rule_entry_id);

-- The severities were reduced to ok, warning, error and critical. As all incidents were removed above, the incident
-- tables don't need any value mapping, only rule entry conditions referencing obsolete severities must be migrated.
ALTER TABLE incident
    MODIFY COLUMN severity enum('ok', 'warning', 'error', 'critical');
ALTER TABLE incident_history
    MODIFY COLUMN new_severity enum('ok', 'warning', 'error', 'critical'),
    MODIFY COLUMN old_severity enum('ok', 'warning', 'error', 'critical');

-- Escalation conditions may reference severities both in their JSON AST (e.g. "value":"crit") and in their query
-- string (e.g. "qs":"incident_severity>=crit"). Since the only other supported attributes are incident_age and
-- is_managed, these severity names cannot occur as any other value and can thus safely be replaced. The obsolete
-- severities debug, info and notice are mapped to ok, alert and emerg to critical, while err and crit were renamed
-- to error and critical respectively.
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

CREATE TABLE channel_state (
    state_key binary(16) NOT NULL,
    channel_id bigint NOT NULL,
    incident_id bigint NOT NULL, -- The incident this state is associated with.
    value varchar(4096) NOT NULL,

    CONSTRAINT pk_channel_state PRIMARY KEY (state_key),
    CONSTRAINT fk_channel_state_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
    CONSTRAINT fk_channel_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE notification_history (
    id bigint NOT NULL AUTO_INCREMENT,
    object_id binary(32) NOT NULL,
    event_id binary(16) NOT NULL,
    contact_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint NOT NULL,
    incident_id bigint,
    event_message longtext NOT NULL,
    event_summary longtext NOT NULL,
    state enum('sent', 'failed'),
    triggered_at bigint NOT NULL,
    incident_closed enum('n', 'y') NOT NULL DEFAULT 'n',  -- Indicates whether the incident was closed at the time of the notification.

    delivery_result mediumtext, -- The result of the notification delivery attempt (if any) as a JSON string.

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT ck_notification_history_state_notnull CHECK (state IS NOT NULL),
    CONSTRAINT fk_notification_history_object_id FOREIGN KEY (object_id) REFERENCES object(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_notification_history_triggered_at ON notification_history(triggered_at);
CREATE INDEX idx_notification_history_state_triggered_at ON notification_history(state, triggered_at);

CREATE TABLE skipped_notification_history (
    id bigint NOT NULL AUTO_INCREMENT,
    notification_history_id bigint NOT NULL, -- The actual notification due to which the notification was skipped.
    rule_id bigint NOT NULL,
    rule_entry_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,

    CONSTRAINT pk_skipped_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_skipped_notification_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE job_queue (
    id binary(16) NOT NULL, -- used for event deduplication
    last_update bigint NOT NULL,
    state smallint NOT NULL DEFAULT 0, -- pending (0), processing (1), done (2), or error (64).
    envelope longtext NOT NULL,

    CONSTRAINT pk_job_queue PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- These indices are used to speed up the cleanup queries by last_update and state.
CREATE INDEX idx_job_queue_last_update ON job_queue (last_update);
CREATE INDEX idx_job_queue_last_update_state ON job_queue (last_update, state);

CREATE TABLE job_processing_lock (
    object_id binary(32) NOT NULL, -- No foreign key, object might not exist at this point.
    job_queue_id binary(16) NOT NULL, -- references job_queue.id

    CONSTRAINT pk_job_processing_lock PRIMARY KEY (object_id),
    CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id),
    CONSTRAINT uk_job_processing_lock_job_queue_id UNIQUE (job_queue_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE notifications_schema (
    id int NOT NULL AUTO_INCREMENT,
    version varchar(64) NOT NULL,
    timestamp bigint NOT NULL,

    CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
    CONSTRAINT uk_notifications_schema_version UNIQUE (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0', UNIX_TIMESTAMP() * 1000);
